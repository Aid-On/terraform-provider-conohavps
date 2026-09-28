// ロードバランサー（LBaaS）の偽の API を提供する.
// オブジェクトをメモリに持ち、ドキュメントの応答の形で返す. provisioning_status は、変更の直後の
// 1 回の読み取りだけ PENDING_* を返してから ACTIVE（削除なら 404）に移り、その間は親のロードバランサーも
// PENDING_UPDATE にして、配下の変更を 409 で拒否する. リクエストの本文はドキュメントの項目と突き合わせ、
// 足りない項目や余計な項目があれば 400 を返す.

package resource_test

import (
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/fakeapi"
)

const lbAPI = "/lbaas/v2.0/lbaas"

type lbObj struct {
	kind   string // loadbalancer・listener・pool・member・healthmonitor
	parent string // 親の ID（ロードバランサーには無い）
	lbID   string // 属するロードバランサーの ID
	fields map[string]any
	// 次の読み取りで状態を進めるか. 変更の直後の 1 回の読み取りだけ PENDING_* を見せる
	pending  bool
	deleting bool
}

type lbRequest struct {
	Method string
	Path   string
	Body   map[string]any
	Status int
	Fault  string
}

type lbFake struct {
	s        *fakeapi.Server
	mu       sync.Mutex
	objs     map[string]*lbObj
	requests []lbRequest
	// 次に追加するロードバランサーを ERROR にする
	failNextLoadBalancer bool
	// 次の配下の変更を、ロードバランサーの変更中として 1 度だけ 409 で拒否する
	busyOnce bool
}

func newLBFake(t *testing.T) *lbFake {
	f := &lbFake{s: fakeapi.New(t), objs: map[string]*lbObj{}}
	m := f.s.Mux
	m.HandleFunc("POST "+lbAPI+"/loadbalancers", f.handle(f.createLoadBalancer))
	m.HandleFunc("POST "+lbAPI+"/listeners", f.handle(f.createListener))
	m.HandleFunc("POST "+lbAPI+"/pools", f.handle(f.createPool))
	m.HandleFunc("POST "+lbAPI+"/pools/{pool}/members", f.handle(f.createMember))
	m.HandleFunc("POST "+lbAPI+"/healthmonitors", f.handle(f.createHealthMonitor))
	for _, c := range []struct{ coll, kind string }{
		{"loadbalancers", "loadbalancer"},
		{"listeners", "listener"},
		{"pools", "pool"},
		{"healthmonitors", "healthmonitor"},
	} {
		m.HandleFunc("GET "+lbAPI+"/"+c.coll+"/{id}", f.handle(f.get(c.kind)))
		m.HandleFunc("PUT "+lbAPI+"/"+c.coll+"/{id}", f.handle(f.update(c.kind)))
		m.HandleFunc("DELETE "+lbAPI+"/"+c.coll+"/{id}", f.handle(f.remove(c.kind)))
	}
	m.HandleFunc("GET "+lbAPI+"/pools/{pool}/members/{id}", f.handle(f.get("member")))
	m.HandleFunc("PUT "+lbAPI+"/pools/{pool}/members/{id}", f.handle(f.update("member")))
	m.HandleFunc("DELETE "+lbAPI+"/pools/{pool}/members/{id}", f.handle(f.remove("member")))
	return f
}

type lbHandler func(r *http.Request, body map[string]any) (int, any)

// 認証とロック、本文の読み取りと記録をまとめる.
func (f *lbFake) handle(h lbHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !fakeapi.Authorized(w, r) {
			return
		}
		var body map[string]any
		if r.Method == http.MethodPost || r.Method == http.MethodPut {
			if err := fakeapi.ReadJSON(r, &body); err != nil {
				fakeapi.WriteJSON(w, http.StatusBadRequest, map[string]any{"faultstring": err.Error()})
				return
			}
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		status, out := h(r, body)
		if r.Method != http.MethodGet {
			req := lbRequest{Method: r.Method, Path: strings.TrimPrefix(r.URL.Path, lbAPI), Body: body, Status: status}
			if m, ok := out.(map[string]any); ok {
				req.Fault, _ = m["faultstring"].(string)
			}
			f.requests = append(f.requests, req)
		}
		if out == nil {
			w.WriteHeader(status)
			return
		}
		fakeapi.WriteJSON(w, status, out)
	}
}

func fault(status int, format string, args ...any) (int, any) {
	return status, map[string]any{"faultcode": http.StatusText(status), "faultstring": fmt.Sprintf(format, args...)}
}

// 本文が {wrapper: {...}} の形で、required をすべて含み、required と optional 以外を含まないことを確かめる.
func bodyOf(body map[string]any, wrapper string, required []string, optional ...string) (map[string]any, error) {
	inner, ok := body[wrapper].(map[string]any)
	if !ok || len(body) != 1 {
		return nil, fmt.Errorf("body must be {%q: {...}}, got %v", wrapper, body)
	}
	for _, k := range required {
		if _, ok := inner[k]; !ok {
			return nil, fmt.Errorf("%s.%s is required", wrapper, k)
		}
	}
	for k := range inner {
		if !slices.Contains(required, k) && !slices.Contains(optional, k) {
			return nil, fmt.Errorf("%s.%s is not a documented parameter", wrapper, k)
		}
	}
	return inner, nil
}

func (f *lbFake) lbBusy(lbID string) bool {
	if f.busyOnce {
		f.busyOnce = false
		return true
	}
	lb, ok := f.objs[lbID]
	return ok && strings.HasPrefix(lb.fields["provisioning_status"].(string), "PENDING_")
}

// 配下のリソースが変わったので、ロードバランサーを PENDING_UPDATE にする. 子が ACTIVE になると戻る.
func (f *lbFake) touch(o *lbObj, status string) {
	o.fields["provisioning_status"] = status
	o.pending = true
	if lb, ok := f.objs[o.lbID]; ok && o.kind != "loadbalancer" {
		lb.fields["provisioning_status"] = "PENDING_UPDATE"
	}
}

func (f *lbFake) add(kind, parent, lbID string, fields map[string]any) *lbObj {
	id := f.s.NewID(kind)
	fields["id"] = id
	fields["project_id"] = fakeapi.TenantID
	fields["tenant_id"] = fakeapi.TenantID
	fields["admin_state_up"] = true
	fields["operating_status"] = "OFFLINE"
	o := &lbObj{kind: kind, parent: parent, lbID: lbID, fields: fields}
	f.objs[id] = o
	f.touch(o, "PENDING_CREATE")
	return o
}

func (f *lbFake) children(id string) []*lbObj {
	var out []*lbObj
	for _, o := range f.objs {
		if o.parent == id {
			out = append(out, o)
		}
	}
	return out
}

func (f *lbFake) createLoadBalancer(_ *http.Request, body map[string]any) (int, any) {
	in, err := bodyOf(body, "loadbalancer", []string{"name"})
	if err != nil {
		return fault(400, "%s", err)
	}
	o := f.add("loadbalancer", "", "", map[string]any{
		"name": in["name"], "description": "",
		"vip_address": "203.0.113.100", "vip_port_id": "port-vip", "vip_subnet_id": "subnet-vip", "vip_network_id": "network-vip",
	})
	o.lbID = o.fields["id"].(string)
	if f.failNextLoadBalancer {
		f.failNextLoadBalancer = false
		o.fields["provisioning_status"] = "ERROR"
		o.pending = false
	}
	return 201, map[string]any{"loadbalancer": f.view(o)}
}

func (f *lbFake) createListener(_ *http.Request, body map[string]any) (int, any) {
	in, err := bodyOf(body, "listener", []string{"protocol", "protocol_port", "loadbalancer_id", "name"})
	if err != nil {
		return fault(400, "%s", err)
	}
	lbID, _ := in["loadbalancer_id"].(string)
	if _, ok := f.objs[lbID]; !ok {
		return fault(404, "load balancer %s not found", lbID)
	}
	if f.lbBusy(lbID) {
		return fault(409, "Load Balancer %s is immutable and cannot be updated.", lbID)
	}
	for _, l := range f.children(lbID) {
		if l.fields["protocol_port"] == in["protocol_port"] {
			return fault(409, "Another Listener on this Load Balancer is already using protocol_port %v", in["protocol_port"])
		}
	}
	o := f.add("listener", lbID, lbID, map[string]any{
		"name": in["name"], "description": "", "protocol": in["protocol"], "protocol_port": in["protocol_port"],
		"connection_limit": -1, "default_pool_id": nil,
	})
	return 201, map[string]any{"listener": f.view(o)}
}

func (f *lbFake) createPool(_ *http.Request, body map[string]any) (int, any) {
	in, err := bodyOf(body, "pool", []string{"lb_algorithm", "protocol", "listener_id", "name"})
	if err != nil {
		return fault(400, "%s", err)
	}
	listenerID, _ := in["listener_id"].(string)
	l, ok := f.objs[listenerID]
	if !ok {
		return fault(404, "listener %s not found", listenerID)
	}
	if f.lbBusy(l.lbID) {
		return fault(409, "Load Balancer %s is immutable and cannot be updated.", l.lbID)
	}
	o := f.add("pool", listenerID, l.lbID, map[string]any{
		"name": in["name"], "description": "", "protocol": in["protocol"], "lb_algorithm": in["lb_algorithm"],
	})
	l.fields["default_pool_id"] = o.fields["id"]
	return 201, map[string]any{"pool": f.view(o)}
}

func (f *lbFake) createMember(r *http.Request, body map[string]any) (int, any) {
	in, err := bodyOf(body, "member", []string{"name", "address", "protocol_port"})
	if err != nil {
		return fault(400, "%s", err)
	}
	p, ok := f.objs[r.PathValue("pool")]
	if !ok || p.kind != "pool" {
		return fault(404, "pool not found")
	}
	if f.lbBusy(p.lbID) {
		return fault(409, "Load Balancer %s is immutable and cannot be updated.", p.lbID)
	}
	o := f.add("member", p.fields["id"].(string), p.lbID, map[string]any{
		"name": in["name"], "address": in["address"], "protocol_port": in["protocol_port"], "weight": 1,
	})
	return 201, map[string]any{"member": f.view(o)}
}

func (f *lbFake) createHealthMonitor(_ *http.Request, body map[string]any) (int, any) {
	in, err := bodyOf(body, "healthmonitor", []string{"name", "pool_id", "delay", "max_retries", "timeout", "type"}, "url_path", "expected_codes")
	if err != nil {
		return fault(400, "%s", err)
	}
	typ, _ := in["type"].(string)
	_, hasPath := in["url_path"]
	_, hasCodes := in["expected_codes"]
	if isHTTP := typ == "HTTP" || typ == "HTTPS"; isHTTP != hasPath || isHTTP != hasCodes {
		return fault(400, "url_path and expected_codes must be sent exactly for HTTP/HTTPS, type %s", typ)
	}
	poolID, _ := in["pool_id"].(string)
	p, ok := f.objs[poolID]
	if !ok {
		return fault(404, "pool %s not found", poolID)
	}
	if f.lbBusy(p.lbID) {
		return fault(409, "Load Balancer %s is immutable and cannot be updated.", p.lbID)
	}
	for _, c := range f.children(poolID) {
		if c.kind == "healthmonitor" {
			return fault(409, "This pool already has a health monitor")
		}
	}
	o := f.add("healthmonitor", poolID, p.lbID, map[string]any{
		"name": in["name"], "type": typ, "delay": in["delay"], "timeout": in["timeout"], "max_retries": in["max_retries"],
		"url_path": in["url_path"], "expected_codes": in["expected_codes"],
	})
	return 201, map[string]any{"healthmonitor": f.view(o)}
}

// 取得. 変更の直後の 1 回だけ PENDING_* を返し、その後 ACTIVE に移る（削除中なら消える）.
func (f *lbFake) get(kind string) lbHandler {
	return func(r *http.Request, _ map[string]any) (int, any) {
		o, ok := f.find(kind, r)
		if !ok {
			return fault(404, "%s not found", kind)
		}
		out := map[string]any{kind: f.view(o)}
		if o.pending {
			o.pending = false
			if o.deleting {
				delete(f.objs, o.fields["id"].(string))
			} else {
				o.fields["provisioning_status"] = "ACTIVE"
				o.fields["operating_status"] = "ONLINE"
			}
			if lb, ok := f.objs[o.lbID]; ok && o.kind != "loadbalancer" {
				lb.fields["provisioning_status"] = "ACTIVE"
			}
		}
		return 200, out
	}
}

func (f *lbFake) update(kind string) lbHandler {
	return func(r *http.Request, body map[string]any) (int, any) {
		o, ok := f.find(kind, r)
		if !ok {
			return fault(404, "%s not found", kind)
		}
		var in map[string]any
		var err error
		switch kind {
		case "pool":
			in, err = bodyOf(body, kind, nil, "lb_algorithm", "name")
		case "member":
			in, err = bodyOf(body, kind, []string{"admin_state_up"})
			if _, isBool := in["admin_state_up"].(bool); err == nil && !isBool {
				err = fmt.Errorf("admin_state_up must be a boolean")
			}
		default:
			in, err = bodyOf(body, kind, []string{"name"})
		}
		if err != nil {
			return fault(400, "%s", err)
		}
		if f.lbBusy(o.lbID) {
			return fault(409, "Load Balancer %s is immutable and cannot be updated.", o.lbID)
		}
		if _, ok := in["lb_algorithm"]; ok && len(f.members(o)) > 0 {
			return fault(409, "lb_algorithm cannot be changed while the pool has members")
		}
		for k, v := range in {
			o.fields[k] = v
		}
		f.touch(o, "PENDING_UPDATE")
		return 200, map[string]any{kind: f.view(o)}
	}
}

// 削除. 子が残っているロードバランサー・リスナー・プールは 409 で拒否する（Terraform が依存の順に消すことを確かめる）.
func (f *lbFake) remove(kind string) lbHandler {
	return func(r *http.Request, _ map[string]any) (int, any) {
		o, ok := f.find(kind, r)
		if !ok {
			return fault(404, "%s not found", kind)
		}
		if kind == "loadbalancer" {
			if strings.HasPrefix(o.fields["provisioning_status"].(string), "PENDING_") {
				return fault(409, "Load Balancer %s is immutable and cannot be updated.", o.lbID)
			}
		} else if f.lbBusy(o.lbID) {
			return fault(409, "Load Balancer %s is immutable and cannot be updated.", o.lbID)
		}
		if c := f.children(o.fields["id"].(string)); len(c) > 0 {
			return fault(409, "%s %s still has %d child resource(s)", kind, o.fields["id"], len(c))
		}
		o.deleting = true
		f.touch(o, "PENDING_DELETE")
		return 204, nil
	}
}

func (f *lbFake) find(kind string, r *http.Request) (*lbObj, bool) {
	o, ok := f.objs[r.PathValue("id")]
	if !ok || o.kind != kind {
		return nil, false
	}
	if kind == "member" && o.parent != r.PathValue("pool") {
		return nil, false
	}
	return o, true
}

func (f *lbFake) members(pool *lbObj) []*lbObj {
	var out []*lbObj
	for _, c := range f.children(pool.fields["id"].(string)) {
		if c.kind == "member" {
			out = append(out, c)
		}
	}
	return out
}

// ドキュメントの応答の形にする（紐づくリソースは [{"id": ...}] で返す）.
func (f *lbFake) view(o *lbObj) map[string]any {
	v := map[string]any{}
	for k, x := range o.fields {
		v[k] = x
	}
	refs := func(ids ...string) []map[string]any {
		out := []map[string]any{}
		for _, id := range ids {
			out = append(out, map[string]any{"id": id})
		}
		return out
	}
	idsOf := func(parent, kind string) []string {
		var ids []string
		for _, c := range f.children(parent) {
			if c.kind == kind {
				ids = append(ids, c.fields["id"].(string))
			}
		}
		sort.Strings(ids)
		return ids
	}
	id := o.fields["id"].(string)
	switch o.kind {
	case "loadbalancer":
		listeners := idsOf(id, "listener")
		var pools []string
		for _, l := range listeners {
			pools = append(pools, idsOf(l, "pool")...)
		}
		v["listeners"], v["pools"] = refs(listeners...), refs(pools...)
	case "listener":
		v["loadbalancers"] = refs(o.lbID)
	case "pool":
		v["loadbalancers"], v["listeners"], v["members"] = refs(o.lbID), refs(o.parent), refs(idsOf(id, "member")...)
	case "healthmonitor":
		v["pools"] = refs(o.parent)
	}
	return v
}

// 記録したリクエストのうち、method と path の接頭辞が一致するものを返す.
func (f *lbFake) sent(method, pathPrefix string) []lbRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []lbRequest
	for _, r := range f.requests {
		if r.Method == method && strings.HasPrefix(r.Path, pathPrefix) {
			out = append(out, r)
		}
	}
	return out
}

func (f *lbFake) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.objs)
}
