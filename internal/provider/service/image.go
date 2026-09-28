// イメージの API を提供する.

package service

import (
	"context"
	"fmt"

	"github.com/gophercloud/gophercloud/v2/openstack/image/v2/images"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// ListImagesByName 名前が一致するイメージの一覧を取得.
func (c *ConohaClient) ListImagesByName(ctx context.Context, name string) ([]images.Image, error) {
	if c.ImageClient == nil {
		return nil, fmt.Errorf("image client is not initialized")
	}

	if c.ImageClient.ProviderClient == nil && c.ProviderClient != nil {
		c.ImageClient.ProviderClient = c.ProviderClient
	}

	tflog.Debug(ctx, "Listing images.", map[string]any{"name": name})

	pages, err := images.List(c.ImageClient, images.ListOpts{Name: name}).AllPages(ctx)
	if err != nil {
		tflog.Error(ctx, "Image listing error.", map[string]any{"error": err.Error(), "name": name})
		return nil, fmt.Errorf("failed to list images: %w", err)
	}

	list, err := images.ExtractImages(pages)
	if err != nil {
		tflog.Error(ctx, "Response parsing error.", map[string]any{"error": err.Error()})
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return list, nil
}
