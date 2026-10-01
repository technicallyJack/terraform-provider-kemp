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
