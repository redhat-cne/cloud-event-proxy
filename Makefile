.PHONY: build

#for examples
# Current  version
VERSION ?=latest
# Default image tag

SIDECAR_IMG ?= quay.io/aneeshkp/cloud-event-proxy:$(VERSION)
PRODUCER_IMG ?= quay.io/aneeshkp/cloud-native-event-producer:$(VERSION)
CONSUMER_IMG ?= quay.io/aneeshkp/cloud-native-event-consumer:$(VERSION)


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

kustomize:
ifeq (, $(shell which kustomize))
		@{ \
		set -e ;\
		KUSTOMIZE_GEN_TMP_DIR=$$(mktemp -d) ;\
		cd $$KUSTOMIZE_GEN_TMP_DIR ;\
		go mod init tmp ;\
		go get sigs.k8s.io/kustomize/kustomize/v3@v3.5.4 ;\
		rm -rf $$KUSTOMIZE_GEN_TMP_DIR ;\
		}
KUSTOMIZE=$(GOBIN)/kustomize
else
KUSTOMIZE=$(shell which kustomize)
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

# Deploy all in the configured Kubernetes cluster in ~/.kube/config
deploy-example:kustomize
	cd ./examples/manifests && $(KUSTOMIZE) edit set image cloud-event-proxy=${SIDECAR_IMG} && $(KUSTOMIZE) edit set image  cloud-native-event-producer=${PRODUCER_IMG} && $(KUSTOMIZE) edit set image  cloud-native-event-consumer=${CONSUMER_IMG}
	$(KUSTOMIZE) build ./examples/manifests | kubectl apply -f -

# Deploy all in the configured Kubernetes cluster in ~/.kube/config
undeploy-example:kustomize
	cd ./examples/manifests  && $(KUSTOMIZE) edit set image cloud-event-proxy=${SIDECAR_IMG} && $(KUSTOMIZE) edit set image  cloud-native-event-producer=${PRODUCER_IMG} && $(KUSTOMIZE) edit set image  cloud-native-event-consumer=${CONSUMER_IMG}
	$(KUSTOMIZE) build ./examples/manifests | kubectl delete -f -

# For Github CI
travis:
	make lint
	go build -o plugins/amqp_plugin.so -buildmode=plugin plugins/amqp/amqp_plugin.go
	go build -o plugins/ptp_operator_plugin.so -buildmode=plugin plugins/ptp_operator/ptp_operator_plugin.go
	go test ./...  -coverprofile=cover.out
