// ロードバランサー（LBaaS）のリソースに共通する処理を提供する.

package resource

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// LBaaS の作成・更新・削除で、provisioning_status の遷移を待つ上限.
// ロードバランサーの追加は VIP の払い出しを伴うため、子リソースより長くかかる.
const (
	lbLoadBalancerTimeout = 15 * time.Minute
	lbChildTimeout        = 10 * time.Minute
)

// プロバイダのクライアントを受け取る.
func configureLBClient(req resource.ConfigureRequest, resp *resource.ConfigureResponse) *service.ConohaClient {
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

// API の失敗を診断に積む. action は create・read・update・delete、kind はリソースの呼び名.
func addLBError(diags *diag.Diagnostics, action, kind string, err error) {
	diags.AddError(
		fmt.Sprintf("Failed to %s %s resource", action, kind),
		fmt.Sprintf("An unexpected error occurred while attempting to %s %s resource.\n\nError: %s", action, kind, err.Error()),
	)
}

// API が返すだけで、追加・更新の要求では指定できない admin_state_up の属性.
// 更新の要求で変わらないので、plan では state の値を引き継ぐ.
func lbReadOnlyAdminStateUp(kind string) schema.BoolAttribute {
	return schema.BoolAttribute{
		MarkdownDescription: "Whether the " + kind + " is administratively up. The API does not accept this value on create or update, so it is read only.",
		Computed:            true,
		PlanModifiers: []planmodifier.Bool{
			boolplanmodifier.UseStateForUnknown(),
		},
	}
}

// null を取りうる文字列を Terraform の値にする.
func lbNullableString(v *string) types.String {
	if v == nil {
		return types.StringNull()
	}
	return types.StringValue(*v)
}

// グローバル IP アドレスであることを確かめるバリデーター.
// メンバーのバランシング先にはグローバル IP アドレスしか指定できない.
type lbGlobalIPValidator struct{}

func (v lbGlobalIPValidator) Description(_ context.Context) string {
	return "value must be a global IP address"
}

func (v lbGlobalIPValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v lbGlobalIPValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	addr, err := netip.ParseAddr(req.ConfigValue.ValueString())
	if err != nil || addr.Zone() != "" || !addr.IsGlobalUnicast() || addr.IsPrivate() {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid attribute value",
			fmt.Sprintf("Attribute must be a global IP address, got: %s.", req.ConfigValue.ValueString()),
		)
	}
}
