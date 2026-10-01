default: build

build:
	go build -v ./...

install:
	go install -v ./...

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

# Retried once: on NFS, files held open (e.g. by an editor) can make the
# docs directory cleanup fail transiently.
generate:
	go tool tfplugindocs generate --provider-name kemp || go tool tfplugindocs generate --provider-name kemp

.PHONY: default build install fmt lint test testacc sweep generate
