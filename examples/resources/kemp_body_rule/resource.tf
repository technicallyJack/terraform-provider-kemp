# Rewrite internal hostnames in pages served through the proxy.
resource "kemp_body_rule" "public_links" {
  name             = "public_links"
  pattern          = "https?://internal\\.example\\.com"
  replacement      = "https://www.example.com"
  case_insensitive = true
}

resource "kemp_virtual_service" "web" {
  address             = "10.0.253.50"
  port                = "443"
  type                = "http"
  response_body_rules = [kemp_body_rule.public_links.name]
}
