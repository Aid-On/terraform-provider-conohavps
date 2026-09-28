// オブジェクトストレージの契約容量の読み方のテストを提供する.

package service

import (
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
