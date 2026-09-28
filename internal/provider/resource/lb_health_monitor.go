// ロードバランサーのヘルスモニタのリソースを提供する.

package resource

import (
	"context"
	"fmt"

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
	_ resource.Resource                   = &lbHealthMonitorResource{}
	_ resource.ResourceWithConfigure      = &lbHealthMonitorResource{}
	_ resource.ResourceWithImportState    = &lbHealthMonitorResource{}
	_ resource.ResourceWithValidateConfig = &lbHealthMonitorResource{}
	_ resource.ResourceWithModifyPlan     = &lbHealthMonitorResource{}
)

func NewLBHealthMonitorResource() resource.Resource {
	return &lbHealthMonitorResource{}
}

type lbHealthMonitorResource struct {
	client *service.ConohaClient
}

// ヘルスモニタのリソースモデル.
type lbHealthMonitorResourceModel struct {
	ID              types.String `tfsdk:"id"`
	PoolID          types.String `tfsdk:"pool_id"`
	Name            types.String `tfsdk:"name"`
	Type            types.String `tfsdk:"type"`
	Delay           types.Int64  `tfsdk:"delay"`
	Timeout         types.Int64  `tfsdk:"timeout"`
	MaxRetries      types.Int64  `tfsdk:"max_retries"`
	URLPath         types.String `tfsdk:"url_path"`
	ExpectedCodes   types.String `tfsdk:"expected_codes"`
	AdminStateUp    types.Bool   `tfsdk:"admin_state_up"`
	OperatingStatus types.String `tfsdk:"operating_status"`
}

func (r *lbHealthMonitorResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_lb_health_monitor"
}

func (r *lbHealthMonitorResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	// 仕様の更新 API（UpdateHealthmonitorReq）はヘルスモニタ名しか変えられないため、それ以外は作り直す
	replaceString := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	replaceInt64 := []planmodifier.Int64{int64planmodifier.RequiresReplace()}
	// 省くと HTTP・HTTPS では API の既定値が入るため、設定が無ければ state の値を引き継ぐ.
	// 作り直しや HTTP・HTTPS 以外への変更での扱いは ModifyPlan で決める
	httpOnly := []planmodifier.String{stringplanmodifier.UseStateForUnknown(), stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a health monitor of a load balancer pool, which checks whether the members are alive. " +
			"Changes wait until the health monitor and its load balancer become `ACTIVE`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The ID of the health monitor.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"pool_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the pool to monitor. Changing this creates a new health monitor.",
				Required:            true,
				PlanModifiers:       replaceString,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the health monitor. The name length needs to be between 1 and 255. Changing this value updates the name in place.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 255),
				},
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "The protocol used for the health check. Allowed values: `TCP`, `UDP-CONNECT`, `PING`, `HTTP`, `HTTPS`. " +
					"`HTTP` and `HTTPS` put load on the members, so `TCP` or `PING` is recommended for few or small members. " +
					"Changing this creates a new health monitor.",
				Required: true,
				Validators: []validator.String{
					stringvalidator.OneOf("TCP", "UDP-CONNECT", "PING", "HTTP", "HTTPS"),
				},
				PlanModifiers: replaceString,
			},
			"delay": schema.Int64Attribute{
				MarkdownDescription: "The interval between checks, in seconds (1-180). Changing this creates a new health monitor.",
				Required:            true,
				Validators: []validator.Int64{
					int64validator.Between(1, 180),
				},
				PlanModifiers: replaceInt64,
			},
			"timeout": schema.Int64Attribute{
				MarkdownDescription: "How long to wait for a check response, in seconds (1-180). Must be less than `delay`. Changing this creates a new health monitor.",
				Required:            true,
				Validators: []validator.Int64{
					int64validator.Between(1, 180),
				},
				PlanModifiers: replaceInt64,
			},
			"max_retries": schema.Int64Attribute{
				MarkdownDescription: "The number of retries after a failed check. Changing this creates a new health monitor.",
				Required:            true,
				PlanModifiers:       replaceInt64,
			},
			"url_path": schema.StringAttribute{
				MarkdownDescription: "The path to request. Only allowed when `type` is `HTTP` or `HTTPS`; when omitted, the API's default is used. " +
					"`null` for other types. Changing this creates a new health monitor.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: httpOnly,
			},
			"expected_codes": schema.StringAttribute{
				MarkdownDescription: "The HTTP status code(s) expected in the response. Only allowed when `type` is `HTTP` or `HTTPS`; when omitted, the API's default is used. " +
					"`null` for other types. Changing this creates a new health monitor.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: httpOnly,
			},
			"admin_state_up": lbReadOnlyAdminStateUp("health monitor"),
			"operating_status": schema.StringAttribute{
				MarkdownDescription: "The operating status of the health monitor.",
				Computed:            true,
			},
		},
	}
}

// 項目をまたぐ制約（timeout は delay より短い、url_path と expected_codes は HTTP・HTTPS のときだけ指定できる）を確かめる.
// 仕様の作成の要求で url_path と expected_codes は必須ではないため、HTTP・HTTPS でも省略を認める.
func (r *lbHealthMonitorResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg lbHealthMonitorResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if lbKnown(cfg.Delay) && lbKnown(cfg.Timeout) && cfg.Timeout.ValueInt64() >= cfg.Delay.ValueInt64() {
		resp.Diagnostics.AddAttributeError(
			path.Root("timeout"),
			"Invalid attribute value",
			fmt.Sprintf("timeout (%d) must be less than delay (%d).", cfg.Timeout.ValueInt64(), cfg.Delay.ValueInt64()),
		)
	}

	if !lbKnown(cfg.Type) {
		return
	}
	typ := cfg.Type.ValueString()
	if lbIsHTTPMonitor(typ) {
		return
	}
	for name, v := range map[string]types.String{"url_path": cfg.URLPath, "expected_codes": cfg.ExpectedCodes} {
		if lbKnown(v) {
			resp.Diagnostics.AddAttributeError(path.Root(name), "Invalid attribute combination",
				fmt.Sprintf("%s can only be set when type is HTTP or HTTPS, got type %s.", name, typ))
		}
	}
}

// url_path と expected_codes の plan を決める.
// HTTP・HTTPS 以外では API が null を返すので null にする. 作り直すときは、設定の無い項目に
// state の値ではなく新しいヘルスモニタに API が入れる既定値が入るので、未確定にする.
func (r *lbHealthMonitorResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	var plan, config lbHealthMonitorResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() || !lbKnown(plan.Type) {
		return
	}

	urlPath, expectedCodes := plan.URLPath, plan.ExpectedCodes
	switch {
	case !lbIsHTTPMonitor(plan.Type.ValueString()):
		urlPath, expectedCodes = types.StringNull(), types.StringNull()
	case !req.State.Raw.IsNull():
		var state lbHealthMonitorResourceModel
		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
		if resp.Diagnostics.HasError() || !plan.replaces(state) {
			return
		}
		if config.URLPath.IsNull() {
			urlPath = types.StringUnknown()
		}
		if config.ExpectedCodes.IsNull() {
			expectedCodes = types.StringUnknown()
		}
	default:
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("url_path"), urlPath)...)
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("expected_codes"), expectedCodes)...)
}

// plan が state のヘルスモニタの作り直しになるか. 名前以外の項目はどれも作り直しになる.
func (m lbHealthMonitorResourceModel) replaces(state lbHealthMonitorResourceModel) bool {
	return !m.PoolID.Equal(state.PoolID) || !m.Type.Equal(state.Type) || !m.Delay.Equal(state.Delay) ||
		!m.Timeout.Equal(state.Timeout) || !m.MaxRetries.Equal(state.MaxRetries) ||
		!m.URLPath.Equal(state.URLPath) || !m.ExpectedCodes.Equal(state.ExpectedCodes)
}

// url_path と expected_codes を使うヘルスモニタの type か.
func lbIsHTTPMonitor(typ string) bool {
	return typ == "HTTP" || typ == "HTTPS"
}

func (r *lbHealthMonitorResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureLBClient(req, resp)
}

func (r *lbHealthMonitorResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan lbHealthMonitorResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	opts := service.CreateHealthMonitorOpts{
		Name:          plan.Name.ValueString(),
		PoolID:        plan.PoolID.ValueString(),
		Delay:         int(plan.Delay.ValueInt64()),
		MaxRetries:    int(plan.MaxRetries.ValueInt64()),
		Timeout:       int(plan.Timeout.ValueInt64()),
		Type:          plan.Type.ValueString(),
		URLPath:       plan.URLPath.ValueString(), // 省略（未確定）なら空文字列になり、送らない
		ExpectedCodes: plan.ExpectedCodes.ValueString(),
	}

	tflog.Debug(ctx, "Starting health monitor creation request.", map[string]any{
		"name":           opts.Name,
		"pool_id":        opts.PoolID,
		"type":           opts.Type,
		"delay":          opts.Delay,
		"timeout":        opts.Timeout,
		"max_retries":    opts.MaxRetries,
		"url_path":       opts.URLPath,
		"expected_codes": opts.ExpectedCodes,
	})

	ctx, cancel := context.WithTimeout(ctx, lbChildTimeout)
	defer cancel()

	monitor, err := r.client.CreateHealthMonitor(ctx, opts)
	if monitor != nil {
		plan.fill(monitor)
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	}
	if err != nil {
		addLBError(&resp.Diagnostics, "create", "health monitor", err)
		return
	}

	tflog.Debug(ctx, "Health monitor creation request completed.", map[string]any{"id": monitor.ID})
}

func (r *lbHealthMonitorResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state lbHealthMonitorResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting health monitor read request.", map[string]any{"id": state.ID.ValueString()})

	monitor, err := r.client.GetHealthMonitor(ctx, state.ID.ValueString())
	if err != nil {
		if gophercloud.ResponseCodeIs(err, 404) {
			resp.State.RemoveResource(ctx)
			return
		}
		addLBError(&resp.Diagnostics, "read", "health monitor", err)
		return
	}

	tflog.Debug(ctx, "Health monitor read request completed.", map[string]any{"id": monitor.ID})

	state.fill(monitor)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *lbHealthMonitorResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan lbHealthMonitorResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting health monitor update request.", map[string]any{
		"id":   plan.ID.ValueString(),
		"name": plan.Name.ValueString(),
	})

	ctx, cancel := context.WithTimeout(ctx, lbChildTimeout)
	defer cancel()

	monitor, err := r.client.UpdateHealthMonitor(ctx, plan.ID.ValueString(), plan.PoolID.ValueString(),
		service.UpdateHealthMonitorOpts{Name: plan.Name.ValueString()})
	if err != nil {
		addLBError(&resp.Diagnostics, "update", "health monitor", err)
		return
	}

	tflog.Debug(ctx, "Health monitor update request completed.", map[string]any{"id": monitor.ID})

	plan.fill(monitor)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *lbHealthMonitorResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state lbHealthMonitorResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting health monitor deletion request.", map[string]any{"id": state.ID.ValueString()})

	ctx, cancel := context.WithTimeout(ctx, lbChildTimeout)
	defer cancel()

	if err := r.client.DeleteHealthMonitor(ctx, state.ID.ValueString(), state.PoolID.ValueString()); err != nil {
		addLBError(&resp.Diagnostics, "delete", "health monitor", err)
		return
	}

	tflog.Debug(ctx, "Health monitor deletion request completed.", map[string]any{"id": state.ID.ValueString()})
}

func (r *lbHealthMonitorResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// API の応答をモデルに写す. HTTP・HTTPS 以外では url_path と expected_codes が null で返るので null のままにする.
func (m *lbHealthMonitorResourceModel) fill(hm *service.LBHealthMonitor) {
	m.ID = types.StringValue(hm.ID)
	if id := hm.PoolID(); id != "" {
		m.PoolID = types.StringValue(id)
	}
	m.Name = types.StringValue(hm.Name)
	m.Type = types.StringValue(hm.Type)
	m.Delay = types.Int64Value(int64(hm.Delay))
	m.Timeout = types.Int64Value(int64(hm.Timeout))
	m.MaxRetries = types.Int64Value(int64(hm.MaxRetries))
	m.URLPath = lbNullableString(hm.URLPath)
	m.ExpectedCodes = lbNullableString(hm.ExpectedCodes)
	m.AdminStateUp = types.BoolValue(hm.AdminStateUp)
	m.OperatingStatus = types.StringValue(hm.OperatingStatus)
}

// 値が確定しているか（null でも unknown でもないか）.
func lbKnown(v interface {
	IsNull() bool
	IsUnknown() bool
}) bool {
	return !v.IsNull() && !v.IsUnknown()
}
