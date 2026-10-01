VERSION ?= 0.1.0
OS_ARCH := $(shell go env GOOS)_$(shell go env GOARCH)
LOCAL_PLUGIN_DIR := $(HOME)/.terraform.d/plugins/registry.terraform.io/jwinkler/kemp/$(VERSION)/$(OS_ARCH)

default: build

build:
	go build -v ./...

install:
	go install -v ./...

# Installs into Terraform's implied local mirror so configs can use
# source = "jwinkler/kemp", version = "$(VERSION)" with a normal init.
# Bump VERSION for each rebuild you want picked up: init records the
# binary's checksum in .terraform.lock.hcl and rejects a changed binary
# under the same version.
install-local:
	go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o $(LOCAL_PLUGIN_DIR)/terraform-provider-kemp_v$(VERSION) .
	@echo "installed jwinkler/kemp $(VERSION) to $(LOCAL_PLUGIN_DIR)"

fmt:
	gofmt -s -w -e .

lint:
	golangci-lint run

test:
	go test -v -cover -timeout=120s -parallel=10 ./...

# Runs acceptance tests against a real LoadMaster (needs KEMP_* env vars).
testacc:
	TF_ACC=1 go test -v -cover -timeout 120m ./...

# Deletes tf-acc-* virtual services on KEMP_TEST_VS_ADDRESS left by failed acceptance tests.
sweep:
	go test ./internal/provider -v -sweep=loadmaster -timeout 10m

# Retried once: on NFS with stale directory listings, tfplugindocs can miss
# a file while cleaning docs/ and then fail to remove the directory.
generate:
	go tool tfplugindocs generate --provider-name kemp || go tool tfplugindocs generate --provider-name kemp

.PHONY: default build install install-local fmt lint test testacc sweep generate
