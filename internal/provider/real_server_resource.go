package provider

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"gitea.jacksonwinkler.com/jackson.winkler/terraform-provider-kemp/internal/client"
)

var (
	_ resource.Resource                = &realServerResource{}
	_ resource.ResourceWithConfigure   = &realServerResource{}
	_ resource.ResourceWithImportState = &realServerResource{}
)

// NewRealServerResource returns the kemp_real_server resource.
func NewRealServerResource() resource.Resource {
	return &realServerResource{}
}

type realServerResource struct {
	client *client.Client
}

type realServerResourceModel struct {
	ID                  types.String `tfsdk:"id"`
	VirtualServiceIndex types.Int64  `tfsdk:"virtual_service_index"`
	Index               types.Int64  `tfsdk:"index"`
	Address             types.String `tfsdk:"address"`
	Port                types.Int64  `tfsdk:"port"`
	Forward             types.String `tfsdk:"forward"`
	Weight              types.Int64  `tfsdk:"weight"`
	Limit               types.Int64  `tfsdk:"limit"`
	Enabled             types.Bool   `tfsdk:"enabled"`
	MatchRules          types.List   `tfsdk:"match_rules"`
}

func (r *realServerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_real_server"
}

func (r *realServerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a real server (backend) attached to a LoadMaster virtual service.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Identifier in the form `<virtual_service_index>/<index>`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"virtual_service_index": schema.Int64Attribute{
				Required:      true,
				Description:   "Index of the virtual service this real server belongs to. Changing this forces a new real server.",
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()},
			},
			"index": schema.Int64Attribute{
				Computed:      true,
				Description:   "LoadMaster-assigned real server index.",
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"address": schema.StringAttribute{
				Required:      true,
				Description:   "IP address of the real server. Changing this forces a new real server.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"port": schema.Int64Attribute{
				Required:    true,
				Description: "Port on the real server that traffic is forwarded to.",
				Validators:  []validator.Int64{int64validator.Between(1, 65535)},
			},
			"forward": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("nat"),
				Description: "Forwarding method: nat or route (direct server return). Defaults to nat.",
				Validators:  []validator.String{stringvalidator.OneOf("nat", "route")},
			},
			"weight": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(1000),
				Description: "Relative weight for weighted scheduling methods. Defaults to 1000.",
				Validators:  []validator.Int64{int64validator.Between(1, 65535)},
			},
			"limit": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(0),
				Description: "Maximum number of open connections to this real server. 0 means unlimited.",
				Validators:  []validator.Int64{int64validator.AtLeast(0)},
			},
			"enabled": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
				Description: "Whether the real server receives traffic. Defaults to true.",
			},
			"match_rules": ruleListAttribute(matchRuleList,
				"evaluate for content switching: the virtual service only sends a request to this real server when one matches"),
		},
	}
}

func (r *realServerResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *realServerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan realServerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	vsIndex := int(plan.VirtualServiceIndex.ValueInt64())
	rs, err := r.client.CreateRealServer(ctx, vsIndex, plan.Address.ValueString(), int(plan.Port.ValueInt64()))
	if err != nil {
		resp.Diagnostics.AddError("Unable to create real server", err.Error())
		return
	}

	// addrs ignores some attributes, so apply them all with modrs. Save the
	// index first so a failure here leaves a tainted resource, not an orphan.
	plan.ID = types.StringValue(realServerID(vsIndex, rs.RsIndex))
	plan.Index = types.Int64Value(int64(rs.RsIndex))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(r.apply(ctx, &plan, false)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *realServerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state realServerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	rs, err := r.client.GetRealServer(ctx, int(state.VirtualServiceIndex.ValueInt64()), int(state.Index.ValueInt64()))
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read real server", err.Error())
		return
	}

	state.fromAPI(rs)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *realServerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state realServerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.ID = state.ID
	plan.Index = state.Index
	resp.Diagnostics.Append(r.apply(ctx, &plan, !plan.Port.Equal(state.Port))...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *realServerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state realServerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Deleting the parent virtual service removes its real servers too.
	err := r.client.DeleteRealServer(ctx, int(state.VirtualServiceIndex.ValueInt64()), int(state.Index.ValueInt64()))
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete real server", err.Error())
	}
}

func (r *realServerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	vs, rs, ok := strings.Cut(req.ID, "/")
	vsIndex, err1 := strconv.ParseInt(vs, 10, 64)
	rsIndex, err2 := strconv.ParseInt(rs, 10, 64)
	if !ok || err1 != nil || err2 != nil {
		resp.Diagnostics.AddError("Invalid import ID",
			fmt.Sprintf("Expected <virtual_service_index>/<real_server_index>, got %q.", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("virtual_service_index"), vsIndex)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("index"), rsIndex)...)
}

// apply pushes the plan's settable attributes with modrs and refreshes the
// model from the LoadMaster, since modrs silently ignores values it doesn't
// accept. NewPort is only sent when the port changed.
func (r *realServerResource) apply(ctx context.Context, m *realServerResourceModel, portChanged bool) diag.Diagnostics {
	var diags diag.Diagnostics
	vsIndex, rsIndex := int(m.VirtualServiceIndex.ValueInt64()), int(m.Index.ValueInt64())

	weight, limit := int(m.Weight.ValueInt64()), int(m.Limit.ValueInt64())
	params := client.RealServerParams{
		Forward: m.Forward.ValueStringPointer(),
		Weight:  &weight,
		Limit:   &limit,
		Enable:  m.Enabled.ValueBoolPointer(),
	}
	if portChanged {
		port := int(m.Port.ValueInt64())
		params.Port = &port
	}

	if err := r.client.UpdateRealServer(ctx, vsIndex, rsIndex, params); err != nil {
		diags.AddError("Unable to update real server", err.Error())
		return diags
	}

	rs, err := r.client.GetRealServer(ctx, vsIndex, rsIndex)
	if err != nil {
		diags.AddError("Unable to read real server after update", err.Error())
		return diags
	}
	diags.Append(setRuleList(ctx, r.client, matchRuleList, client.RSMatchRules(vsIndex, rsIndex), rs.MatchRules, m.MatchRules)...)
	if diags.HasError() {
		return diags
	}

	rs, err = r.client.GetRealServer(ctx, vsIndex, rsIndex)
	if err != nil {
		diags.AddError("Unable to read real server after update", err.Error())
		return diags
	}
	m.fromAPI(rs)
	return diags
}

func (m *realServerResourceModel) fromAPI(rs *client.RealServer) {
	m.ID = types.StringValue(realServerID(rs.VSIndex, rs.RsIndex))
	m.VirtualServiceIndex = types.Int64Value(int64(rs.VSIndex))
	m.Index = types.Int64Value(int64(rs.RsIndex))
	m.Address = types.StringValue(rs.Addr)
	m.Port = types.Int64Value(int64(rs.Port))
	m.Forward = types.StringValue(rs.Forward)
	m.Weight = types.Int64Value(int64(rs.Weight))
	m.Limit = types.Int64Value(int64(rs.Limit))
	m.Enabled = types.BoolValue(rs.Enable)
	m.MatchRules = stringList(rs.MatchRules)
}

func realServerID(vsIndex, rsIndex int) string {
	return fmt.Sprintf("%d/%d", vsIndex, rsIndex)
}
