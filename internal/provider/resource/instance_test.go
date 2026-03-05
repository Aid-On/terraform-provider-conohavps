// インスタンスのリソースの受け入れテストを提供する.
// 実際にインスタンスのリソースを作成し、作成、更新、削除、インポートの動作を検証する.

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

var instancePrefix = acctest.GetTestPrefix("instance")

func init() {
	resource.AddTestSweepers("conohavps_instance", &resource.Sweeper{
		Name: "conohavps_instance",
		F:    acctest.TestSweepResources(instancePrefix),
	})
}

// TestAccInstanceResource_Minimal - 最小限のパラメータのみ.
func TestAccInstanceResource_Minimal(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.TestAccPreCheck(t) },
		ProtoV6ProviderFactories: acctest.TestAccProtoV6ProviderFactories,
		CheckDestroy:             acctest.TestAccCheckResourceDestroy(t),
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceResourceMinimalConfig(fmt.Sprintf("%s-minimal-instance", instancePrefix)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("conohavps_instance.test", plancheck.ResourceActionCreate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_instance.test", tfjsonpath.New("instance_name_tag"), knownvalue.StringExact(fmt.Sprintf("%s-minimal-instance", instancePrefix))),
					statecheck.ExpectKnownValue("conohavps_instance.test", tfjsonpath.New("flavor_id"), knownvalue.StringExact("f2a77529-1815-43a2-bc14-1f3f6b09079c")),
					statecheck.ExpectKnownValue("conohavps_instance.test", tfjsonpath.New("status"), knownvalue.StringExact("ACTIVE")),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					// Required attributes
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "flavor_id"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "instance_name_tag"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "block_device.#"),
					// Computed attributes
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "id"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "status"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "addresses.%"),
				),
			},
			{
				Config: testAccInstanceResourceMinimalConfig(fmt.Sprintf("%s-minimal-instance", instancePrefix)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectEmptyPlan(),
					},
				},
			},
			{
				ResourceName:      "conohavps_instance.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"admin_pass",
				},
			},
		},
	})
}

// TestAccInstanceResource_WithAdminPass - admin_passパラメータのテスト.
func TestAccInstanceResource_WithAdminPass(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.TestAccPreCheck(t) },
		ProtoV6ProviderFactories: acctest.TestAccProtoV6ProviderFactories,
		CheckDestroy:             acctest.TestAccCheckResourceDestroy(t),
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceResourceWithAdminPassConfig(fmt.Sprintf("%s-adminpass-instance", instancePrefix), "TestPass123!"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_instance.test", tfjsonpath.New("instance_name_tag"), knownvalue.StringExact(fmt.Sprintf("%s-adminpass-instance", instancePrefix))),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					// Required attributes
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "flavor_id"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "instance_name_tag"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "block_device.#"),
					// Computed attributes
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "id"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "status"),
					// Optional attributes (specified)
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "admin_pass"),
				),
			},
			{
				Config: testAccInstanceResourceWithAdminPassConfig(fmt.Sprintf("%s-adminpass-instance", instancePrefix), "NewPass456@"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("conohavps_instance.test", plancheck.ResourceActionReplace),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_instance.test", tfjsonpath.New("instance_name_tag"), knownvalue.StringExact(fmt.Sprintf("%s-adminpass-instance", instancePrefix))),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					// Required attributes
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "flavor_id"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "instance_name_tag"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "block_device.#"),
					// Computed attributes
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "id"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "status"),
					// Optional attributes (specified)
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "admin_pass"),
				),
			},
			{
				Config: testAccInstanceResourceWithAdminPassConfig(fmt.Sprintf("%s-adminpass-instance", instancePrefix), "NewPass456@"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectEmptyPlan(),
					},
				},
			},
		},
	})
}

// TestAccInstanceResource_WithPowerState - power_stateパラメータのテスト.
func TestAccInstanceResource_WithPowerState(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.TestAccPreCheck(t) },
		ProtoV6ProviderFactories: acctest.TestAccProtoV6ProviderFactories,
		CheckDestroy:             acctest.TestAccCheckResourceDestroy(t),
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceResourceWithPowerStateConfig(fmt.Sprintf("%s-power-instance", instancePrefix), "ACTIVE"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_instance.test", tfjsonpath.New("power_state"), knownvalue.StringExact("ACTIVE")),
					statecheck.ExpectKnownValue("conohavps_instance.test", tfjsonpath.New("status"), knownvalue.StringExact("ACTIVE")),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					// Required attributes
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "flavor_id"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "instance_name_tag"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "block_device.#"),
					// Computed attributes
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "id"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "status"),
					// Optional attributes (specified)
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "power_state"),
				),
			},
			{
				Config: testAccInstanceResourceWithPowerStateConfig(fmt.Sprintf("%s-power-instance", instancePrefix), "SHUTOFF"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("conohavps_instance.test", plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_instance.test", tfjsonpath.New("power_state"), knownvalue.StringExact("SHUTOFF")),
					statecheck.ExpectKnownValue("conohavps_instance.test", tfjsonpath.New("status"), knownvalue.StringExact("SHUTOFF")),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					// Required attributes
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "flavor_id"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "instance_name_tag"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "block_device.#"),
					// Computed attributes
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "id"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "status"),
					// Optional attributes (specified)
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "power_state"),
				),
			},
			{
				Config: testAccInstanceResourceWithPowerStateConfig(fmt.Sprintf("%s-power-instance", instancePrefix), "SHUTOFF"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectEmptyPlan(),
					},
				},
			},
		},
	})
}

// TestAccInstanceResource_WithUserData - user_dataパラメータのテスト.
func TestAccInstanceResource_WithUserData(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.TestAccPreCheck(t) },
		ProtoV6ProviderFactories: acctest.TestAccProtoV6ProviderFactories,
		CheckDestroy:             acctest.TestAccCheckResourceDestroy(t),
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceResourceWithUserDataConfig(fmt.Sprintf("%s-userdata-instance", instancePrefix), "IyEvYmluL2Jhc2gKZWNobyAnSGVsbG8gV29ybGQn"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_instance.test", tfjsonpath.New("user_data"), knownvalue.StringExact("IyEvYmluL2Jhc2gKZWNobyAnSGVsbG8gV29ybGQn")),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "flavor_id"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "instance_name_tag"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "block_device.#"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "id"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "status"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "user_data"),
				),
			},
			{
				Config: testAccInstanceResourceWithUserDataConfig(fmt.Sprintf("%s-userdata-instance", instancePrefix), "IyEvYmluL2Jhc2gKeXVtIHVwZGF0ZSAteQ=="),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("conohavps_instance.test", plancheck.ResourceActionReplace),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_instance.test", tfjsonpath.New("user_data"), knownvalue.StringExact("IyEvYmluL2Jhc2gKeXVtIHVwZGF0ZSAteQ==")),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "flavor_id"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "instance_name_tag"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "block_device.#"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "id"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "status"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "user_data"),
				),
			},
			{
				Config: testAccInstanceResourceWithUserDataConfig(fmt.Sprintf("%s-userdata-instance", instancePrefix), "IyEvYmluL2Jhc2gKeXVtIHVwZGF0ZSAteQ=="),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectEmptyPlan(),
					},
				},
			},
		},
	})
}

// TestAccInstanceResource_WithKeyPair - key_nameパラメータのテスト.
func TestAccInstanceResource_WithKeyPair(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.TestAccPreCheck(t) },
		ProtoV6ProviderFactories: acctest.TestAccProtoV6ProviderFactories,
		CheckDestroy:             acctest.TestAccCheckResourceDestroy(t),
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceResourceWithKeyPairConfig(fmt.Sprintf("%s-keypair-instance", instancePrefix), fmt.Sprintf("%s-keypair-1", instancePrefix)),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_instance.test", tfjsonpath.New("key_name"), knownvalue.StringExact(fmt.Sprintf("%s-keypair-1", instancePrefix))),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "flavor_id"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "instance_name_tag"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "block_device.#"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "id"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "status"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "key_name"),
				),
			},
			{
				Config: testAccInstanceResourceWithKeyPairConfig(fmt.Sprintf("%s-keypair-instance", instancePrefix), fmt.Sprintf("%s-keypair-2", instancePrefix)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("conohavps_instance.test", plancheck.ResourceActionReplace),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_instance.test", tfjsonpath.New("key_name"), knownvalue.StringExact(fmt.Sprintf("%s-keypair-2", instancePrefix))),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "flavor_id"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "instance_name_tag"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "block_device.#"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "id"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "status"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "key_name"),
				),
			},
			{
				Config: testAccInstanceResourceWithKeyPairConfig(fmt.Sprintf("%s-keypair-instance", instancePrefix), fmt.Sprintf("%s-keypair-2", instancePrefix)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectEmptyPlan(),
					},
				},
			},
		},
	})
}

// TestAccInstanceResource_WithSingleSecurityGroup - 単一のセキュリティグループ指定.
func TestAccInstanceResource_WithSingleSecurityGroup(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.TestAccPreCheck(t) },
		ProtoV6ProviderFactories: acctest.TestAccProtoV6ProviderFactories,
		CheckDestroy:             acctest.TestAccCheckResourceDestroy(t),
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceResourceWithSecurityGroupConfig(fmt.Sprintf("%s-sg-instance", instancePrefix), []string{fmt.Sprintf("%s-sg-1", instancePrefix)}),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_instance.test", tfjsonpath.New("security_group").AtSliceIndex(0).AtMapKey("name"), knownvalue.StringExact(fmt.Sprintf("%s-sg-1", instancePrefix))),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "flavor_id"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "instance_name_tag"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "block_device.#"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "id"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "status"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "security_group.#"),
				),
			},
			{
				Config: testAccInstanceResourceWithSecurityGroupConfig(fmt.Sprintf("%s-sg-instance", instancePrefix), []string{fmt.Sprintf("%s-sg-1", instancePrefix), fmt.Sprintf("%s-sg-2", instancePrefix)}),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("conohavps_instance.test", plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_instance.test", tfjsonpath.New("security_group").AtSliceIndex(0).AtMapKey("name"), knownvalue.StringExact(fmt.Sprintf("%s-sg-1", instancePrefix))),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "flavor_id"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "instance_name_tag"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "block_device.#"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "id"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "status"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "security_group.#"),
				),
			},
			{
				Config: testAccInstanceResourceWithSecurityGroupConfig(fmt.Sprintf("%s-sg-instance", instancePrefix), []string{fmt.Sprintf("%s-sg-1", instancePrefix), fmt.Sprintf("%s-sg-2", instancePrefix)}),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectEmptyPlan(),
					},
				},
			},
		},
	})
}

// TestAccInstanceResource_WithMultipleSecurityGroups - 複数のセキュリティグループ指定.
func TestAccInstanceResource_WithMultipleSecurityGroups(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.TestAccPreCheck(t) },
		ProtoV6ProviderFactories: acctest.TestAccProtoV6ProviderFactories,
		CheckDestroy:             acctest.TestAccCheckResourceDestroy(t),
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceResourceWithSecurityGroupConfig(fmt.Sprintf("%s-multi-sg-instance", instancePrefix), []string{fmt.Sprintf("%s-sg-1", instancePrefix), fmt.Sprintf("%s-sg-2", instancePrefix)}),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_instance.test", tfjsonpath.New("security_group").AtSliceIndex(0).AtMapKey("name"), knownvalue.StringExact(fmt.Sprintf("%s-sg-1", instancePrefix))),
					statecheck.ExpectKnownValue("conohavps_instance.test", tfjsonpath.New("security_group").AtSliceIndex(1).AtMapKey("name"), knownvalue.StringExact(fmt.Sprintf("%s-sg-2", instancePrefix))),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "flavor_id"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "instance_name_tag"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "block_device.#"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "id"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "status"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "security_group.#"),
				),
			},
			{
				Config: testAccInstanceResourceWithSecurityGroupConfig(fmt.Sprintf("%s-multi-sg-instance", instancePrefix), []string{fmt.Sprintf("%s-sg-1", instancePrefix), fmt.Sprintf("%s-sg-2", instancePrefix), fmt.Sprintf("%s-sg-3", instancePrefix)}),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("conohavps_instance.test", plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_instance.test", tfjsonpath.New("security_group").AtSliceIndex(0).AtMapKey("name"), knownvalue.StringExact(fmt.Sprintf("%s-sg-1", instancePrefix))),
					statecheck.ExpectKnownValue("conohavps_instance.test", tfjsonpath.New("security_group").AtSliceIndex(1).AtMapKey("name"), knownvalue.StringExact(fmt.Sprintf("%s-sg-2", instancePrefix))),
					statecheck.ExpectKnownValue("conohavps_instance.test", tfjsonpath.New("security_group").AtSliceIndex(2).AtMapKey("name"), knownvalue.StringExact(fmt.Sprintf("%s-sg-3", instancePrefix))),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "flavor_id"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "instance_name_tag"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "block_device.#"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "id"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "status"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "security_group.#"),
				),
			},
			{
				Config: testAccInstanceResourceWithSecurityGroupConfig(fmt.Sprintf("%s-multi-sg-instance", instancePrefix), []string{fmt.Sprintf("%s-sg-1", instancePrefix), fmt.Sprintf("%s-sg-2", instancePrefix), fmt.Sprintf("%s-sg-3", instancePrefix)}),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectEmptyPlan(),
					},
				},
			},
		},
	})
}

// TestAccInstanceResource_WithMultipleBlockDevices - 複数のブロックデバイス指定.
func TestAccInstanceResource_WithMultipleBlockDevices(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.TestAccPreCheck(t) },
		ProtoV6ProviderFactories: acctest.TestAccProtoV6ProviderFactories,
		CheckDestroy:             acctest.TestAccCheckResourceDestroy(t),
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceResourceWithMultipleBlockDevicesConfig(fmt.Sprintf("%s-multi-block-instance", instancePrefix)),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_instance.test", tfjsonpath.New("instance_name_tag"), knownvalue.StringExact(fmt.Sprintf("%s-multi-block-instance", instancePrefix))),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "flavor_id"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "instance_name_tag"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "block_device.#"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "id"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "status"),
				),
			},
		},
	})
}

// TestAccInstanceResource_WithFlavorIDChange - flavor_id変更のテスト.
func TestAccInstanceResource_WithFlavorIDChange(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.TestAccPreCheck(t) },
		ProtoV6ProviderFactories: acctest.TestAccProtoV6ProviderFactories,
		CheckDestroy:             acctest.TestAccCheckResourceDestroy(t),
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceResourceWithFlavorIDConfig(fmt.Sprintf("%s-flavor-instance", instancePrefix), "f2a77529-1815-43a2-bc14-1f3f6b09079c"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_instance.test", tfjsonpath.New("flavor_id"), knownvalue.StringExact("f2a77529-1815-43a2-bc14-1f3f6b09079c")),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					// Required attributes
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "flavor_id"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "instance_name_tag"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "block_device.#"),
					// Computed attributes
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "id"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "status"),
				),
			},
			{
				Config: testAccInstanceResourceWithFlavorIDConfig(fmt.Sprintf("%s-flavor-instance", instancePrefix), "784f1ae8-0bc8-4d06-a06b-2afaa9580e0a"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("conohavps_instance.test", plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_instance.test", tfjsonpath.New("flavor_id"), knownvalue.StringExact("784f1ae8-0bc8-4d06-a06b-2afaa9580e0a")),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					// Required attributes
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "flavor_id"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "instance_name_tag"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "block_device.#"),
					// Computed attributes
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "id"),
					resource.TestCheckResourceAttrSet("conohavps_instance.test", "status"),
				),
			},
			{
				Config: testAccInstanceResourceWithFlavorIDConfig(fmt.Sprintf("%s-flavor-instance", instancePrefix), "784f1ae8-0bc8-4d06-a06b-2afaa9580e0a"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectEmptyPlan(),
					},
				},
			},
		},
	})
}

// ヘルパー関数：Provider + BootVolume設定.
func testAccProviderAndBootVolumeConfig() string {
	return fmt.Sprintf(`
%s
`, testAccBootVolumeConfig())
}

// ヘルパー関数：BootVolume設定.
func testAccBootVolumeConfig() string {
	return fmt.Sprintf(`
resource "conohavps_volume" "test" {
	size      = 100
	image_ref = "884c1899-fefe-40bd-aab8-1001b1d9c895"
	name      = "%s-boot-volume"
	volume_type = "c3j1-ds02-boot"
}
`, instancePrefix)
}

// ヘルパー関数：flavorID変数設定.
func testAccFlavorIDVarConfig(flavorID string) string {
	return fmt.Sprintf(`
variable "flavor_id" {
	type    = string
	default = "%s"
}
`, flavorID)
}

// ヘルパー関数：最小限Instance設定.
func testAccInstanceResourceMinimalConfig(instanceName string) string {
	return fmt.Sprintf(`
%s

%s

resource "conohavps_instance" "test" {
	flavor_id           = var.flavor_id
	instance_name_tag   = "%s"
	block_device        = [
		{
			uuid = conohavps_volume.test.id
		},
	]
}
`, testAccProviderAndBootVolumeConfig(), testAccFlavorIDVarConfig("f2a77529-1815-43a2-bc14-1f3f6b09079c"), instanceName)
}

// ヘルパー関数：admin_passパラメータ設定.
func testAccInstanceResourceWithAdminPassConfig(instanceName string, adminPass string) string {
	return fmt.Sprintf(`
%s

%s

resource "conohavps_instance" "test" {
	flavor_id           = var.flavor_id
	instance_name_tag   = "%s"
	admin_pass          = "%s"
	block_device        = [
		{
			uuid = conohavps_volume.test.id
		},
	]
}
`, testAccProviderAndBootVolumeConfig(), testAccFlavorIDVarConfig("f2a77529-1815-43a2-bc14-1f3f6b09079c"), instanceName, adminPass)
}

// ヘルパー関数：power_stateパラメータ設定.
func testAccInstanceResourceWithPowerStateConfig(instanceName string, powerState string) string {
	return fmt.Sprintf(`
%s

%s

resource "conohavps_instance" "test" {
	flavor_id           = var.flavor_id
	instance_name_tag   = "%s"
	power_state         = "%s"
	block_device        = [
		{
			uuid = conohavps_volume.test.id
		},
	]
}
`, testAccProviderAndBootVolumeConfig(), testAccFlavorIDVarConfig("f2a77529-1815-43a2-bc14-1f3f6b09079c"), instanceName, powerState)
}

// ヘルパー関数：user_dataパラメータ設定.
func testAccInstanceResourceWithUserDataConfig(instanceName string, userData string) string {
	return fmt.Sprintf(`
%s

%s

resource "conohavps_instance" "test" {
	flavor_id           = var.flavor_id
	instance_name_tag   = "%s"
	user_data           = "%s"
	block_device        = [
		{
			uuid = conohavps_volume.test.id
		},
	]
}
`, testAccProviderAndBootVolumeConfig(), testAccFlavorIDVarConfig("f2a77529-1815-43a2-bc14-1f3f6b09079c"), instanceName, userData)
}

// ヘルパー関数：key_nameパラメータ設定.
func testAccInstanceResourceWithKeyPairConfig(instanceName string, keyName string) string {
	return fmt.Sprintf(`
%s

%s

resource "conohavps_keypair" "test" {
	name = "%s"
}

resource "conohavps_instance" "test" {
	flavor_id           = var.flavor_id
	instance_name_tag   = "%s"
	key_name            = conohavps_keypair.test.name
	block_device        = [
		{
			uuid = conohavps_volume.test.id
		},
	]
}
`, testAccProviderAndBootVolumeConfig(), testAccFlavorIDVarConfig("f2a77529-1815-43a2-bc14-1f3f6b09079c"), keyName, instanceName)
}

// ヘルパー関数：security_groupパラメータ設定.
func testAccInstanceResourceWithSecurityGroupConfig(instanceName string, sgNames []string) string {
	sgResources := ""
	sgBlocks := ""

	for _, sgName := range sgNames {
		sgResources += fmt.Sprintf(`
resource "conohavps_securitygroup" "test_%s" {
	name = "%s"
}
`, sgName, sgName)
	}

	for _, sgName := range sgNames {
		sgBlocks += fmt.Sprintf(`
		{
			name = conohavps_securitygroup.test_%s.name
		},`, sgName)
	}

	return fmt.Sprintf(`
%s

%s

%s

resource "conohavps_instance" "test" {
	flavor_id           = var.flavor_id
	instance_name_tag   = "%s"
	block_device        = [
		{
			uuid = conohavps_volume.test.id
		},
	]
	security_group      = [%s
	]
}
`, testAccProviderAndBootVolumeConfig(), testAccFlavorIDVarConfig("f2a77529-1815-43a2-bc14-1f3f6b09079c"), sgResources, instanceName, sgBlocks)
}

// ヘルパー関数：複数block_deviceパラメータ設定.
func testAccInstanceResourceWithMultipleBlockDevicesConfig(instanceName string) string {
	baseConfig := testAccProviderAndBootVolumeConfig()
	flavorConfig := testAccFlavorIDVarConfig("f2a77529-1815-43a2-bc14-1f3f6b09079c")
	additionalVolumeConfig := testAccAdditionalVolumeConfig()

	return fmt.Sprintf(`
%s

%s

%s

resource "conohavps_instance" "test" {
	flavor_id           = var.flavor_id
	instance_name_tag   = "%s"
	block_device        = [
		{
			uuid = conohavps_volume.test.id
		},
		{
			uuid = conohavps_volume.test2.id
		},
	]
}
`, baseConfig, flavorConfig, additionalVolumeConfig, instanceName)
}

// ヘルパー関数：flavor_id動的指定Instance設定.
func testAccInstanceResourceWithFlavorIDConfig(instanceName string, flavorID string) string {
	return fmt.Sprintf(`
%s

%s

resource "conohavps_instance" "test" {
	flavor_id           = var.flavor_id
	instance_name_tag   = "%s"
	block_device        = [
		{
			uuid = conohavps_volume.test.id
		},
	]
}
`, testAccProviderAndBootVolumeConfig(), testAccFlavorIDVarConfig(flavorID), instanceName)
}

// ヘルパー関数：追加Volume設定.
func testAccAdditionalVolumeConfig() string {
	return fmt.Sprintf(`
resource "conohavps_volume" "test2" {
	size      = 200
	name      = "%s-add-volume"
	volume_type = "c3j1-ds02-add"
}
`, instancePrefix)
}
