// クレデンシャル（EC2 形式のアクセスキーとシークレットキー）のリソースを提供する.
// クレデンシャルは更新できないため、属性の変更はすべて作り直しになる.

package resource

import (
	"context"
	"strings"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ resource.Resource                = &credentialResource{}
	_ resource.ResourceWithImportState = &credentialResource{}
)

func NewCredentialResource() resource.Resource {
	return &credentialResource{}
}

type credentialResource struct {
	client *service.ConohaClient
}

// クレデンシャルのリソースモデル.
type credentialResourceModel struct {
	ID       types.String `tfsdk:"id"`        // アクセスキー（クレデンシャル ID）
	UserID   types.String `tfsdk:"user_id"`   // ユーザー ID
	TenantID types.String `tfsdk:"tenant_id"` // テナント ID
	Access   types.String `tfsdk:"access"`    // アクセスキー
	Secret   types.String `tfsdk:"secret"`    // シークレットキー
}

func (r *credentialResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_credential"
}

func (r *credentialResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages an EC2-style credential (an access key and a secret key) of an API user, used by S3-compatible clients of the object storage. " +
			"An API user can have at most 3 credentials. Credentials cannot be updated; changing any argument creates a new credential.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The ID of the credential, which is the access key.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"user_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the API user that owns the credential. Changing this value will force the credential to be recreated.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"tenant_id": schema.StringAttribute{
				MarkdownDescription: "The tenant ID the credential is scoped to. Defaults to the provider's `tenant_id`. Changing this value will force the credential to be recreated.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"access": schema.StringAttribute{
				MarkdownDescription: "The access key.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"secret": schema.StringAttribute{
				MarkdownDescription: "The secret key.",
				Computed:            true,
				Sensitive:           true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *credentialResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = identityClientOf(req, resp)
}

func (r *credentialResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data credentialResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tenantID := data.TenantID.ValueString()
	if data.TenantID.IsNull() || data.TenantID.IsUnknown() {
		tenantID = r.client.TenantID
	}

	tflog.Debug(ctx, "Starting credential creation request.", map[string]any{"user_id": data.UserID.ValueString(), "tenant_id": tenantID})

	credential, err := r.client.CreateCredential(ctx, data.UserID.ValueString(), tenantID)
	if err != nil {
		tflog.Error(ctx, "Failed to create credential.", map[string]any{"error": err.Error()})
		resp.Diagnostics.AddError("Failed to create credential resource",
			"An unexpected error occurred while attempting to create credential resource. An API user can have at most 3 credentials.\n\nError: "+err.Error())
		return
	}

	tflog.Debug(ctx, "Credential creation request completed.", map[string]any{"access": credential.Access})

	setCredentialModel(&data, credential)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *credentialResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data credentialResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting credential read request.", map[string]any{"access": data.ID.ValueString()})

	credential, err := r.client.GetCredential(ctx, data.UserID.ValueString(), data.ID.ValueString())
	if err != nil {
		if gophercloud.ResponseCodeIs(err, 404) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read credential resource",
			"An unexpected error occurred while attempting to read credential resource.\n\nError: "+err.Error())
		return
	}

	setCredentialModel(&data, credential)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *credentialResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	// クレデンシャルは更新不可（削除して再作成される）
	resp.Diagnostics.AddError(
		"An unexpected operation occurred",
		"Updating credential resource is not supported. If changes are needed, delete and recreate the credential.",
	)
}

func (r *credentialResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data credentialResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting credential deletion request.", map[string]any{"access": data.ID.ValueString()})

	if err := r.client.DeleteCredential(ctx, data.UserID.ValueString(), data.ID.ValueString()); err != nil && !gophercloud.ResponseCodeIs(err, 404) {
		resp.Diagnostics.AddError("Failed to delete credential resource",
			"An unexpected error occurred while attempting to delete credential resource.\n\nError: "+err.Error())
		return
	}

	tflog.Debug(ctx, "Credential deletion request completed.", map[string]any{"access": data.ID.ValueString()})
}

// インポートの ID は "<ユーザー ID>/<アクセスキー>".
func (r *credentialResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	userID, access, ok := strings.Cut(req.ID, "/")
	if !ok || userID == "" || access == "" {
		resp.Diagnostics.AddError("Invalid import ID",
			"The import ID of a credential must be \"<user_id>/<access>\", but got: "+req.ID)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("user_id"), userID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), access)...)
}

// API のクレデンシャルをモデルへ写す. 作成と詳細取得の応答はどちらも全項目を返す.
func setCredentialModel(data *credentialResourceModel, credential *service.Credential) {
	data.ID = types.StringValue(credential.Access)
	data.Access = types.StringValue(credential.Access)
	data.UserID = types.StringValue(credential.UserID)
	data.TenantID = types.StringValue(credential.TenantID)
	data.Secret = types.StringValue(credential.Secret)
}
