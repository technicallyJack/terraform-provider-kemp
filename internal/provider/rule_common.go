package provider

import (
	"context"
	"fmt"
	"regexp"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
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

// The LoadMaster rejects other characters with a misleading "Cannot read
// rules" error.
var ruleNameRegexp = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

var ruleNameValidator = stringvalidator.RegexMatches(ruleNameRegexp,
	"may only contain letters, digits and underscores")

var ruleTypeNames = map[int]string{
	client.RuleTypeMatch:         "match rule",
	client.RuleTypeAddHeader:     "add header rule",
	client.RuleTypeDeleteHeader:  "delete header rule",
	client.RuleTypeReplaceHeader: "replace header rule",
	client.RuleTypeModifyURL:     "modify URL rule",
	client.RuleTypeReplaceBody:   "replace body rule",
}

func ruleNameAttribute() schema.Attribute {
	return schema.StringAttribute{
		Required:      true,
		Description:   "Rule name: letters, digits and underscores. Changing this forces a new rule.",
		Validators:    []validator.String{ruleNameValidator},
		PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
	}
}

func ruleIDAttribute() schema.Attribute {
	return schema.StringAttribute{
		Computed:      true,
		Description:   "The rule name.",
		PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
	}
}

// flagAttribute is an optional rule flag, 1-9. The LoadMaster drops values
// of 10 and above without an error.
func flagAttribute(description string) schema.Attribute {
	return schema.Int64Attribute{
		Optional:    true,
		Description: description + " Flags are 1-9.",
		Validators:  []validator.Int64{int64validator.Between(1, 9)},
	}
}

func flagValue(v types.Int64) int {
	if v.IsNull() {
		return 0
	}
	return int(v.ValueInt64())
}

func flagFromAPI(f int) types.Int64 {
	if f == 0 {
		return types.Int64Null()
	}
	return types.Int64Value(int64(f))
}

// ruleResourceBase holds what the rule resources share: the client, delete
// and import by name.
type ruleResourceBase struct {
	client *client.Client
}

func (r *ruleResourceBase) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// readRule fetches a rule and checks it is one of the wanted types. It
// returns nil with no error if the rule no longer exists.
func (r *ruleResourceBase) readRule(ctx context.Context, name string, kind string, types ...int) (*client.Rule, diag.Diagnostics) {
	var diags diag.Diagnostics
	rule, err := r.client.GetRule(ctx, name)
	if client.IsNotFound(err) {
		return nil, diags
	}
	if err != nil {
		diags.AddError("Unable to read rule", err.Error())
		return nil, diags
	}
	if !slices.Contains(types, rule.Type) {
		diags.AddError("Wrong rule type",
			fmt.Sprintf("Rule %q is a %s on the LoadMaster, not a %s.", name, ruleTypeNames[rule.Type], kind))
		return nil, diags
	}
	return rule, diags
}

func (r *ruleResourceBase) deleteRule(ctx context.Context, name string, diags *diag.Diagnostics) {
	// Deleting a rule also detaches it from every virtual service and real
	// server; their rule lists pick that up on the next refresh.
	if err := r.client.DeleteRule(ctx, name); err != nil && !client.IsNotFound(err) {
		diags.AddError("Unable to delete rule", err.Error())
	}
}

func importRuleByName(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if !ruleNameRegexp.MatchString(req.ID) {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Expected a rule name, got %q.", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
}
