package provider

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccBodyRuleResource(t *testing.T) {
	const name = "kemp_body_rule.test"
	config := func(pattern, replacement string, nocase bool) string {
		return fmt.Sprintf(`
resource "kemp_body_rule" "test" {
  name             = "tfacc_body"
  pattern          = %q
  replacement      = %q
  case_insensitive = %t
}
`, pattern, replacement, nocase)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config("padding", "PADDED", false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "pattern", "padding"),
					resource.TestCheckResourceAttr(name, "replacement", "PADDED"),
					resource.TestCheckResourceAttr(name, "case_insensitive", "false"),
				),
			},
			{ResourceName: name, ImportState: true, ImportStateId: "tfacc_body", ImportStateVerify: true},
			{
				Config: config("internal\\.example\\.com", "www.example.com", true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "pattern", `internal\.example\.com`),
					resource.TestCheckResourceAttr(name, "case_insensitive", "true"),
				),
			},
		},
	})
}

// TestAccResponseBodyRules attaches a body rule to a virtual service. With
// KEMP_TEST_ECHO_ADDRESS it also checks the replacement on real traffic, and
// it changes the service type with the rule attached, since a type change
// clears the LoadMaster's body rule list.
func TestAccResponseBodyRules(t *testing.T) {
	addr := os.Getenv("KEMP_TEST_VS_ADDRESS")
	if addr == "" {
		t.Skip("KEMP_TEST_VS_ADDRESS not set")
	}
	echoAddr := os.Getenv("KEMP_TEST_ECHO_ADDRESS")
	rsAddr, rsPort := echoAddr, 18090
	if echoAddr == "" {
		rsAddr, rsPort = "10.0.254.250", 18080
	} else {
		startEcho(t)
	}
	vip := net.JoinHostPort(addr, "18080")

	config := func(vsType, rules string) string {
		return fmt.Sprintf(`
resource "kemp_body_rule" "test" {
  name        = "tfacc_body_vs"
  pattern     = "padding"
  replacement = "PADDED"
}

resource "kemp_virtual_service" "test" {
  address             = %q
  port                = "18080"
  nickname            = "tf-acc-body"
  type                = %q
  check_type          = "none"
  response_body_rules = [%s]
}

resource "kemp_real_server" "test" {
  virtual_service_id = kemp_virtual_service.test.id
  address            = %q
  port               = %d
}
`, addr, vsType, rules, rsAddr, rsPort)
	}
	body := func(want string, present bool) resource.TestCheckFunc {
		return func(*terraform.State) error {
			if echoAddr == "" {
				return nil
			}
			client := &http.Client{Timeout: 5 * time.Second}
			var last string
			for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(2 * time.Second) {
				resp, err := client.Get("http://" + vip + "/")
				if err != nil {
					continue
				}
				b, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				last = string(b)
				if strings.Contains(last, want) == present {
					return nil
				}
			}
			return fmt.Errorf("response body: expected %q present=%t, got %.80q", want, present, last)
		}
	}
	const name = "kemp_virtual_service.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config("http", "kemp_body_rule.test.name"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "response_body_rules.#", "1"),
					resource.TestCheckResourceAttr(name, "response_body_rules.0", "tfacc_body_vs"),
					body("PADDED", true),
				),
			},
			{ResourceName: name, ImportState: true, ImportStateVerify: true},
			{
				// The type change clears the list on the LoadMaster; the
				// provider must attach the rule again.
				Config: config("http2", "kemp_body_rule.test.name"),
				Check:  resource.TestCheckResourceAttr(name, "response_body_rules.0", "tfacc_body_vs"),
			},
			{
				Config: config("http", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "response_body_rules.#", "0"),
					body("padding", true),
				),
			},
		},
	})
}
