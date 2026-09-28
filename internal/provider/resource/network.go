// ローカルネットワークのリソースを提供する.
// ローカルネットワークの作成はリクエストの本文を取らず、名前等は ConoHa が決めるため、指定できる引数は無い.

package resource

import (
	"context"
	"fmt"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ resource.Resource                = &networkResource{}
	_ resource.ResourceWithImportState = &networkResource{}
)

func NewNetworkResource() resource.Resource {
	return &networkResource{}
}

type networkResource struct {
	client *service.ConohaClient
}

// ローカルネットワークのリソースモデル. すべてレスポンスの値.
type networkResourceModel struct {
	ID     types.String `tfsdk:"id"`     // ネットワーク ID
	Name   types.String `tfsdk:"name"`   // ネットワーク名（ConoHa が付ける）
	MTU    types.Int64  `tfsdk:"mtu"`    // MTU
	Status types.String `tfsdk:"status"` // 状態
}

func (r *networkResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_network"
}

func (r *networkResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a local network (VLAN). The network takes no arguments: ConoHa names it. " +
			"Up to 10 local networks can be created per ConoHa account. Add a `conohavps_subnet` to it before creating ports. " +
			"A network cannot be deleted while subnets remain on it.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The ID of the network.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name ConoHa gives the network, such as `local-gnct24510032-1`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"mtu": schema.Int64Attribute{
				MarkdownDescription: "The MTU of the network.",
				Computed:            true,
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "The status of the network, such as `ACTIVE`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *networkResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = networkingClientOf(req, resp)
}

func (r *networkResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data networkResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting network creation request.", map[string]any{})

	network, err := r.client.CreateLocalNetwork(ctx)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to create network resource",
			"An unexpected error occurred while attempting to create network resource.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "Network creation request completed.", map[string]any{"id": network.ID})

	setNetworkModel(&data, network)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *networkResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data networkResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting network read request.", map[string]any{"id": data.ID.ValueString()})

	network, err := r.client.GetNetwork(ctx, data.ID.ValueString())
	if err != nil {
		if gophercloud.ResponseCodeIs(err, 404) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to read network resource",
			"An unexpected error occurred while attempting to read network resource.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	setNetworkModel(&data, network)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *networkResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	// 指定できる引数が無いため、更新は起きない
	resp.Diagnostics.AddError(
		"An unexpected operation occurred",
		"Updating network resource is not supported.",
	)
}

func (r *networkResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data networkResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting network deletion request.", map[string]any{"id": data.ID.ValueString()})

	if err := r.client.DeleteNetwork(ctx, data.ID.ValueString()); err != nil && !gophercloud.ResponseCodeIs(err, 404) {
		resp.Diagnostics.AddError(
			"Failed to delete network resource",
			"An unexpected error occurred while attempting to delete network resource. "+
				"A network cannot be deleted while subnets remain on it.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "Network deletion request completed.", map[string]any{"id": data.ID.ValueString()})
}

func (r *networkResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func setNetworkModel(data *networkResourceModel, network *service.Network) {
	data.ID = types.StringValue(network.ID)
	data.Name = types.StringValue(network.Name)
	data.MTU = types.Int64Value(int64(network.MTU))
	data.Status = types.StringValue(network.Status)
}

// プロバイダから渡された API クライアントを取り出す（ネットワーク系のリソースで共用する）.
func networkingClientOf(req resource.ConfigureRequest, resp *resource.ConfigureResponse) *service.ConohaClient {
	if req.ProviderData == nil {
		return nil
	}

	client, ok := req.ProviderData.(*service.ConohaClient)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected resource configure type",
			fmt.Sprintf("Expected *service.ConohaClient, but got: %T.", req.ProviderData),
		)
		return nil
	}

	return client
}
