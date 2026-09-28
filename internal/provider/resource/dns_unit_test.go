// DNS のドメインとレコードのリソースのテストを提供する.
// 偽の ConoHa API に OpenAPI 仕様どおりの DNS API（/v1/domains...）を足し、
// 作成・更新・作り直し・インポート・削除を、実際の API を使わずに検証する.
// HTML ドキュメントの形（uuid・domain_uuid・伏せ字のメールアドレス）で返す切り替えも持つ.

package resource_test

import (
	"fmt"
	"net/http"
	"net/netip"
	"regexp"
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

// 偽の DNS API. ドメインとレコードをメモリに持ち、受け取ったリクエスト本文を記録する.
type fakeDNS struct {
	mu      sync.Mutex
	s       *fakeapi.Server
	domains map[string]map[string]any            // ドメイン ID → ドメイン
	records map[string]map[string]map[string]any // ドメイン ID → レコード ID → レコード
	bodies  []fakeDNSRequest
	legacy  bool // HTML ドキュメントのレスポンス例の形で返す
}

type fakeDNSRequest struct {
	Method, Path string
	Body         map[string]any
}

func newFakeDNS(t *testing.T) *fakeDNS {
	f := &fakeDNS{s: fakeapi.New(t), domains: map[string]map[string]any{}, records: map[string]map[string]map[string]any{}}
	m := f.s.Mux
	m.HandleFunc("POST /dns-service/v1/domains", f.createDomain)
	m.HandleFunc("GET /dns-service/v1/domains/{domain}", f.getDomain)
	m.HandleFunc("PUT /dns-service/v1/domains/{domain}", f.updateDomain)
	m.HandleFunc("DELETE /dns-service/v1/domains/{domain}", f.deleteDomain)
	m.HandleFunc("POST /dns-service/v1/domains/{domain}/records", f.createRecord)
	m.HandleFunc("GET /dns-service/v1/domains/{domain}/records/{record}", f.getRecord)
	m.HandleFunc("PUT /dns-service/v1/domains/{domain}/records/{record}", f.updateRecord)
	m.HandleFunc("DELETE /dns-service/v1/domains/{domain}/records/{record}", f.deleteRecord)
	return f
}

// リクエスト本文を読み、記録する. 読めなければ 400 を返して nil を返す.
func (f *fakeDNS) body(w http.ResponseWriter, r *http.Request) map[string]any {
	var b map[string]any
	if err := fakeapi.ReadJSON(r, &b); err != nil {
		fakeapi.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return nil
	}
	f.bodies = append(f.bodies, fakeDNSRequest{r.Method, r.URL.Path, b})
	return b
}

// レスポンスを返す. legacy なら HTML ドキュメントのレスポンス例の形
// （ID は uuid・domain_uuid、メールアドレスは伏せ字、説明は無し）に直して返す.
func (f *fakeDNS) write(w http.ResponseWriter, v map[string]any) {
	if !f.legacy {
		fakeapi.WriteJSON(w, http.StatusOK, v)
		return
	}
	out := map[string]any{}
	for k, x := range v {
		switch k {
		case "id":
			out["uuid"] = x
		case "domain_id":
			out["domain_uuid"] = x
		case "email":
			out["email"] = "******@****.***"
		default:
			out[k] = x
		}
	}
	fakeapi.WriteJSON(w, http.StatusOK, out)
}

func badRequest(w http.ResponseWriter, msg string) {
	fakeapi.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": msg})
}

// 名前は末尾のピリオドが必須で、実物に倣い小文字に揃えて保存する.
func fakeName(v any) (string, bool) {
	s, ok := v.(string)
	if !ok || !strings.HasSuffix(s, ".") {
		return "", false
	}
	return strings.ToLower(s), true
}

func (f *fakeDNS) createDomain(w http.ResponseWriter, r *http.Request) {
	if !fakeapi.Authorized(w, r) {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	b := f.body(w, r)
	if b == nil {
		return
	}
	name, ok := fakeName(b["name"])
	ttl, okTTL := b["ttl"].(float64)
	email, okEmail := b["email"].(string)
	if !ok || !okTTL || !okEmail {
		badRequest(w, "name, ttl and email are required")
		return
	}
	id := f.s.NewID("domain")
	// OpenAPI 仕様の DomainBody の形. project_id は HTML ドキュメントのレスポンス例にだけある
	d := map[string]any{
		"id": id, "name": name, "project_id": fakeapi.TenantID, "serial": 1701912034,
		"ttl": ttl, "email": email, "created_at": "2023-12-07T01:20:34.840919Z", "updated_at": "2023-12-07T01:20:34.840950Z",
	}
	f.domains[id] = d
	// 実物と同じく SOA と NS のレコードを自動で作る
	f.records[id] = map[string]map[string]any{}
	for _, rec := range []map[string]any{
		{"type": "SOA", "data": "a.conoha-dns.com. postmaster.example.org. 1701909248 3600 600 86400 3600"},
		{"type": "NS", "data": "a.conoha-dns.com."},
	} {
		rid := f.s.NewID("record")
		rec["id"], rec["domain_id"], rec["name"], rec["ttl"], rec["priority"] = rid, id, name, 3600, nil
		f.records[id][rid] = rec
	}
	f.write(w, d)
}

func (f *fakeDNS) getDomain(w http.ResponseWriter, r *http.Request) {
	if !fakeapi.Authorized(w, r) {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.domains[r.PathValue("domain")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	f.write(w, d)
}

func (f *fakeDNS) updateDomain(w http.ResponseWriter, r *http.Request) {
	if !fakeapi.Authorized(w, r) {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.domains[r.PathValue("domain")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	b := f.body(w, r)
	if b == nil {
		return
	}
	if _, bad := b["name"]; bad {
		badRequest(w, "name cannot be updated")
		return
	}
	for _, k := range []string{"ttl", "email"} {
		if v, ok := b[k]; ok {
			d[k] = v
		}
	}
	d["updated_at"] = "2023-12-07T04:19:36.090636Z"
	f.write(w, d)
}

func (f *fakeDNS) deleteDomain(w http.ResponseWriter, r *http.Request) {
	if !fakeapi.Authorized(w, r) {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	id := r.PathValue("domain")
	if _, ok := f.domains[id]; !ok {
		http.NotFound(w, r)
		return
	}
	delete(f.domains, id)
	delete(f.records, id)
	w.WriteHeader(http.StatusNoContent)
}

// レコードの値を検証し、実物が揃えそうな表記（ホスト名の小文字、IPv6 の省略形、TXT の引用符）に直す.
func fakeRecordFields(rec, b map[string]any) string {
	if v, ok := b["name"]; ok {
		name, ok := fakeName(v)
		if !ok {
			return "name must end with a period"
		}
		rec["name"] = name
	}
	if v, ok := b["type"].(string); ok {
		rec["type"] = v
	}
	// priority・ttl は OpenAPI 仕様どおり整数. weight・port は HTML ドキュメントにだけある. null は値を消す
	for _, k := range []string{"priority", "weight", "port", "ttl"} {
		if v, ok := b[k]; ok {
			if _, num := v.(float64); !num && (v != nil || k == "ttl") {
				return k + " must be a number"
			}
			rec[k] = v
		}
	}
	if v, ok := b["data"].(string); ok {
		switch rec["type"] {
		case "AAAA":
			a, err := netip.ParseAddr(v)
			if err != nil {
				return "bad AAAA data"
			}
			v = a.String()
		case "CNAME", "MX", "NS", "SRV":
			v = strings.ToLower(v)
		case "TXT":
			if !strings.HasPrefix(v, `"`) {
				v = `"` + v + `"`
			}
		}
		rec["data"] = v
	}
	switch rec["type"] {
	case "A", "AAAA", "CNAME", "MX", "NS", "SRV", "TXT":
	default:
		return fmt.Sprintf("type %v is not allowed", rec["type"])
	}
	if rec["name"] == nil || rec["data"] == nil {
		return "name, type and data are required"
	}
	return ""
}

func (f *fakeDNS) createRecord(w http.ResponseWriter, r *http.Request) {
	if !fakeapi.Authorized(w, r) {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	did := r.PathValue("domain")
	if _, ok := f.domains[did]; !ok {
		http.NotFound(w, r)
		return
	}
	b := f.body(w, r)
	if b == nil {
		return
	}
	id := f.s.NewID("record")
	// OpenAPI 仕様の RecordBody の形. weight・port は HTML ドキュメントのレスポンス例にだけある
	rec := map[string]any{
		"id": id, "domain_id": did, "priority": nil, "weight": nil, "port": nil, "ttl": 3600,
		"created_at": "2023-12-07T07:58:39.098406Z", "updated_at": "2023-12-07T07:58:39.098447Z",
	}
	if msg := fakeRecordFields(rec, b); msg != "" {
		badRequest(w, msg)
		return
	}
	f.records[did][id] = rec
	f.write(w, rec)
}

func (f *fakeDNS) record(w http.ResponseWriter, r *http.Request) map[string]any {
	rec, ok := f.records[r.PathValue("domain")][r.PathValue("record")]
	if !ok {
		http.NotFound(w, r)
		return nil
	}
	return rec
}

func (f *fakeDNS) getRecord(w http.ResponseWriter, r *http.Request) {
	if !fakeapi.Authorized(w, r) {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if rec := f.record(w, r); rec != nil {
		f.write(w, rec)
	}
}

func (f *fakeDNS) updateRecord(w http.ResponseWriter, r *http.Request) {
	if !fakeapi.Authorized(w, r) {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	rec := f.record(w, r)
	if rec == nil {
		return
	}
	b := f.body(w, r)
	if b == nil {
		return
	}
	updated := map[string]any{}
	for k, v := range rec {
		updated[k] = v
	}
	if msg := fakeRecordFields(updated, b); msg != "" {
		badRequest(w, msg)
		return
	}
	updated["updated_at"] = "2023-12-07T08:57:54.008234Z"
	f.records[r.PathValue("domain")][r.PathValue("record")] = updated
	f.write(w, updated)
}

func (f *fakeDNS) deleteRecord(w http.ResponseWriter, r *http.Request) {
	if !fakeapi.Authorized(w, r) {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if rec := f.record(w, r); rec != nil {
		delete(f.records[r.PathValue("domain")], r.PathValue("record"))
		w.WriteHeader(http.StatusNoContent)
	}
}

// path が正規表現 pattern の全体に一致するか.
func pathMatches(path, pattern string) bool {
	return regexp.MustCompile("^" + pattern + "$").MatchString(path)
}

// リクエスト先の正規表現.
const (
	dnsDomainsPath = "/dns-service/v1/domains"
	dnsDomainPath  = dnsDomainsPath + "/[^/]+"
	dnsRecordsPath = dnsDomainPath + "/records(/[^/]+)?"
)

// 本文が want のキーと値をすべて持ち、forbidden のキーを持たないことを確かめる.
// want の値が nil なら、そのキーを null で送っていることを確かめる. want に type があれば、そのタイプの本文だけを見る.
func (f *fakeDNS) expectBody(method, pathPattern, name string, want map[string]any, forbidden ...string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		for i := len(f.bodies) - 1; i >= 0; i-- {
			b := f.bodies[i]
			if b.Method != method || !pathMatches(b.Path, pathPattern) || (name != "" && b.Body["name"] != name) {
				continue
			}
			// 同じ名前のレコードが複数あるときは、want のタイプで見分ける
			if typ, ok := want["type"]; ok && b.Body["type"] != typ {
				continue
			}
			for k, v := range want {
				if got, ok := b.Body[k]; v == nil && (!ok || got != nil) {
					return fmt.Errorf("%s %s %q: %s must be sent as null, got %v (sent: %v)", method, pathPattern, name, k, got, ok)
				}
				if fmt.Sprint(b.Body[k]) != fmt.Sprint(v) {
					return fmt.Errorf("%s %s %q: %s = %v, want %v", method, pathPattern, name, k, b.Body[k], v)
				}
			}
			for _, k := range forbidden {
				if _, ok := b.Body[k]; ok {
					return fmt.Errorf("%s %s %q: %s must not be sent, got %v", method, pathPattern, name, k, b.Body[k])
				}
			}
			return nil
		}
		return fmt.Errorf("no %s %s request for %q", method, pathPattern, name)
	}
}

func (f *fakeDNS) checkDestroyed(*terraform.State) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.domains) != 0 {
		return fmt.Errorf("%d domains remain", len(f.domains))
	}
	return nil
}

func dnsConfig(f *fakeDNS, domain string, ttl int, email, body string) string {
	return f.s.ProviderConfig() + fmt.Sprintf(`
resource "conohavps_dns_domain" "main" {
  name  = %q
  ttl   = %d
  email = %q
}
`, domain, ttl, email) + body
}

const dnsRecordsV1 = `
resource "conohavps_dns_record" "www" {
  domain_id = conohavps_dns_domain.main.id
  name      = "www.example.com."
  type      = "A"
  data      = "192.0.2.10"
}

resource "conohavps_dns_record" "mx" {
  domain_id   = conohavps_dns_domain.main.id
  name        = "example.com."
  type        = "MX"
  data        = "mail.example.com."
  priority    = 10
  ttl         = 600
}

resource "conohavps_dns_record" "legacy_mx" {
  domain_id = conohavps_dns_domain.main.id
  name      = "old.example.com."
  type      = "MX"
  data      = "mail.example.com."
  priority  = 5
}

resource "conohavps_dns_record" "srv" {
  domain_id = conohavps_dns_domain.main.id
  name      = "_sip._tcp.example.com."
  type      = "SRV"
  data      = "sip.example.com."
  priority  = 10
  weight    = 60
  port      = 5060
}

resource "conohavps_dns_record" "txt" {
  domain_id = conohavps_dns_domain.main.id
  name      = "example.com."
  type      = "TXT"
  data      = "\"v=spf1 -all\""
}

resource "conohavps_dns_record" "alias" {
  domain_id = conohavps_dns_domain.main.id
  name      = "blog.example.com."
  type      = "CNAME"
  data      = "www.example.com."
}
`

// 更新: A の値と名前、MX の優先度・TTL・説明、SRV の重みとポートを変え、CNAME を AAAA に、MX を CNAME に変える.
const dnsRecordsV2 = `
resource "conohavps_dns_record" "www" {
  domain_id = conohavps_dns_domain.main.id
  name      = "web.example.com."
  type      = "A"
  data      = "192.0.2.20"
}

resource "conohavps_dns_record" "mx" {
  domain_id = conohavps_dns_domain.main.id
  name      = "example.com."
  type      = "MX"
  data      = "mail.example.com."
  priority  = 20
  ttl       = 300
}

resource "conohavps_dns_record" "legacy_mx" {
  domain_id = conohavps_dns_domain.main.id
  name      = "old.example.com."
  type      = "CNAME"
  data      = "www.example.com."
}

resource "conohavps_dns_record" "srv" {
  domain_id = conohavps_dns_domain.main.id
  name      = "_sip._tcp.example.com."
  type      = "SRV"
  data      = "sip.example.com."
  priority  = 10
  weight    = 5
  port      = 5061
}

resource "conohavps_dns_record" "txt" {
  domain_id = conohavps_dns_domain.main.id
  name      = "example.com."
  type      = "TXT"
  data      = "\"v=spf1 -all\""
}

resource "conohavps_dns_record" "alias" {
  domain_id = conohavps_dns_domain.main.id
  name      = "blog.example.com."
  type      = "AAAA"
  data      = "2001:db8::1"
}
`

func TestDNSDomainAndRecords(t *testing.T) {
	f := newFakeDNS(t)

	recordImport := func(name string) resource.ImportStateIdFunc {
		return func(s *terraform.State) (string, error) {
			rs := s.RootModule().Resources[name]
			return rs.Primary.Attributes["domain_id"] + "/" + rs.Primary.ID, nil
		}
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		CheckDestroy:             f.checkDestroyed,
		Steps: []resource.TestStep{
			// 作成
			{
				Config: dnsConfig(f, "example.com.", 3600, "admin@example.com", dnsRecordsV1),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_dns_domain.main", tfjsonpath.New("name"), knownvalue.StringExact("example.com.")),
					statecheck.ExpectKnownValue("conohavps_dns_domain.main", tfjsonpath.New("ttl"), knownvalue.Int64Exact(3600)),
					statecheck.ExpectKnownValue("conohavps_dns_domain.main", tfjsonpath.New("project_id"), knownvalue.StringExact(fakeapi.TenantID)),
					statecheck.ExpectKnownValue("conohavps_dns_domain.main", tfjsonpath.New("id"), knownvalue.StringRegexp(regexp.MustCompile(`^domain-`))),
					statecheck.ExpectKnownValue("conohavps_dns_record.www", tfjsonpath.New("ttl"), knownvalue.Int64Exact(3600)),
					statecheck.ExpectKnownValue("conohavps_dns_record.www", tfjsonpath.New("priority"), knownvalue.Null()),
					statecheck.ExpectKnownValue("conohavps_dns_record.mx", tfjsonpath.New("priority"), knownvalue.Int64Exact(10)),
					statecheck.ExpectKnownValue("conohavps_dns_record.mx", tfjsonpath.New("ttl"), knownvalue.Int64Exact(600)),
					statecheck.ExpectKnownValue("conohavps_dns_record.srv", tfjsonpath.New("port"), knownvalue.Int64Exact(5060)),
					statecheck.ExpectKnownValue("conohavps_dns_record.txt", tfjsonpath.New("data"), knownvalue.StringExact(`"v=spf1 -all"`)),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					f.expectBody("POST", dnsDomainsPath, "example.com.", map[string]any{"ttl": 3600, "email": "admin@example.com"}),
					f.expectBody("POST", dnsRecordsPath, "www.example.com.", map[string]any{"type": "A", "data": "192.0.2.10"}, "priority", "weight", "port", "ttl", "description"),
					f.expectBody("POST", dnsRecordsPath, "example.com.", map[string]any{"type": "MX", "priority": 10, "ttl": 600}, "weight", "port", "description"),
					f.expectBody("POST", dnsRecordsPath, "_sip._tcp.example.com.", map[string]any{"type": "SRV", "data": "sip.example.com.", "priority": 10, "weight": 60, "port": 5060}),
					f.expectBody("POST", dnsRecordsPath, "blog.example.com.", map[string]any{"type": "CNAME", "data": "www.example.com."}),
					resource.TestCheckResourceAttrPair("conohavps_dns_record.www", "domain_id", "conohavps_dns_domain.main", "id"),
				),
			},
			// インポート
			{ResourceName: "conohavps_dns_domain.main", ImportState: true, ImportStateVerify: true},
			{ResourceName: "conohavps_dns_record.www", ImportState: true, ImportStateVerify: true, ImportStateIdFunc: recordImport("conohavps_dns_record.www")},
			{ResourceName: "conohavps_dns_record.mx", ImportState: true, ImportStateVerify: true, ImportStateIdFunc: recordImport("conohavps_dns_record.mx")},
			{ResourceName: "conohavps_dns_record.legacy_mx", ImportState: true, ImportStateVerify: true, ImportStateIdFunc: recordImport("conohavps_dns_record.legacy_mx")},
			{ResourceName: "conohavps_dns_record.srv", ImportState: true, ImportStateVerify: true, ImportStateIdFunc: recordImport("conohavps_dns_record.srv")},
			{ResourceName: "conohavps_dns_record.txt", ImportState: true, ImportStateVerify: true, ImportStateIdFunc: recordImport("conohavps_dns_record.txt")},
			// 更新（ドメインの TTL とメールアドレス、レコードはタイプの変更も含めてその場で更新）
			{
				Config: dnsConfig(f, "example.com.", 600, "hostmaster@example.com", dnsRecordsV2),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("conohavps_dns_domain.main", plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction("conohavps_dns_record.www", plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction("conohavps_dns_record.mx", plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction("conohavps_dns_record.srv", plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction("conohavps_dns_record.txt", plancheck.ResourceActionNoop),
						plancheck.ExpectResourceAction("conohavps_dns_record.alias", plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction("conohavps_dns_record.legacy_mx", plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_dns_domain.main", tfjsonpath.New("ttl"), knownvalue.Int64Exact(600)),
					statecheck.ExpectKnownValue("conohavps_dns_domain.main", tfjsonpath.New("email"), knownvalue.StringExact("hostmaster@example.com")),
					statecheck.ExpectKnownValue("conohavps_dns_record.www", tfjsonpath.New("name"), knownvalue.StringExact("web.example.com.")),
					statecheck.ExpectKnownValue("conohavps_dns_record.www", tfjsonpath.New("data"), knownvalue.StringExact("192.0.2.20")),
					statecheck.ExpectKnownValue("conohavps_dns_record.mx", tfjsonpath.New("priority"), knownvalue.Int64Exact(20)),
					statecheck.ExpectKnownValue("conohavps_dns_record.srv", tfjsonpath.New("weight"), knownvalue.Int64Exact(5)),
					statecheck.ExpectKnownValue("conohavps_dns_record.alias", tfjsonpath.New("type"), knownvalue.StringExact("AAAA")),
					statecheck.ExpectKnownValue("conohavps_dns_record.mx", tfjsonpath.New("ttl"), knownvalue.Int64Exact(300)),
					statecheck.ExpectKnownValue("conohavps_dns_record.legacy_mx", tfjsonpath.New("type"), knownvalue.StringExact("CNAME")),
					statecheck.ExpectKnownValue("conohavps_dns_record.legacy_mx", tfjsonpath.New("priority"), knownvalue.Null()),
					// 設定に無い TTL は今の値を保つ
					statecheck.ExpectKnownValue("conohavps_dns_record.www", tfjsonpath.New("ttl"), knownvalue.Int64Exact(3600)),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					// ドメインの更新は ttl と email を送り、設定に無い説明と、更新できない名前は送らない
					f.expectBody("PUT", dnsDomainPath, "", map[string]any{"ttl": 600, "email": "hostmaster@example.com"}, "name", "description"),
					f.expectBody("PUT", dnsRecordsPath, "web.example.com.", map[string]any{"type": "A", "data": "192.0.2.20"}, "priority", "description"),
					f.expectBody("PUT", dnsRecordsPath, "_sip._tcp.example.com.", map[string]any{"priority": 10, "weight": 5, "port": 5061}),
					f.expectBody("PUT", dnsRecordsPath, "blog.example.com.", map[string]any{"type": "AAAA", "data": "2001:db8::1"}),
					f.expectBody("PUT", dnsRecordsPath, "example.com.", map[string]any{"type": "MX", "priority": 20, "ttl": 300}, "description"),
					// MX から CNAME に変えると、要らなくなった優先度を null で消す
					f.expectBody("PUT", dnsRecordsPath, "old.example.com.", map[string]any{"type": "CNAME", "data": "www.example.com.", "priority": nil}, "weight", "port"),
				),
			},
			{ResourceName: "conohavps_dns_record.alias", ImportState: true, ImportStateVerify: true, ImportStateIdFunc: recordImport("conohavps_dns_record.alias")},
			// ドメイン名の変更は作り直しになり、レコードも新しいドメインへ作り直す
			{
				Config: dnsConfig(f, "example.net.", 600, "hostmaster@example.com", `
resource "conohavps_dns_record" "www" {
  domain_id = conohavps_dns_domain.main.id
  name      = "www.example.net."
  type      = "A"
  data      = "192.0.2.20"
}
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("conohavps_dns_domain.main", plancheck.ResourceActionDestroyBeforeCreate),
						plancheck.ExpectResourceAction("conohavps_dns_record.www", plancheck.ResourceActionDestroyBeforeCreate),
						plancheck.ExpectResourceAction("conohavps_dns_record.mx", plancheck.ResourceActionDestroy),
						plancheck.ExpectResourceAction("conohavps_dns_record.legacy_mx", plancheck.ResourceActionDestroy),
					},
				},
				Check: f.expectBody("POST", dnsDomainsPath, "example.net.", map[string]any{"ttl": 600}),
			},
		},
	})
}

// API が表記を揃えて返しても（小文字化・IPv6 の省略・TXT の引用符）、plan に差分が出ない.
func TestDNSRecordNormalization(t *testing.T) {
	f := newFakeDNS(t)
	config := dnsConfig(f, "Example.COM.", 3600, "admin@example.com", `
resource "conohavps_dns_record" "v6" {
  domain_id = conohavps_dns_domain.main.id
  name      = "WWW.Example.COM."
  type      = "AAAA"
  data      = "2001:DB8:0:0:0:0:0:1"
}

resource "conohavps_dns_record" "alias" {
  domain_id = conohavps_dns_domain.main.id
  name      = "Blog.Example.COM."
  type      = "CNAME"
  data      = "WWW.Example.COM."
}

resource "conohavps_dns_record" "txt" {
  domain_id = conohavps_dns_domain.main.id
  name      = "Example.COM."
  type      = "TXT"
  data      = "v=spf1 -all"
}
`)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		CheckDestroy:             f.checkDestroyed,
		Steps: []resource.TestStep{
			{
				Config: config,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_dns_domain.main", tfjsonpath.New("name"), knownvalue.StringExact("Example.COM.")),
					statecheck.ExpectKnownValue("conohavps_dns_record.v6", tfjsonpath.New("name"), knownvalue.StringExact("WWW.Example.COM.")),
					statecheck.ExpectKnownValue("conohavps_dns_record.v6", tfjsonpath.New("data"), knownvalue.StringExact("2001:DB8:0:0:0:0:0:1")),
					statecheck.ExpectKnownValue("conohavps_dns_record.txt", tfjsonpath.New("data"), knownvalue.StringExact("v=spf1 -all")),
				},
			},
			// 更新して読み直しても差分が出ない
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
		},
	})
}

// メールアドレスとレコードの TTL を外で変えられたら差分として出して戻す.
// description は ConoHa の API が保存も返却もしないため、リソースにもリクエストにも無い.
func TestDNSDrift(t *testing.T) {
	f := newFakeDNS(t)
	config := f.s.ProviderConfig() + `
resource "conohavps_dns_domain" "main" {
  name  = "example.com."
  ttl   = 3600
  email = "admin@example.com"
}

resource "conohavps_dns_record" "www" {
  domain_id = conohavps_dns_domain.main.id
  name      = "www.example.com."
  type      = "A"
  data      = "192.0.2.10"
  ttl       = 300
}
`
	// 外で変える. 次の plan で差分が出る
	changeOutside := func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		for id, d := range f.domains {
			d["email"] = "intruder@example.org"
			for _, rec := range f.records[id] {
				if rec["type"] == "A" {
					rec["ttl"] = 7200
				}
			}
		}
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		CheckDestroy:             f.checkDestroyed,
		Steps: []resource.TestStep{
			{
				Config: config,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_dns_record.www", tfjsonpath.New("ttl"), knownvalue.Int64Exact(300)),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					f.expectBody("POST", dnsDomainsPath, "example.com.", map[string]any{"email": "admin@example.com"}, "description"),
					f.expectBody("POST", dnsRecordsPath, "www.example.com.", map[string]any{"ttl": 300}, "description"),
				),
			},
			{ResourceName: "conohavps_dns_domain.main", ImportState: true, ImportStateVerify: true},
			// 外での変更は refresh で差分になり、apply で設定の値に戻す
			{
				PreConfig: changeOutside,
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("conohavps_dns_domain.main", plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction("conohavps_dns_record.www", plancheck.ResourceActionUpdate),
						plancheck.ExpectKnownValue("conohavps_dns_domain.main", tfjsonpath.New("email"), knownvalue.StringExact("admin@example.com")),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					f.expectBody("PUT", dnsDomainPath, "", map[string]any{"email": "admin@example.com"}, "description"),
					f.expectBody("PUT", dnsRecordsPath, "www.example.com.", map[string]any{"ttl": 300}, "description"),
				),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_dns_domain.main", tfjsonpath.New("email"), knownvalue.StringExact("admin@example.com")),
					statecheck.ExpectKnownValue("conohavps_dns_record.www", tfjsonpath.New("ttl"), knownvalue.Int64Exact(300)),
				},
			},
		},
	})
}

// API が HTML ドキュメントのレスポンス例の形（uuid・domain_uuid・伏せ字のメールアドレス）で返しても、
// ID を読み、伏せ字のメールアドレスで差分を出さない.
func TestDNSLegacyResponseShape(t *testing.T) {
	f := newFakeDNS(t)
	f.legacy = true
	config := dnsConfig(f, "example.com.", 3600, "admin@example.com", `
resource "conohavps_dns_record" "www" {
  domain_id = conohavps_dns_domain.main.id
  name      = "www.example.com."
  type      = "A"
  data      = "192.0.2.10"
}
`)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		CheckDestroy:             f.checkDestroyed,
		Steps: []resource.TestStep{
			{
				Config: config,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_dns_domain.main", tfjsonpath.New("id"), knownvalue.StringRegexp(regexp.MustCompile(`^domain-`))),
					statecheck.ExpectKnownValue("conohavps_dns_domain.main", tfjsonpath.New("email"), knownvalue.StringExact("admin@example.com")),
					statecheck.ExpectKnownValue("conohavps_dns_record.www", tfjsonpath.New("id"), knownvalue.StringRegexp(regexp.MustCompile(`^record-`))),
				},
				Check: resource.TestCheckResourceAttrPair("conohavps_dns_record.www", "domain_id", "conohavps_dns_domain.main", "id"),
			},
			{ResourceName: "conohavps_dns_record.www", ImportState: true, ImportStateVerify: true, ImportStateIdFunc: func(s *terraform.State) (string, error) {
				rs := s.RootModule().Resources["conohavps_dns_record.www"]
				return rs.Primary.Attributes["domain_id"] + "/" + rs.Primary.ID, nil
			}},
			{Config: config, PlanOnly: true},
		},
	})
}

// 外で消されたレコードは読み込み時に state から外れ、次の apply で作り直す.
func TestDNSRecordDeletedOutside(t *testing.T) {
	f := newFakeDNS(t)
	config := dnsConfig(f, "example.com.", 3600, "admin@example.com", `
resource "conohavps_dns_record" "www" {
  domain_id = conohavps_dns_domain.main.id
  name      = "www.example.com."
  type      = "A"
  data      = "192.0.2.10"
}
`)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		CheckDestroy:             f.checkDestroyed,
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig: func() {
					f.mu.Lock()
					defer f.mu.Unlock()
					for _, recs := range f.records {
						for id, rec := range recs {
							if rec["type"] == "A" {
								delete(recs, id)
							}
						}
					}
				},
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("conohavps_dns_record.www", plancheck.ResourceActionCreate),
					},
				},
			},
		},
	})
}

// ドキュメントの制約に反する設定は、API を呼ぶ前に弾く.
func TestDNSValidation(t *testing.T) {
	f := newFakeDNS(t)
	record := func(body string) string {
		return dnsConfig(f, "example.com.", 3600, "admin@example.com", `
resource "conohavps_dns_record" "r" {
  domain_id = conohavps_dns_domain.main.id
`+body+`
}
`)
	}
	cases := []struct {
		config string
		err    string
	}{
		{dnsConfig(f, "example.com", 3600, "admin@example.com", ""), `must be a domain name ending with a period`},
		{dnsConfig(f, "example.com.", 0, "admin@example.com", ""), `must be between 1 and 2147483647`},
		{dnsConfig(f, "example.com.", 3600, "admin", ""), `must be an email address`},
		{record(`name = "www.example.com"
  type = "A"
  data = "192.0.2.1"`), `must be a name ending with a period`},
		{record(`name = "example.com."
  type = "SOA"
  data = "x"`), `value must be one of`},
		{record(`name = "example.com."
  type = "MX"
  data = "mail.example.com."`), `"priority" is required for MX records`},
		{record(`name = "_sip._tcp.example.com."
  type = "SRV"
  data = "sip.example.com."
  priority = 10`), `"weight" is required for SRV records`},
		{record(`name = "www.example.com."
  type = "A"
  data = "192.0.2.1"
  priority = 10`), `"priority" is not allowed for A records`},
		{record(`name = "www.example.com."
  type = "A"
  data = "2001:db8::1"`), `is not an IPv4 address`},
		{record(`name = "www.example.com."
  type = "AAAA"
  data = "192.0.2.1"`), `is not an IPv6 address`},
		{record(`name = "example.com."
  type = "MX"
  data = "mail.example.com."
  priority = 70000`), `must be between 0 and 65535`},
		{record(`name = "www.example.com."
  type = "A"
  data = "192.0.2.1"
  ttl = 0`), `must be between 1 and 2147483647`},
	}
	var steps []resource.TestStep
	for _, c := range cases {
		steps = append(steps, resource.TestStep{Config: c.config, PlanOnly: true, ExpectError: regexp.MustCompile(c.err)})
	}
	resource.UnitTest(t, resource.TestCase{ProtoV6ProviderFactories: fakeapi.Factories, Steps: steps})
}

// レコードのインポート ID は "<ドメイン ID>/<レコード ID>" でなければならない.
func TestDNSRecordImportID(t *testing.T) {
	f := newFakeDNS(t)
	config := f.s.ProviderConfig() + `
resource "conohavps_dns_record" "r" {
  domain_id = "d"
  name      = "www.example.com."
  type      = "A"
  data      = "192.0.2.1"
}
`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		Steps: []resource.TestStep{{
			Config: config, ResourceName: "conohavps_dns_record.r", ImportState: true, ImportStateId: "record-only",
			ExpectError: regexp.MustCompile(`<domain_id>/<record_id>`),
		}},
	})
}
