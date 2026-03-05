// セキュリティグループルールのリソースを提供する.

package resource

import (
	"context"
	"fmt"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/rules"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
)

var (
	_ resource.Resource                = &SecurityGroupRuleResource{}
	_ resource.ResourceWithImportState = &SecurityGroupRuleResource{}
)

func NewSecurityGroupRuleResource() resource.Resource {
	return &SecurityGroupRuleResource{}
}

type SecurityGroupRuleResource struct {
	client *service.ConohaClient
}

// セキュリティグループルールのリソースモデル.
type SecurityGroupRuleResourceModel struct {
	ID              types.String `tfsdk:"id"`
	SecurityGroupID types.String `tfsdk:"securitygroup_id"`
	Direction       types.String `tfsdk:"direction"`
	Ethertype       types.String `tfsdk:"ethertype"`
	Protocol        types.String `tfsdk:"protocol"`
	PortRangeMin    types.Int64  `tfsdk:"port_range_min"`
	PortRangeMax    types.Int64  `tfsdk:"port_range_max"`
	RemoteIpPrefix  types.String `tfsdk:"remote_ip_prefix"`
	RemoteGroupID   types.String `tfsdk:"remote_group_id"`
}

func (r *SecurityGroupRuleResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_securitygroup_rule"
}

func (r *SecurityGroupRuleResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages security group rule resource.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The ID of the security group rule.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"securitygroup_id": schema.StringAttribute{
				MarkdownDescription: "The security group ID to which the security group rule belongs. Changing this value will create a new security group rule.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"direction": schema.StringAttribute{
				MarkdownDescription: "The direction of the security group rule. Valid values are `egress` or `ingress` . Changing this value will create a new security group rule.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("egress", "ingress"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"ethertype": schema.StringAttribute{
				MarkdownDescription: "The layer 3 protocol type. Valid values are `IPv4` or `IPv6` . Changing this value will create a new security group rule.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("IPv4", "IPv6"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"protocol": schema.StringAttribute{
				MarkdownDescription: "The layer 4 protocol type. Valid values are `tcp` or `udp` or `icmp` . If omitted, accepts any protocol. When specifying this option, a port range needs to be specified. Changing this value will create a new security group rule.",
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("tcp", "udp", "icmp"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"port_range_min": schema.Int64Attribute{
				// protocol、port_range_max と併せて使用するオプション
				MarkdownDescription: "The lower part of the allowed port range. If omitted, accepts any port or any ICMP type. When the protocol is `tcp` or `udp` , valid value needs to be between `1` and `65535` . When the protocol is `icmp`, valid value needs to be between `0` and `255` . Changing this value will create a new security group rule.",
				Optional:            true,
				Validators: []validator.Int64{
					int64validator.Between(0, 65535),
					int64validator.AlsoRequires(path.MatchRoot("protocol")),
					int64validator.AlsoRequires(path.MatchRoot("port_range_max")),
				},
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"port_range_max": schema.Int64Attribute{
				// protocol、port_range_min と併せて使用するオプション
				MarkdownDescription: "The higher part of the allowed port range. If omitted, accepts any port or any ICMP type. When the protocol is `tcp` or `udp` , valid value needs to be between `1` and `65535` . When the protocol is `icmp`, valid value needs to be between `0` and `255` . Changing this value will create a new security group rule.",
				Optional:            true,
				Validators: []validator.Int64{
					int64validator.Between(0, 65535),
					int64validator.AlsoRequires(path.MatchRoot("protocol")),
					int64validator.AlsoRequires(path.MatchRoot("port_range_min")),
				},
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"remote_ip_prefix": schema.StringAttribute{
				// remote_group_id と競合するオプション
				MarkdownDescription: "The remote CIDR. The value needs to be a valid CIDR. This option is mutually exclusive with `remote_group_id` . Changing this value will create a new security group rule.",
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.ConflictsWith(
						path.Expressions{
							path.MatchRoot("remote_group_id"),
						}...),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"remote_group_id": schema.StringAttribute{
				// remote_ip_prefix と競合するオプション
				MarkdownDescription: "The ID of remote group. The value needs to be the security group ID. This option is mutually exclusive with `remote_ip_prefix` . Changing this value will create a new security group rule.",
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.ConflictsWith(
						path.Expressions{
							path.MatchRoot("remote_ip_prefix"),
						}...),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
		},
	}
}

func (r *SecurityGroupRuleResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*service.ConohaClient)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected resource configure type",
			fmt.Sprintf("Expected *service.ConohaClient, but got: %T.", req.ProviderData),
		)

		return
	}

	r.client = client
}

func (r *SecurityGroupRuleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan SecurityGroupRuleResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	createOpts := rules.CreateOpts{
		SecGroupID:     plan.SecurityGroupID.ValueString(),
		Direction:      rules.RuleDirection(plan.Direction.ValueString()),
		EtherType:      rules.RuleEtherType(plan.Ethertype.ValueString()),
		Protocol:       rules.RuleProtocol(plan.Protocol.ValueString()),
		PortRangeMin:   int(plan.PortRangeMin.ValueInt64()),
		PortRangeMax:   int(plan.PortRangeMax.ValueInt64()),
		RemoteIPPrefix: plan.RemoteIpPrefix.ValueString(),
		RemoteGroupID:  plan.RemoteGroupID.ValueString(),
	}

	tflog.Debug(ctx, "Starting security group rule creation request.", map[string]any{
		"securitygroup_id": plan.SecurityGroupID.ValueString(),
		"direction":        plan.Direction.ValueString(),
		"ethertype":        plan.Ethertype.ValueString(),
		"protocol":         plan.Protocol.ValueString(),
		"port_range_min":   plan.PortRangeMin.ValueInt64(),
		"port_range_max":   plan.PortRangeMax.ValueInt64(),
		"remote_ip_prefix": plan.RemoteIpPrefix.ValueString(),
		"remote_group_id":  plan.RemoteGroupID.ValueString(),
	})

	createResp, err := rules.Create(ctx, r.client.NetworkClient, createOpts).Extract()
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to create security group rule resource",
			"An unexpected error occurred while attempting to create the resource.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "Security group rule creation request completed.", map[string]any{"id": plan.ID.ValueString()})

	plan.ID = types.StringValue(createResp.ID)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SecurityGroupRuleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state SecurityGroupRuleResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	tflog.Debug(ctx, "Starting security group rule read request.", map[string]any{})

	readResp, err := rules.Get(ctx, r.client.NetworkClient, id).Extract()
	if err != nil {
		if gophercloud.ResponseCodeIs(err, 404) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to read security group rule resource",
			"An unexpected error occurred while attempting to read security group rule resource.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "Security group rule read request completed.", map[string]any{})

	state.SecurityGroupID = types.StringValue(readResp.SecGroupID)
	state.Direction = types.StringValue(readResp.Direction)
	state.Ethertype = types.StringValue(readResp.EtherType)

	if !state.Protocol.IsNull() {
		state.Protocol = types.StringValue(readResp.Protocol)
	}

	if !state.PortRangeMin.IsNull() {
		state.PortRangeMin = types.Int64Value(int64(readResp.PortRangeMin))
	}

	if !state.PortRangeMax.IsNull() {
		state.PortRangeMax = types.Int64Value(int64(readResp.PortRangeMax))
	}

	if !state.RemoteIpPrefix.IsNull() {
		state.RemoteIpPrefix = types.StringValue(readResp.RemoteIPPrefix)
	}

	if !state.RemoteGroupID.IsNull() {
		state.RemoteGroupID = types.StringValue(readResp.RemoteGroupID)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *SecurityGroupRuleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"An unexpected operation occurred",
		"Updating security group rule resource is not supported. If changes are needed, delete and recreate the security group rule.",
	)
}

func (r *SecurityGroupRuleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state SecurityGroupRuleResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	tflog.Debug(ctx, "Starting security group rule deletion request.", map[string]any{"id": state.ID.ValueString()})

	err := rules.Delete(ctx, r.client.NetworkClient, id).ExtractErr()
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to delete security group rule resource",
			"An unexpected error occurred while attempting to delete the resource.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "Security group rule deletion request completed.", map[string]any{"name": state.ID.ValueString()})
}

func (r *SecurityGroupRuleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
