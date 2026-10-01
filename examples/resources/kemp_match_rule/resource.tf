# Route /api/ requests to a dedicated SubVS (see match_rules on
# kemp_sub_virtual_service).
resource "kemp_match_rule" "api" {
  name       = "api_path"
  pattern    = "/api/"
  match_type = "prefix"
}

# Match on a header instead of the URL, and set a flag other rules can check.
resource "kemp_match_rule" "mobile" {
  name             = "mobile_clients"
  header           = "User-Agent"
  pattern          = "(iPhone|Android)"
  case_insensitive = true
  set_flag         = 1
}
