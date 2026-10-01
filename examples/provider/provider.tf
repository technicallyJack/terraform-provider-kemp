terraform {
  required_providers {
    kemp = {
      source = "jwinkler/kemp"
    }
  }
}

# Credentials can also come from KEMP_HOST, KEMP_API_KEY,
# KEMP_USERNAME, KEMP_PASSWORD and KEMP_INSECURE.
provider "kemp" {
  host     = "loadmaster.example.com"
  api_key  = var.kemp_api_key
  insecure = true
}

variable "kemp_api_key" {
  type      = string
  sensitive = true
}
