// ローカルネットワークのリソースの単体テストと、ネットワーク系のリソースの単体テストで共用する偽の API を提供する.
// 偽の API はネットワーク・サブネット・ポート・追加IP・ポートのアタッチをメモリに持ち、
// ドキュメントのレスポンスの形で返す. 受け取ったリクエストの本文を記録し、テストで確かめる.

package resource_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/fakeapi"
	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// fakeNetworking はネットワーク系の偽の API.
type fakeNetworking struct {
	*fakeapi.Server
	mu          sync.Mutex
	networks    map[string]map[string]any
	subnets     map[string]map[string]any
	ports       map[string]map[string]any
	attachments map[string]string // ポート ID → サーバー ID
	detaching   map[string]int    // デタッチ後、まだ一覧に残して返す回数
	nextHost    map[string]int    // サブネットごとの次に自動で割り当てるホスト番号
	requests    []fakeRequest
}

type fakeRequest struct {
	Method string
	Path   string
	Body   map[string]any
}

func newFakeNetworking(t *testing.T) *fakeNetworking {
	t.Helper()
	service.DetachPollInterval = 0
	f := &fakeNetworking{
		Server:      fakeapi.New(t),
		networks:    map[string]map[string]any{},
		subnets:     map[string]map[string]any{},
		ports:       map[string]map[string]any{},
		attachments: map[string]string{},
		detaching:   map[string]int{},
		nextHost:    map[string]int{},
	}
	h := func(pattern string, fn func(w http.ResponseWriter, r *http.Request, body map[string]any)) {
		f.Mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if !fakeapi.Authorized(w, r) {
				return
			}
			var body map[string]any
			if r.Body != nil {
				if err := fakeapi.ReadJSON(r, &body); err != nil && err.Error() != "EOF" {
					fakeapi.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
					return
				}
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			f.requests = append(f.requests, fakeRequest{Method: r.Method, Path: r.URL.Path, Body: body})
			fn(w, r, body)
		})
	}

	h("POST /networking/v2.0/networks", f.createNetwork)
	h("GET /networking/v2.0/networks/{id}", fakeNetGet(f.networks, "network"))
	h("DELETE /networking/v2.0/networks/{id}", f.deleteNetwork)
	h("POST /networking/v2.0/subnets", f.createSubnet)
	h("GET /networking/v2.0/subnets/{id}", fakeNetGet(f.subnets, "subnet"))
	h("DELETE /networking/v2.0/subnets/{id}", f.deleteSubnet)
	h("POST /networking/v2.0/ports", f.createPort)
	h("POST /networking/v2.0/allocateips", f.allocateIPs)
	h("GET /networking/v2.0/ports/{id}", fakeNetGet(f.ports, "port"))
	h("PUT /networking/v2.0/ports/{id}", f.updatePort)
	h("DELETE /networking/v2.0/ports/{id}", f.deletePort)
	h("POST /compute/v2.1/servers/{server}/os-interface", f.attach)
	h("GET /compute/v2.1/servers/{server}/os-interface/{port}", f.getAttachment)
	h("DELETE /compute/v2.1/servers/{server}/os-interface/{port}", f.detach)
	return f
}

func fakeNetGet(store map[string]map[string]any, key string) func(http.ResponseWriter, *http.Request, map[string]any) {
	return func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		v, ok := store[r.PathValue("id")]
		if !ok {
			fakeapi.WriteJSON(w, http.StatusNotFound, map[string]any{"NeutronError": "not found"})
			return
		}
		fakeapi.WriteJSON(w, http.StatusOK, map[string]any{key: v})
	}
}

func (f *fakeNetworking) createNetwork(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
	id := f.NewID("net")
	n := map[string]any{
		"id": id, "name": "local-gnct24510032-" + id, "tenant_id": fakeapi.TenantID, "admin_state_up": true,
		"mtu": 1450, "status": "ACTIVE", "subnets": []any{}, "shared": false, "project_id": fakeapi.TenantID, "router:external": false,
	}
	f.networks[id] = n
	fakeapi.WriteJSON(w, http.StatusCreated, map[string]any{"network": n})
}

func (f *fakeNetworking) deleteNetwork(w http.ResponseWriter, r *http.Request, _ map[string]any) {
	n, ok := f.networks[r.PathValue("id")]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if len(n["subnets"].([]any)) > 0 {
		fakeapi.WriteJSON(w, http.StatusConflict, map[string]any{"NeutronError": "subnets remain"})
		return
	}
	delete(f.networks, r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

func (f *fakeNetworking) createSubnet(w http.ResponseWriter, _ *http.Request, body map[string]any) {
	req, _ := body["subnet"].(map[string]any)
	netID, _ := req["network_id"].(string)
	n, ok := f.networks[netID]
	if !ok {
		fakeapi.WriteJSON(w, http.StatusBadRequest, map[string]any{"NeutronError": "no network"})
		return
	}
	prefix, err := netip.ParsePrefix(fmt.Sprint(req["cidr"]))
	if err != nil {
		fakeapi.WriteJSON(w, http.StatusBadRequest, map[string]any{"NeutronError": err.Error()})
		return
	}
	id := f.NewID("subnet")
	first := prefix.Addr().Next()
	last := fakeNetLastHost(prefix)
	s := map[string]any{
		"id": id, "name": "local-" + strings.NewReplacer(".", "-", "/", "-").Replace(prefix.String()), "tenant_id": fakeapi.TenantID,
		"network_id": netID, "ip_version": 4, "enable_dhcp": false, "ipv6_ra_mode": nil, "ipv6_address_mode": nil, "gateway_ip": nil,
		"cidr": prefix.String(), "allocation_pools": []any{map[string]any{"start": first.String(), "end": last.String()}},
		"host_routes": []any{}, "dns_nameservers": []any{}, "project_id": fakeapi.TenantID,
	}
	f.subnets[id] = s
	f.nextHost[id] = 100
	n["subnets"] = append(n["subnets"].([]any), id)
	fakeapi.WriteJSON(w, http.StatusCreated, map[string]any{"subnet": s})
}

func fakeNetLastHost(p netip.Prefix) netip.Addr {
	a := p.Addr().As4()
	host := uint32(1)<<(32-p.Bits()) - 1
	v := (uint32(a[0])<<24 | uint32(a[1])<<16 | uint32(a[2])<<8 | uint32(a[3])) | host
	v--
	return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
}

func (f *fakeNetworking) deleteSubnet(w http.ResponseWriter, r *http.Request, _ map[string]any) {
	id := r.PathValue("id")
	s, ok := f.subnets[id]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	for _, p := range f.ports {
		for _, ip := range p["fixed_ips"].([]any) {
			if ip.(map[string]any)["subnet_id"] == id {
				fakeapi.WriteJSON(w, http.StatusConflict, map[string]any{"NeutronError": "ports remain"})
				return
			}
		}
	}
	n := f.networks[s["network_id"].(string)]
	var rest []any
	for _, sid := range n["subnets"].([]any) {
		if sid != id {
			rest = append(rest, sid)
		}
	}
	if rest == nil {
		rest = []any{}
	}
	n["subnets"] = rest
	delete(f.subnets, id)
	w.WriteHeader(http.StatusNoContent)
}

// サブネットから次の IP アドレスを自動で割り当てる.
func (f *fakeNetworking) autoIP(subnetID string) string {
	prefix := netip.MustParsePrefix(f.subnets[subnetID]["cidr"].(string))
	a := prefix.Addr().As4()
	f.nextHost[subnetID]++
	a[3] += byte(f.nextHost[subnetID])
	return netip.AddrFrom4(a).String()
}

func (f *fakeNetworking) fixedIPs(netID string, raw any) ([]any, error) {
	list, _ := raw.([]any)
	if list == nil {
		// fixed_ips を省くと、ネットワークの最初のサブネットから割り当てる
		subnets := f.networks[netID]["subnets"].([]any)
		if len(subnets) == 0 {
			return nil, fmt.Errorf("no subnet")
		}
		list = []any{map[string]any{"subnet_id": subnets[0]}}
	}
	var out []any
	for _, v := range list {
		m := v.(map[string]any)
		sid, _ := m["subnet_id"].(string)
		if _, ok := f.subnets[sid]; !ok {
			return nil, fmt.Errorf("no subnet %s", sid)
		}
		ip, _ := m["ip_address"].(string)
		if ip == "" {
			ip = f.autoIP(sid)
		}
		out = append(out, map[string]any{"subnet_id": sid, "ip_address": ip})
	}
	return out, nil
}

func (f *fakeNetworking) newPort(name, netID string, ips, sgs any) map[string]any {
	if sgs == nil {
		sgs = []any{"sg-default"}
	}
	id := f.NewID("port")
	return map[string]any{
		"id": id, "name": name, "network_id": netID, "tenant_id": fakeapi.TenantID, "mac_address": "fa:16:3e:00:00:01",
		"admin_state_up": true, "status": "DOWN", "device_id": "", "device_owner": "", "fixed_ips": ips,
		"project_id": fakeapi.TenantID, "security_groups": sgs, "allowed_address_pairs": []any{}, "extra_dhcp_opts": []any{},
		"binding:vnic_type": "normal", "qos_policy_id": nil,
	}
}

func (f *fakeNetworking) createPort(w http.ResponseWriter, _ *http.Request, body map[string]any) {
	req, _ := body["port"].(map[string]any)
	netID, _ := req["network_id"].(string)
	if _, ok := f.networks[netID]; !ok {
		fakeapi.WriteJSON(w, http.StatusBadRequest, map[string]any{"NeutronError": "no network"})
		return
	}
	ips, err := f.fixedIPs(netID, req["fixed_ips"])
	if err != nil {
		fakeapi.WriteJSON(w, http.StatusBadRequest, map[string]any{"NeutronError": err.Error()})
		return
	}
	p := f.newPort("local-gnct24510032", netID, ips, req["security_groups"])
	if pairs, ok := req["allowed_address_pairs"].([]any); ok {
		p["allowed_address_pairs"] = withMAC(pairs)
	}
	f.ports[p["id"].(string)] = p
	fakeapi.WriteJSON(w, http.StatusCreated, map[string]any{"port": p})
}

// 実物と同じく、VIP にポートの MAC アドレスを添えて返す.
func withMAC(pairs []any) []any {
	out := []any{}
	for _, v := range pairs {
		out = append(out, map[string]any{"ip_address": v.(map[string]any)["ip_address"], "mac_address": "fa:16:3e:00:00:01"})
	}
	return out
}

func (f *fakeNetworking) allocateIPs(w http.ResponseWriter, _ *http.Request, body map[string]any) {
	req, _ := body["allocateip"].(map[string]any)
	count, _ := req["count"].(float64)
	if count < 1 || count > 16 {
		fakeapi.WriteJSON(w, http.StatusBadRequest, map[string]any{"NeutronError": "count"})
		return
	}
	var ips []any
	for i := 0; i < int(count); i++ {
		f.nextHost["global"]++
		ips = append(ips, map[string]any{"subnet_id": "subnet-global", "ip_address": fmt.Sprintf("203.0.113.%d", f.nextHost["global"])})
	}
	p := f.newPort("add-i_100000-o_100000-p_0a", "fb00d078-8ae1-4145-b3b9-82dfb7596227", ips, req["security_groups"])
	f.ports[p["id"].(string)] = p
	fakeapi.WriteJSON(w, http.StatusCreated, map[string]any{"port": p})
}

func (f *fakeNetworking) updatePort(w http.ResponseWriter, r *http.Request, body map[string]any) {
	p, ok := f.ports[r.PathValue("id")]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	req, _ := body["port"].(map[string]any)
	for k, v := range req {
		switch k {
		case "fixed_ips":
			ips, err := f.fixedIPs(p["network_id"].(string), v)
			if err != nil {
				fakeapi.WriteJSON(w, http.StatusBadRequest, map[string]any{"NeutronError": err.Error()})
				return
			}
			p[k] = ips
		case "allowed_address_pairs":
			p[k] = withMAC(v.([]any))
		case "security_groups", "qos_policy_id":
			p[k] = v
		default:
			fakeapi.WriteJSON(w, http.StatusBadRequest, map[string]any{"NeutronError": "unexpected field " + k})
			return
		}
	}
	fakeapi.WriteJSON(w, http.StatusOK, map[string]any{"port": p})
}

func (f *fakeNetworking) deletePort(w http.ResponseWriter, r *http.Request, _ map[string]any) {
	id := r.PathValue("id")
	p, ok := f.ports[id]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if p["device_id"] != "" {
		fakeapi.WriteJSON(w, http.StatusConflict, map[string]any{"NeutronError": "port is attached"})
		return
	}
	delete(f.ports, id)
	w.WriteHeader(http.StatusNoContent)
}

func (f *fakeNetworking) interfaceAttachment(portID string) map[string]any {
	p := f.ports[portID]
	return map[string]any{"interfaceAttachment": map[string]any{
		"net_id": p["network_id"], "port_id": portID, "mac_addr": p["mac_address"], "port_state": "ACTIVE", "fixed_ips": p["fixed_ips"],
	}}
}

func (f *fakeNetworking) attach(w http.ResponseWriter, r *http.Request, body map[string]any) {
	req, _ := body["interfaceAttachment"].(map[string]any)
	portID, _ := req["port_id"].(string)
	p, ok := f.ports[portID]
	if !ok || p["device_id"] != "" {
		fakeapi.WriteJSON(w, http.StatusBadRequest, map[string]any{"badRequest": "port"})
		return
	}
	p["device_id"] = r.PathValue("server")
	p["device_owner"] = "compute:cell1-az1"
	f.attachments[portID] = r.PathValue("server")
	fakeapi.WriteJSON(w, http.StatusOK, f.interfaceAttachment(portID))
}

func (f *fakeNetworking) getAttachment(w http.ResponseWriter, r *http.Request, _ map[string]any) {
	portID := r.PathValue("port")
	if f.attachments[portID] != r.PathValue("server") {
		fakeapi.WriteJSON(w, http.StatusNotFound, map[string]any{"itemNotFound": "port"})
		return
	}
	// デタッチは非同期のため、しばらくはまだアタッチ済みとして返す
	if f.detaching[portID] > 0 {
		f.detaching[portID]--
		if f.detaching[portID] == 0 {
			f.finishDetach(portID)
			fakeapi.WriteJSON(w, http.StatusNotFound, map[string]any{"itemNotFound": "port"})
			return
		}
	}
	fakeapi.WriteJSON(w, http.StatusOK, f.interfaceAttachment(portID))
}

func (f *fakeNetworking) detach(w http.ResponseWriter, r *http.Request, _ map[string]any) {
	portID := r.PathValue("port")
	if f.attachments[portID] != r.PathValue("server") {
		fakeapi.WriteJSON(w, http.StatusNotFound, map[string]any{"itemNotFound": "port"})
		return
	}
	f.detaching[portID] = 2
	w.WriteHeader(http.StatusAccepted)
}

func (f *fakeNetworking) finishDetach(portID string) {
	delete(f.attachments, portID)
	delete(f.detaching, portID)
	if p, ok := f.ports[portID]; ok {
		p["device_id"] = ""
		p["device_owner"] = ""
	}
}

// 記録したリクエストのうち、method と path の正規表現に合う最後のものの本文を返す.
func (f *fakeNetworking) lastBody(method, pathPattern string) (map[string]any, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	re := regexp.MustCompile("^" + pathPattern + "$")
	for i := len(f.requests) - 1; i >= 0; i-- {
		if r := f.requests[i]; r.Method == method && re.MatchString(r.Path) {
			return r.Body, true
		}
	}
	return nil, false
}

// 記録したリクエストのうち、method と path の正規表現に合うものの本文を JSON で返す.
func (f *fakeNetworking) bodies(method, pathPattern string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	re := regexp.MustCompile("^" + pathPattern + "$")
	var out []string
	for _, r := range f.requests {
		if r.Method == method && re.MatchString(r.Path) {
			b, _ := json.Marshal(r.Body)
			out = append(out, string(b))
		}
	}
	return out
}

// 記録したリクエストのうち、method と path の正規表現に合うものの数を返す.
func (f *fakeNetworking) count(method, pathPattern string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	re := regexp.MustCompile("^" + pathPattern + "$")
	n := 0
	for _, r := range f.requests {
		if r.Method == method && re.MatchString(r.Path) {
			n++
		}
	}
	return n
}

// 最後のリクエストの本文が want（JSON）と一致するか確かめる.
func (f *fakeNetworking) expectBody(method, pathPattern, want string) resource.TestCheckFunc {
	return f.expectBodyFn(method, pathPattern, func() string { return want })
}

// expectBody の、期待する本文を確かめる時点で組み立てるもの（偽の API が振った ID を含む場合に使う）.
func (f *fakeNetworking) expectBodyFn(method, pathPattern string, wantFn func() string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		want := wantFn()
		got, ok := f.lastBody(method, pathPattern)
		if !ok {
			return fmt.Errorf("no %s %s request was made", method, pathPattern)
		}
		var w map[string]any
		if want != "" {
			if err := json.Unmarshal([]byte(want), &w); err != nil {
				return err
			}
		}
		gotJSON, _ := json.Marshal(got)
		wantJSON, _ := json.Marshal(w)
		if string(gotJSON) != string(wantJSON) {
			return fmt.Errorf("%s %s body = %s, want %s", method, pathPattern, gotJSON, wantJSON)
		}
		return nil
	}
}

// 偽の API にあるネットワークの ID を1つ返す（1つだけ作るテストで使う）.
func (f *fakeNetworking) onlyNetworkID() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id := range f.networks {
		return id
	}
	return ""
}

// 偽の API に何も残っていないか確かめる.
func (f *fakeNetworking) checkDestroyed(*terraform.State) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.networks)+len(f.subnets)+len(f.ports)+len(f.attachments) > 0 {
		return fmt.Errorf("resources remain: %d networks, %d subnets, %d ports, %d attachments",
			len(f.networks), len(f.subnets), len(f.ports), len(f.attachments))
	}
	return nil
}

func TestNetworkUnit(t *testing.T) {
	f := newFakeNetworking(t)
	config := f.ProviderConfig() + `
resource "conohavps_network" "test" {}
`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		CheckDestroy:             f.checkDestroyed,
		Steps: []resource.TestStep{
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("conohavps_network.test", plancheck.ResourceActionCreate),
				}},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("conohavps_network.test", tfjsonpath.New("id"), knownvalue.StringRegexp(regexp.MustCompile(`^net-\d+$`))),
					statecheck.ExpectKnownValue("conohavps_network.test", tfjsonpath.New("name"), knownvalue.StringRegexp(regexp.MustCompile(`^local-`))),
					statecheck.ExpectKnownValue("conohavps_network.test", tfjsonpath.New("mtu"), knownvalue.Int64Exact(1450)),
					statecheck.ExpectKnownValue("conohavps_network.test", tfjsonpath.New("status"), knownvalue.StringExact("ACTIVE")),
				},
				// ネットワーク作成（ローカルネットワーク用）は本文を取らない
				Check: f.expectBody("POST", "/networking/v2.0/networks", ""),
			},
			{
				ResourceName:      "conohavps_network.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestNetworkUnit_RecreatedWhenDeletedOutside(t *testing.T) {
	f := newFakeNetworking(t)
	config := f.ProviderConfig() + `resource "conohavps_network" "test" {}`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		CheckDestroy:             f.checkDestroyed,
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig: func() {
					f.mu.Lock()
					defer f.mu.Unlock()
					for id := range f.networks {
						delete(f.networks, id)
					}
				},
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("conohavps_network.test", plancheck.ResourceActionCreate),
				}},
			},
		},
	})
}
