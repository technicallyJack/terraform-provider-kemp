# The private key is write-only, so it can come straight from an ephemeral
# resource and is never stored in state.
ephemeral "vault_kv_secret_v2" "web_key" {
  mount = "secret"
  name  = "tls/web"
}

resource "kemp_certificate" "web" {
  name                   = "web_example_com"
  admin_certificate      = false # true: also serve it on the LoadMaster's own web UI and API
  certificate            = file("${path.module}/web.example.com.crt")
  private_key_wo         = ephemeral.vault_kv_secret_v2.web_key.data["private_key"]
  private_key_wo_version = 1 # bump to push a new key with the same certificate
}

output "web_certificate_expires" {
  value = kemp_certificate.web.not_after
}
