.PHONY: build, test

#for examples
# Current  version
VERSION ?=latest
# Default image tag

IMG ?= quay.io/openshift/origin-cloud-event-proxy:$(VERSION)
CONSUMER_IMG ?= quay.io/redhat-cne/cloud-event-consumer:$(VERSION)

export GO111MODULE=on
export CGO_ENABLED=1
export GOFLAGS=-mod=vendor
export COMMON_GO_ARGS=-race

OS := $(shell uname -s)
ifeq ($(OS), Darwin)
export GOOS=darwin
else
export GOOS=linux
endif

ifeq (,$(shell go env GOBIN))
  GOBIN=$(shell go env GOPATH)/bin
else
  GOBIN=$(shell go env GOBIN)
endif

export GOPATH=$(shell go env GOPATH)

##@ Build Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Versions
KUSTOMIZE ?= $(LOCALBIN)/kustomize
KUSTOMIZE_VERSION ?= v4.5.7
KUSTOMIZE_INSTALL_SCRIPT ?= "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"

.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary. If wrong version is installed, it will be removed before downloading.
$(KUSTOMIZE): $(LOCALBIN)
	@if test -x $(LOCALBIN)/kustomize && ! $(LOCALBIN)/kustomize version | grep -q $(KUSTOMIZE_VERSION); then \
		echo "$(LOCALBIN)/kustomize version is not expected $(KUSTOMIZE_VERSION). Removing it before installing."; \
		rm -rf $(LOCALBIN)/kustomize; \
	fi
	test -s $(LOCALBIN)/kustomize || { curl -Ss $(KUSTOMIZE_INSTALL_SCRIPT) | bash -s -- $(subst v,,$(KUSTOMIZE_VERSION)) $(LOCALBIN); }

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
	go build -a -o plugins/ptp_operator_plugin.so -buildmode=plugin plugins/ptp_operator/ptp_operator_plugin.go
	go build -a -o plugins/http_plugin.so -buildmode=plugin plugins/http/http_plugin.go
	go build -a -o plugins/mock_plugin.so -buildmode=plugin plugins/mock/mock_plugin.go

build-ptp-operator-plugin:
	go build -a -o plugins/ptp_operator_plugin.so -buildmode=plugin plugins/ptp_operator/ptp_operator_plugin.go

build-http-plugin:
	go build -a -o plugins/http_plugin.so -buildmode=plugin plugins/http/http_plugin.go

build-mock-plugin:
	go build -a -o plugins/mock_plugin.so -buildmode=plugin plugins/mock/mock_plugin.go

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

undeploy-consumer:kustomize
	cd ./examples/manifests  && $(KUSTOMIZE) edit set image cloud-event-sidecar=${IMG} && $(KUSTOMIZE) edit set image cloud-event-consumer=${CONSUMER_IMG}
	$(KUSTOMIZE) build ./examples/manifests | kubectl delete -f -

deploy-consumer-v2:kustomize
	cd ./examples/manifests/v2 && $(KUSTOMIZE) edit set image cloud-event-consumer=${CONSUMER_IMG}
	$(KUSTOMIZE) build ./examples/manifests/v2 | kubectl apply -f -

undeploy-consumer-v2:kustomize
	cd ./examples/manifests/v2 && $(KUSTOMIZE) edit set image cloud-event-consumer=${CONSUMER_IMG}
	$(KUSTOMIZE) build ./examples/manifests/v2 | kubectl delete -f -

# For GitHub Actions CI
gha:
	mkdir -p $(GOPATH)/src/github.com/redhat-cne/cloud-event-proxy
	rm -rf $(GOPATH)/src/github.com/redhat-cne/cloud-event-proxy/*
	cp -r cmd examples pkg plugins test $(GOPATH)/src/github.com/redhat-cne/cloud-event-proxy
	cp -r vendor/* $(GOPATH)/src
	GO111MODULE=off go build -a -o plugins/ptp_operator_plugin.so -buildmode=plugin plugins/ptp_operator/ptp_operator_plugin.go
	GO111MODULE=off go build -a -o plugins/mock_plugin.so -buildmode=plugin plugins/mock/mock_plugin.go
	GO111MODULE=off go build -a -o plugins/http_plugin.so -buildmode=plugin plugins/http/http_plugin.go
	rm -rf /tmp/sub-store && mkdir -p /tmp/sub-store
	GO111MODULE=off STORE_PATH=/tmp/sub-store go test ./... --tags=unittests -coverprofile=cover.out

docker-build: #test ## Build docker image with the manager.
	docker build --no-cache -t ${IMG} .

docker-push: ## Push docker image with the manager.
	docker push ${IMG}

docker-build-consumer: #test ## Build docker image with the manager.
	docker build -f ./examples/consumer.Dockerfile -t ${CONSUMER_IMG} .

docker-push-consumer: ## Push docker image with the manager.
	docker push ${CONSUMER_IMG}

podman-build: #test ## Build docker image with the manager.
	podman build --no-cache -t ${IMG} .

podman-push: ## Push docker image with the manager.
	podman push ${IMG}

podman-build-consumer: #test ## Build docker image with the manager.
	podman build -f ./examples/consumer.Dockerfile -t ${CONSUMER_IMG} .

podman-push-consumer: ## Push docker image with the manager.
	podman push ${CONSUMER_IMG}

fmt: ## Go fmt your code
	hack/gofmt.sh

fmt-code: ## Run go fmt against code.
	go fmt ./...

vet: ## Run go vet against code.
	go vet ./...

generate-api:
	hack/verify-codegen.sh
	rm -rf github.com

install-tools:
	hack/install-kubebuilder-tools.sh
