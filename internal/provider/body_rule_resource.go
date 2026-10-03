package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/technicallyJack/terraform-provider-kemp/internal/client"
)

var (
	_ resource.Resource                = &bodyRuleResource{}
	_ resource.ResourceWithConfigure   = &bodyRuleResource{}
	_ resource.ResourceWithImportState = &bodyRuleResource{}
)

// NewBodyRuleResource returns the kemp_body_rule resource.
func NewBodyRuleResource() resource.Resource {
	return &bodyRuleResource{}
}

type bodyRuleResource struct {
	ruleResourceBase
}

type bodyRuleModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	Pattern         types.String `tfsdk:"pattern"`
	Replacement     types.String `tfsdk:"replacement"`
	CaseInsensitive types.Bool   `tfsdk:"case_insensitive"`
}

func (r *bodyRuleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_body_rule"
}

func (r *bodyRuleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a body rule that replaces text in response bodies. Attach it to a virtual " +
			"service's (or SubVS's) response_body_rules. Each rule matches the original body, so one rule " +
			"can't act on another's replacement.",
		Attributes: map[string]schema.Attribute{
			"id":   ruleIDAttribute(),
			"name": ruleNameAttribute(),
			"pattern": schema.StringAttribute{
				Required:    true,
				Description: "Regular expression matched against the response body.",
			},
			"replacement": schema.StringAttribute{
				Required:    true,
				Description: "Replacement for each match.",
			},
			"case_insensitive": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Match without regard to case. Defaults to false.",
			},
		},
	}
}

func (r *bodyRuleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan bodyRuleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.CreateRule(ctx, plan.Name.ValueString(), plan.params()); err != nil {
		resp.Diagnostics.AddError("Unable to create body rule", err.Error())
		return
	}
	r.refresh(ctx, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *bodyRuleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state bodyRuleModel
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

func (r *bodyRuleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan bodyRuleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.UpdateRule(ctx, plan.Name.ValueString(), plan.params()); err != nil {
		resp.Diagnostics.AddError("Unable to update body rule", err.Error())
		return
	}
	r.refresh(ctx, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *bodyRuleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state bodyRuleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if !resp.Diagnostics.HasError() {
		r.deleteRule(ctx, state.Name.ValueString(), &resp.Diagnostics)
	}
}

func (r *bodyRuleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importRuleByName(ctx, req, resp)
}

func (r *bodyRuleResource) refresh(ctx context.Context, m *bodyRuleModel, diags *diag.Diagnostics) bool {
	rule, d := r.readRule(ctx, m.Name.ValueString(), "body rule", client.RuleTypeReplaceBody)
	diags.Append(d...)
	if rule == nil {
		return false
	}
	m.ID = types.StringValue(rule.Name)
	m.Pattern = types.StringValue(rule.Pattern)
	m.Replacement = types.StringValue(rule.Replacement)
	m.CaseInsensitive = types.BoolValue(rule.CaseIndependent)
	return true
}

func (m *bodyRuleModel) params() client.RuleParams {
	return client.RuleParams{
		Type:            client.RuleTypeReplaceBody,
		Pattern:         m.Pattern.ValueString(),
		Replacement:     m.Replacement.ValueString(),
		CaseIndependent: m.CaseInsensitive.ValueBool(),
	}
}
