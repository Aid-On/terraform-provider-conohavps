package service_test

import (
	"context"
	"strings"
	"testing"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
)

// カタログにロードバランサーが無いときは、リクエストを送らずに分かるエラーを返す.
func TestLoadBalancerWithoutCatalogEntry(t *testing.T) {
	c := &service.ConohaClient{}
	_, err := c.GetLoadBalancer(context.Background(), "lb")
	if err == nil || !strings.Contains(err.Error(), "load-balancer service is not in the ConoHa service catalog") {
		t.Fatalf("want a service-unavailable error, got %v", err)
	}
	if err := c.DeleteMember(context.Background(), "pool", "member"); err == nil {
		t.Fatal("want an error from DeleteMember without a load balancer client")
	}
}
