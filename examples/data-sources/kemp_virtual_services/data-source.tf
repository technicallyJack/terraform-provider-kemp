data "kemp_virtual_services" "all" {}

output "virtual_services" {
  value = data.kemp_virtual_services.all.virtual_services
}
