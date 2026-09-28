// ロードバランサーのプールのリソースを提供する.

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
	_ resource.Resource                = &lbPoolResource{}
	_ resource.ResourceWithConfigure   = &lbPoolResource{}
	_ resource.ResourceWithImportState = &lbPoolResource{}
)

func NewLBPoolResource() resource.Resource {
	return &lbPoolResource{}
}

type lbPoolResource struct {
	client *service.ConohaClient
}

// プールのリソースモデル.
type lbPoolResourceModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	LBAlgorithm     types.String `tfsdk:"lb_algorithm"`
	Protocol        types.String `tfsdk:"protocol"`
	ListenerID      types.String `tfsdk:"listener_id"`
	LoadBalancerID  types.String `tfsdk:"loadbalancer_id"`
	AdminStateUp    types.Bool   `tfsdk:"admin_state_up"`
	OperatingStatus types.String `tfsdk:"operating_status"`
}

func (r *lbPoolResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_lb_pool"
}

func (r *lbPoolResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a pool of a load balancer, which sets the balancing algorithm for a listener. " +
			"Changes wait until the pool and its load balancer become `ACTIVE`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The ID of the pool.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the pool. The name length needs to be between 1 and 255. Changing this value updates the name in place.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 255),
				},
			},
			"lb_algorithm": schema.StringAttribute{
				// メンバーが紐づくプールでは API が変更を拒否する. 作り直すとメンバーも作り直しになるため、
				// その場で更新を試み、拒否されたらエラーとして返す
				MarkdownDescription: "The balancing algorithm. Allowed values: `ROUND_ROBIN` (in turn), `LEAST_CONNECTIONS` (by current sessions). " +
					"Changing this value updates the pool in place, but the API rejects the change while the pool has members.",
				Required: true,
				Validators: []validator.String{
					stringvalidator.OneOf("ROUND_ROBIN", "LEAST_CONNECTIONS"),
				},
			},
			"protocol": schema.StringAttribute{
				MarkdownDescription: "The protocol to balance with. Allowed values: `TCP`, `UDP`. Changing this creates a new pool.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("TCP", "UDP"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"listener_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the listener to attach the pool to. Changing this creates a new pool.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"loadbalancer_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the load balancer that the pool belongs to.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"admin_state_up": lbReadOnlyAdminStateUp("pool"),
			"operating_status": schema.StringAttribute{
				MarkdownDescription: "The operating status of the pool.",
				Computed:            true,
			},
		},
	}
}

func (r *lbPoolResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureLBClient(req, resp)
}

func (r *lbPoolResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan lbPoolResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	opts := service.CreatePoolOpts{
		LBAlgorithm: plan.LBAlgorithm.ValueString(),
		Protocol:    plan.Protocol.ValueString(),
		ListenerID:  plan.ListenerID.ValueString(),
		Name:        plan.Name.ValueString(),
	}

	tflog.Debug(ctx, "Starting pool creation request.", map[string]any{
		"name":         opts.Name,
		"lb_algorithm": opts.LBAlgorithm,
		"protocol":     opts.Protocol,
		"listener_id":  opts.ListenerID,
	})

	ctx, cancel := context.WithTimeout(ctx, lbChildTimeout)
	defer cancel()

	pool, err := r.client.CreatePool(ctx, opts)
	if pool != nil {
		plan.fill(pool)
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	}
	if err != nil {
		addLBError(&resp.Diagnostics, "create", "pool", err)
		return
	}

	tflog.Debug(ctx, "Pool creation request completed.", map[string]any{"id": pool.ID})
}

func (r *lbPoolResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state lbPoolResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting pool read request.", map[string]any{"id": state.ID.ValueString()})

	pool, err := r.client.GetPool(ctx, state.ID.ValueString())
	if err != nil {
		if gophercloud.ResponseCodeIs(err, 404) {
			resp.State.RemoveResource(ctx)
			return
		}
		addLBError(&resp.Diagnostics, "read", "pool", err)
		return
	}

	tflog.Debug(ctx, "Pool read request completed.", map[string]any{"id": pool.ID})

	state.fill(pool)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *lbPoolResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state lbPoolResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// 変わった項目だけを送る（メンバーのいるプールで名前だけを変えるときに、バランシング方式を送らないため）
	opts := service.UpdatePoolOpts{}
	if !plan.Name.Equal(state.Name) {
		opts.Name = plan.Name.ValueStringPointer()
	}
	if !plan.LBAlgorithm.Equal(state.LBAlgorithm) {
		opts.LBAlgorithm = plan.LBAlgorithm.ValueStringPointer()
	}

	tflog.Debug(ctx, "Starting pool update request.", map[string]any{
		"id":           plan.ID.ValueString(),
		"name":         plan.Name.ValueString(),
		"lb_algorithm": plan.LBAlgorithm.ValueString(),
	})

	ctx, cancel := context.WithTimeout(ctx, lbChildTimeout)
	defer cancel()

	pool, err := r.client.UpdatePool(ctx, state.ID.ValueString(), state.LoadBalancerID.ValueString(), opts)
	if err != nil {
		addLBError(&resp.Diagnostics, "update", "pool", err)
		return
	}

	tflog.Debug(ctx, "Pool update request completed.", map[string]any{"id": pool.ID})

	plan.fill(pool)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *lbPoolResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state lbPoolResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting pool deletion request.", map[string]any{"id": state.ID.ValueString()})

	ctx, cancel := context.WithTimeout(ctx, lbChildTimeout)
	defer cancel()

	if err := r.client.DeletePool(ctx, state.ID.ValueString(), state.LoadBalancerID.ValueString()); err != nil {
		addLBError(&resp.Diagnostics, "delete", "pool", err)
		return
	}

	tflog.Debug(ctx, "Pool deletion request completed.", map[string]any{"id": state.ID.ValueString()})
}

func (r *lbPoolResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// API の応答をモデルに写す.
func (m *lbPoolResourceModel) fill(p *service.LBPool) {
	m.ID = types.StringValue(p.ID)
	m.Name = types.StringValue(p.Name)
	m.LBAlgorithm = types.StringValue(p.LBAlgorithm)
	m.Protocol = types.StringValue(p.Protocol)
	if id := p.ListenerID(); id != "" {
		m.ListenerID = types.StringValue(id)
	}
	m.LoadBalancerID = types.StringValue(p.LoadBalancerID())
	m.AdminStateUp = types.BoolValue(p.AdminStateUp)
	m.OperatingStatus = types.StringValue(p.OperatingStatus)
}
