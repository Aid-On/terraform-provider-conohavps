// オブジェクトストレージのコンテナのリソースの単体テストを提供する.
// 偽の API を相手に、作成・設定の変更・再作成・インポート・削除と、ドキュメントどおりのヘッダーが送られることを検証する.

package resource_test

import (
	"fmt"
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

func TestObjectStorageContainer_Lifecycle(t *testing.T) {
	f := newFakeObjectStorage(t)
	const addr = "conohavps_objectstorage_container.test"

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
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.StringExact("photos")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("versions_location"), knownvalue.Null()),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("web_publishing"), knownvalue.Bool(false)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("storage_policy"), knownvalue.StringExact("default-placement")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("object_count"), knownvalue.Int64Exact(0)),
				},
				Check: func(_ *terraform.State) error {
					if n := len(f.find("PUT", objectStorageBase+"/photos")); n != 1 {
						return fmt.Errorf("want 1 PUT for the container, got %d", n)
					}
					// 設定が無いときは POST しない
					if n := len(f.find("POST", objectStorageBase+"/photos")); n != 0 {
						return fmt.Errorf("want no POST for the container, got %d", n)
					}
					return nil
				},
			},
			// バージョニングと Web 公開を設定（その場で更新）
			{
				Config: containerConfig(f, "photos", `
  versions_location = conohavps_objectstorage_container.archive.name
  web_publishing    = true
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("versions_location"), knownvalue.StringExact("archive")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("web_publishing"), knownvalue.Bool(true)),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					func(_ *terraform.State) error {
						return f.expectHeader("POST", objectStorageBase+"/photos", "X-Versions-Location", "archive")
					},
					func(_ *terraform.State) error {
						return f.expectHeader("POST", objectStorageBase+"/photos", "X-Container-Read", ".r:*")
					},
				),
			},
			// インポート
			{
				ResourceName:      addr,
				ImportState:       true,
				ImportStateId:     "photos",
				ImportStateVerify: true,
			},
			// 設定を外すと、ドキュメントどおり値が空のヘッダーで解除する
			{
				Config: containerConfig(f, "photos", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("versions_location"), knownvalue.Null()),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("web_publishing"), knownvalue.Bool(false)),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					func(_ *terraform.State) error {
						return f.expectHeader("POST", objectStorageBase+"/photos", "X-Remove-Versions-Location", "")
					},
					func(_ *terraform.State) error {
						return f.expectHeader("POST", objectStorageBase+"/photos", "X-Container-Read", "")
					},
				),
			},
			// 名前の変更は再作成
			{
				Config: containerConfig(f, "pictures", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(addr, plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.StringExact("pictures")),
				},
				Check: func(_ *terraform.State) error {
					if n := len(f.find("DELETE", objectStorageBase+"/photos")); n != 1 {
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
			// 作成時に設定も渡せる
			{
				Config: containerConfig(f, "pictures", `
  web_publishing = true
`),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("web_publishing"), knownvalue.Bool(true)),
				},
			},
			// 外で消されたコンテナは作り直す
			{
				PreConfig: func() { f.with(func(f *fakeObjectStorage) { delete(f.containers, "pictures") }) },
				Config: containerConfig(f, "pictures", `
  web_publishing = true
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(addr, plancheck.ResourceActionCreate),
					},
				},
			},
		},
	})
}

func TestObjectStorageContainer_CreateWithSettings(t *testing.T) {
	f := newFakeObjectStorage(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		Steps: []resource.TestStep{{
			Config: containerConfig(f, "docs", `
  versions_location = conohavps_objectstorage_container.archive.name
`),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("conohavps_objectstorage_container.test", tfjsonpath.New("versions_location"), knownvalue.StringExact("archive")),
				statecheck.ExpectKnownValue("conohavps_objectstorage_container.test", tfjsonpath.New("web_publishing"), knownvalue.Bool(false)),
			},
			Check: func(_ *terraform.State) error {
				found := f.find("POST", objectStorageBase+"/docs")
				if len(found) != 1 {
					return fmt.Errorf("want 1 POST, got %d", len(found))
				}
				// 変えていない Web 公開のヘッダーは送らない
				if _, ok := found[0].Header["X-Container-Read"]; ok {
					return fmt.Errorf("X-Container-Read was sent although web_publishing is unchanged")
				}
				return f.expectHeader("POST", objectStorageBase+"/docs", "X-Versions-Location", "archive")
			},
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
			// オブジェクトが残っていれば、API のエラーを返して削除しない
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
	f.with(func(f *fakeObjectStorage) { f.containers["taken"] = &fakeContainer{} })

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		Steps: []resource.TestStep{
			// 既存のコンテナを黙って管理下に置かない
			{
				Config:      f.ProviderConfig() + `resource "conohavps_objectstorage_container" "x" { name = "taken" }`,
				ExpectError: regexp.MustCompile(`already exists; import it`),
			},
			{
				Config:      f.ProviderConfig() + `resource "conohavps_objectstorage_container" "x" { name = "a/b" }`,
				ExpectError: regexp.MustCompile(`must not contain a slash`),
			},
		},
	})
}
