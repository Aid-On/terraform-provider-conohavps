// API クライアントを提供する.

package service

import (
	"context"
	"fmt"
	"strings"

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
	ImageClient        *gophercloud.ServiceClient
	// 以下はサーバー以外のサービスのクライアント. カタログに無い場合は nil のままにし、
	// 使うリソースだけがエラーになる（サーバー等の既存リソースは影響を受けない）.
	LoadBalancerClient  *gophercloud.ServiceClient
	ObjectStorageClient *gophercloud.ServiceClient
	DNSClient           *gophercloud.ServiceClient
	IdentityClient      *gophercloud.ServiceClient
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

	if c.ImageClient, err = openstack.NewImageV2(ctx, c.ProviderClient, gophercloud.EndpointOpts{
		Region: c.Region,
	}); err != nil {
		tflog.Error(ctx, "Failed to initialize ImageV2 client.", map[string]any{"error": err.Error()})
		return err
	}
	c.ImageClient.ProviderClient = c.ProviderClient
	c.ImageClient.ResourceBase = imageResourceBase(c.ImageClient.Endpoint)

	c.initOptionalClients(ctx)

	return nil
}

// サーバー以外のサービスのクライアントを初期化する.
// ConoHa の DNS API は v1 など OpenStack 標準と版が異なるため、gophercloud の版の探索を通さず、
// カタログのエンドポイントからリクエスト先を組み立てる.
func (c *ConohaClient) initOptionalClients(ctx context.Context) {
	c.LoadBalancerClient = c.catalogClient(ctx, "load-balancer", "v2.0/")
	c.ObjectStorageClient = c.catalogClient(ctx, "object-store", "")
	c.DNSClient = c.catalogClient(ctx, "dns", "v1/")
	c.IdentityClient = c.catalogClient(ctx, "identity", "")
}

// カタログから種別 typ のエンドポイントを引き、base（"v2.0/" など）を付けたクライアントを返す.
// 見つからなければ nil を返す.
func (c *ConohaClient) catalogClient(ctx context.Context, typ, base string) *gophercloud.ServiceClient {
	url, err := c.ProviderClient.EndpointLocator(ctx, gophercloud.EndpointOpts{
		Type:         typ,
		Region:       c.Region,
		Availability: gophercloud.AvailabilityPublic,
	})
	if err != nil {
		tflog.Warn(ctx, "Service is not in the catalog; resources using it are unavailable.", map[string]any{"type": typ, "error": err.Error()})
		return nil
	}
	endpoint := gophercloud.NormalizeURL(url)
	return &gophercloud.ServiceClient{
		ProviderClient: c.ProviderClient,
		Endpoint:       endpoint,
		ResourceBase:   endpoint + base,
		Type:           typ,
	}
}

// ServiceUnavailable は、カタログに無いサービスを使おうとしたときのエラー.
func ServiceUnavailable(typ string) error {
	return fmt.Errorf("the %s service is not in the ConoHa service catalog for this region", typ)
}

// イメージ API のリクエスト先を決める.
// gophercloud はカタログのエンドポイントに "v2/" を付け足すため、
// エンドポイントがすでに /v2 で終わる場合に二重にならないようにする.
func imageResourceBase(endpoint string) string {
	base := strings.TrimSuffix(endpoint, "/")
	if strings.HasSuffix(base, "/v2") {
		return base + "/"
	}
	return base + "/v2/"
}
