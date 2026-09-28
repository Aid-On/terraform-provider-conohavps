// API サブユーザーのリソースを提供する.
// サブユーザーは作成時にロールが1つ以上必須で、0 にはできないため、ロールの全体を
// このリソースが持ち、差分を紐づけ（assign）と紐づけ解除（unassign）で反映する.
// API はパスワードを返さないため、トークンを発行できるサブユーザーは、読み込みのたびに
// State のパスワードでトークンを発行して、Terraform の外での変更を検出する.

package resource

import (
	"context"
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
	_ resource.Resource                = &subUserResource{}
	_ resource.ResourceWithImportState = &subUserResource{}
)

func NewSubUserResource() resource.Resource {
	return &subUserResource{}
}

type subUserResource struct {
	client *service.ConohaClient
}

// サブユーザーのリソースモデル.
type subUserResourceModel struct {
	ID       types.String `tfsdk:"id"`       // サブユーザー ID
	Name     types.String `tfsdk:"name"`     // サブユーザー名（API が決める）
	Password types.String `tfsdk:"password"` // パスワード（API から読み戻せず、トークンの発行で確かめる）
	Roles    types.Set    `tfsdk:"roles"`    // ロール ID またはロール名
}

func (r *subUserResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_subuser"
}

func (r *subUserResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages an API sub-user: an API user of the account that can only call the operations its roles permit. " +
			"Use it to give an automated client (such as an AI agent) its own narrowed API user instead of the account's main API user. " +
			"The sub-user's name is chosen by ConoHa. This resource owns the full set of the sub-user's roles.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The ID of the sub-user. Use it as the user ID when authenticating as the sub-user.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the sub-user, assigned by ConoHa.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"password": schema.StringAttribute{
				MarkdownDescription: "The password of the sub-user. Must be 9-70 characters long, use only alphanumeric characters and the symbols `!#$%&?\"'=+-_{}[]^~:;().,/|\\*@`, " +
					"and contain at least one lowercase letter, one uppercase letter, and one digit or symbol. " +
					"The API does not return the password. When the sub-user has the standard role `gmo-identity`, each refresh verifies the password by issuing a token as the sub-user, " +
					"and a password changed outside Terraform shows up as a change that sets it back. Without `gmo-identity` the password cannot be verified, so such changes are not detected. " +
					"If the verification cannot complete (for example on a network error or a server error), the stored password is kept and a warning is logged. Changing this value updates the password in place.",
				Required:  true,
				Sensitive: true,
				Validators: []validator.String{
					subUserPasswordValidator{},
				},
			},
			"roles": schema.SetAttribute{
				MarkdownDescription: "The roles granted to the sub-user, each given by role ID or role name (such as `conohavps_role.example.id` or the standard role `gmo-identity`). " +
					"At least one and at most 500; the API does not allow a sub-user without roles. " +
					"A sub-user needs a role with the `post-token` permission (such as `gmo-identity`) to issue a token. Changing this value assigns and unassigns roles in place.",
				Required:    true,
				ElementType: types.StringType,
				Validators: []validator.Set{
					setvalidator.SizeBetween(1, 500),
					setvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1)),
				},
			},
		},
	}
}

func (r *subUserResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = identityClientOf(req, resp)
}

func (r *subUserResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data subUserResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var roles []string
	resp.Diagnostics.Append(data.Roles.ElementsAs(ctx, &roles, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting sub-user creation request.", map[string]any{"roles": roles})

	user, err := r.client.CreateSubUser(ctx, data.Password.ValueString(), roles)
	if err != nil {
		tflog.Error(ctx, "Failed to create sub-user.", map[string]any{"error": err.Error()})
		resp.Diagnostics.AddError("Failed to create sub-user resource",
			"An unexpected error occurred while attempting to create sub-user resource.\n\nError: "+err.Error())
		return
	}

	tflog.Debug(ctx, "Sub-user creation request completed.", map[string]any{"id": user.ID})

	resp.Diagnostics.Append(setSubUserModel(ctx, &data, user, roles)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *subUserResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data subUserResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting sub-user read request.", map[string]any{"id": data.ID.ValueString()})

	user, err := r.client.GetSubUser(ctx, data.ID.ValueString())
	if err != nil {
		if gophercloud.ResponseCodeIs(err, 404) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read sub-user resource",
			"An unexpected error occurred while attempting to read sub-user resource.\n\nError: "+err.Error())
		return
	}

	var prior []string
	if !data.Roles.IsNull() && !data.Roles.IsUnknown() {
		resp.Diagnostics.Append(data.Roles.ElementsAs(ctx, &prior, false)...)
	}

	resp.Diagnostics.Append(setSubUserModel(ctx, &data, user, prior)...)
	data.Password = r.verifiedPassword(ctx, user, data.Password)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *subUserResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state subUserResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()

	tflog.Debug(ctx, "Starting sub-user update request.", map[string]any{"id": id})

	if !plan.Password.Equal(state.Password) {
		if err := r.client.UpdateSubUserPassword(ctx, id, plan.Password.ValueString()); err != nil {
			resp.Diagnostics.AddError("Failed to update sub-user resource", err.Error())
			return
		}
	}

	var want []string
	resp.Diagnostics.Append(plan.Roles.ElementsAs(ctx, &want, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !plan.Roles.Equal(state.Roles) {
		resp.Diagnostics.Append(r.applyRoles(ctx, id, want)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	user, err := r.client.GetSubUser(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read sub-user resource after update", err.Error())
		return
	}

	tflog.Debug(ctx, "Sub-user update request completed.", map[string]any{"id": id})

	resp.Diagnostics.Append(setSubUserModel(ctx, &plan, user, want)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// State のパスワードをトークンの発行で確かめ、残す値を返す. パスワードは API から読み戻せないため、
// 発行が認証で拒否されたとき（Terraform の外で変えられたとき）だけ null にして、設定の値を当て直す差分にする.
// トークンを発行できるロール（gmo-identity）が無いとき、または確かめられないときは State の値を残す.
func (r *subUserResource) verifiedPassword(ctx context.Context, user *service.SubUser, password types.String) types.String {
	if password.IsNull() || password.IsUnknown() {
		return password
	}
	if subUserRoleIndex(user.Roles, service.IdentityTokenRole) < 0 {
		tflog.Debug(ctx, "Skipping sub-user password verification: the sub-user cannot issue a token.", map[string]any{"id": user.ID})
		return password
	}
	valid, err := r.client.SubUserPasswordValid(ctx, user.ID, password.ValueString())
	switch {
	case err != nil:
		tflog.Warn(ctx, "Could not verify the sub-user password; keeping the stored password.", map[string]any{"id": user.ID, "error": err.Error()})
		return password
	case !valid:
		tflog.Info(ctx, "The sub-user password was changed outside Terraform.", map[string]any{"id": user.ID})
		return types.StringNull()
	default:
		return password
	}
}

// サブユーザーのロールを want（ロール ID またはロール名）に合わせる.
// 紐づけと解除の API はロール ID を取るため、名前はロール一覧から ID に引き直す.
func (r *subUserResource) applyRoles(ctx context.Context, id string, want []string) (diags diag.Diagnostics) {
	current, err := r.client.GetSubUser(ctx, id)
	if err != nil {
		diags.AddError("Failed to update sub-user resource", err.Error())
		return diags
	}

	var add []string
	var all []service.Role
	for _, w := range want {
		if subUserRoleIndex(current.Roles, w) >= 0 {
			continue
		}
		if all == nil {
			if all, err = r.client.ListRoles(ctx); err != nil {
				diags.AddError("Failed to update sub-user resource", err.Error())
				return diags
			}
		}
		roleID, ok := roleIDOf(all, w)
		if !ok {
			diags.AddError("Failed to update sub-user resource", "No role has the ID or name "+w+".")
			return diags
		}
		add = append(add, roleID)
	}

	var remove []string
	for _, role := range current.Roles {
		if !roleWanted(role, want) {
			remove = append(remove, role.ID)
		}
	}

	// サブユーザーのロールは 0 にできないため、先に紐づけてから解除する
	if len(add) > 0 {
		if err := r.client.AssignSubUserRoles(ctx, id, add); err != nil {
			diags.AddError("Failed to update sub-user resource", err.Error())
			return diags
		}
	}
	if len(remove) > 0 {
		if err := r.client.UnassignSubUserRoles(ctx, id, remove); err != nil {
			diags.AddError("Failed to update sub-user resource", err.Error())
			return diags
		}
	}
	return diags
}

func (r *subUserResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data subUserResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting sub-user deletion request.", map[string]any{"id": data.ID.ValueString()})

	if err := r.client.DeleteSubUser(ctx, data.ID.ValueString()); err != nil && !gophercloud.ResponseCodeIs(err, 404) {
		resp.Diagnostics.AddError("Failed to delete sub-user resource",
			"An unexpected error occurred while attempting to delete sub-user resource.\n\nError: "+err.Error())
		return
	}

	tflog.Debug(ctx, "Sub-user deletion request completed.", map[string]any{"id": data.ID.ValueString()})
}

func (r *subUserResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// API のサブユーザーをモデルへ写す. ロールは、prior（設定または State の値）が ID で書いていれば ID、
// 名前で書いていれば名前で持つ（どちらで書いても差分が出ないようにする）. prior に無いロールは ID で持つ.
func setSubUserModel(ctx context.Context, data *subUserResourceModel, user *service.SubUser, prior []string) (diags diag.Diagnostics) {
	data.ID = types.StringValue(user.ID)
	data.Name = types.StringValue(user.Name)

	roles := make([]string, 0, len(user.Roles))
	for _, role := range user.Roles {
		roles = append(roles, roleRef(role, prior))
	}
	set, d := types.SetValueFrom(ctx, types.StringType, roles)
	diags.Append(d...)
	data.Roles = set
	return diags
}

// ロールを prior での書き方（ID か名前）で返す.
func roleRef(role service.SubUserRole, prior []string) string {
	for _, p := range prior {
		if p == role.ID {
			return role.ID
		}
	}
	for _, p := range prior {
		if p == role.Name {
			return role.Name
		}
	}
	return role.ID
}

// ID または名前が ref のロールの位置を返す. 無ければ -1.
func subUserRoleIndex(roles []service.SubUserRole, ref string) int {
	for i, role := range roles {
		if role.ID == ref || role.Name == ref {
			return i
		}
	}
	return -1
}

// ロールが want（ID または名前）に含まれるかを返す.
func roleWanted(role service.SubUserRole, want []string) bool {
	for _, w := range want {
		if w == role.ID || w == role.Name {
			return true
		}
	}
	return false
}

// ID または名前が ref のロールの ID を返す.
func roleIDOf(roles []service.Role, ref string) (string, bool) {
	for _, role := range roles {
		if role.ID == ref {
			return role.ID, true
		}
	}
	for _, role := range roles {
		if role.Name == ref {
			return role.ID, true
		}
	}
	return "", false
}

// パスワードに使える文字（半角英数字と記号 !#$%&?"'=+-_{}[]^~:;().,/|\*@）.
var (
	subUserPasswordChars      = regexp.MustCompile(`^[a-zA-Z0-9!#$%&?"'=+\-_{}\[\]^~:;().,/|\\*@]*$`)
	subUserPasswordLower      = regexp.MustCompile(`[a-z]`)
	subUserPasswordUpper      = regexp.MustCompile(`[A-Z]`)
	subUserPasswordDigitOrSym = regexp.MustCompile(`[^a-zA-Z]`)
)

// サブユーザーのパスワードの規則を確かめる. エラーにパスワードの値を含めない.
type subUserPasswordValidator struct{}

func (v subUserPasswordValidator) Description(_ context.Context) string {
	return "must be 9-70 characters of alphanumerics and the symbols !#$%&?\"'=+-_{}[]^~:;().,/|\\*@, with at least one lowercase letter, one uppercase letter, and one digit or symbol"
}

func (v subUserPasswordValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v subUserPasswordValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	password := req.ConfigValue.ValueString()
	var problems []string
	if n := len(password); n < 9 || n > 70 {
		problems = append(problems, "be 9-70 characters long")
	}
	if !subUserPasswordChars.MatchString(password) {
		problems = append(problems, "contain only alphanumeric characters and the symbols !#$%&?\"'=+-_{}[]^~:;().,/|\\*@")
	}
	if !subUserPasswordLower.MatchString(password) {
		problems = append(problems, "contain at least one lowercase letter")
	}
	if !subUserPasswordUpper.MatchString(password) {
		problems = append(problems, "contain at least one uppercase letter")
	}
	if !subUserPasswordDigitOrSym.MatchString(password) {
		problems = append(problems, "contain at least one digit or symbol")
	}
	for _, p := range problems {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid sub-user password", "The password must "+p+".")
	}
}
