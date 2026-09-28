// サブユーザーに付与するロールのリソースを提供する.
// ロールは作成時にパーミッションが1つ以上必須で、0 にはできないため、パーミッションの全体を
// このリソースが持ち、差分を紐づけ（assign）と紐づけ解除（unassign）で反映する.

package resource

import (
	"context"
	"fmt"
	"regexp"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
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
	_ resource.Resource                = &roleResource{}
	_ resource.ResourceWithImportState = &roleResource{}
)

func NewRoleResource() resource.Resource {
	return &roleResource{}
}

type roleResource struct {
	client *service.ConohaClient
}

// ロールのリソースモデル.
type roleResourceModel struct {
	ID          types.String `tfsdk:"id"`          // ロール ID
	Name        types.String `tfsdk:"name"`        // ロール名
	Permissions types.Set    `tfsdk:"permissions"` // パーミッション名
	Visibility  types.String `tfsdk:"visibility"`  // 公開範囲（作成したロールは private）
}

func (r *roleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role"
}

func (r *roleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a role for API sub-users. A role is a set of permissions (API operations) that can be granted to a `conohavps_subuser`. " +
			"This resource owns the full set of the role's permissions: permissions added to or removed from the role outside Terraform show up as a change.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The ID of the role.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the role. Must be 1-32 characters long and contain only alphanumeric characters, underscores (_), and hyphens (-). Changing this value updates the name in place.",
				Required:            true,
				Validators: []validator.String{
					// 半角英数字・アンダースコア・ハイフンのみ、1～32文字
					stringvalidator.LengthBetween(1, 32),
					stringvalidator.RegexMatches(
						regexp.MustCompile(`^[a-zA-Z0-9_-]*$`),
						"The role name must contain only alphanumeric characters, underscores (_), and hyphens (-).",
					),
				},
			},
			"permissions": schema.SetAttribute{
				MarkdownDescription: "The names of the permissions granted by the role, such as `post-token` or `get-server-list`. At least one is required; the API does not allow a role without permissions. " +
					"See the `conohavps_permissions` data source for the available names. Changing this value assigns and unassigns permissions in place.",
				Required:    true,
				ElementType: types.StringType,
				Validators: []validator.Set{
					setvalidator.SizeAtLeast(1),
					setvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1)),
				},
			},
			"visibility": schema.StringAttribute{
				MarkdownDescription: "The visibility of the role. Roles created by users are `private`; the standard roles (`gmo-*`) are `public`.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *roleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = identityClientOf(req, resp)
}

func (r *roleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data roleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var permissions []string
	resp.Diagnostics.Append(data.Permissions.ElementsAs(ctx, &permissions, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting role creation request.", map[string]any{"name": data.Name.ValueString()})

	role, err := r.client.CreateRole(ctx, data.Name.ValueString(), permissions)
	if err != nil {
		tflog.Error(ctx, "Failed to create role.", map[string]any{"error": err.Error()})
		resp.Diagnostics.AddError("Failed to create role resource",
			"An unexpected error occurred while attempting to create role resource.\n\nError: "+err.Error())
		return
	}

	tflog.Debug(ctx, "Role creation request completed.", map[string]any{"id": role.ID})

	resp.Diagnostics.Append(setRoleModel(ctx, &data, role)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *roleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data roleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting role read request.", map[string]any{"id": data.ID.ValueString()})

	role, err := r.client.GetRole(ctx, data.ID.ValueString())
	if err != nil {
		if gophercloud.ResponseCodeIs(err, 404) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read role resource",
			"An unexpected error occurred while attempting to read role resource.\n\nError: "+err.Error())
		return
	}

	resp.Diagnostics.Append(setRoleModel(ctx, &data, role)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *roleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state roleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()

	tflog.Debug(ctx, "Starting role update request.", map[string]any{"id": id})

	if !plan.Name.Equal(state.Name) {
		if err := r.client.UpdateRoleName(ctx, id, plan.Name.ValueString()); err != nil {
			resp.Diagnostics.AddError("Failed to update role resource", err.Error())
			return
		}
	}

	if !plan.Permissions.Equal(state.Permissions) {
		var want []string
		resp.Diagnostics.Append(plan.Permissions.ElementsAs(ctx, &want, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		// 差分は API 上の現在の紐づけから取る（Terraform の外で変えられていても合わせられる）
		current, err := r.client.GetRole(ctx, id)
		if err != nil {
			resp.Diagnostics.AddError("Failed to update role resource", err.Error())
			return
		}
		add, remove := identityDiff(current.Permissions, want)
		// ロールのパーミッションは 0 にできないため、先に紐づけてから解除する
		if len(add) > 0 {
			if err := r.client.AssignRolePermissions(ctx, id, add); err != nil {
				resp.Diagnostics.AddError("Failed to update role resource", err.Error())
				return
			}
		}
		if len(remove) > 0 {
			if err := r.client.UnassignRolePermissions(ctx, id, remove); err != nil {
				resp.Diagnostics.AddError("Failed to update role resource", err.Error())
				return
			}
		}
	}

	role, err := r.client.GetRole(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read role resource after update", err.Error())
		return
	}

	tflog.Debug(ctx, "Role update request completed.", map[string]any{"id": id})

	resp.Diagnostics.Append(setRoleModel(ctx, &plan, role)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *roleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data roleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting role deletion request.", map[string]any{"id": data.ID.ValueString()})

	if err := r.client.DeleteRole(ctx, data.ID.ValueString()); err != nil && !gophercloud.ResponseCodeIs(err, 404) {
		resp.Diagnostics.AddError("Failed to delete role resource",
			"An unexpected error occurred while attempting to delete role resource. A role cannot be deleted while it is the only role of a sub-user.\n\nError: "+err.Error())
		return
	}

	tflog.Debug(ctx, "Role deletion request completed.", map[string]any{"id": data.ID.ValueString()})
}

func (r *roleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// API のロールをモデルへ写す.
func setRoleModel(ctx context.Context, data *roleResourceModel, role *service.Role) (diags diag.Diagnostics) {
	data.ID = types.StringValue(role.ID)
	data.Name = types.StringValue(role.Name)
	data.Visibility = types.StringValue(role.Visibility)
	permissions, d := types.SetValueFrom(ctx, types.StringType, identityNonNil(role.Permissions))
	diags.Append(d...)
	data.Permissions = permissions
	return diags
}

// プロバイダから渡された API クライアントを取り出す（Identity のリソースで共有する）.
func identityClientOf(req resource.ConfigureRequest, resp *resource.ConfigureResponse) *service.ConohaClient {
	if req.ProviderData == nil {
		return nil
	}
	client, ok := req.ProviderData.(*service.ConohaClient)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected resource configure type",
			fmt.Sprintf("Expected *service.ConohaClient, but got: %T.", req.ProviderData),
		)
		return nil
	}
	return client
}

// have から want にするために足すものと外すものを返す.
func identityDiff(have, want []string) (add, remove []string) {
	haveSet := make(map[string]bool, len(have))
	for _, h := range have {
		haveSet[h] = true
	}
	wantSet := make(map[string]bool, len(want))
	for _, w := range want {
		wantSet[w] = true
		if !haveSet[w] {
			add = append(add, w)
		}
	}
	for _, h := range have {
		if !wantSet[h] {
			remove = append(remove, h)
		}
	}
	return add, remove
}

// nil のスライスを空のスライスにする（空の集合を null にしない）.
func identityNonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
