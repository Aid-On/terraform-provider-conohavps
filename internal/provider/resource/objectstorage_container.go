// オブジェクトストレージのコンテナのリソースを提供する.
// バージョニング（X-Versions-Location）・ACL（X-Container-Read / Write）・Web 公開（X-Container-Meta-Web-*）・
// メタデータ（X-Container-Meta-*）は、どれもコンテナにヘッダーで設定する値なので、コンテナの属性として持つ.
// Read は HEAD が返したヘッダーだけから属性を埋め、外で変えられた値を差分として見せる.

package resource

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &objectStorageContainerResource{}
var _ resource.ResourceWithImportState = &objectStorageContainerResource{}

// コンテナ名の制約（Swift の上限 256 バイト、"/" を含まない）.
var containerNameValidators = []validator.String{
	stringvalidator.LengthBetween(1, 256),
	stringvalidator.RegexMatches(regexp.MustCompile(`^[^/]+$`), "must not contain a slash (/)."),
}

// ヘッダーの値の制約. 空の値は解除と同じ意味になり、前後の空白と制御文字は HTTP で保たれないため、どれも許さない.
var headerValueValidators = []validator.String{
	stringvalidator.RegexMatches(
		regexp.MustCompile(`^[^\x00-\x20\x7f](?:[^\x00-\x1f\x7f]*[^\x00-\x20\x7f])?$`),
		"must not be empty, start or end with a space, or contain control characters.",
	),
}

// メタデータのキーの制約. API はキーの大文字・小文字を区別せず、"_" を "-" と同じに扱うため、小文字・数字・"-"・"." に限る.
var metadataKeyValidators = []validator.String{
	stringvalidator.RegexMatches(regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`), "must be lowercase letters, digits, hyphens (-) and dots (.), such as `owner` or `access-control-allow-origin`."),
	stringvalidator.NoneOf(service.ReservedContainerMetaKeys...),
}

// ACL の書き方の別名（Swift 互換の API は .r: に書き換えて保存する）.
var aclReferrerAlias = regexp.MustCompile(`(?:^|,)\.(?:ref|referer|referrer):`)

// containerACLValidator は、ACL が API の保存する形で書かれていることを検証する.
// Swift 互換の API は空白を詰め、.referrer: などの別名を .r: に書き換えて保存するため、それ以外の形では Read との差分が出続ける.
type containerACLValidator struct{}

func (v containerACLValidator) Description(_ context.Context) string {
	return "value must be comma-separated grants without spaces, using `.r:` for referrer grants"
}

func (v containerACLValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v containerACLValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	acl := req.ConfigValue.ValueString()
	switch {
	case acl == "" || strings.ContainsAny(acl, " \t\r\n"):
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid ACL",
			fmt.Sprintf("The ACL must not be empty or contain spaces; separate the grants with commas only, such as \".r:*,.rlistings\", got: %q.", acl))
	case aclReferrerAlias.MatchString(acl):
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid ACL",
			fmt.Sprintf("Write referrer grants as \".r:\"; the API stores \".ref:\", \".referer:\" and \".referrer:\" as \".r:\", got: %q.", acl))
	}
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
	ContainerRead    types.String `tfsdk:"container_read"`    // Read 権限の ACL
	ContainerWrite   types.String `tfsdk:"container_write"`   // Write 権限の ACL
	WebIndex         types.String `tfsdk:"web_index"`         // Web 公開のインデックスファイル
	WebListings      types.Bool   `tfsdk:"web_listings"`      // Web 公開でオブジェクト一覧を表示するか
	WebListingsCSS   types.String `tfsdk:"web_listings_css"`  // オブジェクト一覧のスタイルシート
	WebError         types.String `tfsdk:"web_error"`         // Web 公開のエラーファイルの接尾辞
	Metadata         types.Map    `tfsdk:"metadata"`          // 任意のメタデータ
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
			"A container that still holds objects cannot be deleted: destroying it fails with the API's error (409) until the objects are removed (this provider does not manage objects).\n\n" +
			"Every setting is read back from the container, so a value changed outside Terraform shows as a difference, and a setting that is not in the configuration is removed on the next apply. " +
			"To publish the container on the web, allow everyone to read it with `container_read = \".r:*\"`, and set `web_index` to serve an index file.",
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
				MarkdownDescription: "The name of the container that keeps old versions of objects (object versioning, `X-Versions-Location`). " +
					"When an object with the same name is uploaded, the old object is saved to this container. " +
					"The container must already exist; reference its `conohavps_objectstorage_container` so that it is created first. Removing this value turns versioning off.",
				Optional:   true,
				Validators: containerNameValidators,
			},
			"container_read": schema.StringAttribute{
				MarkdownDescription: "The access control list that grants read access (`X-Container-Read`), as comma-separated grants without spaces. " +
					"`.r:*` lets everyone read the objects, which publishes them on the web; `.r:*,.rlistings` also lets everyone list them. Removing this value removes the ACL.",
				Optional:   true,
				Validators: []validator.String{containerACLValidator{}},
			},
			"container_write": schema.StringAttribute{
				MarkdownDescription: "The access control list that grants write access (`X-Container-Write`), as comma-separated grants without spaces. Removing this value removes the ACL.",
				Optional:            true,
				Validators:          []validator.String{containerACLValidator{}},
			},
			"web_index": schema.StringAttribute{
				MarkdownDescription: "The index file served for the container and its pseudo-directories when it is published on the web, such as `index.html` (`X-Container-Meta-Web-Index`). " +
					"It takes effect only when `container_read` lets everyone read.",
				Optional:   true,
				Validators: headerValueValidators,
			},
			"web_listings": schema.BoolAttribute{
				MarkdownDescription: "Whether a pseudo-directory without an index file is shown as an HTML list of its objects when the container is published on the web (`X-Container-Meta-Web-Listings`). " +
					"Leaving it unset removes the setting, which the API treats as `false`.",
				Optional: true,
			},
			"web_listings_css": schema.StringAttribute{
				MarkdownDescription: "The stylesheet used for the object lists of `web_listings` (`X-Container-Meta-Web-Listings-CSS`).",
				Optional:            true,
				Validators:          headerValueValidators,
			},
			"web_error": schema.StringAttribute{
				MarkdownDescription: "The suffix of the error files served when the container is published on the web, such as `error.html`, which serves `404error.html` for a missing object (`X-Container-Meta-Web-Error`).",
				Optional:            true,
				Validators:          headerValueValidators,
			},
			"metadata": schema.MapAttribute{
				MarkdownDescription: "Custom metadata of the container (`X-Container-Meta-{key}`). Keys must be lowercase letters, digits, hyphens and dots; " +
					"the keys that other attributes manage (`web-index`, `web-listings`, `web-listings-css`, `web-error`) and the temporary URL keys (`temp-url-key`, `temp-url-key-2`) cannot be used. " +
					"Metadata set outside Terraform shows as a difference and is removed on the next apply.",
				ElementType: types.StringType,
				Optional:    true,
				Validators: []validator.Map{
					mapvalidator.SizeAtLeast(1),
					mapvalidator.KeysAre(metadataKeyValidators...),
					mapvalidator.ValueStringsAre(headerValueValidators...),
				},
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

	// コンテナ作成. バージョニングは OpenAPI 仕様のとおり作成と同時に有効にする
	if err := r.client.CreateContainer(ctx, name, plan.VersionsLocation.ValueString()); err != nil {
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
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("versions_location"), plan.VersionsLocation)...)

	// ACL・Web 公開・メタデータは、作成とは別のリクエスト（POST）で設定する
	created := emptyContainerModel()
	created.VersionsLocation = plan.VersionsLocation
	headers, diags := containerSettingHeaders(ctx, created, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if len(headers) > 0 {
		if err := r.client.UpdateContainer(ctx, name, headers); err != nil {
			resp.Diagnostics.AddError(
				"Failed to configure object storage container resource",
				"The container was created, but an error occurred while attempting to set its ACLs, web publishing or metadata.\n\n"+
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

	resp.Diagnostics.Append(setContainerState(ctx, &state, container)...)
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
	headers, diags := containerSettingHeaders(ctx, state, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

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
	return setContainerState(ctx, model, container)
}

// 設定が1つも無い（作成した直後の）コンテナの model を返す.
func emptyContainerModel() objectStorageContainerResourceModel {
	return objectStorageContainerResourceModel{
		VersionsLocation: types.StringNull(),
		ContainerRead:    types.StringNull(),
		ContainerWrite:   types.StringNull(),
		WebIndex:         types.StringNull(),
		WebListings:      types.BoolNull(),
		WebListingsCSS:   types.StringNull(),
		WebError:         types.StringNull(),
		Metadata:         types.MapNull(types.StringType),
	}
}

// API の値を model に写す. ヘッダーが無い設定は null にする.
func setContainerState(ctx context.Context, model *objectStorageContainerResourceModel, container *service.ObjectStorageContainer) (diags diag.Diagnostics) {
	model.ID = types.StringValue(container.Name)
	model.Name = types.StringValue(container.Name)
	model.VersionsLocation = optionalString(container.VersionsLocation)
	model.ContainerRead = optionalString(container.ContainerRead)
	model.ContainerWrite = optionalString(container.ContainerWrite)
	model.WebIndex = optionalString(container.WebIndex)
	model.WebListings = types.BoolPointerValue(container.WebListings)
	model.WebListingsCSS = optionalString(container.WebListingsCSS)
	model.WebError = optionalString(container.WebError)
	if len(container.Metadata) == 0 {
		model.Metadata = types.MapNull(types.StringType)
	} else {
		model.Metadata, diags = types.MapValueFrom(ctx, types.StringType, container.Metadata)
	}
	model.ObjectCount = types.Int64Value(container.ObjectCount)
	model.BytesUsed = types.Int64Value(container.BytesUsed)
	model.StoragePolicy = types.StringValue(container.StoragePolicy)
	return diags
}

// 空の値を null にする.
func optionalString(v string) types.String {
	if v == "" {
		return types.StringNull()
	}
	return types.StringValue(v)
}

// 現在の値 from から from→to に変えるためのヘッダーを返す. 変わらない設定のヘッダーは送らない.
// 解除は X-Remove- を前に付けたヘッダー（OpenAPI 仕様の X-Remove-Versions-Location と同じ形）で行う.
func containerSettingHeaders(ctx context.Context, from, to objectStorageContainerResourceModel) (map[string]string, diag.Diagnostics) {
	headers := map[string]string{}

	setString := func(header string, from, to types.String, encode func(string) string) {
		switch {
		case from.Equal(to):
		case to.IsNull():
			headers[service.RemoveHeader(header)] = service.RemoveHeaderValue
		default:
			headers[header] = encode(to.ValueString())
		}
	}
	asIs := func(v string) string { return v }
	setString(service.HeaderVersionsLocation, from.VersionsLocation, to.VersionsLocation, service.EncodeVersionsLocation)
	setString(service.HeaderContainerRead, from.ContainerRead, to.ContainerRead, asIs)
	setString(service.HeaderContainerWrite, from.ContainerWrite, to.ContainerWrite, asIs)
	setString(service.HeaderWebIndex, from.WebIndex, to.WebIndex, asIs)
	setString(service.HeaderWebListingsCSS, from.WebListingsCSS, to.WebListingsCSS, asIs)
	setString(service.HeaderWebError, from.WebError, to.WebError, asIs)

	switch {
	case from.WebListings.Equal(to.WebListings):
	case to.WebListings.IsNull():
		headers[service.RemoveHeader(service.HeaderWebListings)] = service.RemoveHeaderValue
	default:
		headers[service.HeaderWebListings] = strconv.FormatBool(to.WebListings.ValueBool())
	}

	var diags diag.Diagnostics
	fromMeta, toMeta := map[string]string{}, map[string]string{}
	if !from.Metadata.IsNull() {
		diags.Append(from.Metadata.ElementsAs(ctx, &fromMeta, false)...)
	}
	if !to.Metadata.IsNull() {
		diags.Append(to.Metadata.ElementsAs(ctx, &toMeta, false)...)
	}
	for key, value := range toMeta {
		if old, ok := fromMeta[key]; !ok || old != value {
			headers[service.ContainerMetaPrefix+key] = value
		}
	}
	for key := range fromMeta {
		if _, ok := toMeta[key]; !ok {
			headers[service.RemoveHeader(service.ContainerMetaPrefix+key)] = service.RemoveHeaderValue
		}
	}

	return headers, diags
}
