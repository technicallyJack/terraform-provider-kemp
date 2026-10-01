package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccSubVirtualServiceResource builds a parent virtual service on
// KEMP_TEST_VS_ADDRESS with SubVSs that each have a real server, mirroring a
// typical http parent with per-cluster SubVSs. The two SubVSs don't depend on
// each other, so Terraform creates them concurrently on the same parent.
//
// The testing framework can't handle for_each resources in state, so the
// SubVSs are written out individually.
func TestAccSubVirtualServiceResource(t *testing.T) {
	vsAddr := os.Getenv("KEMP_TEST_VS_ADDRESS")
	if vsAddr == "" {
		t.Skip("KEMP_TEST_VS_ADDRESS not set")
	}
	rsAddr := os.Getenv("KEMP_TEST_RS_ADDRESS")
	if rsAddr == "" {
		rsAddr = "10.0.254.250"
	}

	parent := fmt.Sprintf(`
resource "kemp_virtual_service" "parent" {
  address  = %q
  port     = "18080"
  nickname = "tf-acc-subvs-parent"
  type     = "http"
}
`, vsAddr)

	sub := func(name, schedule string, weight int, enabled bool) string {
		return fmt.Sprintf(`
resource "kemp_sub_virtual_service" %[1]q {
  parent_index = kemp_virtual_service.parent.index
  nickname     = %[1]q
  type         = "http"
  schedule     = %[2]q
  weight       = %[3]d
  enabled      = %[4]t
  check_type   = "http"
  check_path   = "/healthz"
}

resource "kemp_real_server" %[1]q {
  virtual_service_index = kemp_sub_virtual_service.%[1]s.index
  address               = %[5]q
  port                  = 18080
}
`, name, schedule, weight, enabled, rsAddr)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: parent + sub("sub_a", "lc", 500, true) + sub("sub_b", "rr", 1000, true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("kemp_sub_virtual_service.sub_a", "parent_index", "kemp_virtual_service.parent", "index"),
					resource.TestCheckResourceAttrPair("kemp_sub_virtual_service.sub_b", "parent_index", "kemp_virtual_service.parent", "index"),
					resource.TestCheckResourceAttr("kemp_sub_virtual_service.sub_a", "nickname", "sub_a"),
					resource.TestCheckResourceAttr("kemp_sub_virtual_service.sub_a", "schedule", "lc"),
					resource.TestCheckResourceAttr("kemp_sub_virtual_service.sub_a", "weight", "500"),
					resource.TestCheckResourceAttr("kemp_sub_virtual_service.sub_a", "check_path", "/healthz"),
					resource.TestCheckResourceAttr("kemp_sub_virtual_service.sub_b", "nickname", "sub_b"),
					resource.TestCheckResourceAttrPair("kemp_real_server.sub_a", "virtual_service_index", "kemp_sub_virtual_service.sub_a", "index"),
					resource.TestCheckResourceAttrPair("kemp_real_server.sub_b", "virtual_service_index", "kemp_sub_virtual_service.sub_b", "index"),
				),
			},
			{
				ResourceName:      "kemp_sub_virtual_service.sub_a",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: parent + sub("sub_a", "wrr", 200, false) + sub("sub_b", "rr", 1000, true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kemp_sub_virtual_service.sub_a", "schedule", "wrr"),
					resource.TestCheckResourceAttr("kemp_sub_virtual_service.sub_a", "weight", "200"),
					resource.TestCheckResourceAttr("kemp_sub_virtual_service.sub_a", "enabled", "false"),
				),
			},
			{
				// Removing a SubVS (and its real server) leaves its sibling alone.
				Config: parent + sub("sub_a", "wrr", 200, false),
				Check:  resource.TestCheckResourceAttr("kemp_sub_virtual_service.sub_a", "nickname", "sub_a"),
			},
		},
	})
}
