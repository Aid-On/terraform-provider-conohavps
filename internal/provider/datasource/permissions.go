// パーミッション一覧のデータソースを提供する.
// ロールに紐づけられるパーミッション名（API 操作の単位）の一覧を引く.

package datasource

import (
	"context"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &permissionsDataSource{}

func NewPermissionsDataSource() datasource.DataSource {
	return &permissionsDataSource{}
}

type permissionsDataSource struct {
	client *service.ConohaClient
}

type permissionsDataSourceModel struct {
	ID          types.String `tfsdk:"id"`          // 固定値
	Names       types.List   `tfsdk:"names"`       // パーミッション名
	Permissions types.List   `tfsdk:"permissions"` // パーミッション名と説明
}

var permissionAttrTypes = map[string]attr.Type{
	"name":        types.StringType,
	"description": types.StringType,
}

func (d *permissionsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_permissions"
}

func (d *permissionsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the permissions that can be granted by a `conohavps_role`. Each permission allows one API operation and is named `<http method>-<resource>[-<sub-resource>][-<operation>]`, such as `get-server-list`. " +
			"There are no permissions for the role APIs, the Object Storage API and the DNS API.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "A fixed identifier of this data source.",
				Computed:            true,
			},
			"names": schema.ListAttribute{
				MarkdownDescription: "The names of the permissions, in the order the API returns them.",
				Computed:            true,
				ElementType:         types.StringType,
			},
			"permissions": schema.ListNestedAttribute{
				MarkdownDescription: "The permissions with their descriptions.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							MarkdownDescription: "The name of the permission.",
							Computed:            true,
						},
						"description": schema.StringAttribute{
							MarkdownDescription: "The description of the permission (in Japanese).",
							Computed:            true,
						},
					},
				},
			},
		},
	}
}

func (d *permissionsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientOf(req, resp)
}

func (d *permissionsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	list, err := d.client.ListPermissions(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list permissions", err.Error())
		return
	}

	names := make([]string, 0, len(list))
	objects := make([]attr.Value, 0, len(list))
	for _, p := range list {
		names = append(names, p.Name)
		obj, diags := types.ObjectValue(permissionAttrTypes, map[string]attr.Value{
			"name":        types.StringValue(p.Name),
			"description": types.StringValue(p.Description),
		})
		resp.Diagnostics.Append(diags...)
		objects = append(objects, obj)
	}

	var data permissionsDataSourceModel
	data.ID = types.StringValue("permissions")
	var diags diag.Diagnostics
	data.Names, diags = types.ListValueFrom(ctx, types.StringType, names)
	resp.Diagnostics.Append(diags...)
	data.Permissions, diags = types.ListValue(types.ObjectType{AttrTypes: permissionAttrTypes}, objects)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
