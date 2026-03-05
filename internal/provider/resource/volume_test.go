// ボリュームのリソースの受け入れテストを提供する.
// 実際にボリュームのリソースを作成し、作成、更新、削除、インポートの動作を検証する.

package resource_test

import (
	"fmt"
	"testing"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/acctest"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

var volumePrefix = acctest.GetTestPrefix("volume")

func init() {
	resource.AddTestSweepers("conohavps_volume", &resource.Sweeper{
		Name: "conohavps_volume",
		F:    acctest.TestSweepResources(volumePrefix),
	})
}

// 最小限のパラメータのみ.
func TestAccVolumeResource_Minimal(t *testing.T) {

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			acctest.TestAccPreCheck(t)
			acctest.TestAccSleep(30)
		},
		ProtoV6ProviderFactories: acctest.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckVolumeDestroy(t),
		Steps: []resource.TestStep{
			{
				Config: testAccVolumeResourceMinimalConfig(fmt.Sprintf("%s-minimal-volume", volumePrefix)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("conohavps_volume.test", plancheck.ResourceActionCreate),
						// Computed な attribute が未知の値になっているか
						plancheck.ExpectUnknownValue("conohavps_volume.test", tfjsonpath.New("id")),
						plancheck.ExpectUnknownValue("conohavps_volume.test", tfjsonpath.New("status")),
						plancheck.ExpectUnknownValue("conohavps_volume.test", tfjsonpath.New("bootable")),
						plancheck.ExpectUnknownValue("conohavps_volume.test", tfjsonpath.New("description")),
						plancheck.ExpectUnknownValue("conohavps_volume.test", tfjsonpath.New("volume_type")),
						// 設定値が反映されているか
						plancheck.ExpectKnownValue("conohavps_volume.test", tfjsonpath.New("name"), knownvalue.StringExact(fmt.Sprintf("%s-minimal-volume", volumePrefix))),
						plancheck.ExpectKnownValue("conohavps_volume.test", tfjsonpath.New("size"), knownvalue.Int64Exact(200)),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					// 設定値とステートファイル値に差異がないか
					statecheck.ExpectKnownValue("conohavps_volume.test", tfjsonpath.New("name"), knownvalue.StringExact(fmt.Sprintf("%s-minimal-volume", volumePrefix))),
					statecheck.ExpectKnownValue("conohavps_volume.test", tfjsonpath.New("size"), knownvalue.Int64Exact(200)),
					statecheck.ExpectKnownValue("conohavps_volume.test", tfjsonpath.New("status"), knownvalue.StringExact("available")),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("conohavps_volume.test", "id"),
					resource.TestCheckResourceAttrSet("conohavps_volume.test", "status"),
					resource.TestCheckResourceAttrSet("conohavps_volume.test", "bootable"),
					resource.TestCheckResourceAttrSet("conohavps_volume.test", "volume_type"),
				),
			},
			// empty plan のテスト
			{
				Config: testAccVolumeResourceMinimalConfig(fmt.Sprintf("%s-minimal-volume", volumePrefix)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectEmptyPlan(),
					},
				},
			},
			// import テスト
			{
				ResourceName:      "conohavps_volume.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// description パラメータのテスト.
func TestAccVolumeResource_Description(t *testing.T) {

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			acctest.TestAccPreCheck(t)
			acctest.TestAccSleep(30)
		},
		ProtoV6ProviderFactories: acctest.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckVolumeDestroy(t),
		Steps: []resource.TestStep{
			{
				Config: testAccVolumeResourceDescriptionConfig(fmt.Sprintf("%s-desc-volume", volumePrefix), "test description"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("conohavps_volume.test", plancheck.ResourceActionCreate),
						// Computed な attribute が未知の値になっているか
						plancheck.ExpectUnknownValue("conohavps_volume.test", tfjsonpath.New("id")),
						plancheck.ExpectUnknownValue("conohavps_volume.test", tfjsonpath.New("status")),
						plancheck.ExpectUnknownValue("conohavps_volume.test", tfjsonpath.New("bootable")),
						plancheck.ExpectUnknownValue("conohavps_volume.test", tfjsonpath.New("volume_type")),
						// 設定値が反映されているか
						plancheck.ExpectKnownValue("conohavps_volume.test", tfjsonpath.New("description"), knownvalue.StringExact("test description")),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					// 設定値とステートファイル値に差異がないか
					statecheck.ExpectKnownValue("conohavps_volume.test", tfjsonpath.New("description"), knownvalue.StringExact("test description")),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("conohavps_volume.test", "id"),
					resource.TestCheckResourceAttrSet("conohavps_volume.test", "status"),
				),
			},
		},
	})
}

// volume_type パラメータのテスト.
func TestAccVolumeResource_VolumeType(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			acctest.TestAccPreCheck(t)
			acctest.TestAccSleep(30)
		},
		ProtoV6ProviderFactories: acctest.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckVolumeDestroy(t),
		Steps: []resource.TestStep{
			{
				Config: testAccVolumeResourceVolumeTypeConfig(fmt.Sprintf("%s-type-volume", volumePrefix), "c3j1-ds02-add"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("conohavps_volume.test", plancheck.ResourceActionCreate),
						// Computed な attribute が未知の値になっているか
						plancheck.ExpectUnknownValue("conohavps_volume.test", tfjsonpath.New("id")),
						plancheck.ExpectUnknownValue("conohavps_volume.test", tfjsonpath.New("status")),
						plancheck.ExpectUnknownValue("conohavps_volume.test", tfjsonpath.New("bootable")),
						plancheck.ExpectUnknownValue("conohavps_volume.test", tfjsonpath.New("description")),
						// 設定値が反映されているか
						plancheck.ExpectKnownValue("conohavps_volume.test", tfjsonpath.New("volume_type"), knownvalue.StringExact("c3j1-ds02-add")),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					// 設定値とステートファイル値に差異がないか
					statecheck.ExpectKnownValue("conohavps_volume.test", tfjsonpath.New("volume_type"), knownvalue.StringExact("c3j1-ds02-add")),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("conohavps_volume.test", "id"),
					resource.TestCheckResourceAttrSet("conohavps_volume.test", "status"),
				),
			},
		},
	})
}

// image_ref パラメータのテスト.
func TestAccVolumeResource_ImageRef(t *testing.T) {

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			acctest.TestAccPreCheck(t)
			acctest.TestAccSleep(30)
		},
		ProtoV6ProviderFactories: acctest.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckVolumeDestroy(t),
		Steps: []resource.TestStep{
			{
				Config: testAccVolumeResourceImageRefConfig(fmt.Sprintf("%s-imageref-volume", volumePrefix), "31e6049e-4f4f-4f65-98b9-c08c33cb8624"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("conohavps_volume.test", plancheck.ResourceActionCreate),
						// Computed な attribute が未知の値になっているか
						plancheck.ExpectUnknownValue("conohavps_volume.test", tfjsonpath.New("id")),
						plancheck.ExpectUnknownValue("conohavps_volume.test", tfjsonpath.New("status")),
						plancheck.ExpectUnknownValue("conohavps_volume.test", tfjsonpath.New("bootable")),
						plancheck.ExpectUnknownValue("conohavps_volume.test", tfjsonpath.New("description")),
						// 設定値が反映されているか
						plancheck.ExpectKnownValue("conohavps_volume.test", tfjsonpath.New("image_ref"), knownvalue.StringExact("31e6049e-4f4f-4f65-98b9-c08c33cb8624")),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					// 設定値とステートファイル値に差異がないか
					statecheck.ExpectKnownValue("conohavps_volume.test", tfjsonpath.New("image_ref"), knownvalue.StringExact("31e6049e-4f4f-4f65-98b9-c08c33cb8624")),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("conohavps_volume.test", "id"),
					resource.TestCheckResourceAttrSet("conohavps_volume.test", "status"),
				),
			},
		},
	})
}

// source_volid パラメータのテスト.
func TestAccVolumeResource_SourceVolid(t *testing.T) {

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			acctest.TestAccPreCheck(t)
			acctest.TestAccSleep(30)
		},
		ProtoV6ProviderFactories: acctest.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckVolumeDestroy(t),
		Steps: []resource.TestStep{
			{
				Config: testAccVolumeResourceSourceVolidConfig(fmt.Sprintf("%s-source-volume", volumePrefix), fmt.Sprintf("%s-cloned-volume", volumePrefix)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						// 複製元ボリュームの作成
						plancheck.ExpectResourceAction("conohavps_volume.source", plancheck.ResourceActionCreate),
						// 複製先ボリュームの作成
						plancheck.ExpectResourceAction("conohavps_volume.test", plancheck.ResourceActionCreate),
						// Computed な attribute が未知の値になっているか
						plancheck.ExpectUnknownValue("conohavps_volume.test", tfjsonpath.New("id")),
						plancheck.ExpectUnknownValue("conohavps_volume.test", tfjsonpath.New("status")),
						plancheck.ExpectUnknownValue("conohavps_volume.test", tfjsonpath.New("bootable")),
						plancheck.ExpectUnknownValue("conohavps_volume.test", tfjsonpath.New("description")),
						// source_volid は複製元のIDを参照しているため unknown
						plancheck.ExpectUnknownValue("conohavps_volume.test", tfjsonpath.New("source_volid")),
						// 設定値が反映されているか
						plancheck.ExpectKnownValue("conohavps_volume.test", tfjsonpath.New("name"), knownvalue.StringExact(fmt.Sprintf("%s-cloned-volume", volumePrefix))),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					// 設定値とステートファイル値に差異がないか
					statecheck.ExpectKnownValue("conohavps_volume.test", tfjsonpath.New("name"), knownvalue.StringExact(fmt.Sprintf("%s-cloned-volume", volumePrefix))),
					statecheck.ExpectKnownValue("conohavps_volume.test", tfjsonpath.New("size"), knownvalue.Int64Exact(200)),
					statecheck.ExpectKnownValue("conohavps_volume.test", tfjsonpath.New("volume_type"), knownvalue.StringExact("c3j1-ds02-add")),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("conohavps_volume.test", "id"),
					resource.TestCheckResourceAttrSet("conohavps_volume.test", "status"),
					resource.TestCheckResourceAttrSet("conohavps_volume.test", "source_volid"),
					// source_volid が source ボリュームの ID と一致することを確認
					resource.TestCheckResourceAttrPair(
						"conohavps_volume.test", "source_volid",
						"conohavps_volume.source", "id",
					),
				),
			},
		},
	})
}

// TestAccVolumeResource_BackupId - backup_id パラメータのテスト
//
// 【未実装】
// backup_id を使用したボリューム作成テストは、以下の理由により手動テスト（examples/resources/volume/main.tf）で実施する
//
// 理由:
//   - backup_id は週次の自動バックアップにより定期的に変更されるため、テスト実行のたびに手動で ID を更新する運用が発生する
//
// 代替案（将来の改善候補）:
//   - テスト実行時に Backup API を呼び出して最新の backup_id を動的に取得する
//   - 環境変数 (例: CONOHAVPS_TEST_BACKUP_ID) から backup_id を読み込む

func TestAccVolumeResource_Update(t *testing.T) {

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			acctest.TestAccPreCheck(t)
			acctest.TestAccSleep(30)
		},
		ProtoV6ProviderFactories: acctest.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckVolumeDestroy(t),
		Steps: []resource.TestStep{
			{
				Config: testAccVolumeResourceMinimalConfig(fmt.Sprintf("%s-update-volume", volumePrefix)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("conohavps_volume.test", plancheck.ResourceActionCreate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_volume.test", tfjsonpath.New("name"), knownvalue.StringExact(fmt.Sprintf("%s-update-volume", volumePrefix))),
				},
			},
			{
				Config: testAccVolumeResourceDescriptionConfig(fmt.Sprintf("%s-update-volume-renamed", volumePrefix), "updated description"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("conohavps_volume.test", plancheck.ResourceActionUpdate),
						// 設定値が反映されているか
						plancheck.ExpectKnownValue("conohavps_volume.test", tfjsonpath.New("name"), knownvalue.StringExact(fmt.Sprintf("%s-update-volume-renamed", volumePrefix))),
						plancheck.ExpectKnownValue("conohavps_volume.test", tfjsonpath.New("description"), knownvalue.StringExact("updated description")),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					// 設定値とステートファイル値に差異がないか
					statecheck.ExpectKnownValue("conohavps_volume.test", tfjsonpath.New("name"), knownvalue.StringExact(fmt.Sprintf("%s-update-volume-renamed", volumePrefix))),
					statecheck.ExpectKnownValue("conohavps_volume.test", tfjsonpath.New("description"), knownvalue.StringExact("updated description")),
				},
			},
			// empty plan のテスト（更新後）
			{
				Config: testAccVolumeResourceDescriptionConfig(fmt.Sprintf("%s-update-volume-renamed", volumePrefix), "updated description"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectEmptyPlan(),
					},
				},
			},
		},
	})
}

// name パラメータの更新テスト.
func TestAccVolumeResource_UpdateName(t *testing.T) {

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			acctest.TestAccPreCheck(t)
			acctest.TestAccSleep(30)
		},
		ProtoV6ProviderFactories: acctest.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckVolumeDestroy(t),
		Steps: []resource.TestStep{
			{
				Config: testAccVolumeResourceMinimalConfig(fmt.Sprintf("%s-name-update-volume", volumePrefix)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("conohavps_volume.test", plancheck.ResourceActionCreate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_volume.test", tfjsonpath.New("name"), knownvalue.StringExact(fmt.Sprintf("%s-name-update-volume", volumePrefix))),
				},
			},
			{
				Config: testAccVolumeResourceMinimalConfig(fmt.Sprintf("%s-name-update-volume-renamed", volumePrefix)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("conohavps_volume.test", plancheck.ResourceActionUpdate),
						// 設定値が反映されているか
						plancheck.ExpectKnownValue("conohavps_volume.test", tfjsonpath.New("name"), knownvalue.StringExact(fmt.Sprintf("%s-name-update-volume-renamed", volumePrefix))),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					// 設定値とステートファイル値に差異がないか
					statecheck.ExpectKnownValue("conohavps_volume.test", tfjsonpath.New("name"), knownvalue.StringExact(fmt.Sprintf("%s-name-update-volume-renamed", volumePrefix))),
				},
			},
			// empty plan のテスト（更新後）
			{
				Config: testAccVolumeResourceMinimalConfig(fmt.Sprintf("%s-name-update-volume-renamed", volumePrefix)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectEmptyPlan(),
					},
				},
			},
		},
	})
}

// description パラメータの更新テスト.
func TestAccVolumeResource_UpdateDescription(t *testing.T) {

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			acctest.TestAccPreCheck(t)
			acctest.TestAccSleep(30)
		},
		ProtoV6ProviderFactories: acctest.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckVolumeDestroy(t),
		Steps: []resource.TestStep{
			{
				Config: testAccVolumeResourceDescriptionConfig(fmt.Sprintf("%s-desc-update-volume", volumePrefix), "initial description"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("conohavps_volume.test", plancheck.ResourceActionCreate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_volume.test", tfjsonpath.New("description"), knownvalue.StringExact("initial description")),
				},
			},
			{
				Config: testAccVolumeResourceDescriptionConfig(fmt.Sprintf("%s-desc-update-volume", volumePrefix), "updated description"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("conohavps_volume.test", plancheck.ResourceActionUpdate),
						// 設定値が反映されているか
						plancheck.ExpectKnownValue("conohavps_volume.test", tfjsonpath.New("description"), knownvalue.StringExact("updated description")),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					// 設定値とステートファイル値に差異がないか
					statecheck.ExpectKnownValue("conohavps_volume.test", tfjsonpath.New("description"), knownvalue.StringExact("updated description")),
				},
			},
			// empty plan のテスト（更新後）
			{
				Config: testAccVolumeResourceDescriptionConfig(fmt.Sprintf("%s-desc-update-volume", volumePrefix), "updated description"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectEmptyPlan(),
					},
				},
			},
		},
	})
}

// リプレースのテスト（size パラメータ）.
func TestAccVolumeResource_Replace(t *testing.T) {

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			acctest.TestAccPreCheck(t)
			acctest.TestAccSleep(30)
		},
		ProtoV6ProviderFactories: acctest.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckVolumeDestroy(t),
		Steps: []resource.TestStep{
			{
				Config: testAccVolumeResourceMinimalConfig(fmt.Sprintf("%s-replace-volume", volumePrefix)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("conohavps_volume.test", plancheck.ResourceActionCreate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_volume.test", tfjsonpath.New("size"), knownvalue.Int64Exact(200)),
				},
			},
			{
				Config: testAccVolumeResourceReplaceConfig(fmt.Sprintf("%s-replace-volume", volumePrefix), 500),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("conohavps_volume.test", plancheck.ResourceActionReplace),
						// 設定値が反映されているか
						plancheck.ExpectKnownValue("conohavps_volume.test", tfjsonpath.New("size"), knownvalue.Int64Exact(500)),
						// Computed な attribute が未知の値になっているか（リプレースなので新規作成扱い）
						plancheck.ExpectUnknownValue("conohavps_volume.test", tfjsonpath.New("id")),
						plancheck.ExpectUnknownValue("conohavps_volume.test", tfjsonpath.New("status")),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					// 設定値とステートファイル値に差異がないか
					statecheck.ExpectKnownValue("conohavps_volume.test", tfjsonpath.New("size"), knownvalue.Int64Exact(500)),
				},
			},
			// empty plan のテスト（リプレース後）
			{
				Config: testAccVolumeResourceReplaceConfig(fmt.Sprintf("%s-replace-volume", volumePrefix), 500),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectEmptyPlan(),
					},
				},
			},
		},
	})
}

// リプレースのテスト（image_ref パラメータ）.
func TestAccVolumeResource_ReplaceImageRef(t *testing.T) {

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			acctest.TestAccPreCheck(t)
			acctest.TestAccSleep(30)
		},
		ProtoV6ProviderFactories: acctest.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckVolumeDestroy(t),
		Steps: []resource.TestStep{
			{
				Config: testAccVolumeResourceImageRefConfig(fmt.Sprintf("%s-replace-imageref-volume", volumePrefix), "31e6049e-4f4f-4f65-98b9-c08c33cb8624"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("conohavps_volume.test", plancheck.ResourceActionCreate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_volume.test", tfjsonpath.New("image_ref"), knownvalue.StringExact("31e6049e-4f4f-4f65-98b9-c08c33cb8624")),
				},
			},
			{
				Config: testAccVolumeResourceImageRefConfig(fmt.Sprintf("%s-replace-imageref-volume", volumePrefix), "884c1899-fefe-40bd-aab8-1001b1d9c895"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("conohavps_volume.test", plancheck.ResourceActionReplace),
						// 設定値が反映されているか
						plancheck.ExpectKnownValue("conohavps_volume.test", tfjsonpath.New("image_ref"), knownvalue.StringExact("884c1899-fefe-40bd-aab8-1001b1d9c895")),
						// Computed な attribute が未知の値になっているか（リプレースなので新規作成扱い）
						plancheck.ExpectUnknownValue("conohavps_volume.test", tfjsonpath.New("id")),
						plancheck.ExpectUnknownValue("conohavps_volume.test", tfjsonpath.New("status")),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					// 設定値とステートファイル値に差異がないか
					statecheck.ExpectKnownValue("conohavps_volume.test", tfjsonpath.New("image_ref"), knownvalue.StringExact("884c1899-fefe-40bd-aab8-1001b1d9c895")),
				},
			},
		},
	})
}

// ※ source_volid, backup_id の replaceテストは複雑な実装になるため、未実装です.

// ボリュームの削除のテスト.
func testAccCheckVolumeDestroy(t *testing.T) func(s *terraform.State) error {
	return func(s *terraform.State) error {
		for _, rs := range s.RootModule().Resources {
			if rs.Type != "conohavps_volume" {
				continue
			}

			_, err := volumes.Get(t.Context(), acctest.TestAccClient.BlockStorageClient, rs.Primary.ID).Extract()
			if err == nil {
				return fmt.Errorf("volume %s still exists", rs.Primary.ID)
			}

			if gophercloud.ResponseCodeIs(err, 404) {
				continue
			}

			return err
		}
		return nil
	}
}

func testAccVolumeResourceMinimalConfig(name string) string {
	return fmt.Sprintf(`
resource "conohavps_volume" "test" {
	name = "%s"
	size = 200
}
`, name)
}

func testAccVolumeResourceDescriptionConfig(name, description string) string {
	return fmt.Sprintf(`
resource "conohavps_volume" "test" {
	name        = "%s"
	size        = 200
	description = "%s"
}
`, name, description)
}

func testAccVolumeResourceVolumeTypeConfig(name, volumeType string) string {
	return fmt.Sprintf(`
resource "conohavps_volume" "test" {
	name        = "%s"
	size        = 200
	volume_type = "%s"
}
`, name, volumeType)
}

func testAccVolumeResourceImageRefConfig(name, imageRef string) string {
	return fmt.Sprintf(`
resource "conohavps_volume" "test" {
	name        = "%s"
	size        = 200
	image_ref   = "%s"
}
`, name, imageRef)
}

func testAccVolumeResourceSourceVolidConfig(sourceName, clonedName string) string {
	return fmt.Sprintf(`
# 複製元ボリューム
resource "conohavps_volume" "source" {
	name        = "%s"
	size        = 200
	volume_type = "c3j1-ds02-add"
}

# 複製先ボリューム (Clone)
resource "conohavps_volume" "test" {
	name         = "%s"
	size         = conohavps_volume.source.size
	volume_type  = conohavps_volume.source.volume_type
	source_volid = conohavps_volume.source.id
}
`, sourceName, clonedName)
}

func testAccVolumeResourceReplaceConfig(name string, size int) string {
	return fmt.Sprintf(`
resource "conohavps_volume" "test" {
	name = "%s"
	size = %d
}
`, name, size)
}
