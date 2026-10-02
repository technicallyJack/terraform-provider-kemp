package provider

import (
	"context"

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
	_ resource.Resource                   = &intermediateCertificateResource{}
	_ resource.ResourceWithConfigure      = &intermediateCertificateResource{}
	_ resource.ResourceWithImportState    = &intermediateCertificateResource{}
	_ resource.ResourceWithModifyPlan     = &intermediateCertificateResource{}
	_ resource.ResourceWithValidateConfig = &intermediateCertificateResource{}
)

// NewIntermediateCertificateResource returns the kemp_intermediate_certificate resource.
func NewIntermediateCertificateResource() resource.Resource {
	return &intermediateCertificateResource{}
}

type intermediateCertificateResource struct {
	ruleResourceBase // for Configure
}

type intermediateCertificateModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Certificate types.String `tfsdk:"certificate"`
	Subject     types.String `tfsdk:"subject"`
	NotAfter    types.String `tfsdk:"not_after"`
}

func (r *intermediateCertificateResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_intermediate_certificate"
}

func (r *intermediateCertificateResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an intermediate (or CA) certificate on the LoadMaster. The LoadMaster sends " +
			"intermediates along with server certificates to complete the chain, and also trusts them as CAs " +
			"for client certificate authentication. They can't be replaced in place, so changing one deletes " +
			"and re-adds it.",
		Attributes: map[string]schema.Attribute{
			"id": ruleIDAttribute(),
			"name": schema.StringAttribute{
				Required:      true,
				Description:   "Name: letters, digits, underscores, hyphens and dots. Changing this forces a new intermediate.",
				Validators:    []validator.String{certNameValidator},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"certificate": schema.StringAttribute{
				Required:      true,
				Description:   "One certificate in PEM. Changing it forces a new intermediate.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"subject":   schema.StringAttribute{Computed: true, Description: "The certificate's subject."},
			"not_after": schema.StringAttribute{Computed: true, Description: "When the certificate expires (RFC 3339, UTC)."},
		},
	}
}

func (r *intermediateCertificateResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cert types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("certificate"), &cert)...)
	if cert.IsNull() || cert.IsUnknown() {
		return
	}
	if _, err := parseSingleCert(cert.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("certificate"), "Invalid certificate",
			err.Error()+" (one intermediate per resource).")
	}
}

func (r *intermediateCertificateResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	var plan intermediateCertificateModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || plan.Certificate.IsUnknown() {
		return
	}
	plan.fill()
	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
}

func (r *intermediateCertificateResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan intermediateCertificateModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.AddIntermediate(ctx, plan.Name.ValueString(), plan.Certificate.ValueString()); err != nil {
		resp.Diagnostics.AddError("Unable to install intermediate certificate", err.Error())
		return
	}
	plan.fill()
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *intermediateCertificateResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state intermediateCertificateModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	remote, err := r.client.ReadIntermediate(ctx, state.Name.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read intermediate certificate", err.Error())
		return
	}
	if !sameCert(state.Certificate.ValueString(), remote) {
		state.Certificate = types.StringValue(normalizePEM(remote))
	}
	state.fill()
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update only runs for computed attributes; every input forces replacement.
func (r *intermediateCertificateResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan intermediateCertificateModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	plan.fill()
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *intermediateCertificateResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state intermediateCertificateModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteIntermediate(ctx, state.Name.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete intermediate certificate", err.Error())
	}
}

func (r *intermediateCertificateResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

func (m *intermediateCertificateModel) fill() {
	info := certInfoFrom(m.Certificate.ValueString())
	m.ID, m.Subject, m.NotAfter = m.Name, info.Subject, info.NotAfter
}
