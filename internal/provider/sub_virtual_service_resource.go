package provider

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"gitea.jacksonwinkler.com/jackson.winkler/terraform-provider-kemp/internal/client"
)

var (
	_ resource.Resource                = &subVirtualServiceResource{}
	_ resource.ResourceWithConfigure   = &subVirtualServiceResource{}
	_ resource.ResourceWithImportState = &subVirtualServiceResource{}
)

// NewSubVirtualServiceResource returns the kemp_sub_virtual_service resource.
func NewSubVirtualServiceResource() resource.Resource {
	return &subVirtualServiceResource{}
}

type subVirtualServiceResource struct {
	client *client.Client
}

type subVirtualServiceResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Index       types.Int64  `tfsdk:"index"`
	ParentIndex types.Int64  `tfsdk:"parent_index"`
	Enabled     types.Bool   `tfsdk:"enabled"`
	Weight      types.Int64  `tfsdk:"weight"`
	Limit       types.Int64  `tfsdk:"limit"`
	serviceSettingsModel
}

func (r *subVirtualServiceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_sub_virtual_service"
}

func (r *subVirtualServiceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a SubVS: a child virtual service that sits behind a parent virtual service " +
			"and has its own real servers, scheduling and health checks. Attach real servers to it with " +
			"kemp_real_server, using this resource's index as virtual_service_index. A virtual service " +
			"with SubVSs can't also have real servers of its own.",
		Attributes: serviceSettingsAttributes(map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The SubVS's virtual service index, as a string.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"index": schema.Int64Attribute{
				Computed:      true,
				Description:   "LoadMaster-assigned virtual service index of the SubVS.",
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"parent_index": schema.Int64Attribute{
				Required:      true,
				Description:   "Index of the parent virtual service. Changing this forces a new SubVS.",
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()},
			},
			"enabled": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
				Description: "Whether the parent sends traffic to this SubVS. Defaults to true.",
			},
			"weight": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(1000),
				Description: "Relative weight within the parent's weighted scheduling methods. Defaults to 1000.",
				Validators:  []validator.Int64{int64validator.Between(1, 65535)},
			},
			"limit": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(0),
				Description: "Maximum number of open connections the parent sends to this SubVS. 0 means unlimited.",
				Validators:  []validator.Int64{int64validator.AtLeast(0)},
			},
		}),
	}
}

func (r *subVirtualServiceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data type",
			fmt.Sprintf("Expected *client.Client, got %T. Please report this issue to the provider developers.", req.ProviderData))
		return
	}
	r.client = c
}

func (r *subVirtualServiceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan subVirtualServiceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	slot, err := r.client.CreateSubVirtualService(ctx, int(plan.ParentIndex.ValueInt64()))
	if err != nil {
		resp.Diagnostics.AddError("Unable to create SubVS", err.Error())
		return
	}

	// Save the index first so a failure below leaves a tainted resource, not an orphan.
	plan.ID = types.StringValue(strconv.Itoa(slot.VSIndex))
	plan.Index = types.Int64Value(int64(slot.VSIndex))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(r.apply(ctx, &plan, slot.RsIndex)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *subVirtualServiceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state subVirtualServiceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	found, diags := r.refresh(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *subVirtualServiceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state subVirtualServiceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = state.ID
	plan.Index = state.Index

	slot, err := r.client.GetSubVSSlot(ctx, int(plan.ParentIndex.ValueInt64()), int(plan.Index.ValueInt64()))
	if err != nil {
		resp.Diagnostics.AddError("Unable to read SubVS", err.Error())
		return
	}

	resp.Diagnostics.Append(r.apply(ctx, &plan, slot.RsIndex)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *subVirtualServiceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state subVirtualServiceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Deleting the parent removes its SubVSs too.
	err := r.client.DeleteVirtualService(ctx, int(state.Index.ValueInt64()))
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete SubVS", err.Error())
	}
}

func (r *subVirtualServiceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	index, err := strconv.ParseInt(req.ID, 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Expected the SubVS's virtual service index, got %q.", req.ID))
		return
	}
	// parent_index is filled in by Read from the SubVS's MasterVSID.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("index"), index)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// apply pushes the SubVS's own settings (modvs) and its parent-side slot
// settings (modrs on the parent), then refreshes the model.
func (r *subVirtualServiceResource) apply(ctx context.Context, m *subVirtualServiceResourceModel, rsIndex int) diag.Diagnostics {
	var diags diag.Diagnostics
	index, parent := int(m.Index.ValueInt64()), int(m.ParentIndex.ValueInt64())

	// modvs rejects Enable for SubVSs; it's part of the parent-side slot.
	if _, err := r.client.UpdateVirtualService(ctx, index, m.serviceSettingsModel.params()); err != nil {
		diags.AddError("Unable to update SubVS", err.Error())
		return diags
	}

	weight, limit := int(m.Weight.ValueInt64()), int(m.Limit.ValueInt64())
	err := r.client.UpdateSubVSSlot(ctx, parent, rsIndex, client.SubVSSlotParams{
		Weight: &weight,
		Limit:  &limit,
		Enable: m.Enabled.ValueBoolPointer(),
	})
	if err != nil {
		diags.AddError("Unable to update SubVS settings on the parent virtual service", err.Error())
		return diags
	}

	found, d := r.refresh(ctx, m)
	diags.Append(d...)
	if !found && !diags.HasError() {
		diags.AddError("SubVS disappeared", fmt.Sprintf("SubVS %d was not found after updating it.", index))
	}
	return diags
}

// refresh reads the SubVS and its parent-side slot into m. It returns false
// if the SubVS no longer exists.
func (r *subVirtualServiceResource) refresh(ctx context.Context, m *subVirtualServiceResourceModel) (bool, diag.Diagnostics) {
	var diags diag.Diagnostics
	index := int(m.Index.ValueInt64())

	vs, err := r.client.GetVirtualService(ctx, index)
	if client.IsNotFound(err) {
		return false, diags
	}
	if err != nil {
		diags.AddError("Unable to read SubVS", err.Error())
		return false, diags
	}
	if vs.MasterVSID == 0 {
		diags.AddError("Not a SubVS",
			fmt.Sprintf("Virtual service %d is a top-level virtual service; manage it with kemp_virtual_service.", index))
		return false, diags
	}

	slot, err := r.client.GetSubVSSlot(ctx, vs.MasterVSID, index)
	if client.IsNotFound(err) {
		return false, diags
	}
	if err != nil {
		diags.AddError("Unable to read SubVS settings from the parent virtual service", err.Error())
		return false, diags
	}

	m.ID = types.StringValue(strconv.Itoa(index))
	m.ParentIndex = types.Int64Value(int64(vs.MasterVSID))
	m.Enabled = types.BoolValue(slot.Enable)
	m.Weight = types.Int64Value(int64(slot.Weight))
	m.Limit = types.Int64Value(int64(slot.Limit))
	m.serviceSettingsModel.fromAPI(vs)
	return true, diags
}
