// ロードバランサー（LBaaS）の偽の API を提供する.
// オブジェクトをメモリに持ち、GMO の OpenAPI 仕様（conoha_vps_openapi）の応答スキーマの形で返す.
// provisioning_status は、変更の直後の 1 回の読み取りだけ PENDING_* を返してから ACTIVE（削除なら 404）に移り、
// その間は親のロードバランサーも PENDING_UPDATE にして、配下の変更を 409 で拒否する.
// リクエストの本文は仕様の要求スキーマと突き合わせ、足りない項目・余計な項目・型や値の誤りがあれば 400 を返す.

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

// specField は仕様の要求スキーマの 1 項目. typ は JSON の型（string・number・boolean）.
// enum は仕様の説明文に挙がっている値（空なら制限しない）.
type specField struct {
	typ      string
	required bool
	enum     []string
}

type specSchema map[string]specField

var (
	lbProtocols  = []string{"TCP", "UDP"}
	lbAlgorithms = []string{"LEAST_CONNECTIONS", "ROUND_ROBIN"}
	// 仕様の「ヘルスモニター作成」の説明に挙がっている type
	lbMonitorTypes = []string{"TCP", "HTTP", "HTTPS", "PING", "UDP-CONNECT"}
)

// 仕様の要求スキーマ（CreateLoadBalancerReq など）.
var (
	createLoadBalancerReq = specSchema{"name": {typ: "string", required: true}}
	updateLoadBalancerReq = specSchema{"name": {typ: "string"}}
	createListenerReq     = specSchema{
		"protocol":        {typ: "string", required: true, enum: lbProtocols},
		"protocol_port":   {typ: "number", required: true},
		"loadbalancer_id": {typ: "string", required: true},
		"name":            {typ: "string", required: true},
	}
	updateListenerReq = specSchema{"name": {typ: "string"}}
	createPoolReq     = specSchema{
		"lb_algorithm": {typ: "string", required: true, enum: lbAlgorithms},
		"protocol":     {typ: "string", required: true, enum: lbProtocols},
		"listener_id":  {typ: "string", required: true},
		"name":         {typ: "string", required: true},
	}
	updatePoolReq = specSchema{
		"lb_algorithm": {typ: "string", enum: lbAlgorithms},
		"name":         {typ: "string"},
	}
	createMemberReq = specSchema{
		"name":          {typ: "string", required: true},
		"address":       {typ: "string", required: true},
		"protocol_port": {typ: "number", required: true},
	}
	// 仕様は admin_state_up を string と書くが、例・説明・応答はいずれも真偽値なので真偽値として確かめる
	updateMemberReq        = specSchema{"admin_state_up": {typ: "boolean", required: true}}
	createHealthmonitorReq = specSchema{
		"name":           {typ: "string", required: true},
		"pool_id":        {typ: "string", required: true},
		"delay":          {typ: "number", required: true},
		"max_retries":    {typ: "number", required: true},
		"timeout":        {typ: "number", required: true},
		"type":           {typ: "string", required: true, enum: lbMonitorTypes},
		"url_path":       {typ: "string"},
		"expected_codes": {typ: "string"},
	}
	updateHealthmonitorReq = specSchema{"name": {typ: "string"}}
)

// 本文が {wrapper: {...}} の形で、schema の必須項目をすべて含み、schema に無い項目を含まず、
// 各項目の型と値が schema に合うことを確かめる.
func bodyOf(body map[string]any, wrapper string, schema specSchema) (map[string]any, error) {
	inner, ok := body[wrapper].(map[string]any)
	if !ok || len(body) != 1 {
		return nil, fmt.Errorf("body must be {%q: {...}}, got %v", wrapper, body)
	}
	for k, f := range schema {
		if _, ok := inner[k]; f.required && !ok {
			return nil, fmt.Errorf("%s.%s is required", wrapper, k)
		}
	}
	for k, v := range inner {
		f, ok := schema[k]
		if !ok {
			return nil, fmt.Errorf("%s.%s is not a parameter of the request schema", wrapper, k)
		}
		var typeOK bool
		switch f.typ {
		case "string":
			_, typeOK = v.(string)
		case "number":
			_, typeOK = v.(float64)
		case "boolean":
			_, typeOK = v.(bool)
		}
		if !typeOK {
			return nil, fmt.Errorf("%s.%s must be a %s, got %T", wrapper, k, f.typ, v)
		}
		if s, _ := v.(string); len(f.enum) > 0 && !slices.Contains(f.enum, s) {
			return nil, fmt.Errorf("%s.%s must be one of %v, got %q", wrapper, k, f.enum, s)
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
	in, err := bodyOf(body, "loadbalancer", createLoadBalancerReq)
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
	in, err := bodyOf(body, "listener", createListenerReq)
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
	in, err := bodyOf(body, "pool", createPoolReq)
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
	in, err := bodyOf(body, "member", createMemberReq)
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
	in, err := bodyOf(body, "healthmonitor", createHealthmonitorReq)
	if err != nil {
		return fault(400, "%s", err)
	}
	// 仕様は url_path と expected_codes を省略可とする. 省いた HTTP・HTTPS には OpenStack の既定値を入れ、
	// それ以外の type に送られたら OpenStack と同じく拒否する
	typ, _ := in["type"].(string)
	isHTTP := typ == "HTTP" || typ == "HTTPS"
	fields := map[string]any{"url_path": nil, "expected_codes": nil}
	for k, def := range map[string]string{"url_path": "/", "expected_codes": "200"} {
		v, sent := in[k]
		switch {
		case sent && !isHTTP:
			return fault(400, "%s is only valid for HTTP and HTTPS health monitors, type %s", k, typ)
		case sent:
			fields[k] = v
		case isHTTP:
			fields[k] = def
		}
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
	fields["name"], fields["type"], fields["delay"], fields["timeout"], fields["max_retries"] =
		in["name"], typ, in["delay"], in["timeout"], in["max_retries"]
	o := f.add("healthmonitor", poolID, p.lbID, fields)
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
		in, err = bodyOf(body, kind, map[string]specSchema{
			"loadbalancer":  updateLoadBalancerReq,
			"listener":      updateListenerReq,
			"pool":          updatePoolReq,
			"member":        updateMemberReq,
			"healthmonitor": updateHealthmonitorReq,
		}[kind])
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
		// 仕様の更新の成功は 202
		return 202, map[string]any{kind: f.view(o)}
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

// 仕様の応答スキーマの形にする. 紐づくリソースは [{"id": ...}] で返すが、プールの members だけは
// 仕様どおり ID の文字列の配列で返す.
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
		members := idsOf(id, "member")
		if members == nil {
			members = []string{}
		}
		v["loadbalancers"], v["listeners"], v["members"] = refs(o.lbID), refs(o.parent), members
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

// Terraform の外での変更として、偽の API のオブジェクトの項目を書き換える.
func (f *lbFake) setField(id, key string, v any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objs[id].fields[key] = v
}
