// キーペアのリソースを提供する.

package resource

import (
	"context"
	"fmt"
	"regexp"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &keypairResource{}
var _ resource.ResourceWithImportState = &keypairResource{}

func NewKeypairResource() resource.Resource {
	return &keypairResource{}
}

type keypairResource struct {
	client *service.ConohaClient
}

// リクエストパラメータ：作成時に指定可能なパラメータのみ.
type keypairResourceModel struct {
	Name       types.String `tfsdk:"name"`        // キーペア名
	PublicKey  types.String `tfsdk:"public_key"`  // 公開鍵
	PrivateKey types.String `tfsdk:"private_key"` // 秘密鍵
}

func (r *keypairResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_keypair"
}

func (r *keypairResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages keypair resource.",
		Attributes: map[string]schema.Attribute{
			// リクエストパラメータ
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the keypair. Must be 1-255 characters long and contain only alphanumeric characters, hyphens (-), and underscores (_). Changing this value will force the keypair to be recreated.",
				Required:            true,
				Validators: []validator.String{
					// 文字列長チェック(1～255文字)
					stringvalidator.LengthBetween(1, 255),
					// 利用可能な文字のみを含むかチェック
					stringvalidator.RegexMatches(
						regexp.MustCompile("^[a-zA-Z0-9-_]*$"),
						"The keypair name must contain only alphanumeric characters, hyphens (-), and underscores (_).",
					),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"public_key": schema.StringAttribute{
				MarkdownDescription: "The public key material. If not provided, a new keypair will be generated and the private key will be returned. Changing this value will force the keypair to be recreated.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			// レスポンス専用フィールド
			"private_key": schema.StringAttribute{
				MarkdownDescription: "The private key material. This is only returned when a new keypair is generated (i.e., when public_key is not provided during creation).",
				Computed:            true,
				Sensitive:           true,
			},
		},
	}
}

func (r *keypairResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *keypairResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data keypairResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting keypair creation request.", map[string]any{
		"name":       data.Name.ValueString(),
		"public_key": data.PublicKey.ValueString(),
	})

	// オプション構築
	opts := make(map[string]interface{})
	if !data.PublicKey.IsNull() {
		opts["public_key"] = data.PublicKey.ValueString()
	}

	// キーペア作成
	keypair, err := r.client.CreateKeypair(ctx, data.Name.ValueString(), opts)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to create keypair resource",
			"An unexpected error occurred while attempting to create keypair resource.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "Keypair creation request completed.", map[string]any{})

	// Stateを更新
	data.Name = types.StringValue(keypair.Name)
	data.PublicKey = types.StringValue(keypair.PublicKey)
	data.PrivateKey = types.StringValue(keypair.PrivateKey)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *keypairResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data keypairResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting keypair read request.", map[string]any{
		"name": data.Name.ValueString(),
	})

	// リソースの存在確認
	keypair, err := r.client.GetKeypair(ctx, data.Name.ValueString())
	if err != nil {
		if gophercloud.ResponseCodeIs(err, 404) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to read keypair resource",
			"An unexpected error occurred while attempting to read keypair resource.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "Keypair read request completed.", map[string]any{})

	// 公開鍵を更新
	data.PublicKey = types.StringValue(keypair.PublicKey)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)

}

func (r *keypairResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	// キーペアは更新不可（削除して再作成される）
	resp.Diagnostics.AddError(
		"An unexpected operation occurred",
		"Updating keypair resource is not supported. If changes are needed, delete and recreate the keypair.",
	)
}

func (r *keypairResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data keypairResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting keypair deletion request.", map[string]any{
		"name": data.Name.ValueString(),
	})

	// キーペア削除.
	err := r.client.DeleteKeypair(ctx, data.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to delete keypair resource",
			"An unexpected error occurred while attempting to delete keypair resource.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "Keypair deletion request completed.", map[string]any{
		"name": data.Name.ValueString(),
	})
}

func (r *keypairResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}
