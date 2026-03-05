// セキュリティグループのリソースの受け入れテストを提供する.
// 実際にセキュリティグループのリソースを作成し、作成、更新、削除、インポートの動作を検証する.

package resource_test

import (
	"fmt"
	"testing"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

var securityGroupPrefix = acctest.GetTestPrefix("securitygroup")

func init() {
	resource.AddTestSweepers("conohavps_securitygroup", &resource.Sweeper{
		Name: "conohavps_securitygroup",
		F:    acctest.TestSweepResources(securityGroupPrefix),
	})
}

// 必須のパラメータのみ.
func TestAccSecurityGroup_Basic(t *testing.T) {
	resourceAddress := "conohavps_securitygroup.group_1"

	name := fmt.Sprintf("%s-sg-group", securityGroupPrefix)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.TestAccPreCheck(t) },
		ProtoV6ProviderFactories: acctest.TestAccProtoV6ProviderFactories,
		CheckDestroy:             acctest.TestAccCheckResourceDestroy(t),
		Steps: []resource.TestStep{
			// 作成
			{
				Config: testAccSecurityGroupConfig_Basic(name),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectUnknownValue(
							resourceAddress,
							tfjsonpath.New("id"),
						),
						plancheck.ExpectKnownValue(
							resourceAddress,
							tfjsonpath.New("name"),
							knownvalue.StringExact(name),
						),
						plancheck.ExpectResourceAction(resourceAddress, plancheck.ResourceActionCreate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(
						resourceAddress,
						tfjsonpath.New("name"),
						knownvalue.StringExact(name),
					),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(resourceAddress, "id"),
					resource.TestCheckResourceAttrSet(resourceAddress, "name"),
				),
			},
			// インポート
			{
				ResourceName:      resourceAddress,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// 任意のパラメータを指定（description）.
func TestAccSecurityGroup_SetDescription(t *testing.T) {
	resourceAddress := "conohavps_securitygroup.group_1"

	description := "accceptance test"

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			acctest.TestAccPreCheck(t)
			acctest.TestAccSleep(30)
		},
		ProtoV6ProviderFactories: acctest.TestAccProtoV6ProviderFactories,
		CheckDestroy:             acctest.TestAccCheckResourceDestroy(t),
		Steps: []resource.TestStep{
			// 作成
			{
				Config: testAccSecurityGroupConfig_SetDescription(description),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectKnownValue(
							resourceAddress,
							tfjsonpath.New("description"),
							knownvalue.StringExact(description),
						),
						plancheck.ExpectResourceAction(resourceAddress, plancheck.ResourceActionCreate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(
						resourceAddress,
						tfjsonpath.New("description"),
						knownvalue.StringExact(description),
					),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(resourceAddress, "id"),
					resource.TestCheckResourceAttrSet(resourceAddress, "name"),
					resource.TestCheckResourceAttrSet(resourceAddress, "description"),
				),
			},
		},
	})
}

// パラメータの更新（name、description）.
func TestAccSecurityGroup_Update(t *testing.T) {
	resourceAddress := "conohavps_securitygroup.group_1"

	name := fmt.Sprintf("%s-sg-group", acctest.TestAccResourcePrefix)
	description := "acceptance test"

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			acctest.TestAccPreCheck(t)
			acctest.TestAccSleep(30)
		},
		ProtoV6ProviderFactories: acctest.TestAccProtoV6ProviderFactories,
		CheckDestroy:             acctest.TestAccCheckResourceDestroy(t),
		Steps: []resource.TestStep{
			// 作成
			{
				Config: testAccSecurityGroupConfig_Basic(name),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectKnownValue(
							resourceAddress,
							tfjsonpath.New("name"),
							knownvalue.StringExact(name),
						),
						plancheck.ExpectResourceAction(resourceAddress, plancheck.ResourceActionCreate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(
						resourceAddress,
						tfjsonpath.New("name"),
						knownvalue.StringExact(name),
					),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(resourceAddress, "id"),
					resource.TestCheckResourceAttrSet(resourceAddress, "name"),
				),
			},
			//　更新（description）
			{
				Config: testAccSecurityGroupConfig_Update(name, description),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectKnownValue(
							resourceAddress,
							tfjsonpath.New("name"),
							knownvalue.StringExact(name),
						),
						plancheck.ExpectKnownValue(
							resourceAddress,
							tfjsonpath.New("description"),
							knownvalue.StringExact(description),
						),
						plancheck.ExpectNonEmptyPlan(),
						plancheck.ExpectResourceAction(resourceAddress, plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(
						resourceAddress,
						tfjsonpath.New("name"),
						knownvalue.StringExact(name),
					),
					statecheck.ExpectKnownValue(
						resourceAddress,
						tfjsonpath.New("description"),
						knownvalue.StringExact(description),
					),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(resourceAddress, "id"),
					resource.TestCheckResourceAttrSet(resourceAddress, "name"),
					resource.TestCheckResourceAttrSet(resourceAddress, "description"),
				),
			},
			// 更新（name）
			{
				Config: testAccSecurityGroupConfig_Update(fmt.Sprintf("%s_1", name), description),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectKnownValue(
							resourceAddress,
							tfjsonpath.New("name"),
							knownvalue.StringExact(fmt.Sprintf("%s_1", name)),
						),
						plancheck.ExpectKnownValue(
							resourceAddress,
							tfjsonpath.New("description"),
							knownvalue.StringExact(description),
						),
						plancheck.ExpectNonEmptyPlan(),
						plancheck.ExpectResourceAction(resourceAddress, plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(
						resourceAddress,
						tfjsonpath.New("name"),
						knownvalue.StringExact(fmt.Sprintf("%s_1", name)),
					),
					statecheck.ExpectKnownValue(
						resourceAddress,
						tfjsonpath.New("description"),
						knownvalue.StringExact(description),
					),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(resourceAddress, "id"),
					resource.TestCheckResourceAttrSet(resourceAddress, "name"),
					resource.TestCheckResourceAttrSet(resourceAddress, "description"),
				),
			},
			// 更新（name、description）
			{
				Config: testAccSecurityGroupConfig_Update(fmt.Sprintf("%s_2", name), fmt.Sprintf("%s #2", description)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectKnownValue(
							resourceAddress,
							tfjsonpath.New("name"),
							knownvalue.StringExact(fmt.Sprintf("%s_2", name)),
						),
						plancheck.ExpectKnownValue(
							resourceAddress,
							tfjsonpath.New("description"),
							knownvalue.StringExact(fmt.Sprintf("%s #2", description)),
						),
						plancheck.ExpectNonEmptyPlan(),
						plancheck.ExpectResourceAction(resourceAddress, plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(
						resourceAddress,
						tfjsonpath.New("name"),
						knownvalue.StringExact(fmt.Sprintf("%s_2", name)),
					),
					statecheck.ExpectKnownValue(
						resourceAddress,
						tfjsonpath.New("description"),
						knownvalue.StringExact(fmt.Sprintf("%s #2", description)),
					),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(resourceAddress, "id"),
					resource.TestCheckResourceAttrSet(resourceAddress, "name"),
					resource.TestCheckResourceAttrSet(resourceAddress, "description"),
				),
			},
		},
	})
}

// パラメータの差分無し.
func TestAccSecurityGroup_EmptyPlan(t *testing.T) {
	resourceAddress := "conohavps_securitygroup.group_1"

	name := fmt.Sprintf("%s-sg-group", acctest.TestAccResourcePrefix)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			acctest.TestAccPreCheck(t)
			acctest.TestAccSleep(30)
		},
		ProtoV6ProviderFactories: acctest.TestAccProtoV6ProviderFactories,
		CheckDestroy:             acctest.TestAccCheckResourceDestroy(t),
		Steps: []resource.TestStep{
			// 作成
			{
				Config: testAccSecurityGroupConfig_Basic(name),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectKnownValue(
							resourceAddress,
							tfjsonpath.New("name"),
							knownvalue.StringExact(name),
						),
						plancheck.ExpectResourceAction(resourceAddress, plancheck.ResourceActionCreate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(
						resourceAddress,
						tfjsonpath.New("name"),
						knownvalue.StringExact(name),
					),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(resourceAddress, "id"),
					resource.TestCheckResourceAttrSet(resourceAddress, "name"),
				),
			},
			// 差分無し
			{
				Config: testAccSecurityGroupConfig_Basic(name),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectEmptyPlan(),
						plancheck.ExpectResourceAction(resourceAddress, plancheck.ResourceActionNoop),
					},
				},
			},
		},
	})
}

func testAccSecurityGroupConfig_Basic(name string) string {
	return fmt.Sprintf(`
resource "conohavps_securitygroup" "group_1" {
	name = "%s"
}
`, name)
}

func testAccSecurityGroupConfig_SetDescription(description string) string {
	return fmt.Sprintf(`
resource "conohavps_securitygroup" "group_1" {
	name = "%s-sg-group"
	description = "%s"
}
`, securityGroupPrefix, description)
}

func testAccSecurityGroupConfig_Update(name, description string) string {
	return fmt.Sprintf(`
resource "conohavps_securitygroup" "group_1" {
	name = "%s"
	description = "%s"
}
`, name, description)
}
