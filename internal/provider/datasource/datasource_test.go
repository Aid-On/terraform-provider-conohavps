// データソースのテストを提供する.
// 偽の ConoHa API にフレーバー一覧とイメージ一覧を足し、Terraform からデータソースを読むところまでを、
// 実際の API を使わずに検証する.

package datasource_test

import (
	"net/http"
	"regexp"
	"testing"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/fakeapi"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

func withFlavorsAndImages(t *testing.T) *fakeapi.Server {
	s := fakeapi.New(t)
	s.Mux.HandleFunc("GET /compute/v2.1/flavors/detail", func(w http.ResponseWriter, r *http.Request) {
		if !fakeapi.Authorized(w, r) {
			return
		}
		fakeapi.WriteJSON(w, http.StatusOK, map[string]any{"flavors": []map[string]any{
			{"id": "uuid-c4m4", "name": "g2l-t-c4m4", "vcpus": 4, "ram": 4096, "disk": 0},
			{"id": "uuid-c6m12", "name": "g2l-t-c6m12", "vcpus": 6, "ram": 12288, "disk": 0},
			{"id": "uuid-p-c4m4", "name": "g2l-p-c4m4", "vcpus": 4, "ram": 4096, "disk": 0},
		}})
	})
	s.Mux.HandleFunc("GET /image-service/v2/images", func(w http.ResponseWriter, r *http.Request) {
		if !fakeapi.Authorized(w, r) {
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
		fakeapi.WriteJSON(w, http.StatusOK, map[string]any{"images": images})
	})
	return s
}

func TestFlavorAndImageByName(t *testing.T) {
	s := withFlavorsAndImages(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		Steps: []resource.TestStep{{
			Config: s.ProviderConfig() + `
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
	s := withFlavorsAndImages(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		Steps: []resource.TestStep{
			{
				Config:      s.ProviderConfig() + `data "conohavps_flavor" "x" { name = "g2l-t-c99m999" }`,
				ExpectError: regexp.MustCompile(`no flavor is named "g2l-t-c99m999"`),
			},
			{
				Config:      s.ProviderConfig() + `data "conohavps_image" "x" { name = "vmi-nope" }`,
				ExpectError: regexp.MustCompile(`no active image is named "vmi-nope"`),
			},
		},
	})
}
