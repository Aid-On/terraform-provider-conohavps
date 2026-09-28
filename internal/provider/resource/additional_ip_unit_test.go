// 追加IPアドレスのリソースの単体テストを提供する.
// 偽の API に対して作成・その場での更新・作り直し・インポート・削除と、送るリクエストの本文を確かめる.

package resource_test

import (
	"regexp"
	"testing"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/fakeapi"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

func additionalIPUnitConfig(f *fakeNetworking, body string) string {
	return f.ProviderConfig() + `
resource "conohavps_additional_ip" "test" {
` + body + `
}
`
}

func TestAdditionalIPUnit(t *testing.T) {
	f := newFakeNetworking(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		CheckDestroy:             f.checkDestroyed,
		Steps: []resource.TestStep{
			{
				Config: additionalIPUnitConfig(f, `
  ip_count = 2
`),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_additional_ip.test", tfjsonpath.New("ip_addresses"), knownvalue.ListExact([]knownvalue.Check{
						knownvalue.StringExact("203.0.113.1"), knownvalue.StringExact("203.0.113.2"),
					})),
					// 未指定なら default のセキュリティグループが付く
					statecheck.ExpectKnownValue("conohavps_additional_ip.test", tfjsonpath.New("security_group_ids"), knownvalue.SetExact([]knownvalue.Check{knownvalue.StringExact("sg-default")})),
					statecheck.ExpectKnownValue("conohavps_additional_ip.test", tfjsonpath.New("name"), knownvalue.StringExact("add-i_100000-o_100000-p_0a")),
					statecheck.ExpectKnownValue("conohavps_additional_ip.test", tfjsonpath.New("network_id"), knownvalue.StringExact("fb00d078-8ae1-4145-b3b9-82dfb7596227")),
					statecheck.ExpectKnownValue("conohavps_additional_ip.test", tfjsonpath.New("qos_policy_id"), knownvalue.Null()),
					statecheck.ExpectKnownValue("conohavps_additional_ip.test", tfjsonpath.New("qos_network_policy_id"), knownvalue.Null()),
				},
				Check: f.expectBody("POST", "/networking/v2.0/allocateips", `{"allocateip":{"count":2}}`),
			},
			{
				// セキュリティグループと QoS ポリシーはその場で更新する
				Config: additionalIPUnitConfig(f, `
  ip_count           = 2
  security_group_ids = ["sg-1"]
  qos_policy_id      = "qos-300"
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("conohavps_additional_ip.test", plancheck.ResourceActionUpdate),
				}},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_additional_ip.test", tfjsonpath.New("qos_policy_id"), knownvalue.StringExact("qos-300")),
					statecheck.ExpectKnownValue("conohavps_additional_ip.test", tfjsonpath.New("ip_addresses"), knownvalue.ListSizeExact(2)),
				},
				Check: f.expectBody("PUT", "/networking/v2.0/ports/port-.*", `{"port":{"security_groups":["sg-1"],"qos_policy_id":"qos-300"}}`),
			},
			{
				// Terraform の外で QoS ポリシーを変えると差分になり、QoS ポリシーだけを送って戻す
				PreConfig: f.editOnlyPort(func(p map[string]any) {
					p["qos_policy_id"] = "qos-100"
					p["qos_network_policy_id"] = "qos-net"
				}),
				Config: additionalIPUnitConfig(f, `
  ip_count           = 2
  security_group_ids = ["sg-1"]
  qos_policy_id      = "qos-300"
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("conohavps_additional_ip.test", plancheck.ResourceActionUpdate),
				}},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_additional_ip.test", tfjsonpath.New("qos_policy_id"), knownvalue.StringExact("qos-300")),
					statecheck.ExpectKnownValue("conohavps_additional_ip.test", tfjsonpath.New("qos_network_policy_id"), knownvalue.StringExact("qos-net")),
				},
				Check: f.expectBody("PUT", "/networking/v2.0/ports/port-.*", `{"port":{"qos_policy_id":"qos-300"}}`),
			},
			{
				ResourceName:      "conohavps_additional_ip.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				// 個数を変えると割り当て直す. QoS ポリシーは作成後の更新で付ける
				Config: additionalIPUnitConfig(f, `
  ip_count           = 3
  security_group_ids = ["sg-1", "sg-2"]
  qos_policy_id      = "qos-300"
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("conohavps_additional_ip.test", plancheck.ResourceActionDestroyBeforeCreate),
				}},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_additional_ip.test", tfjsonpath.New("ip_addresses"), knownvalue.ListSizeExact(3)),
					statecheck.ExpectKnownValue("conohavps_additional_ip.test", tfjsonpath.New("qos_policy_id"), knownvalue.StringExact("qos-300")),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					f.expectBodyFn("POST", "/networking/v2.0/allocateips", func() string {
						// セキュリティグループは集合のため、送る順は決まらない
						b, _ := f.lastBody("POST", "/networking/v2.0/allocateips")
						if sgs, _ := b["allocateip"].(map[string]any)["security_groups"].([]any); len(sgs) == 2 && sgs[0] == "sg-2" {
							return `{"allocateip":{"count":3,"security_groups":["sg-2","sg-1"]}}`
						}
						return `{"allocateip":{"count":3,"security_groups":["sg-1","sg-2"]}}`
					}),
					f.expectBody("PUT", "/networking/v2.0/ports/port-.*", `{"port":{"qos_policy_id":"qos-300"}}`),
				),
			},
		},
	})
}

func TestAdditionalIPUnit_InvalidCount(t *testing.T) {
	f := newFakeNetworking(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		Steps: []resource.TestStep{
			{Config: additionalIPUnitConfig(f, `ip_count = 0`), ExpectError: regexp.MustCompile(`between 1 and 16`)},
			{Config: additionalIPUnitConfig(f, `ip_count = 17`), ExpectError: regexp.MustCompile(`between 1 and 16`)},
		},
	})
}
