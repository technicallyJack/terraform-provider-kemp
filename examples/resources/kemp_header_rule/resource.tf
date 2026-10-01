resource "kemp_header_rule" "env" {
  name   = "add_env_header"
  action = "add"
  header = "X-Environment"
  value  = "production"
}

resource "kemp_header_rule" "host" {
  name    = "rewrite_host"
  action  = "replace"
  header  = "Host"
  pattern = "internal\\.example\\.com"
  value   = "www.example.com"
}

resource "kemp_header_rule" "server" {
  name   = "strip_server_header"
  action = "delete"
  header = "Server"
}
