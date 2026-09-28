// オブジェクトストレージの契約容量とコンテナのヘッダーの読み方のテストを提供する.

package service

import (
	"maps"
	"net/http"
	"testing"
)

func TestQuotaGigaBytes(t *testing.T) {
	cases := []struct {
		name    string
		headers map[string]string
		want    int64
		wantErr bool
	}{
		{"default", map[string]string{"X-Account-Meta-Quota-Bytes": "0"}, 0, false},
		{"no header", map[string]string{}, 0, false},
		{"GiB", map[string]string{"X-Account-Meta-Quota-Bytes": "107374182400"}, 100, false},
		{"GB", map[string]string{"X-Account-Meta-Quota-Bytes": "100000000000"}, 100, false},
		{"giga header wins", map[string]string{"X-Account-Meta-Quota-Giga-Bytes": "200", "X-Account-Meta-Quota-Bytes": "100000000000"}, 200, false},
		{"not whole GB", map[string]string{"X-Account-Meta-Quota-Bytes": "123"}, 0, true},
		{"not a number", map[string]string{"X-Account-Meta-Quota-Bytes": "x"}, 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := http.Header{}
			for k, v := range c.headers {
				h.Set(k, v)
			}
			got, err := quotaGigaBytes(h)
			if (err != nil) != c.wantErr || got != c.want {
				t.Errorf("quotaGigaBytes = %d, %v; want %d, error %v", got, err, c.want, c.wantErr)
			}
		})
	}
}

func TestParseContainer(t *testing.T) {
	h := http.Header{}
	h.Set("X-Container-Object-Count", "3")
	h.Set("X-Container-Bytes-Used", "42")
	h.Set("X-Versions-Location", "%E5%8F%A4%E3%81%84%20%E7%89%88")
	h.Set("X-Container-Read", ".r:*")
	h.Set("X-Container-Meta-Web-Index", "index.html")
	h.Set("X-Container-Meta-Web-Listings", "yes")
	h.Set("X-Container-Meta-Web-Listings-CSS", "listing.css")
	h.Set("X-Container-Meta-Owner", "team-a")
	h.Set("X-Container-Meta-Access-Control-Allow-Origin", "*")
	h.Set("X-Container-Meta-Temp-Url-Key", "secret")

	c, err := parseContainer("photos", h)
	if err != nil {
		t.Fatal(err)
	}
	if c.ObjectCount != 3 || c.BytesUsed != 42 {
		t.Errorf("counts = %d, %d; want 3, 42", c.ObjectCount, c.BytesUsed)
	}
	if c.VersionsLocation != "古い 版" {
		t.Errorf("VersionsLocation = %q, want it URL-decoded", c.VersionsLocation)
	}
	if c.ContainerRead != ".r:*" || c.ContainerWrite != "" || c.WebIndex != "index.html" || c.WebListingsCSS != "listing.css" || c.WebError != "" {
		t.Errorf("settings = %+v", c)
	}
	if c.WebListings == nil || !*c.WebListings {
		t.Errorf("WebListings = %v, want true for %q", c.WebListings, "yes")
	}
	// Web 公開の設定と一時 URL の鍵はメタデータに含めず、キーは小文字にする
	want := map[string]string{"owner": "team-a", "access-control-allow-origin": "*"}
	if !maps.Equal(c.Metadata, want) {
		t.Errorf("Metadata = %v, want %v", c.Metadata, want)
	}

	// 設定が無ければ空（Web 公開の一覧は nil）
	c, err = parseContainer("empty", http.Header{})
	if err != nil || c.WebListings != nil || c.VersionsLocation != "" || len(c.Metadata) != 0 {
		t.Errorf("parseContainer(no headers) = %+v, %v", c, err)
	}

	h = http.Header{}
	h.Set("X-Versions-Location", "%zz")
	if _, err := parseContainer("bad", h); err == nil {
		t.Error("parseContainer accepted a versions location that is not URL-encoded")
	}
}

func TestRemoveHeader(t *testing.T) {
	for in, want := range map[string]string{
		HeaderVersionsLocation:        "X-Remove-Versions-Location",
		HeaderContainerRead:           "X-Remove-Container-Read",
		HeaderWebListingsCSS:          "X-Remove-Container-Meta-Web-Listings-CSS",
		ContainerMetaPrefix + "owner": "X-Remove-Container-Meta-owner",
	} {
		if got := RemoveHeader(in); got != want {
			t.Errorf("RemoveHeader(%q) = %q, want %q", in, got, want)
		}
	}
}
