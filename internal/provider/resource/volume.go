// ボリュームのリソースを提供する.

package resource

import (
	"context"
	"fmt"
	"time"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
	"github.com/gophercloud/gophercloud/v2"
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
	_ resource.Resource                = &volumeResource{}
	_ resource.ResourceWithConfigure   = &volumeResource{}
	_ resource.ResourceWithImportState = &volumeResource{}
)

func NewVolumeResource() resource.Resource {
	return &volumeResource{}
}

type volumeResource struct {
	client *service.BlockStorageService
}

type volumeResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Size        types.Int64  `tfsdk:"size"`
	Description types.String `tfsdk:"description"`
	Name        types.String `tfsdk:"name"`
	VolumeType  types.String `tfsdk:"volume_type"`
	ImageRef    types.String `tfsdk:"image_ref"`
	SourceVolID types.String `tfsdk:"source_volid"`
	BackupID    types.String `tfsdk:"backup_id"`
	Status      types.String `tfsdk:"status"`
	Bootable    types.String `tfsdk:"bootable"`
}

func (r *volumeResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_volume"
}

func (r *volumeResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages volume resource.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Volume ID.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"size": schema.Int64Attribute{
				MarkdownDescription: "Volume size in GB. Allowed values: 30, 100, 200, 500, 1000, 5000, 10000.",
				Required:            true,
				Validators: []validator.Int64{
					&oneOfInt64Validator{values: []int64{30, 100, 200, 500, 1000, 5000, 10000}},
				},
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Volume description.",
				Optional:            true,
				Computed:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Volume name.",
				Required:            true,
			},
			"volume_type": schema.StringAttribute{
				MarkdownDescription: "Volume type. Allowed values: c3j1-ds02-boot, c3j1-ds02-add.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					&oneOfStringValidator{values: []string{"c3j1-ds02-boot", "c3j1-ds02-add"}},
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"image_ref": schema.StringAttribute{
				MarkdownDescription: "Image ID to create volume from.",
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.ConflictsWith(
						path.MatchRoot("source_volid"), path.MatchRoot("backup_id"),
					),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"source_volid": schema.StringAttribute{
				MarkdownDescription: "Source Volume ID to clone from.",
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.ConflictsWith(
						path.MatchRoot("image_ref"), path.MatchRoot("backup_id"),
					),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"backup_id": schema.StringAttribute{
				MarkdownDescription: "Backup ID to restore from.",
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.ConflictsWith(
						path.MatchRoot("image_ref"), path.MatchRoot("source_volid"),
					),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Volume status.",
				Computed:            true,
			},
			"bootable": schema.StringAttribute{
				MarkdownDescription: "Whether the volume is bootable.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *volumeResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

	if client == nil || client.BlockStorageClient == nil {
		return
	}

	r.client = service.NewBlockStorageService(client.BlockStorageClient)
}

func (r *volumeResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan volumeResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting volume creation request.", map[string]any{
		"name":         plan.Name.ValueString(),
		"size":         plan.Size.ValueInt64(),
		"description":  plan.Description.ValueString(),
		"volume_type":  plan.VolumeType.ValueString(),
		"image_ref":    plan.ImageRef.ValueString(),
		"source_volid": plan.SourceVolID.ValueString(),
		"backup_id":    plan.BackupID.ValueString(),
	})

	// 方針に基づき、以下のクライアント側バリデーションを削除し、APIのレスポンスに任せる：
	// 1. image_ref, source_volid, backup_id の排他チェック
	// 2. volume_type と size の組み合わせチェック
	// 3. source_volid 指定時のソースボリューム存在確認とサイズ一致チェック

	opts := service.CreateVolumeOpts{
		Size:        int(plan.Size.ValueInt64()),
		Description: plan.Description.ValueString(),
		Name:        plan.Name.ValueString(),
		VolumeType:  plan.VolumeType.ValueString(),
		ImageID:     plan.ImageRef.ValueString(),
		SourceVolID: plan.SourceVolID.ValueString(),
		BackupID:    plan.BackupID.ValueString(),
	}

	volume, err := r.client.CreateVolume(ctx, opts)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to create volume resource",
			"An unexpected error occurred while attempting to create volume resource.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "Volume creation request completed.", map[string]any{
		"id": volume.ID,
	})

	plan.ID = types.StringValue(volume.ID)
	plan.Description = types.StringValue(volume.Description)
	plan.Status = types.StringValue(volume.Status)
	plan.Bootable = types.StringValue(volume.Bootable)
	plan.VolumeType = types.StringValue(volume.VolumeType)

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *volumeResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state volumeResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting volume read request.", map[string]any{
		"id": state.ID.ValueString(),
	})

	volume, err := r.client.GetVolume(ctx, state.ID.ValueString())
	if err != nil {
		if gophercloud.ResponseCodeIs(err, 404) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to read volume resource",
			"An unexpected error occurred while attempting to read volume resource.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "Volume read request completed.", map[string]any{
		"id": state.ID.ValueString(),
	})

	state.ID = types.StringValue(volume.ID)
	state.Name = types.StringValue(volume.Name)
	state.Size = types.Int64Value(int64(volume.Size))
	state.Description = types.StringValue(volume.Description)
	state.Status = types.StringValue(volume.Status)
	state.Bootable = types.StringValue(volume.Bootable)
	state.VolumeType = types.StringValue(volume.VolumeType)

	// source_volid, image_ref, backup_id は Create 時のみ指定され、通常の GET レスポンスでは異なるフィールドに格納されている
	// Import 時に正しく State を復元するため、API レスポンスから対応するフィールドを読み取って設定する
	//
	// 値が空の場合は State を更新しない（null のまま維持）
	// これにより、空のボリューム作成時に不要な属性が設定されることを防ぐ
	if volume.SourceVolID != "" {
		state.SourceVolID = types.StringValue(volume.SourceVolID)
	}

	if imageID, ok := volume.VolumeImageMetadata["image_id"]; ok && imageID != "" {
		state.ImageRef = types.StringValue(imageID)
	}

	if backupID, ok := volume.Metadata["src_backup_id"]; ok && backupID != "" {
		state.BackupID = types.StringValue(backupID)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *volumeResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan volumeResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting volume update request.", map[string]any{
		"id":          plan.ID.ValueString(),
		"name":        plan.Name.ValueString(),
		"description": plan.Description.ValueString(),
	})

	opts := service.UpdateVolumeOpts{
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueString(),
	}

	volume, err := r.client.UpdateVolume(ctx, plan.ID.ValueString(), opts)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to update volume resource",
			"An unexpected error occurred while attempting to update volume resource.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "Volume update request completed.", map[string]any{
		"id": plan.ID.ValueString(),
	})

	plan.Name = types.StringValue(volume.Name)
	plan.Description = types.StringValue(volume.Description)
	plan.Status = types.StringValue(volume.Status)

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *volumeResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state volumeResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting volume deletion request.", map[string]any{
		"id": state.ID.ValueString(),
	})

	// ボリュームがavailableになるまで待機
	waitCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	if err := r.client.WaitForVolumeStatus(waitCtx, state.ID.ValueString(), "available"); err != nil {
		resp.Diagnostics.AddError(
			"Wait timeout exceeded",
			"An unexpected error occurred while waiting for volume resource to become available.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	err := r.client.DeleteVolume(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to delete volume",
			"An unexpected error occurred while attempting to delete volume resource.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "Volume deletion request completed.", map[string]any{
		"id": state.ID.ValueString(),
	})
}

func (r *volumeResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// バリデーター.

type oneOfInt64Validator struct {
	values []int64
}

func (v oneOfInt64Validator) Description(ctx context.Context) string {
	return fmt.Sprintf("value must be one of: %v", v.values)
}

func (v oneOfInt64Validator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v oneOfInt64Validator) ValidateInt64(ctx context.Context, req validator.Int64Request, resp *validator.Int64Response) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	val := req.ConfigValue.ValueInt64()
	for _, allowed := range v.values {
		if val == allowed {
			return
		}
	}
	resp.Diagnostics.AddAttributeError(
		req.Path,
		"Invalid attribute value",
		fmt.Sprintf("Attribute must be one of %v, got: %d.", v.values, val),
	)
}

type oneOfStringValidator struct {
	values []string
}

func (v oneOfStringValidator) Description(ctx context.Context) string {
	return fmt.Sprintf("value must be one of: %v", v.values)
}

func (v oneOfStringValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v oneOfStringValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	val := req.ConfigValue.ValueString()
	for _, allowed := range v.values {
		if val == allowed {
			return
		}
	}
	resp.Diagnostics.AddAttributeError(
		req.Path,
		"Invalid attribute value",
		fmt.Sprintf("Attribute must be one of %v, got: %s.", v.values, val),
	)
}
