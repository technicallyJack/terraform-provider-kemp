package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"gitea.jacksonwinkler.com/jackson.winkler/terraform-provider-kemp/internal/client"
)

var (
	_ datasource.DataSource              = &virtualServicesDataSource{}
	_ datasource.DataSourceWithConfigure = &virtualServicesDataSource{}
)

// NewVirtualServicesDataSource returns the kemp_virtual_services data source.
func NewVirtualServicesDataSource() datasource.DataSource {
	return &virtualServicesDataSource{}
}

type virtualServicesDataSource struct {
	client *client.Client
}

type virtualServicesModel struct {
	VirtualServices []virtualServiceModel `tfsdk:"virtual_services"`
}

type virtualServiceModel struct {
	Index    types.Int64  `tfsdk:"index"`
	Nickname types.String `tfsdk:"nickname"`
	Address  types.String `tfsdk:"address"`
	Port     types.String `tfsdk:"port"`
	Protocol types.String `tfsdk:"protocol"`
	Status   types.String `tfsdk:"status"`
	Enabled  types.Bool   `tfsdk:"enabled"`
}

func (d *virtualServicesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_virtual_services"
}

func (d *virtualServicesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists all virtual services configured on the LoadMaster.",
		Attributes: map[string]schema.Attribute{
			"virtual_services": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"index":    schema.Int64Attribute{Computed: true, Description: "LoadMaster-assigned virtual service index."},
						"nickname": schema.StringAttribute{Computed: true},
						"address":  schema.StringAttribute{Computed: true},
						"port":     schema.StringAttribute{Computed: true},
						"protocol": schema.StringAttribute{Computed: true},
						"status":   schema.StringAttribute{Computed: true},
						"enabled":  schema.BoolAttribute{Computed: true},
					},
				},
			},
		},
	}
}

func (d *virtualServicesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data type",
			fmt.Sprintf("Expected *client.Client, got %T. Please report this issue to the provider developers.", req.ProviderData))
		return
	}
	d.client = c
}

func (d *virtualServicesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	vss, err := d.client.ListVirtualServices(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to list virtual services", err.Error())
		return
	}

	state := virtualServicesModel{VirtualServices: make([]virtualServiceModel, 0, len(vss))}
	for _, vs := range vss {
		state.VirtualServices = append(state.VirtualServices, virtualServiceModel{
			Index:    types.Int64Value(int64(vs.Index)),
			Nickname: types.StringValue(vs.NickName),
			Address:  types.StringValue(vs.VSAddress),
			Port:     types.StringValue(vs.VSPort),
			Protocol: types.StringValue(vs.Protocol),
			Status:   types.StringValue(vs.Status),
			Enabled:  types.BoolValue(vs.Enable),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
