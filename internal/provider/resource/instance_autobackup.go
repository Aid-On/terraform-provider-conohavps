// サーバーの自動バックアップのリソースを提供する.
// リソースが存在することは「そのサーバーの自動バックアップを申し込んでいる」ことを表し、
// 削除すると自動バックアップを解約する（取得済みのバックアップは残る）. 保存期間は更新 API でその場で変える.
// 保存期間を読み戻す API は無いため、Read はサーバーのメタデータで申し込みの有無だけを確かめ、保存期間は State を保つ.

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

// 自動バックアップの既定値（API 仕様の既定値）.
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
		MarkdownDescription: "Enables daily auto-backup of the volumes attached to a server. " +
			"The boot storage volume and, when attached, the additional storage volume are backed up. " +
			"Changing `retention` updates it in place. " +
			"Destroying this resource cancels auto-backup of the server (both daily and weekly); backups already taken are not deleted, " +
			"and `conohavps_backups` still lists them.\n\n" +
			"Whether auto-backup is enabled is read from the server's metadata (`backup_status`), so cancelling it outside Terraform " +
			"shows as a change that enables it again. No API returns the retention, so `retention` is kept as Terraform last set it, " +
			"and a retention changed outside Terraform is not detected.",
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
				MarkdownDescription: "The backup schedule. Only `daily` is allowed, and it is the default. " +
					"The API has deprecated this parameter and the provider does not send it; omit it.",
				DeprecationMessage: "The ConoHa API has deprecated the backup schedule and only daily backups can be applied for. " +
					"Remove `schedule` from the configuration; it is always `daily`.",
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString(autoBackupDefaultSchedule),
				Validators: []validator.String{
					&oneOfStringValidator{values: []string{"daily"}},
				},
			},
			"retention": schema.Int64Attribute{
				MarkdownDescription: "The number of days daily backups are kept, from 14 to 30. Defaults to 14. " +
					"Changing this value updates it in place. No API returns this value, so it is not refreshed from ConoHa.",
				Optional: true,
				Computed: true,
				Default:  int64default.StaticInt64(autoBackupDefaultRetention),
				Validators: []validator.Int64{
					int64validator.Between(14, 30),
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
		"retention":   plan.Retention.ValueInt64(),
	})

	backup, err := r.client.EnableAutoBackup(ctx, service.EnableAutoBackupOpts{
		InstanceID: plan.InstanceID.ValueString(),
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

	// 自動バックアップの状態はサーバーのメタデータ（backup_status）で読む.
	// 保存期間を読む項目は無いため、保存期間は state の値を保つ
	server, err := r.client.GetInstance(ctx, state.InstanceID.ValueString())
	if err != nil {
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

	// Terraform の外（コントロールパネルなど）で無効にされていれば、作り直す差分にする
	if !autoBackupEnabled(server.Metadata) {
		tflog.Debug(ctx, "Auto-backup is not enabled on the server; removing it from state.", map[string]any{
			"instance_id": state.InstanceID.ValueString(),
		})
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// サーバーのメタデータから、自動バックアップが有効かを判定する.
// 有効なサーバーには backup_status（"active" など）が載る. このキーと値の一覧は API 仕様に無く、
// サーバー詳細取得のドキュメントの応答例にだけ載るため、空でなければ有効とみなす.
func autoBackupEnabled(metadata map[string]string) bool {
	return metadata["backup_status"] != ""
}

func (r *instanceAutoBackupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state instanceAutoBackupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// 変えられるのは保存期間だけ（instance_id は再作成、schedule は daily のみ）
	if !plan.Retention.Equal(state.Retention) {
		tflog.Debug(ctx, "Starting auto-backup retention update request.", map[string]any{
			"instance_id": plan.InstanceID.ValueString(),
			"retention":   plan.Retention.ValueInt64(),
		})

		retention, err := r.client.UpdateAutoBackupRetention(ctx, plan.InstanceID.ValueString(), int(plan.Retention.ValueInt64()))
		if err != nil {
			resp.Diagnostics.AddError(
				"Failed to update instance auto-backup resource",
				"An unexpected error occurred while attempting to update the retention of auto-backup. Daily auto-backup must be applied for.\n\n"+
					"Error: "+err.Error(),
			)
			return
		}
		// 応答に保存期間が載っていればそれを正とする
		if retention != 0 {
			plan.Retention = types.Int64Value(int64(retention))
		}

		tflog.Debug(ctx, "Auto-backup retention update request completed.", map[string]any{
			"instance_id": plan.InstanceID.ValueString(),
			"retention":   plan.Retention.ValueInt64(),
		})
	}

	plan.ID = plan.InstanceID
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *instanceAutoBackupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state instanceAutoBackupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// 解約しても取得済みのバックアップは削除されない
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
