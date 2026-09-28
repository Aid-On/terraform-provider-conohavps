package service_test

import (
	"context"
	"testing"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/fakeapi"
	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
	"github.com/gophercloud/gophercloud/v2"
)

// カタログの形が実物どおりのとき、各サービスのリクエスト先がドキュメントの URL になる.
func TestServiceClientsFollowTheCatalog(t *testing.T) {
	s := fakeapi.New(t)
	c := &service.ConohaClient{}
	err := c.Authenticate(context.Background(), gophercloud.AuthOptions{
		IdentityEndpoint: s.URL + "/identity/v3", UserID: "user", Password: "password", TenantID: fakeapi.TenantID,
	}, "c3j1")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"load-balancer": s.URL + "/lbaas/v2.0/",
		"object-store":  s.URL + "/object-storage/v1/AUTH_" + fakeapi.TenantID + "/",
		"dns":           s.URL + "/dns-service/v1/",
		"identity":      s.URL + "/identity/v3/",
		"image":         s.URL + "/image-service/v2/",
	}
	got := map[string]*gophercloud.ServiceClient{
		"load-balancer": c.LoadBalancerClient, "object-store": c.ObjectStorageClient,
		"dns": c.DNSClient, "identity": c.IdentityClient, "image": c.ImageClient,
	}
	for typ, w := range want {
		sc := got[typ]
		if sc == nil {
			t.Errorf("%s client is nil", typ)
			continue
		}
		if base := sc.ResourceBaseURL(); base != w {
			t.Errorf("%s requests go to %q, want %q", typ, base, w)
		}
	}
}
