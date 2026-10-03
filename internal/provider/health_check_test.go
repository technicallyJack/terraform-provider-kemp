package provider

import (
	"fmt"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// TestAccHealthCheckDepth exercises check timing, response patterns, check
// headers and enhanced checks with a minimum of healthy real servers. With
// KEMP_TEST_ECHO_ADDRESS the echo server is a real server and the test waits
// for the LoadMaster's view of its health to change.
func TestAccHealthCheckDepth(t *testing.T) {
	addr := os.Getenv("KEMP_TEST_VS_ADDRESS")
	if addr == "" {
		t.Skip("KEMP_TEST_VS_ADDRESS not set")
	}
	echoAddr := os.Getenv("KEMP_TEST_ECHO_ADDRESS")
	rsAddr := echoAddr
	if echoAddr == "" {
		rsAddr = "10.0.254.250"
	} else {
		startEcho(t)
	}
	c, err := sweeperClient()
	if err != nil {
		t.Fatal(err)
	}

	config := func(settings string) string {
		return fmt.Sprintf(`
resource "kemp_virtual_service" "test" {
  address    = %q
  port       = "18080"
  nickname   = "tf-acc-hc"
  type       = "http"
  check_type = "http"
  check_path = "/"
  %s
}

# The echo server, and a port nothing listens on.
resource "kemp_real_server" "echo" {
  virtual_service_id = kemp_virtual_service.test.id
  address            = %q
  port               = 18090
}

resource "kemp_real_server" "closed" {
  virtual_service_id = kemp_virtual_service.test.id
  address            = %q
  port               = 18091
}
`, addr, settings, rsAddr, rsAddr)
	}
	// waitFor polls the LoadMaster until the echo real server (and the
	// virtual service) report the wanted status.
	waitFor := func(rsStatus, vsStatus string) resource.TestCheckFunc {
		return func(*terraform.State) error {
			if echoAddr == "" {
				return nil
			}
			var gotRS, gotVS string
			for deadline := time.Now().Add(90 * time.Second); time.Now().Before(deadline); time.Sleep(3 * time.Second) {
				vs := findVS(t, c, "tf-acc-hc", addr)
				if vs == nil {
					return fmt.Errorf("virtual service not found")
				}
				// Status comes from listvs: in a showvs response the API's own
				// "status" field shadows the virtual service's "Status".
				gotVS = vs.Status
				for _, rs := range rsList(t, c, vs.Index) {
					if rs.Port == 18090 {
						gotRS = rs.Status
					}
				}
				if gotRS == rsStatus && (vsStatus == "" || gotVS == vsStatus) {
					return nil
				}
			}
			return fmt.Errorf("echo real server %q (want %q), virtual service %q (want %q)", gotRS, rsStatus, gotVS, vsStatus)
		}
	}
	const name = "kemp_virtual_service.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      config(`min_healthy_real_servers = 2`),
				ExpectError: regexp.MustCompile(`requires enhanced_health_checks`),
			},
			{
				Config: config(`check_interval = 9
  check_timeout  = 4
  check_retries  = 2
  check_method   = "GET"
  check_pattern  = "padding"
  check_headers  = { X-Probe = "kemp", X-Env = "test" }`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "check_interval", "9"),
					resource.TestCheckResourceAttr(name, "check_timeout", "4"),
					resource.TestCheckResourceAttr(name, "check_retries", "2"),
					resource.TestCheckResourceAttr(name, "check_pattern", "padding"),
					resource.TestCheckResourceAttr(name, "check_headers.X-Probe", "kemp"),
					resource.TestCheckResourceAttr(name, "check_headers.%", "2"),
					waitFor("Up", ""),
				),
			},
			{ResourceName: name, ImportState: true, ImportStateVerify: true},
			{
				// A pattern the response doesn't contain marks the server down.
				Config: config(`check_interval = 9
  check_retries  = 2
  check_method   = "GET"
  check_pattern  = "nosuchword"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "check_headers.%", "0"),
					waitFor("Down", ""),
				),
			},
			{
				// With a minimum of two and the closed port down, the whole
				// virtual service is down even though the echo server is up.
				Config: config(`check_interval           = 9
  check_retries            = 2
  check_method             = "GET"
  check_pattern            = "padding"
  enhanced_health_checks   = true
  min_healthy_real_servers = 2`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "enhanced_health_checks", "true"),
					resource.TestCheckResourceAttr(name, "min_healthy_real_servers", "2"),
					waitFor("Up", "Down"),
				),
			},
			{
				// Removing everything: the minimum goes to 0 before enhanced
				// checks are turned off. Timing keeps its last values.
				Config: config(""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "enhanced_health_checks", "false"),
					resource.TestCheckResourceAttr(name, "min_healthy_real_servers", "0"),
					resource.TestCheckResourceAttr(name, "check_pattern", ""),
					resource.TestCheckResourceAttr(name, "check_interval", "9"),
					waitFor("Up", "Up"),
				),
			},
		},
	})
}
