package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/technicallyJack/terraform-provider-kemp/internal/client"
)

var (
	_ resource.Resource                = &matchRuleResource{}
	_ resource.ResourceWithConfigure   = &matchRuleResource{}
	_ resource.ResourceWithImportState = &matchRuleResource{}
)

// NewMatchRuleResource returns the kemp_match_rule resource.
func NewMatchRuleResource() resource.Resource {
	return &matchRuleResource{}
}

type matchRuleResource struct {
	ruleResourceBase
}

type matchRuleModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	Pattern         types.String `tfsdk:"pattern"`
	MatchType       types.String `tfsdk:"match_type"`
	Header          types.String `tfsdk:"header"`
	CaseInsensitive types.Bool   `tfsdk:"case_insensitive"`
	Negate          types.Bool   `tfsdk:"negate"`
	IncludeHost     types.Bool   `tfsdk:"include_host"`
	IncludeQuery    types.Bool   `tfsdk:"include_query"`
	FailOnMatch     types.Bool   `tfsdk:"fail_on_match"`
	SetFlag         types.Int64  `tfsdk:"set_flag"`
	OnlyIfFlag      types.Int64  `tfsdk:"only_if_flag"`
	OnlyIfNotFlag   types.Int64  `tfsdk:"only_if_not_flag"`
}

func (r *matchRuleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_match_rule"
}

func (r *matchRuleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	boolAttr := func(desc string) schema.Attribute {
		return schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), Description: desc}
	}
	resp.Schema = schema.Schema{
		Description: "Manages a content matching rule. Attach it to a real server or SubVS (match_rules) for " +
			"content switching, or to a virtual service's pre_process_rules to set flags for later rules.",
		Attributes: map[string]schema.Attribute{
			"id":   ruleIDAttribute(),
			"name": ruleNameAttribute(),
			"pattern": schema.StringAttribute{
				Required:    true,
				Description: "Pattern to match against the request URL, or against the header named by header.",
			},
			"match_type": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("regex"),
				Description: "How pattern is matched: regex, prefix or postfix. Defaults to regex.",
				Validators:  []validator.String{stringvalidator.OneOf("regex", "prefix", "postfix")},
			},
			"header": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
				Description: "Request header to match instead of the URL. Empty (the default) matches the URL.",
			},
			"case_insensitive": boolAttr("Match without regard to case."),
			"negate":           boolAttr("Invert the match: the rule matches when pattern does not."),
			"include_host":     boolAttr("Prepend the Host header to the URL before matching."),
			"include_query":    boolAttr("Include the query string in the URL being matched."),
			"fail_on_match":    boolAttr("Reject the connection when the rule matches."),
			"set_flag":         flagAttribute("Flag to set when the rule matches, for later rules to check."),
			"only_if_flag":     flagAttribute("Only evaluate this rule if this flag is set."),
			"only_if_not_flag": flagAttribute("Only evaluate this rule if this flag is not set."),
		},
	}
}

func (r *matchRuleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan matchRuleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.CreateRule(ctx, plan.Name.ValueString(), plan.params()); err != nil {
		resp.Diagnostics.AddError("Unable to create match rule", err.Error())
		return
	}
	r.refresh(ctx, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *matchRuleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state matchRuleModel
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

func (r *matchRuleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan matchRuleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.UpdateRule(ctx, plan.Name.ValueString(), plan.params()); err != nil {
		resp.Diagnostics.AddError("Unable to update match rule", err.Error())
		return
	}
	r.refresh(ctx, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *matchRuleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state matchRuleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if !resp.Diagnostics.HasError() {
		r.deleteRule(ctx, state.Name.ValueString(), &resp.Diagnostics)
	}
}

func (r *matchRuleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importRuleByName(ctx, req, resp)
}

// refresh reads the rule into m. It returns false if the rule is gone or
// couldn't be read (diags says which).
func (r *matchRuleResource) refresh(ctx context.Context, m *matchRuleModel, diags *diag.Diagnostics) bool {
	rule, d := r.readRule(ctx, m.Name.ValueString(), "match rule", client.RuleTypeMatch)
	diags.Append(d...)
	if rule == nil {
		return false
	}
	m.ID = types.StringValue(rule.Name)
	m.Pattern = types.StringValue(rule.Pattern)
	m.MatchType = types.StringValue(strings.ToLower(rule.MatchType)) // API reports regex as "Regex"
	m.Header = types.StringValue(rule.Header)
	m.CaseInsensitive = types.BoolValue(rule.CaseIndependent)
	m.Negate = types.BoolValue(rule.Negate)
	m.IncludeHost = types.BoolValue(rule.AddHost)
	m.IncludeQuery = types.BoolValue(rule.IncludeQuery)
	m.FailOnMatch = types.BoolValue(rule.MustFail)
	m.SetFlag = flagFromAPI(rule.SetFlagOnMatch)
	m.OnlyIfFlag = flagFromAPI(rule.OnlyOnFlag)
	m.OnlyIfNotFlag = flagFromAPI(rule.OnlyOnNoFlag)
	return true
}

func (m *matchRuleModel) params() client.RuleParams {
	return client.RuleParams{
		Type:            client.RuleTypeMatch,
		Pattern:         m.Pattern.ValueString(),
		MatchType:       m.MatchType.ValueString(),
		Header:          m.Header.ValueString(),
		CaseIndependent: m.CaseInsensitive.ValueBool(),
		Negate:          m.Negate.ValueBool(),
		AddHost:         m.IncludeHost.ValueBool(),
		IncludeQuery:    m.IncludeQuery.ValueBool(),
		MustFail:        m.FailOnMatch.ValueBool(),
		SetFlagOnMatch:  flagValue(m.SetFlag),
		OnlyOnFlag:      flagValue(m.OnlyIfFlag),
		OnlyOnNoFlag:    flagValue(m.OnlyIfNotFlag),
	}
}
