package provider

import (
	"context"
	"fmt"
	"maps"
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

	"github.com/technicallyJack/terraform-provider-kemp/internal/client"
)

var (
	_ resource.Resource                   = &subVirtualServiceResource{}
	_ resource.ResourceWithConfigure      = &subVirtualServiceResource{}
	_ resource.ResourceWithImportState    = &subVirtualServiceResource{}
	_ resource.ResourceWithValidateConfig = &subVirtualServiceResource{}
	_ resource.ResourceWithUpgradeState   = &subVirtualServiceResource{}
)

// NewSubVirtualServiceResource returns the kemp_sub_virtual_service resource.
func NewSubVirtualServiceResource() resource.Resource {
	return &subVirtualServiceResource{}
}

type subVirtualServiceResource struct {
	client *client.Client
}

type subVirtualServiceResourceModel struct {
	ID         types.String `tfsdk:"id"`
	Index      types.Int64  `tfsdk:"index"`
	ParentID   types.String `tfsdk:"parent_id"`
	Enabled    types.Bool   `tfsdk:"enabled"`
	Weight     types.Int64  `tfsdk:"weight"`
	Limit      types.Int64  `tfsdk:"limit"`
	MatchRules types.List   `tfsdk:"match_rules"`
	serviceSettingsModel
}

func (r *subVirtualServiceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_sub_virtual_service"
}

func (r *subVirtualServiceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a SubVS: a child virtual service that sits behind a parent virtual service " +
			"and has its own real servers, scheduling and health checks. Attach real servers to it with " +
			"kemp_real_server, using this resource's id as virtual_service_id. A virtual service " +
			"with SubVSs can't also have real servers of its own.",
		Version: 1,
		Attributes: serviceSettingsAttributes(map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				Description: "Stable reference to the SubVS, <parent id>/sub/<slot>, where slot is the SubVS's " +
					"real server index on the parent. Use this to attach real servers.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"index": schema.Int64Attribute{
				Computed: true,
				Description: "The LoadMaster's current virtual service index for the SubVS. Informational only: " +
					"the LoadMaster renumbers virtual services whenever global configuration changes.",
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"parent_id": schema.StringAttribute{
				Required:      true,
				Description:   "id of the parent kemp_virtual_service. Changing this forces a new SubVS.",
				Validators:    []validator.String{vsRefValidator{allowTopLevel: true}},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
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
			"match_rules": ruleListAttribute(matchRuleList,
				"evaluate on the parent for content switching: the parent only sends a request to this SubVS when one matches"),
		}),
	}
}

func (r *subVirtualServiceResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	resp.Diagnostics.Append(validatePersistence(ctx, req.Config)...)
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

	parentRef, err := client.ParseVSRef(plan.ParentID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid parent_id", err.Error())
		return
	}
	parent, err := r.client.ResolveVS(ctx, parentRef)
	if err != nil {
		resp.Diagnostics.AddError("Unable to find the parent virtual service", err.Error())
		return
	}
	slot, err := r.client.CreateSubVirtualService(ctx, parent.Index)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create SubVS", err.Error())
		return
	}

	// Save the reference first so a failure below leaves a tainted resource, not an orphan.
	ref := parentRef
	ref.SubSlot = slot.RsIndex
	plan.ID = types.StringValue(ref.String())
	plan.Index = types.Int64Value(int64(slot.VSIndex))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(r.apply(ctx, &plan)...)
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

	resp.Diagnostics.Append(r.apply(ctx, &plan)...)
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
	ref, diags := r.ref(ctx, state.ID.ValueString())
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	sub, err := r.client.ResolveVS(ctx, ref)
	if client.IsNotFound(err) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to find SubVS", err.Error())
		return
	}
	if err := r.client.DeleteVirtualService(ctx, sub.Index); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete SubVS", err.Error())
	}
}

func (r *subVirtualServiceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Accepts the reference (tcp/10.0.0.1/443/sub/2) or, for convenience,
	// the SubVS's current virtual service index.
	ref, err := client.ParseVSRef(req.ID)
	if err != nil {
		index, convErr := strconv.Atoi(req.ID)
		if convErr != nil {
			resp.Diagnostics.AddError("Invalid import ID",
				fmt.Sprintf("Expected <protocol>/<address>/<port>/sub/<slot> or a SubVS index, got %q.", req.ID))
			return
		}
		if ref, err = r.client.RefForIndex(ctx, index); err != nil {
			resp.Diagnostics.AddError("Unable to find SubVS", err.Error())
			return
		}
	}
	if ref.SubSlot == 0 {
		resp.Diagnostics.AddError("Not a SubVS", fmt.Sprintf("%s is a top-level virtual service; import it as kemp_virtual_service.", ref))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), ref.String())...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("parent_id"), ref.Parent().String())...)
}

// UpgradeState converts state from before stable references (version 0),
// which identified the parent by index in parent_index. See legacyPrefix.
func (r *subVirtualServiceResource) UpgradeState(ctx context.Context) map[int64]resource.StateUpgrader {
	var current resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &current)
	attrs := maps.Clone(current.Schema.Attributes)
	delete(attrs, "parent_id")
	attrs["parent_index"] = schema.Int64Attribute{Required: true}
	prior := schema.Schema{Attributes: attrs}

	return map[int64]resource.StateUpgrader{
		0: {
			PriorSchema: &prior,
			StateUpgrader: func(ctx context.Context, req resource.UpgradeStateRequest, resp *resource.UpgradeStateResponse) {
				var old subVirtualServiceModelV0
				resp.Diagnostics.Append(req.State.Get(ctx, &old)...)
				if resp.Diagnostics.HasError() {
					return
				}
				upgraded := subVirtualServiceResourceModel{
					ID:                   types.StringValue(legacyValue(old.Index.ValueInt64())),
					Index:                old.Index,
					ParentID:             types.StringValue(legacyValue(old.ParentIndex.ValueInt64())),
					Enabled:              old.Enabled,
					Weight:               old.Weight,
					Limit:                old.Limit,
					MatchRules:           old.MatchRules,
					serviceSettingsModel: old.serviceSettingsModel,
				}
				resp.Diagnostics.Append(resp.State.Set(ctx, &upgraded)...)
			},
		},
	}
}

type subVirtualServiceModelV0 struct {
	ID          types.String `tfsdk:"id"`
	Index       types.Int64  `tfsdk:"index"`
	ParentIndex types.Int64  `tfsdk:"parent_index"`
	Enabled     types.Bool   `tfsdk:"enabled"`
	Weight      types.Int64  `tfsdk:"weight"`
	Limit       types.Int64  `tfsdk:"limit"`
	MatchRules  types.List   `tfsdk:"match_rules"`
	serviceSettingsModel
}

// apply pushes the SubVS's own settings (modvs) and its parent-side slot
// settings (modrs on the parent), then refreshes the model. Indexes are
// looked up from the reference immediately before use.
func (r *subVirtualServiceResource) apply(ctx context.Context, m *subVirtualServiceResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics
	ref, err := client.ParseVSRef(m.ID.ValueString())
	if err != nil {
		diags.AddError("Invalid SubVS id", err.Error())
		return diags
	}
	sub, err := r.client.ResolveVS(ctx, ref)
	if err != nil {
		diags.AddError("Unable to find SubVS", err.Error())
		return diags
	}
	parentIndex := sub.MasterVSID

	// modvs rejects Enable for SubVSs; it's part of the parent-side slot.
	vs, err := r.client.UpdateVirtualService(ctx, sub.Index, m.serviceSettingsModel.params())
	if err != nil {
		diags.AddError("Unable to update SubVS", err.Error())
		return diags
	}
	if err := client.CheckSame(ref, vs, parentIndex); err != nil {
		diags.AddError("SubVS changed unexpectedly", err.Error())
		return diags
	}

	slot, err := r.client.GetSubVSSlot(ctx, parentIndex, sub.Index)
	if err != nil {
		diags.AddError("Unable to read SubVS settings from the parent virtual service", err.Error())
		return diags
	}
	diags.Append(setServiceRules(ctx, r.client, sub.Index, vs, &m.serviceSettingsModel)...)
	diags.Append(setRuleList(ctx, r.client, matchRuleList, client.RSMatchRules(parentIndex, slot.RsIndex), slot.MatchRules, m.MatchRules)...)
	if diags.HasError() {
		return diags
	}

	weight, limit := int(m.Weight.ValueInt64()), int(m.Limit.ValueInt64())
	err = r.client.UpdateSubVSSlot(ctx, parentIndex, slot.RsIndex, client.SubVSSlotParams{
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
		diags.AddError("SubVS disappeared", fmt.Sprintf("SubVS %s was not found after updating it.", ref))
	}
	return diags
}

// refresh reads the SubVS and its parent-side slot into m. It returns false
// if the SubVS no longer exists.
func (r *subVirtualServiceResource) refresh(ctx context.Context, m *subVirtualServiceResourceModel) (bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	if _, legacy := legacyIndex(m.ID.ValueString()); legacy {
		// A legacy index whose virtual service is gone means the SubVS is gone.
		if _, err := r.client.GetVirtualService(ctx, mustLegacyIndex(m.ID.ValueString())); client.IsNotFound(err) {
			return false, diags
		}
	}
	ref, d := r.ref(ctx, m.ID.ValueString())
	diags.Append(d...)
	if diags.HasError() {
		return false, diags
	}

	vs, err := r.client.ResolveVS(ctx, ref)
	if client.IsNotFound(err) {
		return false, diags
	}
	if err != nil {
		diags.AddError("Unable to read SubVS", err.Error())
		return false, diags
	}
	slot, err := r.client.GetSubVSSlot(ctx, vs.MasterVSID, vs.Index)
	if client.IsNotFound(err) {
		return false, diags
	}
	if err != nil {
		diags.AddError("Unable to read SubVS settings from the parent virtual service", err.Error())
		return false, diags
	}

	m.ID = types.StringValue(ref.String())
	m.Index = types.Int64Value(int64(vs.Index))
	m.ParentID = types.StringValue(ref.Parent().String())
	m.Enabled = types.BoolValue(slot.Enable)
	m.Weight = types.Int64Value(int64(slot.Weight))
	m.Limit = types.Int64Value(int64(slot.Limit))
	m.MatchRules = stringList(slot.MatchRules)
	m.serviceSettingsModel.fromAPI(vs)
	return true, diags
}

// ref parses a SubVS id, converting a legacy one (see legacyPrefix) by index.
func (r *subVirtualServiceResource) ref(ctx context.Context, id string) (client.VSRef, diag.Diagnostics) {
	var diags diag.Diagnostics
	if index, ok := legacyIndex(id); ok {
		ref, err := r.client.RefForIndex(ctx, index)
		if err != nil {
			diags.AddError("Unable to convert SubVS state to a stable reference", err.Error())
			return client.VSRef{}, diags
		}
		if ref.SubSlot == 0 {
			diags.AddError("Not a SubVS", fmt.Sprintf("Virtual service %d is a top-level virtual service; manage it with kemp_virtual_service.", index))
		}
		return ref, diags
	}
	ref, err := client.ParseVSRef(id)
	if err != nil {
		diags.AddError("Invalid SubVS id", err.Error())
	}
	return ref, diags
}

func mustLegacyIndex(s string) int {
	n, _ := legacyIndex(s)
	return n
}
