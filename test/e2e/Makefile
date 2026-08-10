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

# --- kubevirtci configuration -----------------------------------------------
#
# kubevirtci uses rootful podman to run virtualised cluster nodes.
# All KUBEVIRT_* variables can be overridden from the environment.

KUBEVIRTCI_PATH        := $(CURDIR)/kubevirtci/cluster-up/
KUBEVIRTCI_CONFIG_PATH := $(CURDIR)/_ci-configs
KUBEVIRT_PROVIDER      ?= k8s-1.36
KUBEVIRT_MEMORY_SIZE   ?= 8192M
KUBEVIRT_HUGEPAGES_2M  ?= 1024

# Environment passed to kubevirtci scripts.  Defined once, used by every
# kubevirtci target via: $(KUBEVIRTCI_ENV) && <command>
define KUBEVIRTCI_ENV
export KUBEVIRTCI_CONFIG_PATH=$(KUBEVIRTCI_CONFIG_PATH) \
       KUBEVIRTCI_TAG=2607140659-baf5e8b3 \
       KUBEVIRT_PROVIDER=$(KUBEVIRT_PROVIDER) \
       KUBEVIRT_NUM_NODES=3 \
       KUBEVIRT_MEMORY_SIZE=$(KUBEVIRT_MEMORY_SIZE) \
       KUBEVIRT_HUGEPAGES_2M=$(KUBEVIRT_HUGEPAGES_2M) \
       CONTAINER_HOST=unix:///run/podman/podman.sock
endef

  .PHONY: test test-openshift test-tier1 test-tier2 \
        test-tier1-openshift test-tier2-openshift \
        build-ovs-image deploy-ovs \
        cluster-up cluster-down cluster-kubeconfig podman-check \
        cluster-up-kind cluster-down-kind

  # --- Test -------------------------------------------------------------------

  # Run e2e tests.  Expects the driver and OVS to be deployed and KUBECONFIG set.
  #
  # Optional env vars:
  #   TOPOLOGY_PCI  — PCI address of a virtio NIC on worker1; enables topology tests.
  #   GINKGO_ARGS   — extra args passed to `go test`, e.g. a --ginkgo.label-filter.
test:
	go test -count=1 -v -timeout $(E2E_TIMEOUT) . $(GINKGO_ARGS)

test-openshift:
	E2E_PLATFORM=openshift $(MAKE) test E2E_TIMEOUT=60m GINKGO_ARGS='$(GINKGO_ARGS)'

# --- Tiered subsets (upstream/kind) ------------------------------------------

test-tier1:
	$(MAKE) test GINKGO_ARGS='-args --ginkgo.label-filter="tier1"'

test-tier2:
	$(MAKE) test GINKGO_ARGS='-args --ginkgo.label-filter="tier2"'

# --- Tiered subsets (OpenShift) -----------------------------------------------

test-openshift-tier1:
	$(MAKE) test-openshift GINKGO_ARGS='-args --ginkgo.label-filter="tier1"'

test-openshift-tier2:
	$(MAKE) test-openshift GINKGO_ARGS='-args --ginkgo.label-filter="tier2"'

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

# --- kubevirtci cluster -----------------------------------------------------
#
# Full workflow:
#   make cluster-up                          # create kubevirtci cluster
#   make deploy-ovs                          # deploy containerised OVS
#   make -C ../.. deploy                     # deploy the driver
#   export KUBECONFIG=$$(make cluster-kubeconfig)
#   make test                                # run e2e tests
#   make cluster-down                        # tear down

podman-check:
	@podman --remote info >/dev/null 2>&1 \
		|| (echo "ERROR: rootful podman is not reachable." >&2; \
		    echo "" >&2; \
		    echo "kubevirtci requires rootful podman with a listening socket." >&2; \
		    echo "See: https://github.com/kubevirt/kubevirtci/blob/main/PODMAN.md" >&2; \
		    echo "" >&2; \
		    exit 1)

cluster-up: podman-check
	$(KUBEVIRTCI_ENV) && $(KUBEVIRTCI_PATH)up.sh

cluster-down: podman-check
	$(KUBEVIRTCI_ENV) && $(KUBEVIRTCI_PATH)down.sh

cluster-kubeconfig:
	@echo $(KUBEVIRTCI_CONFIG_PATH)/$(KUBEVIRT_PROVIDER)/.kubeconfig

# --- Kind cluster -----------------------------------------------------------
#
# Lightweight alternative using kind (no hugepages / NUMA simulation).
#
# Full workflow:
#   make cluster-up-kind                     # create cluster, build+load images
#   make deploy-ovs                          # deploy containerised OVS
#   make -C ../.. deploy                     # deploy the driver
#   make test                                # run e2e tests
#   make cluster-down-kind                   # tear down

cluster-up-kind: build-ovs-image
	$(MAKE) -C $(REPO_ROOT) build-image CONTAINER_TOOL=docker
	kind create cluster \
		--name $(E2E_CLUSTER) \
		--config $(MANIFESTS)/kind-config.yaml
	kind load docker-image $(E2E_IMAGE) --name $(E2E_CLUSTER)
	kind load docker-image $(OVS_IMAGE) --name $(E2E_CLUSTER)

cluster-down-kind:
	kind delete cluster --name $(E2E_CLUSTER)
