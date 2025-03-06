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
    -tags it \
    ./...
  @cat coverage.txt | grep -v debug.go | grep -v "/machine/" > coverage2.txt
  @mv coverage2.txt coverage.txt
