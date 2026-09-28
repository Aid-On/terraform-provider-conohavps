// 追加IPアドレスのリソースを提供する.
// ConoHa 独自の allocateips で、指定した個数の IP アドレスを持つポートを1つ作る.
// 個数は変えられないため作り直しになるが、セキュリティグループと QoS ポリシーはポート更新の API でその場で変える.

package resource

import (
	"context"
	"net/netip"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ resource.Resource                = &additionalIPResource{}
	_ resource.ResourceWithImportState = &additionalIPResource{}
)

func NewAdditionalIPResource() resource.Resource {
	return &additionalIPResource{}
}

type additionalIPResource struct {
	client *service.ConohaClient
}

// 追加IPのリソースモデル.
type additionalIPResourceModel struct {
	ID                 types.String `tfsdk:"id"`                    // ポート ID
	Count              types.Int64  `tfsdk:"ip_count"`              // IP アドレスの個数（リクエスト）
	SecurityGroupIDs   types.Set    `tfsdk:"security_group_ids"`    // セキュリティグループ ID（リクエスト）
	QoSPolicyID        types.String `tfsdk:"qos_policy_id"`         // QoS ポリシー ID（更新のリクエスト）
	QoSNetworkPolicyID types.String `tfsdk:"qos_network_policy_id"` // ネットワークの QoS ポリシー ID
	IPAddresses        types.List   `tfsdk:"ip_addresses"`          // 割り当てられた IP アドレス
	NetworkID          types.String `tfsdk:"network_id"`            // ネットワーク ID
	Name               types.String `tfsdk:"name"`                  // ポート名（ConoHa が付ける）
	MACAddress         types.String `tfsdk:"mac_address"`           // MAC アドレス
}

func (r *additionalIPResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_additional_ip"
}

func (r *additionalIPResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages additional IP addresses. ConoHa allocates the addresses on one port; " +
			"attach it to a server with `conohavps_port_attachment`. The port cannot be deleted while it is attached to a server. " +
			"ConoHa does not let additional IP addresses be cancelled for 30 days after allocation, and the fee grows with the number of addresses.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The ID of the port that holds the addresses.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"ip_count": schema.Int64Attribute{
				MarkdownDescription: "The number of IP addresses to allocate, from 1 to 16. Changing this value will force the addresses to be reallocated (they change).",
				Required:            true,
				Validators:          []validator.Int64{int64validator.Between(1, 16)},
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.RequiresReplace()},
			},
			"security_group_ids":    securityGroupIDsAttribute("port"),
			"qos_policy_id":         qosPolicyIDAttribute("port"),
			"qos_network_policy_id": qosNetworkPolicyIDAttribute("port"),
			"ip_addresses": schema.ListAttribute{
				MarkdownDescription: "The allocated IP addresses.",
				ElementType:         types.StringType,
				Computed:            true,
				PlanModifiers:       []planmodifier.List{listplanmodifier.UseStateForUnknown()},
			},
			"network_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the network the addresses belong to.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name ConoHa gives the port, such as `add-i_100000-o_100000-p_0a`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"mac_address": schema.StringAttribute{
				MarkdownDescription: "The MAC address of the port.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *additionalIPResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = networkingClientOf(req, resp)
}

func (r *additionalIPResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan additionalIPResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	opts := service.AllocateIPsOpts{Count: int(plan.Count.ValueInt64())}
	if !plan.SecurityGroupIDs.IsUnknown() && !plan.SecurityGroupIDs.IsNull() {
		resp.Diagnostics.Append(plan.SecurityGroupIDs.ElementsAs(ctx, &opts.SecurityGroups, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	tflog.Debug(ctx, "Starting additional IP allocation request.", map[string]any{"count": opts.Count})

	port, err := r.client.AllocateIPs(ctx, opts)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to create additional IP resource",
			"An unexpected error occurred while attempting to allocate additional IP addresses.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	// ID を先に保存し、QoS ポリシーの設定に失敗しても作ったポートを見失わないようにする
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), port.ID)...)

	if qos := plan.QoSPolicyID; !qos.IsUnknown() && !qos.IsNull() {
		id := qos.ValueString()
		port, err = r.client.UpdatePort(ctx, port.ID, service.PortUpdateOpts{QoSPolicyID: &id})
		if err != nil {
			resp.Diagnostics.AddError(
				"Failed to set QoS policy of additional IP resource",
				"The additional IP addresses were allocated, but setting their QoS policy failed.\n\n"+
					"Error: "+err.Error(),
			)
			return
		}
	}

	tflog.Debug(ctx, "Additional IP allocation request completed.", map[string]any{"id": port.ID})

	resp.Diagnostics.Append(setAdditionalIPModel(ctx, &plan, port)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *additionalIPResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state additionalIPResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting additional IP read request.", map[string]any{"id": state.ID.ValueString()})

	port, err := r.client.GetPort(ctx, state.ID.ValueString())
	if err != nil {
		if gophercloud.ResponseCodeIs(err, 404) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to read additional IP resource",
			"An unexpected error occurred while attempting to read additional IP resource.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	resp.Diagnostics.Append(setAdditionalIPModel(ctx, &state, port)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *additionalIPResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state additionalIPResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var opts service.PortUpdateOpts
	opts.SecurityGroups, opts.QoSPolicyID = changedSecurityAndQoS(ctx, plan.SecurityGroupIDs, state.SecurityGroupIDs, plan.QoSPolicyID, state.QoSPolicyID, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting additional IP update request.", map[string]any{"id": state.ID.ValueString()})

	port, err := r.client.UpdatePort(ctx, state.ID.ValueString(), opts)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to update additional IP resource",
			"An unexpected error occurred while attempting to update additional IP resource.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "Additional IP update request completed.", map[string]any{"id": port.ID})

	resp.Diagnostics.Append(setAdditionalIPModel(ctx, &plan, port)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *additionalIPResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state additionalIPResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting additional IP deletion request.", map[string]any{"id": state.ID.ValueString()})

	if err := r.client.DeletePort(ctx, state.ID.ValueString()); err != nil && !gophercloud.ResponseCodeIs(err, 404) {
		resp.Diagnostics.AddError(
			"Failed to delete additional IP resource",
			"An unexpected error occurred while attempting to delete additional IP resource. "+
				"The port cannot be deleted while it is attached to a server, and ConoHa refuses to cancel additional IP addresses within 30 days of allocation.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "Additional IP deletion request completed.", map[string]any{"id": state.ID.ValueString()})
}

func (r *additionalIPResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// レスポンスの値をモデルに写す.
// 個数は作成時の指定を保ち、インポート時だけ割り当てられた IPv4 アドレスの数から決める.
func setAdditionalIPModel(ctx context.Context, data *additionalIPResourceModel, port *service.Port) diag.Diagnostics {
	var diags diag.Diagnostics

	data.ID = types.StringValue(port.ID)
	data.NetworkID = types.StringValue(port.NetworkID)
	data.Name = types.StringValue(port.Name)
	data.MACAddress = types.StringValue(port.MACAddress)

	addrs := make([]string, 0, len(port.FixedIPs))
	v4 := 0
	for _, ip := range port.FixedIPs {
		addrs = append(addrs, ip.IPAddress)
		if a, err := netip.ParseAddr(ip.IPAddress); err == nil && a.Is4() {
			v4++
		}
	}
	list, d := types.ListValueFrom(ctx, types.StringType, addrs)
	diags.Append(d...)
	data.IPAddresses = list

	if data.Count.IsNull() || data.Count.IsUnknown() {
		data.Count = types.Int64Value(int64(v4))
	}

	data.SecurityGroupIDs, data.QoSPolicyID = securityAndQoSValues(ctx, port, data.QoSPolicyID, &diags)
	data.QoSNetworkPolicyID = types.StringPointerValue(port.QoSNetworkPolicyID)
	return diags
}
