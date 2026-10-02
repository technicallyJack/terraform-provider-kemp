package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/technicallyJack/terraform-provider-kemp/internal/client"
)

var (
	_ resource.Resource                = &cipherSetResource{}
	_ resource.ResourceWithConfigure   = &cipherSetResource{}
	_ resource.ResourceWithImportState = &cipherSetResource{}
)

// NewCipherSetResource returns the kemp_cipher_set resource.
func NewCipherSetResource() resource.Resource {
	return &cipherSetResource{}
}

type cipherSetResource struct {
	ruleResourceBase // for Configure
}

type cipherSetModel struct {
	ID      types.String `tfsdk:"id"`
	Name    types.String `tfsdk:"name"`
	Ciphers types.List   `tfsdk:"ciphers"`
}

func (r *cipherSetResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cipher_set"
}

func (r *cipherSetResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a custom cipher set, for a virtual service's ssl.cipher_set. The built-in sets " +
			"(Default, BestPractices, Intermediate_compatibility, Backward_compatibility, ...) can be used by name " +
			"without this resource, but can't be changed.",
		Attributes: map[string]schema.Attribute{
			"id": ruleIDAttribute(),
			"name": schema.StringAttribute{
				Required:      true,
				Description:   "Cipher set name. Changing this forces a new cipher set.",
				Validators:    []validator.String{certNameValidator},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"ciphers": schema.ListAttribute{
				ElementType: types.StringType,
				Required:    true,
				Description: "OpenSSL cipher names, in order of preference, e.g. ECDHE-RSA-AES256-GCM-SHA384. The " +
					"LoadMaster rejects names it doesn't know.",
				Validators: []validator.List{listvalidator.SizeAtLeast(1), listvalidator.UniqueValues()},
			},
		},
	}
}

func (r *cipherSetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	r.write(ctx, req.Plan.Get, func(m *cipherSetModel) { resp.Diagnostics.Append(resp.State.Set(ctx, m)...) }, &resp.Diagnostics)
}

func (r *cipherSetResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.write(ctx, req.Plan.Get, func(m *cipherSetModel) { resp.Diagnostics.Append(resp.State.Set(ctx, m)...) }, &resp.Diagnostics)
}

func (r *cipherSetResource) write(ctx context.Context, get func(context.Context, any) diag.Diagnostics, save func(*cipherSetModel), diags *diag.Diagnostics) {
	var plan cipherSetModel
	diags.Append(get(ctx, &plan)...)
	if diags.HasError() {
		return
	}
	if err := r.client.SetCipherSet(ctx, plan.Name.ValueString(), listStrings(plan.Ciphers)); err != nil {
		diags.AddError("Unable to save cipher set", err.Error())
		return
	}
	plan.ID = plan.Name
	save(&plan)
}

func (r *cipherSetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state cipherSetModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ciphers, err := r.client.GetCipherSet(ctx, state.Name.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read cipher set", err.Error())
		return
	}
	state.ID = state.Name
	state.Ciphers = stringList(ciphers)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *cipherSetResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state cipherSetModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteCipherSet(ctx, state.Name.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete cipher set", err.Error()+". A cipher set can't be deleted while a virtual service uses it.")
	}
}

func (r *cipherSetResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
