// ローカルネットワーク用のポートのリソースを提供する.
// ネットワークは作り直しになるが、IP アドレス・セキュリティグループ・VIP・QoS ポリシーはポート更新の API でその場で変える.

package resource

import (
	"context"
	"fmt"
	"net/netip"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ resource.Resource                = &portResource{}
	_ resource.ResourceWithImportState = &portResource{}
)

func NewPortResource() resource.Resource {
	return &portResource{}
}

type portResource struct {
	client *service.ConohaClient
}

// ポートのリソースモデル.
type portResourceModel struct {
	ID                  types.String `tfsdk:"id"`                    // ポート ID
	NetworkID           types.String `tfsdk:"network_id"`            // ネットワーク ID（リクエスト）
	FixedIPs            types.List   `tfsdk:"fixed_ips"`             // 割り当てる IP アドレス（リクエスト）
	SecurityGroupIDs    types.Set    `tfsdk:"security_group_ids"`    // セキュリティグループ ID（リクエスト）
	AllowedAddressPairs types.Set    `tfsdk:"allowed_address_pairs"` // VIP のネットワークアドレス（リクエスト）
	QoSPolicyID         types.String `tfsdk:"qos_policy_id"`         // QoS ポリシー ID（更新のリクエスト）
	Name                types.String `tfsdk:"name"`                  // ポート名（ConoHa が付ける）
	MACAddress          types.String `tfsdk:"mac_address"`           // MAC アドレス
}

type portFixedIPModel struct {
	SubnetID  types.String `tfsdk:"subnet_id"`
	IPAddress types.String `tfsdk:"ip_address"`
}

type portAddressPairModel struct {
	IPAddress types.String `tfsdk:"ip_address"`
}

var (
	portFixedIPType = types.ObjectType{AttrTypes: map[string]attr.Type{
		"subnet_id":  types.StringType,
		"ip_address": types.StringType,
	}}
	portAddressPairType = types.ObjectType{AttrTypes: map[string]attr.Type{
		"ip_address": types.StringType,
	}}
)

func (r *portResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_port"
}

func (r *portResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a port on a local network. The network needs a `conohavps_subnet` before a port can be created. " +
			"Attach the port to a server with `conohavps_port_attachment`. A port cannot be deleted while it is attached to a server.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The ID of the port.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"network_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the local network (`conohavps_network`). Changing this value will force the port to be recreated.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"fixed_ips": schema.ListNestedAttribute{
				MarkdownDescription: "The IP addresses of the port. Omit it to have one address assigned automatically. " +
					"Changing this value will update the addresses in place; removing it from the configuration keeps the current addresses.",
				Optional:      true,
				Computed:      true,
				Validators:    []validator.List{listvalidator.SizeAtLeast(1)},
				PlanModifiers: []planmodifier.List{fixedIPsPlanModifier{}},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"subnet_id": schema.StringAttribute{
							MarkdownDescription: "The ID of the subnet (`conohavps_subnet`) the address comes from.",
							Required:            true,
						},
						"ip_address": schema.StringAttribute{
							MarkdownDescription: "The IP address to assign from the subnet. Omit it to have one assigned automatically.",
							Optional:            true,
							Computed:            true,
							Validators:          []validator.String{portIPAddressValidator{}},
						},
					},
				},
			},
			"security_group_ids": securityGroupIDsAttribute("port"),
			"allowed_address_pairs": schema.SetNestedAttribute{
				MarkdownDescription: "The network addresses the port can also use, for use as a VIP. Changing this value will update the port in place.",
				Optional:            true,
				Computed:            true,
				Default:             setdefault.StaticValue(types.SetValueMust(portAddressPairType, []attr.Value{})),
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"ip_address": schema.StringAttribute{
							MarkdownDescription: "The network address in CIDR notation, such as `10.0.0.100/32`.",
							Required:            true,
							Validators:          []validator.String{portCIDRValidator{}},
						},
					},
				},
			},
			"qos_policy_id": qosPolicyIDAttribute("port"),
			"name": schema.StringAttribute{
				MarkdownDescription: "The name ConoHa gives the port.",
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

// ポートと追加IPで共通の、セキュリティグループ ID の属性.
func securityGroupIDsAttribute(what string) schema.SetAttribute {
	return schema.SetAttribute{
		MarkdownDescription: fmt.Sprintf("The IDs of the security groups of the %s. When omitted, ConoHa sets the `default` security group. "+
			"Changing this value will update the %s in place; removing it from the configuration keeps the current security groups.", what, what),
		ElementType:   types.StringType,
		Optional:      true,
		Computed:      true,
		Validators:    []validator.Set{setvalidator.SizeAtLeast(1)},
		PlanModifiers: []planmodifier.Set{setplanmodifier.UseStateForUnknown()},
	}
}

// ポートと追加IPで共通の、QoS ポリシー ID の属性. 作成の API は取らないため、作成後に更新の API で設定する.
func qosPolicyIDAttribute(what string) schema.StringAttribute {
	return schema.StringAttribute{
		MarkdownDescription: fmt.Sprintf("The ID of the QoS policy of the %s (see the `conohavps_qos_policy` data source). "+
			"It is set with a port update right after creation. Changing this value will update the %s in place; "+
			"removing it from the configuration keeps the current policy.", what, what),
		Optional:      true,
		Computed:      true,
		PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
	}
}

func (r *portResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = networkingClientOf(req, resp)
}

func (r *portResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan portResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	opts := service.PortCreateOpts{NetworkID: plan.NetworkID.ValueString()}
	if !plan.FixedIPs.IsUnknown() && !plan.FixedIPs.IsNull() {
		opts.FixedIPs = fixedIPsRequest(ctx, plan.FixedIPs, &resp.Diagnostics)
	}
	if !plan.SecurityGroupIDs.IsUnknown() && !plan.SecurityGroupIDs.IsNull() {
		resp.Diagnostics.Append(plan.SecurityGroupIDs.ElementsAs(ctx, &opts.SecurityGroups, false)...)
	}
	opts.AllowedAddressPairs = addressPairsRequest(ctx, plan.AllowedAddressPairs, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting port creation request.", map[string]any{"network_id": opts.NetworkID})

	port, err := r.client.CreatePort(ctx, opts)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to create port resource",
			"An unexpected error occurred while attempting to create port resource.\n\n"+
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
				"Failed to set QoS policy of port resource",
				"The port was created, but setting its QoS policy failed.\n\n"+
					"Error: "+err.Error(),
			)
			return
		}
	}

	tflog.Debug(ctx, "Port creation request completed.", map[string]any{"id": port.ID})

	resp.Diagnostics.Append(setPortModel(ctx, &plan, port)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *portResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state portResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting port read request.", map[string]any{"id": state.ID.ValueString()})

	port, err := r.client.GetPort(ctx, state.ID.ValueString())
	if err != nil {
		if gophercloud.ResponseCodeIs(err, 404) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to read port resource",
			"An unexpected error occurred while attempting to read port resource.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	resp.Diagnostics.Append(setPortModel(ctx, &state, port)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *portResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state portResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// 変わった項目だけを送る（ポート更新は指定した項目だけを変える）
	var opts service.PortUpdateOpts
	if !plan.FixedIPs.Equal(state.FixedIPs) {
		ips := fixedIPsRequest(ctx, plan.FixedIPs, &resp.Diagnostics)
		opts.FixedIPs = &ips
	}
	if !plan.AllowedAddressPairs.Equal(state.AllowedAddressPairs) {
		pairs := addressPairsRequest(ctx, plan.AllowedAddressPairs, &resp.Diagnostics)
		if pairs == nil {
			pairs = []service.PortAddressPair{}
		}
		opts.AllowedAddressPairs = &pairs
	}
	opts.SecurityGroups, opts.QoSPolicyID = changedSecurityAndQoS(ctx, plan.SecurityGroupIDs, state.SecurityGroupIDs, plan.QoSPolicyID, state.QoSPolicyID, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting port update request.", map[string]any{"id": state.ID.ValueString()})

	port, err := r.client.UpdatePort(ctx, state.ID.ValueString(), opts)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to update port resource",
			"An unexpected error occurred while attempting to update port resource.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "Port update request completed.", map[string]any{"id": port.ID})

	resp.Diagnostics.Append(setPortModel(ctx, &plan, port)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *portResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state portResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting port deletion request.", map[string]any{"id": state.ID.ValueString()})

	if err := r.client.DeletePort(ctx, state.ID.ValueString()); err != nil && !gophercloud.ResponseCodeIs(err, 404) {
		resp.Diagnostics.AddError(
			"Failed to delete port resource",
			"An unexpected error occurred while attempting to delete port resource. "+
				"A port cannot be deleted while it is attached to a server.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "Port deletion request completed.", map[string]any{"id": state.ID.ValueString()})
}

func (r *portResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// レスポンスの値をモデルに写す.
func setPortModel(ctx context.Context, data *portResourceModel, port *service.Port) diag.Diagnostics {
	var diags diag.Diagnostics

	data.ID = types.StringValue(port.ID)
	data.NetworkID = types.StringValue(port.NetworkID)
	data.Name = types.StringValue(port.Name)
	data.MACAddress = types.StringValue(port.MACAddress)

	ips := make([]portFixedIPModel, 0, len(port.FixedIPs))
	for _, ip := range port.FixedIPs {
		ips = append(ips, portFixedIPModel{SubnetID: types.StringValue(ip.SubnetID), IPAddress: types.StringValue(ip.IPAddress)})
	}
	var d diag.Diagnostics
	data.FixedIPs, d = types.ListValueFrom(ctx, portFixedIPType, ips)
	diags.Append(d...)

	pairs := make([]portAddressPairModel, 0, len(port.AllowedAddressPairs))
	for _, p := range port.AllowedAddressPairs {
		pairs = append(pairs, portAddressPairModel{IPAddress: types.StringValue(p.IPAddress)})
	}
	data.AllowedAddressPairs, d = types.SetValueFrom(ctx, portAddressPairType, pairs)
	diags.Append(d...)

	data.SecurityGroupIDs, data.QoSPolicyID = securityAndQoSValues(ctx, port, data.QoSPolicyID, &diags)
	return diags
}

// セキュリティグループと QoS ポリシーの値をレスポンスから作る.
// QoS ポリシー ID がレスポンスに無い場合は、今の値を保つ.
func securityAndQoSValues(ctx context.Context, port *service.Port, current types.String, diags *diag.Diagnostics) (types.Set, types.String) {
	groups := port.SecurityGroups
	if groups == nil {
		groups = []string{}
	}
	sgs, d := types.SetValueFrom(ctx, types.StringType, groups)
	diags.Append(d...)

	qos := current
	if port.QoSPolicyID != nil {
		qos = types.StringValue(*port.QoSPolicyID)
	} else if qos.IsUnknown() {
		qos = types.StringNull()
	}
	return sgs, qos
}

// 変わったセキュリティグループと QoS ポリシーだけを更新のリクエストにする.
func changedSecurityAndQoS(ctx context.Context, planSGs, stateSGs types.Set, planQoS, stateQoS types.String, diags *diag.Diagnostics) (*[]string, *string) {
	var sgs *[]string
	if !planSGs.IsUnknown() && !planSGs.IsNull() && !planSGs.Equal(stateSGs) {
		var ids []string
		diags.Append(planSGs.ElementsAs(ctx, &ids, false)...)
		sgs = &ids
	}
	var qos *string
	if !planQoS.IsUnknown() && !planQoS.IsNull() && !planQoS.Equal(stateQoS) {
		id := planQoS.ValueString()
		qos = &id
	}
	return sgs, qos
}

// fixed_ips をリクエストの形にする. ip_address が決まっていないものはサブネットだけを送り、自動で割り当てさせる.
func fixedIPsRequest(ctx context.Context, list types.List, diags *diag.Diagnostics) []service.PortFixedIP {
	var models []portFixedIPModel
	diags.Append(list.ElementsAs(ctx, &models, false)...)
	ips := make([]service.PortFixedIP, 0, len(models))
	for _, m := range models {
		ip := service.PortFixedIP{SubnetID: m.SubnetID.ValueString()}
		if !m.IPAddress.IsUnknown() && !m.IPAddress.IsNull() {
			ip.IPAddress = m.IPAddress.ValueString()
		}
		ips = append(ips, ip)
	}
	return ips
}

// allowed_address_pairs をリクエストの形にする. 空なら nil を返す.
func addressPairsRequest(ctx context.Context, set types.Set, diags *diag.Diagnostics) []service.PortAddressPair {
	if set.IsUnknown() || set.IsNull() {
		return nil
	}
	var models []portAddressPairModel
	diags.Append(set.ElementsAs(ctx, &models, false)...)
	var pairs []service.PortAddressPair
	for _, m := range models {
		pairs = append(pairs, service.PortAddressPair{IPAddress: m.IPAddress.ValueString()})
	}
	return pairs
}

// fixed_ips の計画を決める.
// 設定が今の割り当てと同じ（サブネットが一致し、IP アドレスを省いたか同じ値を書いた）なら今の値を計画にし、
// 自動で割り当てられた IP アドレスを不明にしない. 設定から消した場合も今の値を保つ.
type fixedIPsPlanModifier struct{}

func (m fixedIPsPlanModifier) Description(_ context.Context) string {
	return "Keeps the current fixed IPs when the configuration matches them or is omitted."
}

func (m fixedIPsPlanModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m fixedIPsPlanModifier) PlanModifyList(ctx context.Context, req planmodifier.ListRequest, resp *planmodifier.ListResponse) {
	if req.StateValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if req.ConfigValue.IsNull() {
		resp.PlanValue = req.StateValue
		return
	}

	var config, state []portFixedIPModel
	resp.Diagnostics.Append(req.ConfigValue.ElementsAs(ctx, &config, false)...)
	resp.Diagnostics.Append(req.StateValue.ElementsAs(ctx, &state, false)...)
	if resp.Diagnostics.HasError() || len(config) != len(state) {
		return
	}
	for i := range config {
		if config[i].SubnetID.IsUnknown() || config[i].IPAddress.IsUnknown() {
			return
		}
		if !config[i].SubnetID.Equal(state[i].SubnetID) {
			return
		}
		if !config[i].IPAddress.IsNull() && !config[i].IPAddress.Equal(state[i].IPAddress) {
			return
		}
	}
	resp.PlanValue = req.StateValue
}

// IP アドレスの形か確かめる.
type portIPAddressValidator struct{}

func (v portIPAddressValidator) Description(_ context.Context) string { return "must be an IP address" }

func (v portIPAddressValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v portIPAddressValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if _, err := netip.ParseAddr(req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid IP address", fmt.Sprintf("%q is not an IP address.", req.ConfigValue.ValueString()))
	}
}

// CIDR 形式のネットワークアドレスか確かめる.
type portCIDRValidator struct{}

func (v portCIDRValidator) Description(_ context.Context) string {
	return "must be a network address in CIDR notation"
}

func (v portCIDRValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v portCIDRValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if _, err := netip.ParsePrefix(req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid CIDR", fmt.Sprintf("%q is not in CIDR notation (e.g. 10.0.0.100/32).", req.ConfigValue.ValueString()))
	}
}
