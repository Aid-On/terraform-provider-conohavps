// オブジェクトストレージのコンテナのリソースを提供する.
// バージョニング（X-Versions-Location）と Web 公開（X-Container-Read: .r:*）は、
// どちらもコンテナにヘッダーで設定する値なので、コンテナの属性として持つ.

package resource

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &objectStorageContainerResource{}
var _ resource.ResourceWithImportState = &objectStorageContainerResource{}

// Web 公開のときに X-Container-Read に設定する値（Read 権限をすべて許可する）.
const publicReadACL = ".r:*"

// コンテナ名の制約（Swift の上限 256 バイト、"/" を含まない）.
var containerNameValidators = []validator.String{
	stringvalidator.LengthBetween(1, 256),
	stringvalidator.RegexMatches(regexp.MustCompile(`^[^/]+$`), "must not contain a slash (/)."),
}

func NewObjectStorageContainerResource() resource.Resource {
	return &objectStorageContainerResource{}
}

type objectStorageContainerResource struct {
	client *service.ConohaClient
}

type objectStorageContainerResourceModel struct {
	ID               types.String `tfsdk:"id"`                // コンテナ名と同じ
	Name             types.String `tfsdk:"name"`              // コンテナ名
	VersionsLocation types.String `tfsdk:"versions_location"` // 古いオブジェクトの保存先コンテナ
	WebPublishing    types.Bool   `tfsdk:"web_publishing"`    // Web 公開するか
	ObjectCount      types.Int64  `tfsdk:"object_count"`      // オブジェクト数
	BytesUsed        types.Int64  `tfsdk:"bytes_used"`        // 使用量（byte）
	StoragePolicy    types.String `tfsdk:"storage_policy"`    // ストレージポリシー
}

func (r *objectStorageContainerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_objectstorage_container"
}

func (r *objectStorageContainerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages an object storage container. Objects cannot be stored until the account has a capacity; set it with `conohavps_objectstorage_quota`. " +
			"A container that still holds objects cannot be deleted: destroying it fails with the API's error until the objects are removed (this provider does not manage objects).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The name of the container.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the container. Must be 1-256 bytes long and must not contain a slash (/). Changing this value will force the container to be recreated.",
				Required:            true,
				Validators:          containerNameValidators,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"versions_location": schema.StringAttribute{
				MarkdownDescription: "The name of the container that keeps old versions of objects (object versioning). " +
					"When an object with the same name is uploaded, the old object is saved to this container with a timestamp. " +
					"The container must already exist; reference its `conohavps_objectstorage_container` so that it is created first. Removing this value turns versioning off.",
				Optional:   true,
				Validators: containerNameValidators,
			},
			"web_publishing": schema.BoolAttribute{
				MarkdownDescription: "Whether the objects in the container are published on the web, by allowing read access to everyone (`X-Container-Read: .r:*`). Defaults to `false`.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"object_count": schema.Int64Attribute{
				MarkdownDescription: "The number of objects in the container.",
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"bytes_used": schema.Int64Attribute{
				MarkdownDescription: "The number of bytes used by the objects in the container.",
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"storage_policy": schema.StringAttribute{
				MarkdownDescription: "The storage policy of the container.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *objectStorageContainerResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *objectStorageContainerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan objectStorageContainerResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := plan.Name.ValueString()
	tflog.Debug(ctx, "Starting container creation request.", map[string]any{"name": name})

	// コンテナ作成
	if err := r.client.CreateContainer(ctx, name); err != nil {
		resp.Diagnostics.AddError(
			"Failed to create object storage container resource",
			"An unexpected error occurred while attempting to create object storage container resource.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	// 作成できた時点で State に残し、設定に失敗しても次の apply で直せるようにする
	plan.ID = types.StringValue(name)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), plan.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), plan.Name)...)

	// バージョニングと Web 公開は、作成とは別のリクエストで設定する
	headers := containerSettingHeaders(objectStorageContainerResourceModel{
		VersionsLocation: types.StringNull(),
		WebPublishing:    types.BoolValue(false),
	}, plan)
	if len(headers) > 0 {
		if err := r.client.UpdateContainer(ctx, name, headers); err != nil {
			resp.Diagnostics.AddError(
				"Failed to configure object storage container resource",
				"The container was created, but an error occurred while attempting to set its versioning or web publishing.\n\n"+
					"Error: "+err.Error(),
			)
			return
		}
	}

	resp.Diagnostics.Append(r.refresh(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Container creation request completed.", map[string]any{"name": name})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *objectStorageContainerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state objectStorageContainerResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Starting container read request.", map[string]any{"name": state.Name.ValueString()})

	container, err := r.client.GetContainer(ctx, state.Name.ValueString())
	if err != nil {
		if gophercloud.ResponseCodeIs(err, http.StatusNotFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to read object storage container resource",
			"An unexpected error occurred while attempting to read object storage container resource.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "Container read request completed.", map[string]any{})

	setContainerState(&state, container)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *objectStorageContainerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state objectStorageContainerResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := state.Name.ValueString()
	headers := containerSettingHeaders(state, plan)

	tflog.Debug(ctx, "Starting container update request.", map[string]any{"name": name, "headers": headers})

	if len(headers) > 0 {
		if err := r.client.UpdateContainer(ctx, name, headers); err != nil {
			resp.Diagnostics.AddError(
				"Failed to update object storage container resource",
				"An unexpected error occurred while attempting to update object storage container resource.\n\n"+
					"Error: "+err.Error(),
			)
			return
		}
	}

	plan.ID = state.ID
	resp.Diagnostics.Append(r.refresh(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Container update request completed.", map[string]any{"name": name})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *objectStorageContainerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state objectStorageContainerResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := state.Name.ValueString()
	tflog.Debug(ctx, "Starting container deletion request.", map[string]any{"name": name})

	if err := r.client.DeleteContainer(ctx, name); err != nil {
		if gophercloud.ResponseCodeIs(err, http.StatusNotFound) {
			return
		}
		detail := ""
		if gophercloud.ResponseCodeIs(err, http.StatusConflict) {
			detail = "The container still holds objects. Delete the objects first; this provider does not delete objects.\n\n"
		}
		resp.Diagnostics.AddError(
			"Failed to delete object storage container resource",
			detail+"An unexpected error occurred while attempting to delete object storage container resource.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "Container deletion request completed.", map[string]any{"name": name})
}

func (r *objectStorageContainerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
}

// コンテナを読み直して、API が返した値で model を埋める.
func (r *objectStorageContainerResource) refresh(ctx context.Context, model *objectStorageContainerResourceModel) (diags diag.Diagnostics) {
	container, err := r.client.GetContainer(ctx, model.Name.ValueString())
	if err != nil {
		diags.AddError(
			"Failed to read object storage container resource",
			"An unexpected error occurred while attempting to read object storage container resource.\n\n"+
				"Error: "+err.Error(),
		)
		return diags
	}
	setContainerState(model, container)
	return diags
}

// API の値を model に写す.
func setContainerState(model *objectStorageContainerResourceModel, container *service.ObjectStorageContainer) {
	model.ID = types.StringValue(container.Name)
	model.Name = types.StringValue(container.Name)
	if container.VersionsLocation == "" {
		model.VersionsLocation = types.StringNull()
	} else {
		model.VersionsLocation = types.StringValue(container.VersionsLocation)
	}
	model.WebPublishing = types.BoolValue(isPublicRead(container.ContainerRead))
	model.ObjectCount = types.Int64Value(container.ObjectCount)
	model.BytesUsed = types.Int64Value(container.BytesUsed)
	model.StoragePolicy = types.StringValue(container.StoragePolicy)
}

// Read 権限の ACL に、すべてを許可する .r:* が含まれるか.
func isPublicRead(acl string) bool {
	for _, grant := range strings.Split(acl, ",") {
		if strings.TrimSpace(grant) == publicReadACL {
			return true
		}
	}
	return false
}

// 現在の値 from から from→to に変えるためのヘッダーを返す.
// 解除はドキュメントのとおり値が空のヘッダー（X-Remove-Versions-Location / X-Container-Read）で行う.
func containerSettingHeaders(from, to objectStorageContainerResourceModel) map[string]string {
	headers := map[string]string{}

	if !from.VersionsLocation.Equal(to.VersionsLocation) {
		if to.VersionsLocation.IsNull() {
			headers["X-Remove-Versions-Location"] = ""
		} else {
			headers["X-Versions-Location"] = to.VersionsLocation.ValueString()
		}
	}

	if !from.WebPublishing.Equal(to.WebPublishing) {
		if to.WebPublishing.ValueBool() {
			headers["X-Container-Read"] = publicReadACL
		} else {
			headers["X-Container-Read"] = ""
		}
	}

	return headers
}
