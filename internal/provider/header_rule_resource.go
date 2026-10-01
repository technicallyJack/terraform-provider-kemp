package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"gitea.jacksonwinkler.com/jackson.winkler/terraform-provider-kemp/internal/client"
)

var (
	_ resource.Resource                   = &headerRuleResource{}
	_ resource.ResourceWithConfigure      = &headerRuleResource{}
	_ resource.ResourceWithImportState    = &headerRuleResource{}
	_ resource.ResourceWithValidateConfig = &headerRuleResource{}
)

// NewHeaderRuleResource returns the kemp_header_rule resource.
func NewHeaderRuleResource() resource.Resource {
	return &headerRuleResource{}
}

type headerRuleResource struct {
	ruleResourceBase
}

type headerRuleModel struct {
	ID      types.String `tfsdk:"id"`
	Name    types.String `tfsdk:"name"`
	Action  types.String `tfsdk:"action"`
	Header  types.String `tfsdk:"header"`
	Pattern types.String `tfsdk:"pattern"`
	Value   types.String `tfsdk:"value"`
}

var headerActions = map[string]int{
	"add":     client.RuleTypeAddHeader,
	"delete":  client.RuleTypeDeleteHeader,
	"replace": client.RuleTypeReplaceHeader,
}

func (r *headerRuleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_header_rule"
}

func (r *headerRuleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a header rule that adds, deletes or replaces an HTTP header. Attach it to a " +
			"virtual service's request_rules or response_rules.",
		Attributes: map[string]schema.Attribute{
			"id":   ruleIDAttribute(),
			"name": ruleNameAttribute(),
			"action": schema.StringAttribute{
				Required:    true,
				Description: "add, delete or replace. Can be changed in place.",
				Validators:  []validator.String{stringvalidator.OneOf("add", "delete", "replace")},
			},
			"header": schema.StringAttribute{
				Required:    true,
				Description: "Header to add or modify. For delete, a pattern matching the header names to remove.",
			},
			"pattern": schema.StringAttribute{
				Optional:    true,
				Description: "For replace: the text in the header's value to replace. Required for replace, not used otherwise.",
			},
			"value": schema.StringAttribute{
				Optional:    true,
				Description: "For add: the header value. For replace: the replacement text. Required for both, not used for delete.",
			},
		},
	}
}

func (r *headerRuleResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m headerRuleModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || m.Action.IsUnknown() || m.Action.IsNull() {
		return
	}
	action := m.Action.ValueString()
	needs := map[string][2]bool{ // action: {pattern, value}
		"add":     {false, true},
		"delete":  {false, false},
		"replace": {true, true},
	}[action]
	check := func(attr string, v types.String, required bool) {
		switch {
		case required && v.IsNull():
			resp.Diagnostics.AddAttributeError(path.Root(attr), "Missing header rule setting",
				fmt.Sprintf("action %q requires %s.", action, attr))
		case !required && !v.IsNull() && !v.IsUnknown():
			resp.Diagnostics.AddAttributeError(path.Root(attr), "Unused header rule setting",
				fmt.Sprintf("%s is not used by action %q.", attr, action))
		}
	}
	check("pattern", m.Pattern, needs[0])
	check("value", m.Value, needs[1])
}

func (r *headerRuleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan headerRuleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.CreateRule(ctx, plan.Name.ValueString(), plan.params()); err != nil {
		resp.Diagnostics.AddError("Unable to create header rule", err.Error())
		return
	}
	r.refresh(ctx, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *headerRuleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state headerRuleModel
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

func (r *headerRuleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan headerRuleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// modrule changes the rule's type too, so action changes happen in place.
	if err := r.client.UpdateRule(ctx, plan.Name.ValueString(), plan.params()); err != nil {
		resp.Diagnostics.AddError("Unable to update header rule", err.Error())
		return
	}
	r.refresh(ctx, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *headerRuleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state headerRuleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if !resp.Diagnostics.HasError() {
		r.deleteRule(ctx, state.Name.ValueString(), &resp.Diagnostics)
	}
}

func (r *headerRuleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importRuleByName(ctx, req, resp)
}

func (r *headerRuleResource) refresh(ctx context.Context, m *headerRuleModel, diags *diag.Diagnostics) bool {
	rule, d := r.readRule(ctx, m.Name.ValueString(), "header rule",
		client.RuleTypeAddHeader, client.RuleTypeDeleteHeader, client.RuleTypeReplaceHeader)
	diags.Append(d...)
	if rule == nil {
		return false
	}
	m.ID = types.StringValue(rule.Name)
	m.Pattern, m.Value = types.StringNull(), types.StringNull()
	switch rule.Type {
	case client.RuleTypeAddHeader:
		m.Action = types.StringValue("add")
		m.Header = types.StringValue(rule.Header)
		m.Value = types.StringValue(rule.HeaderValue)
	case client.RuleTypeDeleteHeader:
		m.Action = types.StringValue("delete")
		m.Header = types.StringValue(rule.Pattern) // delete rules keep the header name pattern in Pattern
	case client.RuleTypeReplaceHeader:
		m.Action = types.StringValue("replace")
		m.Header = types.StringValue(rule.Header)
		m.Pattern = types.StringValue(rule.Pattern)
		m.Value = types.StringValue(rule.Replacement)
	}
	return true
}

func (m *headerRuleModel) params() client.RuleParams {
	p := client.RuleParams{Type: headerActions[m.Action.ValueString()]}
	switch p.Type {
	case client.RuleTypeAddHeader:
		p.Header, p.Replacement = m.Header.ValueString(), m.Value.ValueString()
	case client.RuleTypeDeleteHeader:
		p.Pattern = m.Header.ValueString()
	case client.RuleTypeReplaceHeader:
		p.Header, p.Pattern, p.Replacement = m.Header.ValueString(), m.Pattern.ValueString(), m.Value.ValueString()
	}
	return p
}
