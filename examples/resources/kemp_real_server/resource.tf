resource "kemp_virtual_service" "web" {
  address  = "10.0.253.50"
  port     = "443"
  nickname = "web_https"
}

resource "kemp_real_server" "web" {
  for_each = toset(["10.0.254.20", "10.0.254.21"])

  virtual_service_id = kemp_virtual_service.web.id
  address            = each.value
  port               = 8443
}
