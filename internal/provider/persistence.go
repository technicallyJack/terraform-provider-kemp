package provider

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/technicallyJack/terraform-provider-kemp/internal/client"
)

// persistenceModel is the optional persistence block. A null block means
// persistence is off.
type persistenceModel struct {
	Mode           types.String `tfsdk:"mode"`
	Timeout        types.Int64  `tfsdk:"timeout"`
	CookieName     types.String `tfsdk:"cookie_name"`
	HeaderName     types.String `tfsdk:"header_name"`
	QueryParameter types.String `tfsdk:"query_parameter"`
}

var (
	persistenceModes = []string{
		"src", "ssl", "cookie", "active-cookie", "cookie-src", "active-cook-src",
		"cookie-hash", "cookie-hash-src", "url", "query-hash", "host", "header",
		"super", "super-src", "rdp", "rdp-src", "rdp-sb",
	}
	// Modes that use the cookie name, and the subset that requires it.
	cookieModes         = []string{"cookie", "cookie-src", "active-cookie", "active-cook-src", "cookie-hash", "cookie-hash-src"}
	cookieRequiredModes = []string{"cookie", "cookie-src"}
)

func persistenceAttribute() schema.Attribute {
	return schema.SingleNestedAttribute{
		Optional: true,
		Description: "Session persistence. Leave out to turn persistence off. Each mode " +
			"needs at most one of cookie_name, header_name or query_parameter.",
		Attributes: map[string]schema.Attribute{
			"mode": schema.StringAttribute{
				Required: true,
				Description: "Persistence mode: src (source IP), ssl (SSL session ID), cookie (server cookie), " +
					"active-cookie (cookie inserted by the LoadMaster), cookie-src / active-cook-src (cookie, " +
					"falling back to source IP), cookie-hash / cookie-hash-src (hash of a server cookie), " +
					"url (URL hash), query-hash (hash of a query parameter), host (Host header), header " +
					"(hash of a request header), super / super-src (super HTTP), rdp / rdp-src (RDP login) " +
					"or rdp-sb (RDP session broker).",
				Validators: []validator.String{stringvalidator.OneOf(persistenceModes...)},
			},
			"timeout": schema.Int64Attribute{
				Optional: true,
				Computed: true,
				Default:  int64default.StaticInt64(360),
				Description: "Seconds a client stays with the same real server, 60-604800. Defaults to 360. " +
					"(The LoadMaster treats anything under 60 as turning persistence off.)",
				Validators: []validator.Int64{int64validator.Between(60, 604800)},
			},
			"cookie_name": schema.StringAttribute{
				Optional:    true,
				Description: "Cookie to persist on, for the cookie modes. Required for cookie and cookie-src.",
			},
			"header_name": schema.StringAttribute{
				Optional:    true,
				Description: "Request header to hash, for the header mode. Required for header.",
			},
			"query_parameter": schema.StringAttribute{
				Optional:    true,
				Description: "Query string parameter to hash, for the query-hash mode. Required for query-hash.",
			},
		},
	}
}

// validatePersistence checks that the name fields match the mode. Unknown
// values are skipped; they're checked again once known.
func validatePersistence(ctx context.Context, config tfsdk.Config) diag.Diagnostics {
	var diags diag.Diagnostics
	var p *persistenceModel
	diags.Append(config.GetAttribute(ctx, path.Root("persistence"), &p)...)
	if diags.HasError() || p == nil || p.Mode.IsUnknown() || p.Mode.IsNull() {
		return diags
	}
	mode := p.Mode.ValueString()
	root := path.Root("persistence")

	check := func(attr string, v types.String, usedBy []string, requiredBy []string) {
		set := !v.IsNull() && !v.IsUnknown()
		switch {
		case set && !slices.Contains(usedBy, mode):
			diags.AddAttributeError(root.AtName(attr), "Unused persistence setting",
				fmt.Sprintf("%s only applies to the %v modes, not %q.", attr, usedBy, mode))
		case v.IsNull() && slices.Contains(requiredBy, mode):
			diags.AddAttributeError(root.AtName(attr), "Missing persistence setting",
				fmt.Sprintf("Persistence mode %q requires %s.", mode, attr))
		}
	}
	check("cookie_name", p.CookieName, cookieModes, cookieRequiredModes)
	check("header_name", p.HeaderName, []string{"header"}, []string{"header"})
	check("query_parameter", p.QueryParameter, []string{"query-hash"}, []string{"query-hash"})
	return diags
}

// persistenceParams sets the persistence fields of the modvs/addvs request.
// Every write sends all of them, so names left over from a previous mode are
// cleared.
func persistenceParams(p *persistenceModel, params *client.VirtualServiceParams) {
	mode, cookie, queryTag := "none", "", ""
	params.Persist, params.Cookie, params.QueryTag = &mode, &cookie, &queryTag
	if p == nil {
		return // no timeout: see VirtualServiceParams
	}

	mode = p.Mode.ValueString()
	timeout := strconv.FormatInt(p.Timeout.ValueInt64(), 10)
	params.PersistTimeout = &timeout
	switch {
	case !p.CookieName.IsNull():
		cookie = p.CookieName.ValueString()
	case !p.HeaderName.IsNull():
		cookie = p.HeaderName.ValueString()
	}
	if !p.QueryParameter.IsNull() {
		queryTag = p.QueryParameter.ValueString()
	}
}

// persistenceFromAPI builds the block from a showvs response. Names are only
// read for the modes that use them, since the LoadMaster keeps stale values
// after a mode change.
func persistenceFromAPI(vs *client.VirtualService) *persistenceModel {
	if vs.Persist == "" || vs.Persist == "none" {
		return nil
	}
	p := &persistenceModel{
		Mode:           types.StringValue(vs.Persist),
		CookieName:     types.StringNull(),
		HeaderName:     types.StringNull(),
		QueryParameter: types.StringNull(),
	}
	if t, err := strconv.ParseInt(vs.PersistTimeout, 10, 64); err == nil {
		p.Timeout = types.Int64Value(t)
	}
	nonEmpty := func(s string) types.String {
		if s == "" {
			return types.StringNull()
		}
		return types.StringValue(s)
	}
	switch {
	case slices.Contains(cookieModes, vs.Persist):
		p.CookieName = nonEmpty(vs.Cookie)
	case vs.Persist == "header":
		p.HeaderName = nonEmpty(vs.Cookie)
	case vs.Persist == "query-hash":
		p.QueryParameter = nonEmpty(vs.QueryTag)
	}
	return p
}
