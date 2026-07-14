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

CONTAINER_TOOL := docker
REGISTRY       ?= quay.io/amorenoz
IMAGE_NAME     ?= $(REGISTRY)/$(DRIVER_NAME)
IMAGE_TAG      ?= $(shell git -C ../.. describe --tags --always --dirty 2>/dev/null || echo "dev")

E2E_IMAGE   ?= $(IMAGE_NAME):$(IMAGE_TAG)
E2E_TIMEOUT ?= 30m
E2E_CLUSTER ?= dra-ovsdpdk-e2e

# OVS/OVS-DPDK test image built from Dockerfile.ovs (Fedora + openvswitch +
# openvswitch-dpdk).  Build it once with 'make build-ovs-image' and reuse
# across test runs.
OVS_IMAGE ?= $(REGISTRY)/ovs-dpdk-test:latest

MANIFESTS := $(CURDIR)/manifests
REPO_ROOT := $(CURDIR)/../..

.PHONY: test build-ovs-image deploy-ovs cluster-up cluster-down

# --- Test -------------------------------------------------------------------

# Run e2e tests.  Expects the driver and OVS to be deployed and KUBECONFIG set.
#
# Optional env vars:
#   TOPOLOGY_PCI  — PCI address of a virtio NIC on worker1; enables topology tests.
test:
	go test -count=1 -v -timeout $(E2E_TIMEOUT) .

# --- OVS deployment ----------------------------------------------------------

build-ovs-image:
	$(CONTAINER_TOOL) build \
		-t $(OVS_IMAGE) \
		-f Dockerfile.ovs \
		.

OVS_HUGEPAGES ?= false
deploy-ovs:
	-kubectl create namespace $(DRIVER_NAME) 2>/dev/null
	kubectl -n $(DRIVER_NAME) create configmap ovs-config \
		--from-literal=HUGEPAGES=$(OVS_HUGEPAGES) \
		--from-literal=INIT_BRIDGES="br-dpdk0,br-dpdk1,br-dpdk2" \
		--dry-run=client -o yaml | kubectl apply -f -
	sed 's|OVS_IMAGE_PLACEHOLDER|$(OVS_IMAGE)|g' $(MANIFESTS)/ovs-daemonset.yaml \
			| kubectl -n $(DRIVER_NAME) apply -f -
	if [ "$(OVS_HUGEPAGES)" == "true" ]; then \
		kubectl -n $(DRIVER_NAME) patch daemonset ovs --patch-file \
			$(MANIFESTS)/ovs-daemonset-hugepages.json; \
	fi

# --- Kind cluster (optional) -----------------------------------------------
#
# Convenience targets for local development with kind.
#
# Full workflow:
#   make cluster-up                          # create cluster, build+load images
#   make deploy-ovs                          # deploy containerised OVS
#   make -C ../.. deploy                     # deploy the driver
#   make test                                # run e2e tests
#   make cluster-down                        # tear down
#
# Or as a one-liner:
#   make cluster-up deploy-ovs && make -C ../.. deploy && make test

# Create a kind cluster with DRA feature gates and 2 workers, build and load images.
cluster-up: build-ovs-image
	$(MAKE) -C $(REPO_ROOT) build-image CONTAINER_TOOL=docker
	kind create cluster \
		--name $(E2E_CLUSTER) \
		--config $(MANIFESTS)/kind-config.yaml
	kind load docker-image $(E2E_IMAGE) --name $(E2E_CLUSTER)
	kind load docker-image $(OVS_IMAGE) --name $(E2E_CLUSTER)

# Destroy the kind cluster.
cluster-down:
	kind delete cluster --name $(E2E_CLUSTER)
