// ロードバランサーのリソースを提供する.

package resource

import (
	"context"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ resource.Resource                = &lbLoadBalancerResource{}
	_ resource.ResourceWithConfigure   = &lbLoadBalancerResource{}
	_ resource.ResourceWithImportState = &lbLoadBalancerResource{}
)

func NewLBLoadBalancerResource() resource.Resource {
	return &lbLoadBalancerResource{}
}

type lbLoadBalancerResource struct {
	client *service.ConohaClient
}

// ロードバランサーのリソースモデル.
type lbLoadBalancerResourceModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	VipAddress      types.String `tfsdk:"vip_address"`
	VipPortID       types.String `tfsdk:"vip_port_id"`
	VipSubnetID     types.String `tfsdk:"vip_subnet_id"`
	VipNetworkID    types.String `tfsdk:"vip_network_id"`
	AdminStateUp    types.Bool   `tfsdk:"admin_state_up"`
	OperatingStatus types.String `tfsdk:"operating_status"`
}

func (r *lbLoadBalancerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_lb_loadbalancer"
}

func (r *lbLoadBalancerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	// VIP の値は追加時に決まり、以後変わらない
	computedVIP := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{
			MarkdownDescription: desc,
			Computed:            true,
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.UseStateForUnknown(),
			},
		}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a load balancer (the VIP that listeners accept connections on). " +
			"Creating it waits until its `provisioning_status` becomes `ACTIVE`.",
		Attributes: map[string]schema.Attribute{
			"id": computedVIP("The ID of the load balancer."),
			"name": schema.StringAttribute{
				// 仕様は名前を必須とするが長さの制約は書いていない. 空の名前は送らず、上限は OpenStack の 255 文字に揃える
				MarkdownDescription: "The name of the load balancer, shown as the name tag in the control panel. " +
					"The name length needs to be between 1 and 255. Changing this value updates the name in place.",
				Required: true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 255),
				},
			},
			"vip_address":    computedVIP("The virtual IP address of the load balancer."),
			"vip_port_id":    computedVIP("The ID of the port of the virtual IP."),
			"vip_subnet_id":  computedVIP("The ID of the subnet of the virtual IP."),
			"vip_network_id": computedVIP("The ID of the network of the virtual IP."),
			"admin_state_up": lbReadOnlyAdminStateUp("load balancer"),
			"operating_status": schema.StringAttribute{
				MarkdownDescription: "The operating status of the load balancer (for example `ONLINE` or `OFFLINE`).",
				Computed:            true,
			},
		},
	}
}

func (r *lbLoadBalancerResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureLBClient(req, resp)
}

func (r *lbLoadBalancerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan lbLoadBalancerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting load balancer creation request.", map[string]any{"name": plan.Name.ValueString()})

	ctx, cancel := context.WithTimeout(ctx, lbLoadBalancerTimeout)
	defer cancel()

	lb, err := r.client.CreateLoadBalancer(ctx, service.CreateLoadBalancerOpts{Name: plan.Name.ValueString()})
	if lb != nil {
		// 追加はできたが ACTIVE にならなかったときも、ID を残して次回の apply で作り直せるようにする
		plan.fill(lb)
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	}
	if err != nil {
		addLBError(&resp.Diagnostics, "create", "load balancer", err)
		return
	}

	tflog.Debug(ctx, "Load balancer creation request completed.", map[string]any{"id": lb.ID})
}

func (r *lbLoadBalancerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state lbLoadBalancerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting load balancer read request.", map[string]any{"id": state.ID.ValueString()})

	lb, err := r.client.GetLoadBalancer(ctx, state.ID.ValueString())
	if err != nil {
		if gophercloud.ResponseCodeIs(err, 404) {
			resp.State.RemoveResource(ctx)
			return
		}
		addLBError(&resp.Diagnostics, "read", "load balancer", err)
		return
	}

	tflog.Debug(ctx, "Load balancer read request completed.", map[string]any{"id": lb.ID})

	state.fill(lb)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *lbLoadBalancerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan lbLoadBalancerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting load balancer update request.", map[string]any{
		"id":   plan.ID.ValueString(),
		"name": plan.Name.ValueString(),
	})

	ctx, cancel := context.WithTimeout(ctx, lbChildTimeout)
	defer cancel()

	lb, err := r.client.UpdateLoadBalancer(ctx, plan.ID.ValueString(), service.UpdateLoadBalancerOpts{Name: plan.Name.ValueString()})
	if err != nil {
		addLBError(&resp.Diagnostics, "update", "load balancer", err)
		return
	}

	tflog.Debug(ctx, "Load balancer update request completed.", map[string]any{"id": lb.ID})

	plan.fill(lb)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *lbLoadBalancerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state lbLoadBalancerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting load balancer deletion request.", map[string]any{"id": state.ID.ValueString()})

	ctx, cancel := context.WithTimeout(ctx, lbChildTimeout)
	defer cancel()

	if err := r.client.DeleteLoadBalancer(ctx, state.ID.ValueString()); err != nil {
		addLBError(&resp.Diagnostics, "delete", "load balancer", err)
		return
	}

	tflog.Debug(ctx, "Load balancer deletion request completed.", map[string]any{"id": state.ID.ValueString()})
}

func (r *lbLoadBalancerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// API の応答をモデルに写す.
func (m *lbLoadBalancerResourceModel) fill(lb *service.LoadBalancer) {
	m.ID = types.StringValue(lb.ID)
	m.Name = types.StringValue(lb.Name)
	m.VipAddress = types.StringValue(lb.VipAddress)
	m.VipPortID = types.StringValue(lb.VipPortID)
	m.VipSubnetID = types.StringValue(lb.VipSubnetID)
	m.VipNetworkID = types.StringValue(lb.VipNetworkID)
	m.AdminStateUp = types.BoolValue(lb.AdminStateUp)
	m.OperatingStatus = types.StringValue(lb.OperatingStatus)
}
