// QoS ポリシーのデータソースの単体テストを提供する.
// 偽の ConoHa API に QoS ポリシーの一覧と詳細を足し、名前（クエリの name で絞る）と ID で引けることと、
// 見つからない名前・ID が失敗することを確かめる.

package datasource_test

import (
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"sync"
	"testing"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/fakeapi"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

func qosPolicy(id, name, desc string, kbps int) map[string]any {
	rule := func(dir string) map[string]any {
		return map[string]any{"max_kbps": kbps, "max_burst_kbps": kbps, "direction": dir, "id": id + "-" + dir, "qos_policy_id": id, "type": "bandwidth_limit"}
	}
	return map[string]any{
		"id": id, "project_id": fakeapi.TenantID, "name": name, "shared": true,
		"rules":      []any{rule("egress"), rule("ingress")},
		"is_default": false, "revision_number": 3, "description": desc,
		"created_at": "2024-08-23T02:16:24Z", "updated_at": "2024-09-05T06:32:13Z", "tenant_id": fakeapi.TenantID,
		"tags": []any{"billing_flag=true"},
	}
}

// fakeQoS は QoS ポリシーの偽の API. 一覧で受け取ったクエリ文字列を記録する.
type fakeQoS struct {
	*fakeapi.Server
	mu      sync.Mutex
	queries []string
}

func withQoSPolicies(t *testing.T) *fakeQoS {
	f := &fakeQoS{Server: fakeapi.New(t)}
	policies := []map[string]any{
		qosPolicy("qos-100", "global-i_100000-o_100000", "Global: In 100.0 Mbps / Out 100.0 Mbps", 100000),
		qosPolicy("qos-300", "global-i_300000-o_300000", "Global: In 300.0 Mbps / Out 300.0 Mbps", 300000),
		qosPolicy("qos-dup-1", "dup", "", 512),
		qosPolicy("qos-dup-2", "dup", "", 512),
	}
	// 実物と同じく、クエリの name で完全一致に絞る
	f.Mux.HandleFunc("GET /networking/v2.0/qos/policies", func(w http.ResponseWriter, r *http.Request) {
		if !fakeapi.Authorized(w, r) {
			return
		}
		f.mu.Lock()
		f.queries = append(f.queries, r.URL.RawQuery)
		f.mu.Unlock()
		name := r.URL.Query().Get("name")
		out := []any{}
		for _, p := range policies {
			if name == "" || p["name"] == name {
				out = append(out, p)
			}
		}
		fakeapi.WriteJSON(w, http.StatusOK, map[string]any{"policies": out})
	})
	f.Mux.HandleFunc("GET /networking/v2.0/qos/policies/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !fakeapi.Authorized(w, r) {
			return
		}
		for _, p := range policies {
			if p["id"] == r.PathValue("id") {
				fakeapi.WriteJSON(w, http.StatusOK, map[string]any{"policy": p})
				return
			}
		}
		fakeapi.WriteJSON(w, http.StatusNotFound, map[string]any{"NeutronError": map[string]any{"type": "QosPolicyNotFound"}})
	})
	return f
}

// 一覧の API に送ったクエリ文字列が want だけか確かめる.
func (f *fakeQoS) expectQueries(want ...string) func(*terraform.State) error {
	return func(*terraform.State) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, q := range f.queries {
			if !slices.Contains(want, q) {
				return fmt.Errorf("list QoS policies was called with query %q, want one of %q", q, want)
			}
		}
		if len(f.queries) == 0 {
			return fmt.Errorf("list QoS policies was not called")
		}
		return nil
	}
}

func TestQoSPolicyByName(t *testing.T) {
	s := withQoSPolicies(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		Steps: []resource.TestStep{{
			Config: s.ProviderConfig() + `
data "conohavps_qos_policy" "fast" {
  name = "global-i_300000-o_300000"
}
`,
			// 名前はクエリの name で API に絞らせる
			Check: s.expectQueries("name=global-i_300000-o_300000"),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("data.conohavps_qos_policy.fast", tfjsonpath.New("id"), knownvalue.StringExact("qos-300")),
				statecheck.ExpectKnownValue("data.conohavps_qos_policy.fast", tfjsonpath.New("tags"), knownvalue.ListExact([]knownvalue.Check{knownvalue.StringExact("billing_flag=true")})),
				statecheck.ExpectKnownValue("data.conohavps_qos_policy.fast", tfjsonpath.New("description"), knownvalue.StringExact("Global: In 300.0 Mbps / Out 300.0 Mbps")),
				statecheck.ExpectKnownValue("data.conohavps_qos_policy.fast", tfjsonpath.New("shared"), knownvalue.Bool(true)),
				statecheck.ExpectKnownValue("data.conohavps_qos_policy.fast", tfjsonpath.New("is_default"), knownvalue.Bool(false)),
				statecheck.ExpectKnownValue("data.conohavps_qos_policy.fast", tfjsonpath.New("rules"), knownvalue.ListExact([]knownvalue.Check{
					knownvalue.ObjectExact(map[string]knownvalue.Check{
						"id": knownvalue.StringExact("qos-300-egress"), "type": knownvalue.StringExact("bandwidth_limit"), "direction": knownvalue.StringExact("egress"),
						"max_kbps": knownvalue.Int64Exact(300000), "max_burst_kbps": knownvalue.Int64Exact(300000),
					}),
					knownvalue.ObjectExact(map[string]knownvalue.Check{
						"id": knownvalue.StringExact("qos-300-ingress"), "type": knownvalue.StringExact("bandwidth_limit"), "direction": knownvalue.StringExact("ingress"),
						"max_kbps": knownvalue.Int64Exact(300000), "max_burst_kbps": knownvalue.Int64Exact(300000),
					}),
				})),
			},
		}},
	})
}

func TestQoSPolicyUnknownOrAmbiguousNameFails(t *testing.T) {
	s := withQoSPolicies(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		Steps: []resource.TestStep{
			{
				Config:      s.ProviderConfig() + `data "conohavps_qos_policy" "x" { name = "global-i_9-o_9" }`,
				ExpectError: regexp.MustCompile(`no QoS policy is named "global-i_9-o_9"`),
			},
			{
				Config:      s.ProviderConfig() + `data "conohavps_qos_policy" "x" { name = "dup" }`,
				ExpectError: regexp.MustCompile(`2 QoS policies are named "dup"`),
			},
		},
	})
}

func TestQoSPolicyByID(t *testing.T) {
	s := withQoSPolicies(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		Steps: []resource.TestStep{
			{
				Config: s.ProviderConfig() + `
data "conohavps_qos_policy" "x" {
  id = "qos-100"
}
`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("data.conohavps_qos_policy.x", tfjsonpath.New("name"), knownvalue.StringExact("global-i_100000-o_100000")),
					statecheck.ExpectKnownValue("data.conohavps_qos_policy.x", tfjsonpath.New("rules"), knownvalue.ListSizeExact(2)),
				},
			},
			{
				Config:      s.ProviderConfig() + `data "conohavps_qos_policy" "x" { id = "qos-missing" }`,
				ExpectError: regexp.MustCompile(`no QoS policy has the ID "qos-missing"`),
			},
		},
	})
}

func TestQoSPolicyNeedsExactlyOneOfNameAndID(t *testing.T) {
	s := withQoSPolicies(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		Steps: []resource.TestStep{
			{
				Config:      s.ProviderConfig() + `data "conohavps_qos_policy" "x" {}`,
				ExpectError: regexp.MustCompile(`Exactly one of these attributes must be configured: \[name,id\]`),
			},
			{
				Config: s.ProviderConfig() + `data "conohavps_qos_policy" "x" {
  name = "dup"
  id   = "qos-dup-1"
}`,
				ExpectError: regexp.MustCompile(`Exactly one of these attributes must be configured: \[name,id\]`),
			},
		},
	})
}
