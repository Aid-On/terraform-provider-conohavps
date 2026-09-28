// データソースのテストを提供する.
// 偽の ConoHa API にフレーバー一覧とイメージ一覧を足し、Terraform からデータソースを読むところまでを、
// 実際の API を使わずに検証する.

package datasource_test

import (
	"net/http"
	"net/url"
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
	// API 仕様（FlavorResBody）の形. swap は空文字列で返る
	flavor := func(id, name string, vcpus, ram int, extra map[string]any) map[string]any {
		f := map[string]any{
			"id": id, "name": name, "vcpus": vcpus, "ram": ram, "disk": 0, "swap": "",
			"OS-FLV-EXT-DATA:ephemeral": 0, "OS-FLV-DISABLED:disabled": false, "os-flavor-access:is_public": true,
			"rxtx_factor": 1.0, "links": []any{},
		}
		if extra != nil {
			f["extra_specs"] = extra
		}
		return f
	}
	s.Mux.HandleFunc("GET /compute/v2.1/flavors/detail", func(w http.ResponseWriter, r *http.Request) {
		if !fakeapi.Authorized(w, r) {
			return
		}
		// 詳細一覧取得にクエリパラメータは無い
		if r.URL.RawQuery != "" {
			fakeapi.WriteJSON(w, http.StatusBadRequest, map[string]any{"badRequest": map[string]any{"message": "unexpected query " + r.URL.RawQuery}})
			return
		}
		fakeapi.WriteJSON(w, http.StatusOK, map[string]any{"flavors": []map[string]any{
			flavor("uuid-c4m4", "g2l-t-c4m4", 4, 4096, nil),
			flavor("uuid-c6m12", "g2l-t-c6m12", 6, 12288, map[string]any{}),
			flavor("uuid-p-c4m4", "g2l-p-c4m4", 4, 4096, nil),
			flavor("uuid-kusanagi", "g2l-t-kusanagi-c4m4", 4, 4096, map[string]any{"specialized_kusanagi": "true"}),
		}})
	})
	s.Mux.HandleFunc("GET /image-service/v2/images", func(w http.ResponseWriter, r *http.Request) {
		if !fakeapi.Authorized(w, r) {
			return
		}
		// API 仕様（ImagesBody）の形. 日時はタイムゾーンを含まず、virtual_size は null で返る
		image := func(id, status string) map[string]any {
			return map[string]any{
				"id": id, "name": "vmi-ubuntu-24.04-amd64", "status": status, "min_disk": 30, "min_ram": 1024,
				"size": 13167616, "visibility": "public", "os_type": "linux", "os_version": "24.04", "architecture": "x86_64",
				"tags": []any{}, "container_format": "bare", "disk_format": "qcow2", "protected": false, "os_hidden": false,
				"created_at": "2018-11-28T06:25:15.288987", "updated_at": "2018-11-28T06:25:15.288987", "virtual_size": nil,
				"self": "/v2/images/" + id, "file": "/v2/images/" + id + "/file", "schema": "/v2/schemas/image",
			}
		}
		// 1件ずつのページに分け、next のリンク（/v2/images?marker=...）で次のページを示す
		all := []map[string]any{image("img-old", "deactivated"), image("img-ubuntu", "active")}
		var images []map[string]any
		for _, i := range all {
			if name := r.URL.Query().Get("name"); name == "" || i["name"] == name {
				images = append(images, i)
			}
		}
		start := 0
		if marker := r.URL.Query().Get("marker"); marker != "" {
			for n, i := range images {
				if i["id"] == marker {
					start = n + 1
				}
			}
		}
		page := map[string]any{"images": images[start:min(start+1, len(images))], "schema": "/v2/schemas/images", "first": "/v2/images"}
		if start+1 < len(images) {
			q := url.Values{"marker": {images[start]["id"].(string)}, "name": {r.URL.Query().Get("name")}}
			page["next"] = "/v2/images?" + q.Encode()
		}
		fakeapi.WriteJSON(w, http.StatusOK, page)
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

data "conohavps_flavor" "kusanagi" {
  name = "g2l-t-kusanagi-c4m4"
}

data "conohavps_image" "ubuntu" {
  name = "vmi-ubuntu-24.04-amd64"
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("data.conohavps_flavor.main", tfjsonpath.New("id"), knownvalue.StringExact("uuid-c6m12")),
				statecheck.ExpectKnownValue("data.conohavps_flavor.main", tfjsonpath.New("vcpus"), knownvalue.Int64Exact(6)),
				statecheck.ExpectKnownValue("data.conohavps_flavor.main", tfjsonpath.New("ram"), knownvalue.Int64Exact(12288)),
				statecheck.ExpectKnownValue("data.conohavps_flavor.main", tfjsonpath.New("extra_specs"), knownvalue.MapExact(map[string]knownvalue.Check{})),
				statecheck.ExpectKnownValue("data.conohavps_flavor.kusanagi", tfjsonpath.New("extra_specs"), knownvalue.MapExact(map[string]knownvalue.Check{
					"specialized_kusanagi": knownvalue.StringExact("true"),
				})),
				// 使える状態のものだけを選ぶ（同名の停止済みイメージは選ばない）. 2ページ目にあっても next をたどって見つける
				statecheck.ExpectKnownValue("data.conohavps_image.ubuntu", tfjsonpath.New("id"), knownvalue.StringExact("img-ubuntu")),
				statecheck.ExpectKnownValue("data.conohavps_image.ubuntu", tfjsonpath.New("min_disk"), knownvalue.Int64Exact(30)),
				statecheck.ExpectKnownValue("data.conohavps_image.ubuntu", tfjsonpath.New("min_ram"), knownvalue.Int64Exact(1024)),
				statecheck.ExpectKnownValue("data.conohavps_image.ubuntu", tfjsonpath.New("size"), knownvalue.Int64Exact(13167616)),
				statecheck.ExpectKnownValue("data.conohavps_image.ubuntu", tfjsonpath.New("visibility"), knownvalue.StringExact("public")),
				statecheck.ExpectKnownValue("data.conohavps_image.ubuntu", tfjsonpath.New("os_type"), knownvalue.StringExact("linux")),
				statecheck.ExpectKnownValue("data.conohavps_image.ubuntu", tfjsonpath.New("os_version"), knownvalue.StringExact("24.04")),
				statecheck.ExpectKnownValue("data.conohavps_image.ubuntu", tfjsonpath.New("architecture"), knownvalue.StringExact("x86_64")),
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
