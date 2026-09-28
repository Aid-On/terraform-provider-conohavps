// DNS のドメインのリソースを提供する.

package resource

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
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
	_ resource.Resource                = &DNSDomainResource{}
	_ resource.ResourceWithImportState = &DNSDomainResource{}
)

// ドメイン名（末尾のピリオドを含む FQDN）の形式.
var dnsDomainNamePattern = regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+$`)

// TTL の上限（RFC 2181 の 2^31 - 1 秒）.
const dnsMaxTTL = 2147483647

func NewDNSDomainResource() resource.Resource {
	return &DNSDomainResource{}
}

type DNSDomainResource struct {
	client *service.ConohaClient
}

// DNS のドメインのリソースモデル.
type DNSDomainResourceModel struct {
	ID        types.String `tfsdk:"id"`         // ドメイン ID
	Name      types.String `tfsdk:"name"`       // ドメイン名（末尾にピリオド）
	TTL       types.Int64  `tfsdk:"ttl"`        // TTL（秒）
	Email     types.String `tfsdk:"email"`      // 連絡先メールアドレス
	ProjectID types.String `tfsdk:"project_id"` // テナント ID
}

func (r *DNSDomainResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_domain"
}

func (r *DNSDomainResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a domain (zone) registered in ConoHa DNS. ConoHa creates the SOA and NS records of the domain automatically.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The ID (UUID) of the domain.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				// ドメイン名は更新 API で変更できない
				MarkdownDescription: "The domain name, ending with a period (e.g. `example.com.`). Letters are compared case-insensitively. Changing this value will force the domain to be recreated.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(2, 254),
					stringvalidator.RegexMatches(dnsDomainNamePattern, "must be a domain name ending with a period, such as \"example.com.\""),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"ttl": schema.Int64Attribute{
				MarkdownDescription: "The TTL of the domain in seconds.",
				Required:            true,
				Validators: []validator.Int64{
					int64validator.Between(1, dnsMaxTTL),
				},
			},
			"email": schema.StringAttribute{
				MarkdownDescription: "The contact email address of the domain.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(regexp.MustCompile(`^[^@\s]+@[^@\s]+$`), "must be an email address"),
				},
			},
			// レスポンス専用フィールド
			"project_id": schema.StringAttribute{
				MarkdownDescription: "The tenant ID that owns the domain.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *DNSDomainResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

	r.client = client
}

func (r *DNSDomainResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan DNSDomainResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting DNS domain creation request.", map[string]any{"name": plan.Name.ValueString()})

	domain, err := r.client.CreateDNSDomain(ctx, service.DNSDomainCreateOpts{
		Name:  plan.Name.ValueString(),
		TTL:   plan.TTL.ValueInt64(),
		Email: plan.Email.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to create DNS domain resource",
			"An unexpected error occurred while attempting to create DNS domain resource.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "DNS domain creation request completed.", map[string]any{"id": domain.ID})

	plan.fromAPI(domain)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *DNSDomainResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state DNSDomainResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting DNS domain read request.", map[string]any{"id": state.ID.ValueString()})

	domain, err := r.client.GetDNSDomain(ctx, state.ID.ValueString())
	if err != nil {
		if gophercloud.ResponseCodeIs(err, 404) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to read DNS domain resource",
			"An unexpected error occurred while attempting to read DNS domain resource.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "DNS domain read request completed.", map[string]any{"id": domain.ID})

	state.fromAPI(domain)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *DNSDomainResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state DNSDomainResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting DNS domain update request.", map[string]any{"id": state.ID.ValueString()})

	// 更新できるのは TTL と連絡先メールアドレスだけ
	domain, err := r.client.UpdateDNSDomain(ctx, state.ID.ValueString(), service.DNSDomainUpdateOpts{
		TTL:   plan.TTL.ValueInt64(),
		Email: plan.Email.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to update DNS domain resource",
			"An unexpected error occurred while attempting to update DNS domain resource.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "DNS domain update request completed.", map[string]any{"id": domain.ID})

	plan.ID = state.ID
	plan.fromAPI(domain)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *DNSDomainResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state DNSDomainResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting DNS domain deletion request.", map[string]any{"id": state.ID.ValueString()})

	if err := r.client.DeleteDNSDomain(ctx, state.ID.ValueString()); err != nil {
		if gophercloud.ResponseCodeIs(err, 404) {
			return
		}
		resp.Diagnostics.AddError(
			"Failed to delete DNS domain resource",
			"An unexpected error occurred while attempting to delete DNS domain resource.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "DNS domain deletion request completed.", map[string]any{"id": state.ID.ValueString()})
}

func (r *DNSDomainResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// API のレスポンスをモデルに写す.
// ドメイン名は大文字小文字だけの違いなら設定の表記を残し、plan に差分を出さない.
func (m *DNSDomainResourceModel) fromAPI(d *service.DNSDomain) {
	m.ID = types.StringValue(d.ID)
	m.Name = types.StringValue(dnsPreferName(m.Name, d.Name))
	m.TTL = types.Int64Value(d.TTL)
	m.Email = types.StringValue(dnsPreferEmail(m.Email, d.Email))
	m.ProjectID = types.StringValue(d.ProjectID)
}

// API が返したメールアドレスが伏せ字（"******@****.***" の形）なら、今の値を残す.
// ドキュメントのレスポンス例は伏せ字で、実際の API が伏せて返す場合に plan へ差分を出さないため.
func dnsPreferEmail(current types.String, fromAPI string) string {
	if !current.IsNull() && !current.IsUnknown() && dnsMaskedEmail.MatchString(fromAPI) {
		return current.ValueString()
	}
	return fromAPI
}

var dnsMaskedEmail = regexp.MustCompile(`^[*]+@[*.]+$`)

// API が返した名前と今の値が同じ名前を指すなら今の値を、違えば API の値を返す.
// DNS の名前は大文字小文字を区別せず、API は小文字に揃えて返すことがあるため.
func dnsPreferName(current types.String, fromAPI string) string {
	if !current.IsNull() && !current.IsUnknown() && dnsSameName(current.ValueString(), fromAPI) {
		return current.ValueString()
	}
	return fromAPI
}

// 2つの名前が同じ名前を指すか（大文字小文字と末尾のピリオドの有無を無視する）.
func dnsSameName(a, b string) bool {
	return strings.EqualFold(strings.TrimSuffix(a, "."), strings.TrimSuffix(b, "."))
}
