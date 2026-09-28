// ロードバランサーのプールのメンバーのリソースを提供する.

package resource

import (
	"context"
	"fmt"
	"strings"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ resource.Resource                = &lbMemberResource{}
	_ resource.ResourceWithConfigure   = &lbMemberResource{}
	_ resource.ResourceWithImportState = &lbMemberResource{}
)

func NewLBMemberResource() resource.Resource {
	return &lbMemberResource{}
}

type lbMemberResource struct {
	client *service.ConohaClient
}

// メンバーのリソースモデル.
type lbMemberResourceModel struct {
	ID              types.String `tfsdk:"id"`
	PoolID          types.String `tfsdk:"pool_id"`
	Name            types.String `tfsdk:"name"`
	Address         types.String `tfsdk:"address"`
	ProtocolPort    types.Int64  `tfsdk:"protocol_port"`
	AdminStateUp    types.Bool   `tfsdk:"admin_state_up"`
	Weight          types.Int64  `tfsdk:"weight"`
	OperatingStatus types.String `tfsdk:"operating_status"`
}

func (r *lbMemberResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_lb_member"
}

func (r *lbMemberResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a member of a load balancer pool: an IP address and port that traffic is balanced to. " +
			"Changes wait until the member and its load balancer become `ACTIVE`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The ID of the member.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"pool_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the pool to add the member to. Changing this creates a new member.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			// ドキュメントの更新 API は admin_state_up しか変えられないため、名前も作り直しで変える
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the member. The name length needs to be between 1 and 255. Changing this creates a new member.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 255),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"address": schema.StringAttribute{
				MarkdownDescription: "The IP address to balance traffic to. Only global IP addresses are allowed. Changing this creates a new member.",
				Required:            true,
				Validators: []validator.String{
					lbGlobalIPValidator{},
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"protocol_port": schema.Int64Attribute{
				MarkdownDescription: "The port to balance traffic to (1-65535). Changing this creates a new member.",
				Required:            true,
				Validators: []validator.Int64{
					int64validator.Between(1, 65535),
				},
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"admin_state_up": schema.BoolAttribute{
				MarkdownDescription: "Whether balancing to this member is enabled. Defaults to `true`. Changing this value updates the member in place.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
			},
			"weight": schema.Int64Attribute{
				MarkdownDescription: "The weight of the member.",
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"operating_status": schema.StringAttribute{
				MarkdownDescription: "The operating status of the member.",
				Computed:            true,
			},
		},
	}
}

func (r *lbMemberResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureLBClient(req, resp)
}

func (r *lbMemberResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan lbMemberResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	poolID := plan.PoolID.ValueString()
	opts := service.CreateMemberOpts{
		Name:         plan.Name.ValueString(),
		Address:      plan.Address.ValueString(),
		ProtocolPort: int(plan.ProtocolPort.ValueInt64()),
	}

	tflog.Debug(ctx, "Starting member creation request.", map[string]any{
		"pool_id":       poolID,
		"name":          opts.Name,
		"address":       opts.Address,
		"protocol_port": opts.ProtocolPort,
	})

	ctx, cancel := context.WithTimeout(ctx, lbChildTimeout)
	defer cancel()

	wantUp := plan.AdminStateUp.ValueBool()
	member, err := r.client.CreateMember(ctx, poolID, opts)
	if member != nil {
		plan.fill(member)
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	}
	if err != nil {
		addLBError(&resp.Diagnostics, "create", "member", err)
		return
	}

	// 追加 API は admin_state_up を受け付けないため、無効で作るときは追加の後に更新する
	if !wantUp && member.AdminStateUp {
		tflog.Debug(ctx, "Disabling the new member.", map[string]any{"id": member.ID})
		member, err = r.client.UpdateMember(ctx, poolID, member.ID, service.UpdateMemberOpts{AdminStateUp: false})
		if err != nil {
			addLBError(&resp.Diagnostics, "create", "member", err)
			return
		}
		plan.fill(member)
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	}

	tflog.Debug(ctx, "Member creation request completed.", map[string]any{"id": member.ID})
}

func (r *lbMemberResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state lbMemberResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting member read request.", map[string]any{
		"pool_id": state.PoolID.ValueString(),
		"id":      state.ID.ValueString(),
	})

	member, err := r.client.GetMember(ctx, state.PoolID.ValueString(), state.ID.ValueString())
	if err != nil {
		if gophercloud.ResponseCodeIs(err, 404) {
			resp.State.RemoveResource(ctx)
			return
		}
		addLBError(&resp.Diagnostics, "read", "member", err)
		return
	}

	tflog.Debug(ctx, "Member read request completed.", map[string]any{"id": member.ID})

	state.fill(member)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *lbMemberResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan lbMemberResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting member update request.", map[string]any{
		"id":             plan.ID.ValueString(),
		"admin_state_up": plan.AdminStateUp.ValueBool(),
	})

	ctx, cancel := context.WithTimeout(ctx, lbChildTimeout)
	defer cancel()

	member, err := r.client.UpdateMember(ctx, plan.PoolID.ValueString(), plan.ID.ValueString(),
		service.UpdateMemberOpts{AdminStateUp: plan.AdminStateUp.ValueBool()})
	if err != nil {
		addLBError(&resp.Diagnostics, "update", "member", err)
		return
	}

	tflog.Debug(ctx, "Member update request completed.", map[string]any{"id": member.ID})

	plan.fill(member)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *lbMemberResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state lbMemberResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting member deletion request.", map[string]any{
		"pool_id": state.PoolID.ValueString(),
		"id":      state.ID.ValueString(),
	})

	ctx, cancel := context.WithTimeout(ctx, lbChildTimeout)
	defer cancel()

	if err := r.client.DeleteMember(ctx, state.PoolID.ValueString(), state.ID.ValueString()); err != nil {
		addLBError(&resp.Diagnostics, "delete", "member", err)
		return
	}

	tflog.Debug(ctx, "Member deletion request completed.", map[string]any{"id": state.ID.ValueString()})
}

// メンバーの API はプールの下にあり、応答にもプールの ID が無いため、インポートの ID は "<pool_id>/<member_id>" とする.
func (r *lbMemberResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	poolID, memberID, ok := strings.Cut(req.ID, "/")
	if !ok || poolID == "" || memberID == "" || strings.Contains(memberID, "/") {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("Expected an import ID of the form <pool_id>/<member_id>, got: %q.", req.ID),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("pool_id"), poolID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), memberID)...)
}

// API の応答をモデルに写す. プールの ID は応答に含まれないため変えない.
func (m *lbMemberResourceModel) fill(member *service.LBMember) {
	m.ID = types.StringValue(member.ID)
	m.Name = types.StringValue(member.Name)
	m.Address = types.StringValue(member.Address)
	m.ProtocolPort = types.Int64Value(int64(member.ProtocolPort))
	m.AdminStateUp = types.BoolValue(member.AdminStateUp)
	m.Weight = types.Int64Value(int64(member.Weight))
	m.OperatingStatus = types.StringValue(member.OperatingStatus)
}
