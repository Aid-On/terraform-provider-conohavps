// イメージのデータソースを提供する.
// イメージ名（vmi-ubuntu-24.04-amd64 など）から UUID を引く.

package datasource

import (
	"context"
	"fmt"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
	"github.com/gophercloud/gophercloud/v2/openstack/image/v2/images"
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
	Name    types.String `tfsdk:"name"`     // イメージ名
	ID      types.String `tfsdk:"id"`       // イメージ ID
	MinDisk types.Int64  `tfsdk:"min_disk"` // 必要なディスク（GB）
	Status  types.String `tfsdk:"status"`   // 状態
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
			"status": schema.StringAttribute{
				MarkdownDescription: "The status of the image.",
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
	data.MinDisk = types.Int64Value(int64(image.MinDiskGigabytes))
	data.Status = types.StringValue(string(image.Status))

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// 名前が一致し、使える状態のイメージを1つだけ選ぶ.
func findImage(list []images.Image, name string) (images.Image, error) {
	var found []images.Image
	for _, i := range list {
		if i.Name == name && i.Status == images.ImageStatusActive {
			found = append(found, i)
		}
	}

	switch len(found) {
	case 0:
		return images.Image{}, fmt.Errorf("no active image is named %q", name)
	case 1:
		return found[0], nil
	default:
		return images.Image{}, fmt.Errorf("%d active images are named %q", len(found), name)
	}
}
