package provider

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// Rule tests create only global rules, which do nothing until attached, so
// they don't need KEMP_TEST_VS_ADDRESS.

func TestAccMatchRuleResource(t *testing.T) {
	const name = "kemp_match_rule.test"
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `resource "kemp_match_rule" "test" {
  name = "tfacc-bad"
  pattern = "/x/"
}`,
				ExpectError: regexp.MustCompile(`letters, digits and underscores`),
			},
			{
				Config: `resource "kemp_match_rule" "test" {
  name = "tfacc_match"
  pattern = "/x/"
  set_flag = 10
}`,
				ExpectError: regexp.MustCompile(`between 1 and 9`),
			},
			{
				Config: `resource "kemp_match_rule" "test" {
  name             = "tfacc_match"
  pattern          = "/api/"
  match_type       = "prefix"
  case_insensitive = true
  include_query    = true
  set_flag         = 3
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "match_type", "prefix"),
					resource.TestCheckResourceAttr(name, "case_insensitive", "true"),
					resource.TestCheckResourceAttr(name, "include_query", "true"),
					resource.TestCheckResourceAttr(name, "negate", "false"),
					resource.TestCheckResourceAttr(name, "header", ""),
					resource.TestCheckResourceAttr(name, "set_flag", "3"),
					resource.TestCheckNoResourceAttr(name, "only_if_flag"),
				),
			},
			{ResourceName: name, ImportState: true, ImportStateVerify: true},
			{
				Config: `resource "kemp_match_rule" "test" {
  name             = "tfacc_match"
  pattern          = "curl"
  header           = "User-Agent"
  negate           = true
  include_host     = true
  fail_on_match    = true
  only_if_flag     = 3
  only_if_not_flag = 4
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "match_type", "regex"),
					resource.TestCheckResourceAttr(name, "header", "User-Agent"),
					resource.TestCheckResourceAttr(name, "negate", "true"),
					resource.TestCheckResourceAttr(name, "include_host", "true"),
					resource.TestCheckResourceAttr(name, "fail_on_match", "true"),
					resource.TestCheckResourceAttr(name, "case_insensitive", "false"),
					resource.TestCheckNoResourceAttr(name, "set_flag"),
					resource.TestCheckResourceAttr(name, "only_if_flag", "3"),
					resource.TestCheckResourceAttr(name, "only_if_not_flag", "4"),
				),
			},
		},
	})
}

func TestAccHeaderRuleResource(t *testing.T) {
	const name = "kemp_header_rule.test"
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `resource "kemp_header_rule" "test" {
  name = "tfacc_hdr"
  action = "add"
  header = "X-Env"
}`,
				ExpectError: regexp.MustCompile(`requires value`),
			},
			{
				Config: `resource "kemp_header_rule" "test" {
  name = "tfacc_hdr"
  action = "delete"
  header = "X-Env"
  value = "x"
}`,
				ExpectError: regexp.MustCompile(`value is not used`),
			},
			{
				Config: `resource "kemp_header_rule" "test" {
  name   = "tfacc_hdr"
  action = "add"
  header = "X-Env"
  value  = "prod"
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "action", "add"),
					resource.TestCheckResourceAttr(name, "header", "X-Env"),
					resource.TestCheckResourceAttr(name, "value", "prod"),
					resource.TestCheckNoResourceAttr(name, "pattern"),
				),
			},
			{ResourceName: name, ImportState: true, ImportStateVerify: true},
			{
				// Action changes happen in place.
				Config: `resource "kemp_header_rule" "test" {
  name    = "tfacc_hdr"
  action  = "replace"
  header  = "Host"
  pattern = "internal\\.example\\.com"
  value   = "www.example.com"
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "action", "replace"),
					resource.TestCheckResourceAttr(name, "header", "Host"),
					resource.TestCheckResourceAttr(name, "pattern", `internal\.example\.com`),
					resource.TestCheckResourceAttr(name, "value", "www.example.com"),
				),
			},
			{
				Config: `resource "kemp_header_rule" "test" {
  name   = "tfacc_hdr"
  action = "delete"
  header = "X-Powered-By"
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "action", "delete"),
					resource.TestCheckResourceAttr(name, "header", "X-Powered-By"),
					resource.TestCheckNoResourceAttr(name, "value"),
				),
			},
			{ResourceName: name, ImportState: true, ImportStateVerify: true},
		},
	})
}

func TestAccURLRuleResource(t *testing.T) {
	const name = "kemp_url_rule.test"
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `resource "kemp_url_rule" "test" {
  name        = "tfacc_url"
  pattern     = "^/old/(.*)"
  replacement = "/new/\\1"
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "pattern", "^/old/(.*)"),
					resource.TestCheckResourceAttr(name, "replacement", `/new/\1`),
				),
			},
			{ResourceName: name, ImportState: true, ImportStateVerify: true},
			{
				Config: `resource "kemp_url_rule" "test" {
  name        = "tfacc_url"
  pattern     = "^/v1/"
  replacement = "/api/v1/"
}`,
				Check: resource.TestCheckResourceAttr(name, "replacement", "/api/v1/"),
			},
		},
	})
}
