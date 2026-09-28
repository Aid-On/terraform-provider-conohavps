// ポートのレスポンスの読み方の単体テストを提供する.

package service_test

import (
	"encoding/json"
	"testing"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
)

// qos_policy_id は、値・null・項目ごと無いの3通りを区別して読む.
func TestPortQoSPolicyIDPresence(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		has     bool
		want    *string
		network *string
	}{
		{"value", `{"id":"p","qos_policy_id":"qos-1","qos_network_policy_id":"qos-net"}`, true, localNetPtr("qos-1"), localNetPtr("qos-net")},
		{"null", `{"id":"p","qos_policy_id":null,"qos_network_policy_id":null}`, true, nil, nil},
		{"absent", `{"id":"p"}`, false, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var p service.Port
			if err := json.Unmarshal([]byte(tt.body), &p); err != nil {
				t.Fatal(err)
			}
			if p.ID != "p" {
				t.Errorf("ID = %q, want p", p.ID)
			}
			if p.HasQoSPolicyID != tt.has {
				t.Errorf("HasQoSPolicyID = %v, want %v", p.HasQoSPolicyID, tt.has)
			}
			if !localNetEqual(p.QoSPolicyID, tt.want) {
				t.Errorf("QoSPolicyID = %v, want %v", localNetDeref(p.QoSPolicyID), localNetDeref(tt.want))
			}
			if !localNetEqual(p.QoSNetworkPolicyID, tt.network) {
				t.Errorf("QoSNetworkPolicyID = %v, want %v", localNetDeref(p.QoSNetworkPolicyID), localNetDeref(tt.network))
			}
		})
	}
}

// allowed_address_pairs は、Neutron のオブジェクトの形と OpenAPI 定義の文字列の形のどちらも読む.
func TestPortAllowedAddressPairsForms(t *testing.T) {
	var p service.Port
	body := `{"allowed_address_pairs":[{"ip_address":"10.0.0.100/32","mac_address":"fa:16:3e:00:00:01"},"10.0.0.101"]}`
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatal(err)
	}
	if len(p.AllowedAddressPairs) != 2 || p.AllowedAddressPairs[0].IPAddress != "10.0.0.100/32" || p.AllowedAddressPairs[1].IPAddress != "10.0.0.101" {
		t.Errorf("AllowedAddressPairs = %+v", p.AllowedAddressPairs)
	}

	// リクエストは ip_address だけを送る
	b, _ := json.Marshal(service.PortAddressPair{IPAddress: "10.0.0.100"})
	if string(b) != `{"ip_address":"10.0.0.100"}` {
		t.Errorf("request = %s", b)
	}
}

func localNetPtr(s string) *string { return &s }

func localNetEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func localNetDeref(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}
