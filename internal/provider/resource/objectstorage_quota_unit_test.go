// オブジェクトストレージの契約容量のリソースの単体テストを提供する.
// 偽の API を相手に、設定・変更・インポート・削除（0GB に戻す）と、100GB 単位の検証、
// 使用量を下回る変更で API のエラーが返ることを検証する.

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

const objectStorageQuotaAddr = "conohavps_objectstorage_quota.test"

func objectStorageQuotaConfig(f *fakeObjectStorage, gb int) string {
	return f.ProviderConfig() + fmt.Sprintf(`
resource "conohavps_objectstorage_quota" "test" {
  quota_gb = %d
}
`, gb)
}

func TestObjectStorageQuota_Lifecycle(t *testing.T) {
	f := newFakeObjectStorage(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		// 削除は契約容量を 0GB に戻す
		CheckDestroy: func(_ *terraform.State) error {
			if err := f.expectHeader("POST", objectStorageBase, "X-Account-Meta-Quota-Giga-Bytes", "0"); err != nil {
				return err
			}
			var err error
			f.with(func(f *fakeObjectStorage) {
				if f.quotaBytes != 0 {
					err = fmt.Errorf("quota is %d bytes after destroy, want 0", f.quotaBytes)
				}
			})
			return err
		},
		Steps: []resource.TestStep{
			{
				Config: objectStorageQuotaConfig(f, 100),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(objectStorageQuotaAddr, tfjsonpath.New("id"), knownvalue.StringExact(fakeapi.TenantID)),
					statecheck.ExpectKnownValue(objectStorageQuotaAddr, tfjsonpath.New("quota_gb"), knownvalue.Int64Exact(100)),
					statecheck.ExpectKnownValue(objectStorageQuotaAddr, tfjsonpath.New("bytes_used"), knownvalue.Int64Exact(0)),
					statecheck.ExpectKnownValue(objectStorageQuotaAddr, tfjsonpath.New("container_count"), knownvalue.Int64Exact(0)),
				},
				Check: func(_ *terraform.State) error {
					return f.expectHeader("POST", objectStorageBase, "X-Account-Meta-Quota-Giga-Bytes", "100")
				},
			},
			// 変更はその場で行う
			{
				Config: objectStorageQuotaConfig(f, 300),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(objectStorageQuotaAddr, plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(objectStorageQuotaAddr, tfjsonpath.New("quota_gb"), knownvalue.Int64Exact(300)),
				},
				Check: func(_ *terraform.State) error {
					return f.expectHeader("POST", objectStorageBase, "X-Account-Meta-Quota-Giga-Bytes", "300")
				},
			},
			{
				ResourceName:      objectStorageQuotaAddr,
				ImportState:       true,
				ImportStateId:     fakeapi.TenantID,
				ImportStateVerify: true,
			},
			{
				ResourceName:  objectStorageQuotaAddr,
				ImportState:   true,
				ImportStateId: "other-tenant",
				ExpectError:   regexp.MustCompile(`imported by the tenant ID of the provider`),
			},
			// 使用量を下回る縮小は API が拒否し、そのエラーを返す（State は変わらない）
			{
				PreConfig: func() {
					f.with(func(f *fakeObjectStorage) { f.usedBytes = 250 * gib })
				},
				Config:      objectStorageQuotaConfig(f, 200),
				ExpectError: regexp.MustCompile(`(?s)quota cannot be less\s+than the usage`),
			},
			{
				Config: objectStorageQuotaConfig(f, 300),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(objectStorageQuotaAddr, tfjsonpath.New("bytes_used"), knownvalue.Int64Exact(250*gib)),
				},
			},
			// 使用量が残っていれば削除（0GB に戻す）も API が拒否する
			{
				Config:      objectStorageQuotaConfig(f, 300),
				Destroy:     true,
				ExpectError: regexp.MustCompile(`(?s)back to 0\s+GB.*quota cannot be less\s+than the usage`),
			},
			// 使用量を消すと、最後の削除が通る
			{
				PreConfig: func() {
					f.with(func(f *fakeObjectStorage) { f.usedBytes = 0 })
				},
				Config: objectStorageQuotaConfig(f, 300),
			},
		},
	})
}

// 外で 0GB に戻された契約容量は、無いものとして作り直す.
func TestObjectStorageQuota_ResetOutside(t *testing.T) {
	f := newFakeObjectStorage(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		Steps: []resource.TestStep{
			{Config: objectStorageQuotaConfig(f, 100)},
			{
				PreConfig: func() { f.with(func(f *fakeObjectStorage) { f.quotaBytes = 0 }) },
				Config:    objectStorageQuotaConfig(f, 100),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(objectStorageQuotaAddr, plancheck.ResourceActionCreate),
					},
				},
			},
		},
	})
}

// 100GB 単位でない容量と 100GB 未満は、API を呼ぶ前に弾く.
func TestObjectStorageQuota_InvalidSize(t *testing.T) {
	f := newFakeObjectStorage(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		Steps: []resource.TestStep{
			{
				Config:      objectStorageQuotaConfig(f, 150),
				ExpectError: regexp.MustCompile(`must be a multiple of 100 and at least 100, got: 150`),
			},
			{
				Config:      objectStorageQuotaConfig(f, 0),
				ExpectError: regexp.MustCompile(`must be a multiple of 100 and at least 100, got: 0`),
			},
			{
				Config: objectStorageQuotaConfig(f, 100),
				PreConfig: func() {
					if n := len(f.find("POST", objectStorageBase)); n != 0 {
						t.Errorf("an invalid quota reached the API (%d POSTs)", n)
					}
				},
			},
		},
	})
}
