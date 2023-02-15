.PHONY: build, test

#for examples
# Current  version
VERSION ?=latest
# Default image tag

IMG ?= quay.io/openshift/origin-cloud-event-proxy:$(VERSION)
CONSUMER_IMG ?= quay.io/redhat-cne/cloud-event-consumer:$(VERSION)

# Export GO111MODULE=on to enable project to be built from within GOPATH/src
export GO111MODULE=on
export CGO_ENABLED=1
export GOFLAGS=-mod=vendor
export COMMON_GO_ARGS=-race
export GOOS=linux

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

deps-update:
	go get github.com/redhat-cne/sdk-go@$(branch) && \
	go get github.com/redhat-cne/rest-api@$(branch) && \
	go mod tidy && \
	go mod vendor

build:build-plugins test
	go fmt ./...
	make lint
	go build -o ./build/cloud-event-proxy cmd/main.go

build-only:
	go build -o ./build/cloud-event-proxy cmd/main.go

build-examples:
	go build -o ./build/cloud-event-consumer ./examples/consumer/main.go

lint:
	golangci-lint run

build-plugins:
	go build  -o plugins/amqp_plugin.so -buildmode=plugin plugins/amqp/amqp_plugin.go
	go build  -o plugins/ptp_operator_plugin.so -buildmode=plugin plugins/ptp_operator/ptp_operator_plugin.go
	go build -o plugins/http_plugin.so -buildmode=plugin plugins/http/http_plugin.go
	go build  -o plugins/mock_plugin.so -buildmode=plugin plugins/mock/mock_plugin.go

build-amqp-plugin:
	go build -o plugins/amqp_plugin.so -buildmode=plugin plugins/amqp/amqp_plugin.go

build-ptp-operator-plugin:
	go build -o plugins/ptp_operator_plugin.so -buildmode=plugin plugins/ptp_operator/ptp_operator_plugin.go

build-http-plugin:
	go build -o plugins/http_plugin.so -buildmode=plugin plugins/http/http_plugin.go

build-mock-plugin:
	go build -o plugins/mock_plugin.so -buildmode=plugin plugins/mock/mock_plugin.go

run:
	go run cmd/main.go

run-consumer:
	go run examples/consumer/main.go

test:
	go test ./... --tags=unittests -coverprofile=cover.out

functests:
	SUITE=./test/cne hack/run-functests.sh

# Deploy all in the configured Kubernetes cluster in ~/.kube/config
deploy-consumer:kustomize
	cd ./examples/manifests && $(KUSTOMIZE) edit set image cloud-event-sidecar=${IMG} && $(KUSTOMIZE) edit set image cloud-event-consumer=${CONSUMER_IMG}
	$(KUSTOMIZE) build ./examples/manifests | kubectl apply -f -

# Deploy all in the configured Kubernetes cluster in ~/.kube/config
undeploy-consumer:kustomize
	cd ./examples/manifests  && $(KUSTOMIZE) edit set image cloud-event-sidecar=${IMG} && $(KUSTOMIZE) edit set image cloud-event-consumer=${CONSUMER_IMG}
	$(KUSTOMIZE) build ./examples/manifests | kubectl delete -f -

# For GitHub Actions CI
gha:
	go build -o plugins/amqp_plugin.so -buildmode=plugin plugins/amqp/amqp_plugin.go
	go build -o plugins/ptp_operator_plugin.so -buildmode=plugin plugins/ptp_operator/ptp_operator_plugin.go
	go build -o plugins/mock_plugin.so -buildmode=plugin plugins/mock/mock_plugin.go
	go build -o plugins/http_plugin.so -buildmode=plugin plugins/http/http_plugin.go
	go test ./... --tags=unittests -coverprofile=cover.out

docker-build: #test ## Build docker image with the manager.
	docker build -t ${IMG} .

docker-push: ## Push docker image with the manager.
	docker push ${IMG}

docker-build-consumer: #test ## Build docker image with the manager.
	docker build -f ./examples/consumer.Dockerfile -t ${CONSUMER_IMG} .

docker-push-consumer: ## Push docker image with the manager.
	docker push ${CONSUMER_IMG}

fmt: ## Go fmt your code
	hack/gofmt.sh

fmt-code: ## Run go fmt against code.
	go fmt ./...

vet: ## Run go vet against code.
	go vet ./...
