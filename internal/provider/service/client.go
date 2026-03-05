// API クライアントを提供する.

package service

import (
	"context"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// API クライアントの構造体.
// サポートするコンポーネントが追加されたら、対応するクライアントを構造体に追加する.
type ConohaClient struct {
	ProviderClient     *gophercloud.ProviderClient
	BlockStorageClient *gophercloud.ServiceClient
	ComputeClient      *gophercloud.ServiceClient
	NetworkClient      *gophercloud.ServiceClient
	Token              string
	TenantID           string
	IdentityEndpoint   string
	Region             string
}

// クライアントの認証を実施する.
// 認証が成功したら、コンポーネント専用のクライアントの初期化処理を呼び出す.
func (c *ConohaClient) Authenticate(ctx context.Context, authOpts gophercloud.AuthOptions, region string) error {

	tflog.Debug(ctx, "Starting authentication request.", map[string]any{})

	provider, err := openstack.AuthenticatedClient(ctx, authOpts)
	if err != nil {
		tflog.Error(ctx, "Failed to authenticate client.", map[string]any{"error": err.Error()})
		return err
	}

	tflog.Debug(ctx, "Authentication request completed.", map[string]any{})

	c.ProviderClient = provider
	c.Token = provider.TokenID
	c.TenantID = authOpts.TenantID
	c.Region = region
	c.IdentityEndpoint = authOpts.IdentityEndpoint

	if err := c.initServiceClients(ctx); err != nil {
		tflog.Error(ctx, "An unexpected error occurred while attempting to initialize service clients.", map[string]any{"error": err.Error()})
		return err
	}

	return nil
}

// コンポーネントのクライアントを初期化する.
func (c *ConohaClient) initServiceClients(ctx context.Context) error {
	var err error

	// コンポーネントのクライアントの初期化時に渡す ProviderClient に API とやりとりするための情報が格納される.
	// 初期化後は、必ずコンポーネントのクライアントへ ProviderClient を渡す.
	if c.BlockStorageClient, err = openstack.NewBlockStorageV3(ctx, c.ProviderClient, gophercloud.EndpointOpts{
		Region: c.Region,
	}); err != nil {
		tflog.Error(ctx, "Failed to initialize BlockStorageV3 client.", map[string]any{"error": err.Error()})
		return err
	}
	c.BlockStorageClient.ProviderClient = c.ProviderClient

	if c.ComputeClient, err = openstack.NewComputeV2(ctx, c.ProviderClient, gophercloud.EndpointOpts{
		Region: c.Region,
	}); err != nil {
		tflog.Error(ctx, "Failed to initialize ComputeV2 client.", map[string]any{"error": err.Error()})
		return err
	}
	c.ComputeClient.ProviderClient = c.ProviderClient

	if c.NetworkClient, err = openstack.NewNetworkV2(ctx, c.ProviderClient, gophercloud.EndpointOpts{
		Region: c.Region,
	}); err != nil {
		tflog.Error(ctx, "Failed to initialize NetworkV2 client.", map[string]any{"error": err.Error()})
		return err
	}
	c.NetworkClient.ProviderClient = c.ProviderClient

	return nil
}
