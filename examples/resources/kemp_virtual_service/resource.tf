resource "kemp_virtual_service" "web" {
  address  = "10.0.253.50"
  port     = "443"
  nickname = "web_https"
  type     = "http"
  schedule = "wlc"

  check_type       = "https"
  check_port       = 8443
  check_path       = "/healthz"
  check_host       = "web.example.com"
  check_method     = "GET"
  check_use_http11 = true

  persistence = {
    mode        = "cookie"
    cookie_name = "JSESSIONID"
    timeout     = 1800
  }

  ssl = {
    certificates = [kemp_certificate.web.name]
    tls_versions = ["1.2", "1.3"]
    cipher_set   = "BestPractices"
    http2        = true
  }

  # Rules run in list order. Reference the rule resources' names so they are
  # created before being attached.
  request_rules  = [kemp_url_rule.legacy.name, kemp_header_rule.env.name]
  response_rules = [kemp_header_rule.server.name]
}

resource "kemp_url_rule" "legacy" {
  name        = "legacy_paths"
  pattern     = "^/old/(.*)"
  replacement = "/new/\\1"
}

resource "kemp_header_rule" "env" {
  name   = "add_env_header"
  action = "add"
  header = "X-Environment"
  value  = "production"
}

resource "kemp_header_rule" "server" {
  name   = "strip_server_header"
  action = "delete"
  header = "Server"
}
