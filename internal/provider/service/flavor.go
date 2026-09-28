// フレーバー（サーバープラン）の API を提供する.

package service

import (
	"context"
	"fmt"

	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/flavors"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// ListFlavors フレーバーの詳細一覧を取得.
// API 仕様の詳細一覧取得にはクエリパラメータが無いため、is_public などの絞り込みは付けない.
func (c *ConohaClient) ListFlavors(ctx context.Context) ([]flavors.Flavor, error) {
	if c.ComputeClient == nil {
		return nil, fmt.Errorf("compute client is not initialized")
	}

	if c.ComputeClient.ProviderClient == nil && c.ProviderClient != nil {
		c.ComputeClient.ProviderClient = c.ProviderClient
	}

	tflog.Debug(ctx, "Listing flavors.", map[string]any{})

	pages, err := flavors.ListDetail(c.ComputeClient, nil).AllPages(ctx)
	if err != nil {
		tflog.Error(ctx, "Flavor listing error.", map[string]any{"error": err.Error()})
		return nil, fmt.Errorf("failed to list flavors: %w", err)
	}

	list, err := flavors.ExtractFlavors(pages)
	if err != nil {
		tflog.Error(ctx, "Response parsing error.", map[string]any{"error": err.Error()})
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return list, nil
}
