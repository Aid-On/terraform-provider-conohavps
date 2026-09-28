// サーバーへのポートのアタッチのリソースを提供する.
// ローカルネットワークのポートや追加IPのポートをサーバーに付け、削除でデタッチする.
// アタッチは同期（200）で終わるため待たず、デタッチは非同期（202）のため一覧から消えるまで待つ.

package resource

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/attachinterfaces"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ resource.Resource                = &portAttachmentResource{}
	_ resource.ResourceWithImportState = &portAttachmentResource{}
)

// デタッチが終わるのを待つ上限.
const portDetachTimeout = 10 * time.Minute

func NewPortAttachmentResource() resource.Resource {
	return &portAttachmentResource{}
}

type portAttachmentResource struct {
	client *service.ConohaClient
}

// ポートのアタッチのリソースモデル.
type portAttachmentResourceModel struct {
	ID          types.String `tfsdk:"id"`           // "<サーバー ID>/<ポート ID>"
	ServerID    types.String `tfsdk:"server_id"`    // サーバー ID（リクエスト）
	PortID      types.String `tfsdk:"port_id"`      // ポート ID（リクエスト）
	NetworkID   types.String `tfsdk:"network_id"`   // ネットワーク ID
	MACAddress  types.String `tfsdk:"mac_address"`  // MAC アドレス
	IPAddresses types.List   `tfsdk:"ip_addresses"` // IP アドレス
}

func (r *portAttachmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_port_attachment"
}

func (r *portAttachmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Attaches a port (a `conohavps_port` on a local network or a `conohavps_additional_ip`) to a server. " +
			"The server must be running or stopped (`ACTIVE` or `SHUTOFF`), not in the middle of another operation. " +
			"Attaching completes in the API call; destroying it detaches the port and waits until the server no longer lists it.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The ID of the attachment, in the form `<server_id>/<port_id>`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"server_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the server (`conohavps_instance`). Changing this value will force the port to be reattached.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"port_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the port to attach. Changing this value will force the port to be reattached.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"network_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the network of the port.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"mac_address": schema.StringAttribute{
				MarkdownDescription: "The MAC address of the port.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"ip_addresses": schema.ListAttribute{
				MarkdownDescription: "The IP addresses of the port.",
				ElementType:         types.StringType,
				Computed:            true,
				PlanModifiers:       []planmodifier.List{listplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *portAttachmentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = networkingClientOf(req, resp)
}

func (r *portAttachmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan portAttachmentResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	serverID, portID := plan.ServerID.ValueString(), plan.PortID.ValueString()

	tflog.Debug(ctx, "Starting port attach request.", map[string]any{"server_id": serverID, "port_id": portID})

	iface, err := r.client.AttachPort(ctx, serverID, portID)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to create port attachment resource",
			"An unexpected error occurred while attempting to attach the port to the server.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "Port attach request completed.", map[string]any{"server_id": serverID, "port_id": iface.PortID})

	resp.Diagnostics.Append(setPortAttachmentModel(ctx, &plan, serverID, iface)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *portAttachmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state portAttachmentResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	serverID, portID := state.ServerID.ValueString(), state.PortID.ValueString()

	tflog.Debug(ctx, "Starting port attachment read request.", map[string]any{"server_id": serverID, "port_id": portID})

	iface, err := r.client.GetAttachedPort(ctx, serverID, portID)
	if err != nil {
		if gophercloud.ResponseCodeIs(err, 404) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to read port attachment resource",
			"An unexpected error occurred while attempting to read port attachment resource.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	resp.Diagnostics.Append(setPortAttachmentModel(ctx, &state, serverID, iface)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *portAttachmentResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	// アタッチは更新不可（引数を変えると付け直す）
	resp.Diagnostics.AddError(
		"An unexpected operation occurred",
		"Updating port attachment resource is not supported. If changes are needed, detach and reattach the port.",
	)
}

func (r *portAttachmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state portAttachmentResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	serverID, portID := state.ServerID.ValueString(), state.PortID.ValueString()

	tflog.Debug(ctx, "Starting port detach request.", map[string]any{"server_id": serverID, "port_id": portID})

	if err := r.client.DetachPort(ctx, serverID, portID, portDetachTimeout); err != nil {
		resp.Diagnostics.AddError(
			"Failed to delete port attachment resource",
			"An unexpected error occurred while attempting to detach the port from the server.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "Port detach request completed.", map[string]any{"server_id": serverID, "port_id": portID})
}

// "<サーバー ID>/<ポート ID>" の形でインポートする.
func (r *portAttachmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	serverID, portID, ok := strings.Cut(req.ID, "/")
	if !ok || serverID == "" || portID == "" || strings.Contains(portID, "/") {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("Expected an import ID in the form <server_id>/<port_id>, but got %q.", req.ID),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("server_id"), serverID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("port_id"), portID)...)
}

func setPortAttachmentModel(ctx context.Context, data *portAttachmentResourceModel, serverID string, iface *attachinterfaces.Interface) diag.Diagnostics {
	data.ID = types.StringValue(serverID + "/" + iface.PortID)
	data.ServerID = types.StringValue(serverID)
	data.PortID = types.StringValue(iface.PortID)
	data.NetworkID = types.StringValue(iface.NetID)
	data.MACAddress = types.StringValue(iface.MACAddr)

	addrs := make([]string, 0, len(iface.FixedIPs))
	for _, ip := range iface.FixedIPs {
		addrs = append(addrs, ip.IPAddress)
	}
	list, diags := types.ListValueFrom(ctx, types.StringType, addrs)
	data.IPAddresses = list
	return diags
}
