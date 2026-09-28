// DNS のドメインのデータソースを提供する.
// ドメイン名（example.com. など）から、DNS に登録済みのドメインの ID と設定を引く.

package datasource

import (
	"context"
	"fmt"
	"strings"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &dnsDomainDataSource{}

func NewDNSDomainDataSource() datasource.DataSource {
	return &dnsDomainDataSource{}
}

type dnsDomainDataSource struct {
	client *service.ConohaClient
}

type dnsDomainDataSourceModel struct {
	Name      types.String `tfsdk:"name"`       // ドメイン名
	ID        types.String `tfsdk:"id"`         // ドメイン ID
	TTL       types.Int64  `tfsdk:"ttl"`        // TTL（秒）
	Email     types.String `tfsdk:"email"`      // 連絡先メールアドレス
	ProjectID types.String `tfsdk:"project_id"` // テナント ID
}

func (d *dnsDomainDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_domain"
}

func (d *dnsDomainDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a domain registered in ConoHa DNS by name. Use its `id` as `domain_id` of `conohavps_dns_record`.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: "The domain name, such as `example.com.`. The trailing period may be omitted, and letters are compared case-insensitively.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"id": schema.StringAttribute{
				MarkdownDescription: "The ID (UUID) of the domain.",
				Computed:            true,
			},
			"ttl": schema.Int64Attribute{
				MarkdownDescription: "The TTL of the domain in seconds.",
				Computed:            true,
			},
			"email": schema.StringAttribute{
				MarkdownDescription: "The contact email address of the domain.",
				Computed:            true,
			},
			"project_id": schema.StringAttribute{
				MarkdownDescription: "The tenant ID that owns the domain.",
				Computed:            true,
			},
		},
	}
}

func (d *dnsDomainDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientOf(req, resp)
}

func (d *dnsDomainDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data dnsDomainDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	list, err := d.client.ListDNSDomains(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list DNS domains", err.Error())
		return
	}

	domain, err := findDNSDomain(list, data.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("DNS domain not found", err.Error())
		return
	}

	data.ID = types.StringValue(domain.ID)
	data.TTL = types.Int64Value(domain.TTL)
	data.Email = types.StringValue(domain.Email)
	data.ProjectID = types.StringValue(domain.ProjectID)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// 名前が一致するドメインを1つだけ選ぶ（大文字小文字と末尾のピリオドの有無を無視する）.
func findDNSDomain(list []service.DNSDomain, name string) (service.DNSDomain, error) {
	want := strings.TrimSuffix(name, ".")
	var found []service.DNSDomain
	for _, dom := range list {
		if strings.EqualFold(strings.TrimSuffix(dom.Name, "."), want) {
			found = append(found, dom)
		}
	}

	switch len(found) {
	case 0:
		return service.DNSDomain{}, fmt.Errorf("no DNS domain is named %q", name)
	case 1:
		return found[0], nil
	default:
		return service.DNSDomain{}, fmt.Errorf("%d DNS domains are named %q", len(found), name)
	}
}
