// DNS のレコードのリソースを提供する.

package resource

import (
	"context"
	"fmt"
	"net/netip"
	"regexp"
	"strings"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
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
	_ resource.Resource                   = &DNSRecordResource{}
	_ resource.ResourceWithImportState    = &DNSRecordResource{}
	_ resource.ResourceWithValidateConfig = &DNSRecordResource{}
)

// 作成できるレコードのタイプ（SOA は ConoHa が自動で作る）.
// HTML ドキュメントはこの7つを挙げ、OpenAPI 仕様は "A, AAAA, CNAME, MX, TXT, SRV, NS, etc." とだけ書く.
var dnsRecordTypes = []string{"A", "AAAA", "CNAME", "MX", "NS", "SRV", "TXT"}

// レコード値がホスト名になるタイプ.
var dnsHostnameTypes = map[string]bool{"CNAME": true, "MX": true, "NS": true, "SRV": true}

// レコード名の形式（空白を含まず、末尾がピリオド）.
var dnsRecordNamePattern = regexp.MustCompile(`^[^\s.][^\s]*\.$`)

func NewDNSRecordResource() resource.Resource {
	return &DNSRecordResource{}
}

type DNSRecordResource struct {
	client *service.ConohaClient
}

// DNS のレコードのリソースモデル.
type DNSRecordResourceModel struct {
	ID          types.String `tfsdk:"id"`          // レコード ID
	DomainID    types.String `tfsdk:"domain_id"`   // ドメイン ID
	Name        types.String `tfsdk:"name"`        // レコード名（末尾にピリオド）
	Type        types.String `tfsdk:"type"`        // レコードタイプ
	Data        types.String `tfsdk:"data"`        // レコード値
	Priority    types.Int64  `tfsdk:"priority"`    // 優先度（MX・SRV）
	Weight      types.Int64  `tfsdk:"weight"`      // 重み（SRV）
	Port        types.Int64  `tfsdk:"port"`        // ポート番号（SRV）
	TTL         types.Int64  `tfsdk:"ttl"`         // TTL（秒）
	Description types.String `tfsdk:"description"` // 説明
}

func (r *DNSRecordResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_record"
}

func (r *DNSRecordResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	// 優先度・重み・ポート番号は DNS の仕様で 16 ビットの符号なし整数
	uint16Range := []validator.Int64{int64validator.Between(0, 65535)}

	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a DNS record of a domain in ConoHa DNS.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The ID (UUID) of the record.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"domain_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the domain (`conohavps_dns_domain`) the record belongs to. Changing this value will force the record to be recreated.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the record: the domain name itself or a name under it, ending with a period (e.g. `example.com.` or `www.example.com.`). Letters are compared case-insensitively. Changing this value will update the record.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(2, 254),
					stringvalidator.RegexMatches(dnsRecordNamePattern, "must be a name ending with a period, such as \"www.example.com.\""),
				},
			},
			"type": schema.StringAttribute{
				// 更新 API はタイプも受け付ける. 新しいタイプで要らなくなる priority・weight・port は null を送って消す
				MarkdownDescription: "The type of the record. One of `A`, `AAAA`, `CNAME`, `MX`, `NS`, `SRV` and `TXT`. Changing this value will update the record.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.OneOf(dnsRecordTypes...),
				},
			},
			"data": schema.StringAttribute{
				MarkdownDescription: "The value of the record: an IPv4 address for `A`, an IPv6 address for `AAAA`, a host name ending with a period for `CNAME`, `MX`, `NS` and `SRV` (the target), or text for `TXT`. Changing this value will update the record.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"priority": schema.Int64Attribute{
				MarkdownDescription: "The priority of the record. Required for `MX` and `SRV`, and not allowed for other types. Changing this value will update the record.",
				Optional:            true,
				Validators:          uint16Range,
			},
			"weight": schema.Int64Attribute{
				MarkdownDescription: "The weight of the record. Required for `SRV`, and not allowed for other types. Changing this value will update the record.",
				Optional:            true,
				Validators:          uint16Range,
			},
			"port": schema.Int64Attribute{
				MarkdownDescription: "The port number of the record. Required for `SRV`, and not allowed for other types. Changing this value will update the record.",
				Optional:            true,
				Validators:          uint16Range,
			},
			"ttl": schema.Int64Attribute{
				MarkdownDescription: "The TTL of the record in seconds. If omitted, ConoHa DNS chooses it, and removing it from the configuration keeps the current value. Changing this value will update the record.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.Int64{
					int64validator.Between(1, dnsMaxTTL),
				},
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "A free-text description of the record. Changing this value will update the record.",
				Optional:            true,
				Validators: []validator.String{
					// API は空の説明を説明なしと同じに返すため、空文字は受け付けない
					stringvalidator.LengthAtLeast(1),
				},
			},
		},
	}
}

// タイプごとに、priority・weight・port の要否とレコード値の形式を確かめる.
func (r *DNSRecordResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data DNSRecordResourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || data.Type.IsNull() || data.Type.IsUnknown() {
		return
	}
	typ := data.Type.ValueString()

	fields := []struct {
		name  string
		value types.Int64
		types map[string]bool
	}{
		{"priority", data.Priority, map[string]bool{"MX": true, "SRV": true}},
		{"weight", data.Weight, map[string]bool{"SRV": true}},
		{"port", data.Port, map[string]bool{"SRV": true}},
	}
	for _, f := range fields {
		switch {
		case f.types[typ] && f.value.IsNull():
			resp.Diagnostics.AddAttributeError(path.Root(f.name), "Missing attribute",
				fmt.Sprintf("%q is required for %s records.", f.name, typ))
		case !f.types[typ] && !f.value.IsNull():
			resp.Diagnostics.AddAttributeError(path.Root(f.name), "Unexpected attribute",
				fmt.Sprintf("%q is not allowed for %s records.", f.name, typ))
		}
	}

	if data.Data.IsNull() || data.Data.IsUnknown() {
		return
	}
	value := data.Data.ValueString()
	switch typ {
	case "A":
		if a, err := netip.ParseAddr(value); err != nil || !a.Is4() {
			resp.Diagnostics.AddAttributeError(path.Root("data"), "Invalid record value",
				fmt.Sprintf("%q is not an IPv4 address.", value))
		}
	case "AAAA":
		if a, err := netip.ParseAddr(value); err != nil || !a.Is6() || a.Is4In6() {
			resp.Diagnostics.AddAttributeError(path.Root("data"), "Invalid record value",
				fmt.Sprintf("%q is not an IPv6 address.", value))
		}
	}
}

func (r *DNSRecordResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *DNSRecordResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan DNSRecordResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting DNS record creation request.", map[string]any{
		"domain_id": plan.DomainID.ValueString(),
		"name":      plan.Name.ValueString(),
		"type":      plan.Type.ValueString(),
	})

	record, err := r.client.CreateDNSRecord(ctx, plan.DomainID.ValueString(), plan.opts(nil))
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to create DNS record resource",
			"An unexpected error occurred while attempting to create DNS record resource.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "DNS record creation request completed.", map[string]any{"id": record.ID})

	plan.fromAPI(record)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *DNSRecordResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state DNSRecordResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting DNS record read request.", map[string]any{
		"domain_id": state.DomainID.ValueString(),
		"id":        state.ID.ValueString(),
	})

	record, err := r.client.GetDNSRecord(ctx, state.DomainID.ValueString(), state.ID.ValueString())
	if err != nil {
		// ドメインごと消えた場合も 404 になる
		if gophercloud.ResponseCodeIs(err, 404) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to read DNS record resource",
			"An unexpected error occurred while attempting to read DNS record resource.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "DNS record read request completed.", map[string]any{"id": record.ID})

	state.fromAPI(record)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *DNSRecordResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state DNSRecordResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting DNS record update request.", map[string]any{
		"domain_id": state.DomainID.ValueString(),
		"id":        state.ID.ValueString(),
	})

	record, err := r.client.UpdateDNSRecord(ctx, state.DomainID.ValueString(), state.ID.ValueString(), plan.opts(&state))
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to update DNS record resource",
			"An unexpected error occurred while attempting to update DNS record resource.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "DNS record update request completed.", map[string]any{"id": record.ID})

	plan.ID = state.ID
	plan.fromAPI(record)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *DNSRecordResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state DNSRecordResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting DNS record deletion request.", map[string]any{
		"domain_id": state.DomainID.ValueString(),
		"id":        state.ID.ValueString(),
	})

	// ドメインを先に消した場合はレコードも消えているため、404 は削除済みとして扱う
	if err := r.client.DeleteDNSRecord(ctx, state.DomainID.ValueString(), state.ID.ValueString()); err != nil {
		if gophercloud.ResponseCodeIs(err, 404) {
			return
		}
		resp.Diagnostics.AddError(
			"Failed to delete DNS record resource",
			"An unexpected error occurred while attempting to delete DNS record resource.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "DNS record deletion request completed.", map[string]any{"id": state.ID.ValueString()})
}

// インポートの ID は "<ドメイン ID>/<レコード ID>".
func (r *DNSRecordResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	domainID, recordID, ok := strings.Cut(req.ID, "/")
	if !ok || domainID == "" || recordID == "" || strings.Contains(recordID, "/") {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("Expected an import ID of the form \"<domain_id>/<record_id>\", but got: %q.", req.ID),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("domain_id"), domainID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), recordID)...)
}

// モデルから作成・更新のリクエスト本文を組み立てる. 更新では今の state を渡す.
// 更新で設定から外れた priority・weight・port（タイプの変更で要らなくなったもの）は null を送って消し、
// 外れた説明は空文字を送って消す. ttl は値が未定（作成時に設定が無い）なら送らない.
func (m *DNSRecordResourceModel) opts(state *DNSRecordResourceModel) service.DNSRecordOpts {
	o := service.DNSRecordOpts{
		Name:        m.Name.ValueString(),
		Type:        m.Type.ValueString(),
		Data:        m.Data.ValueString(),
		Priority:    m.Priority.ValueInt64Pointer(),
		Weight:      m.Weight.ValueInt64Pointer(),
		Port:        m.Port.ValueInt64Pointer(),
		Description: m.Description.ValueStringPointer(),
	}
	if !m.TTL.IsUnknown() {
		o.TTL = m.TTL.ValueInt64Pointer()
	}
	if state == nil {
		return o
	}
	for _, f := range []struct {
		key         string
		plan, state types.Int64
	}{
		{"priority", m.Priority, state.Priority},
		{"weight", m.Weight, state.Weight},
		{"port", m.Port, state.Port},
	} {
		if f.plan.IsNull() && !f.state.IsNull() {
			o.Null = append(o.Null, f.key)
		}
	}
	o.Description = dnsDescriptionOpt(m.Description, state.Description)
	return o
}

// API のレスポンスをモデルに写す.
// API が表記を揃えて返す値（名前の大文字小文字、ホスト名の末尾のピリオド、IPv6 の省略形、
// TXT の引用符）は、今の値と同じものを指すなら今の値を残し、plan に差分を出さない.
func (m *DNSRecordResourceModel) fromAPI(rec *service.DNSRecord) {
	m.ID = types.StringValue(rec.ID)
	if rec.DomainID != "" {
		m.DomainID = types.StringValue(rec.DomainID)
	}
	m.Name = types.StringValue(dnsPreferName(m.Name, rec.Name))
	m.Type = types.StringValue(strings.ToUpper(rec.Type))
	if m.Data.IsNull() || m.Data.IsUnknown() || !dnsSameData(rec.Type, m.Data.ValueString(), rec.Data) {
		m.Data = types.StringValue(rec.Data)
	}
	m.Priority = types.Int64PointerValue(rec.Priority)
	m.Weight = types.Int64PointerValue(rec.Weight)
	m.Port = types.Int64PointerValue(rec.Port)
	m.TTL = types.Int64PointerValue(rec.TTL)
	m.Description = dnsDescriptionValue(rec.Description)
}

// 2つのレコード値が同じものを指すか.
func dnsSameData(typ, a, b string) bool {
	if a == b {
		return true
	}
	switch typ = strings.ToUpper(typ); {
	case typ == "AAAA":
		x, errX := netip.ParseAddr(a)
		y, errY := netip.ParseAddr(b)
		return errX == nil && errY == nil && x == y
	case dnsHostnameTypes[typ]:
		return dnsSameName(a, b)
	case typ == "TXT":
		return dnsUnquote(a) == dnsUnquote(b)
	}
	return false
}

// 前後の二重引用符を1組だけ外す.
func dnsUnquote(s string) string {
	if len(s) >= 2 && strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`) {
		return s[1 : len(s)-1]
	}
	return s
}
