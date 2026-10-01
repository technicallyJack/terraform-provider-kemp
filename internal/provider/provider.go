package provider

import (
	"context"
	"os"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/technicallyJack/terraform-provider-kemp/internal/client"
)

var _ provider.Provider = &kempProvider{}

type kempProvider struct {
	version string
}

type kempProviderModel struct {
	Host     types.String `tfsdk:"host"`
	APIKey   types.String `tfsdk:"api_key"`
	Username types.String `tfsdk:"username"`
	Password types.String `tfsdk:"password"`
	Insecure types.Bool   `tfsdk:"insecure"`

	MaxConcurrentRequests types.Int64 `tfsdk:"max_concurrent_requests"`
}

// New returns a provider factory for the given version.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &kempProvider{version: version}
	}
}

func (p *kempProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "kemp"
	resp.Version = p.version
}

func (p *kempProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manage Kemp LoadMaster appliances via the LoadMaster JSON API.",
		Attributes: map[string]schema.Attribute{
			"host": schema.StringAttribute{
				Description: "LoadMaster hostname or IP (optionally with port). May also be set with KEMP_HOST.",
				Optional:    true,
			},
			"api_key": schema.StringAttribute{
				Description: "LoadMaster API key. May also be set with KEMP_API_KEY. Takes precedence over username/password.",
				Optional:    true,
				Sensitive:   true,
			},
			"username": schema.StringAttribute{
				Description: "LoadMaster username. May also be set with KEMP_USERNAME.",
				Optional:    true,
			},
			"password": schema.StringAttribute{
				Description: "LoadMaster password. May also be set with KEMP_PASSWORD.",
				Optional:    true,
				Sensitive:   true,
			},
			"insecure": schema.BoolAttribute{
				Description: "Skip TLS certificate verification. May also be set with KEMP_INSECURE. Defaults to false.",
				Optional:    true,
			},
			"max_concurrent_requests": schema.Int64Attribute{
				Description: "Maximum simultaneous API calls to the LoadMaster. The free LoadMaster rate-limits " +
					"its API and drops connections at around eight at once; licensed appliances may allow more. " +
					"Requests that fail before reaching the LoadMaster are retried. Defaults to 4.",
				Optional:   true,
				Validators: []validator.Int64{int64validator.Between(1, 32)},
			},
		},
	}
}

func (p *kempProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg kempProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	for attr, v := range map[string]interface{ IsUnknown() bool }{
		"host": cfg.Host, "api_key": cfg.APIKey, "username": cfg.Username,
		"password": cfg.Password, "insecure": cfg.Insecure, "max_concurrent_requests": cfg.MaxConcurrentRequests,
	} {
		if v.IsUnknown() {
			resp.Diagnostics.AddAttributeError(path.Root(attr), "Unknown provider configuration value",
				"The provider cannot be configured with a value that is unknown until apply. Set it statically or via environment variable.")
		}
	}
	if resp.Diagnostics.HasError() {
		return
	}

	host := stringOrEnv(cfg.Host, "KEMP_HOST")
	apiKey := stringOrEnv(cfg.APIKey, "KEMP_API_KEY")
	username := stringOrEnv(cfg.Username, "KEMP_USERNAME")
	password := stringOrEnv(cfg.Password, "KEMP_PASSWORD")

	insecure := false
	if !cfg.Insecure.IsNull() {
		insecure = cfg.Insecure.ValueBool()
	} else if v, err := strconv.ParseBool(os.Getenv("KEMP_INSECURE")); err == nil {
		insecure = v
	}

	if host == "" {
		resp.Diagnostics.AddAttributeError(path.Root("host"), "Missing LoadMaster host",
			"Set the host attribute or the KEMP_HOST environment variable.")
	}
	if apiKey == "" && (username == "" || password == "") {
		resp.Diagnostics.AddError("Missing LoadMaster credentials",
			"Set api_key (KEMP_API_KEY), or both username (KEMP_USERNAME) and password (KEMP_PASSWORD).")
	}
	if resp.Diagnostics.HasError() {
		return
	}

	ctx = tflog.SetField(ctx, "kemp_host", host)
	tflog.Debug(ctx, "Creating LoadMaster client")

	c, err := client.New(client.Config{
		Host:                  host,
		APIKey:                apiKey,
		Username:              username,
		Password:              password,
		Insecure:              insecure,
		MaxConcurrentRequests: int(cfg.MaxConcurrentRequests.ValueInt64()), // 0 (unset) means the default
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to create LoadMaster client", err.Error())
		return
	}

	resp.DataSourceData = c
	resp.ResourceData = c
}

func (p *kempProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewVirtualServiceResource,
		NewRealServerResource,
		NewSubVirtualServiceResource,
		NewMatchRuleResource,
		NewHeaderRuleResource,
		NewURLRuleResource,
	}
}

func (p *kempProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewVirtualServicesDataSource,
	}
}

func stringOrEnv(v types.String, env string) string {
	if !v.IsNull() {
		return v.ValueString()
	}
	return os.Getenv(env)
}
