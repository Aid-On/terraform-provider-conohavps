// Identity のリソース（ロール・サブユーザー・クレデンシャル）とパーミッション一覧のデータソースの
// 単体テストを提供する. 偽の ConoHa API にドキュメントどおりの Identity API を足し、
// ロール → ロールを付けたサブユーザー → サブユーザーのクレデンシャル の連鎖を、
// 作成・その場での更新・作り直し・インポート・外部での変更・削除まで、実際の API を使わずに検証する.

package resource_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/fakeapi"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// 偽の Identity API が持つロール.
type fakeRole struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Visibility  string   `json:"visibility"`
	Permissions []string `json:"permissions"`
}

// 偽の Identity API が持つサブユーザー.
type fakeSubUser struct {
	ID       string
	Name     string
	Password string
	RoleIDs  []string
}

// 偽の Identity API が持つクレデンシャル.
type fakeCredential struct {
	UserID   string
	TenantID string
	Access   string
	Secret   string
}

// 受け取ったリクエスト（検証用）.
type identityCall struct {
	Method string
	Path   string
	Body   map[string]any
}

// Identity API の偽物. ドキュメントの制約（パーミッション・ロールを 0 にできない、
// 付与中のロールは削除できない、紐づけはロール ID で行う、クレデンシャルは 3 つまで）を再現する.
type identityFake struct {
	*fakeapi.Server
	mu    sync.Mutex
	roles map[string]*fakeRole
	users map[string]*fakeSubUser
	creds map[string]*fakeCredential
	calls []identityCall
	// サブユーザーとしてのトークン発行（パスワードの検証）の記録と、0 以外なら返す状態コード
	tokenCalls  []identityTokenCall
	tokenStatus int
}

// サブユーザーとしてのトークン発行のリクエスト（検証用）.
type identityTokenCall struct {
	UserID    string
	ProjectID string
	Methods   []string
	AuthToken string // X-Auth-Token ヘッダー（プロバイダのトークンを付けていないことを確かめる）
	Query     string
}

var identityFakePermissions = []map[string]string{
	{"name": "post-token", "description": "トークン発行"},
	{"name": "get-server-list", "description": "サーバー一覧取得"},
	{"name": "get-server", "description": "サーバー詳細取得"},
	{"name": "post-server-action-reboot", "description": "サーバー再起動"},
	{"name": "delete-server", "description": "サーバー削除"},
}

func newIdentityFake(t *testing.T) *identityFake {
	f := &identityFake{
		Server: fakeapi.New(t),
		roles:  map[string]*fakeRole{},
		users:  map[string]*fakeSubUser{},
		creds:  map[string]*fakeCredential{},
	}
	// 標準で用意されているロール
	for _, name := range []string{"gmo-identity", "gmo-compute", "gmo-network"} {
		id := "std-" + name
		f.roles[id] = &fakeRole{ID: id, Name: name, Visibility: "public", Permissions: []string{"post-token"}}
	}

	const v3 = "/identity/v3"
	f.handle("GET "+v3+"/permissions", f.listPermissions)
	f.handle("POST "+v3+"/sub-users/roles", f.createRole)
	f.handle("GET "+v3+"/sub-users/roles", f.listRoles)
	f.handle("GET "+v3+"/sub-users/roles/{id}", f.getRole)
	f.handle("PUT "+v3+"/sub-users/roles/{id}", f.updateRole)
	f.handle("DELETE "+v3+"/sub-users/roles/{id}", f.deleteRole)
	f.handle("POST "+v3+"/sub-users/roles/{id}/{action}", f.changePermissions)
	f.handle("POST "+v3+"/sub-users", f.createSubUser)
	f.handle("GET "+v3+"/sub-users/{id}", f.getSubUser)
	f.handle("PUT "+v3+"/sub-users/{id}", f.updateSubUser)
	f.handle("DELETE "+v3+"/sub-users/{id}", f.deleteSubUser)
	f.handle("POST "+v3+"/sub-users/{id}/{action}", f.changeRoles)
	f.handle("POST "+v3+"/users/{uid}/credentials/OS-EC2", f.createCredential)
	f.handle("GET "+v3+"/users/{uid}/credentials/OS-EC2/{access}", f.getCredential)
	f.handle("DELETE "+v3+"/users/{uid}/credentials/OS-EC2/{access}", f.deleteCredential)

	// トークン発行は fakeapi がプロバイダ自身のログイン用に受けている. ServeMux はホスト付きの
	// パターンをホスト無しのものより優先するため、ホスト付きで登録してサブユーザーのログインだけを受け、
	// それ以外はホストを変えて fakeapi のハンドラへ回す
	u, err := url.Parse(f.URL)
	if err != nil {
		t.Fatal(err)
	}
	f.Mux.HandleFunc("POST "+u.Hostname()+v3+"/auth/tokens", f.issueToken)
	return f
}

// サブユーザーのトークン発行を受ける. パスワードが合えば 201、違えば 401 を返す.
func (f *identityFake) issueToken(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		identityBadRequest(w, err.Error())
		return
	}
	var body struct {
		Auth struct {
			Identity struct {
				Methods  []string `json:"methods"`
				Password struct {
					User struct {
						ID       string `json:"id"`
						Password string `json:"password"`
					} `json:"user"`
				} `json:"password"`
			} `json:"identity"`
			Scope struct {
				Project struct {
					ID string `json:"id"`
				} `json:"project"`
			} `json:"scope"`
		} `json:"auth"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		identityBadRequest(w, err.Error())
		return
	}
	user := body.Auth.Identity.Password.User

	f.mu.Lock()
	sub, ok := f.users[user.ID]
	if ok {
		f.tokenCalls = append(f.tokenCalls, identityTokenCall{
			UserID: user.ID, ProjectID: body.Auth.Scope.Project.ID, Methods: body.Auth.Identity.Methods,
			AuthToken: r.Header.Get("X-Auth-Token"), Query: r.URL.RawQuery,
		})
	}
	status := f.tokenStatus
	f.mu.Unlock()

	if !ok {
		// プロバイダ自身のログイン
		r2 := r.Clone(r.Context())
		r2.Host = "provider.invalid"
		r2.Body = io.NopCloser(bytes.NewReader(raw))
		f.Mux.ServeHTTP(w, r2)
		return
	}
	switch {
	case status != 0:
		identityStatus(w, status)
	case user.Password != sub.Password:
		identityStatus(w, http.StatusUnauthorized)
	default:
		w.Header().Set("X-Subject-Token", "token-of-"+user.ID)
		fakeapi.WriteJSON(w, http.StatusCreated, map[string]any{"token": map[string]any{"expires_at": "2099-01-01T00:00:00.000000Z"}})
	}
}

func identityStatus(w http.ResponseWriter, status int) {
	fakeapi.WriteJSON(w, status, map[string]any{"error": map[string]any{"code": status, "message": http.StatusText(status)}})
}

// サブユーザーとしてのトークン発行の記録を返す.
func (f *identityFake) tokens() []identityTokenCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.tokenCalls)
}

// トークンを確かめ、リクエストを記録してからハンドラを呼ぶ.
func (f *identityFake) handle(pattern string, h func(w http.ResponseWriter, r *http.Request, body map[string]any)) {
	f.Mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		if !fakeapi.Authorized(w, r) {
			return
		}
		var body map[string]any
		if r.Method == http.MethodPost || r.Method == http.MethodPut {
			if err := fakeapi.ReadJSON(r, &body); err != nil {
				fakeapi.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
				return
			}
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.calls = append(f.calls, identityCall{Method: r.Method, Path: strings.TrimPrefix(r.URL.Path, "/identity/v3"), Body: body})
		h(w, r, body)
	})
}

func identityBadRequest(w http.ResponseWriter, msg string) {
	fakeapi.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": msg}})
}

func identityNotFound(w http.ResponseWriter) {
	fakeapi.WriteJSON(w, http.StatusNotFound, map[string]any{"error": map[string]any{"message": "not found"}})
}

func identityStrings(v any) []string {
	var out []string
	list, _ := v.([]any)
	for _, s := range list {
		if str, ok := s.(string); ok {
			out = append(out, str)
		}
	}
	return out
}

func (f *identityFake) listPermissions(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
	fakeapi.WriteJSON(w, http.StatusOK, map[string]any{"permissions": identityFakePermissions})
}

func (f *identityFake) roleJSON(role *fakeRole) map[string]any {
	return map[string]any{"role": role}
}

func (f *identityFake) createRole(w http.ResponseWriter, _ *http.Request, body map[string]any) {
	in, _ := body["role"].(map[string]any)
	name, _ := in["name"].(string)
	permissions := identityStrings(in["permissions"])
	if name == "" || len(permissions) == 0 {
		identityBadRequest(w, "name and permissions are required")
		return
	}
	role := &fakeRole{ID: f.NewID("role"), Name: name, Visibility: "private", Permissions: permissions}
	f.roles[role.ID] = role
	fakeapi.WriteJSON(w, http.StatusOK, f.roleJSON(role))
}

func (f *identityFake) listRoles(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
	var roles []map[string]any
	for _, role := range f.roles {
		// 一覧にはパーミッションは含まれない
		roles = append(roles, map[string]any{"id": role.ID, "name": role.Name, "visibility": role.Visibility})
	}
	fakeapi.WriteJSON(w, http.StatusOK, map[string]any{"roles": roles})
}

func (f *identityFake) getRole(w http.ResponseWriter, r *http.Request, _ map[string]any) {
	role, ok := f.roles[r.PathValue("id")]
	if !ok {
		identityNotFound(w)
		return
	}
	fakeapi.WriteJSON(w, http.StatusOK, f.roleJSON(role))
}

func (f *identityFake) updateRole(w http.ResponseWriter, r *http.Request, body map[string]any) {
	role, ok := f.roles[r.PathValue("id")]
	if !ok {
		identityNotFound(w)
		return
	}
	in, _ := body["role"].(map[string]any)
	if name, _ := in["name"].(string); name != "" {
		role.Name = name
	}
	fakeapi.WriteJSON(w, http.StatusOK, f.roleJSON(role))
}

func (f *identityFake) deleteRole(w http.ResponseWriter, r *http.Request, _ map[string]any) {
	id := r.PathValue("id")
	if _, ok := f.roles[id]; !ok {
		identityNotFound(w)
		return
	}
	for _, u := range f.users {
		if slices.Contains(u.RoleIDs, id) {
			fakeapi.WriteJSON(w, http.StatusConflict, map[string]any{"error": map[string]any{"message": "role is assigned to a sub-user"}})
			return
		}
	}
	delete(f.roles, id)
	w.WriteHeader(http.StatusNoContent)
}

func (f *identityFake) changePermissions(w http.ResponseWriter, r *http.Request, body map[string]any) {
	role, ok := f.roles[r.PathValue("id")]
	if !ok {
		identityNotFound(w)
		return
	}
	permissions := identityStrings(body["permissions"])
	switch r.PathValue("action") {
	case "assign":
		for _, p := range permissions {
			if !slices.Contains(role.Permissions, p) {
				role.Permissions = append(role.Permissions, p)
			}
		}
	case "unassign":
		kept := slices.DeleteFunc(slices.Clone(role.Permissions), func(p string) bool { return slices.Contains(permissions, p) })
		if len(kept) == 0 {
			identityBadRequest(w, "a role needs at least one permission")
			return
		}
		role.Permissions = kept
	default:
		identityNotFound(w)
		return
	}
	fakeapi.WriteJSON(w, http.StatusOK, f.roleJSON(role))
}

// ID または名前からロールを引く（サブユーザー作成はどちらでも受け付ける）.
func (f *identityFake) findRole(ref string) *fakeRole {
	if role, ok := f.roles[ref]; ok {
		return role
	}
	for _, role := range f.roles {
		if role.Name == ref {
			return role
		}
	}
	return nil
}

func (f *identityFake) userJSON(u *fakeSubUser) map[string]any {
	roles := []map[string]any{}
	for _, id := range u.RoleIDs {
		roles = append(roles, map[string]any{"id": id, "name": f.roles[id].Name})
	}
	return map[string]any{"user": map[string]any{"id": u.ID, "name": u.Name, "roles": roles}}
}

func (f *identityFake) createSubUser(w http.ResponseWriter, _ *http.Request, body map[string]any) {
	in, _ := body["user"].(map[string]any)
	password, _ := in["password"].(string)
	refs := identityStrings(in["roles"])
	if password == "" || len(refs) == 0 {
		identityBadRequest(w, "password and roles are required")
		return
	}
	u := &fakeSubUser{ID: f.NewID("subuser"), Password: password}
	u.Name = "gncu" + strings.TrimPrefix(u.ID, "subuser-")
	for _, ref := range refs {
		role := f.findRole(ref)
		if role == nil {
			identityBadRequest(w, "unknown role "+ref)
			return
		}
		u.RoleIDs = append(u.RoleIDs, role.ID)
	}
	f.users[u.ID] = u
	fakeapi.WriteJSON(w, http.StatusOK, f.userJSON(u))
}

func (f *identityFake) getSubUser(w http.ResponseWriter, r *http.Request, _ map[string]any) {
	u, ok := f.users[r.PathValue("id")]
	if !ok {
		identityNotFound(w)
		return
	}
	fakeapi.WriteJSON(w, http.StatusOK, f.userJSON(u))
}

func (f *identityFake) updateSubUser(w http.ResponseWriter, r *http.Request, body map[string]any) {
	u, ok := f.users[r.PathValue("id")]
	if !ok {
		identityNotFound(w)
		return
	}
	in, _ := body["user"].(map[string]any)
	password, _ := in["password"].(string)
	if password == "" {
		identityBadRequest(w, "password is required")
		return
	}
	u.Password = password
	fakeapi.WriteJSON(w, http.StatusOK, f.userJSON(u))
}

func (f *identityFake) deleteSubUser(w http.ResponseWriter, r *http.Request, _ map[string]any) {
	id := r.PathValue("id")
	if _, ok := f.users[id]; !ok {
		identityNotFound(w)
		return
	}
	delete(f.users, id)
	w.WriteHeader(http.StatusNoContent)
}

func (f *identityFake) changeRoles(w http.ResponseWriter, r *http.Request, body map[string]any) {
	u, ok := f.users[r.PathValue("id")]
	if !ok {
		identityNotFound(w)
		return
	}
	ids := identityStrings(body["roles"])
	for _, id := range ids {
		// 紐づけと解除はロール ID で指定する
		if _, ok := f.roles[id]; !ok {
			identityBadRequest(w, "unknown role ID "+id)
			return
		}
	}
	switch r.PathValue("action") {
	case "assign":
		for _, id := range ids {
			if !slices.Contains(u.RoleIDs, id) {
				u.RoleIDs = append(u.RoleIDs, id)
			}
		}
	case "unassign":
		kept := slices.DeleteFunc(slices.Clone(u.RoleIDs), func(id string) bool { return slices.Contains(ids, id) })
		if len(kept) == 0 {
			identityBadRequest(w, "a sub-user needs at least one role")
			return
		}
		u.RoleIDs = kept
	default:
		identityNotFound(w)
		return
	}
	fakeapi.WriteJSON(w, http.StatusOK, f.userJSON(u))
}

func identityCredentialJSON(c *fakeCredential) map[string]any {
	return map[string]any{"credential": map[string]any{
		"user_id": c.UserID, "tenant_id": c.TenantID, "access": c.Access, "secret": c.Secret,
		"trust_id": nil, "links": map[string]any{"self": "string"},
	}}
}

func (f *identityFake) createCredential(w http.ResponseWriter, r *http.Request, body map[string]any) {
	uid := r.PathValue("uid")
	tenant, _ := body["tenant_id"].(string)
	if tenant == "" {
		identityBadRequest(w, "tenant_id is required")
		return
	}
	n := 0
	for _, c := range f.creds {
		if c.UserID == uid {
			n++
		}
	}
	if n >= 3 {
		fakeapi.WriteJSON(w, http.StatusForbidden, map[string]any{"error": map[string]any{"message": "credential limit exceeded"}})
		return
	}
	id := f.NewID("access")
	c := &fakeCredential{UserID: uid, TenantID: tenant, Access: id, Secret: "secret-of-" + id}
	f.creds[c.Access] = c
	fakeapi.WriteJSON(w, http.StatusCreated, identityCredentialJSON(c))
}

func (f *identityFake) getCredential(w http.ResponseWriter, r *http.Request, _ map[string]any) {
	c, ok := f.creds[r.PathValue("access")]
	if !ok || c.UserID != r.PathValue("uid") {
		identityNotFound(w)
		return
	}
	fakeapi.WriteJSON(w, http.StatusOK, identityCredentialJSON(c))
}

func (f *identityFake) deleteCredential(w http.ResponseWriter, r *http.Request, _ map[string]any) {
	c, ok := f.creds[r.PathValue("access")]
	if !ok || c.UserID != r.PathValue("uid") {
		identityNotFound(w)
		return
	}
	delete(f.creds, c.Access)
	w.WriteHeader(http.StatusNoContent)
}

// 条件に合う最後のリクエストを返す.
func (f *identityFake) lastCall(method, pathPattern string) (identityCall, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	re := regexp.MustCompile("^" + pathPattern + "$")
	for i := len(f.calls) - 1; i >= 0; i-- {
		if c := f.calls[i]; c.Method == method && re.MatchString(c.Path) {
			return c, nil
		}
	}
	return identityCall{}, fmt.Errorf("no %s request matched %s", method, pathPattern)
}

// 条件に合うリクエストの位置（最後のもの）を返す.
func (f *identityFake) callIndex(method, pathPattern string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	re := regexp.MustCompile("^" + pathPattern + "$")
	for i := len(f.calls) - 1; i >= 0; i-- {
		if c := f.calls[i]; c.Method == method && re.MatchString(c.Path) {
			return i
		}
	}
	return -1
}

// リクエストの本文を JSON のパスでたどって値を比べる.
func identityExpectBody(f *identityFake, method, pathPattern string, keys []string, want any) resource.TestCheckFunc {
	return func(*terraform.State) error {
		call, err := f.lastCall(method, pathPattern)
		if err != nil {
			return err
		}
		var v any = call.Body
		for _, k := range keys {
			m, _ := v.(map[string]any)
			v = m[k]
		}
		if list, ok := v.([]any); ok {
			got := identityStrings(list)
			sort.Strings(got)
			v = got
		}
		if !reflect.DeepEqual(v, want) {
			return fmt.Errorf("%s %s body %v = %#v, want %#v", method, call.Path, keys, v, want)
		}
		return nil
	}
}

// 属性が State で機密（sensitive）として扱われていることを確かめる.
type identitySensitiveCheck struct{ address, attribute string }

func (c identitySensitiveCheck) CheckState(_ context.Context, req statecheck.CheckStateRequest, resp *statecheck.CheckStateResponse) {
	if req.State == nil || req.State.Values == nil || req.State.Values.RootModule == nil {
		resp.Error = fmt.Errorf("state is empty")
		return
	}
	for _, r := range req.State.Values.RootModule.Resources {
		if r.Address != c.address {
			continue
		}
		var sensitive map[string]any
		if err := json.Unmarshal(r.SensitiveValues, &sensitive); err != nil {
			resp.Error = err
			return
		}
		if sensitive[c.attribute] != true {
			resp.Error = fmt.Errorf("%s.%s is not sensitive", c.address, c.attribute)
		}
		return
	}
	resp.Error = fmt.Errorf("%s is not in the state", c.address)
}

type identityConfig struct {
	roleName    string
	permissions string // HCL の集合
	password    string
	roles       string // HCL の集合
	tenant      string // 空なら指定しない
}

func (f *identityFake) config(c identityConfig) string {
	tenant := ""
	if c.tenant != "" {
		tenant = fmt.Sprintf("  tenant_id = %q\n", c.tenant)
	}
	return f.ProviderConfig() + fmt.Sprintf(`
data "conohavps_permissions" "all" {}

resource "conohavps_role" "agent" {
  name        = %q
  permissions = %s
}

resource "conohavps_subuser" "agent" {
  password = %q
  roles    = %s
}

resource "conohavps_credential" "agent" {
  user_id = conohavps_subuser.agent.id
%s}
`, c.roleName, c.permissions, c.password, c.roles, tenant)
}

func TestIdentityChain(t *testing.T) {
	f := newIdentityFake(t)

	step1 := identityConfig{
		roleName:    "agent-readonly",
		permissions: `["get-server-list", "get-server"]`,
		password:    "Agent-pass-1",
		roles:       `[conohavps_role.agent.id, "gmo-identity"]`,
	}
	step2 := identityConfig{
		roleName:    "agent-ops",
		permissions: `["get-server-list", "post-server-action-reboot"]`,
		password:    "Agent-pass-2",
		roles:       `[conohavps_role.agent.id, "gmo-compute"]`,
	}
	step3 := step2
	step3.tenant = "tenant-2"

	var roleID, userID, access string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		CheckDestroy: func(*terraform.State) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			if len(f.users) != 0 || len(f.creds) != 0 {
				return fmt.Errorf("sub-users %d, credentials %d remain", len(f.users), len(f.creds))
			}
			for _, role := range f.roles {
				if role.Visibility == "private" {
					return fmt.Errorf("role %s remains", role.Name)
				}
			}
			return nil
		},
		Steps: []resource.TestStep{
			// 作成: ロール → ロールを付けたサブユーザー → サブユーザーのクレデンシャル
			{
				Config: f.config(step1),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("data.conohavps_permissions.all", tfjsonpath.New("names"), knownvalue.ListSizeExact(len(identityFakePermissions))),
					statecheck.ExpectKnownValue("data.conohavps_permissions.all", tfjsonpath.New("permissions").AtSliceIndex(0).AtMapKey("name"), knownvalue.StringExact("post-token")),
					statecheck.ExpectKnownValue("conohavps_role.agent", tfjsonpath.New("visibility"), knownvalue.StringExact("private")),
					statecheck.ExpectKnownValue("conohavps_role.agent", tfjsonpath.New("permissions"), knownvalue.SetExact([]knownvalue.Check{
						knownvalue.StringExact("get-server-list"), knownvalue.StringExact("get-server"),
					})),
					statecheck.ExpectKnownValue("conohavps_subuser.agent", tfjsonpath.New("name"), knownvalue.StringRegexp(regexp.MustCompile(`^gncu`))),
					statecheck.ExpectKnownValue("conohavps_credential.agent", tfjsonpath.New("tenant_id"), knownvalue.StringExact(fakeapi.TenantID)),
					statecheck.ExpectKnownValue("conohavps_credential.agent", tfjsonpath.New("secret"), knownvalue.StringRegexp(regexp.MustCompile(`^secret-of-access-`))),
					identitySensitiveCheck{"conohavps_subuser.agent", "password"},
					identitySensitiveCheck{"conohavps_credential.agent", "secret"},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("conohavps_credential.agent", "user_id", "conohavps_subuser.agent", "id"),
					resource.TestCheckResourceAttrPair("conohavps_credential.agent", "id", "conohavps_credential.agent", "access"),
					resource.TestCheckTypeSetElemAttrPair("conohavps_subuser.agent", "roles.*", "conohavps_role.agent", "id"),
					resource.TestCheckTypeSetElemAttr("conohavps_subuser.agent", "roles.*", "gmo-identity"),
					identityExpectBody(f, "POST", "/sub-users/roles", []string{"role", "name"}, "agent-readonly"),
					identityExpectBody(f, "POST", "/sub-users/roles", []string{"role", "permissions"}, []string{"get-server", "get-server-list"}),
					identityExpectBody(f, "POST", "/sub-users", []string{"user", "password"}, "Agent-pass-1"),
					identityExpectBody(f, "POST", "/users/subuser-[0-9]+/credentials/OS-EC2", []string{"tenant_id"}, fakeapi.TenantID),
					func(s *terraform.State) error {
						roleID = s.RootModule().Resources["conohavps_role.agent"].Primary.ID
						userID = s.RootModule().Resources["conohavps_subuser.agent"].Primary.ID
						access = s.RootModule().Resources["conohavps_credential.agent"].Primary.ID
						// サブユーザー作成はロール ID とロール名をそのまま送る
						return identityExpectBody(f, "POST", "/sub-users", []string{"user", "roles"}, identitySorted(roleID, "gmo-identity"))(s)
					},
				),
			},
			// その場での更新: ロール名とパーミッション、パスワードとロール. クレデンシャルは変わらない
			{
				Config: f.config(step2),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("conohavps_role.agent", plancheck.ResourceActionUpdate),
					plancheck.ExpectResourceAction("conohavps_subuser.agent", plancheck.ResourceActionUpdate),
					plancheck.ExpectResourceAction("conohavps_credential.agent", plancheck.ResourceActionNoop),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("conohavps_role.agent", "name", "agent-ops"),
					resource.TestCheckTypeSetElemAttr("conohavps_subuser.agent", "roles.*", "gmo-compute"),
					identityExpectBody(f, "PUT", "/sub-users/roles/role-[0-9]+", []string{"role", "name"}, "agent-ops"),
					identityExpectBody(f, "POST", "/sub-users/roles/role-[0-9]+/assign", []string{"permissions"}, []string{"post-server-action-reboot"}),
					identityExpectBody(f, "POST", "/sub-users/roles/role-[0-9]+/unassign", []string{"permissions"}, []string{"get-server"}),
					identityExpectBody(f, "PUT", "/sub-users/subuser-[0-9]+", []string{"user", "password"}, "Agent-pass-2"),
					// 紐づけと解除はロール名ではなくロール ID で送る
					identityExpectBody(f, "POST", "/sub-users/subuser-[0-9]+/assign", []string{"roles"}, []string{"std-gmo-compute"}),
					identityExpectBody(f, "POST", "/sub-users/subuser-[0-9]+/unassign", []string{"roles"}, []string{"std-gmo-identity"}),
					func(s *terraform.State) error {
						// 0 にならないよう、紐づけてから解除する
						if f.callIndex("POST", "/sub-users/roles/role-[0-9]+/assign") > f.callIndex("POST", "/sub-users/roles/role-[0-9]+/unassign") {
							return fmt.Errorf("permissions were unassigned before being assigned")
						}
						if f.callIndex("POST", "/sub-users/subuser-[0-9]+/assign") > f.callIndex("POST", "/sub-users/subuser-[0-9]+/unassign") {
							return fmt.Errorf("roles were unassigned before being assigned")
						}
						if got := s.RootModule().Resources["conohavps_role.agent"].Primary.ID; got != roleID {
							return fmt.Errorf("role was recreated: %s -> %s", roleID, got)
						}
						if got := s.RootModule().Resources["conohavps_subuser.agent"].Primary.ID; got != userID {
							return fmt.Errorf("sub-user was recreated: %s -> %s", userID, got)
						}
						return nil
					},
				),
			},
			// 作り直し: クレデンシャルは更新できない
			{
				Config: f.config(step3),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("conohavps_credential.agent", plancheck.ResourceActionReplace),
					plancheck.ExpectResourceAction("conohavps_subuser.agent", plancheck.ResourceActionNoop),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("conohavps_credential.agent", "tenant_id", "tenant-2"),
					identityExpectBody(f, "POST", "/users/subuser-[0-9]+/credentials/OS-EC2", []string{"tenant_id"}, "tenant-2"),
					func(s *terraform.State) error {
						got := s.RootModule().Resources["conohavps_credential.agent"].Primary.ID
						f.mu.Lock()
						defer f.mu.Unlock()
						if got == access {
							return fmt.Errorf("credential was not recreated")
						}
						if _, ok := f.creds[access]; ok || len(f.creds) != 1 {
							return fmt.Errorf("old credential %s was not deleted (%d remain)", access, len(f.creds))
						}
						return nil
					},
				),
			},
			// インポート
			{
				ResourceName:      "conohavps_role.agent",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				ResourceName:      "conohavps_subuser.agent",
				ImportState:       true,
				ImportStateVerify: true,
				// パスワードは API から読み戻せない. ロールは設定では名前（gmo-compute）で書いているが、
				// インポートでは ID で読まれる
				ImportStateVerifyIgnore: []string{"password", "roles"},
			},
			{
				ResourceName:      "conohavps_credential.agent",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					r := s.RootModule().Resources["conohavps_credential.agent"].Primary
					return r.Attributes["user_id"] + "/" + r.ID, nil
				},
			},
			// Terraform の外でパーミッションとロールを足すと差分になり、次の適用で外される
			{
				PreConfig: func() {
					f.mu.Lock()
					defer f.mu.Unlock()
					role := f.roles[roleID]
					role.Permissions = append(role.Permissions, "delete-server")
					u := f.users[userID]
					u.RoleIDs = append(u.RoleIDs, "std-gmo-network")
				},
				Config:             f.config(step3),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config: f.config(step3),
				Check: resource.ComposeAggregateTestCheckFunc(
					identityExpectBody(f, "POST", "/sub-users/roles/role-[0-9]+/unassign", []string{"permissions"}, []string{"delete-server"}),
					identityExpectBody(f, "POST", "/sub-users/subuser-[0-9]+/unassign", []string{"roles"}, []string{"std-gmo-network"}),
					func(*terraform.State) error {
						f.mu.Lock()
						defer f.mu.Unlock()
						got := slices.Sorted(slices.Values(f.roles[roleID].Permissions))
						if want := []string{"get-server-list", "post-server-action-reboot"}; !reflect.DeepEqual(got, want) {
							return fmt.Errorf("role permissions = %v, want %v", got, want)
						}
						return nil
					},
				),
			},
		},
	})
}

// Terraform の外で消されたリソースは State から外れ、作り直しの計画になる.
func TestIdentityRemovedOutside(t *testing.T) {
	f := newIdentityFake(t)
	cfg := f.config(identityConfig{
		roleName:    "agent",
		permissions: `["post-token"]`,
		password:    "Agent-pass-1",
		roles:       `[conohavps_role.agent.id]`,
	})
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				PreConfig: func() {
					f.mu.Lock()
					defer f.mu.Unlock()
					clear(f.creds)
					clear(f.users)
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("conohavps_subuser.agent", plancheck.ResourceActionCreate),
					plancheck.ExpectResourceAction("conohavps_credential.agent", plancheck.ResourceActionCreate),
					plancheck.ExpectResourceAction("conohavps_role.agent", plancheck.ResourceActionNoop),
				}},
			},
		},
	})
}

// ドキュメントの制約（ロール名・パスワード・件数）は計画の時点で弾く.
func TestIdentityValidation(t *testing.T) {
	f := newIdentityFake(t)
	cases := []struct {
		config identityConfig
		err    string
	}{
		{identityConfig{roleName: "has space", permissions: `["post-token"]`, password: "Agent-pass-1", roles: `["gmo-identity"]`}, `role name must contain only`},
		{identityConfig{roleName: strings.Repeat("a", 33), permissions: `["post-token"]`, password: "Agent-pass-1", roles: `["gmo-identity"]`}, `length must be between 1 and 32`},
		{identityConfig{roleName: "agent", permissions: `[]`, password: "Agent-pass-1", roles: `["gmo-identity"]`}, `set must contain at least 1`},
		{identityConfig{roleName: "agent", permissions: `["post-token"]`, password: "Short-1", roles: `["gmo-identity"]`}, `must be 9-70 characters long`},
		{identityConfig{roleName: "agent", permissions: `["post-token"]`, password: "alllower-1", roles: `["gmo-identity"]`}, `at least one uppercase`},
		{identityConfig{roleName: "agent", permissions: `["post-token"]`, password: "ALLUPPER-1", roles: `["gmo-identity"]`}, `at least one lowercase`},
		{identityConfig{roleName: "agent", permissions: `["post-token"]`, password: "NoDigitsOrSymbols", roles: `["gmo-identity"]`}, `at least one digit or symbol`},
		{identityConfig{roleName: "agent", permissions: `["post-token"]`, password: "Agent-pass-1<", roles: `["gmo-identity"]`}, `must contain only alphanumeric characters and the\s+symbols`},
		{identityConfig{roleName: "agent", permissions: `["post-token"]`, password: "Agent-pass-1", roles: `[]`}, `set must contain at least 1`},
	}
	var steps []resource.TestStep
	for _, c := range cases {
		steps = append(steps, resource.TestStep{
			Config:      f.config(c.config),
			PlanOnly:    true,
			ExpectError: regexp.MustCompile(c.err),
		})
	}
	resource.UnitTest(t, resource.TestCase{ProtoV6ProviderFactories: fakeapi.Factories, Steps: steps})
}

// サブユーザーだけの設定（パスワードの検証を確かめる）.
func (f *identityFake) subUserConfig(password, roles string) string {
	return f.ProviderConfig() + fmt.Sprintf(`
resource "conohavps_role" "agent" {
  name        = "agent"
  permissions = ["get-server-list"]
}

resource "conohavps_subuser" "agent" {
  password = %q
  roles    = %s
}
`, password, roles)
}

// gmo-identity を持つサブユーザーは、読み込みのたびに State のパスワードでトークンを発行して確かめる.
// 有効なら差分は出ず、Terraform の外で変えられていれば差分になって、適用で設定の値に戻る.
func TestIdentitySubUserPasswordDrift(t *testing.T) {
	f := newIdentityFake(t)
	cfg := f.subUserConfig("Agent-pass-1", `[conohavps_role.agent.id, "gmo-identity"]`)
	var userID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: func(s *terraform.State) error {
					userID = s.RootModule().Resources["conohavps_subuser.agent"].Primary.ID
					return nil
				},
			},
			// パスワードが今も有効: 検証のトークン発行は行われ、差分は出ない
			{
				PreConfig: func() {
					f.mu.Lock()
					defer f.mu.Unlock()
					f.tokenCalls = nil
				},
				Config:   cfg,
				PlanOnly: true,
			},
			{
				Config:   cfg,
				PlanOnly: true,
				// 直前の計画での検証リクエストを確かめる. プロバイダのトークンを付けず、テナントで絞る
				PreConfig: func() {
					calls := f.tokens()
					if len(calls) == 0 {
						t.Fatal("the sub-user password was not verified")
					}
					want := identityTokenCall{UserID: userID, ProjectID: fakeapi.TenantID, Methods: []string{"password"}, Query: "nocatalog"}
					for _, c := range calls {
						if !reflect.DeepEqual(c, want) {
							t.Fatalf("token request = %+v, want %+v", c, want)
						}
					}
				},
			},
			// Terraform の外でパスワードが変えられた: 差分になり、適用で設定の値に戻る
			{
				PreConfig: func() {
					f.mu.Lock()
					defer f.mu.Unlock()
					f.users[userID].Password = "Changed-outside-1"
				},
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("conohavps_subuser.agent", plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					identityExpectBody(f, "PUT", "/sub-users/subuser-[0-9]+", []string{"user", "password"}, "Agent-pass-1"),
					func(*terraform.State) error {
						f.mu.Lock()
						defer f.mu.Unlock()
						if got := f.users[userID].Password; got != "Agent-pass-1" {
							return fmt.Errorf("sub-user password was not restored")
						}
						return nil
					},
				),
			},
			// トークン発行がサーバーエラー: 確かめられないので State を残し、差分もエラーも出さない
			{
				PreConfig: func() {
					f.mu.Lock()
					defer f.mu.Unlock()
					f.tokenCalls = nil
					f.tokenStatus = http.StatusServiceUnavailable
				},
				Config:   cfg,
				PlanOnly: true,
			},
			{
				PreConfig: func() {
					if len(f.tokens()) == 0 {
						t.Fatal("the sub-user password was not verified while the token endpoint failed")
					}
					f.mu.Lock()
					defer f.mu.Unlock()
					f.tokenStatus = 0
				},
				Config:   cfg,
				PlanOnly: true,
			},
		},
	})
}

// gmo-identity を持たないサブユーザーはトークンを発行できないため、パスワードを確かめない
// （Terraform の外で変えられても差分にならない）.
func TestIdentitySubUserPasswordNotVerifiedWithoutTokenRole(t *testing.T) {
	f := newIdentityFake(t)
	cfg := f.subUserConfig("Agent-pass-1", `[conohavps_role.agent.id]`)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				PreConfig: func() {
					f.mu.Lock()
					defer f.mu.Unlock()
					for _, u := range f.users {
						u.Password = "Changed-outside-1"
					}
				},
				Config:   cfg,
				PlanOnly: true,
			},
			{
				PreConfig: func() {
					if calls := f.tokens(); len(calls) != 0 {
						t.Fatalf("%d verification requests were made for a sub-user without gmo-identity", len(calls))
					}
				},
				Config:   cfg,
				PlanOnly: true,
			},
		},
	})
}

func identitySorted(s ...string) []string {
	return slices.Sorted(slices.Values(s))
}
