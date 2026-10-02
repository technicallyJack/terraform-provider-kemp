# Imported by name. The private key can't be read back, so set private_key_wo
# in the configuration; the first apply re-sends the certificate once.
terraform import kemp_certificate.web web_example_com
