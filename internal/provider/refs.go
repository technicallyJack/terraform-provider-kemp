package provider

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"

	"github.com/technicallyJack/terraform-provider-kemp/internal/client"
)

// legacyPrefix marks values carried over from state written before stable
// references, when objects were identified by LoadMaster index. The state
// upgraders can't call the API, so they record the old index behind this
// prefix and Read converts it to a reference on the first refresh, while the
// saved indexes still match the LoadMaster.
const legacyPrefix = "legacy-index:"

func legacyValue(index int64) string {
	return legacyPrefix + strconv.FormatInt(index, 10)
}

// legacyIndex returns the index behind a legacy value, if s is one.
func legacyIndex(s string) (int, bool) {
	if !strings.HasPrefix(s, legacyPrefix) {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(s, legacyPrefix))
	return n, err == nil
}

// vsRefValidator checks that a string is a virtual service reference, and
// optionally that it is (or isn't) a SubVS reference.
type vsRefValidator struct {
	allowSubVS, allowTopLevel bool
}

func (v vsRefValidator) Description(context.Context) string {
	return "must be a virtual service or SubVS id"
}

func (v vsRefValidator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (v vsRefValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	ref, err := client.ParseVSRef(req.ConfigValue.ValueString())
	switch {
	case err != nil:
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid virtual service id",
			err.Error()+". Use the id attribute of a kemp_virtual_service or kemp_sub_virtual_service.")
	case ref.SubSlot != 0 && !v.allowSubVS:
		resp.Diagnostics.AddAttributeError(req.Path, "SubVS not allowed",
			fmt.Sprintf("%s is a SubVS; a top-level kemp_virtual_service id is required here.", ref))
	case ref.SubSlot == 0 && !v.allowTopLevel:
		resp.Diagnostics.AddAttributeError(req.Path, "SubVS required",
			fmt.Sprintf("%s is a top-level virtual service; a kemp_sub_virtual_service id is required here.", ref))
	}
}
