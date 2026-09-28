// サブネットのリソースの単体テストを提供する.
// 偽の API に対して作成・作り直し・インポート・削除と、ネットワークアドレスの検証を確かめる.

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

func subnetUnitConfig(f *fakeNetworking, cidr string) string {
	return f.ProviderConfig() + `
resource "conohavps_network" "test" {}

resource "conohavps_subnet" "test" {
  network_id = conohavps_network.test.id
  cidr       = "` + cidr + `"
}
`
}

func TestSubnetUnit(t *testing.T) {
	f := newFakeNetworking(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		CheckDestroy:             f.checkDestroyed,
		Steps: []resource.TestStep{
			{
				Config: subnetUnitConfig(f, "10.0.0.0/24"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_subnet.test", tfjsonpath.New("cidr"), knownvalue.StringExact("10.0.0.0/24")),
					statecheck.ExpectKnownValue("conohavps_subnet.test", tfjsonpath.New("name"), knownvalue.StringExact("local-10-0-0-0-24")),
					statecheck.ExpectKnownValue("conohavps_subnet.test", tfjsonpath.New("ip_version"), knownvalue.Int64Exact(4)),
					statecheck.ExpectKnownValue("conohavps_subnet.test", tfjsonpath.New("gateway_ip"), knownvalue.Null()),
					statecheck.ExpectKnownValue("conohavps_subnet.test", tfjsonpath.New("allocation_pools"), knownvalue.ListExact([]knownvalue.Check{
						knownvalue.ObjectExact(map[string]knownvalue.Check{
							"start": knownvalue.StringExact("10.0.0.1"),
							"end":   knownvalue.StringExact("10.0.0.254"),
						}),
					})),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("conohavps_subnet.test", "network_id", "conohavps_network.test", "id"),
					// ドキュメントの本文は network_id と cidr だけ
					f.expectBodyFn("POST", "/networking/v2.0/subnets", func() string {
						return `{"subnet":{"network_id":"` + f.onlyNetworkID() + `","cidr":"10.0.0.0/24"}}`
					}),
				),
			},
			{
				// サブネットは更新の API が無いため、作り直す
				Config: subnetUnitConfig(f, "192.168.10.0/27"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("conohavps_subnet.test", plancheck.ResourceActionDestroyBeforeCreate),
					plancheck.ExpectResourceAction("conohavps_network.test", plancheck.ResourceActionNoop),
				}},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_subnet.test", tfjsonpath.New("cidr"), knownvalue.StringExact("192.168.10.0/27")),
				},
			},
			{
				ResourceName:      "conohavps_subnet.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestSubnetUnit_InvalidCIDR(t *testing.T) {
	f := newFakeNetworking(t)
	cases := map[string]string{
		"10.0.0.0/20":     `prefix length of "10.0.0.0/20" must be from /21 to /27`,
		"10.0.0.0/28":     `prefix length of "10.0.0.0/28" must be from /21 to /27`,
		"8.8.8.0/24":      `not in 10.0.0.0/8, 172.16.0.0/12 or 192.168.0.0/16`,
		"172.32.0.0/24":   `not in 10.0.0.0/8, 172.16.0.0/12 or 192.168.0.0/16`,
		"10.0.0.1/24":     `not a network address; use "10.0.0.0/24"`,
		"fd00::/64":       `not an IPv4 network`,
		"not-a-cidr":      `not in CIDR notation`,
		"172.31.248.0/21": "",
	}
	var steps []resource.TestStep
	for cidr, msg := range cases {
		step := resource.TestStep{Config: subnetUnitConfig(f, cidr), PlanOnly: true, ExpectNonEmptyPlan: true}
		if msg != "" {
			step.ExpectError = regexp.MustCompile(regexp.QuoteMeta(msg))
		}
		steps = append(steps, step)
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		Steps:                    steps,
	})
}
