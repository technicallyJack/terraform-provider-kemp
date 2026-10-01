package provider

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"gitea.jacksonwinkler.com/jackson.winkler/terraform-provider-kemp/internal/client"
)

var (
	_ resource.Resource                = &virtualServiceResource{}
	_ resource.ResourceWithConfigure   = &virtualServiceResource{}
	_ resource.ResourceWithImportState = &virtualServiceResource{}
)

// NewVirtualServiceResource returns the kemp_virtual_service resource.
func NewVirtualServiceResource() resource.Resource {
	return &virtualServiceResource{}
}

type virtualServiceResource struct {
	client *client.Client
}

type virtualServiceResourceModel struct {
	ID       types.String `tfsdk:"id"`
	Index    types.Int64  `tfsdk:"index"`
	Address  types.String `tfsdk:"address"`
	Port     types.String `tfsdk:"port"`
	Protocol types.String `tfsdk:"protocol"`
	Enabled  types.Bool   `tfsdk:"enabled"`
	serviceSettingsModel
}

func (r *virtualServiceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_virtual_service"
}

func (r *virtualServiceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a LoadMaster virtual service.",
		Attributes: serviceSettingsAttributes(map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The virtual service index, as a string.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"index": schema.Int64Attribute{
				Computed:      true,
				Description:   "LoadMaster-assigned virtual service index.",
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"address": schema.StringAttribute{
				Required:    true,
				Description: "IP address the virtual service listens on.",
			},
			"port": schema.StringAttribute{
				Required:    true,
				Description: "Port the virtual service listens on.",
			},
			"protocol": schema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Default:       stringdefault.StaticString("tcp"),
				Description:   "Protocol: tcp or udp. Changing this forces a new virtual service. Defaults to tcp.",
				Validators:    []validator.String{stringvalidator.OneOf("tcp", "udp")},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"enabled": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
				Description: "Whether the virtual service is enabled. Defaults to true.",
			},
		}),
	}
}

func (r *virtualServiceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *virtualServiceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan virtualServiceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	params := plan.params()
	params.Address = plan.Address.ValueStringPointer()
	params.Port = plan.Port.ValueStringPointer()
	params.Protocol = plan.Protocol.ValueStringPointer()

	vs, err := r.client.CreateVirtualService(ctx, params)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create virtual service", err.Error())
		return
	}

	plan.fromAPI(vs)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *virtualServiceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state virtualServiceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	vs, err := r.client.GetVirtualService(ctx, int(state.Index.ValueInt64()))
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read virtual service", err.Error())
		return
	}

	state.fromAPI(vs)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *virtualServiceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state virtualServiceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	params := plan.params()
	params.Address = plan.Address.ValueStringPointer()
	params.Port = plan.Port.ValueStringPointer()

	vs, err := r.client.UpdateVirtualService(ctx, int(state.Index.ValueInt64()), params)
	if err != nil {
		resp.Diagnostics.AddError("Unable to update virtual service", err.Error())
		return
	}

	plan.fromAPI(vs)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *virtualServiceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state virtualServiceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteVirtualService(ctx, int(state.Index.ValueInt64()))
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete virtual service", err.Error())
	}
}

func (r *virtualServiceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	index, err := strconv.ParseInt(req.ID, 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Expected a virtual service index, got %q.", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("index"), index)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

func (m *virtualServiceResourceModel) fromAPI(vs *client.VirtualService) {
	m.ID = types.StringValue(strconv.Itoa(vs.Index))
	m.Index = types.Int64Value(int64(vs.Index))
	m.Address = types.StringValue(vs.VSAddress)
	m.Port = types.StringValue(vs.VSPort)
	m.Protocol = types.StringValue(vs.Protocol)
	m.Enabled = types.BoolValue(vs.Enable)
	m.serviceSettingsModel.fromAPI(vs)
}

// params returns the attributes shared by addvs and modvs.
func (m *virtualServiceResourceModel) params() client.VirtualServiceParams {
	p := m.serviceSettingsModel.params()
	p.Enable = m.Enabled.ValueBoolPointer()
	return p
}
