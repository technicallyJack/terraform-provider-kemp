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

## Identifying objects

The LoadMaster renumbers its virtual services whenever global configuration changes (a certificate or cipher set change, for example), so its indexes can't identify anything for long. The provider uses stable references instead and looks up the current index right before each call:

| Object | `id` |
|---|---|
| virtual service | `<protocol>/<address>/<port>`, e.g. `tcp/10.0.253.2/443` |
| SubVS | `<parent id>/sub/<slot>`, where slot is the SubVS's real server index on the parent |
| real server | `<virtual service or SubVS id>/rs/<index>` (real server indexes don't change) |

Refer to virtual services and SubVSs from other resources by `id`. The `index` attributes are informational only.

## Upgrading from 0.1.x

0.2.0 replaces index references with stable ones:

- `kemp_sub_virtual_service`: `parent_index = kemp_virtual_service.x.index` becomes `parent_id = kemp_virtual_service.x.id`
- `kemp_real_server`: `virtual_service_index = ….index` becomes `virtual_service_id = ….id`

Existing state converts on its own: change the configuration, then run a normal `terraform plan`/`apply`. The first refresh turns the saved indexes into references, with nothing replaced. Do this before anything renumbers the appliance, while the saved indexes are still correct, and don't use `-refresh=false` for that first run.

## Releases

Releases are automatic. Every push to `main` on Gitea runs `.gitea/workflows/release.yaml`, which:

1. works out the next version from the [Conventional Commits](https://www.conventionalcommits.org/) since the last tag (`scripts/next-version.sh`): `feat` bumps the minor version, `fix`/`perf` the patch version, and a breaking change (`type!:` or a `BREAKING CHANGE:` footer) the major version, or the minor version while below 1.0. Pushes containing only other types (`docs`, `chore`, `test`, `build`, ...) don't release.
2. tags the commit on Gitea, then pushes `main` and the `v*` tags to [GitHub](https://github.com/technicallyJack/terraform-provider-kemp).
3. On GitHub, `.github/workflows/release.yml` runs the unit tests, then goreleaser builds, signs and publishes the release, which the Terraform Registry picks up.

`make test-scripts` tests the version calculation.

## Local development

Point Terraform at your locally built binary with a dev override in `~/.terraformrc`:

```hcl
provider_installation {
  dev_overrides {
    "technicallyjack/kemp" = "/home/<you>/go/bin"
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

## Resources

- `kemp_virtual_service`: manages a virtual service (address, port, protocol, nickname, enabled, type, scheduling method, health checks, persistence)
- `kemp_sub_virtual_service`: manages a SubVS behind a parent virtual service (same settings as a virtual service, plus weight, limit and enabled on the parent)
- `kemp_real_server`: manages a real server on a virtual service or SubVS
- `kemp_match_rule`: content matching rule (URL or header) for content switching and flags
- `kemp_header_rule`: adds, deletes or replaces an HTTP header
- `kemp_url_rule`: rewrites the request URL (address, port, forward, weight, limit, enabled)

## Acceptance tests

Set `KEMP_HOST`, `KEMP_API_KEY` (or username/password) and `KEMP_INSECURE` as needed, then `make testacc`.
Resource tests also need `KEMP_TEST_VS_ADDRESS`: an unused IP the LoadMaster can claim. They are skipped without it.
Rule tests only create `tfacc_*` rules, which do nothing until attached. `make sweep` also removes leftover `tfacc_*` rules.
Real server tests point backends at `KEMP_TEST_RS_ADDRESS` and the next address up (default `10.0.254.250`/`.251`); those only receive health checks.
