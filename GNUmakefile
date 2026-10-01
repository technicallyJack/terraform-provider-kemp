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

generate:
	go tool tfplugindocs generate --provider-name kemp

.PHONY: default build install fmt lint test testacc generate
