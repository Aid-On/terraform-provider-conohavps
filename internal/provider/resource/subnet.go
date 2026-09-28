// ローカルネットワーク用のサブネットのリソースを提供する.
// サブネットは更新の API が無いため、引数を変えると作り直す.

package resource

import (
	"context"
	"fmt"
	"net/netip"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/hashicorp/terraform-plugin-framework/attr"
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
	_ resource.Resource                = &subnetResource{}
	_ resource.ResourceWithImportState = &subnetResource{}
)

func NewSubnetResource() resource.Resource {
	return &subnetResource{}
}

type subnetResource struct {
	client *service.ConohaClient
}

// サブネットのリソースモデル.
type subnetResourceModel struct {
	ID              types.String `tfsdk:"id"`               // サブネット ID
	NetworkID       types.String `tfsdk:"network_id"`       // ネットワーク ID（リクエスト）
	CIDR            types.String `tfsdk:"cidr"`             // ネットワークアドレス（リクエスト）
	Name            types.String `tfsdk:"name"`             // サブネット名（ConoHa が付ける）
	IPVersion       types.Int64  `tfsdk:"ip_version"`       // IP のバージョン
	GatewayIP       types.String `tfsdk:"gateway_ip"`       // ゲートウェイ（ローカルネットワークでは無い）
	AllocationPools types.List   `tfsdk:"allocation_pools"` // 割り当て範囲
}

var allocationPoolType = types.ObjectType{AttrTypes: map[string]attr.Type{
	"start": types.StringType,
	"end":   types.StringType,
}}

func (r *subnetResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_subnet"
}

func (r *subnetResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a subnet of a local network. A subnet cannot be deleted while ports remain on it.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The ID of the subnet.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"network_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the local network (`conohavps_network`). Changing this value will force the subnet to be recreated.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"cidr": schema.StringAttribute{
				MarkdownDescription: "The network address in CIDR notation. It must be a private IPv4 network " +
					"(`10.0.0.0/8`, `172.16.0.0/12` or `192.168.0.0/16`) with a prefix length from /21 to /27, written as its network address (e.g. `10.0.0.0/24`). " +
					"Changing this value will force the subnet to be recreated.",
				Required:      true,
				Validators:    []validator.String{localSubnetCIDRValidator{}},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name ConoHa gives the subnet, such as `local-10-0-0-0-24`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"ip_version": schema.Int64Attribute{
				MarkdownDescription: "The IP version of the subnet.",
				Computed:            true,
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"gateway_ip": schema.StringAttribute{
				MarkdownDescription: "The gateway IP address of the subnet. Local network subnets have none, so it is usually null.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"allocation_pools": schema.ListNestedAttribute{
				MarkdownDescription: "The ranges of addresses ports on the subnet get.",
				Computed:            true,
				PlanModifiers:       []planmodifier.List{listplanmodifier.UseStateForUnknown()},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"start": schema.StringAttribute{MarkdownDescription: "The first address of the range.", Computed: true},
						"end":   schema.StringAttribute{MarkdownDescription: "The last address of the range.", Computed: true},
					},
				},
			},
		},
	}
}

func (r *subnetResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = networkingClientOf(req, resp)
}

func (r *subnetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data subnetResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	opts := service.SubnetCreateOpts{
		NetworkID: data.NetworkID.ValueString(),
		CIDR:      data.CIDR.ValueString(),
	}

	tflog.Debug(ctx, "Starting subnet creation request.", map[string]any{"network_id": opts.NetworkID, "cidr": opts.CIDR})

	subnet, err := r.client.CreateSubnet(ctx, opts)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to create subnet resource",
			"An unexpected error occurred while attempting to create subnet resource.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "Subnet creation request completed.", map[string]any{"id": subnet.ID})

	resp.Diagnostics.Append(setSubnetModel(ctx, &data, subnet)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *subnetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data subnetResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting subnet read request.", map[string]any{"id": data.ID.ValueString()})

	subnet, err := r.client.GetSubnet(ctx, data.ID.ValueString())
	if err != nil {
		if gophercloud.ResponseCodeIs(err, 404) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to read subnet resource",
			"An unexpected error occurred while attempting to read subnet resource.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	resp.Diagnostics.Append(setSubnetModel(ctx, &data, subnet)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *subnetResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	// サブネットは更新不可（引数を変えると作り直す）
	resp.Diagnostics.AddError(
		"An unexpected operation occurred",
		"Updating subnet resource is not supported. If changes are needed, delete and recreate the subnet.",
	)
}

func (r *subnetResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data subnetResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting subnet deletion request.", map[string]any{"id": data.ID.ValueString()})

	if err := r.client.DeleteSubnet(ctx, data.ID.ValueString()); err != nil && !gophercloud.ResponseCodeIs(err, 404) {
		resp.Diagnostics.AddError(
			"Failed to delete subnet resource",
			"An unexpected error occurred while attempting to delete subnet resource. "+
				"A subnet cannot be deleted while ports remain on it.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "Subnet deletion request completed.", map[string]any{"id": data.ID.ValueString()})
}

func (r *subnetResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func setSubnetModel(ctx context.Context, data *subnetResourceModel, subnet *service.Subnet) diag.Diagnostics {
	data.ID = types.StringValue(subnet.ID)
	data.NetworkID = types.StringValue(subnet.NetworkID)
	data.CIDR = types.StringValue(subnet.CIDR)
	data.Name = types.StringValue(subnet.Name)
	data.IPVersion = types.Int64Value(int64(subnet.IPVersion))
	data.GatewayIP = types.StringPointerValue(subnet.GatewayIP)

	pools := make([]subnetAllocationPoolModel, 0, len(subnet.AllocationPools))
	for _, p := range subnet.AllocationPools {
		pools = append(pools, subnetAllocationPoolModel{Start: types.StringValue(p.Start), End: types.StringValue(p.End)})
	}
	list, diags := types.ListValueFrom(ctx, allocationPoolType, pools)
	data.AllocationPools = list
	return diags
}

type subnetAllocationPoolModel struct {
	Start types.String `tfsdk:"start"`
	End   types.String `tfsdk:"end"`
}

// ローカルネットワーク用のサブネットに指定できるネットワークアドレスか確かめる.
// ドキュメントの「ネットワークアドレスの種類」（クラス A・B・C のプライベートアドレスで /21～/27）に従う.
// OpenAPI 定義は cidr を文字列とだけ書き、この制約を載せていない（否定もしていない）.
type localSubnetCIDRValidator struct{}

var localSubnetRanges = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
}

func (v localSubnetCIDRValidator) Description(_ context.Context) string {
	return "must be a private IPv4 network address (10.0.0.0/8, 172.16.0.0/12 or 192.168.0.0/16) with a prefix length from /21 to /27"
}

func (v localSubnetCIDRValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v localSubnetCIDRValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if err := checkLocalSubnetCIDR(req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid subnet CIDR", err.Error())
	}
}

func checkLocalSubnetCIDR(s string) error {
	prefix, err := netip.ParsePrefix(s)
	if err != nil {
		return fmt.Errorf("%q is not in CIDR notation: %w", s, err)
	}
	if !prefix.Addr().Is4() {
		return fmt.Errorf("%q is not an IPv4 network", s)
	}
	if prefix.Bits() < 21 || prefix.Bits() > 27 {
		return fmt.Errorf("the prefix length of %q must be from /21 to /27", s)
	}
	if prefix.Masked() != prefix {
		return fmt.Errorf("%q is not a network address; use %q", s, prefix.Masked().String())
	}
	for _, r := range localSubnetRanges {
		if r.Contains(prefix.Addr()) {
			return nil
		}
	}
	return fmt.Errorf("%q is not in 10.0.0.0/8, 172.16.0.0/12 or 192.168.0.0/16", s)
}
