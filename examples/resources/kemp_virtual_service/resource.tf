resource "kemp_virtual_service" "web" {
  address  = "10.0.253.50"
  port     = "443"
  nickname = "web_https"
  type     = "http"
}
