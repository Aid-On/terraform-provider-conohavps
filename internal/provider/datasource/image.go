// イメージのデータソースを提供する.
// イメージ名（vmi-ubuntu-24.04-amd64 など）から UUID を引く.

package datasource

import (
	"context"
	"fmt"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &imageDataSource{}

func NewImageDataSource() datasource.DataSource {
	return &imageDataSource{}
}

type imageDataSource struct {
	client *service.ConohaClient
}

type imageDataSourceModel struct {
	Name         types.String `tfsdk:"name"`         // イメージ名
	ID           types.String `tfsdk:"id"`           // イメージ ID
	MinDisk      types.Int64  `tfsdk:"min_disk"`     // 必要なディスク（GB）
	MinRAM       types.Int64  `tfsdk:"min_ram"`      // 必要なメモリ（MB）
	Size         types.Int64  `tfsdk:"size"`         // イメージのサイズ（バイト）
	Status       types.String `tfsdk:"status"`       // 状態
	Visibility   types.String `tfsdk:"visibility"`   // 公開範囲
	OSType       types.String `tfsdk:"os_type"`      // OS の種類
	OSVersion    types.String `tfsdk:"os_version"`   // OS のバージョン
	Architecture types.String `tfsdk:"architecture"` // CPU アーキテクチャ
}

func (d *imageDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_image"
}

func (d *imageDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up an image by name, such as `vmi-ubuntu-24.04-amd64`. Use its `id` as `image_ref` of a boot `conohavps_volume`.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the image. It must match exactly one active image.",
				Required:            true,
			},
			"id": schema.StringAttribute{
				MarkdownDescription: "The UUID of the image.",
				Computed:            true,
			},
			"min_disk": schema.Int64Attribute{
				MarkdownDescription: "The smallest disk in GB the image boots from.",
				Computed:            true,
			},
			"min_ram": schema.Int64Attribute{
				MarkdownDescription: "The smallest memory in MB the image boots with.",
				Computed:            true,
			},
			"size": schema.Int64Attribute{
				MarkdownDescription: "The size of the image in bytes.",
				Computed:            true,
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "The status of the image.",
				Computed:            true,
			},
			"visibility": schema.StringAttribute{
				MarkdownDescription: "The visibility of the image, such as `public` or `private`.",
				Computed:            true,
			},
			"os_type": schema.StringAttribute{
				MarkdownDescription: "The type of the operating system, such as `linux` or `windows`. Empty when the image does not set it.",
				Computed:            true,
			},
			"os_version": schema.StringAttribute{
				MarkdownDescription: "The version of the operating system. Empty when the image does not set it.",
				Computed:            true,
			},
			"architecture": schema.StringAttribute{
				MarkdownDescription: "The CPU architecture of the image, such as `x86_64`. Empty when the image does not set it.",
				Computed:            true,
			},
		},
	}
}

func (d *imageDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientOf(req, resp)
}

func (d *imageDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data imageDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	list, err := d.client.ListImagesByName(ctx, data.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to list images", err.Error())
		return
	}

	image, err := findImage(list, data.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Image not found", err.Error())
		return
	}

	data.ID = types.StringValue(image.ID)
	data.MinDisk = types.Int64Value(int64(image.MinDisk))
	data.MinRAM = types.Int64Value(int64(image.MinRAM))
	data.Size = types.Int64Value(image.Size)
	data.Status = types.StringValue(image.Status)
	data.Visibility = types.StringValue(image.Visibility)
	data.OSType = types.StringValue(image.OSType)
	data.OSVersion = types.StringValue(image.OSVersion)
	data.Architecture = types.StringValue(image.Architecture)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// 名前が一致し、使える状態のイメージを1つだけ選ぶ.
func findImage(list []service.Image, name string) (service.Image, error) {
	var found []service.Image
	for _, i := range list {
		if i.Name == name && i.Status == "active" {
			found = append(found, i)
		}
	}

	switch len(found) {
	case 0:
		return service.Image{}, fmt.Errorf("no active image is named %q", name)
	case 1:
		return found[0], nil
	default:
		return service.Image{}, fmt.Errorf("%d active images are named %q", len(found), name)
	}
}
