package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccVirtualServiceResource creates a real virtual service. It needs
// KEMP_TEST_VS_ADDRESS set to an IP that is safe for the LoadMaster to claim
// (unused on the network), and is skipped otherwise.
func TestAccVirtualServiceResource(t *testing.T) {
	addr := os.Getenv("KEMP_TEST_VS_ADDRESS")
	if addr == "" {
		t.Skip("KEMP_TEST_VS_ADDRESS not set")
	}

	config := func(port, nickname string, enabled bool) string {
		return fmt.Sprintf(`
resource "kemp_virtual_service" "test" {
  address  = %q
  port     = %q
  nickname = %q
  enabled  = %t
}
`, addr, port, nickname, enabled)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config("18080", "tf-acc-test", true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kemp_virtual_service.test", "address", addr),
					resource.TestCheckResourceAttr("kemp_virtual_service.test", "port", "18080"),
					resource.TestCheckResourceAttr("kemp_virtual_service.test", "protocol", "tcp"),
					resource.TestCheckResourceAttr("kemp_virtual_service.test", "nickname", "tf-acc-test"),
					resource.TestCheckResourceAttr("kemp_virtual_service.test", "enabled", "true"),
					resource.TestCheckResourceAttr("kemp_virtual_service.test", "type", "gen"),
					resource.TestCheckResourceAttr("kemp_virtual_service.test", "schedule", "rr"),
					resource.TestCheckResourceAttr("kemp_virtual_service.test", "check_type", "tcp"),
					resource.TestCheckResourceAttr("kemp_virtual_service.test", "check_port", "0"),
					resource.TestCheckResourceAttr("kemp_virtual_service.test", "check_method", "HEAD"),
					resource.TestCheckResourceAttrSet("kemp_virtual_service.test", "index"),
				),
			},
			{
				ResourceName:      "kemp_virtual_service.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: config("18081", "tf-acc-test-updated", false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kemp_virtual_service.test", "port", "18081"),
					resource.TestCheckResourceAttr("kemp_virtual_service.test", "nickname", "tf-acc-test-updated"),
					resource.TestCheckResourceAttr("kemp_virtual_service.test", "enabled", "false"),
				),
			},
		},
	})
}

func TestAccVirtualServiceResource_healthCheckAndSchedule(t *testing.T) {
	addr := os.Getenv("KEMP_TEST_VS_ADDRESS")
	if addr == "" {
		t.Skip("KEMP_TEST_VS_ADDRESS not set")
	}

	config := func(body string) string {
		return fmt.Sprintf(`
resource "kemp_virtual_service" "test" {
  address  = %q
  port     = "18080"
  nickname = "tf-acc-hc-test"
  %s
}
`, addr, body)
	}
	const name = "kemp_virtual_service.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// Non-default values at create time, to catch addvs ignoring any.
				Config: config(`
  schedule         = "wlc"
  check_type       = "http"
  check_port       = 8080
  check_path       = "/healthz"
  check_host       = "app.example.com"
  check_method     = "GET"
  check_use_http11 = true`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "schedule", "wlc"),
					resource.TestCheckResourceAttr(name, "check_type", "http"),
					resource.TestCheckResourceAttr(name, "check_port", "8080"),
					resource.TestCheckResourceAttr(name, "check_path", "/healthz"),
					resource.TestCheckResourceAttr(name, "check_host", "app.example.com"),
					resource.TestCheckResourceAttr(name, "check_method", "GET"),
					resource.TestCheckResourceAttr(name, "check_use_http11", "true"),
				),
			},
			{
				ResourceName:      name,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: config(`
  schedule     = "sh"
  check_type   = "https"
  check_port   = 8443
  check_path   = "/ready"
  check_method = "POST"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "schedule", "sh"),
					resource.TestCheckResourceAttr(name, "check_type", "https"),
					resource.TestCheckResourceAttr(name, "check_port", "8443"),
					resource.TestCheckResourceAttr(name, "check_path", "/ready"),
					resource.TestCheckResourceAttr(name, "check_host", ""),
					resource.TestCheckResourceAttr(name, "check_method", "POST"),
					resource.TestCheckResourceAttr(name, "check_use_http11", "false"),
				),
			},
			{
				// Removing everything returns to the defaults.
				Config: config(""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "schedule", "rr"),
					resource.TestCheckResourceAttr(name, "check_type", "tcp"),
					resource.TestCheckResourceAttr(name, "check_port", "0"),
					resource.TestCheckResourceAttr(name, "check_path", ""),
					resource.TestCheckResourceAttr(name, "check_method", "HEAD"),
				),
			},
		},
	})
}
