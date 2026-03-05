// キーペアのリソースの受け入れテストを提供する.
// 実際にキーペアのリソースを作成し、作成、更新、削除、インポートの動作を検証する.

package resource_test

import (
	"fmt"
	"testing"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/acctest"
	ac "github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

var keypairPrefix = acctest.GetTestPrefix("keypair")

func init() {
	resource.AddTestSweepers("conohavps_keypair", &resource.Sweeper{
		Name: "conohavps_keypair",
		F:    acctest.TestSweepResources(keypairPrefix),
	})
}

// 最小限のパラメータのみ.
func TestAccKeypairResource_Minimal(t *testing.T) {
	random := ac.RandString(6)
	rName := fmt.Sprintf("%s-minimal-%s", keypairPrefix, random)
	rNameUpdated := fmt.Sprintf("%s-minimal-%s-updated", keypairPrefix, random)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			acctest.TestAccPreCheck(t)
			acctest.TestAccSleep(30)
		},
		ProtoV6ProviderFactories: acctest.TestAccProtoV6ProviderFactories,
		CheckDestroy:             acctest.TestAccCheckResourceDestroy(t),
		Steps: []resource.TestStep{
			{
				Config: testAccKeypairResourceMinimalConfig(rName),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("conohavps_keypair.test", plancheck.ResourceActionCreate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_keypair.test", tfjsonpath.New("name"), knownvalue.StringExact(rName)),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					// Required attributes
					resource.TestCheckResourceAttrSet("conohavps_keypair.test", "name"),
					resource.TestCheckResourceAttrSet("conohavps_keypair.test", "public_key"),
					resource.TestCheckResourceAttrSet("conohavps_keypair.test", "private_key"),
				),
			},
			{
				Config: testAccKeypairResourceMinimalConfig(rName),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectEmptyPlan(),
					},
				},
			},
			{
				Config: testAccKeypairResourceMinimalConfig(rNameUpdated),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("conohavps_keypair.test", plancheck.ResourceActionReplace),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_keypair.test", tfjsonpath.New("name"), knownvalue.StringExact(rNameUpdated)),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					// Required attributes
					resource.TestCheckResourceAttrSet("conohavps_keypair.test", "name"),
					resource.TestCheckResourceAttrSet("conohavps_keypair.test", "public_key"),
					resource.TestCheckResourceAttrSet("conohavps_keypair.test", "private_key"),
				),
			},
			{
				Config: testAccKeypairResourceMinimalConfig(rNameUpdated),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectEmptyPlan(),
					},
				},
			},
			{
				ResourceName:                         "conohavps_keypair.test",
				ImportState:                          true,
				ImportStateId:                        rNameUpdated,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "name",
				ImportStateVerifyIgnore: []string{
					"private_key",
				},
			},
		},
	})
}

// TestAccKeypairResource_WithPublicKey - public_keyパラメータ設定.
func TestAccKeypairResource_WithPublicKey(t *testing.T) {
	rName := "tf-acctest-keypair-with-pubkey"

	// SSH公開鍵のサンプル（2つの異なる鍵）
	publicKey1 := "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDN/oHMQBSSMzlxs4KmBcESyJMlLKNSIpbTxuNV6yxbwr3++s2kNADq+L2ImdQmtrBfTcJGhd5SB4pTQ/s32fa9WVj4gNzZeHxCiQHcxfqNNq7W0EM/Picpu8zlqZNKJ0bBOHvpVOLIWVy7NxnADzHgZnV/Mj0wvqFfdiM12hUnovv3FPRVcqu0lbqnlcc3WrfxmJdV3bSIeUtc+RTn39EqSrHd2gGDccKyNBFxAogENhrED9nPDp49CNoyh/suTYce5BTjdE13VvcC+mwdP3gMtbHUzdGmKPUXjoUmDLXpoekxfWpvh10QkXBIbaTrYaOCzxNj3Ph1Lkqgb7gcwEf7"
	publicKey2 := "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQD2xsbfpHXo6wHDDZD7TDs4XWIEKXeBdhrso9V0by3F5HftYQyLfxPUy4czCaxtSft5WrXUHIpmv4h3DgBtUHSOsEbenSHH39zI3Vc+DigCev6PBDeEisCUbcPkVWQklnRhCQCf5fL/53ENwKn+z1z5uxgolmgupP+QBO2vf85tyl4cKViQb6zOH/GKjtred3E90e84yD/QFjuFkCBewIK7jInPHSxMPzEov/a0PqeIWo6/oa3FfAGUgYCj5YOAkAyQCxwihFmP7ujFrlFf9zG6Xs6UA0neuY/zWcg7tAnxnYglCRi2M8cMVbUPd6f/4ptkCBcaYS3Pvw27RZGYGr9R"

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			acctest.TestAccPreCheck(t)
			acctest.TestAccSleep(30)
		},
		ProtoV6ProviderFactories: acctest.TestAccProtoV6ProviderFactories,
		CheckDestroy:             acctest.TestAccCheckResourceDestroy(t),
		Steps: []resource.TestStep{
			{
				Config: testAccKeypairResourceWithPublicKeyConfig(rName, publicKey1),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("conohavps_keypair.test", plancheck.ResourceActionCreate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_keypair.test", tfjsonpath.New("name"), knownvalue.StringExact(rName)),
					statecheck.ExpectKnownValue("conohavps_keypair.test", tfjsonpath.New("public_key"), knownvalue.StringExact(publicKey1)),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("conohavps_keypair.test", "name"),
					resource.TestCheckResourceAttrSet("conohavps_keypair.test", "public_key"),
				),
			},
			{
				Config: testAccKeypairResourceWithPublicKeyConfig(rName, publicKey1),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectEmptyPlan(),
					},
				},
			},
			{
				Config: testAccKeypairResourceWithPublicKeyConfig(rName, publicKey2),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("conohavps_keypair.test", plancheck.ResourceActionReplace),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_keypair.test", tfjsonpath.New("name"), knownvalue.StringExact(rName)),
					statecheck.ExpectKnownValue("conohavps_keypair.test", tfjsonpath.New("public_key"), knownvalue.StringExact(publicKey2)),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("conohavps_keypair.test", "name"),
					resource.TestCheckResourceAttrSet("conohavps_keypair.test", "public_key"),
				),
			},
		},
	})
}

// ヘルパー関数：最小限設定.
func testAccKeypairResourceMinimalConfig(name string) string {
	return fmt.Sprintf(`
resource "conohavps_keypair" "test" {
	name = "%s"
}
`, name)
}

// ヘルパー関数：public_key設定.
func testAccKeypairResourceWithPublicKeyConfig(name, publicKey string) string {
	return fmt.Sprintf(`
resource "conohavps_keypair" "test" {
	name       = "%s"
	public_key = "%s"
}
`, name, publicKey)
}
