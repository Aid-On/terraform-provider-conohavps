// Identity API（サブユーザー・ロール・パーミッション・クレデンシャル）の呼び出しを提供する.
// ConoHa 独自の API（/v3/sub-users など）のため、gophercloud の OpenStack 実装を通さず、
// サービスクライアントで直接リクエストする.

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// 成功コードは OpenAPI 定義に従う. サブユーザーとロールの作成・更新・紐づけは 200、
// クレデンシャルの作成は 201、削除はすべて 204.
var (
	identityOK            = []int{200}
	identityOKCredCreate  = []int{201}
	identityOKDelete      = []int{204}
	identityTokenIssued   = http.StatusCreated
	identityTokenRejected = http.StatusUnauthorized
)

// IdentityTokenRole はトークンの発行を許す標準のロールの名前.
const IdentityTokenRole = "gmo-identity"

// SubUserRole はサブユーザーに紐づくロール.
type SubUserRole struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// SubUser はサブユーザー.
type SubUser struct {
	ID    string        `json:"id"`
	Name  string        `json:"name"`
	Roles []SubUserRole `json:"roles"`
}

// Role はサブユーザーに付与するロール.
type Role struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Visibility  string   `json:"visibility"`
	Permissions []string `json:"permissions"`
}

// Permission はロールに紐づけられるパーミッション.
type Permission struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Credential は EC2 形式（S3 互換のアクセスキー）のクレデンシャル.
type Credential struct {
	UserID   string `json:"user_id"`
	TenantID string `json:"tenant_id"`
	Access   string `json:"access"`
	Secret   string `json:"secret"`
}

type subUserBody struct {
	User SubUser `json:"user"`
}

type roleBody struct {
	Role Role `json:"role"`
}

type credentialBody struct {
	Credential Credential `json:"credential"`
}

// Identity API のクライアントを返す. カタログに無ければエラーを返す.
func (c *ConohaClient) requireIdentity() (*gophercloud.ServiceClient, error) {
	if c.IdentityClient == nil {
		return nil, ServiceUnavailable("identity")
	}
	return c.IdentityClient, nil
}

// CreateSubUser はサブユーザーを作成する. roles はロール ID またはロール名.
func (c *ConohaClient) CreateSubUser(ctx context.Context, password string, roles []string) (*SubUser, error) {
	client, err := c.requireIdentity()
	if err != nil {
		return nil, err
	}
	tflog.Debug(ctx, "Creating sub-user.", map[string]any{"roles": roles})

	body := map[string]any{"user": map[string]any{"password": password, "roles": roles}}
	var out subUserBody
	if _, err := client.Post(ctx, client.ServiceURL("sub-users"), body, &out, &gophercloud.RequestOpts{OkCodes: identityOK}); err != nil {
		return nil, fmt.Errorf("failed to create sub-user: %w", err)
	}
	return &out.User, nil
}

// GetSubUser はサブユーザーの詳細を取得する.
func (c *ConohaClient) GetSubUser(ctx context.Context, id string) (*SubUser, error) {
	client, err := c.requireIdentity()
	if err != nil {
		return nil, err
	}
	var out subUserBody
	if _, err := client.Get(ctx, client.ServiceURL("sub-users", id), &out, &gophercloud.RequestOpts{OkCodes: identityOK}); err != nil {
		return nil, err
	}
	return &out.User, nil
}

// SubUserPasswordValid は、サブユーザーとしてトークンを発行し、パスワードが今も有効かを確かめる.
// 発行できれば（201）true、認証に失敗すれば（401）false を返す. それ以外は確かめられなかったとしてエラーを返す.
// gophercloud のクライアントは 401 でプロバイダ自身を再認証するため通さず、
// プロバイダのトークンを付けない素の HTTP リクエストで送る. パスワードはログにもエラーにも含めない.
func (c *ConohaClient) SubUserPasswordValid(ctx context.Context, userID, password string) (bool, error) {
	client, err := c.requireIdentity()
	if err != nil {
		return false, err
	}
	body, err := json.Marshal(map[string]any{"auth": map[string]any{
		"identity": map[string]any{
			"methods":  []string{"password"},
			"password": map[string]any{"user": map[string]any{"id": userID, "password": password}},
		},
		"scope": map[string]any{"project": map[string]any{"id": c.TenantID}},
	}})
	if err != nil {
		return false, fmt.Errorf("failed to encode the token request: %w", err)
	}
	// 本文のサービスカタログは使わないため、nocatalog で省かせる
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, client.ServiceURL("auth", "tokens")+"?nocatalog", bytes.NewReader(body))
	if err != nil {
		return false, fmt.Errorf("failed to build the token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if ua := c.ProviderClient.UserAgent.Join(); ua != "" {
		req.Header.Set("User-Agent", ua)
	}

	tflog.Debug(ctx, "Issuing a token as the sub-user to verify its password.", map[string]any{"id": userID})
	resp, err := c.ProviderClient.HTTPClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to issue a token as the sub-user: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))

	switch resp.StatusCode {
	case identityTokenIssued:
		return true, nil
	case identityTokenRejected:
		return false, nil
	default:
		return false, fmt.Errorf("issuing a token as the sub-user returned HTTP %d", resp.StatusCode)
	}
}

// UpdateSubUserPassword はサブユーザーのパスワードを変更する.
func (c *ConohaClient) UpdateSubUserPassword(ctx context.Context, id, password string) error {
	client, err := c.requireIdentity()
	if err != nil {
		return err
	}
	tflog.Debug(ctx, "Updating sub-user password.", map[string]any{"id": id})

	body := map[string]any{"user": map[string]any{"password": password}}
	if _, err := client.Put(ctx, client.ServiceURL("sub-users", id), body, nil, &gophercloud.RequestOpts{OkCodes: identityOK}); err != nil {
		return fmt.Errorf("failed to update sub-user password: %w", err)
	}
	return nil
}

// AssignSubUserRoles はサブユーザーにロールを追加で紐づける. roleIDs はロール ID.
func (c *ConohaClient) AssignSubUserRoles(ctx context.Context, id string, roleIDs []string) error {
	return c.changeSubUserRoles(ctx, id, "assign", roleIDs)
}

// UnassignSubUserRoles はサブユーザーからロールの紐づけを解除する. roleIDs はロール ID.
func (c *ConohaClient) UnassignSubUserRoles(ctx context.Context, id string, roleIDs []string) error {
	return c.changeSubUserRoles(ctx, id, "unassign", roleIDs)
}

func (c *ConohaClient) changeSubUserRoles(ctx context.Context, id, action string, roleIDs []string) error {
	client, err := c.requireIdentity()
	if err != nil {
		return err
	}
	tflog.Debug(ctx, "Changing sub-user roles.", map[string]any{"id": id, "action": action, "roles": roleIDs})

	body := map[string]any{"roles": roleIDs}
	if _, err := client.Post(ctx, client.ServiceURL("sub-users", id, action), body, nil, &gophercloud.RequestOpts{OkCodes: identityOK}); err != nil {
		return fmt.Errorf("failed to %s sub-user roles: %w", action, err)
	}
	return nil
}

// DeleteSubUser はサブユーザーを削除する.
func (c *ConohaClient) DeleteSubUser(ctx context.Context, id string) error {
	client, err := c.requireIdentity()
	if err != nil {
		return err
	}
	_, err = client.Delete(ctx, client.ServiceURL("sub-users", id), &gophercloud.RequestOpts{OkCodes: identityOKDelete})
	return err
}

// CreateRole はロールを作成する.
func (c *ConohaClient) CreateRole(ctx context.Context, name string, permissions []string) (*Role, error) {
	client, err := c.requireIdentity()
	if err != nil {
		return nil, err
	}
	tflog.Debug(ctx, "Creating role.", map[string]any{"name": name, "permissions": permissions})

	body := map[string]any{"role": map[string]any{"name": name, "permissions": permissions}}
	var out roleBody
	if _, err := client.Post(ctx, client.ServiceURL("sub-users", "roles"), body, &out, &gophercloud.RequestOpts{OkCodes: identityOK}); err != nil {
		return nil, fmt.Errorf("failed to create role: %w", err)
	}
	return &out.Role, nil
}

// GetRole はロールの詳細を取得する.
func (c *ConohaClient) GetRole(ctx context.Context, id string) (*Role, error) {
	client, err := c.requireIdentity()
	if err != nil {
		return nil, err
	}
	var out roleBody
	if _, err := client.Get(ctx, client.ServiceURL("sub-users", "roles", id), &out, &gophercloud.RequestOpts{OkCodes: identityOK}); err != nil {
		return nil, err
	}
	return &out.Role, nil
}

// ListRoles はロールの一覧（標準のロールを含む）を取得する. 一覧にはパーミッションは含まれない.
func (c *ConohaClient) ListRoles(ctx context.Context) ([]Role, error) {
	client, err := c.requireIdentity()
	if err != nil {
		return nil, err
	}
	var out struct {
		Roles []Role `json:"roles"`
	}
	if _, err := client.Get(ctx, client.ServiceURL("sub-users", "roles"), &out, &gophercloud.RequestOpts{OkCodes: identityOK}); err != nil {
		return nil, fmt.Errorf("failed to list roles: %w", err)
	}
	return out.Roles, nil
}

// UpdateRoleName はロール名を変更する.
func (c *ConohaClient) UpdateRoleName(ctx context.Context, id, name string) error {
	client, err := c.requireIdentity()
	if err != nil {
		return err
	}
	tflog.Debug(ctx, "Updating role name.", map[string]any{"id": id, "name": name})

	body := map[string]any{"role": map[string]any{"name": name}}
	if _, err := client.Put(ctx, client.ServiceURL("sub-users", "roles", id), body, nil, &gophercloud.RequestOpts{OkCodes: identityOK}); err != nil {
		return fmt.Errorf("failed to update role name: %w", err)
	}
	return nil
}

// AssignRolePermissions はロールにパーミッションを追加で紐づける.
func (c *ConohaClient) AssignRolePermissions(ctx context.Context, id string, permissions []string) error {
	return c.changeRolePermissions(ctx, id, "assign", permissions)
}

// UnassignRolePermissions はロールからパーミッションの紐づけを解除する.
func (c *ConohaClient) UnassignRolePermissions(ctx context.Context, id string, permissions []string) error {
	return c.changeRolePermissions(ctx, id, "unassign", permissions)
}

func (c *ConohaClient) changeRolePermissions(ctx context.Context, id, action string, permissions []string) error {
	client, err := c.requireIdentity()
	if err != nil {
		return err
	}
	tflog.Debug(ctx, "Changing role permissions.", map[string]any{"id": id, "action": action, "permissions": permissions})

	body := map[string]any{"permissions": permissions}
	if _, err := client.Post(ctx, client.ServiceURL("sub-users", "roles", id, action), body, nil, &gophercloud.RequestOpts{OkCodes: identityOK}); err != nil {
		return fmt.Errorf("failed to %s role permissions: %w", action, err)
	}
	return nil
}

// DeleteRole はロールを削除する. サブユーザーに紐づく唯一のロールは削除できない.
func (c *ConohaClient) DeleteRole(ctx context.Context, id string) error {
	client, err := c.requireIdentity()
	if err != nil {
		return err
	}
	_, err = client.Delete(ctx, client.ServiceURL("sub-users", "roles", id), &gophercloud.RequestOpts{OkCodes: identityOKDelete})
	return err
}

// ListPermissions はロールに紐づけられるパーミッションの一覧を取得する.
func (c *ConohaClient) ListPermissions(ctx context.Context) ([]Permission, error) {
	client, err := c.requireIdentity()
	if err != nil {
		return nil, err
	}
	var out struct {
		Permissions []Permission `json:"permissions"`
	}
	if _, err := client.Get(ctx, client.ServiceURL("permissions"), &out, &gophercloud.RequestOpts{OkCodes: identityOK}); err != nil {
		return nil, fmt.Errorf("failed to list permissions: %w", err)
	}
	return out.Permissions, nil
}

// CreateCredential はユーザーのクレデンシャル（アクセスキーとシークレットキー）を作成する.
func (c *ConohaClient) CreateCredential(ctx context.Context, userID, tenantID string) (*Credential, error) {
	client, err := c.requireIdentity()
	if err != nil {
		return nil, err
	}
	tflog.Debug(ctx, "Creating credential.", map[string]any{"user_id": userID, "tenant_id": tenantID})

	body := map[string]any{"tenant_id": tenantID}
	var out credentialBody
	if _, err := client.Post(ctx, client.ServiceURL("users", userID, "credentials", "OS-EC2"), body, &out, &gophercloud.RequestOpts{OkCodes: identityOKCredCreate}); err != nil {
		return nil, fmt.Errorf("failed to create credential: %w", err)
	}
	return &out.Credential, nil
}

// GetCredential はクレデンシャルの詳細を取得する.
func (c *ConohaClient) GetCredential(ctx context.Context, userID, access string) (*Credential, error) {
	client, err := c.requireIdentity()
	if err != nil {
		return nil, err
	}
	var out credentialBody
	if _, err := client.Get(ctx, client.ServiceURL("users", userID, "credentials", "OS-EC2", access), &out, &gophercloud.RequestOpts{OkCodes: identityOK}); err != nil {
		return nil, err
	}
	return &out.Credential, nil
}

// DeleteCredential はクレデンシャルを削除する.
func (c *ConohaClient) DeleteCredential(ctx context.Context, userID, access string) error {
	client, err := c.requireIdentity()
	if err != nil {
		return err
	}
	_, err = client.Delete(ctx, client.ServiceURL("users", userID, "credentials", "OS-EC2", access), &gophercloud.RequestOpts{OkCodes: identityOKDelete})
	return err
}
