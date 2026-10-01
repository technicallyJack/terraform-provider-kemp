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
}
