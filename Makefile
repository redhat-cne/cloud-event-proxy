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
	go build -o cloud-event-proxy cmd/main.go

build-only:
	go build -mod=readonly -o cloud-event-proxy cmd/main.go

lint:
	golint `go list ./... | grep -v vendor`
	golangci-lint run

build-plugins:
	go build -mod=readonly -o plugins/rest_api_plugin.so -buildmode=plugin plugins/rest_api/rest_api_plugin.go
	go build -mod=readonly -o plugins/amqp_plugin.so -buildmode=plugin plugins/amqp/amqp_plugin.go

build-rest-plugin:
	go build -mod=readonly -o plugins/rest_api_plugin.so -buildmode=plugin plugins/rest_api/rest_api_plugin.go

build-amqp-plugin:
	go build -mod=readonly -o plugins/amqp_plugin.so -buildmode=plugin plugins/amqp/amqp_plugin.go

run:
	go run cmd/main.go

run-producer:
	go run examples/producer/main.go

run-consumer:
	go run examples/consumer/main.go

test:
	go test ./...  -coverprofile=cover.out


