// ロードバランサーのリスナーのリソースを提供する.

package resource

import (
	"context"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
	"github.com/gophercloud/gophercloud/v2"
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
)

var (
	_ resource.Resource                = &lbListenerResource{}
	_ resource.ResourceWithConfigure   = &lbListenerResource{}
	_ resource.ResourceWithImportState = &lbListenerResource{}
)

func NewLBListenerResource() resource.Resource {
	return &lbListenerResource{}
}

type lbListenerResource struct {
	client *service.ConohaClient
}

// リスナーのリソースモデル.
type lbListenerResourceModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	Protocol        types.String `tfsdk:"protocol"`
	ProtocolPort    types.Int64  `tfsdk:"protocol_port"`
	LoadBalancerID  types.String `tfsdk:"loadbalancer_id"`
	OperatingStatus types.String `tfsdk:"operating_status"`
}

func (r *lbListenerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_lb_listener"
}

func (r *lbListenerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a listener of a load balancer, which sets the protocol and port that the load balancer accepts connections on. " +
			"Changes wait until the listener and its load balancer become `ACTIVE`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The ID of the listener.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the listener. The name length needs to be between 1 and 255. Changing this value updates the name in place.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 255),
				},
			},
			// ドキュメントの更新 API はリスナー名しか変えられないため、それ以外は作り直す
			"protocol": schema.StringAttribute{
				MarkdownDescription: "The protocol to accept connections with. Allowed values: `TCP`, `UDP`. Changing this creates a new listener.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("TCP", "UDP"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"protocol_port": schema.Int64Attribute{
				MarkdownDescription: "The port number to accept connections on (1-65535). Changing this creates a new listener.",
				Required:            true,
				Validators: []validator.Int64{
					int64validator.Between(1, 65535),
				},
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"loadbalancer_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the load balancer to attach the listener to. Changing this creates a new listener.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"operating_status": schema.StringAttribute{
				MarkdownDescription: "The operating status of the listener.",
				Computed:            true,
			},
		},
	}
}

func (r *lbListenerResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureLBClient(req, resp)
}

func (r *lbListenerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan lbListenerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	opts := service.CreateListenerOpts{
		Protocol:       plan.Protocol.ValueString(),
		ProtocolPort:   int(plan.ProtocolPort.ValueInt64()),
		LoadBalancerID: plan.LoadBalancerID.ValueString(),
		Name:           plan.Name.ValueString(),
	}

	tflog.Debug(ctx, "Starting listener creation request.", map[string]any{
		"name":            opts.Name,
		"protocol":        opts.Protocol,
		"protocol_port":   opts.ProtocolPort,
		"loadbalancer_id": opts.LoadBalancerID,
	})

	ctx, cancel := context.WithTimeout(ctx, lbChildTimeout)
	defer cancel()

	listener, err := r.client.CreateListener(ctx, opts)
	if listener != nil {
		plan.fill(listener)
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	}
	if err != nil {
		addLBError(&resp.Diagnostics, "create", "listener", err)
		return
	}

	tflog.Debug(ctx, "Listener creation request completed.", map[string]any{"id": listener.ID})
}

func (r *lbListenerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state lbListenerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting listener read request.", map[string]any{"id": state.ID.ValueString()})

	listener, err := r.client.GetListener(ctx, state.ID.ValueString())
	if err != nil {
		if gophercloud.ResponseCodeIs(err, 404) {
			resp.State.RemoveResource(ctx)
			return
		}
		addLBError(&resp.Diagnostics, "read", "listener", err)
		return
	}

	tflog.Debug(ctx, "Listener read request completed.", map[string]any{"id": listener.ID})

	state.fill(listener)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *lbListenerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan lbListenerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting listener update request.", map[string]any{
		"id":   plan.ID.ValueString(),
		"name": plan.Name.ValueString(),
	})

	ctx, cancel := context.WithTimeout(ctx, lbChildTimeout)
	defer cancel()

	listener, err := r.client.UpdateListener(ctx, plan.ID.ValueString(), plan.LoadBalancerID.ValueString(),
		service.UpdateListenerOpts{Name: plan.Name.ValueString()})
	if err != nil {
		addLBError(&resp.Diagnostics, "update", "listener", err)
		return
	}

	tflog.Debug(ctx, "Listener update request completed.", map[string]any{"id": listener.ID})

	plan.fill(listener)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *lbListenerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state lbListenerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting listener deletion request.", map[string]any{"id": state.ID.ValueString()})

	ctx, cancel := context.WithTimeout(ctx, lbChildTimeout)
	defer cancel()

	if err := r.client.DeleteListener(ctx, state.ID.ValueString(), state.LoadBalancerID.ValueString()); err != nil {
		addLBError(&resp.Diagnostics, "delete", "listener", err)
		return
	}

	tflog.Debug(ctx, "Listener deletion request completed.", map[string]any{"id": state.ID.ValueString()})
}

func (r *lbListenerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// API の応答をモデルに写す.
func (m *lbListenerResourceModel) fill(l *service.LBListener) {
	m.ID = types.StringValue(l.ID)
	m.Name = types.StringValue(l.Name)
	m.Protocol = types.StringValue(l.Protocol)
	m.ProtocolPort = types.Int64Value(int64(l.ProtocolPort))
	if id := l.LoadBalancerID(); id != "" {
		m.LoadBalancerID = types.StringValue(id)
	}
	m.OperatingStatus = types.StringValue(l.OperatingStatus)
}
