set dotenv-load

default:
  @just --list

pre-commit: tidy generate lint
pc: pre-commit

lint:
  @golangci-lint run --fix --build-tags it --timeout 5m

tidy:
  @go mod tidy

generate:
  @go generate ./...

tests:
  @go test -race -covermode=atomic \
    -coverprofile coverage.txt \
    ./...

fmt:
  @gci write --skip-generated --skip-vendor -s standard -s default -s 'prefix(github.com/formancehq/)' -s localmodule .
  @gofmt -s -w .
  @goimports -w .
