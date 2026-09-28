// ローカルネットワーク用のポートのリソースの単体テストを提供する.
// 偽の API に対して作成・その場での更新・作り直し・インポート・削除と、送るリクエストの本文を確かめる.

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

// ネットワーク2つ（a・b）とそれぞれのサブネットに、port 以下の本文のポートを足した設定.
func portUnitConfig(f *fakeNetworking, port string) string {
	return f.ProviderConfig() + `
resource "conohavps_network" "a" {}

resource "conohavps_subnet" "a" {
  network_id = conohavps_network.a.id
  cidr       = "10.0.0.0/24"
}

resource "conohavps_network" "b" {}

resource "conohavps_subnet" "b" {
  network_id = conohavps_network.b.id
  cidr       = "172.16.0.0/24"
}

resource "conohavps_port" "test" {
` + port + `
}
`
}

// 本文の中の ID を、状態から引いた値に置き換えて比べる.
func portBody(f *fakeNetworking, method, path, want string, ids map[string]string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		r := want
		for placeholder, addr := range ids {
			rs, ok := s.RootModule().Resources[addr]
			if !ok {
				return fmt.Errorf("%s is not in the state", addr)
			}
			r = regexp.MustCompile(regexp.QuoteMeta(placeholder)).ReplaceAllLiteralString(r, rs.Primary.ID)
		}
		return f.expectBody(method, path, r)(s)
	}
}

var portIDs = map[string]string{
	"{net_a}":    "conohavps_network.a",
	"{net_b}":    "conohavps_network.b",
	"{subnet_a}": "conohavps_subnet.a",
	"{subnet_b}": "conohavps_subnet.b",
}

func TestPortUnit(t *testing.T) {
	f := newFakeNetworking(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		CheckDestroy:             f.checkDestroyed,
		Steps: []resource.TestStep{
			{
				Config: portUnitConfig(f, `
  network_id = conohavps_network.a.id
  fixed_ips = [{
    subnet_id  = conohavps_subnet.a.id
    ip_address = "10.0.0.10"
  }]
  security_group_ids = ["sg-1"]
`),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_port.test", tfjsonpath.New("fixed_ips").AtSliceIndex(0).AtMapKey("ip_address"), knownvalue.StringExact("10.0.0.10")),
					statecheck.ExpectKnownValue("conohavps_port.test", tfjsonpath.New("security_group_ids"), knownvalue.SetExact([]knownvalue.Check{knownvalue.StringExact("sg-1")})),
					statecheck.ExpectKnownValue("conohavps_port.test", tfjsonpath.New("allowed_address_pairs"), knownvalue.SetSizeExact(0)),
					statecheck.ExpectKnownValue("conohavps_port.test", tfjsonpath.New("qos_policy_id"), knownvalue.Null()),
					statecheck.ExpectKnownValue("conohavps_port.test", tfjsonpath.New("name"), knownvalue.StringExact("local-gnct24510032")),
					statecheck.ExpectKnownValue("conohavps_port.test", tfjsonpath.New("mac_address"), knownvalue.StringExact("fa:16:3e:00:00:01")),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					portBody(f, "POST", "/networking/v2.0/ports",
						`{"port":{"network_id":"{net_a}","fixed_ips":[{"subnet_id":"{subnet_a}","ip_address":"10.0.0.10"}],"security_groups":["sg-1"]}}`, portIDs),
					// QoS ポリシーを指定しなければ、作成後の更新はしない
					func(*terraform.State) error {
						if n := f.count("PUT", "/networking/v2.0/ports/.*"); n != 0 {
							t.Errorf("port was updated %d times after creation", n)
						}
						return nil
					},
				),
			},
			{
				// セキュリティグループ・VIP・QoS ポリシーはその場で更新し、変わった項目だけを送る
				Config: portUnitConfig(f, `
  network_id = conohavps_network.a.id
  fixed_ips = [{
    subnet_id  = conohavps_subnet.a.id
    ip_address = "10.0.0.10"
  }]
  security_group_ids    = ["sg-1", "sg-2"]
  allowed_address_pairs = [{ ip_address = "10.0.0.100/32" }]
  qos_policy_id         = "qos-local"
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("conohavps_port.test", plancheck.ResourceActionUpdate),
				}},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_port.test", tfjsonpath.New("security_group_ids"), knownvalue.SetExact([]knownvalue.Check{knownvalue.StringExact("sg-1"), knownvalue.StringExact("sg-2")})),
					statecheck.ExpectKnownValue("conohavps_port.test", tfjsonpath.New("allowed_address_pairs"), knownvalue.SetExact([]knownvalue.Check{
						knownvalue.ObjectExact(map[string]knownvalue.Check{"ip_address": knownvalue.StringExact("10.0.0.100/32")}),
					})),
					statecheck.ExpectKnownValue("conohavps_port.test", tfjsonpath.New("qos_policy_id"), knownvalue.StringExact("qos-local")),
				},
				Check: f.expectBody("PUT", "/networking/v2.0/ports/port-.*",
					`{"port":{"security_groups":["sg-1","sg-2"],"allowed_address_pairs":[{"ip_address":"10.0.0.100/32"}],"qos_policy_id":"qos-local"}}`),
			},
			{
				// IP アドレスもその場で変える. VIP を外すと空の一覧を送る
				Config: portUnitConfig(f, `
  network_id = conohavps_network.a.id
  fixed_ips = [{
    subnet_id  = conohavps_subnet.a.id
    ip_address = "10.0.0.20"
  }, {
    subnet_id = conohavps_subnet.a.id
  }]
  security_group_ids = ["sg-1", "sg-2"]
  qos_policy_id      = "qos-local"
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("conohavps_port.test", plancheck.ResourceActionUpdate),
				}},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_port.test", tfjsonpath.New("fixed_ips").AtSliceIndex(0).AtMapKey("ip_address"), knownvalue.StringExact("10.0.0.20")),
					statecheck.ExpectKnownValue("conohavps_port.test", tfjsonpath.New("fixed_ips").AtSliceIndex(1).AtMapKey("ip_address"), knownvalue.StringRegexp(regexp.MustCompile(`^10\.0\.0\.\d+$`))),
					statecheck.ExpectKnownValue("conohavps_port.test", tfjsonpath.New("allowed_address_pairs"), knownvalue.SetSizeExact(0)),
				},
				Check: portBody(f, "PUT", "/networking/v2.0/ports/port-.*",
					`{"port":{"fixed_ips":[{"subnet_id":"{subnet_a}","ip_address":"10.0.0.20"},{"subnet_id":"{subnet_a}"}],"allowed_address_pairs":[]}}`, portIDs),
			},
			{
				// 自動で割り当てられた IP アドレスを省いた設定は、差分にならない
				Config: portUnitConfig(f, `
  network_id = conohavps_network.a.id
  fixed_ips = [{
    subnet_id  = conohavps_subnet.a.id
    ip_address = "10.0.0.20"
  }, {
    subnet_id = conohavps_subnet.a.id
  }]
  security_group_ids = ["sg-1", "sg-2"]
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				ResourceName:      "conohavps_port.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				// ネットワークを変えると作り直す. fixed_ips を省くと自動で割り当てられる
				Config: portUnitConfig(f, `
  network_id = conohavps_network.b.id
  depends_on = [conohavps_subnet.b]
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("conohavps_port.test", plancheck.ResourceActionDestroyBeforeCreate),
				}},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_port.test", tfjsonpath.New("fixed_ips").AtSliceIndex(0).AtMapKey("ip_address"), knownvalue.StringRegexp(regexp.MustCompile(`^172\.16\.0\.\d+$`))),
					statecheck.ExpectKnownValue("conohavps_port.test", tfjsonpath.New("security_group_ids"), knownvalue.SetExact([]knownvalue.Check{knownvalue.StringExact("sg-default")})),
				},
				Check: portBody(f, "POST", "/networking/v2.0/ports", `{"port":{"network_id":"{net_b}"}}`, portIDs),
			},
		},
	})
}

func TestPortUnit_InvalidArguments(t *testing.T) {
	f := newFakeNetworking(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		Steps: []resource.TestStep{
			{
				Config: portUnitConfig(f, `
  network_id            = conohavps_network.a.id
  allowed_address_pairs = [{ ip_address = "10.0.0.100" }]
`),
				ExpectError: regexp.MustCompile(`is not in CIDR notation`),
			},
			{
				Config: portUnitConfig(f, `
  network_id = conohavps_network.a.id
  fixed_ips  = [{ subnet_id = "x", ip_address = "10.0.0.300" }]
`),
				ExpectError: regexp.MustCompile(`is not an IP address`),
			},
			{
				Config: portUnitConfig(f, `
  network_id         = conohavps_network.a.id
  security_group_ids = []
`),
				ExpectError: regexp.MustCompile(`at least 1`),
			},
		},
	})
}
