# Terraform Provider for Kemp LoadMaster

Manages Kemp (Progress) LoadMaster appliances through the LoadMaster JSON API (`POST /accessv2`).

## Layout

```
main.go                       provider entrypoint
internal/client/              LoadMaster API client (no Terraform deps)
internal/provider/            provider, resources, data sources
examples/                     example configs (also used by tfplugindocs)
```

## Requirements

- Go 1.23+
- Terraform 1.5+
- A LoadMaster with the API enabled (Certificates & Security > Remote Access > Enable API Interface)

## Building

```sh
make build      # compile
make install    # install binary into $GOPATH/bin
make test       # unit tests
make testacc    # acceptance tests against a real LoadMaster
```

## Local development

Point Terraform at your locally built binary with a dev override in `~/.terraformrc`:

```hcl
provider_installation {
  dev_overrides {
    "jwinkler/kemp" = "/home/<you>/go/bin"
  }
  direct {}
}
```

Then run `make install` and use `terraform plan` (skip `terraform init`) in any config that uses the provider.

## Configuration

| Attribute  | Env var         | Notes                                   |
|------------|-----------------|-----------------------------------------|
| `host`     | `KEMP_HOST`     | Hostname/IP, optionally with port       |
| `api_key`  | `KEMP_API_KEY`  | Takes precedence over username/password |
| `username` | `KEMP_USERNAME` |                                         |
| `password` | `KEMP_PASSWORD` |                                         |
| `insecure` | `KEMP_INSECURE` | Skip TLS verification (self-signed certs) |

## Data sources

- `kemp_virtual_services`: lists all virtual services
