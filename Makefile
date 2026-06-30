# Copyright 2026 Red Hat, Inc.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

DRIVER_NAME := dra-driver-ovsdpdk
MODULE      := github.com/amorenoz/dra-driver-ovsdpdk

GOLANG_VERSION        ?= 1.26
GOLANGCI_LINT_VERSION ?= v2.7.2
CONTROLLER_GEN_VERSION ?= v0.21.0
MOCKERY_VERSION        ?= v2.53.6

CONTAINER_TOOL ?= podman
REGISTRY       ?= quay.io/amorenoz
IMAGE_NAME     ?= $(REGISTRY)/$(DRIVER_NAME)
IMAGE_TAG      ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

GOOS   ?= linux
GOARCH ?= amd64

BIN_DIR := $(CURDIR)/bin

APIS := ovsdpdkdra/v1alpha1

.PHONY: all build binary check vet lint test coverage vendor generate generate-deepcopy generate-crds generate-mocks build-image push-image deploy undeploy

all: check test build

build:
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build ./...

binary:
	GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -ldflags "-s -w" \
		-o $(DRIVER_NAME) \
		$(MODULE)/cmd/$(DRIVER_NAME)

check: vet lint

GOLANGCI_LINT := $(BIN_DIR)/golangci-lint
lint: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run ./...

$(GOLANGCI_LINT):
	GOBIN=$(BIN_DIR) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

vet:
	go vet ./...

test:
	go test -v -coverprofile=coverage.out ./cmd/... ./pkg/...

coverage: test
	go tool cover -func=coverage.out

vendor:
	go mod vendor

# ---- code generation ------------------------------------------------------

CONTROLLER_GEN := $(BIN_DIR)/controller-gen

generate: generate-deepcopy generate-crds generate-mocks

generate-deepcopy: $(CONTROLLER_GEN)
	for api in $(APIS); do \
		rm -f $(CURDIR)/pkg/api/$${api}/zz_generated.deepcopy.go; \
		$(CONTROLLER_GEN) \
			object:headerFile=$(CURDIR)/hack/boilerplate.generatego.txt \
			paths=$(CURDIR)/pkg/api/$${api}/; \
	done

generate-crds: $(CONTROLLER_GEN)
	@mkdir -p $(CURDIR)/deployments/crds/
	$(CONTROLLER_GEN) \
		crd \
		paths=$(CURDIR)/pkg/api/ovsdpdkdra/v1alpha1/ \
		output:crd:dir=$(CURDIR)/deployments/crds/

$(CONTROLLER_GEN):
	GOBIN=$(BIN_DIR) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION)

MOCKERY := $(BIN_DIR)/mockery

generate-mocks: $(MOCKERY)
	$(MOCKERY)

$(MOCKERY):
	GOBIN=$(BIN_DIR) go install github.com/vektra/mockery/v2@$(MOCKERY_VERSION)

build-image:
	$(CONTAINER_TOOL) build \
		--build-arg GOLANG_VERSION=$(GOLANG_VERSION) \
		-t $(IMAGE_NAME):$(IMAGE_TAG) \
		-f $(CURDIR)/Dockerfile \
		$(CURDIR)

push-image: build-image
	$(CONTAINER_TOOL) push $(IMAGE_NAME):$(IMAGE_TAG)

# ---- cluster deployment ---------------------------------------------------

deploy: ## Deploy the driver into the current kubectl context
	kubectl apply -f $(CURDIR)/deployments/crds/
	kubectl apply -f $(CURDIR)/deployments/namespace.yaml
	kubectl apply -f $(CURDIR)/deployments/rbac.yaml
	sed 's|IMAGE|$(IMAGE_NAME):$(IMAGE_TAG)|g' \
		$(CURDIR)/deployments/daemonset.yaml | kubectl apply -f -

undeploy: ## Remove the driver from the current kubectl context
	kubectl delete --ignore-not-found -f $(CURDIR)/deployments/rbac.yaml
	kubectl delete --ignore-not-found -f $(CURDIR)/deployments/namespace.yaml
	kubectl delete --ignore-not-found -f $(CURDIR)/deployments/crds/
