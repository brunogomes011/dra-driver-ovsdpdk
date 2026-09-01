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
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Vhost-user port lifecycle", Label(tier1), func() {
	const base = "e2e-vhost"

	var pod *corev1.Pod
	var ports []string
	var uid string

	BeforeEach(func(ctx SpecContext) {
		applyPolicy(ctx, base, workers[0], plat.bridge0)
		pod, uid = applyClaimPod(ctx, base, claimData{BridgeName: plat.bridge0})
		ports = waitForOvsPorts(ctx, pod.Spec.NodeName, uid)
	})

	It("OVS port exists after pod is running", func(_ SpecContext) {
		Expect(ports).NotTo(BeEmpty())
	})

	It("OVS port is on the correct bridge", func(ctx SpecContext) {
		got, err := ovsExec(ctx, pod.Spec.NodeName, "ovs-vsctl", "port-to-br", ports[0])
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(plat.bridge0))
	})

	It("interface type is dpdkvhostuserclient", func(ctx SpecContext) {
		got, err := ovsExec(ctx, pod.Spec.NodeName, "ovs-vsctl", "get", "interface", ports[0], "type")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("dpdkvhostuserclient"))
	})

	It("vhost-server-path matches the socket path", func(ctx SpecContext) {
		got, err := ovsExec(ctx, pod.Spec.NodeName,
			"ovs-vsctl", "get", "interface", ports[0], "options:vhost-server-path")
		Expect(err).NotTo(HaveOccurred())
		wantDir := socketDirPath(string(pod.UID), base+"-claim", "vhost-port")
		Expect(strings.Trim(got, `"`)).To(Equal(filepath.Join(wantDir, "vhost.sock")))
	})

	It("OVS port is removed after pod deletion", func(ctx SpecContext) {
		nodeName := pod.Spec.NodeName
		deletePodAndWait(ctx, testNamespace, base+"-pod")
		Eventually(func(g Gomega) {
			p, err := ovsPortsForClaim(ctx, nodeName, uid)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p).To(BeEmpty())
		}).WithTimeout(30 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
	})

	It("two claims on the same bridge get distinct ports", func(ctx SpecContext) {
		const (
			claim0 = "e2e-vhost-multi-0"
			claim1 = "e2e-vhost-multi-1"
			pod0   = "e2e-pod-vhost-multi-0"
			pod1   = "e2e-pod-vhost-multi-1"
		)
		for _, name := range []string{claim0, claim1} {
			applyAndCleanup(mustRenderManifest("claim.yaml.tmpl", claimData{Name: name, Namespace: testNamespace, BridgeName: plat.bridge0}))
		}
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl", podData{Name: pod0, Namespace: testNamespace, ClaimName: claim0}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl", podData{Name: pod1, Namespace: testNamespace, ClaimName: claim1}))

		runPod0 := waitForPodRunning(ctx, testNamespace, pod0)
		runPod1 := waitForPodRunning(ctx, testNamespace, pod1)

		rc0, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claim0, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		rc1, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claim1, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		ports0 := waitForOvsPorts(ctx, runPod0.Spec.NodeName, string(rc0.UID))
		ports1 := waitForOvsPorts(ctx, runPod1.Spec.NodeName, string(rc1.UID))

		if runPod0.Spec.NodeName == runPod1.Spec.NodeName {
			Expect(ports0[0]).NotTo(Equal(ports1[0]), "two claims got the same OVS port name")
		}
	})
})

var _ = Describe("Port on non-existent bridge (race)", Label(tier2), func() {
	const (
		bridge    = "br-race-create"
		claimName = "e2e-race-create-claim"
		podName   = "e2e-pod-race-create"
	)

	It("pod does not reach Running and no socket dir is leaked when the bridge disappears before prepare", func(ctx SpecContext) {
		nodeName := workers[0]
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-race-create-policy", NodeNames: []string{nodeName}, Bridges: []string{bridge}}))
		addBridgeToOVS(ctx, nodeName, bridge)
		DeferCleanup(deleteBridgeFromOVS, context.Background(), nodeName, bridge)
		waitForDeviceInSlice(ctx, nodeName, bridge)

		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{Name: claimName, Namespace: testNamespace, BridgeName: bridge}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{Name: podName, Namespace: testNamespace, ClaimName: claimName, NodeName: nodeName}))

		pod, err := cs.CoreV1().Pods(testNamespace).Get(ctx, podName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		podUID := string(pod.UID)

		// Race: remove the bridge immediately after the pod is created,
		// aiming to land the deletion before NodePrepareResources runs.
		deleteBridgeFromOVS(ctx, nodeName, bridge)

		Consistently(func(g Gomega) {
			p, err := cs.CoreV1().Pods(testNamespace).Get(ctx, podName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p.Status.Phase).NotTo(Equal(corev1.PodRunning))
		}).WithTimeout(20 * time.Second).WithPolling(3 * time.Second).Should(Succeed())

		socketDir := socketDirPath(podUID, claimName, "vhost-port")
		Expect(dirExists(ctx, nodeName, socketDir)).To(BeFalse(),
			"socket dir must be rolled back after a failed prepare")
	})
})

var _ = Describe("Port already gone before deletion", Label(tier2), func() {
	const (
		claimName = "e2e-port-gone-claim"
		podName   = "e2e-pod-port-gone"
	)

	It("unprepare succeeds and the socket dir is still removed when the OVS port is already gone", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-port-gone-policy", NodeNames: []string{workers[0]}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{Name: claimName, Namespace: testNamespace, BridgeName: plat.bridge0}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{Name: podName, Namespace: testNamespace, ClaimName: claimName}))
		pod := waitForPodRunning(ctx, testNamespace, podName)

		claim, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claimName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		ports := waitForOvsPorts(ctx, pod.Spec.NodeName, string(claim.UID))

		removeDPDKPort(ctx, pod.Spec.NodeName, ports[0])

		socketDir := socketDirPath(string(pod.UID), claimName, "vhost-port")
		deletePodAndWait(ctx, testNamespace, podName)

		Expect(dirExists(ctx, pod.Spec.NodeName, socketDir)).To(BeFalse(),
			"socket dir must be removed even though the OVS port was already gone")
	})
})

var _ = Describe("Two claims get distinct ports — different nodes", Label(tier2), func() {
	const (
		claim0           = "e2e-distinct-diff-0"
		claim1           = "e2e-distinct-diff-1"
		pod0             = "e2e-pod-distinct-diff-0"
		pod1             = "e2e-pod-distinct-diff-1"
		kubernetes_label = "kubernetes.io/hostname"
	)

	It("each node has exactly one port on its local bridge", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-distinct-diff-policy", NodeNames: []string{workers[0], workers[1]}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		waitForDeviceInSlice(ctx, workers[1], plat.bridge0)

		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl", claimData{Name: claim0, Namespace: testNamespace, BridgeName: plat.bridge0}))
		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl", claimData{Name: claim1, Namespace: testNamespace, BridgeName: plat.bridge0}))
		// todo: The nodeName approach is not recommended
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{Name: pod0, Namespace: testNamespace, ClaimName: claim0, NodeSelector: map[string]string{kubernetes_label: workers[0]}}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{Name: pod1, Namespace: testNamespace, ClaimName: claim1, NodeSelector: map[string]string{kubernetes_label: workers[1]}}))

		waitForPodRunning(ctx, testNamespace, pod0)
		waitForPodRunning(ctx, testNamespace, pod1)

		rc0, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claim0, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		rc1, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claim1, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		ports0 := waitForOvsPorts(ctx, workers[0], string(rc0.UID))
		ports1 := waitForOvsPorts(ctx, workers[1], string(rc1.UID))

		Expect(len(ports0)).To(Equal(1), "worker0 should have exactly one port")
		Expect(len(ports1)).To(Equal(1), "worker1 should have exactly one port")
	})
})

var _ = Describe("Bridge hot-plug", Label(tier2), func() {
	const (
		bridge     = "br-hotplug"
		policyName = "e2e-hotplug-policy"
		claimName  = "e2e-hotplug-claim"
		podName    = "e2e-hotplug-pod"
	)

	It("bridge absent from ResourceSlice before OVS bridge is created", func(ctx SpecContext) {
		nodeName := workers[0]
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: policyName, NodeNames: []string{nodeName}, Bridges: []string{bridge}}))

		nodeSlices, err := resourceSlicesForNode(ctx, nodeName)
		Expect(err).NotTo(HaveOccurred())
		Expect(deviceNamesFromSlices(nodeSlices)).NotTo(ContainElement(bridge))
	})

	It("bridge appears in ResourceSlice after OVS bridge is created", func(ctx SpecContext) {
		nodeName := workers[0]
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: policyName, NodeNames: []string{nodeName}, Bridges: []string{bridge}}))

		addBridgeToOVS(ctx, nodeName, bridge)
		DeferCleanup(deleteBridgeFromOVS, context.Background(), nodeName, bridge)

		Eventually(func(g Gomega) {
			nodeSlices, err := resourceSlicesForNode(ctx, nodeName)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(deviceNamesFromSlices(nodeSlices)).To(ContainElement(bridge))
		}).WithTimeout(60 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
	})

	It("pod can be scheduled after bridge is created", func(ctx SpecContext) {
		nodeName := workers[0]
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: policyName, NodeNames: []string{nodeName}, Bridges: []string{bridge}}))

		addBridgeToOVS(ctx, nodeName, bridge)
		DeferCleanup(deleteBridgeFromOVS, context.Background(), nodeName, bridge)

		Eventually(func(g Gomega) {
			nodeSlices, err := resourceSlicesForNode(ctx, nodeName)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(deviceNamesFromSlices(nodeSlices)).To(ContainElement(bridge))
		}).WithTimeout(60 * time.Second).WithPolling(3 * time.Second).Should(Succeed())

		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl", claimData{Name: claimName, Namespace: testNamespace, BridgeName: bridge}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl", podData{Name: podName, Namespace: testNamespace, ClaimName: claimName}))
		waitForPodRunning(ctx, testNamespace, podName)
	})

	It("bridge disappears from ResourceSlice after OVS bridge is deleted", func(ctx SpecContext) {
		nodeName := workers[0]
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: policyName, NodeNames: []string{nodeName}, Bridges: []string{bridge}}))

		addBridgeToOVS(ctx, nodeName, bridge)
		Eventually(func(g Gomega) {
			nodeSlices, err := resourceSlicesForNode(ctx, nodeName)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(deviceNamesFromSlices(nodeSlices)).To(ContainElement(bridge))
		}).WithTimeout(60 * time.Second).WithPolling(3 * time.Second).Should(Succeed())

		deleteBridgeFromOVS(ctx, nodeName, bridge)
		Eventually(func(g Gomega) {
			nodeSlices, err := resourceSlicesForNode(ctx, nodeName)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(deviceNamesFromSlices(nodeSlices)).NotTo(ContainElement(bridge))
		}).WithTimeout(60 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
	})

	It("bridge deleted while pod is running — pod stays Running but new claims cannot be scheduled", func(ctx SpecContext) {
		nodeName := workers[0]
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: policyName, NodeNames: []string{nodeName}, Bridges: []string{bridge}}))

		addBridgeToOVS(ctx, nodeName, bridge)
		Eventually(func(g Gomega) {
			nodeSlices, err := resourceSlicesForNode(ctx, nodeName)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(deviceNamesFromSlices(nodeSlices)).To(ContainElement(bridge))
		}).WithTimeout(60 * time.Second).WithPolling(3 * time.Second).Should(Succeed())

		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl", claimData{Name: claimName, Namespace: testNamespace, BridgeName: bridge}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl", podData{Name: podName, Namespace: testNamespace, ClaimName: claimName}))
		waitForPodRunning(ctx, testNamespace, podName)

		deleteBridgeFromOVS(ctx, nodeName, bridge)

		pod, err := cs.CoreV1().Pods(testNamespace).Get(ctx, podName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(pod.Status.Phase).To(Equal(corev1.PodRunning), "pod should remain Running even though bridge is deleted")

		Eventually(func(g Gomega) {
			nodeSlices, err := resourceSlicesForNode(ctx, nodeName)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(deviceNamesFromSlices(nodeSlices)).NotTo(ContainElement(bridge))
		}).WithTimeout(60 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
	})

	It("bridge re-created after deletion — appears again in ResourceSlice and claims work", func(ctx SpecContext) {
		nodeName := workers[0]
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: policyName, NodeNames: []string{nodeName}, Bridges: []string{bridge}}))

		addBridgeToOVS(ctx, nodeName, bridge)
		Eventually(func(g Gomega) {
			nodeSlices, err := resourceSlicesForNode(ctx, nodeName)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(deviceNamesFromSlices(nodeSlices)).To(ContainElement(bridge))
		}).WithTimeout(60 * time.Second).WithPolling(3 * time.Second).Should(Succeed())

		deleteBridgeFromOVS(ctx, nodeName, bridge)
		Eventually(func(g Gomega) {
			nodeSlices, err := resourceSlicesForNode(ctx, nodeName)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(deviceNamesFromSlices(nodeSlices)).NotTo(ContainElement(bridge))
		}).WithTimeout(60 * time.Second).WithPolling(3 * time.Second).Should(Succeed())

		addBridgeToOVS(ctx, nodeName, bridge)
		Eventually(func(g Gomega) {
			nodeSlices, err := resourceSlicesForNode(ctx, nodeName)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(deviceNamesFromSlices(nodeSlices)).To(ContainElement(bridge), "bridge should reappear after re-creation")
		}).WithTimeout(60 * time.Second).WithPolling(3 * time.Second).Should(Succeed())

		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl", claimData{Name: claimName, Namespace: testNamespace, BridgeName: bridge}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl", podData{Name: podName, Namespace: testNamespace, ClaimName: claimName}))
		waitForPodRunning(ctx, testNamespace, podName)
		deleteBridgeFromOVS(ctx, nodeName, bridge)
	})
})

// This requires the env var OVS_IMAGE set for OVS daemonset image
var _ = Describe("OVS lifecycle detection", Label(tier2), func() {
	const policyName = "e2e-ovs-lifecycle-policy"

	BeforeEach(func() {
		if isOpenShift {
			Skip("OVS daemonset lifecycle tests are not supported on OpenShift")
		}
		if ovsImage == "" {
			Skip("OVS_IMAGE environment variable must be set to run OVS lifecycle tests")
		}
	})

	It("DRA Driver does not publish any configured slice when OVS is stopped", func(ctx SpecContext) {
		nodeName := workers[0]

		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: policyName, NodeNames: []string{nodeName}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, nodeName, plat.bridge0)

		stopOVSDaemonSet(ctx)
		DeferCleanup(startOVSDaemonSet, context.Background())

		restartDriverDaemonSet(ctx)

		waitForResourceSlicesEmpty(ctx, nodeName)

		slices, err := resourceSlicesForNode(ctx, nodeName)
		Expect(err).NotTo(HaveOccurred())

		totalDevices := 0
		for _, s := range slices {
			totalDevices += len(s.Spec.Devices)
		}
		Expect(totalDevices).To(Equal(0), "DRA Driver should not publish devices when OVS is not running")
	})

	It("DRA Driver reconnects to OVS and re-publishes slices after OVS restart", func(ctx SpecContext) {
		nodeName := workers[0]

		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: policyName + "-reconnect", NodeNames: []string{nodeName}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, nodeName, plat.bridge0)

		stopOVSDaemonSet(ctx)
		DeferCleanup(startOVSDaemonSet, context.Background())

		restartDriverDaemonSet(ctx)

		waitForResourceSlicesEmpty(ctx, nodeName)

		startOVSDaemonSet(ctx)

		Eventually(func(g Gomega) {
			slices, err := resourceSlicesForNode(ctx, nodeName)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(deviceNamesFromSlices(slices)).To(ContainElement(plat.bridge0),
				"DRA Driver should reconnect to OVS and re-publish the bridge")
		}).WithTimeout(90 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
	})
})
