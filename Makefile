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

build:build-plugins test
	go fmt ./...
	make lint
	go build -o ./build/cloud-event-proxy cmd/main.go

build-only:
	go build -mod=readonly -o ./build/cloud-event-proxy cmd/main.go

build-examples:
	go build -mod=readonly -o ./build/cloud-native-event-consumer ./examples/consumer/main.go
	go build -mod=readonly -o ./build/cloud-native-event-producer ./examples/producer/main.go

lint:
	golint `go list ./... | grep -v vendor`
	golangci-lint run

build-plugins:
	go build -mod=readonly -o plugins/amqp_plugin.so -buildmode=plugin plugins/amqp/amqp_plugin.go
	go build -mod=readonly -o plugins/ptp_operator_plugin.so -buildmode=plugin plugins/ptp_operator/ptp_operator_plugin.go


build-amqp-plugin:
	go build -mod=readonly -o plugins/amqp_plugin.so -buildmode=plugin plugins/amqp/amqp_plugin.go

build-ptp-operator-plugin:
	go build -mod=readonly -o plugins/ptp_operator_plugin.so -buildmode=plugin plugins/ptp_operator/ptp_operator_plugin.go

run:
	go run cmd/main.go

run-producer:
	go run examples/producer/main.go

run-consumer:
	go run examples/consumer/main.go

test:
	go test ./...  -coverprofile=cover.out


