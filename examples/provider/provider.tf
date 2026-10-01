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

  # The free LoadMaster rate-limits its API; licensed appliances may allow more.
  # max_concurrent_requests = 4
}

variable "kemp_api_key" {
  type      = string
  sensitive = true
}
