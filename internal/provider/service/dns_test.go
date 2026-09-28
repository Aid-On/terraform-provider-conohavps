package service_test

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"testing"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/fakeapi"
	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
	"github.com/gophercloud/gophercloud/v2"
)

// レコード一覧は limit と offset でページを辿り、total_count 件をすべて返す.
func TestListDNSRecordsFollowsPages(t *testing.T) {
	s := fakeapi.New(t)
	var offsets []string
	s.Mux.HandleFunc("GET /dns-service/v1/domains/dom/records", func(w http.ResponseWriter, r *http.Request) {
		if !fakeapi.Authorized(w, r) {
			return
		}
		q := r.URL.Query()
		if q.Get("limit") == "" {
			t.Errorf("offset is sent without limit: %s", r.URL.RawQuery)
		}
		offsets = append(offsets, q.Get("offset"))
		offset, _ := strconv.Atoi(q.Get("offset"))
		var page []map[string]any
		for i := offset; i < min(offset+2, 5); i++ {
			page = append(page, map[string]any{"uuid": "rec-" + strconv.Itoa(i), "domain_uuid": "dom", "type": "A"})
		}
		fakeapi.WriteJSON(w, http.StatusOK, map[string]any{"records": page, "total_count": 5})
	})

	c := &service.ConohaClient{}
	if err := c.Authenticate(context.Background(), gophercloud.AuthOptions{
		IdentityEndpoint: s.URL + "/identity/v3", UserID: "user", Password: "password", TenantID: fakeapi.TenantID,
	}, "c3j1"); err != nil {
		t.Fatal(err)
	}

	records, err := c.ListDNSRecords(context.Background(), "dom")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 5 || records[4].ID != "rec-4" {
		t.Errorf("got %d records: %+v", len(records), records)
	}
	if want := []string{"0", "2", "4"}; !slices.Equal(offsets, want) {
		t.Errorf("offsets = %v, want %v", offsets, want)
	}
}

// カタログに DNS が無ければ、DNS を使う関数だけがエラーになる.
func TestDNSUnavailableWithoutCatalogEntry(t *testing.T) {
	c := &service.ConohaClient{}
	if _, err := c.GetDNSDomain(context.Background(), "x"); err == nil || err.Error() != service.ServiceUnavailable("dns").Error() {
		t.Errorf("err = %v", err)
	}
}

// ID は OpenAPI 仕様の id・domain_id と、HTML ドキュメントの uuid・domain_uuid のどちらでも読む.
func TestDNSDecodesBothIDKeys(t *testing.T) {
	cases := []struct{ body, id, domainID string }{
		{`{"id": "r1", "domain_id": "d1", "ttl": 300, "description": "x"}`, "r1", "d1"},
		{`{"uuid": "r2", "domain_uuid": "d2", "ttl": 3600}`, "r2", "d2"},
	}
	for _, c := range cases {
		var r service.DNSRecord
		if err := json.Unmarshal([]byte(c.body), &r); err != nil {
			t.Fatal(err)
		}
		if r.ID != c.id || r.DomainID != c.domainID || r.TTL == nil {
			t.Errorf("%s: got %+v", c.body, r)
		}
		var d service.DNSDomain
		if err := json.Unmarshal([]byte(c.body), &d); err != nil {
			t.Fatal(err)
		}
		if d.ID != c.id {
			t.Errorf("%s: domain id = %q", c.body, d.ID)
		}
	}
}

// レコードの本文は、値の無いフィールドを送らず、Null に挙げたものだけ null で送る.
func TestDNSRecordOptsNull(t *testing.T) {
	ten := int64(10)
	b, err := json.Marshal(service.DNSRecordOpts{Name: "a.", Type: "CNAME", Data: "b.", Priority: &ten, Null: []string{"priority", "weight"}})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m["priority"] != float64(10) {
		t.Errorf("priority = %v", m["priority"])
	}
	if v, ok := m["weight"]; !ok || v != nil {
		t.Errorf("weight = %v (sent: %v), want null", v, ok)
	}
	for _, k := range []string{"port", "ttl", "description", "Null"} {
		if _, ok := m[k]; ok {
			t.Errorf("%s must not be sent: %s", k, b)
		}
	}
}
