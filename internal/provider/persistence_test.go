package provider

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"gitea.jacksonwinkler.com/jackson.winkler/terraform-provider-kemp/internal/client"
)

func TestPersistenceParams(t *testing.T) {
	var off client.VirtualServiceParams
	persistenceParams(nil, &off)
	if *off.Persist != "none" || *off.Cookie != "" || *off.QueryTag != "" || off.PersistTimeout != nil {
		t.Errorf("off: got Persist=%q Cookie=%q QueryTag=%q timeout=%v", *off.Persist, *off.Cookie, *off.QueryTag, off.PersistTimeout)
	}

	var header client.VirtualServiceParams
	persistenceParams(&persistenceModel{
		Mode:           types.StringValue("header"),
		Timeout:        types.Int64Value(900),
		CookieName:     types.StringNull(),
		HeaderName:     types.StringValue("X-User"),
		QueryParameter: types.StringNull(),
	}, &header)
	if *header.Persist != "header" || *header.PersistTimeout != "900" || *header.Cookie != "X-User" || *header.QueryTag != "" {
		t.Errorf("header: got %q %q %q %q", *header.Persist, *header.PersistTimeout, *header.Cookie, *header.QueryTag)
	}
}

func TestPersistenceFromAPI(t *testing.T) {
	if p := persistenceFromAPI(&client.VirtualService{PersistTimeout: "0", Cookie: "stale"}); p != nil {
		t.Errorf("off: got %+v, want nil", p)
	}

	// Stale Cookie/QueryTag values from earlier modes must not leak into unrelated fields.
	p := persistenceFromAPI(&client.VirtualService{Persist: "src", PersistTimeout: "600", Cookie: "stale", QueryTag: "stale"})
	if p.Mode.ValueString() != "src" || p.Timeout.ValueInt64() != 600 || !p.CookieName.IsNull() || !p.HeaderName.IsNull() || !p.QueryParameter.IsNull() {
		t.Errorf("src: got %+v", p)
	}

	p = persistenceFromAPI(&client.VirtualService{Persist: "header", PersistTimeout: "360", Cookie: "X-User"})
	if p.HeaderName.ValueString() != "X-User" || !p.CookieName.IsNull() {
		t.Errorf("header: got %+v", p)
	}

	p = persistenceFromAPI(&client.VirtualService{Persist: "cookie-hash", PersistTimeout: "360"})
	if !p.CookieName.IsNull() {
		t.Errorf("cookie-hash without name: got %+v", p)
	}
}

func TestAccVirtualServiceResource_persistence(t *testing.T) {
	addr := os.Getenv("KEMP_TEST_VS_ADDRESS")
	if addr == "" {
		t.Skip("KEMP_TEST_VS_ADDRESS not set")
	}

	config := func(persistence string) string {
		return fmt.Sprintf(`
resource "kemp_virtual_service" "test" {
  address  = %q
  port     = "18080"
  nickname = "tf-acc-persist-test"
  type     = "http"
  %s
}

resource "kemp_sub_virtual_service" "test" {
  parent_index = kemp_virtual_service.test.index
  nickname     = "tf-acc-persist-sub"
  persistence = {
    mode    = "src"
    timeout = 600
  }
}
`, addr, persistence)
	}
	const name = "kemp_virtual_service.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Validation runs before anything is created.
			{
				Config:      config(`persistence = { mode = "cookie" }`),
				ExpectError: regexp.MustCompile(`requires cookie_name`),
			},
			{
				Config:      config(`persistence = { mode = "src", header_name = "X-User" }`),
				ExpectError: regexp.MustCompile(`header_name only applies`),
			},
			{
				Config:      config(`persistence = { mode = "src", timeout = 30 }`),
				ExpectError: regexp.MustCompile(`between 60 and 604800`),
			},
			{
				// Persistence set at create time.
				Config: config(`persistence = { mode = "cookie", cookie_name = "JSESSIONID", timeout = 1800 }`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "persistence.mode", "cookie"),
					resource.TestCheckResourceAttr(name, "persistence.cookie_name", "JSESSIONID"),
					resource.TestCheckResourceAttr(name, "persistence.timeout", "1800"),
					resource.TestCheckResourceAttr("kemp_sub_virtual_service.test", "persistence.mode", "src"),
					resource.TestCheckResourceAttr("kemp_sub_virtual_service.test", "persistence.timeout", "600"),
				),
			},
			{
				ResourceName:      name,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				// The cookie name and header name share one API field.
				Config: config(`persistence = { mode = "header", header_name = "X-User" }`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "persistence.mode", "header"),
					resource.TestCheckResourceAttr(name, "persistence.header_name", "X-User"),
					resource.TestCheckNoResourceAttr(name, "persistence.cookie_name"),
					resource.TestCheckResourceAttr(name, "persistence.timeout", "360"),
				),
			},
			{
				Config: config(`persistence = { mode = "query-hash", query_parameter = "session" }`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "persistence.query_parameter", "session"),
					resource.TestCheckNoResourceAttr(name, "persistence.header_name"),
				),
			},
			{
				// Removing the block turns persistence off.
				Config: config(""),
				Check:  resource.TestCheckNoResourceAttr(name, "persistence.mode"),
			},
		},
	})
}
