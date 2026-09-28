// QoS ポリシーのデータソースを提供する.
// QoS ポリシー名（global-i_300000-o_300000 など）または ID から、帯域制限のルールとタグを引く.

package datasource

import (
	"context"
	"fmt"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource                     = &qosPolicyDataSource{}
	_ datasource.DataSourceWithConfigValidators = &qosPolicyDataSource{}
)

func NewQoSPolicyDataSource() datasource.DataSource {
	return &qosPolicyDataSource{}
}

type qosPolicyDataSource struct {
	client *service.ConohaClient
}

type qosPolicyDataSourceModel struct {
	Name        types.String `tfsdk:"name"`        // QoS ポリシー名（name と id のどちらかで引く）
	ID          types.String `tfsdk:"id"`          // QoS ポリシー ID
	Description types.String `tfsdk:"description"` // 説明
	Shared      types.Bool   `tfsdk:"shared"`      // 共有されているか
	IsDefault   types.Bool   `tfsdk:"is_default"`  // 既定のポリシーか
	Rules       types.List   `tfsdk:"rules"`       // 帯域制限のルール
	Tags        types.List   `tfsdk:"tags"`        // タグ（billing_flag=true など）
}

type qosPolicyRuleModel struct {
	ID           types.String `tfsdk:"id"`
	Type         types.String `tfsdk:"type"`
	Direction    types.String `tfsdk:"direction"`
	MaxKbps      types.Int64  `tfsdk:"max_kbps"`
	MaxBurstKbps types.Int64  `tfsdk:"max_burst_kbps"`
}

var qosPolicyRuleType = types.ObjectType{AttrTypes: map[string]attr.Type{
	"id":             types.StringType,
	"type":           types.StringType,
	"direction":      types.StringType,
	"max_kbps":       types.Int64Type,
	"max_burst_kbps": types.Int64Type,
}}

func (d *qosPolicyDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_qos_policy"
}

func (d *qosPolicyDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a QoS policy by name, such as `global-i_300000-o_300000` (300 Mbps, a paid option) or " +
			"`global-i_100000-o_100000` (the 100 Mbps default), or by ID. Use its `id` as `qos_policy_id` of `conohavps_port` or `conohavps_additional_ip`.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the QoS policy. It must match exactly one policy. Exactly one of `name` and `id` must be set.",
				Optional:            true,
				Computed:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"id": schema.StringAttribute{
				MarkdownDescription: "The ID of the QoS policy. Exactly one of `name` and `id` must be set.",
				Optional:            true,
				Computed:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "The description of the QoS policy, such as `Global: In 300.0 Mbps / Out 300.0 Mbps`.",
				Computed:            true,
			},
			"shared": schema.BoolAttribute{
				MarkdownDescription: "Whether the QoS policy is shared.",
				Computed:            true,
			},
			"is_default": schema.BoolAttribute{
				MarkdownDescription: "Whether the QoS policy is the default of the project.",
				Computed:            true,
			},
			"rules": schema.ListNestedAttribute{
				MarkdownDescription: "The bandwidth limit rules of the QoS policy.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":             schema.StringAttribute{MarkdownDescription: "The ID of the rule.", Computed: true},
						"type":           schema.StringAttribute{MarkdownDescription: "The type of the rule, such as `bandwidth_limit`.", Computed: true},
						"direction":      schema.StringAttribute{MarkdownDescription: "The direction of the traffic, `ingress` or `egress`.", Computed: true},
						"max_kbps":       schema.Int64Attribute{MarkdownDescription: "The maximum bandwidth in kbps.", Computed: true},
						"max_burst_kbps": schema.Int64Attribute{MarkdownDescription: "The maximum burst in kbps.", Computed: true},
					},
				},
			},
			"tags": schema.ListAttribute{
				MarkdownDescription: "The tags of the QoS policy, such as `billing_flag=true` for a paid policy.",
				ElementType:         types.StringType,
				Computed:            true,
			},
		},
	}
}

// name と id のどちらか一方だけを指定させる.
func (d *qosPolicyDataSource) ConfigValidators(_ context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.ExactlyOneOf(path.MatchRoot("name"), path.MatchRoot("id")),
	}
}

func (d *qosPolicyDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientOf(req, resp)
}

func (d *qosPolicyDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data qosPolicyDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	policy, ok := d.lookup(ctx, data, resp)
	if !ok {
		return
	}

	data.ID = types.StringValue(policy.ID)
	data.Name = types.StringValue(policy.Name)
	data.Description = types.StringValue(policy.Description)
	data.Shared = types.BoolValue(policy.Shared)
	data.IsDefault = types.BoolValue(policy.IsDefault)

	rules := make([]qosPolicyRuleModel, 0, len(policy.Rules))
	for _, r := range policy.Rules {
		rules = append(rules, qosPolicyRuleModel{
			ID:           types.StringValue(r.ID),
			Type:         types.StringValue(r.Type),
			Direction:    types.StringValue(r.Direction),
			MaxKbps:      types.Int64Value(r.MaxKbps),
			MaxBurstKbps: types.Int64Value(r.MaxBurstKbps),
		})
	}
	ruleList, diags := types.ListValueFrom(ctx, qosPolicyRuleType, rules)
	resp.Diagnostics.Append(diags...)
	data.Rules = ruleList

	tags := policy.Tags
	if tags == nil {
		tags = []string{}
	}
	tagList, diags := types.ListValueFrom(ctx, types.StringType, tags)
	resp.Diagnostics.Append(diags...)
	data.Tags = tagList
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// ID なら詳細の API で、名前なら一覧の API（クエリの name で絞る）で QoS ポリシーを引く.
func (d *qosPolicyDataSource) lookup(ctx context.Context, data qosPolicyDataSourceModel, resp *datasource.ReadResponse) (service.QoSPolicy, bool) {
	if id := data.ID.ValueString(); id != "" {
		policy, err := d.client.GetQoSPolicy(ctx, id)
		if err != nil {
			if gophercloud.ResponseCodeIs(err, 404) {
				resp.Diagnostics.AddError("QoS policy not found", fmt.Sprintf("no QoS policy has the ID %q", id))
				return service.QoSPolicy{}, false
			}
			resp.Diagnostics.AddError("Failed to retrieve QoS policy", err.Error())
			return service.QoSPolicy{}, false
		}
		return *policy, true
	}

	name := data.Name.ValueString()
	list, err := d.client.ListQoSPolicies(ctx, name)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list QoS policies", err.Error())
		return service.QoSPolicy{}, false
	}
	// 絞り込みの結果も名前が完全に一致するものだけに限る
	policy, err := findQoSPolicy(list, name)
	if err != nil {
		resp.Diagnostics.AddError("QoS policy not found", err.Error())
		return service.QoSPolicy{}, false
	}
	return policy, true
}

// 名前が一致する QoS ポリシーを1つだけ選ぶ.
func findQoSPolicy(list []service.QoSPolicy, name string) (service.QoSPolicy, error) {
	var found []service.QoSPolicy
	for _, p := range list {
		if p.Name == name {
			found = append(found, p)
		}
	}

	switch len(found) {
	case 0:
		return service.QoSPolicy{}, fmt.Errorf("no QoS policy is named %q", name)
	case 1:
		return found[0], nil
	default:
		return service.QoSPolicy{}, fmt.Errorf("%d QoS policies are named %q", len(found), name)
	}
}
