package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/technicallyJack/terraform-provider-kemp/internal/client"
)

var (
	_ resource.Resource                   = &certificateResource{}
	_ resource.ResourceWithConfigure      = &certificateResource{}
	_ resource.ResourceWithImportState    = &certificateResource{}
	_ resource.ResourceWithModifyPlan     = &certificateResource{}
	_ resource.ResourceWithValidateConfig = &certificateResource{}
)

// NewCertificateResource returns the kemp_certificate resource.
func NewCertificateResource() resource.Resource {
	return &certificateResource{}
}

type certificateResource struct {
	ruleResourceBase // for Configure
}

type certificateModel struct {
	ID                  types.String `tfsdk:"id"`
	Name                types.String `tfsdk:"name"`
	Certificate         types.String `tfsdk:"certificate"`
	PrivateKeyWO        types.String `tfsdk:"private_key_wo"`
	PrivateKeyWOVersion types.Int64  `tfsdk:"private_key_wo_version"`
	AdminCertificate    types.Bool   `tfsdk:"admin_certificate"`
	Subject             types.String `tfsdk:"subject"`
	DNSNames            types.List   `tfsdk:"dns_names"`
	NotAfter            types.String `tfsdk:"not_after"`
	KeyType             types.String `tfsdk:"key_type"`
}

func (r *certificateResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_certificate"
}

func (r *certificateResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a TLS certificate and its private key on the LoadMaster, for virtual services' ssl " +
			"blocks. Changing the certificate replaces it in place, so virtual services using it keep working " +
			"(a renewal needs no virtual service change). The private key is write-only: it is never stored in " +
			"state or plans, so it can come from an ephemeral resource.",
		Attributes: map[string]schema.Attribute{
			"id": ruleIDAttribute(),
			"name": schema.StringAttribute{
				Required:      true,
				Description:   "Certificate name: letters, digits, underscores, hyphens and dots. Changing this forces a new certificate.",
				Validators:    []validator.String{certNameValidator},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"certificate": schema.StringAttribute{
				Required: true,
				Description: "The certificate in PEM, leaf only: the LoadMaster keeps just the first certificate, so " +
					"install the chain with kemp_intermediate_certificate.",
			},
			"private_key_wo": schema.StringAttribute{
				Required:  true,
				WriteOnly: true,
				Sensitive: true,
				Description: "The private key in PEM. Write-only: sent when the certificate is created or replaced, " +
					"never stored. Requires Terraform 1.11 or later.",
			},
			"private_key_wo_version": schema.Int64Attribute{
				Optional: true,
				Description: "Change this to push private_key_wo again without changing the certificate. Terraform " +
					"can't see changes to write-only values, so a new key with the same certificate needs a new version.",
			},
			"admin_certificate": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				Description: "Use this certificate for the LoadMaster's own web UI and API. The LoadMaster drops " +
					"this assignment whenever the certificate is replaced (while still serving the old one from " +
					"memory until a restart), so the provider re-assigns it after every replacement and checks it " +
					"on every refresh. Needs a LoadMaster user with All Permissions. Setting it back to false " +
					"doesn't unassign the certificate. Defaults to false.",
			},
			"subject": schema.StringAttribute{
				Computed:    true,
				Description: "The certificate's subject.",
			},
			"dns_names": schema.ListAttribute{
				ElementType: types.StringType,
				Computed:    true,
				Description: "DNS names from the certificate's subject alternative names.",
			},
			"not_after": schema.StringAttribute{
				Computed:    true,
				Description: "When the certificate expires (RFC 3339, UTC).",
			},
			"key_type": schema.StringAttribute{
				Computed:    true,
				Description: "Key type: RSA or ECC.",
			},
		},
	}
}

func (r *certificateResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cert types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("certificate"), &cert)...)
	if cert.IsNull() || cert.IsUnknown() {
		return
	}
	if _, err := parseSingleCert(cert.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("certificate"), "Invalid certificate", err.Error())
	}
}

// ModifyPlan computes the certificate-derived attributes at plan time, so
// not_after and friends show up in plans.
func (r *certificateResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	var plan certificateModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || plan.Certificate.IsUnknown() {
		return
	}
	info := certInfoFrom(plan.Certificate.ValueString())
	plan.Subject, plan.DNSNames, plan.NotAfter, plan.KeyType = info.Subject, info.DNSNames, info.NotAfter, info.KeyType
	plan.ID = plan.Name
	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
}

func (r *certificateResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan certificateModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	key, d := privateKeyFromConfig(ctx, req.Config.GetAttribute)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.AddCertificate(ctx, plan.Name.ValueString(), plan.Certificate.ValueString(), key, false); err != nil {
		resp.Diagnostics.AddError("Unable to install certificate", err.Error())
		return
	}
	r.assignAdmin(ctx, &plan, &resp.Diagnostics)
	r.save(ctx, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *certificateResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state certificateModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	remote, err := r.client.ReadCertificate(ctx, state.Name.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read certificate", err.Error())
		return
	}
	// Keep the configured formatting unless the certificate itself changed.
	if !sameCert(state.Certificate.ValueString(), remote) {
		state.Certificate = types.StringValue(normalizePEM(remote))
	}
	// Only checked when it's meant to be the admin certificate (or after an
	// import), so a lost assignment shows up as a change to re-apply.
	if state.AdminCertificate.IsNull() || state.AdminCertificate.ValueBool() {
		admin, err := r.client.GetAdminCertificate(ctx)
		if err != nil {
			resp.Diagnostics.AddError("Unable to read the administrative certificate", err.Error())
			return
		}
		state.AdminCertificate = types.BoolValue(admin == state.Name.ValueString())
	}
	r.save(ctx, &state, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *certificateResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state certificateModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Re-sending the certificate is what drops the admin assignment, so only
	// do it when the certificate or key actually changed.
	if !sameCert(plan.Certificate.ValueString(), state.Certificate.ValueString()) ||
		!plan.PrivateKeyWOVersion.Equal(state.PrivateKeyWOVersion) {
		key, d := privateKeyFromConfig(ctx, req.Config.GetAttribute)
		resp.Diagnostics.Append(d...)
		if resp.Diagnostics.HasError() {
			return
		}
		// Replaced in place: virtual services referencing the name are unaffected.
		if err := r.client.AddCertificate(ctx, plan.Name.ValueString(), plan.Certificate.ValueString(), key, true); err != nil {
			resp.Diagnostics.AddError("Unable to replace certificate", err.Error())
			return
		}
	}
	r.assignAdmin(ctx, &plan, &resp.Diagnostics)
	r.save(ctx, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *certificateResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state certificateModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteCertificate(ctx, state.Name.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete certificate",
			err.Error()+". A certificate can't be deleted while a virtual service uses it.")
	}
}

func (r *certificateResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// The private key can't be read back; set private_key_wo in config.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// assignAdmin makes the certificate the administrative one when configured,
// which has to happen after every create or replacement.
func (r *certificateResource) assignAdmin(ctx context.Context, m *certificateModel, diags *diag.Diagnostics) {
	if !m.AdminCertificate.ValueBool() {
		return
	}
	if err := r.client.SetAdminCertificate(ctx, m.Name.ValueString()); err != nil {
		diags.AddError("Unable to assign the administrative certificate",
			err.Error()+". Assigning the administrative certificate needs a LoadMaster user with All Permissions.")
	}
}

// save fills the computed attributes. The write-only key is always null in
// state.
func (r *certificateResource) save(_ context.Context, m *certificateModel, _ *diag.Diagnostics) {
	info := certInfoFrom(m.Certificate.ValueString())
	m.ID = m.Name
	m.Subject, m.DNSNames, m.NotAfter, m.KeyType = info.Subject, info.DNSNames, info.NotAfter, info.KeyType
	m.PrivateKeyWO = types.StringNull()
}

// privateKeyFromConfig reads the write-only key, which is only available from
// the configuration, never from the plan or state.
func privateKeyFromConfig(ctx context.Context, get func(context.Context, path.Path, any) diag.Diagnostics) (string, diag.Diagnostics) {
	var key types.String
	diags := get(ctx, path.Root("private_key_wo"), &key)
	if !diags.HasError() && (key.IsNull() || key.IsUnknown()) {
		diags.AddAttributeError(path.Root("private_key_wo"), "Missing private key",
			"private_key_wo must be known at apply time to install the certificate.")
	}
	return key.ValueString(), diags
}
