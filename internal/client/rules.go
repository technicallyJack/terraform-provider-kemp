package client

import (
	"context"
	"fmt"
	"strconv"
)

// Rule types, as used by addrule/modrule's type parameter.
const (
	RuleTypeMatch         = 0
	RuleTypeAddHeader     = 1
	RuleTypeDeleteHeader  = 2
	RuleTypeReplaceHeader = 3
	RuleTypeModifyURL     = 4
	RuleTypeReplaceBody   = 5
)

// Rule is a content rule as returned by showrule. Which fields are set
// depends on Type.
type Rule struct {
	Type int    `json:"-"` // from the group the rule was listed under
	Name string `json:"Name"`

	Pattern     string `json:"Pattern"`
	Header      string `json:"Header"`
	HeaderValue string `json:"HeaderValue"` // add header rules
	Replacement string `json:"Replacement"` // replace header, modify URL, replace body

	// Match rules.
	MatchType       string `json:"MatchType"` // "Regex", "prefix" or "postfix"
	AddHost         bool   `json:"AddHost"`
	Negate          bool   `json:"Negate"`
	CaseIndependent bool   `json:"CaseIndependent"` // also replace body rules
	IncludeQuery    bool   `json:"IncludeQuery"`
	MustFail        bool   `json:"MustFail"`
	SetFlagOnMatch  int    `json:"SetFlagOnMatch"` // omitted when 0
	OnlyOnFlag      int    `json:"OnlyOnFlag"`
	OnlyOnNoFlag    int    `json:"OnlyOnNoFlag"`
}

// showrule groups rules by type.
type showRuleResponse struct {
	MatchContentRule  []Rule `json:"MatchContentRule"`
	AddHeaderRule     []Rule `json:"AddHeaderRule"`
	DeleteHeaderRule  []Rule `json:"DeleteHeaderRule"`
	ReplaceHeaderRule []Rule `json:"ReplaceHeaderRule"`
	ModifyURLRule     []Rule `json:"ModifyURLRule"`
	ReplaceBodyRule   []Rule `json:"ReplaceBodyRule"`
}

func (r showRuleResponse) all() []Rule {
	var out []Rule
	for typ, group := range [][]Rule{
		RuleTypeMatch:         r.MatchContentRule,
		RuleTypeAddHeader:     r.AddHeaderRule,
		RuleTypeDeleteHeader:  r.DeleteHeaderRule,
		RuleTypeReplaceHeader: r.ReplaceHeaderRule,
		RuleTypeModifyURL:     r.ModifyURLRule,
		RuleTypeReplaceBody:   r.ReplaceBodyRule,
	} {
		for _, rule := range group {
			rule.Type = typ
			out = append(out, rule)
		}
	}
	return out
}

// RuleParams is a complete rule definition for addrule/modrule. modrule
// replaces the whole rule, resetting anything left out, so callers always
// send every field their rule type uses.
type RuleParams struct {
	Type        int
	Pattern     string
	Header      string
	Replacement string

	// Match rules.
	MatchType       string // regex, prefix or postfix
	AddHost         bool
	Negate          bool
	CaseIndependent bool // also replace body rules
	IncludeQuery    bool
	MustFail        bool
	SetFlagOnMatch  int // 0 = none, otherwise 1-9
	OnlyOnFlag      int
	OnlyOnNoFlag    int
}

func boolParam(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func (p RuleParams) toMap(name string) map[string]any {
	m := map[string]any{"name": name, "type": strconv.Itoa(p.Type)}
	switch p.Type {
	case RuleTypeMatch:
		m["pattern"] = p.Pattern
		m["header"] = p.Header
		m["matchtype"] = p.MatchType
		m["inchost"] = boolParam(p.AddHost)
		m["negate"] = boolParam(p.Negate)
		m["nocase"] = boolParam(p.CaseIndependent)
		m["incquery"] = boolParam(p.IncludeQuery)
		m["mustfail"] = boolParam(p.MustFail)
		m["setonmatch"] = strconv.Itoa(p.SetFlagOnMatch)
		m["onlyonflag"] = strconv.Itoa(p.OnlyOnFlag)
		m["onlyonnoflag"] = strconv.Itoa(p.OnlyOnNoFlag)
	case RuleTypeAddHeader:
		m["header"] = p.Header
		m["replacement"] = p.Replacement
	case RuleTypeDeleteHeader:
		m["pattern"] = p.Pattern
	case RuleTypeReplaceHeader, RuleTypeModifyURL, RuleTypeReplaceBody:
		m["pattern"] = p.Pattern
		m["replacement"] = p.Replacement
		if p.Type == RuleTypeReplaceHeader {
			m["header"] = p.Header
		}
		if p.Type == RuleTypeReplaceBody {
			m["nocase"] = boolParam(p.CaseIndependent)
		}
	}
	return m
}

// GetRule returns the named rule. Use IsNotFound to detect a missing rule.
func (c *Client) GetRule(ctx context.Context, name string) (*Rule, error) {
	var out showRuleResponse
	if err := c.Do(ctx, "showrule", map[string]any{"name": name}, &out); err != nil {
		return nil, err
	}
	for _, r := range out.all() {
		if r.Name == name {
			return &r, nil
		}
	}
	return nil, &APIError{Code: 422, Message: fmt.Sprintf("Rule not found: %s", name)}
}

// ListRules returns every rule on the LoadMaster.
func (c *Client) ListRules(ctx context.Context) ([]Rule, error) {
	var out showRuleResponse
	if err := c.Do(ctx, "showrule", nil, &out); err != nil {
		return nil, err
	}
	return out.all(), nil
}

// ruleWrite runs a rule change; rule changes are global writes.
func (c *Client) ruleWrite(ctx context.Context, cmd string, params map[string]any) error {
	return c.globalWrite(ctx, cmd, params)
}

// CreateRule adds a rule.
func (c *Client) CreateRule(ctx context.Context, name string, p RuleParams) error {
	return c.ruleWrite(ctx, "addrule", p.toMap(name))
}

// UpdateRule replaces a rule's definition, including its type.
func (c *Client) UpdateRule(ctx context.Context, name string, p RuleParams) error {
	return c.ruleWrite(ctx, "modrule", p.toMap(name))
}

// DeleteRule deletes a rule, detaching it from every virtual service and
// real server that uses it.
func (c *Client) DeleteRule(ctx context.Context, name string) error {
	return c.ruleWrite(ctx, "delrule", map[string]any{"name": name})
}

// RuleList identifies an ordered list of rules attached to a virtual service
// or real server.
type RuleList struct {
	addCmd, delCmd string
	params         map[string]any
}

// VSRequestRules, VSResponseRules and VSPreProcessRules are the rule lists of
// a virtual service (or SubVS, by its own index).
func VSRequestRules(vs int) RuleList {
	return RuleList{"addrequestrule", "delrequestrule", map[string]any{"vs": strconv.Itoa(vs)}}
}

func VSResponseRules(vs int) RuleList {
	return RuleList{"addresponserule", "delresponserule", map[string]any{"vs": strconv.Itoa(vs)}}
}

// VSResponseBodyRules is a virtual service's list of body replacement rules.
func VSResponseBodyRules(vs int) RuleList {
	return RuleList{"addresponsebodyrule", "delresponsebodyrule", map[string]any{"vs": strconv.Itoa(vs)}}
}

func VSPreProcessRules(vs int) RuleList {
	return RuleList{"addprerule", "delprerule", map[string]any{"vs": strconv.Itoa(vs)}}
}

// RSMatchRules is the content switching rule list of a real server, or of a
// SubVS slot on its parent (rsIndex is then the slot index).
func RSMatchRules(vs, rsIndex int) RuleList {
	return RuleList{"addrsrule", "delrsrule", rsRef(vs, rsIndex)}
}

func (l RuleList) call(ctx context.Context, c *Client, cmd, rule string) error {
	params := map[string]any{"rule": rule}
	for k, v := range l.params {
		params[k] = v
	}
	return c.ruleWrite(ctx, cmd, params)
}

// SetRules changes the rules in l from current to desired, in order. Rules
// run in attachment order and new attachments always go last, so a rule that
// is out of place has to be detached and re-attached along with every rule
// after it. Rules that are only being added at the end, or only removed,
// cause no gap.
func (c *Client) SetRules(ctx context.Context, l RuleList, current, desired []string) error {
	detach, attach := planRuleChanges(current, desired)
	for _, r := range detach {
		if err := l.call(ctx, c, l.delCmd, r); err != nil {
			return fmt.Errorf("detaching rule %s: %w", r, err)
		}
	}
	for _, r := range attach {
		if err := l.call(ctx, c, l.addCmd, r); err != nil {
			return fmt.Errorf("attaching rule %s: %w", r, err)
		}
	}
	return nil
}

// planRuleChanges returns the rules to detach and then attach (in order) to
// turn current into desired with as few changes as possible.
func planRuleChanges(current, desired []string) (detach, attach []string) {
	want := map[string]bool{}
	for _, r := range desired {
		want[r] = true
	}

	// Drop rules that are going away; this never disturbs the others' order.
	var kept []string
	for _, r := range current {
		if want[r] {
			kept = append(kept, r)
		} else {
			detach = append(detach, r)
		}
	}

	// Keep the longest prefix that is already in order; everything after it
	// is detached and re-attached in the desired order.
	p := 0
	for p < len(kept) && p < len(desired) && kept[p] == desired[p] {
		p++
	}
	detach = append(detach, kept[p:]...)
	attach = append(attach, desired[p:]...)
	return detach, attach
}
