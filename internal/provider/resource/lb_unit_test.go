// ロードバランサー（LBaaS）のリソースのテストを提供する.
// 偽の API（lb_fake_test.go）に対して、ロードバランサー → リスナー → プール → メンバー → ヘルスモニタの一式を
// 作成・その場で更新・作り直し・インポート・依存の順での削除まで、実際の API を使わずに検証する.

package resource_test

import (
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/fakeapi"
	"github.com/hashicorp/terraform-plugin-testing/compare"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// 一式の設定. 引数で変える項目だけを差し替える.
type lbStackConfig struct {
	suffix       string // 名前の末尾（名前の更新に使う）
	listenerPort int
	algorithm    string
	memberPort   int
	memberUp     bool
	monitor      string // ヘルスモニタの type ごとの項目
}

const httpMonitor = `
  type           = "HTTP"
  delay          = 10
  timeout        = 5
  max_retries    = 3
  url_path       = "/health"
  expected_codes = "200"
`

const tcpMonitor = `
  type        = "TCP"
  delay       = 30
  timeout     = 10
  max_retries = 3
`

func (c lbStackConfig) render(f *lbFake) string {
	return f.s.ProviderConfig() + fmt.Sprintf(`
resource "conohavps_lb_loadbalancer" "main" {
  name = "lb%[1]s"
}

resource "conohavps_lb_listener" "main" {
  name            = "listener%[1]s"
  protocol        = "TCP"
  protocol_port   = %[2]d
  loadbalancer_id = conohavps_lb_loadbalancer.main.id
}

resource "conohavps_lb_pool" "main" {
  name         = "pool%[1]s"
  protocol     = "TCP"
  lb_algorithm = %[3]q
  listener_id  = conohavps_lb_listener.main.id
}

resource "conohavps_lb_member" "web1" {
  pool_id       = conohavps_lb_pool.main.id
  name          = "web1"
  address       = "203.0.113.10"
  protocol_port = %[4]d
}

resource "conohavps_lb_member" "web2" {
  pool_id        = conohavps_lb_pool.main.id
  name           = "web2"
  address        = "203.0.113.11"
  protocol_port  = 80
  admin_state_up = %[5]t
}

resource "conohavps_lb_health_monitor" "main" {
  pool_id = conohavps_lb_pool.main.id
  name    = "monitor%[1]s"
%[6]s}
`, c.suffix, c.listenerPort, c.algorithm, c.memberPort, c.memberUp, c.monitor)
}

var lbBase = lbStackConfig{listenerPort: 80, algorithm: "ROUND_ROBIN", memberPort: 80, memberUp: false, monitor: httpMonitor}

func TestLBStack(t *testing.T) {
	f := newLBFake(t)
	// 最初のリスナーの作成を、他の変更の反映中として 1 度だけ 409 で拒否させる（待ってやり直すことを確かめる）
	f.busyOnce = true

	renamed := lbBase
	renamed.suffix = "-renamed"
	renamed.memberUp = true

	algorithmChanged := renamed
	algorithmChanged.algorithm = "LEAST_CONNECTIONS"

	membersReplaced := renamed
	membersReplaced.memberPort = 8080
	membersReplaced.monitor = tcpMonitor

	listenerReplaced := membersReplaced
	listenerReplaced.listenerPort = 443

	var ids map[string]string
	var memberID string
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		CheckDestroy: func(*terraform.State) error {
			if n := f.count(); n != 0 {
				return fmt.Errorf("%d LBaaS objects remain after destroy", n)
			}
			return nil
		},
		Steps: []resource.TestStep{
			// 一式を作成する. web2 は無効で作るので、追加の後に admin_state_up=false で更新される
			{
				Config: lbBase.render(f),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_lb_loadbalancer.main", tfjsonpath.New("vip_address"), knownvalue.StringExact("203.0.113.100")),
					statecheck.ExpectKnownValue("conohavps_lb_loadbalancer.main", tfjsonpath.New("operating_status"), knownvalue.StringExact("ONLINE")),
					statecheck.ExpectKnownValue("conohavps_lb_listener.main", tfjsonpath.New("protocol_port"), knownvalue.Int64Exact(80)),
					statecheck.ExpectKnownValue("conohavps_lb_member.web1", tfjsonpath.New("admin_state_up"), knownvalue.Bool(true)),
					statecheck.ExpectKnownValue("conohavps_lb_member.web1", tfjsonpath.New("weight"), knownvalue.Int64Exact(1)),
					statecheck.ExpectKnownValue("conohavps_lb_member.web2", tfjsonpath.New("admin_state_up"), knownvalue.Bool(false)),
					statecheck.ExpectKnownValue("conohavps_lb_health_monitor.main", tfjsonpath.New("url_path"), knownvalue.StringExact("/health")),
					statecheck.CompareValuePairs("conohavps_lb_pool.main", tfjsonpath.New("loadbalancer_id"),
						"conohavps_lb_loadbalancer.main", tfjsonpath.New("id"), compare.ValuesSame()),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					func(s *terraform.State) error {
						ids = map[string]string{}
						for _, n := range []string{"lb_loadbalancer", "lb_listener", "lb_pool", "lb_member", "lb_health_monitor"} {
							name := "main"
							if n == "lb_member" {
								name = "web2"
							}
							ids[n] = s.RootModule().Resources["conohavps_"+n+"."+name].Primary.ID
						}
						return nil
					},
					f.expectBodies(t, "POST", "/loadbalancers", map[string]any{"loadbalancer": map[string]any{"name": "lb"}}),
					f.expectBodies(t, "POST", "/listeners", map[string]any{"listener": map[string]any{
						"name": "listener", "protocol": "TCP", "protocol_port": float64(80), "loadbalancer_id": "loadbalancer-1",
					}}),
					f.expectBodies(t, "POST", "/pools", map[string]any{"pool": map[string]any{
						"name": "pool", "protocol": "TCP", "lb_algorithm": "ROUND_ROBIN", "listener_id": "listener-2",
					}}),
					f.expectBodies(t, "POST", "/healthmonitors", map[string]any{"healthmonitor": map[string]any{
						"name": "monitor", "pool_id": "pool-3", "type": "HTTP", "delay": float64(10), "timeout": float64(5),
						"max_retries": float64(3), "url_path": "/health", "expected_codes": "200",
					}}),
					// web2 を無効にする更新だけが送られている
					f.expectBodies(t, "PUT", "/pools/pool-3/members", map[string]any{"member": map[string]any{"admin_state_up": false}}),
				),
			},
			// 名前と web2 の有効状態をその場で更新する
			{
				Config: renamed.render(f),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("conohavps_lb_loadbalancer.main", plancheck.ResourceActionUpdate),
					plancheck.ExpectResourceAction("conohavps_lb_listener.main", plancheck.ResourceActionUpdate),
					plancheck.ExpectResourceAction("conohavps_lb_pool.main", plancheck.ResourceActionUpdate),
					plancheck.ExpectResourceAction("conohavps_lb_member.web1", plancheck.ResourceActionNoop),
					plancheck.ExpectResourceAction("conohavps_lb_member.web2", plancheck.ResourceActionUpdate),
					plancheck.ExpectResourceAction("conohavps_lb_health_monitor.main", plancheck.ResourceActionUpdate),
				}},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_lb_loadbalancer.main", tfjsonpath.New("name"), knownvalue.StringExact("lb-renamed")),
					statecheck.ExpectKnownValue("conohavps_lb_pool.main", tfjsonpath.New("name"), knownvalue.StringExact("pool-renamed")),
					statecheck.ExpectKnownValue("conohavps_lb_member.web2", tfjsonpath.New("admin_state_up"), knownvalue.Bool(true)),
					statecheck.ExpectKnownValue("conohavps_lb_health_monitor.main", tfjsonpath.New("name"), knownvalue.StringExact("monitor-renamed")),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					func(s *terraform.State) error {
						for n, id := range ids {
							name := "main"
							if n == "lb_member" {
								name = "web2"
							}
							if got := s.RootModule().Resources["conohavps_"+n+"."+name].Primary.ID; got != id {
								return fmt.Errorf("%s was replaced (%s -> %s), want an in-place update", n, id, got)
							}
						}
						return nil
					},
					f.expectBodies(t, "PUT", "/loadbalancers/", map[string]any{"loadbalancer": map[string]any{"name": "lb-renamed"}}),
					f.expectBodies(t, "PUT", "/listeners/", map[string]any{"listener": map[string]any{"name": "listener-renamed"}}),
					// 名前だけを変えたので、バランシング方式は送らない
					f.expectBodies(t, "PUT", "/pools/pool-3", map[string]any{"pool": map[string]any{"name": "pool-renamed"}}),
					f.expectBodies(t, "PUT", "/healthmonitors/", map[string]any{"healthmonitor": map[string]any{"name": "monitor-renamed"}}),
					f.expectBodies(t, "PUT", "/pools/pool-3/members/", map[string]any{"member": map[string]any{"admin_state_up": true}}),
				),
			},
			// メンバーのいるプールのバランシング方式は API が拒否する
			{
				Config:      algorithmChanged.render(f),
				ExpectError: regexp.MustCompile(`lb_algorithm cannot be changed while\s+the pool has members`),
			},
			// メンバーのポートとヘルスモニタの type は更新できないので作り直す（TCP では url_path 等を送らない）
			{
				Config: membersReplaced.render(f),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("conohavps_lb_member.web1", plancheck.ResourceActionReplace),
					plancheck.ExpectResourceAction("conohavps_lb_member.web2", plancheck.ResourceActionNoop),
					plancheck.ExpectResourceAction("conohavps_lb_health_monitor.main", plancheck.ResourceActionReplace),
					plancheck.ExpectResourceAction("conohavps_lb_pool.main", plancheck.ResourceActionNoop),
				}},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_lb_member.web1", tfjsonpath.New("protocol_port"), knownvalue.Int64Exact(8080)),
					statecheck.ExpectKnownValue("conohavps_lb_health_monitor.main", tfjsonpath.New("type"), knownvalue.StringExact("TCP")),
					statecheck.ExpectKnownValue("conohavps_lb_health_monitor.main", tfjsonpath.New("url_path"), knownvalue.Null()),
				},
				Check: f.expectLastBody(t, "POST", "/healthmonitors", map[string]any{"healthmonitor": map[string]any{
					"name": "monitor-renamed", "pool_id": "pool-3", "type": "TCP", "delay": float64(30), "timeout": float64(10), "max_retries": float64(3),
				}}),
			},
			// リスナーのポートを変えると、リスナーから下がすべて作り直しになる
			{
				Config: listenerReplaced.render(f),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("conohavps_lb_loadbalancer.main", plancheck.ResourceActionNoop),
					plancheck.ExpectResourceAction("conohavps_lb_listener.main", plancheck.ResourceActionReplace),
					plancheck.ExpectResourceAction("conohavps_lb_pool.main", plancheck.ResourceActionReplace),
					plancheck.ExpectResourceAction("conohavps_lb_member.web1", plancheck.ResourceActionReplace),
					plancheck.ExpectResourceAction("conohavps_lb_member.web2", plancheck.ResourceActionReplace),
					plancheck.ExpectResourceAction("conohavps_lb_health_monitor.main", plancheck.ResourceActionReplace),
				}},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_lb_listener.main", tfjsonpath.New("protocol_port"), knownvalue.Int64Exact(443)),
				},
				Check: func(s *terraform.State) error {
					memberID = s.RootModule().Resources["conohavps_lb_member.web1"].Primary.ID
					return nil
				},
			},
			// インポート
			{ResourceName: "conohavps_lb_loadbalancer.main", ImportState: true, ImportStateVerify: true},
			{ResourceName: "conohavps_lb_listener.main", ImportState: true, ImportStateVerify: true},
			{ResourceName: "conohavps_lb_pool.main", ImportState: true, ImportStateVerify: true},
			{ResourceName: "conohavps_lb_health_monitor.main", ImportState: true, ImportStateVerify: true},
			{
				ResourceName: "conohavps_lb_member.web1", ImportState: true, ImportStateVerify: true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					r := s.RootModule().Resources["conohavps_lb_member.web1"].Primary
					return r.Attributes["pool_id"] + "/" + r.ID, nil
				},
			},
			{
				ResourceName: "conohavps_lb_member.web1", ImportState: true,
				ImportStateIdFunc: func(*terraform.State) (string, error) { return memberID, nil },
				ExpectError:       regexp.MustCompile(`<pool_id>/<member_id>`),
			},
		},
	})

	if reqs := f.sent("POST", "/listeners"); len(reqs) < 2 || reqs[0].Status != http.StatusConflict || reqs[1].Status != http.StatusCreated {
		t.Errorf("the listener creation must be retried after a 409 from a busy load balancer, got %+v", reqs)
	}

	// 削除は子から順に行われ、偽の API の「子が残っていれば 409」に一度も当たっていない
	deletes := f.sent("DELETE", "/")
	if len(deletes) == 0 {
		t.Fatal("no DELETE requests were sent")
	}
	for _, r := range deletes {
		if r.Status != http.StatusNoContent && r.Status != http.StatusConflict {
			t.Errorf("DELETE %s returned %d", r.Path, r.Status)
		}
		if strings.Contains(r.Fault, "child resource") {
			t.Errorf("DELETE %s was sent before its children were deleted: %s", r.Path, r.Fault)
		}
	}
}

// 追加したロードバランサーが ERROR になったら、待機を打ち切って失敗させる.
func TestLBLoadBalancerErrorStatus(t *testing.T) {
	f := newLBFake(t)
	f.failNextLoadBalancer = true
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		Steps: []resource.TestStep{{
			Config:      f.s.ProviderConfig() + `resource "conohavps_lb_loadbalancer" "main" { name = "broken" }`,
			ExpectError: regexp.MustCompile(`load\s+balancer\s+loadbalancer-1\s+is\s+in\s+ERROR\s+provisioning\s+status`),
		}},
	})
}

// ドキュメントの制約は plan の時点で弾く.
func TestLBValidation(t *testing.T) {
	f := newLBFake(t)
	monitor := func(body string) string {
		return f.s.ProviderConfig() + `resource "conohavps_lb_health_monitor" "x" {
  pool_id     = "pool"
  name        = "m"
  max_retries = 3
` + body + "\n}\n"
	}
	cases := []struct {
		name, config, want string
	}{
		{"timeout not less than delay", monitor(`type = "TCP"
delay = 10
timeout = 10`), `timeout \(10\) must be less than delay \(10\)`},
		{"delay out of range", monitor(`type = "TCP"
delay = 181
timeout = 10`), `delay value must be between 1 and 180`},
		{"HTTP without url_path", monitor(`type = "HTTP"
delay = 10
timeout = 5
expected_codes = "200"`), `url_path is required when type is HTTP`},
		{"TCP with expected_codes", monitor(`type = "TCP"
delay = 10
timeout = 5
expected_codes = "200"`), `expected_codes can only be set when type is HTTP or HTTPS`},
		{"monitor type", monitor(`type = "UDP-CONNECT"
delay = 10
timeout = 5`), `type value must be one of`},
		{"listener protocol", f.s.ProviderConfig() + `resource "conohavps_lb_listener" "x" {
  name = "l"
  protocol = "HTTP"
  protocol_port = 80
  loadbalancer_id = "lb"
}`, `protocol value must be one of`},
		{"pool algorithm", f.s.ProviderConfig() + `resource "conohavps_lb_pool" "x" {
  name = "p"
  protocol = "TCP"
  lb_algorithm = "SOURCE_IP"
  listener_id = "l"
}`, `lb_algorithm value must be one of`},
		{"private member address", f.s.ProviderConfig() + `resource "conohavps_lb_member" "x" {
  pool_id = "p"
  name = "m"
  address = "192.168.0.10"
  protocol_port = 80
}`, `Attribute must be a global IP address, got: 192.168.0.10`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: fakeapi.Factories,
				Steps: []resource.TestStep{{
					Config:      c.config,
					PlanOnly:    true,
					ExpectError: regexp.MustCompile(c.want),
				}},
			})
		})
	}
	if n := len(f.sent("POST", "/")); n != 0 {
		t.Fatalf("validation errors must stop before any request, got %d POST requests", n)
	}
}

// method と path の接頭辞が一致するリクエストのうち、少なくとも 1 つの本文が want と一致することを確かめる.
func (f *lbFake) expectBodies(t *testing.T, method, pathPrefix string, want map[string]any) resource.TestCheckFunc {
	t.Helper()
	return func(*terraform.State) error {
		reqs := f.sent(method, pathPrefix)
		for _, r := range reqs {
			if reflect.DeepEqual(r.Body, want) {
				return nil
			}
		}
		var got []map[string]any
		for _, r := range reqs {
			got = append(got, r.Body)
		}
		return fmt.Errorf("no %s %s request with body %v; got %v", method, pathPrefix, want, got)
	}
}

// method と path の接頭辞が一致する最後のリクエストの本文が want と一致することを確かめる.
func (f *lbFake) expectLastBody(t *testing.T, method, pathPrefix string, want map[string]any) resource.TestCheckFunc {
	t.Helper()
	return func(*terraform.State) error {
		reqs := f.sent(method, pathPrefix)
		if len(reqs) == 0 {
			return fmt.Errorf("no %s %s request", method, pathPrefix)
		}
		if got := reqs[len(reqs)-1].Body; !reflect.DeepEqual(got, want) {
			return fmt.Errorf("last %s %s body = %v, want %v", method, pathPrefix, got, want)
		}
		return nil
	}
}
