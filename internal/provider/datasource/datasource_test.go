// データソースのテストを提供する.
// 認証・サービスカタログ・フレーバー一覧・イメージ一覧を返す偽の ConoHa API を立て、
// Terraform からデータソースを読むところまでを、実際の API を使わずに検証する.

package datasource_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

const fakeToken = "fake-token"

// 偽の ConoHa API. イメージのエンドポイントは、カタログに /v2 付きで載っている場合を再現する.
func fakeConoHa(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("POST /identity/v3/auth/tokens", func(w http.ResponseWriter, _ *http.Request) {
		endpoint := func(typ, url string) map[string]any {
			return map[string]any{"type": typ, "endpoints": []map[string]any{{"interface": "public", "region": "c3j1", "region_id": "c3j1", "url": url}}}
		}
		w.Header().Set("X-Subject-Token", fakeToken)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"token": map[string]any{
			"expires_at": "2099-01-01T00:00:00.000000Z",
			"catalog": []map[string]any{
				endpoint("compute", srv.URL+"/compute/v2.1"),
				endpoint("image", srv.URL+"/image/v2"),
				endpoint("volumev3", srv.URL+"/volume/v3/tenant"),
				endpoint("network", srv.URL+"/network"),
			},
		}})
	})
	// ネットワーク API の初期化はバージョン一覧を読む
	mux.HandleFunc("GET /network/", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"versions": []map[string]any{{"id": "v2.0", "status": "CURRENT", "links": []map[string]any{{"rel": "self", "href": srv.URL + "/network/v2.0/"}}}}})
	})
	mux.HandleFunc("GET /compute/v2.1/flavors/detail", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(w, r) {
			return
		}
		writeJSON(w, map[string]any{"flavors": []map[string]any{
			{"id": "uuid-c4m4", "name": "g2l-t-c4m4", "vcpus": 4, "ram": 4096, "disk": 0},
			{"id": "uuid-c6m12", "name": "g2l-t-c6m12", "vcpus": 6, "ram": 12288, "disk": 0},
			{"id": "uuid-p-c4m4", "name": "g2l-p-c4m4", "vcpus": 4, "ram": 4096, "disk": 0},
		}})
	})
	mux.HandleFunc("GET /image/v2/images", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(w, r) {
			return
		}
		all := []map[string]any{
			{"id": "img-ubuntu", "name": "vmi-ubuntu-24.04-amd64", "status": "active", "min_disk": 30},
			{"id": "img-old", "name": "vmi-ubuntu-24.04-amd64", "status": "deactivated", "min_disk": 30},
		}
		var images []map[string]any
		for _, i := range all {
			if name := r.URL.Query().Get("name"); name == "" || i["name"] == name {
				images = append(images, i)
			}
		}
		writeJSON(w, map[string]any{"images": images})
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func authorized(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("X-Auth-Token") != fakeToken {
		w.WriteHeader(http.StatusUnauthorized)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func providerConfig(srv *httptest.Server) string {
	return fmt.Sprintf(`
provider "conohavps" {
  identity_endpoint = "%s/identity/v3"
  user_id           = "user"
  password          = "password"
  tenant_id         = "tenant"
  region            = "c3j1"
}
`, srv.URL)
}

var factories = map[string]func() (tfprotov6.ProviderServer, error){
	"conohavps": providerserver.NewProtocol6WithError(provider.New("0.0.0-test")()),
}

func TestFlavorAndImageByName(t *testing.T) {
	t.Setenv("CONOHAVPS_USER_ID", "")
	t.Setenv("CONOHAVPS_PASSWORD", "")
	t.Setenv("CONOHAVPS_TENANT_ID", "")
	t.Setenv("CONOHAVPS_IDENTITY_ENDPOINT", "")
	t.Setenv("CONOHAVPS_REGION", "")
	srv := fakeConoHa(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: providerConfig(srv) + `
data "conohavps_flavor" "main" {
  name = "g2l-t-c6m12"
}

data "conohavps_image" "ubuntu" {
  name = "vmi-ubuntu-24.04-amd64"
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("data.conohavps_flavor.main", tfjsonpath.New("id"), knownvalue.StringExact("uuid-c6m12")),
				statecheck.ExpectKnownValue("data.conohavps_flavor.main", tfjsonpath.New("vcpus"), knownvalue.Int64Exact(6)),
				statecheck.ExpectKnownValue("data.conohavps_flavor.main", tfjsonpath.New("ram"), knownvalue.Int64Exact(12288)),
				// 使える状態のものだけを選ぶ（同名の停止済みイメージは選ばない）
				statecheck.ExpectKnownValue("data.conohavps_image.ubuntu", tfjsonpath.New("id"), knownvalue.StringExact("img-ubuntu")),
				statecheck.ExpectKnownValue("data.conohavps_image.ubuntu", tfjsonpath.New("min_disk"), knownvalue.Int64Exact(30)),
			},
		}},
	})
}

func TestUnknownNamesFail(t *testing.T) {
	t.Setenv("CONOHAVPS_USER_ID", "")
	t.Setenv("CONOHAVPS_PASSWORD", "")
	t.Setenv("CONOHAVPS_TENANT_ID", "")
	t.Setenv("CONOHAVPS_IDENTITY_ENDPOINT", "")
	t.Setenv("CONOHAVPS_REGION", "")
	srv := fakeConoHa(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:      providerConfig(srv) + `data "conohavps_flavor" "x" { name = "g2l-t-c99m999" }`,
				ExpectError: regexp.MustCompile(`no flavor is named "g2l-t-c99m999"`),
			},
			{
				Config:      providerConfig(srv) + `data "conohavps_image" "x" { name = "vmi-nope" }`,
				ExpectError: regexp.MustCompile(`no active image is named "vmi-nope"`),
			},
		},
	})
}
