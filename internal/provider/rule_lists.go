package provider

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listdefault"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/technicallyJack/terraform-provider-kemp/internal/client"
)

// ruleListKind describes one kind of rule list: which rule types the
// LoadMaster accepts in it. Attaching any other type returns "ok" but does
// nothing, so the provider checks types itself before changing anything.
type ruleListKind struct {
	attr    string
	allowed []int
}

var (
	requestRuleList    = ruleListKind{"request_rules", []int{client.RuleTypeAddHeader, client.RuleTypeDeleteHeader, client.RuleTypeReplaceHeader, client.RuleTypeModifyURL}}
	responseRuleList   = ruleListKind{"response_rules", []int{client.RuleTypeAddHeader, client.RuleTypeDeleteHeader, client.RuleTypeReplaceHeader, client.RuleTypeModifyURL}}
	preProcessRuleList = ruleListKind{"pre_process_rules", []int{client.RuleTypeMatch}}
	bodyRuleList       = ruleListKind{"response_body_rules", []int{client.RuleTypeReplaceBody}}
	matchRuleList      = ruleListKind{"match_rules", []int{client.RuleTypeMatch}}
)

func (k ruleListKind) allowedNames() string {
	var names []string
	for _, t := range k.allowed {
		names = append(names, ruleTypeNames[t]+"s")
	}
	return strings.Join(names, ", ")
}

// ruleListAttribute is an ordered list of rule names. It defaults to empty,
// so rules attached outside Terraform are detached on the next apply, like
// any other setting.
func ruleListAttribute(k ruleListKind, purpose string) schema.Attribute {
	return schema.ListAttribute{
		ElementType: types.StringType,
		Optional:    true,
		Computed:    true,
		Default:     listdefault.StaticValue(types.ListValueMust(types.StringType, []attr.Value{})),
		Description: fmt.Sprintf("Names of the rules to %s, applied in order. Accepts %s. Reference the "+
			"rule resources' name attributes so Terraform creates rules before attaching them. "+
			"Reordering detaches and re-attaches the rules from the first moved one onward, briefly "+
			"leaving them off; adding rules at the end or removing rules causes no gap.",
			purpose, k.allowedNames()),
		Validators: []validator.List{
			listvalidator.UniqueValues(),
			listvalidator.ValueStringsAre(ruleNameValidator),
		},
	}
}

func listStrings(l types.List) []string {
	var out []string
	for _, e := range l.Elements() {
		out = append(out, e.(types.String).ValueString())
	}
	return out
}

func stringList(s []string) types.List {
	vals := make([]attr.Value, len(s))
	for i, v := range s {
		vals[i] = types.StringValue(v)
	}
	return types.ListValueMust(types.StringType, vals)
}

// setRuleList changes an attached rule list from current to desired, after
// checking that every newly attached rule exists and has a type the list
// accepts.
func setRuleList(ctx context.Context, c *client.Client, k ruleListKind, l client.RuleList, current []string, desired types.List) diag.Diagnostics {
	var diags diag.Diagnostics
	want := listStrings(desired)
	for _, name := range want {
		if slices.Contains(current, name) {
			continue // already attached, so its type is fine
		}
		rule, err := c.GetRule(ctx, name)
		if client.IsNotFound(err) {
			diags.AddError("Unknown rule", fmt.Sprintf("%s: rule %q does not exist.", k.attr, name))
			continue
		}
		if err != nil {
			diags.AddError("Unable to read rule", err.Error())
			continue
		}
		if !slices.Contains(k.allowed, rule.Type) {
			diags.AddError("Wrong rule type", fmt.Sprintf(
				"%s: rule %q is a %s, but %s only accepts %s. The LoadMaster would silently ignore it.",
				k.attr, name, ruleTypeNames[rule.Type], k.attr, k.allowedNames()))
		}
	}
	if diags.HasError() {
		return diags
	}
	if err := c.SetRules(ctx, l, current, want); err != nil {
		diags.AddError(fmt.Sprintf("Unable to update %s", k.attr), err.Error())
	}
	return diags
}

// setServiceRules applies the request, response and pre-processing rule
// lists of a virtual service or SubVS, given its current state.
func setServiceRules(ctx context.Context, c *client.Client, index int, current *client.VirtualService, m *serviceSettingsModel) diag.Diagnostics {
	var diags diag.Diagnostics
	diags.Append(setRuleList(ctx, c, requestRuleList, client.VSRequestRules(index), current.RequestRules, m.RequestRules)...)
	diags.Append(setRuleList(ctx, c, responseRuleList, client.VSResponseRules(index), current.ResponseRules, m.ResponseRules)...)
	diags.Append(setRuleList(ctx, c, preProcessRuleList, client.VSPreProcessRules(index), current.PreProcessRules, m.PreProcessRules)...)
	diags.Append(setRuleList(ctx, c, bodyRuleList, client.VSResponseBodyRules(index), current.MatchBodyRules, m.ResponseBodyRules)...)
	return diags
}
