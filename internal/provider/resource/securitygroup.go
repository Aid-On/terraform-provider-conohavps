// セキュリティグループのリソースを提供する.

package resource

import (
	"context"
	"fmt"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/groups"
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
	_ resource.Resource                = &SecurityGroupResource{}
	_ resource.ResourceWithImportState = &SecurityGroupResource{}
)

func NewSecurityGroupResource() resource.Resource {
	return &SecurityGroupResource{}
}

type SecurityGroupResource struct {
	client *service.ConohaClient
}

// セキュリティグループのリソースモデル.
type SecurityGroupResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
}

func (r *SecurityGroupResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_securitygroup"
}

func (r *SecurityGroupResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages security group resource.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The ID of the security group.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				// 文字数の下限は 1 文字、上限は 255 文字
				MarkdownDescription: "The name of the security group. The name length needs to be between 1 and 255. Changing this value will update the name.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 255),
				},
			},
			"description": schema.StringAttribute{
				// 内部的には空文字が標準値として設定されるが、任意の値も設定できるため、Optional かつ Computed な arttribute とする
				// 文字数の下限は 0 文字（空文字で更新可能なため）、上限は 255 文字
				MarkdownDescription: "The description of the security group. The description length needs to be between 0 and 255. Changing this value will update the description.",
				Computed:            true,
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(0, 255),
				},
			},
		},
	}
}

func (r *SecurityGroupResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *SecurityGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan SecurityGroupResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	createOpts := groups.CreateOpts{
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueString(),
	}

	tflog.Debug(ctx, "Starting security group creation request.", map[string]any{
		"name":        plan.Name.ValueString(),
		"description": plan.Description.ValueString(),
	})

	createResp, err := groups.Create(ctx, r.client.NetworkClient, createOpts).Extract()
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to create security group resource",
			"An unexpected error occurred while attempting to create security group resource.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "Security group creation request completed.", map[string]any{"id": plan.ID.ValueString()})

	plan.ID = types.StringValue(createResp.ID)
	plan.Description = types.StringValue(createResp.Description)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SecurityGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state SecurityGroupResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	tflog.Debug(ctx, "Starting security group read request.", map[string]any{})

	readResp, err := groups.Get(ctx, r.client.NetworkClient, id).Extract()

	if err != nil {
		if gophercloud.ResponseCodeIs(err, 404) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to read security group resource",
			"An unexpected error occurred while attempting to read security group resource.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "Security group read request completed.", map[string]any{})

	state.Name = types.StringValue(readResp.Name)
	state.Description = types.StringValue(readResp.Description)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *SecurityGroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state SecurityGroupResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	updateOpts := groups.UpdateOpts{}

	if !plan.Name.Equal(state.Name) {
		updateOpts.Name = plan.Name.ValueStringPointer()
	}

	if !plan.Description.Equal(state.Description) {
		updateOpts.Description = plan.Description.ValueStringPointer()
	}

	id := plan.ID.ValueString()

	tflog.Debug(ctx, "Starting security group update request.", map[string]any{
		"name":        plan.Name.ValueString(),
		"description": plan.Description.ValueString(),
	})

	group, err := groups.Update(ctx, r.client.NetworkClient, id, updateOpts).Extract()

	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to update security group resource",
			"An unexpected error occurred while attempting to update security group resource.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	plan.Name = types.StringValue(group.Name)
	plan.Description = types.StringValue(group.Description)

	tflog.Debug(ctx, "Security group updated request completed.", map[string]any{})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SecurityGroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state SecurityGroupResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	tflog.Debug(ctx, "Starting security group deletion request.", map[string]any{
		"id": state.ID.ValueString(),
	})

	err := groups.Delete(ctx, r.client.NetworkClient, id).ExtractErr()

	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to delete security group resource",
			"An unexpected error occurred while attempting to delete security group resource.\n\n"+
				"Error: "+err.Error(),
		)
		return
	}

	tflog.Debug(ctx, "Security group deleted request completed.", map[string]any{
		"id": state.ID.ValueString(),
	})
}

func (r *SecurityGroupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
