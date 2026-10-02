package provider

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/technicallyJack/terraform-provider-kemp/internal/client"
)

// sslModel is the optional ssl block of a virtual service. A null block means
// SSL is off.
type sslModel struct {
	Certificates      types.List   `tfsdk:"certificates"`
	TLSVersions       types.Set    `tfsdk:"tls_versions"`
	CipherSet         types.String `tfsdk:"cipher_set"`
	Reencrypt         types.Bool   `tfsdk:"reencrypt"`
	HTTP2             types.Bool   `tfsdk:"http2"`
	PassSNI           types.Bool   `tfsdk:"pass_sni"`
	ClientCertificate types.String `tfsdk:"client_certificate"`
}

// TlsType is a bitmask of *disabled* protocols. SSLv3 (bit 1) is always
// disabled by the provider; the others map to tls_versions.
var tlsVersionBits = map[string]int{"1.0": 2, "1.1": 4, "1.2": 8, "1.3": 16}

const sslv3Bit = 1

// clientCertModes are the ClientCert levels 0-6, as labelled in the
// LoadMaster UI. Levels 1-6 all require a trusted client certificate
// (verified); 2-6 additionally pass it to the real servers (header formats
// per Kemp's labels, not independently verified).
var clientCertModes = []string{
	"none",
	"required",
	"required_add_headers",
	"required_pass_der_ssl_client_cert",
	"required_pass_der_x_client_cert",
	"required_pass_pem_ssl_client_cert",
	"required_pass_pem_x_client_cert",
}

func sslAttribute() schema.Attribute {
	defaultVersions := types.SetValueMust(types.StringType, []attr.Value{
		types.StringValue("1.1"), types.StringValue("1.2"), types.StringValue("1.3"),
	})
	return schema.SingleNestedAttribute{
		Optional: true,
		Description: "SSL offload. Leave out to turn SSL off. Only for top-level virtual services: a SubVS " +
			"receives traffic its parent has already decrypted.",
		Attributes: map[string]schema.Attribute{
			"certificates": schema.ListAttribute{
				ElementType: types.StringType,
				Required:    true,
				Description: "Names of kemp_certificate certificates to serve. With several, the LoadMaster picks " +
					"one by SNI. (Turning SSL on without any makes the LoadMaster generate a self-signed " +
					"certificate, so at least one is required.)",
				Validators: []validator.List{
					listvalidator.SizeAtLeast(1),
					listvalidator.UniqueValues(),
					listvalidator.ValueStringsAre(certNameValidator),
				},
			},
			"tls_versions": schema.SetAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
				Default:     setdefault.StaticValue(defaultVersions),
				Description: "TLS versions to accept: any of 1.0, 1.1, 1.2, 1.3. Defaults to the LoadMaster's " +
					"default, 1.1-1.3. SSLv3 is always off.",
				Validators: []validator.Set{
					setvalidator.SizeAtLeast(1),
					setvalidator.ValueStringsAre(stringvalidator.OneOf("1.0", "1.1", "1.2", "1.3")),
				},
			},
			"cipher_set": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("Default"),
				Description: "Cipher set: a built-in one (Default, BestPractices, Intermediate_compatibility, ...) or a kemp_cipher_set. Defaults to Default.",
			},
			"reencrypt": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Re-encrypt traffic to the real servers. Defaults to false.",
			},
			"http2": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Allow HTTP/2 from clients. Defaults to false.",
			},
			"pass_sni": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Pass the client's SNI hostname on to the real servers. Requires reencrypt. Defaults to false.",
			},
			"client_certificate": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("none"),
				Description: "Client certificate authentication: none, or required (clients must present a " +
					"certificate signed by one of the LoadMaster's intermediate certificates, see " +
					"kemp_intermediate_certificate), optionally passing it to the real servers: " +
					"required_add_headers, required_pass_der_ssl_client_cert, required_pass_der_x_client_cert, " +
					"required_pass_pem_ssl_client_cert or required_pass_pem_x_client_cert (header formats as " +
					"labelled by Kemp). Defaults to none.",
				Validators: []validator.String{stringvalidator.OneOf(clientCertModes...)},
			},
		},
	}
}

func validateSSL(ctx context.Context, config tfsdk.Config) diag.Diagnostics {
	var diags diag.Diagnostics
	var s *sslModel
	diags.Append(config.GetAttribute(ctx, path.Root("ssl"), &s)...)
	if diags.HasError() || s == nil {
		return diags
	}
	if s.PassSNI.ValueBool() && !s.Reencrypt.IsUnknown() && !s.Reencrypt.ValueBool() {
		diags.AddAttributeError(path.Root("ssl").AtName("pass_sni"), "pass_sni requires reencrypt",
			"The LoadMaster only passes SNI on when it re-encrypts to the real servers; set reencrypt = true.")
	}
	return diags
}

func tlsMask(versions []string) int {
	mask := sslv3Bit
	for v, bit := range tlsVersionBits {
		if !slices.Contains(versions, v) {
			mask |= bit
		}
	}
	return mask
}

func tlsVersionsFromMask(mask int) []string {
	var out []string
	if mask&sslv3Bit == 0 {
		out = append(out, "ssl3") // not configurable, so it shows up as drift
	}
	for v, bit := range tlsVersionBits {
		if mask&bit == 0 {
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

func sslFromAPI(vs *client.VirtualService) *sslModel {
	if !vs.SSLAcceleration {
		return nil
	}
	mask, _ := strconv.Atoi(vs.TlsType)
	versions := tlsVersionsFromMask(mask)
	vals := make([]attr.Value, len(versions))
	for i, v := range versions {
		vals[i] = types.StringValue(v)
	}
	mode := fmt.Sprintf("level_%d", vs.ClientCert)
	if vs.ClientCert >= 0 && vs.ClientCert < len(clientCertModes) {
		mode = clientCertModes[vs.ClientCert]
	}
	return &sslModel{
		Certificates:      stringList(strings.Fields(vs.CertFile)),
		TLSVersions:       types.SetValueMust(types.StringType, vals),
		CipherSet:         types.StringValue(vs.CipherSet),
		Reencrypt:         types.BoolValue(vs.SSLReencrypt),
		HTTP2:             types.BoolValue(vs.AllowHTTP2),
		PassSNI:           types.BoolValue(vs.PassSni),
		ClientCertificate: types.StringValue(mode),
	}
}

// applySSL brings a virtual service's SSL settings in line with s (nil turns
// SSL off). current is the virtual service as last read or written. The
// LoadMaster has ordering rules: SSL options can only change while SSL is on,
// and pass_sni needs reencrypt, so pass_sni goes off first and on last.
func applySSL(ctx context.Context, c *client.Client, ref client.VSRef, current *client.VirtualService, s *sslModel) diag.Diagnostics {
	var diags diag.Diagnostics
	index := current.Index
	f, t := false, true
	update := func(p client.VirtualServiceParams) bool {
		vs, err := c.UpdateVirtualService(ctx, index, p)
		if err == nil {
			err = client.CheckSame(ref, vs, 0)
		}
		if err != nil {
			diags.AddError("Unable to update SSL settings", err.Error())
			return false
		}
		return true
	}

	if current.SSLAcceleration && current.PassSni && (s == nil || !s.PassSNI.ValueBool()) {
		if !update(client.VirtualServiceParams{PassSni: &f}) {
			return diags
		}
	}

	if s == nil {
		if !current.SSLAcceleration {
			return diags
		}
		// Reset the options before turning SSL off; they can't be changed
		// while it's off, and would otherwise come back on re-enable.
		zero := 0
		if !update(client.VirtualServiceParams{SSLReencrypt: &f, AllowHTTP2: &f, ClientCert: &zero}) {
			return diags
		}
		update(client.VirtualServiceParams{SSLAcceleration: &f})
		return diags
	}

	certs := strings.Join(listStrings(s.Certificates), " ")
	mask := tlsMask(setStrings(s.TLSVersions))
	level := slices.Index(clientCertModes, s.ClientCertificate.ValueString())
	if !update(client.VirtualServiceParams{
		SSLAcceleration: &t,
		CertFile:        &certs,
		TlsType:         &mask,
		CipherSet:       s.CipherSet.ValueStringPointer(),
		SSLReencrypt:    s.Reencrypt.ValueBoolPointer(),
		AllowHTTP2:      s.HTTP2.ValueBoolPointer(),
		ClientCert:      &level,
	}) {
		return diags
	}
	if s.PassSNI.ValueBool() {
		update(client.VirtualServiceParams{PassSni: &t})
	}
	return diags
}

func setStrings(s types.Set) []string {
	var out []string
	for _, e := range s.Elements() {
		out = append(out, e.(types.String).ValueString())
	}
	return out
}
