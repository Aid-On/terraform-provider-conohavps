// DNS のドメインのデータソースのテストを提供する.
// 偽の ConoHa API にドメイン一覧（1回に2件までしか返さない）を足し、ページをまたいで名前で引けることを検証する.

package datasource_test

import (
	"net/http"
	"regexp"
	"strconv"
	"testing"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/fakeapi"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

func withDNSDomains(t *testing.T) *fakeapi.Server {
	s := fakeapi.New(t)
	all := []map[string]any{
		// OpenAPI 仕様の DomainBody の形. 1件目だけ HTML ドキュメントの形（uuid）で返す
		{"uuid": "dom-a", "name": "a.example.com.", "project_id": fakeapi.TenantID, "serial": 1, "ttl": 3600, "email": "a@example.com"},
		{"id": "dom-b", "name": "b.example.com.", "project_id": fakeapi.TenantID, "serial": 1, "ttl": 3600, "email": "b@example.com", "description": ""},
		{"id": "dom-c", "name": "c.example.com.", "project_id": fakeapi.TenantID, "serial": 1, "ttl": 600, "email": "c@example.com", "description": "My domain"},
	}
	s.Mux.HandleFunc("GET /dns-service/v1/domains", func(w http.ResponseWriter, r *http.Request) {
		if !fakeapi.Authorized(w, r) {
			return
		}
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		end := min(offset+2, len(all)) // 実物の上限が小さくても辿れることを確かめるため、2件ずつ返す
		page := []map[string]any{}
		if offset < len(all) {
			page = all[offset:end]
		}
		fakeapi.WriteJSON(w, http.StatusOK, map[string]any{"domains": page, "total_count": len(all)})
	})
	return s
}

func TestDNSDomainByName(t *testing.T) {
	s := withDNSDomains(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		Steps: []resource.TestStep{
			{
				// 3件目は2ページ目にある. 末尾のピリオドの省略と大文字も受け付ける
				Config: s.ProviderConfig() + `data "conohavps_dns_domain" "c" { name = "C.example.com" }`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("data.conohavps_dns_domain.c", tfjsonpath.New("id"), knownvalue.StringExact("dom-c")),
					statecheck.ExpectKnownValue("data.conohavps_dns_domain.c", tfjsonpath.New("ttl"), knownvalue.Int64Exact(600)),
					statecheck.ExpectKnownValue("data.conohavps_dns_domain.c", tfjsonpath.New("email"), knownvalue.StringExact("c@example.com")),
					statecheck.ExpectKnownValue("data.conohavps_dns_domain.c", tfjsonpath.New("description"), knownvalue.StringExact("My domain")),
					statecheck.ExpectKnownValue("data.conohavps_dns_domain.c", tfjsonpath.New("project_id"), knownvalue.StringExact(fakeapi.TenantID)),
				},
			},
			{
				// uuid で返るドメインも ID を読み、説明が無ければ null にする
				Config: s.ProviderConfig() + `data "conohavps_dns_domain" "a" { name = "a.example.com." }`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("data.conohavps_dns_domain.a", tfjsonpath.New("id"), knownvalue.StringExact("dom-a")),
					statecheck.ExpectKnownValue("data.conohavps_dns_domain.a", tfjsonpath.New("description"), knownvalue.Null()),
				},
			},
			{
				Config:      s.ProviderConfig() + `data "conohavps_dns_domain" "x" { name = "nope.example.com." }`,
				ExpectError: regexp.MustCompile(`no DNS domain is named "nope.example.com."`),
			},
		},
	})
}
