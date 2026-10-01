package provider

import (
	"slices"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/technicallyJack/terraform-provider-kemp/internal/client"
)

// serviceSettingsModel holds the settings shared by virtual services and
// SubVSs. It is embedded in both resource models.
type serviceSettingsModel struct {
	Nickname       types.String `tfsdk:"nickname"`
	Type           types.String `tfsdk:"type"`
	Schedule       types.String `tfsdk:"schedule"`
	CheckType      types.String `tfsdk:"check_type"`
	CheckPort      types.Int64  `tfsdk:"check_port"`
	CheckPath      types.String `tfsdk:"check_path"`
	CheckHost      types.String `tfsdk:"check_host"`
	CheckMethod    types.String `tfsdk:"check_method"`
	CheckUseHTTP11 types.Bool   `tfsdk:"check_use_http11"`

	Persistence *persistenceModel `tfsdk:"persistence"`

	RequestRules    types.List `tfsdk:"request_rules"`
	ResponseRules   types.List `tfsdk:"response_rules"`
	PreProcessRules types.List `tfsdk:"pre_process_rules"`
}

// checkMethods maps check_method values to the API's CheckUseGet codes.
var checkMethods = []string{"HEAD", "GET", "POST"}

// serviceSettingsAttributes returns the schema for serviceSettingsModel,
// merged with the resource-specific attributes in extra.
func serviceSettingsAttributes(extra map[string]schema.Attribute) map[string]schema.Attribute {
	attrs := map[string]schema.Attribute{
		"nickname": schema.StringAttribute{
			Optional:    true,
			Computed:    true,
			Default:     stringdefault.StaticString(""),
			Description: "Display name.",
		},
		"type": schema.StringAttribute{
			Optional:    true,
			Computed:    true,
			Default:     stringdefault.StaticString("gen"),
			Description: "Service type: gen, http, http2, ts, tls or log. Defaults to gen.",
			Validators:  []validator.String{stringvalidator.OneOf("gen", "http", "http2", "ts", "tls", "log")},
		},
		"schedule": schema.StringAttribute{
			Optional: true,
			Computed: true,
			Default:  stringdefault.StaticString("rr"),
			Description: "Scheduling method: rr (round robin), wrr (weighted round robin), lc (least connection), " +
				"wlc (weighted least connection), fixed (fixed weighting), sh (source IP hash) or " +
				"dl (weighted response time). Defaults to rr.",
			Validators: []validator.String{stringvalidator.OneOf("rr", "wrr", "lc", "wlc", "fixed", "sh", "dl")},
		},
		"check_type": schema.StringAttribute{
			Optional:    true,
			Computed:    true,
			Default:     stringdefault.StaticString("tcp"),
			Description: "Health check type for the real servers. Defaults to tcp; none disables checks.",
			Validators: []validator.String{stringvalidator.OneOf(
				"tcp", "icmp", "http", "https", "smtp", "nntp", "ftp", "telnet",
				"pop3", "imap", "rdp", "ldap", "bdata", "none",
			)},
		},
		"check_port": schema.Int64Attribute{
			Optional:    true,
			Computed:    true,
			Default:     int64default.StaticInt64(0),
			Description: "Port to health check. 0 (the default) checks each real server on its own port; otherwise 3-65530.",
			Validators: []validator.Int64{int64validator.Any(
				int64validator.OneOf(0),
				int64validator.Between(3, 65530),
			)},
		},
		"check_path": schema.StringAttribute{
			Optional:    true,
			Computed:    true,
			Default:     stringdefault.StaticString(""),
			Description: "URL path requested by http/https health checks, e.g. /healthz.",
		},
		"check_host": schema.StringAttribute{
			Optional:    true,
			Computed:    true,
			Default:     stringdefault.StaticString(""),
			Description: "Host header sent by http/https health checks.",
		},
		"check_method": schema.StringAttribute{
			Optional:    true,
			Computed:    true,
			Default:     stringdefault.StaticString("HEAD"),
			Description: "HTTP method used by http/https health checks: HEAD, GET or POST. Defaults to HEAD.",
			Validators:  []validator.String{stringvalidator.OneOf(checkMethods...)},
		},
		"check_use_http11": schema.BoolAttribute{
			Optional:    true,
			Computed:    true,
			Default:     booldefault.StaticBool(false),
			Description: "Use HTTP/1.1 for http/https health checks. Defaults to false (HTTP/1.0).",
		},
		"persistence":       persistenceAttribute(),
		"request_rules":     ruleListAttribute(requestRuleList, "apply to requests (header changes, URL rewrites)"),
		"response_rules":    ruleListAttribute(responseRuleList, "apply to responses"),
		"pre_process_rules": ruleListAttribute(preProcessRuleList, "evaluate before content switching, typically to set flags"),
	}
	for k, v := range extra {
		attrs[k] = v
	}
	return attrs
}

func (m *serviceSettingsModel) fromAPI(vs *client.VirtualService) {
	m.Nickname = types.StringValue(vs.NickName)
	m.Type = types.StringValue(vs.VSType)
	m.Schedule = types.StringValue(vs.Schedule)
	m.CheckType = types.StringValue(vs.CheckType)
	m.CheckPath = types.StringValue(vs.CheckURL)
	m.CheckHost = types.StringValue(vs.CheckHost)
	m.CheckUseHTTP11 = types.BoolValue(vs.CheckUseHTTP11)

	if port, err := strconv.ParseInt(vs.CheckPort, 10, 64); err == nil {
		m.CheckPort = types.Int64Value(port)
	}
	if vs.CheckUseGet >= 0 && vs.CheckUseGet < len(checkMethods) {
		m.CheckMethod = types.StringValue(checkMethods[vs.CheckUseGet])
	}
	m.Persistence = persistenceFromAPI(vs)
	m.RequestRules = stringList(vs.RequestRules)
	m.ResponseRules = stringList(vs.ResponseRules)
	m.PreProcessRules = stringList(vs.PreProcessRules)
}

// params returns the modvs parameters for these settings.
func (m *serviceSettingsModel) params() client.VirtualServiceParams {
	checkPort := strconv.FormatInt(m.CheckPort.ValueInt64(), 10)
	checkUseGet := slices.Index(checkMethods, m.CheckMethod.ValueString())
	p := client.VirtualServiceParams{
		NickName:       m.Nickname.ValueStringPointer(),
		VSType:         m.Type.ValueStringPointer(),
		Schedule:       m.Schedule.ValueStringPointer(),
		CheckType:      m.CheckType.ValueStringPointer(),
		CheckPort:      &checkPort,
		CheckURL:       m.CheckPath.ValueStringPointer(),
		CheckHost:      m.CheckHost.ValueStringPointer(),
		CheckUseGet:    &checkUseGet,
		CheckUseHTTP11: m.CheckUseHTTP11.ValueBoolPointer(),
	}
	persistenceParams(m.Persistence, &p)
	return p
}
