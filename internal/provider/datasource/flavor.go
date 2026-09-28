// フレーバー（サーバープラン）のデータソースを提供する.
// フレーバー名（g2l-t-c4m4 など）から UUID とスペックを引く.

package datasource

import (
	"context"
	"fmt"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/flavors"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &flavorDataSource{}

func NewFlavorDataSource() datasource.DataSource {
	return &flavorDataSource{}
}

type flavorDataSource struct {
	client *service.ConohaClient
}

type flavorDataSourceModel struct {
	Name  types.String `tfsdk:"name"`  // フレーバー名
	ID    types.String `tfsdk:"id"`    // フレーバー ID
	VCPUs types.Int64  `tfsdk:"vcpus"` // CPU コア数
	RAM   types.Int64  `tfsdk:"ram"`   // メモリ（MB）
	Disk  types.Int64  `tfsdk:"disk"`  // ディスク（GB）
}

func (d *flavorDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_flavor"
}

func (d *flavorDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a flavor (server plan) by name, such as `g2l-t-c4m4` (Linux, hourly billing, 4 cores, 4 GB). Use its `id` as `flavor_id` of `conohavps_instance`.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the flavor. It must match exactly one flavor.",
				Required:            true,
			},
			"id": schema.StringAttribute{
				MarkdownDescription: "The UUID of the flavor.",
				Computed:            true,
			},
			"vcpus": schema.Int64Attribute{
				MarkdownDescription: "The number of CPU cores.",
				Computed:            true,
			},
			"ram": schema.Int64Attribute{
				MarkdownDescription: "The memory in MB.",
				Computed:            true,
			},
			"disk": schema.Int64Attribute{
				MarkdownDescription: "The disk in GB the flavor defines.",
				Computed:            true,
			},
		},
	}
}

func (d *flavorDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientOf(req, resp)
}

func (d *flavorDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data flavorDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	list, err := d.client.ListFlavors(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list flavors", err.Error())
		return
	}

	flavor, err := findFlavor(list, data.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Flavor not found", err.Error())
		return
	}

	data.ID = types.StringValue(flavor.ID)
	data.VCPUs = types.Int64Value(int64(flavor.VCPUs))
	data.RAM = types.Int64Value(int64(flavor.RAM))
	data.Disk = types.Int64Value(int64(flavor.Disk))

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// 名前が一致するフレーバーを1つだけ選ぶ.
func findFlavor(list []flavors.Flavor, name string) (flavors.Flavor, error) {
	var found []flavors.Flavor
	for _, f := range list {
		if f.Name == name {
			found = append(found, f)
		}
	}

	switch len(found) {
	case 0:
		return flavors.Flavor{}, fmt.Errorf("no flavor is named %q", name)
	case 1:
		return found[0], nil
	default:
		return flavors.Flavor{}, fmt.Errorf("%d flavors are named %q", len(found), name)
	}
}
