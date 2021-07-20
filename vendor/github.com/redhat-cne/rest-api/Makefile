.PHONY: build

# Export GO111MODULE=on to enable project to be built from within GOPATH/src
export GO111MODULE=on
export CGO_ENABLED=0
export COMMON_GO_ARGS=-race

ifeq (,$(shell go env GOBIN))
  GOBIN=$(shell go env GOPATH)/bin
else
  GOBIN=$(shell go env GOBIN)
endif

build:test
	go fmt ./...
	make lint

lint:
	golint `go list ./... | grep -v vendor`
	golangci-lint run

test:
	go test ./...  -coverprofile=cover.out




