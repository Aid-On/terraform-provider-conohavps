// イメージ保存容量の使用量のデータソースを提供する.
// conohavps_image_quota を縮める前に、使用量を確かめるために使う.

package datasource

import (
	"context"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &imageUsageDataSource{}

func NewImageUsageDataSource() datasource.DataSource {
	return &imageUsageDataSource{}
}

type imageUsageDataSource struct {
	client *service.ConohaClient
}

type imageUsageDataSourceModel struct {
	ID        types.String `tfsdk:"id"`         // テナント ID
	SizeBytes types.Int64  `tfsdk:"size_bytes"` // 使用量（byte）
}

func (d *imageUsageDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_image_usage"
}

func (d *imageUsageDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads how much of the image save capacity the account's images use. `conohavps_image_quota` cannot be set below this usage.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The tenant ID of the account.",
				Computed:            true,
			},
			"size_bytes": schema.Int64Attribute{
				MarkdownDescription: "The total size of the saved images in bytes.",
				Computed:            true,
			},
		},
	}
}

func (d *imageUsageDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientOf(req, resp)
}

func (d *imageUsageDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	size, err := d.client.GetImageUsage(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read image usage", err.Error())
		return
	}

	data := imageUsageDataSourceModel{
		ID:        types.StringValue(d.client.TenantID),
		SizeBytes: types.Int64Value(size),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
