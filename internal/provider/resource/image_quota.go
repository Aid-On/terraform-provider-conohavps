// イメージ保存容量（アカウントに1つ）のリソースを提供する.
// 既定の 50GB は無料で、それを超える分が 500GB 単位で課金されるため、刻みを検証し、
// 使用量を下回る変更は API のエラーをそのまま返す.

package resource

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &imageQuotaResource{}
var _ resource.ResourceWithImportState = &imageQuotaResource{}

func NewImageQuotaResource() resource.Resource {
	return &imageQuotaResource{}
}

type imageQuotaResource struct {
	client *service.ConohaClient
}

type imageQuotaResourceModel struct {
	ID          types.String `tfsdk:"id"`            // テナント ID
	ImageSizeGB types.Int64  `tfsdk:"image_size_gb"` // イメージ保存容量（GB）
}

func (r *imageQuotaResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_image_quota"
}

func (r *imageQuotaResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages the image save capacity (quota) of the account, where saved images are stored. **This changes your bill**: the default 50 GB is free, and ConoHa charges for each 500 GB added to it. " +
			"The account has exactly one image quota, so declare this resource at most once per tenant.\n\n" +
			"**Destroy** sets the capacity back to 50 GB (the free default; it cannot be set lower). " +
			"The capacity cannot be reduced below the space the images use: shrinking it, or destroying this resource while more than 50 GB of images remain, fails with the API's error and nothing is changed. Delete images first; `data.conohavps_image_usage` shows the usage.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The tenant ID of the account.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"image_size_gb": schema.Int64Attribute{
				MarkdownDescription: "The image save capacity in GB: 50 plus a multiple of 500 (50, 550, 1050, ...). It cannot be set below the space the images use.",
				Required:            true,
				Validators: []validator.Int64{
					quotaStepValidator{min: service.ImageQuotaDefaultGB, base: service.ImageQuotaDefaultGB, step: service.ImageQuotaStepGB},
				},
			},
		},
	}
}

func (r *imageQuotaResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *imageQuotaResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan imageQuotaResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	size := plan.ImageSizeGB.ValueInt64()
	tflog.Debug(ctx, "Starting image quota creation request.", map[string]any{"image_size_gb": size})

	// 既に容量が足されている場合は、それを上書きすることを知らせる
	current, err := r.client.GetImageQuota(ctx)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to create image quota resource",
			"An unexpected error occurred while attempting to read the current image quota.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}
	if current != service.ImageQuotaDefaultGB && current != size {
		resp.Diagnostics.AddWarning(
			"Existing image quota changed",
			fmt.Sprintf("The account already had an image quota of %d GB. It was changed to %d GB and is now managed by Terraform.", current, size),
		)
	}

	got, err := r.client.SetImageQuota(ctx, size)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to create image quota resource",
			"An error occurred while attempting to set the image quota. The quota cannot be set below the space the images use.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "Image quota creation request completed.", map[string]any{"image_size_gb": got})

	plan.ID = types.StringValue(r.client.TenantID)
	plan.ImageSizeGB = types.Int64Value(got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *imageQuotaResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state imageQuotaResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting image quota read request.", map[string]any{})

	size, err := r.client.GetImageQuota(ctx)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to read image quota resource",
			"An unexpected error occurred while attempting to read image quota resource.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "Image quota read request completed.", map[string]any{"image_size_gb": size})

	state.ID = types.StringValue(r.client.TenantID)
	state.ImageSizeGB = types.Int64Value(size)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *imageQuotaResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state imageQuotaResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	size := plan.ImageSizeGB.ValueInt64()
	tflog.Debug(ctx, "Starting image quota update request.", map[string]any{
		"from_gb": state.ImageSizeGB.ValueInt64(),
		"to_gb":   size,
	})

	got, err := r.client.SetImageQuota(ctx, size)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to update image quota resource",
			"An error occurred while attempting to change the image quota. The quota cannot be set below the space the images use.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "Image quota update request completed.", map[string]any{"image_size_gb": got})

	plan.ID = state.ID
	plan.ImageSizeGB = types.Int64Value(got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *imageQuotaResource) Delete(ctx context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	tflog.Debug(ctx, "Starting image quota deletion request.", map[string]any{"image_size_gb": service.ImageQuotaDefaultGB})

	// 削除は既定（無料）の 50GB に戻す. 使用量がそれを超えていれば API が拒否する
	if _, err := r.client.SetImageQuota(ctx, service.ImageQuotaDefaultGB); err != nil {
		resp.Diagnostics.AddError(
			"Failed to delete image quota resource",
			"An error occurred while attempting to set the image quota back to 50 GB. "+
				"The quota cannot be set below the space the images use; delete images first.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "Image quota deletion request completed.", map[string]any{})
}

func (r *imageQuotaResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// イメージ保存容量はアカウントに1つなので、プロバイダのテナント ID でだけインポートできる
	if r.client != nil && req.ID != r.client.TenantID {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("The image quota is imported by the tenant ID of the provider (%q), got %q.", r.client.TenantID, req.ID),
		)
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
