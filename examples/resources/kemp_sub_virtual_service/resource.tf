resource "kemp_virtual_service" "ingress" {
  address  = "10.0.253.2"
  port     = "443"
  nickname = "k8s_https"
  type     = "http"
}

resource "kemp_sub_virtual_service" "cluster_a" {
  parent_id  = kemp_virtual_service.ingress.id
  nickname   = "cluster_a"
  type       = "http"
  check_type = "https"
  check_port = 30443
}

resource "kemp_real_server" "cluster_a" {
  for_each = toset(["10.0.254.30", "10.0.254.31", "10.0.254.32"])

  virtual_service_id = kemp_sub_virtual_service.cluster_a.id
  address            = each.value
  port               = 30443
}

# Content switching: send /api/ requests on the same parent to their own SubVS.
resource "kemp_match_rule" "api" {
  name       = "api_path"
  pattern    = "/api/"
  match_type = "prefix"
}

resource "kemp_sub_virtual_service" "api" {
  parent_id   = kemp_virtual_service.ingress.id
  nickname    = "api"
  type        = "http"
  match_rules = [kemp_match_rule.api.name]
}
