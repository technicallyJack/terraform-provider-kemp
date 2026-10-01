package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccRealServerResource creates a virtual service on KEMP_TEST_VS_ADDRESS
// with a real server pointing at KEMP_TEST_RS_ADDRESS (defaults to
// 10.0.254.250). The real server only receives health checks.
func TestAccRealServerResource(t *testing.T) {
	vsAddr := os.Getenv("KEMP_TEST_VS_ADDRESS")
	if vsAddr == "" {
		t.Skip("KEMP_TEST_VS_ADDRESS not set")
	}
	rsAddr := os.Getenv("KEMP_TEST_RS_ADDRESS")
	if rsAddr == "" {
		rsAddr = "10.0.254.250"
	}

	config := func(addr, body string) string {
		return fmt.Sprintf(`
resource "kemp_virtual_service" "test" {
  address  = %q
  port     = "18080"
  nickname = "tf-acc-rs-test"
}

resource "kemp_real_server" "test" {
  virtual_service_index = kemp_virtual_service.test.index
  address               = %q
  %s
}
`, vsAddr, addr, body)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(rsAddr, `port = 18080`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("kemp_real_server.test", "virtual_service_index", "kemp_virtual_service.test", "index"),
					resource.TestCheckResourceAttr("kemp_real_server.test", "address", rsAddr),
					resource.TestCheckResourceAttr("kemp_real_server.test", "port", "18080"),
					resource.TestCheckResourceAttr("kemp_real_server.test", "forward", "nat"),
					resource.TestCheckResourceAttr("kemp_real_server.test", "weight", "1000"),
					resource.TestCheckResourceAttr("kemp_real_server.test", "limit", "0"),
					resource.TestCheckResourceAttr("kemp_real_server.test", "enabled", "true"),
					resource.TestCheckResourceAttrSet("kemp_real_server.test", "index"),
				),
			},
			{
				ResourceName:      "kemp_real_server.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				// Every settable attribute changes in place.
				Config: config(rsAddr, `
  port    = 18081
  forward = "route"
  weight  = 200
  limit   = 50
  enabled = false`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kemp_real_server.test", "port", "18081"),
					resource.TestCheckResourceAttr("kemp_real_server.test", "forward", "route"),
					resource.TestCheckResourceAttr("kemp_real_server.test", "weight", "200"),
					resource.TestCheckResourceAttr("kemp_real_server.test", "limit", "50"),
					resource.TestCheckResourceAttr("kemp_real_server.test", "enabled", "false"),
				),
			},
			{
				// Address changes force replacement; the new real server must pick
				// up non-default settings that addrs ignores at create time.
				Config: config(nextIP(rsAddr), `
  port    = 18081
  forward = "route"
  weight  = 200`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kemp_real_server.test", "address", nextIP(rsAddr)),
					resource.TestCheckResourceAttr("kemp_real_server.test", "forward", "route"),
					resource.TestCheckResourceAttr("kemp_real_server.test", "weight", "200"),
					resource.TestCheckResourceAttr("kemp_real_server.test", "enabled", "true"),
				),
			},
		},
	})
}

// nextIP returns addr with its last octet incremented (no overflow handling;
// test addresses are chosen to avoid it).
func nextIP(addr string) string {
	var a, b, c, d int
	_, _ = fmt.Sscanf(addr, "%d.%d.%d.%d", &a, &b, &c, &d)
	return fmt.Sprintf("%d.%d.%d.%d", a, b, c, d+1)
}
