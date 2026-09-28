// サーバーへの追加ストレージ用のボリュームのアタッチのリソースを提供する.

package resource

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ resource.Resource                = &volumeAttachmentResource{}
	_ resource.ResourceWithConfigure   = &volumeAttachmentResource{}
	_ resource.ResourceWithImportState = &volumeAttachmentResource{}
)

// アタッチ・デタッチの完了（ボリュームの in-use / available）を待つ時間.
const volumeAttachmentTimeout = 10 * time.Minute

func NewVolumeAttachmentResource() resource.Resource {
	return &volumeAttachmentResource{}
}

type volumeAttachmentResource struct {
	client *service.ConohaClient
}

type volumeAttachmentResourceModel struct {
	ID         types.String `tfsdk:"id"`          // <instance_id>/<volume_id>
	InstanceID types.String `tfsdk:"instance_id"` // サーバー ID
	VolumeID   types.String `tfsdk:"volume_id"`   // ボリューム ID
	Device     types.String `tfsdk:"device"`      // デバイス名（/dev/vdb など）
}

func (r *volumeAttachmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_volume_attachment"
}

func (r *volumeAttachmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Attaches an additional storage volume to a server. The volume is not added to the server's `block_device`, which keeps only the volumes given when the server was created. " +
			"The server must be stopped when the volume is attached and detached, and only one additional storage volume can be attached to a server. " +
			"A volume that is still being processed cannot be attached until the processing finishes. " +
			"A boot storage volume cannot be detached.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The ID of the attachment, in the form `<instance_id>/<volume_id>`.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"instance_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the server to attach the volume to. The server must be stopped. Changing this value will force the attachment to be recreated.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"volume_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the additional storage volume to attach. Changing this value will force the attachment to be recreated.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"device": schema.StringAttribute{
				MarkdownDescription: "The device name the volume is attached as, such as `/dev/vdb`.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *volumeAttachmentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *volumeAttachmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan volumeAttachmentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting volume attachment creation request.", map[string]any{
		"instance_id": plan.InstanceID.ValueString(),
		"volume_id":   plan.VolumeID.ValueString(),
	})

	waitCtx, cancel := context.WithTimeout(ctx, volumeAttachmentTimeout)
	defer cancel()

	attachment, err := r.client.AttachVolume(waitCtx, plan.InstanceID.ValueString(), plan.VolumeID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to create volume attachment resource",
			"An unexpected error occurred while attempting to attach the volume. The server must be stopped, and only one additional storage volume can be attached.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	plan.ID = types.StringValue(volumeAttachmentID(plan.InstanceID.ValueString(), plan.VolumeID.ValueString()))
	plan.Device = types.StringValue(attachment.Device)

	tflog.Debug(ctx, "Volume attachment creation request completed.", map[string]any{
		"id":     plan.ID.ValueString(),
		"device": attachment.Device,
	})

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *volumeAttachmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state volumeAttachmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting volume attachment read request.", map[string]any{
		"id": state.ID.ValueString(),
	})

	attachment, err := r.client.GetVolumeAttachment(ctx, state.InstanceID.ValueString(), state.VolumeID.ValueString())
	if err != nil {
		if gophercloud.ResponseCodeIs(err, 404) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to read volume attachment resource",
			"An unexpected error occurred while attempting to read volume attachment resource.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	if attachment.ServerID != "" {
		state.InstanceID = types.StringValue(attachment.ServerID)
	}
	if attachment.VolumeID != "" {
		state.VolumeID = types.StringValue(attachment.VolumeID)
	}
	state.ID = types.StringValue(volumeAttachmentID(state.InstanceID.ValueString(), state.VolumeID.ValueString()))
	state.Device = types.StringValue(attachment.Device)

	tflog.Debug(ctx, "Volume attachment read request completed.", map[string]any{
		"id": state.ID.ValueString(),
	})

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *volumeAttachmentResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	// すべての属性が再作成を伴うため、更新は発生しない
	resp.Diagnostics.AddError(
		"An unexpected operation occurred",
		"Updating volume attachment resource is not supported. If changes are needed, delete and recreate the attachment.",
	)
}

func (r *volumeAttachmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state volumeAttachmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting volume attachment deletion request.", map[string]any{
		"id": state.ID.ValueString(),
	})

	waitCtx, cancel := context.WithTimeout(ctx, volumeAttachmentTimeout)
	defer cancel()

	if err := r.client.DetachVolume(waitCtx, state.InstanceID.ValueString(), state.VolumeID.ValueString()); err != nil {
		resp.Diagnostics.AddError(
			"Failed to delete volume attachment resource",
			"An unexpected error occurred while attempting to detach the volume. The server must be stopped.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "Volume attachment deletion request completed.", map[string]any{
		"id": state.ID.ValueString(),
	})
}

// `<instance_id>/<volume_id>` の形式の ID から取り込む.
func (r *volumeAttachmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	instanceID, volumeID, ok := strings.Cut(req.ID, "/")
	if !ok || instanceID == "" || volumeID == "" || strings.Contains(volumeID, "/") {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("Expected an import ID in the form <instance_id>/<volume_id>, got: %q.", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("instance_id"), instanceID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("volume_id"), volumeID)...)
}

// アタッチの ID を組み立てる.
func volumeAttachmentID(instanceID, volumeID string) string {
	return instanceID + "/" + volumeID
}
