# Completes the chain served with certificates, and is trusted as a CA for
# client certificate authentication.
resource "kemp_intermediate_certificate" "issuer" {
  name        = "ZeroSSL_ECC_DV_CA_2"
  certificate = file("${path.module}/zerossl-ecc-dv-ca-2.crt")
}
