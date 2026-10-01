package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"gitea.jacksonwinkler.com/jackson.winkler/terraform-provider-kemp/internal/client"
)

var (
	_ resource.Resource                = &urlRuleResource{}
	_ resource.ResourceWithConfigure   = &urlRuleResource{}
	_ resource.ResourceWithImportState = &urlRuleResource{}
)

// NewURLRuleResource returns the kemp_url_rule resource.
func NewURLRuleResource() resource.Resource {
	return &urlRuleResource{}
}

type urlRuleResource struct {
	ruleResourceBase
}

type urlRuleModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Pattern     types.String `tfsdk:"pattern"`
	Replacement types.String `tfsdk:"replacement"`
}

func (r *urlRuleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_url_rule"
}

func (r *urlRuleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a URL modification rule that rewrites the request URL. Attach it to a " +
			"virtual service's request_rules.",
		Attributes: map[string]schema.Attribute{
			"id":   ruleIDAttribute(),
			"name": ruleNameAttribute(),
			"pattern": schema.StringAttribute{
				Required:    true,
				Description: "Regular expression matched against the URL.",
			},
			"replacement": schema.StringAttribute{
				Required:    true,
				Description: "Replacement for the matched part of the URL.",
			},
		},
	}
}

func (r *urlRuleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan urlRuleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.CreateRule(ctx, plan.Name.ValueString(), plan.params()); err != nil {
		resp.Diagnostics.AddError("Unable to create URL rule", err.Error())
		return
	}
	r.refresh(ctx, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *urlRuleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state urlRuleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !r.refresh(ctx, &state, &resp.Diagnostics) {
		if !resp.Diagnostics.HasError() {
			resp.State.RemoveResource(ctx)
		}
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *urlRuleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan urlRuleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.UpdateRule(ctx, plan.Name.ValueString(), plan.params()); err != nil {
		resp.Diagnostics.AddError("Unable to update URL rule", err.Error())
		return
	}
	r.refresh(ctx, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *urlRuleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state urlRuleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if !resp.Diagnostics.HasError() {
		r.deleteRule(ctx, state.Name.ValueString(), &resp.Diagnostics)
	}
}

func (r *urlRuleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importRuleByName(ctx, req, resp)
}

func (r *urlRuleResource) refresh(ctx context.Context, m *urlRuleModel, diags *diag.Diagnostics) bool {
	rule, d := r.readRule(ctx, m.Name.ValueString(), "URL rule", client.RuleTypeModifyURL)
	diags.Append(d...)
	if rule == nil {
		return false
	}
	m.ID = types.StringValue(rule.Name)
	m.Pattern = types.StringValue(rule.Pattern)
	m.Replacement = types.StringValue(rule.Replacement)
	return true
}

func (m *urlRuleModel) params() client.RuleParams {
	return client.RuleParams{
		Type:        client.RuleTypeModifyURL,
		Pattern:     m.Pattern.ValueString(),
		Replacement: m.Replacement.ValueString(),
	}
}
