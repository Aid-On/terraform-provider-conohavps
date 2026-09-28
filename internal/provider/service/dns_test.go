package service_test

import (
	"context"
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
