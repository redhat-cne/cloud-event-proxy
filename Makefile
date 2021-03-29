.PHONY: build

# Export GO111MODULE=on to enable project to be built from within GOPATH/src
export GO111MODULE=on
export CGO_ENABLED=1
export COMMON_GO_ARGS=-race
export GOOS=linux
export GOARCH=amd64
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

build-plugins:
	go build -o plugins/rest_api_plugin.so -buildmode=plugin plugins/rest_api/rest_api_plugin.go
	go build -o plugins/amqp_plugin.so -buildmode=plugin plugins/amqp/amqp_plugin.go

test:
	go test ./...  -coverprofile=cover.out


