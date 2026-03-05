// セキュリティグループルールのリソースの受け入れテストを提供する.
// 実際にセキュリティグループルールのリソースを作成し、作成、更新、削除、インポートの動作を検証する.

package resource_test

import (
	"fmt"
	"testing"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/acctest"
	"github.com/hashicorp/terraform-plugin-testing/compare"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

var securityGroupRulePrefix = acctest.GetTestPrefix("securitygroup_rule")

func init() {
	resource.AddTestSweepers("conohavps_securitygroup_rule", &resource.Sweeper{
		Name: "conohavps_securitygroup_rule",
		F:    acctest.TestSweepResources(securityGroupRulePrefix),
	})
}

// 必須のパラメータのみ.
func TestAccSecurityGroupRule_Basic(t *testing.T) {
	groupResourceAddress := "conohavps_securitygroup.group_1"
	ruleResourceAddress := "conohavps_securitygroup_rule.rule_1"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.TestAccPreCheck(t) },
		ProtoV6ProviderFactories: acctest.TestAccProtoV6ProviderFactories,
		CheckDestroy:             acctest.TestAccCheckResourceDestroy(t),
		Steps: []resource.TestStep{
			// 作成
			{
				Config: testAccSecurityGroupRuleConfig_Basic(),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectKnownValue(
							ruleResourceAddress,
							tfjsonpath.New("direction"),
							knownvalue.StringExact("ingress"),
						),
						plancheck.ExpectKnownValue(
							ruleResourceAddress,
							tfjsonpath.New("ethertype"),
							knownvalue.StringExact("IPv4"),
						),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.CompareValuePairs(
						groupResourceAddress,
						tfjsonpath.New("id"),
						ruleResourceAddress,
						tfjsonpath.New("securitygroup_id"),
						compare.ValuesSame(),
					),
					statecheck.ExpectKnownValue(
						ruleResourceAddress,
						tfjsonpath.New("direction"),
						knownvalue.StringExact("ingress"),
					),
					statecheck.ExpectKnownValue(
						ruleResourceAddress,
						tfjsonpath.New("ethertype"),
						knownvalue.StringExact("IPv4"),
					),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "id"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "securitygroup_id"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "direction"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "ethertype"),
				),
			},
			// インポート
			{
				ResourceName:      ruleResourceAddress,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// 任意のパラメータを指定、更新（protocol）.
func TestAccSecurityGroupRule_SetProtocol(t *testing.T) {
	ruleResourceAddress := "conohavps_securitygroup_rule.rule_1"

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			acctest.TestAccPreCheck(t)
			acctest.TestAccSleep(60)
		},
		ProtoV6ProviderFactories: acctest.TestAccProtoV6ProviderFactories,
		CheckDestroy:             acctest.TestAccCheckResourceDestroy(t),
		Steps: []resource.TestStep{
			// 作成
			{
				Config: testAccSecurityGroupRuleConfig_SetProtocol("tcp"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectKnownValue(
							ruleResourceAddress,
							tfjsonpath.New("protocol"),
							knownvalue.StringExact("tcp"),
						),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(
						ruleResourceAddress,
						tfjsonpath.New("protocol"),
						knownvalue.StringExact("tcp"),
					),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "id"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "securitygroup_id"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "direction"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "ethertype"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "protocol"),
				),
			},
			// 更新
			{
				Config: testAccSecurityGroupRuleConfig_SetProtocol("udp"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectKnownValue(
							ruleResourceAddress,
							tfjsonpath.New("protocol"),
							knownvalue.StringExact("udp"),
						),
						plancheck.ExpectResourceAction(ruleResourceAddress, plancheck.ResourceActionReplace),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(
						ruleResourceAddress,
						tfjsonpath.New("protocol"),
						knownvalue.StringExact("udp"),
					),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "id"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "securitygroup_id"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "direction"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "ethertype"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "protocol"),
				),
			},
		},
	})
}

// 任意のパラメータを指定、更新（port_range_min、port_range_max）.
func TestAccSecurityGroupRule_SetPortRange(t *testing.T) {
	ruleResourceAddress := "conohavps_securitygroup_rule.rule_1"

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			acctest.TestAccPreCheck(t)
			acctest.TestAccSleep(60)
		},
		ProtoV6ProviderFactories: acctest.TestAccProtoV6ProviderFactories,
		CheckDestroy:             acctest.TestAccCheckResourceDestroy(t),
		Steps: []resource.TestStep{
			// 作成
			{
				Config: testAccSecurityGroupRuleConfig_SetPortrange(80, 80),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectKnownValue(
							ruleResourceAddress,
							tfjsonpath.New("port_range_min"),
							knownvalue.Int64Exact(80),
						),
						plancheck.ExpectKnownValue(
							ruleResourceAddress,
							tfjsonpath.New("port_range_max"),
							knownvalue.Int64Exact(80),
						),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(
						ruleResourceAddress,
						tfjsonpath.New("port_range_min"),
						knownvalue.Int64Exact(80),
					),
					statecheck.ExpectKnownValue(
						ruleResourceAddress,
						tfjsonpath.New("port_range_max"),
						knownvalue.Int64Exact(80),
					),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "id"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "securitygroup_id"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "direction"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "ethertype"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "protocol"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "port_range_min"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "port_range_max"),
				),
			},
			// 更新
			{
				Config: testAccSecurityGroupRuleConfig_SetPortrange(443, 443),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectKnownValue(
							ruleResourceAddress,
							tfjsonpath.New("port_range_min"),
							knownvalue.Int64Exact(443),
						),
						plancheck.ExpectKnownValue(
							ruleResourceAddress,
							tfjsonpath.New("port_range_max"),
							knownvalue.Int64Exact(443),
						),
						plancheck.ExpectResourceAction(ruleResourceAddress, plancheck.ResourceActionReplace),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(
						ruleResourceAddress,
						tfjsonpath.New("port_range_min"),
						knownvalue.Int64Exact(443),
					),
					statecheck.ExpectKnownValue(
						ruleResourceAddress,
						tfjsonpath.New("port_range_max"),
						knownvalue.Int64Exact(443),
					),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "id"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "securitygroup_id"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "direction"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "ethertype"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "protocol"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "port_range_min"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "port_range_max"),
				),
			},
		},
	})
}

// 任意のパラメータを指定、更新（remote_ip_prefix）.
func TestAccSecurityGroupRule_SetRemoteIPPrefix(t *testing.T) {
	ruleResourceAddress := "conohavps_securitygroup_rule.rule_1"

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			acctest.TestAccPreCheck(t)
			acctest.TestAccSleep(60)
		},
		ProtoV6ProviderFactories: acctest.TestAccProtoV6ProviderFactories,
		CheckDestroy:             acctest.TestAccCheckResourceDestroy(t),
		Steps: []resource.TestStep{
			// 作成
			{
				Config: testAccSecurityGroupRuleConfig_SetRemoteIPPrefix("0.0.0.0/0"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectKnownValue(
							ruleResourceAddress,
							tfjsonpath.New("remote_ip_prefix"),
							knownvalue.StringExact("0.0.0.0/0"),
						),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(
						ruleResourceAddress,
						tfjsonpath.New("remote_ip_prefix"),
						knownvalue.StringExact("0.0.0.0/0"),
					),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "id"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "securitygroup_id"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "direction"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "ethertype"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "protocol"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "port_range_min"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "port_range_max"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "remote_ip_prefix"),
				),
			},
			// 更新
			{
				Config: testAccSecurityGroupRuleConfig_SetRemoteIPPrefix("103.21.244.0/22"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectKnownValue(
							ruleResourceAddress,
							tfjsonpath.New("remote_ip_prefix"),
							knownvalue.StringExact("103.21.244.0/22"),
						),
						plancheck.ExpectResourceAction(ruleResourceAddress, plancheck.ResourceActionReplace),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(
						ruleResourceAddress,
						tfjsonpath.New("remote_ip_prefix"),
						knownvalue.StringExact("103.21.244.0/22"),
					),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "id"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "securitygroup_id"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "direction"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "ethertype"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "protocol"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "port_range_min"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "port_range_max"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "remote_ip_prefix"),
				),
			},
		},
	})
}

// 任意のパラメータを指定、更新（remote_group_id）.
func TestAccSecurityGroupRule_SetRemoteGroupID(t *testing.T) {
	beforeGroupResourceAddress := "conohavps_securitygroup.group_2"
	afterGroupResourceAddress := "conohavps_securitygroup.group_3"
	ruleResourceAddress := "conohavps_securitygroup_rule.rule_1"

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			acctest.TestAccPreCheck(t)
			acctest.TestAccSleep(60)
		},
		ProtoV6ProviderFactories: acctest.TestAccProtoV6ProviderFactories,
		CheckDestroy:             acctest.TestAccCheckResourceDestroy(t),
		Steps: []resource.TestStep{
			// 作成
			{
				Config: testAccSecurityGroupRuleConfig_SetRemoteGroupID(2),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.CompareValuePairs(
						beforeGroupResourceAddress,
						tfjsonpath.New("id"),
						ruleResourceAddress,
						tfjsonpath.New("remote_group_id"),
						compare.ValuesSame(),
					),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "id"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "securitygroup_id"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "direction"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "ethertype"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "protocol"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "port_range_min"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "port_range_max"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "remote_group_id"),
				),
			},
			// 更新
			{
				Config: testAccSecurityGroupRuleConfig_SetRemoteGroupID(3),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(ruleResourceAddress, plancheck.ResourceActionReplace),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.CompareValuePairs(
						afterGroupResourceAddress,
						tfjsonpath.New("id"),
						ruleResourceAddress,
						tfjsonpath.New("remote_group_id"),
						compare.ValuesSame(),
					),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "id"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "securitygroup_id"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "direction"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "ethertype"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "protocol"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "port_range_min"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "port_range_max"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "remote_group_id"),
				),
			},
		},
	})
}

// パラメータを更新（securitygroup_id）.
func TestAccSecurityGroupRule_UpdateSecurityGroup(t *testing.T) {
	newGroupResourceAddress := "conohavps_securitygroup.group_2"
	ruleResourceAddress := "conohavps_securitygroup_rule.rule_1"

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			acctest.TestAccPreCheck(t)
			acctest.TestAccSleep(60)
		},
		ProtoV6ProviderFactories: acctest.TestAccProtoV6ProviderFactories,
		CheckDestroy:             acctest.TestAccCheckResourceDestroy(t),
		Steps: []resource.TestStep{
			// 作成
			{
				Config: testAccSecurityGroupRuleConfig_Basic(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "id"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "securitygroup_id"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "direction"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "ethertype"),
				),
			},
			// 更新
			{
				Config: testAccSecurityGroupRuleConfig_UpdateSecurityGroup(),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.CompareValuePairs(
						newGroupResourceAddress,
						tfjsonpath.New("id"),
						ruleResourceAddress,
						tfjsonpath.New("securitygroup_id"),
						compare.ValuesSame(),
					),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "id"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "securitygroup_id"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "direction"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "ethertype"),
				),
			},
		},
	})
}

// パラメータを更新（direction）.
func TestAccSecurityGroupRule_UpdateDirection(t *testing.T) {
	ruleResourceAddress := "conohavps_securitygroup_rule.rule_1"

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			acctest.TestAccPreCheck(t)
			acctest.TestAccSleep(60)
		},
		ProtoV6ProviderFactories: acctest.TestAccProtoV6ProviderFactories,
		CheckDestroy:             acctest.TestAccCheckResourceDestroy(t),
		Steps: []resource.TestStep{
			// 作成
			{
				Config: testAccSecurityGroupRuleConfig_Basic(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "id"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "securitygroup_id"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "direction"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "ethertype"),
				),
			},
			// 更新
			{
				Config: testAccSecurityGroupRuleConfig_UpdateDirection("egress"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectKnownValue(
							ruleResourceAddress,
							tfjsonpath.New("direction"),
							knownvalue.StringExact("egress"),
						),
						plancheck.ExpectResourceAction(ruleResourceAddress, plancheck.ResourceActionReplace),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(
						ruleResourceAddress,
						tfjsonpath.New("direction"),
						knownvalue.StringExact("egress"),
					),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "id"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "securitygroup_id"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "direction"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "ethertype"),
				),
			},
		},
	})
}

// パラメータを更新（ethertype）.
func TestAccSecurityGroupRule_UpdateEthertype(t *testing.T) {
	ruleResourceAddress := "conohavps_securitygroup_rule.rule_1"

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			acctest.TestAccPreCheck(t)
			acctest.TestAccSleep(60)
		},
		ProtoV6ProviderFactories: acctest.TestAccProtoV6ProviderFactories,
		CheckDestroy:             acctest.TestAccCheckResourceDestroy(t),
		Steps: []resource.TestStep{
			// 作成
			{
				Config: testAccSecurityGroupRuleConfig_Basic(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "id"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "securitygroup_id"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "direction"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "ethertype"),
				),
			},
			// 更新
			{
				Config: testAccSecurityGroupRuleConfig_UpdateEthertype("IPv6"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectKnownValue(
							ruleResourceAddress,
							tfjsonpath.New("ethertype"),
							knownvalue.StringExact("IPv6"),
						),
						plancheck.ExpectResourceAction(ruleResourceAddress, plancheck.ResourceActionReplace),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(
						ruleResourceAddress,
						tfjsonpath.New("ethertype"),
						knownvalue.StringExact("IPv6"),
					),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "id"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "securitygroup_id"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "direction"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "ethertype"),
				),
			},
		},
	})
}

// パラメータの差分無し.
func TestAccSecurityGroupRule_EmptyPlan(t *testing.T) {
	ruleResourceAddress := "conohavps_securitygroup_rule.rule_1"

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
				Config: testAccSecurityGroupRuleConfig_Basic(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "id"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "securitygroup_id"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "direction"),
					resource.TestCheckResourceAttrSet(ruleResourceAddress, "ethertype"),
				),
			},
			// 差分無し
			{
				Config: testAccSecurityGroupRuleConfig_Basic(),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectEmptyPlan(),
						plancheck.ExpectResourceAction(ruleResourceAddress, plancheck.ResourceActionNoop),
					},
				},
			},
		},
	})
}

func testAccSecurityGroupRuleConfig_Basic() string {
	return fmt.Sprintf(`
resource "conohavps_securitygroup" "group_1" {
	name = "%s-sg-rule-group"
}

resource "conohavps_securitygroup_rule" "rule_1" {
	securitygroup_id = conohavps_securitygroup.group_1.id
	direction = "ingress"
	ethertype = "IPv4"
}
`, securityGroupRulePrefix)
}

func testAccSecurityGroupRuleConfig_SetPortrange(portRangeMin, portRangeMax int64) string {
	return fmt.Sprintf(`
resource "conohavps_securitygroup" "group_1" {
	name = "%s-sg-rule-group"
}

resource "conohavps_securitygroup_rule" "rule_1" {
	securitygroup_id = conohavps_securitygroup.group_1.id
	direction = "ingress"
	ethertype = "IPv4"
	protocol = "tcp"
	port_range_min = "%d"
	port_range_max = "%d"
}
`, securityGroupRulePrefix, portRangeMin, portRangeMax)
}

func testAccSecurityGroupRuleConfig_SetRemoteIPPrefix(remoteIPPrefix string) string {
	return fmt.Sprintf(`
resource "conohavps_securitygroup" "group_1" {
	name = "%s-sg-rule-group"
}

resource "conohavps_securitygroup_rule" "rule_1" {
	securitygroup_id = conohavps_securitygroup.group_1.id
	direction = "ingress"
	ethertype = "IPv4"
	protocol = "tcp"
	port_range_min = "80"
	port_range_max = "80"
	remote_ip_prefix = "%s"
}
`, securityGroupRulePrefix, remoteIPPrefix)
}

func testAccSecurityGroupRuleConfig_SetRemoteGroupID(group int) string {
	return fmt.Sprintf(`
resource "conohavps_securitygroup" "group_1" {
	name = "%s-sg-rule-group-1"
}

resource "conohavps_securitygroup" "group_2" {
	name = "%s-sg-rule-group-2"
}

resource "conohavps_securitygroup" "group_3" {
	name = "%s-sg-rule-group-3"
}

resource "conohavps_securitygroup_rule" "rule_1" {
	securitygroup_id = conohavps_securitygroup.group_1.id
	direction = "ingress"
	ethertype = "IPv4"
	protocol = "tcp"
	port_range_min = "80"
	port_range_max = "80"
	remote_group_id = conohavps_securitygroup.group_%d.id
}
`, securityGroupRulePrefix, securityGroupRulePrefix, securityGroupRulePrefix, group)
}

func testAccSecurityGroupRuleConfig_UpdateSecurityGroup() string {
	return fmt.Sprintf(`
resource "conohavps_securitygroup" "group_1" {
	name = "%s-sg-rule-group"
}

resource "conohavps_securitygroup" "group_2" {
	name = "%s-sg-rule-group-2"
}

resource "conohavps_securitygroup_rule" "rule_1" {
	securitygroup_id = conohavps_securitygroup.group_2.id
	direction = "ingress"
	ethertype = "IPv4"
}
`, securityGroupRulePrefix, securityGroupRulePrefix)
}

func testAccSecurityGroupRuleConfig_UpdateDirection(direction string) string {
	return fmt.Sprintf(`
resource "conohavps_securitygroup" "group_1" {
	name = "%s-sg-rule-group"
}

resource "conohavps_securitygroup_rule" "rule_1" {
	securitygroup_id = conohavps_securitygroup.group_1.id
	direction = "%s"
	ethertype = "IPv4"
	protocol = "tcp"
}
`, securityGroupRulePrefix, direction)
}

func testAccSecurityGroupRuleConfig_UpdateEthertype(ethertype string) string {
	return fmt.Sprintf(`
resource "conohavps_securitygroup" "group_1" {
	name = "%s-sg-rule-group"
}

resource "conohavps_securitygroup_rule" "rule_1" {
	securitygroup_id = conohavps_securitygroup.group_1.id
	direction = "ingress"
	ethertype = "%s"
	protocol = "tcp"
}
`, securityGroupRulePrefix, ethertype)
}

func testAccSecurityGroupRuleConfig_SetProtocol(protocol string) string {
	return fmt.Sprintf(`
resource "conohavps_securitygroup" "group_1" {
	name = "%s-sg-rule-group"
}

resource "conohavps_securitygroup_rule" "rule_1" {
	securitygroup_id = conohavps_securitygroup.group_1.id
	direction = "ingress"
	ethertype = "IPv4"
	protocol = "%s"
}
`, securityGroupRulePrefix, protocol)
}
