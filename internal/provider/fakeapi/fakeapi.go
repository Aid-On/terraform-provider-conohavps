// テスト用の偽の ConoHa API を提供する.
// 認証とサービスカタログは実物と同じ形（ドキュメントのトークン発行のレスポンス）で返し、
// 各テストは Mux に自分の API のハンドラを足して、実際の API を使わずにプロバイダを動かす.

package fakeapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

const (
	// Token は偽の API が発行し、受け付けるトークン.
	Token = "fake-token"
	// TenantID は偽のテナント ID.
	TenantID = "tenant"
)

// Factories はテストで使うプロバイダ.
var Factories = map[string]func() (tfprotov6.ProviderServer, error){
	"conohavps": providerserver.NewProtocol6WithError(provider.New("0.0.0-test")()),
}

// Server は偽の ConoHa API.
type Server struct {
	*httptest.Server
	Mux *http.ServeMux
	ids atomic.Int64
}

// サービスの種別と、カタログに載せるパス（実物と同じく版を含むものと含まないものがある）.
var catalog = []struct{ typ, path string }{
	{"identity", "/identity/v3"},
	{"compute", "/compute/v2.1"},
	{"load-balancer", "/lbaas"},
	{"object-store", "/object-storage/v1/AUTH_" + TenantID},
	{"dns", "/dns-service"},
	{"volumev3", "/block-storage/v3/" + TenantID},
	{"image", "/image-service"},
	{"network", "/networking"},
	{"account", "/account/v1"},
}

// New は偽の ConoHa API を立てる. テストの終わりに閉じる.
func New(t *testing.T) *Server {
	t.Helper()
	for _, k := range []string{"CONOHAVPS_USER_ID", "CONOHAVPS_PASSWORD", "CONOHAVPS_TENANT_ID", "CONOHAVPS_IDENTITY_ENDPOINT", "CONOHAVPS_REGION"} {
		t.Setenv(k, "")
	}
	s := &Server{Mux: http.NewServeMux()}
	s.Server = httptest.NewServer(s.Mux)
	t.Cleanup(s.Close)
	s.Mux.HandleFunc("POST /identity/v3/auth/tokens", s.issueToken)
	// ネットワークとイメージのクライアントは、版を含まないエンドポイントから版の一覧を読む
	s.Mux.HandleFunc("GET /networking/", s.versions("v2.0", "/networking/v2.0/"))
	s.Mux.HandleFunc("GET /image-service/", s.versions("v2.0", "/image-service/v2/"))
	return s
}

func (s *Server) issueToken(w http.ResponseWriter, _ *http.Request) {
	var entries []map[string]any
	for _, c := range catalog {
		entries = append(entries, map[string]any{
			"type":      c.typ,
			"endpoints": []map[string]any{{"interface": "public", "region": "c3j1", "region_id": "c3j1", "url": s.URL + c.path}},
		})
	}
	w.Header().Set("X-Subject-Token", Token)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"token": map[string]any{
		"expires_at": "2099-01-01T00:00:00.000000Z",
		"catalog":    entries,
	}})
}

func (s *Server) versions(id, path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/networking/" && r.URL.Path != "/image-service/" {
			http.NotFound(w, r)
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{"versions": []map[string]any{
			{"id": id, "status": "CURRENT", "links": []map[string]any{{"rel": "self", "href": s.URL + path}}},
		}})
	}
}

// ProviderConfig はこの偽の API を使うプロバイダの設定.
func (s *Server) ProviderConfig() string {
	return fmt.Sprintf(`
provider "conohavps" {
  identity_endpoint = "%s/identity/v3"
  user_id           = "user"
  password          = "password"
  tenant_id         = "%s"
  region            = "c3j1"
}
`, s.URL, TenantID)
}

// NewID は偽の API が振る ID を返す.
func (s *Server) NewID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, s.ids.Add(1))
}

// Authorized はトークンを確かめ、違えば 401 を返して false を返す.
func Authorized(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("X-Auth-Token") != Token {
		w.WriteHeader(http.StatusUnauthorized)
		return false
	}
	return true
}

// WriteJSON は status と JSON の本文を返す.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ReadJSON はリクエストの本文を v に読む.
func ReadJSON(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}
