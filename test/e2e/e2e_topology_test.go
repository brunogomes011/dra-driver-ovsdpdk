/*
 * Copyright 2026 Red Hat, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package e2e_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Topology Device Plugin", Label(tier2), func() {
	const (
		dpdkPort   = "dpdk-topo0"
		policyName = "e2e-topology-policy"
	)

	var bridge, topologyResource, topologyResourceConfig string

	BeforeEach(func() {
		bridge = plat.topoBridge
		topologyResourceConfig = "topology-" + plat.topoBridge
		topologyResource = driverName + "/topology-" + plat.topoBridge
		if topologyPCI_1 == "" {
			Skip("topology tests require TOPOLOGY_PCI env var")
		}
		if topologyPCI_2 == "" {
			Skip("test requires TOPOLOGY_PCI_SECOND env var (PCI address on different NUMA node)")
		}

	})

	It("no extended resource before DPDK interface exists", func(ctx SpecContext) {
		nodeName := workers[0]
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: policyName, NodeNames: []string{nodeName}, Bridges: []string{bridge}, TopologyResource: topologyResourceConfig}))

		Consistently(func() int64 {
			return nodeAllocatableQuantity(ctx, nodeName, topologyResource)
		}).WithTimeout(10 * time.Second).WithPolling(2 * time.Second).Should(BeZero())
	})

	It("extended resource appears after adding DPDK interface", func(ctx SpecContext) {
		nodeName := workers[0]
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: policyName, NodeNames: []string{nodeName}, Bridges: []string{bridge}, TopologyResource: topologyResourceConfig}))

		addDPDKPort(ctx, nodeName, bridge, dpdkPort, topologyPCI_1)
		DeferCleanup(removeDPDKPort, context.Background(), nodeName, dpdkPort)

		waitForNodeResource(ctx, nodeName, topologyResource)
		Expect(nodeAllocatableQuantity(ctx, nodeName, topologyResource)).To(
			BeNumerically("==", 1024), "DefaultTopologyDeviceCount")
	})

	It("extended resource disappears when DPDK interface is removed", func(ctx SpecContext) {
		nodeName := workers[0]
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: policyName, NodeNames: []string{nodeName}, Bridges: []string{bridge}, TopologyResource: topologyResourceConfig}))

		addDPDKPort(ctx, nodeName, bridge, dpdkPort, topologyPCI_1)
		waitForNodeResource(ctx, nodeName, topologyResource)

		removeDPDKPort(ctx, nodeName, dpdkPort)
		waitForNodeResourceGone(ctx, nodeName, topologyResource)
	})

	It("pod requesting topology resource and DRA claim gets scheduled", func(ctx SpecContext) {
		const (
			claimName = "e2e-topo-claim"
			podName   = "e2e-topo-pod"
		)
		nodeName := workers[0]
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: policyName, NodeNames: []string{nodeName}, Bridges: []string{bridge}, TopologyResource: topologyResourceConfig}))

		addDPDKPort(ctx, nodeName, bridge, dpdkPort, topologyPCI_1)
		DeferCleanup(removeDPDKPort, context.Background(), nodeName, dpdkPort)
		waitForNodeResource(ctx, nodeName, topologyResource)

		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl", claimData{Name: claimName, Namespace: testNamespace, BridgeName: bridge}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{Name: podName, Namespace: testNamespace, ClaimName: claimName, TopologyResource: topologyResource}))

		pod := waitForPodRunning(ctx, testNamespace, podName)
		Expect(pod.Spec.NodeName).To(Equal(nodeName))
	})

	It("DPDK interface removed and re-added — DP recovers", func(ctx SpecContext) {
		const (
			claimName = "e2e-topo-recover-claim"
			podName   = "e2e-topo-recover-pod"
		)
		nodeName := workers[0]
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: policyName, NodeNames: []string{nodeName}, Bridges: []string{bridge}, TopologyResource: topologyResourceConfig}))

		addDPDKPort(ctx, nodeName, bridge, dpdkPort, topologyPCI_1)
		waitForNodeResource(ctx, nodeName, topologyResource)

		removeDPDKPort(ctx, nodeName, dpdkPort)
		waitForNodeResourceGone(ctx, nodeName, topologyResource)

		addDPDKPort(ctx, nodeName, bridge, dpdkPort, topologyPCI_1)
		DeferCleanup(removeDPDKPort, context.Background(), nodeName, dpdkPort)
		waitForNodeResource(ctx, nodeName, topologyResource)

		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl", claimData{Name: claimName, Namespace: testNamespace, BridgeName: bridge}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{Name: podName, Namespace: testNamespace, ClaimName: claimName, TopologyResource: topologyResource}))

		pod := waitForPodRunning(ctx, testNamespace, podName)
		Expect(pod.Spec.NodeName).To(Equal(nodeName))
	})

	It("multiple DPDK interfaces with different NUMAs — deviceplugin not started, driver logs error", func(ctx SpecContext) {
		const (
			dpdkPort1 = "dpdk-multi-numa-0"
			dpdkPort2 = "dpdk-multi-numa-1"
		)
		nodeName := workers[0]

		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-topo-multinuma-policy", NodeNames: []string{nodeName}, Bridges: []string{bridge}, TopologyResource: topologyResourceConfig}))

		addDPDKPort(ctx, nodeName, bridge, dpdkPort1, topologyPCI_1)
		waitForNodeResource(ctx, nodeName, topologyResource)

		addDPDKPort(ctx, nodeName, bridge, dpdkPort2, topologyPCI_2)
		DeferCleanup(removeDPDKPort, context.Background(), nodeName, dpdkPort1)
		DeferCleanup(removeDPDKPort, context.Background(), nodeName, dpdkPort2)

		waitForNodeResourceGone(ctx, nodeName, topologyResource)
	})
})
