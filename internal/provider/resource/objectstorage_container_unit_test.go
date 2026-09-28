// オブジェクトストレージのコンテナのリソースの単体テストを提供する.
// 偽の API を相手に、作成・設定の変更・外での変更の検出・再作成・インポート・削除と、
// OpenAPI 仕様どおりのヘッダーが送られることを検証する.

package resource_test

import (
	"fmt"
	"net/http"
	"regexp"
	"testing"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/fakeapi"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

const containerAddr = "conohavps_objectstorage_container.test"

func containerConfig(f *fakeObjectStorage, name, settings string) string {
	return f.ProviderConfig() + fmt.Sprintf(`
resource "conohavps_objectstorage_container" "archive" {
  name = "archive"
}

resource "conohavps_objectstorage_container" "test" {
  name = %q
%s
}
`, name, settings)
}

// すべての設定を持つコンテナ.
const containerAllSettings = `
  versions_location = conohavps_objectstorage_container.archive.name
  container_read    = ".r:*,.rlistings"
  container_write   = "tenant:user"
  web_index         = "index.html"
  web_listings      = true
  web_listings_css  = "listing.css"
  web_error         = "error.html"
  metadata = {
    owner                         = "team-a"
    "access-control-allow-origin" = "*"
  }
`

// 最後の該当リクエストが、ヘッダーを1つずつ期待どおりの値で持つかを確かめる.
func (f *fakeObjectStorage) expectHeaders(method, path string, want map[string]string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		for key, value := range want {
			if err := f.expectHeader(method, path, key, value); err != nil {
				return err
			}
		}
		return nil
	}
}

// 最後の該当リクエストが、ヘッダー key を持たないことを確かめる.
func (f *fakeObjectStorage) expectNoHeader(method, path, key string) error {
	found := f.find(method, path)
	if len(found) == 0 {
		return fmt.Errorf("no %s %s request was sent", method, path)
	}
	if v, ok := found[len(found)-1].Header[http.CanonicalHeaderKey(key)]; ok {
		return fmt.Errorf("the last %s %s request has %s: %q, want none", method, path, key, v)
	}
	return nil
}

func TestObjectStorageContainer_Lifecycle(t *testing.T) {
	f := newFakeObjectStorage(t)
	const photos = objectStorageBase + "/photos"

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		CheckDestroy: func(_ *terraform.State) (err error) {
			f.with(func(f *fakeObjectStorage) {
				if len(f.containers) != 0 {
					err = fmt.Errorf("containers remain after destroy: %v", f.containers)
				}
			})
			return err
		},
		Steps: []resource.TestStep{
			// 作成（設定なし）
			{
				Config: containerConfig(f, "photos", ""),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(containerAddr, tfjsonpath.New("id"), knownvalue.StringExact("photos")),
					statecheck.ExpectKnownValue(containerAddr, tfjsonpath.New("versions_location"), knownvalue.Null()),
					statecheck.ExpectKnownValue(containerAddr, tfjsonpath.New("container_read"), knownvalue.Null()),
					statecheck.ExpectKnownValue(containerAddr, tfjsonpath.New("container_write"), knownvalue.Null()),
					statecheck.ExpectKnownValue(containerAddr, tfjsonpath.New("web_index"), knownvalue.Null()),
					statecheck.ExpectKnownValue(containerAddr, tfjsonpath.New("web_listings"), knownvalue.Null()),
					statecheck.ExpectKnownValue(containerAddr, tfjsonpath.New("metadata"), knownvalue.Null()),
					statecheck.ExpectKnownValue(containerAddr, tfjsonpath.New("storage_policy"), knownvalue.StringExact("default-placement")),
					statecheck.ExpectKnownValue(containerAddr, tfjsonpath.New("object_count"), knownvalue.Int64Exact(0)),
				},
				Check: func(_ *terraform.State) error {
					if n := len(f.find("PUT", photos)); n != 1 {
						return fmt.Errorf("want 1 PUT for the container, got %d", n)
					}
					// 設定が無いときは、作成時にバージョニングのヘッダーを送らず、POST もしない
					if err := f.expectNoHeader("PUT", photos, "X-Versions-Location"); err != nil {
						return err
					}
					if n := len(f.find("POST", photos)); n != 0 {
						return fmt.Errorf("want no POST for the container, got %d", n)
					}
					return nil
				},
			},
			// すべての設定をその場で変える
			{
				Config: containerConfig(f, "photos", containerAllSettings),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(containerAddr, plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(containerAddr, tfjsonpath.New("versions_location"), knownvalue.StringExact("archive")),
					statecheck.ExpectKnownValue(containerAddr, tfjsonpath.New("container_read"), knownvalue.StringExact(".r:*,.rlistings")),
					statecheck.ExpectKnownValue(containerAddr, tfjsonpath.New("container_write"), knownvalue.StringExact("tenant:user")),
					statecheck.ExpectKnownValue(containerAddr, tfjsonpath.New("web_index"), knownvalue.StringExact("index.html")),
					statecheck.ExpectKnownValue(containerAddr, tfjsonpath.New("web_listings"), knownvalue.Bool(true)),
					statecheck.ExpectKnownValue(containerAddr, tfjsonpath.New("web_listings_css"), knownvalue.StringExact("listing.css")),
					statecheck.ExpectKnownValue(containerAddr, tfjsonpath.New("web_error"), knownvalue.StringExact("error.html")),
					statecheck.ExpectKnownValue(containerAddr, tfjsonpath.New("metadata"), knownvalue.MapExact(map[string]knownvalue.Check{
						"owner":                       knownvalue.StringExact("team-a"),
						"access-control-allow-origin": knownvalue.StringExact("*"),
					})),
				},
				Check: f.expectHeaders("POST", photos, map[string]string{
					"X-Versions-Location":                          "archive",
					"X-Container-Read":                             ".r:*,.rlistings",
					"X-Container-Write":                            "tenant:user",
					"X-Container-Meta-Web-Index":                   "index.html",
					"X-Container-Meta-Web-Listings":                "true",
					"X-Container-Meta-Web-Listings-CSS":            "listing.css",
					"X-Container-Meta-Web-Error":                   "error.html",
					"X-Container-Meta-Owner":                       "team-a",
					"X-Container-Meta-Access-Control-Allow-Origin": "*",
				}),
			},
			// インポートは、すべての設定をコンテナから読む
			{
				ResourceName:      containerAddr,
				ImportState:       true,
				ImportStateId:     "photos",
				ImportStateVerify: true,
			},
			// 外で変えられた設定は差分として見せ、変わった設定のヘッダーだけを送って戻す
			{
				PreConfig: func() {
					f.with(func(f *fakeObjectStorage) {
						c := f.containers["photos"]
						c.containerRead = ".r:*"
						c.meta["web-listings"] = "false"
						c.meta["extra"] = "added outside"
					})
				},
				Config: containerConfig(f, "photos", containerAllSettings),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(containerAddr, plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(containerAddr, tfjsonpath.New("container_read"), knownvalue.StringExact(".r:*,.rlistings")),
					statecheck.ExpectKnownValue(containerAddr, tfjsonpath.New("web_listings"), knownvalue.Bool(true)),
					statecheck.ExpectKnownValue(containerAddr, tfjsonpath.New("metadata"), knownvalue.MapSizeExact(2)),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					f.expectHeaders("POST", photos, map[string]string{
						"X-Container-Read":              ".r:*,.rlistings",
						"X-Container-Meta-Web-Listings": "true",
						"X-Remove-Container-Meta-Extra": "x",
					}),
					func(_ *terraform.State) error {
						return f.expectNoHeader("POST", photos, "X-Container-Meta-Web-Index")
					},
				),
			},
			// 設定を外すと、X-Remove- のヘッダーで解除する
			{
				Config: containerConfig(f, "photos", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(containerAddr, plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(containerAddr, tfjsonpath.New("versions_location"), knownvalue.Null()),
					statecheck.ExpectKnownValue(containerAddr, tfjsonpath.New("container_read"), knownvalue.Null()),
					statecheck.ExpectKnownValue(containerAddr, tfjsonpath.New("web_listings"), knownvalue.Null()),
					statecheck.ExpectKnownValue(containerAddr, tfjsonpath.New("metadata"), knownvalue.Null()),
				},
				Check: f.expectHeaders("POST", photos, map[string]string{
					"X-Remove-Versions-Location":                          "x",
					"X-Remove-Container-Read":                             "x",
					"X-Remove-Container-Write":                            "x",
					"X-Remove-Container-Meta-Web-Index":                   "x",
					"X-Remove-Container-Meta-Web-Listings":                "x",
					"X-Remove-Container-Meta-Web-Listings-CSS":            "x",
					"X-Remove-Container-Meta-Web-Error":                   "x",
					"X-Remove-Container-Meta-Owner":                       "x",
					"X-Remove-Container-Meta-Access-Control-Allow-Origin": "x",
				}),
			},
			// 名前の変更は再作成
			{
				Config: containerConfig(f, "pictures", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(containerAddr, plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(containerAddr, tfjsonpath.New("id"), knownvalue.StringExact("pictures")),
				},
				Check: func(_ *terraform.State) error {
					if n := len(f.find("DELETE", photos)); n != 1 {
						return fmt.Errorf("want 1 DELETE for the old container, got %d", n)
					}
					var err error
					f.with(func(f *fakeObjectStorage) {
						if _, ok := f.containers["photos"]; ok {
							err = fmt.Errorf("the old container remains")
						}
					})
					return err
				},
			},
			// 外で消されたコンテナは作り直す
			{
				PreConfig: func() { f.with(func(f *fakeObjectStorage) { delete(f.containers, "pictures") }) },
				Config:    containerConfig(f, "pictures", `container_read = ".r:*"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(containerAddr, plancheck.ResourceActionCreate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(containerAddr, tfjsonpath.New("container_read"), knownvalue.StringExact(".r:*")),
				},
			},
		},
	})
}

// 作成時の設定は、バージョニングを作成（PUT）と同時に、残りを続く POST で送る.
// 保存先のコンテナ名は OpenAPI 仕様のとおり URL エンコードして送り、読むときはデコードする.
func TestObjectStorageContainer_CreateWithSettings(t *testing.T) {
	f := newFakeObjectStorage(t)
	const docs = objectStorageBase + "/docs"

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		Steps: []resource.TestStep{{
			Config: f.ProviderConfig() + `
resource "conohavps_objectstorage_container" "archive" {
  name = "古い 版"
}

resource "conohavps_objectstorage_container" "test" {
  name              = "docs"
  versions_location = conohavps_objectstorage_container.archive.name
  web_listings      = false
  metadata          = { owner = "team-b" }
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(containerAddr, tfjsonpath.New("versions_location"), knownvalue.StringExact("古い 版")),
				statecheck.ExpectKnownValue(containerAddr, tfjsonpath.New("web_listings"), knownvalue.Bool(false)),
				statecheck.ExpectKnownValue(containerAddr, tfjsonpath.New("container_read"), knownvalue.Null()),
			},
			Check: resource.ComposeAggregateTestCheckFunc(
				f.expectHeaders("PUT", docs, map[string]string{"X-Versions-Location": "%E5%8F%A4%E3%81%84%20%E7%89%88"}),
				f.expectHeaders("POST", docs, map[string]string{
					"X-Container-Meta-Web-Listings": "false",
					"X-Container-Meta-Owner":        "team-b",
				}),
				func(_ *terraform.State) error {
					found := f.find("POST", docs)
					if len(found) != 1 {
						return fmt.Errorf("want 1 POST, got %d", len(found))
					}
					// 作成時に送ったバージョニングと、設定していない ACL のヘッダーは POST で送らない
					for _, key := range []string{"X-Versions-Location", "X-Container-Read", "X-Remove-Container-Read"} {
						if err := f.expectNoHeader("POST", docs, key); err != nil {
							return err
						}
					}
					return nil
				},
			),
		}},
	})
}

func TestObjectStorageContainer_DeleteNonEmptyFails(t *testing.T) {
	f := newFakeObjectStorage(t)
	config := containerConfig(f, "photos", "")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		Steps: []resource.TestStep{
			{Config: config},
			// オブジェクトが残っていれば、API のエラー（409）を返して削除しない
			{
				PreConfig: func() {
					f.with(func(f *fakeObjectStorage) { f.containers["photos"].objects, f.containers["photos"].bytes = 1, 10 })
				},
				Config:      config,
				Destroy:     true,
				ExpectError: regexp.MustCompile(`(?s)still holds objects.*409`),
			},
			// オブジェクトを消せば削除できる
			{
				PreConfig: func() {
					f.with(func(f *fakeObjectStorage) { f.containers["photos"].objects, f.containers["photos"].bytes = 0, 0 })
				},
				Config: config,
			},
		},
	})
}

func TestObjectStorageContainer_Errors(t *testing.T) {
	f := newFakeObjectStorage(t)
	f.with(func(f *fakeObjectStorage) {
		f.containers["taken"] = &fakeContainer{meta: map[string]string{}}
		f.containers["racing"] = &fakeContainer{meta: map[string]string{}}
		f.hidden["racing"] = true
	})
	container := func(body string) string {
		return f.ProviderConfig() + `resource "conohavps_objectstorage_container" "x" {` + "\n" + body + "\n}"
	}

	steps := []resource.TestStep{
		// 既存のコンテナを黙って管理下に置かない
		{Config: container(`name = "taken"`), ExpectError: regexp.MustCompile(`already exists; import it`)},
		// 存在を確かめた後に作られていた（PUT が 202 を返した）場合も同じ
		{Config: container(`name = "racing"`), ExpectError: regexp.MustCompile(`created by someone else`)},
	}
	for _, c := range []struct{ body, want string }{
		{`name = "a/b"`, `must not contain a slash`},
		{"name = \"a\"\ncontainer_read = \".r:*, .rlistings\"", `must not be empty or contain spaces`},
		{"name = \"a\"\ncontainer_read = \".referrer:*\"", `Write referrer grants as ".r:"`},
		{"name = \"a\"\ncontainer_write = \"\"", `must not be empty or contain spaces`},
		{"name = \"a\"\nweb_index = \" index.html\"", `must not be empty, start or end with a space`},
		{"name = \"a\"\nweb_error = \"\"", `must not be empty, start or end with a space`},
		{"name = \"a\"\nmetadata = {}", `map must contain at least 1 elements`},
		{"name = \"a\"\nmetadata = { Owner = \"x\" }", `must be lowercase letters, digits`},
		{"name = \"a\"\nmetadata = { owner_name = \"x\" }", `must be lowercase letters, digits`},
		{"name = \"a\"\nmetadata = { \"web-index\" = \"x\" }", `value must be none of`},
		{"name = \"a\"\nmetadata = { \"temp-url-key\" = \"x\" }", `value must be none of`},
		{"name = \"a\"\nmetadata = { owner = \"\" }", `must not be empty, start or end with a space`},
	} {
		steps = append(steps, resource.TestStep{Config: container(c.body), ExpectError: regexp.MustCompile(regexp.QuoteMeta(c.want))})
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		Steps:                    steps,
	})

	// 検証で弾いた設定は API に届かない
	if n := len(f.find("PUT", objectStorageBase+"/a")); n != 0 {
		t.Errorf("an invalid container reached the API (%d PUTs)", n)
	}
	// 存在を確かめられた既存のコンテナには、送ったヘッダーで書き換えないよう PUT しない
	if n := len(f.find("PUT", objectStorageBase+"/taken")); n != 0 {
		t.Errorf("the existing container was sent a PUT (%d)", n)
	}
}
