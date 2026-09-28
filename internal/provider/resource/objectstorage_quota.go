// オブジェクトストレージの契約容量（アカウントに1つ）のリソースを提供する.
// 契約容量は 100GB 単位で課金されるため、刻みを検証し、使用量を下回る変更は API のエラーをそのまま返す.

package resource

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &objectStorageQuotaResource{}
var _ resource.ResourceWithImportState = &objectStorageQuotaResource{}

func NewObjectStorageQuotaResource() resource.Resource {
	return &objectStorageQuotaResource{}
}

type objectStorageQuotaResource struct {
	client *service.ConohaClient
}

type objectStorageQuotaResourceModel struct {
	ID             types.String `tfsdk:"id"`              // テナント ID
	QuotaGB        types.Int64  `tfsdk:"quota_gb"`        // 契約容量（GB）
	BytesUsed      types.Int64  `tfsdk:"bytes_used"`      // 使用量（byte）
	ContainerCount types.Int64  `tfsdk:"container_count"` // コンテナ数
	ObjectCount    types.Int64  `tfsdk:"object_count"`    // オブジェクト数
}

func (r *objectStorageQuotaResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_objectstorage_quota"
}

func (r *objectStorageQuotaResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages the object storage capacity (quota) of the account. **This changes your bill**: ConoHa charges for the capacity in units of 100 GB, whether or not it is used. " +
			"The account has exactly one quota, so declare this resource at most once per tenant. The default capacity is 0 GB, and objects cannot be stored until it is 100 GB or more.\n\n" +
			"**Destroy** sets the capacity back to 0 GB (the default, no contract). " +
			"The capacity cannot be reduced below the space the objects use: shrinking it, or destroying this resource while objects remain, fails with the API's error and nothing is changed. Delete the objects first.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The tenant ID of the account.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"quota_gb": schema.Int64Attribute{
				MarkdownDescription: "The capacity in GB. Must be a multiple of 100 and at least 100 (100, 200, 300, ...). It cannot be set below the space the objects use.",
				Required:            true,
				Validators: []validator.Int64{
					quotaStepValidator{min: service.ObjectStorageQuotaStepGB, base: 0, step: service.ObjectStorageQuotaStepGB},
				},
			},
			"bytes_used": schema.Int64Attribute{
				MarkdownDescription: "The number of bytes the objects of the account use.",
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"container_count": schema.Int64Attribute{
				MarkdownDescription: "The number of containers of the account.",
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"object_count": schema.Int64Attribute{
				MarkdownDescription: "The number of objects of the account.",
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *objectStorageQuotaResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *objectStorageQuotaResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan objectStorageQuotaResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	quota := plan.QuotaGB.ValueInt64()
	tflog.Debug(ctx, "Starting object storage quota creation request.", map[string]any{"quota_gb": quota})

	// 既に契約容量がある場合は、それを上書きすることを知らせる
	current, err := r.client.GetObjectStorageAccount(ctx)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to create object storage quota resource",
			"An unexpected error occurred while attempting to read the current object storage quota.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}
	if current.QuotaGB != service.ObjectStorageQuotaDefaultGB && current.QuotaGB != quota {
		resp.Diagnostics.AddWarning(
			"Existing object storage quota changed",
			fmt.Sprintf("The account already had an object storage quota of %d GB. It was changed to %d GB and is now managed by Terraform.", current.QuotaGB, quota),
		)
	}

	if err := r.client.SetObjectStorageQuota(ctx, quota); err != nil {
		resp.Diagnostics.AddError(
			"Failed to create object storage quota resource",
			"An error occurred while attempting to set the object storage quota. The quota cannot be set below the space the objects use.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	resp.Diagnostics.Append(r.refresh(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Object storage quota creation request completed.", map[string]any{"quota_gb": quota})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *objectStorageQuotaResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state objectStorageQuotaResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting object storage quota read request.", map[string]any{})

	account, err := r.client.GetObjectStorageAccount(ctx)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to read object storage quota resource",
			"An unexpected error occurred while attempting to read object storage quota resource.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	// 契約容量が既定（0GB）に戻っていれば、契約が無い（リソースが無い）ものとして扱う
	if account.QuotaGB == service.ObjectStorageQuotaDefaultGB {
		resp.State.RemoveResource(ctx)
		return
	}

	tflog.Debug(ctx, "Object storage quota read request completed.", map[string]any{"quota_gb": account.QuotaGB})

	r.setState(&state, account)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *objectStorageQuotaResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state objectStorageQuotaResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	quota := plan.QuotaGB.ValueInt64()
	tflog.Debug(ctx, "Starting object storage quota update request.", map[string]any{
		"from_gb": state.QuotaGB.ValueInt64(),
		"to_gb":   quota,
	})

	if err := r.client.SetObjectStorageQuota(ctx, quota); err != nil {
		resp.Diagnostics.AddError(
			"Failed to update object storage quota resource",
			"An error occurred while attempting to change the object storage quota. The quota cannot be set below the space the objects use.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	resp.Diagnostics.Append(r.refresh(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Object storage quota update request completed.", map[string]any{"quota_gb": quota})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *objectStorageQuotaResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	tflog.Debug(ctx, "Starting object storage quota deletion request.", map[string]any{"quota_gb": service.ObjectStorageQuotaDefaultGB})

	// 削除は契約容量を既定の 0GB に戻す. オブジェクトが残っていれば API が拒否する
	if err := r.client.SetObjectStorageQuota(ctx, service.ObjectStorageQuotaDefaultGB); err != nil {
		resp.Diagnostics.AddError(
			"Failed to delete object storage quota resource",
			"An error occurred while attempting to set the object storage quota back to 0 GB. "+
				"The quota cannot be set below the space the objects use; delete the objects first.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "Object storage quota deletion request completed.", map[string]any{})
}

func (r *objectStorageQuotaResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// 契約容量はアカウントに1つなので、プロバイダのテナント ID でだけインポートできる
	if r.client != nil && req.ID != r.client.TenantID {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("The object storage quota is imported by the tenant ID of the provider (%q), got %q.", r.client.TenantID, req.ID),
		)
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// 契約容量を読み直して model を埋める.
func (r *objectStorageQuotaResource) refresh(ctx context.Context, model *objectStorageQuotaResourceModel) (diags diag.Diagnostics) {
	account, err := r.client.GetObjectStorageAccount(ctx)
	if err != nil {
		diags.AddError(
			"Failed to read object storage quota resource",
			"The quota was set, but an error occurred while attempting to read it back.\n\n"+
				"Error: "+err.Error(),
		)
		return diags
	}
	r.setState(model, account)
	return diags
}

// API の値を model に写す.
func (r *objectStorageQuotaResource) setState(model *objectStorageQuotaResourceModel, account *service.ObjectStorageAccount) {
	model.ID = types.StringValue(r.client.TenantID)
	model.QuotaGB = types.Int64Value(account.QuotaGB)
	model.BytesUsed = types.Int64Value(account.BytesUsed)
	model.ContainerCount = types.Int64Value(account.ContainerCount)
	model.ObjectCount = types.Int64Value(account.ObjectCount)
}
