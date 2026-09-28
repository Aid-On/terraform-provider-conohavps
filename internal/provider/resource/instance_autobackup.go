// サーバーの自動バックアップのリソースを提供する.
// リソースが存在することは「そのサーバーにアタッチされているボリュームの自動バックアップが有効である」ことを表し、
// 削除すると自動バックアップを無効にする.
// 有効・無効の状態と保存期間を読み戻す API は無いため、Read はサーバーの存在だけを確かめ、それ以外は State を保つ.

package resource

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ resource.Resource                = &instanceAutoBackupResource{}
	_ resource.ResourceWithConfigure   = &instanceAutoBackupResource{}
	_ resource.ResourceWithImportState = &instanceAutoBackupResource{}
)

// 自動バックアップの既定値（ドキュメントの既定値）.
const (
	autoBackupDefaultSchedule  = "daily"
	autoBackupDefaultRetention = 14
)

func NewInstanceAutoBackupResource() resource.Resource {
	return &instanceAutoBackupResource{}
}

type instanceAutoBackupResource struct {
	client *service.ConohaClient
}

type instanceAutoBackupResourceModel struct {
	ID         types.String `tfsdk:"id"`          // サーバー ID と同じ
	InstanceID types.String `tfsdk:"instance_id"` // サーバー ID
	Schedule   types.String `tfsdk:"schedule"`    // バックアップ頻度
	Retention  types.Int64  `tfsdk:"retention"`   // 保存期間（日数）
}

func (r *instanceAutoBackupResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_instance_autobackup"
}

func (r *instanceAutoBackupResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Enables auto-backup of the volumes attached to a server. " +
			"The boot storage volume and, when attached, the additional storage volume are backed up. " +
			"Destroying this resource disables auto-backup of the server.\n\n" +
			"The API cannot read back whether auto-backup is enabled or its retention, so this resource keeps the values it applied " +
			"and only detects that the server itself was deleted. Changes made outside Terraform are not detected. " +
			"The API has no update operation, so changing `schedule` or `retention` disables auto-backup and enables it again.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The ID of the server (same as `instance_id`).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"instance_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the server to enable auto-backup for. Changing this value will force the resource to be recreated.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"schedule": schema.StringAttribute{
				MarkdownDescription: "The backup schedule. Allowed value: `daily`. Defaults to `daily`. Changing this value will force the resource to be recreated.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(autoBackupDefaultSchedule),
				Validators: []validator.String{
					&oneOfStringValidator{values: []string{"daily"}},
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"retention": schema.Int64Attribute{
				MarkdownDescription: "The retention period of backups in days, from 14 to 30. Defaults to 14. Changing this value will force the resource to be recreated.",
				Optional:            true,
				Computed:            true,
				Default:             int64default.StaticInt64(autoBackupDefaultRetention),
				Validators: []validator.Int64{
					int64validator.Between(14, 30),
				},
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
		},
	}
}

func (r *instanceAutoBackupResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	conohaClient, ok := req.ProviderData.(*service.ConohaClient)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected resource configure type",
			fmt.Sprintf("Expected *service.ConohaClient, but got: %T.", req.ProviderData),
		)
		return
	}

	r.client = conohaClient
}

func (r *instanceAutoBackupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan instanceAutoBackupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting auto-backup enabling request.", map[string]any{
		"instance_id": plan.InstanceID.ValueString(),
		"schedule":    plan.Schedule.ValueString(),
		"retention":   plan.Retention.ValueInt64(),
	})

	backup, err := r.client.EnableAutoBackup(ctx, service.EnableAutoBackupOpts{
		InstanceID: plan.InstanceID.ValueString(),
		Schedule:   plan.Schedule.ValueString(),
		Retention:  int(plan.Retention.ValueInt64()),
	})
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to create instance auto-backup resource",
			"An unexpected error occurred while attempting to enable auto-backup. The volumes must be attached to the server.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "Auto-backup enabling request completed.", map[string]any{
		"instance_id": backup.InstanceID,
		"id":          backup.ID,
	})

	plan.ID = plan.InstanceID
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *instanceAutoBackupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state instanceAutoBackupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting instance auto-backup read request.", map[string]any{
		"instance_id": state.InstanceID.ValueString(),
	})

	// 自動バックアップの設定を読む API は無いため、サーバーが消えたことだけを検知する
	if _, err := r.client.GetInstance(ctx, state.InstanceID.ValueString()); err != nil {
		if gophercloud.ResponseCodeIs(err, 404) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to read instance auto-backup resource",
			"An unexpected error occurred while attempting to read the server of the auto-backup.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *instanceAutoBackupResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	// 更新 API は無く、すべての属性が再作成を伴うため、更新は発生しない
	resp.Diagnostics.AddError(
		"An unexpected operation occurred",
		"Updating instance auto-backup resource is not supported. If changes are needed, delete and recreate the resource.",
	)
}

func (r *instanceAutoBackupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state instanceAutoBackupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting auto-backup disabling request.", map[string]any{
		"instance_id": state.InstanceID.ValueString(),
	})

	if err := r.client.DisableAutoBackup(ctx, state.InstanceID.ValueString()); err != nil {
		resp.Diagnostics.AddError(
			"Failed to delete instance auto-backup resource",
			"An unexpected error occurred while attempting to disable auto-backup.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "Auto-backup disabling request completed.", map[string]any{
		"instance_id": state.InstanceID.ValueString(),
	})
}

// `<instance_id>` または `<instance_id>/<retention>` の形式の ID から取り込む.
// 保存期間は API から読み戻せないため、ID で渡されなければ既定値の 14 とする.
func (r *instanceAutoBackupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	instanceID, retentionText, hasRetention := strings.Cut(req.ID, "/")
	retention := int64(autoBackupDefaultRetention)
	if hasRetention {
		v, err := strconv.ParseInt(retentionText, 10, 64)
		if err != nil || v < 14 || v > 30 {
			resp.Diagnostics.AddError(
				"Invalid import ID",
				fmt.Sprintf("Expected an import ID in the form <instance_id> or <instance_id>/<retention> with retention from 14 to 30, got: %q.", req.ID),
			)
			return
		}
		retention = v
	}
	if instanceID == "" {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("Expected an import ID in the form <instance_id> or <instance_id>/<retention>, got: %q.", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), instanceID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("instance_id"), instanceID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("schedule"), autoBackupDefaultSchedule)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("retention"), retention)...)
}
