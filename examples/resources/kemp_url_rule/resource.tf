resource "kemp_url_rule" "legacy" {
  name        = "legacy_paths"
  pattern     = "^/old/(.*)"
  replacement = "/new/\\1"
}
