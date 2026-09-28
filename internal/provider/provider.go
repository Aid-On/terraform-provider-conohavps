// プロバイダを提供する.

package provider

import (
	"context"
	"os"

	d "github.com/gmo-internet/terraform-provider-conohavps/internal/provider/datasource"
	r "github.com/gmo-internet/terraform-provider-conohavps/internal/provider/resource"
	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ provider.Provider = &conohaProvider{}
)

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &conohaProvider{
			version: version,
		}
	}
}

type conohaProvider struct {
	version string
}

type conohaProviderModel struct {
	UserID           types.String `tfsdk:"user_id"`
	Password         types.String `tfsdk:"password"`
	TenantID         types.String `tfsdk:"tenant_id"`
	IdentityEndpoint types.String `tfsdk:"identity_endpoint"`
	Region           types.String `tfsdk:"region"`
}

func (p *conohaProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "conohavps"
	resp.Version = p.version
}

func (p *conohaProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"user_id": schema.StringAttribute{
				MarkdownDescription: "The user ID of the ConoHa VPS Public API. Can also be set with the `CONOHAVPS_USER_ID` environment variable.",
				Optional:            true,
			},
			"password": schema.StringAttribute{
				MarkdownDescription: "The password of the ConoHa VPS Public API. Can also be set with the `CONOHAVPS_PASSWORD` environment variable.",
				Optional:            true,
				Sensitive:           true,
			},
			"tenant_id": schema.StringAttribute{
				MarkdownDescription: "The Tenant ID of the ConoHa VPS Public API. Can also be set with the `CONOHAVPS_TENANT_ID` environment variable.",
				Optional:            true,
			},
			"identity_endpoint": schema.StringAttribute{
				MarkdownDescription: "The identity endpoint of ConoHa VPS Public API. The default value is `https://identity.c3j1.conoha.io/v3`. Can also be set with the `CONOHAVPS_IDENTITY_ENDPOINT` environment variable.",
				Optional:            true,
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "The region of ConoHa VPS. The default value is `c3j1`. Can also be set with the `CONOHAVPS_REGION` environment variable.",
				Optional:            true,
			},
		},
	}
}

func (p *conohaProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data conohaProviderModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// 構成ファイルまたは環境変数から値を受け入れる
	// 環境変数の値が空でない場合、構成ファイルの値よりも優先する
	userID := os.Getenv("CONOHAVPS_USER_ID")
	tenantID := os.Getenv("CONOHAVPS_TENANT_ID")
	password := os.Getenv("CONOHAVPS_PASSWORD")
	region := os.Getenv("CONOHAVPS_REGION")
	identityEndpoint := os.Getenv("CONOHAVPS_IDENTITY_ENDPOINT")

	if userID == "" {
		userID = data.UserID.ValueString()
	}

	if password == "" {
		password = data.Password.ValueString()
	}

	if tenantID == "" {
		tenantID = data.TenantID.ValueString()
	}

	// リージョンと認証エンドポイントのみ
	// 構成ファイルおよび環境変数から受け取った値が空文字のとき、標準値を採用する
	if region == "" {
		if data.Region.IsNull() {
			region = "c3j1"
		} else {
			region = data.Region.ValueString()
		}
	}

	if identityEndpoint == "" {
		if data.IdentityEndpoint.IsNull() {
			identityEndpoint = "https://identity.c3j1.conoha.io/v3"
		} else {
			identityEndpoint = data.IdentityEndpoint.ValueString()
		}
	}

	if userID == "" {
		resp.Diagnostics.AddError(
			"Missing user ID configuration",
			"While configuring the provider, the user ID was not found in "+
				"the CONOHAVPS_USER_ID environment variable or provider "+
				"configuration block user_id attribute.")
	}

	if password == "" {
		resp.Diagnostics.AddError(
			"Missing user password configuration",
			"While configuring the provider, the password was not found in "+
				"the CONOHAVPS_PASSWORD environment variable or provider "+
				"configuration block password attribute.")
	}

	if tenantID == "" {
		resp.Diagnostics.AddError(
			"Missing tenant ID configuration",
			"While configuring the provider, the tenant ID was not found in "+
				"the CONOHAVPS_TENANT_ID environment variable or provider "+
				"configuration block tenant_id attribute.")
	}

	conohaClient := &service.ConohaClient{Region: region}

	authOpts := gophercloud.AuthOptions{
		UserID:           userID,
		Password:         password,
		TenantID:         tenantID,
		IdentityEndpoint: identityEndpoint,
	}

	if err := conohaClient.Authenticate(ctx, authOpts, region); err != nil {
		resp.Diagnostics.AddError(
			"Failed to authenticate client or initialize client",
			err.Error())
		return
	}

	resp.ResourceData = conohaClient
	resp.DataSourceData = conohaClient
}

func (p *conohaProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		d.NewFlavorDataSource,
		d.NewImageDataSource,
		d.NewDNSDomainDataSource,
	}
}

func (p *conohaProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		r.NewInstanceResource,
		r.NewKeypairResource,
		r.NewSecurityGroupResource,
		r.NewSecurityGroupRuleResource,
		r.NewVolumeResource,
		r.NewDNSDomainResource,
		r.NewDNSRecordResource,
	}
}
