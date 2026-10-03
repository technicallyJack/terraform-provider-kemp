package provider

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
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

	Transparent       types.Bool   `tfsdk:"transparent"`
	SubnetOriginating types.Bool   `tfsdk:"subnet_originating"`
	ForwardedHeaders  types.String `tfsdk:"forwarded_headers"`
	IdleTimeout       types.Int64  `tfsdk:"idle_timeout"`
	Cache             types.Bool   `tfsdk:"cache"`
	Compress          types.Bool   `tfsdk:"compress"`
	ForceL7           types.Bool   `tfsdk:"force_l7"`

	Persistence *persistenceModel `tfsdk:"persistence"`

	RequestRules    types.List `tfsdk:"request_rules"`
	ResponseRules   types.List `tfsdk:"response_rules"`
	PreProcessRules types.List `tfsdk:"pre_process_rules"`
}

// forwardedHeaderModes are the AddVia values 0-6, in order. Verified with a
// header-echoing real server on 7.2.63 (legacy added nothing in that setup).
var forwardedHeaderModes = []string{
	"legacy",
	"x_forwarded_for_and_via",
	"none",
	"x_clientside_and_via",
	"x_clientside",
	"x_forwarded_for",
	"via",
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
		// No defaults for these two: the LoadMaster's own choice varies (an http
		// service created through the API came out transparent, a gen one not),
		// so left unset they keep whatever the LoadMaster has.
		"transparent": schema.BoolAttribute{
			Optional: true,
			Computed: true,
			Description: "Keep the client's IP address as the source of connections to the real servers. The real " +
				"servers must then route replies back through the LoadMaster; the LoadMaster doesn't use transparency " +
				"for clients on the real server's own network. If unset, the LoadMaster's current setting is kept. " +
				"Adding a SubVS switches its parent to transparent (and subnet_originating off); if these are set " +
				"on the parent, the next apply puts them back.",
			PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
		},
		"subnet_originating": schema.BoolAttribute{
			Optional: true,
			Computed: true,
			Description: "When not transparent, connect to the real servers from the LoadMaster's own address on " +
				"their subnet rather than from the virtual service's address. If unset, the LoadMaster's current " +
				"setting is kept.",
			PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
		},
		"forwarded_headers": schema.StringAttribute{
			Optional: true,
			Computed: true,
			Default:  stringdefault.StaticString("legacy"),
			Description: "Headers carrying the client's address to the real servers (layer 7 services): " +
				"x_forwarded_for, x_forwarded_for_and_via, x_clientside (\"client:port -> vip:port\"), " +
				"x_clientside_and_via, via (Via only), none, or legacy (the LoadMaster's default). Defaults to legacy.",
			Validators: []validator.String{stringvalidator.OneOf(forwardedHeaderModes...)},
		},
		"idle_timeout": schema.Int64Attribute{
			Optional:    true,
			Computed:    true,
			Default:     int64default.StaticInt64(660),
			Description: "Seconds an idle connection is kept open, 1-86400. Defaults to 660.",
			Validators:  []validator.Int64{int64validator.Between(1, 86400)},
		},
		"cache": schema.BoolAttribute{
			Optional:    true,
			Computed:    true,
			Default:     booldefault.StaticBool(false),
			Description: "Cache static content on the LoadMaster (http services). Defaults to false.",
		},
		"compress": schema.BoolAttribute{
			Optional:    true,
			Computed:    true,
			Default:     booldefault.StaticBool(false),
			Description: "Gzip responses for clients that accept it (http services). Defaults to false.",
		},
		"force_l7": schema.BoolAttribute{
			Optional: true,
			Computed: true,
			Default:  booldefault.StaticBool(true),
			Description: "Handle gen services at layer 7. Turning it off makes them layer 4, which is always " +
				"transparent and can't add headers. Other service types are always layer 7. Defaults to true.",
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
	m.Transparent = types.BoolValue(vs.Transparent)
	m.SubnetOriginating = types.BoolValue(vs.SubnetOriginating)
	m.ForwardedHeaders = types.StringValue(fmt.Sprintf("unknown_%d", vs.AddVia))
	if vs.AddVia >= 0 && vs.AddVia < len(forwardedHeaderModes) {
		m.ForwardedHeaders = types.StringValue(forwardedHeaderModes[vs.AddVia])
	}
	m.IdleTimeout = types.Int64Value(int64(vs.Idletime))
	m.Cache = types.BoolValue(vs.Cache)
	m.Compress = types.BoolValue(vs.Compress)
	m.ForceL7 = types.BoolValue(vs.ForceL7)
	m.Persistence = persistenceFromAPI(vs)
	m.RequestRules = stringList(vs.RequestRules)
	m.ResponseRules = stringList(vs.ResponseRules)
	m.PreProcessRules = stringList(vs.PreProcessRules)
}

// params returns the modvs parameters for these settings, except the service
// type: applying a type resets some options (AddVia, Cache), and the
// LoadMaster applies a request's fields in its own order, so the type is only
// ever sent on its own (setTypeFirst, and addvs on create).
func (m *serviceSettingsModel) params() client.VirtualServiceParams {
	checkPort := strconv.FormatInt(m.CheckPort.ValueInt64(), 10)
	checkUseGet := slices.Index(checkMethods, m.CheckMethod.ValueString())
	p := client.VirtualServiceParams{
		NickName:       m.Nickname.ValueStringPointer(),
		Schedule:       m.Schedule.ValueStringPointer(),
		CheckType:      m.CheckType.ValueStringPointer(),
		CheckPort:      &checkPort,
		CheckURL:       m.CheckPath.ValueStringPointer(),
		CheckHost:      m.CheckHost.ValueStringPointer(),
		CheckUseGet:    &checkUseGet,
		CheckUseHTTP11: m.CheckUseHTTP11.ValueBoolPointer(),
	}
	addVia := slices.Index(forwardedHeaderModes, m.ForwardedHeaders.ValueString())
	idle := int(m.IdleTimeout.ValueInt64())
	p.Transparent = knownBool(m.Transparent)
	p.SubnetOriginating = knownBool(m.SubnetOriginating)
	p.AddVia = &addVia
	p.Idletime = &idle
	p.Cache = m.Cache.ValueBoolPointer()
	p.Compress = m.Compress.ValueBoolPointer()
	p.ForceL7 = m.ForceL7.ValueBoolPointer()
	persistenceParams(m.Persistence, &p)
	return p
}

// setTypeFirst changes the service type on its own when it differs from
// current, before the other settings are applied (see params).
func setTypeFirst(ctx context.Context, c *client.Client, current *client.VirtualService, m *serviceSettingsModel) (*client.VirtualService, error) {
	want := m.Type.ValueString()
	if want == "" || want == current.VSType {
		return current, nil
	}
	return c.UpdateVirtualService(ctx, current.Index, client.VirtualServiceParams{VSType: &want})
}

// knownBool is the value to send for an optional, computed setting: nil when
// it isn't known yet, so the LoadMaster's own value is left alone.
func knownBool(b types.Bool) *bool {
	if b.IsNull() || b.IsUnknown() {
		return nil
	}
	v := b.ValueBool()
	return &v
}
