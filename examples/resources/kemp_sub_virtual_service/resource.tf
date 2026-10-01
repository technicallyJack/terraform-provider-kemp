resource "kemp_virtual_service" "ingress" {
  address  = "10.0.253.2"
  port     = "443"
  nickname = "k8s_https"
  type     = "http"
}

resource "kemp_sub_virtual_service" "cluster_a" {
  parent_index = kemp_virtual_service.ingress.index
  nickname     = "cluster_a"
  type         = "http"
  check_type   = "https"
  check_port   = 30443
}

resource "kemp_real_server" "cluster_a" {
  for_each = toset(["10.0.254.30", "10.0.254.31", "10.0.254.32"])

  virtual_service_index = kemp_sub_virtual_service.cluster_a.index
  address               = each.value
  port                  = 30443
}
