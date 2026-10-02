terraform {
  required_providers {
    kemp = {
      source = "technicallyjack/kemp"
    }
  }
}

# Credentials can also come from KEMP_HOST, KEMP_API_KEY,
# KEMP_USERNAME, KEMP_PASSWORD and KEMP_INSECURE.
provider "kemp" {
  host     = "loadmaster.example.com"
  api_key  = var.kemp_api_key
  insecure = true

  # The management API drops connections when it gets too many at once; larger
  # appliances may cope with more.
  # max_concurrent_requests = 4
}

variable "kemp_api_key" {
  type      = string
  sensitive = true
}
