// ボリュームのスナップショットのリソースを提供する.

package resource

import (
	"context"
	"fmt"
	"time"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/snapshots"
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
	_ resource.Resource                = &volumeSnapshotResource{}
	_ resource.ResourceWithConfigure   = &volumeSnapshotResource{}
	_ resource.ResourceWithImportState = &volumeSnapshotResource{}
)

// スナップショットの作成・削除の完了を待つ時間.
const volumeSnapshotTimeout = 30 * time.Minute

func NewVolumeSnapshotResource() resource.Resource {
	return &volumeSnapshotResource{}
}

type volumeSnapshotResource struct {
	client *service.ConohaClient
}

type volumeSnapshotResourceModel struct {
	ID          types.String `tfsdk:"id"`          // スナップショット ID
	VolumeID    types.String `tfsdk:"volume_id"`   // ボリューム ID
	Name        types.String `tfsdk:"name"`        // スナップショット名
	Description types.String `tfsdk:"description"` // 説明
	Status      types.String `tfsdk:"status"`      // ステータス
	Size        types.Int64  `tfsdk:"size"`        // サイズ（GB）
	CreatedAt   types.String `tfsdk:"created_at"`  // 作成日時
}

func (r *volumeSnapshotResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_volume_snapshot"
}

func (r *volumeSnapshotResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a snapshot of a volume. " +
			"Only one snapshot can be created, and ConoHa deletes a snapshot automatically 24 hours after it is created; " +
			"after that the snapshot is removed from the state and the next apply creates it again. " +
			"The API has no update operation, so every change recreates the snapshot.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Snapshot ID.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"volume_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the volume to snapshot. Changing this value will force the snapshot to be recreated.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Snapshot name. Changing this value will force the snapshot to be recreated.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Snapshot description. Changing this value will force the snapshot to be recreated.",
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Snapshot status.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"size": schema.Int64Attribute{
				MarkdownDescription: "Snapshot size in GB.",
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "The date and time the snapshot was created, in RFC 3339 format.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *volumeSnapshotResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *volumeSnapshotResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan volumeSnapshotResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting volume snapshot creation request.", map[string]any{
		"volume_id": plan.VolumeID.ValueString(),
		"name":      plan.Name.ValueString(),
	})

	waitCtx, cancel := context.WithTimeout(ctx, volumeSnapshotTimeout)
	defer cancel()

	snapshot, err := r.client.CreateSnapshot(waitCtx, service.CreateSnapshotOpts{
		VolumeID:    plan.VolumeID.ValueString(),
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to create volume snapshot resource",
			"An unexpected error occurred while attempting to create volume snapshot resource. Only one snapshot can be created.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "Volume snapshot creation request completed.", map[string]any{
		"id": snapshot.ID,
	})

	setVolumeSnapshotState(&plan, snapshot)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *volumeSnapshotResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state volumeSnapshotResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting volume snapshot read request.", map[string]any{
		"id": state.ID.ValueString(),
	})

	snapshot, err := r.client.GetSnapshot(ctx, state.ID.ValueString())
	if err != nil {
		// 24時間経過による自動削除を含め、消えていれば State から外す
		if gophercloud.ResponseCodeIs(err, 404) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to read volume snapshot resource",
			"An unexpected error occurred while attempting to read volume snapshot resource.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "Volume snapshot read request completed.", map[string]any{
		"id": state.ID.ValueString(),
	})

	setVolumeSnapshotState(&state, snapshot)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *volumeSnapshotResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	// スナップショットの更新 API は無く、すべての属性が再作成を伴うため、更新は発生しない
	resp.Diagnostics.AddError(
		"An unexpected operation occurred",
		"Updating volume snapshot resource is not supported. If changes are needed, delete and recreate the snapshot.",
	)
}

func (r *volumeSnapshotResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state volumeSnapshotResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting volume snapshot deletion request.", map[string]any{
		"id": state.ID.ValueString(),
	})

	waitCtx, cancel := context.WithTimeout(ctx, volumeSnapshotTimeout)
	defer cancel()

	if err := r.client.DeleteSnapshot(waitCtx, state.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError(
			"Failed to delete volume snapshot resource",
			"An unexpected error occurred while attempting to delete volume snapshot resource.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "Volume snapshot deletion request completed.", map[string]any{
		"id": state.ID.ValueString(),
	})
}

func (r *volumeSnapshotResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// API のスナップショットを State に写す.
// 説明は省略可能なので、空であれば null のままにする.
func setVolumeSnapshotState(m *volumeSnapshotResourceModel, s *snapshots.Snapshot) {
	m.ID = types.StringValue(s.ID)
	m.VolumeID = types.StringValue(s.VolumeID)
	m.Name = types.StringValue(s.Name)
	if s.Description != "" {
		m.Description = types.StringValue(s.Description)
	} else {
		m.Description = types.StringNull()
	}
	m.Status = types.StringValue(s.Status)
	m.Size = types.Int64Value(int64(s.Size))
	m.CreatedAt = types.StringValue(s.CreatedAt.UTC().Format(time.RFC3339))
}
