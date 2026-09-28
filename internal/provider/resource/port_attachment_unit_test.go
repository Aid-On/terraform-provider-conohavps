// サーバーへのポートのアタッチのリソースの単体テストを提供する.
// 偽の API に対してアタッチ・付け直し・インポート・デタッチ（非同期の完了待ち）を確かめる.

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

func portAttachmentUnitConfig(f *fakeNetworking, serverID string) string {
	return f.ProviderConfig() + `
resource "conohavps_network" "test" {}

resource "conohavps_subnet" "test" {
  network_id = conohavps_network.test.id
  cidr       = "10.0.0.0/24"
}

resource "conohavps_port" "test" {
  network_id = conohavps_network.test.id
  fixed_ips  = [{ subnet_id = conohavps_subnet.test.id, ip_address = "10.0.0.5" }]
}

resource "conohavps_additional_ip" "test" {
  ip_count = 1
}

resource "conohavps_port_attachment" "local" {
  server_id = "` + serverID + `"
  port_id   = conohavps_port.test.id
}

resource "conohavps_port_attachment" "additional" {
  server_id = "` + serverID + `"
  port_id   = conohavps_additional_ip.test.id
}
`
}

func TestPortAttachmentUnit(t *testing.T) {
	f := newFakeNetworking(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		// ポートの削除はデタッチが終わってからでないと失敗するため、全部消えていればデタッチを待てている
		CheckDestroy: f.checkDestroyed,
		Steps: []resource.TestStep{
			{
				Config: portAttachmentUnitConfig(f, "server-1"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_port_attachment.local", tfjsonpath.New("ip_addresses"), knownvalue.ListExact([]knownvalue.Check{knownvalue.StringExact("10.0.0.5")})),
					statecheck.ExpectKnownValue("conohavps_port_attachment.local", tfjsonpath.New("id"), knownvalue.StringRegexp(regexp.MustCompile(`^server-1/port-\d+$`))),
					statecheck.ExpectKnownValue("conohavps_port_attachment.local", tfjsonpath.New("mac_address"), knownvalue.StringExact("fa:16:3e:00:00:01")),
					statecheck.ExpectKnownValue("conohavps_port_attachment.additional", tfjsonpath.New("network_id"), knownvalue.StringExact("fb00d078-8ae1-4145-b3b9-82dfb7596227")),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("conohavps_port_attachment.local", "network_id", "conohavps_network.test", "id"),
					// 本文は port_id だけ. 2つのポートを1回ずつアタッチする
					func(s *terraform.State) error {
						want := map[string]bool{
							fmt.Sprintf(`{"interfaceAttachment":{"port_id":"%s"}}`, s.RootModule().Resources["conohavps_port.test"].Primary.ID):          true,
							fmt.Sprintf(`{"interfaceAttachment":{"port_id":"%s"}}`, s.RootModule().Resources["conohavps_additional_ip.test"].Primary.ID): true,
						}
						got := f.bodies("POST", "/compute/v2.1/servers/server-1/os-interface")
						if len(got) != 2 || !want[got[0]] || !want[got[1]] || got[0] == got[1] {
							return fmt.Errorf("attach bodies = %v, want %v", got, want)
						}
						return nil
					},
				),
			},
			{
				ResourceName:      "conohavps_port_attachment.local",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				// サーバーを変えると、デタッチしてから付け直す
				Config: portAttachmentUnitConfig(f, "server-2"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("conohavps_port_attachment.local", plancheck.ResourceActionDestroyBeforeCreate),
					plancheck.ExpectResourceAction("conohavps_port.test", plancheck.ResourceActionNoop),
				}},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_port_attachment.local", tfjsonpath.New("server_id"), knownvalue.StringExact("server-2")),
				},
				Check: func(*terraform.State) error {
					if n := f.count("DELETE", "/compute/v2.1/servers/server-1/os-interface/port-.*"); n != 2 {
						return fmt.Errorf("detached from server-1 %d times, want 2", n)
					}
					// デタッチの後、アタッチ済みポートから消えるまで詳細取得を繰り返している
					if n := f.count("GET", "/compute/v2.1/servers/server-1/os-interface/port-.*"); n < 4 {
						return fmt.Errorf("polled the detached ports %d times, want at least 4", n)
					}
					return nil
				},
			},
		},
	})
}

func TestPortAttachmentUnit_InvalidImportID(t *testing.T) {
	f := newFakeNetworking(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		Steps: []resource.TestStep{
			{
				Config: f.ProviderConfig() + `
resource "conohavps_port_attachment" "x" {
  server_id = "server-1"
  port_id   = "port-1"
}
`,
				ResourceName:  "conohavps_port_attachment.x",
				ImportState:   true,
				ImportStateId: "server-1",
				ExpectError:   regexp.MustCompile(`<server_id>/<port_id>`),
			},
		},
	})
}
