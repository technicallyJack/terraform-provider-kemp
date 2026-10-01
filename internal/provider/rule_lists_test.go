package provider

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccRuleLists attaches rules every way the provider supports: request,
// response and pre-processing rules on a virtual service, match rules on a
// SubVS slot (content switching) and on a real server.
func TestAccRuleLists(t *testing.T) {
	vsAddr := os.Getenv("KEMP_TEST_VS_ADDRESS")
	if vsAddr == "" {
		t.Skip("KEMP_TEST_VS_ADDRESS not set")
	}
	rsAddr := os.Getenv("KEMP_TEST_RS_ADDRESS")
	if rsAddr == "" {
		rsAddr = "10.0.254.250"
	}

	rules := `
resource "kemp_match_rule" "api" {
  name       = "tfacc_api"
  pattern    = "/api/"
  match_type = "prefix"
}

resource "kemp_match_rule" "flag" {
  name     = "tfacc_flag"
  pattern  = "/beta/"
  set_flag = 1
}

resource "kemp_header_rule" "add" {
  name   = "tfacc_add_env"
  action = "add"
  header = "X-Env"
  value  = "test"
}

resource "kemp_header_rule" "strip" {
  name   = "tfacc_strip_server"
  action = "delete"
  header = "Server"
}

resource "kemp_url_rule" "rewrite" {
  name        = "tfacc_rewrite"
  pattern     = "^/old/"
  replacement = "/new/"
}
`
	config := func(vsRules, subRules, rsRules string) string {
		return rules + fmt.Sprintf(`
resource "kemp_virtual_service" "parent" {
  address  = %[1]q
  port     = "18080"
  nickname = "tf-acc-rules-parent"
  type     = "http"
  %[2]s
}

resource "kemp_sub_virtual_service" "api" {
  parent_index = kemp_virtual_service.parent.index
  nickname     = "tf-acc-rules-api"
  type         = "http"
  %[3]s
}

resource "kemp_real_server" "api" {
  virtual_service_index = kemp_sub_virtual_service.api.index
  address               = %[5]q
  port                  = 18080
}

resource "kemp_virtual_service" "plain" {
  address  = %[1]q
  port     = "18081"
  nickname = "tf-acc-rules-plain"
  type     = "http"
}

resource "kemp_real_server" "plain" {
  virtual_service_index = kemp_virtual_service.plain.index
  address               = %[5]q
  port                  = 18080
  %[4]s
}
`, vsAddr, vsRules, subRules, rsRules, rsAddr)
	}

	const parent, sub, rs = "kemp_virtual_service.parent", "kemp_sub_virtual_service.api", "kemp_real_server.plain"
	fullVS := `
  request_rules     = [kemp_header_rule.add.name, kemp_url_rule.rewrite.name]
  response_rules    = [kemp_header_rule.strip.name]
  pre_process_rules = [kemp_match_rule.flag.name]`
	subMatch := `match_rules = [kemp_match_rule.api.name]`
	rsMatch := `match_rules = [kemp_match_rule.api.name, kemp_match_rule.flag.name]`

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(fullVS, subMatch, rsMatch),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(parent, "request_rules.#", "2"),
					resource.TestCheckResourceAttr(parent, "request_rules.0", "tfacc_add_env"),
					resource.TestCheckResourceAttr(parent, "request_rules.1", "tfacc_rewrite"),
					resource.TestCheckResourceAttr(parent, "response_rules.0", "tfacc_strip_server"),
					resource.TestCheckResourceAttr(parent, "pre_process_rules.0", "tfacc_flag"),
					resource.TestCheckResourceAttr(sub, "match_rules.0", "tfacc_api"),
					resource.TestCheckResourceAttr(rs, "match_rules.#", "2"),
					resource.TestCheckResourceAttr(rs, "match_rules.1", "tfacc_flag"),
					resource.TestCheckResourceAttr("kemp_virtual_service.plain", "request_rules.#", "0"),
				),
			},
			// The parent has a pre-processing rule, so this also reads through
			// the firmware JSON repair.
			{ResourceName: parent, ImportState: true, ImportStateVerify: true},
			{ResourceName: sub, ImportState: true, ImportStateVerify: true},
			{ResourceName: rs, ImportState: true, ImportStateVerify: true},
			{
				// Reorder request rules, append a response rule, reorder match rules.
				Config: config(`
  request_rules     = [kemp_url_rule.rewrite.name, kemp_header_rule.add.name]
  response_rules    = [kemp_header_rule.strip.name, kemp_header_rule.add.name]
  pre_process_rules = [kemp_match_rule.flag.name]`,
					subMatch,
					`match_rules = [kemp_match_rule.flag.name, kemp_match_rule.api.name]`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(parent, "request_rules.0", "tfacc_rewrite"),
					resource.TestCheckResourceAttr(parent, "request_rules.1", "tfacc_add_env"),
					resource.TestCheckResourceAttr(parent, "response_rules.#", "2"),
					resource.TestCheckResourceAttr(parent, "response_rules.1", "tfacc_add_env"),
					resource.TestCheckResourceAttr(rs, "match_rules.0", "tfacc_flag"),
					resource.TestCheckResourceAttr(rs, "match_rules.1", "tfacc_api"),
				),
			},
			{
				// A match rule in request_rules would be silently ignored by the API.
				Config:      config(`request_rules = [kemp_match_rule.api.name]`, subMatch, rsMatch),
				ExpectError: regexp.MustCompile(`Wrong rule type`),
			},
			{
				// Clearing every list detaches everything.
				Config: config("", "", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(parent, "request_rules.#", "0"),
					resource.TestCheckResourceAttr(parent, "response_rules.#", "0"),
					resource.TestCheckResourceAttr(parent, "pre_process_rules.#", "0"),
					resource.TestCheckResourceAttr(sub, "match_rules.#", "0"),
					resource.TestCheckResourceAttr(rs, "match_rules.#", "0"),
				),
			},
		},
	})
}
