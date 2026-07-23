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
	"fmt"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("ResourceSlice advertisement", func() {
	BeforeEach(func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-rs-policy-worker1", workers[0], []string{"br-dpdk0"}}))
		waitForDeviceInSlice(ctx, workers[0], "br-dpdk0")
	})

	It("each worker has at least one ResourceSlice", func(ctx SpecContext) {
		for _, node := range workers {
			nodeSlices, err := resourceSlicesForNode(ctx, node)
			Expect(err).NotTo(HaveOccurred(), "node %s", node)
			Expect(nodeSlices).NotTo(BeEmpty(), "node %s: expected at least one ResourceSlice", node)
		}
	})

	It("driver name is correct in each ResourceSlice", func(ctx SpecContext) {
		driverSlices, err := resourceSlicesForDriver(ctx)
		Expect(err).NotTo(HaveOccurred())
		for _, s := range driverSlices {
			Expect(s.Spec.Driver).To(Equal(driverName), "slice %s", s.Name)
		}
	})

	It("pool name equals node name in each ResourceSlice", func(ctx SpecContext) {
		driverSlices, err := resourceSlicesForDriver(ctx)
		Expect(err).NotTo(HaveOccurred())
		for _, s := range driverSlices {
			nodeName := ""
			if s.Spec.NodeName != nil {
				nodeName = *s.Spec.NodeName
			}
			Expect(s.Spec.Pool.Name).To(Equal(nodeName), "slice %s", s.Name)
		}
	})

	It("AllowMultipleAllocations=true on all devices", func(ctx SpecContext) {
		driverSlices, err := resourceSlicesForDriver(ctx)
		Expect(err).NotTo(HaveOccurred())
		for _, s := range driverSlices {
			for _, d := range s.Spec.Devices {
				Expect(d.AllowMultipleAllocations).NotTo(BeNil(), "slice %s device %s", s.Name, d.Name)
				Expect(*d.AllowMultipleAllocations).To(BeTrue(), "slice %s device %s", s.Name, d.Name)
			}
		}
	})

	It("bridgeName attribute present on all devices", func(ctx SpecContext) {
		driverSlices, err := resourceSlicesForDriver(ctx)
		Expect(err).NotTo(HaveOccurred())
		attrKey := resourceapi.QualifiedName(driverName + "/bridgeName")
		for _, s := range driverSlices {
			for _, d := range s.Spec.Devices {
				Expect(d.Attributes).To(HaveKey(attrKey), "slice %s device %s", s.Name, d.Name)
			}
		}
	})
})

var _ = Describe("Node selector", func() {
	BeforeEach(func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-ns-policy-worker1", workers[0], []string{"br-dpdk0", "br-dpdk1"}}))
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-ns-policy-worker2", workers[1], []string{"br-dpdk2"}}))
		waitForDeviceInSlice(ctx, workers[0], "br-dpdk0")
		waitForDeviceInSlice(ctx, workers[0], "br-dpdk1")
		waitForDeviceInSlice(ctx, workers[1], "br-dpdk2")
	})

	It("worker1 advertises br-dpdk0 and br-dpdk1", func(ctx SpecContext) {
		nodeSlices, err := resourceSlicesForNode(ctx, workers[0])
		Expect(err).NotTo(HaveOccurred())
		Expect(deviceNamesFromSlices(nodeSlices)).To(ContainElements("br-dpdk0", "br-dpdk1"))
	})

	It("worker2 advertises br-dpdk2 only", func(ctx SpecContext) {
		nodeSlices, err := resourceSlicesForNode(ctx, workers[1])
		Expect(err).NotTo(HaveOccurred())
		devices := deviceNamesFromSlices(nodeSlices)
		Expect(devices).To(ContainElement("br-dpdk2"))
		Expect(devices).NotTo(ContainElements("br-dpdk0", "br-dpdk1"))
	})

	It("worker1 does not advertise br-dpdk2", func(ctx SpecContext) {
		nodeSlices, err := resourceSlicesForNode(ctx, workers[0])
		Expect(err).NotTo(HaveOccurred())
		Expect(deviceNamesFromSlices(nodeSlices)).NotTo(ContainElement("br-dpdk2"))
	})
})

var _ = Describe("Claim lifecycle on worker1", func() {
	const (
		claimName = "e2e-claim-lifecycle"
		podName   = "e2e-pod-lifecycle"
	)

	var pod *corev1.Pod
	var socketDir string

	BeforeEach(func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-lifecycle-policy", workers[0], []string{"br-dpdk0"}}))
		waitForDeviceInSlice(ctx, workers[0], "br-dpdk0")
		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl", claimData{claimName, testNamespace, "br-dpdk0"}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl", podData{podName, testNamespace, claimName}))
		pod = waitForPodRunning(ctx, testNamespace, podName)
		socketDir = filepath.Join(hostSocketRoot, string(pod.UID)+"_"+claimName+"_vhost-port")
		Eventually(func() bool { return dirExists(ctx, pod.Spec.NodeName, socketDir) }).
			WithTimeout(60 * time.Second).WithPolling(3 * time.Second).Should(BeTrue())
	})

	It("socket directory is created", func(_ SpecContext) {
		// Assertion is in BeforeEach — if we got here, the dir exists.
	})

	It("socket directory has correct ownership per OvsDpdkConfig", func(ctx SpecContext) {
		uid, gid := statOwnership(ctx, pod.Spec.NodeName, socketDir)
		Expect(uid).To(Equal("1001"), "UID should be 1001 (ovsdpdk)")
		Expect(gid).To(Equal("107"), "GID should be 107 (qemu)")
	})

	It("socket directory has ACL entry for ovsdpdk user", func(ctx SpecContext) {
		Expect(hasACLEntry(ctx, pod.Spec.NodeName, socketDir, "user:1001")).To(BeTrue())
	})

	It("socket directory is removed when pod is deleted", func(ctx SpecContext) {
		nodeName := pod.Spec.NodeName
		deletePodAndWait(ctx, testNamespace, podName)
		Eventually(func() bool { return dirExists(ctx, nodeName, socketDir) }).
			WithTimeout(60 * time.Second).WithPolling(3 * time.Second).Should(BeFalse())
	})
})

var _ = Describe("Claim status", func() {
	const (
		claimName = "e2e-claim-status"
		podName   = "e2e-pod-status"
	)

	It("ResourceClaim.Status.Devices[0].Data is populated after prepare", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-status-policy", workers[0], []string{"br-dpdk0"}}))
		waitForDeviceInSlice(ctx, workers[0], "br-dpdk0")
		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl", claimData{claimName, testNamespace, "br-dpdk0"}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl", podData{podName, testNamespace, claimName}))
		waitForPodRunning(ctx, testNamespace, podName)

		var claim resourceapi.ResourceClaim
		Eventually(func(g Gomega) {
			c, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claimName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(c.Status.Devices).NotTo(BeEmpty())
			g.Expect(c.Status.Devices[0].Data).NotTo(BeNil())
			claim = *c
		}).WithTimeout(30 * time.Second).WithPolling(3 * time.Second).Should(Succeed())

		data := string(claim.Status.Devices[0].Data.Raw)
		Expect(data).To(ContainSubstring(claimName))
		Expect(data).To(SatisfyAny(ContainSubstring("hostPath"), ContainSubstring("hostDir")))
		Expect(data).To(SatisfyAny(ContainSubstring("containerPath"), ContainSubstring("containerDir")))
		Expect(data).To(ContainSubstring("bridgeName"))
	})
})

var _ = Describe("Vhost-user port lifecycle", func() {
	const (
		claimName = "e2e-vhost-port"
		podName   = "e2e-pod-vhost-port"
		bridge    = "br-dpdk0"
	)

	var pod *corev1.Pod
	var ports []string
	var uid string

	BeforeEach(func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-vhost-policy", workers[0], []string{bridge}}))
		waitForDeviceInSlice(ctx, workers[0], bridge)
		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl", claimData{claimName, testNamespace, bridge}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl", podData{podName, testNamespace, claimName}))
		pod = waitForPodRunning(ctx, testNamespace, podName)

		c, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claimName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		uid = string(c.UID)

		Eventually(func(g Gomega) {
			p, err := ovsPortsForClaim(ctx, pod.Spec.NodeName, uid)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p).NotTo(BeEmpty())
			ports = p
		}).WithTimeout(30 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
	})

	It("OVS port exists after pod is running", func(_ SpecContext) {
		Expect(ports).NotTo(BeEmpty())
	})

	It("OVS port is on the correct bridge", func(ctx SpecContext) {
		got, err := ovsPodExec(ctx, pod.Spec.NodeName, "ovs-vsctl", "port-to-br", ports[0])
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(bridge))
	})

	It("interface type is dpdkvhostuserclient", func(ctx SpecContext) {
		got, err := ovsPodExec(ctx, pod.Spec.NodeName, "ovs-vsctl", "get", "interface", ports[0], "type")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("dpdkvhostuserclient"))
	})

	It("vhost-server-path matches the socket path", func(ctx SpecContext) {
		got, err := ovsPodExec(ctx, pod.Spec.NodeName,
			"ovs-vsctl", "get", "interface", ports[0], "options:vhost-server-path")
		Expect(err).NotTo(HaveOccurred())
		wantDir := filepath.Join(hostSocketRoot, string(pod.UID)+"_"+claimName+"_vhost-port")
		Expect(strings.Trim(got, `"`)).To(Equal(filepath.Join(wantDir, "vhost.sock")))
	})

	It("OVS port is removed after pod deletion", func(ctx SpecContext) {
		nodeName := pod.Spec.NodeName
		deletePodAndWait(ctx, testNamespace, podName)
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
			applyAndCleanup(mustRenderManifest("claim.yaml.tmpl", claimData{name, testNamespace, bridge}))
		}
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl", podData{pod0, testNamespace, claim0}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl", podData{pod1, testNamespace, claim1}))

		runPod0 := waitForPodRunning(ctx, testNamespace, pod0)
		runPod1 := waitForPodRunning(ctx, testNamespace, pod1)

		rc0, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claim0, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		rc1, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claim1, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		var ports0, ports1 []string
		Eventually(func(g Gomega) {
			p, err := ovsPortsForClaim(ctx, runPod0.Spec.NodeName, string(rc0.UID))
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p).NotTo(BeEmpty())
			ports0 = p
		}).WithTimeout(30 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
		Eventually(func(g Gomega) {
			p, err := ovsPortsForClaim(ctx, runPod1.Spec.NodeName, string(rc1.UID))
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p).NotTo(BeEmpty())
			ports1 = p
		}).WithTimeout(30 * time.Second).WithPolling(3 * time.Second).Should(Succeed())

		if runPod0.Spec.NodeName == runPod1.Spec.NodeName {
			Expect(ports0[0]).NotTo(Equal(ports1[0]), "two claims got the same OVS port name")
		}
	})
})

var _ = Describe("SELinux label CRD validation", func() {
	It("valid label is accepted by the API server", func(_ SpecContext) {
		manifest := mustRenderManifest("ovsdpdkconfig-test.yaml.tmpl",
			ovsDpdkConfigData{"e2e-selinux-valid", "system_u:object_r:container_file_t:s0"})
		applyAndCleanup(manifest)
	})

	It("label missing a component is rejected", func(_ SpecContext) {
		manifest := mustRenderManifest("ovsdpdkconfig-test.yaml.tmpl",
			ovsDpdkConfigData{"e2e-selinux-invalid-short", "system_u:object_r:container_file_t"})
		Expect(tryApplyYAML(manifest)).To(HaveOccurred())
	})

	It("label with an empty component is rejected", func(_ SpecContext) {
		manifest := mustRenderManifest("ovsdpdkconfig-test.yaml.tmpl",
			ovsDpdkConfigData{"e2e-selinux-invalid-empty", "system_u::container_file_t:s0"})
		Expect(tryApplyYAML(manifest)).To(HaveOccurred())
	})

	It("label with no colons is rejected", func(_ SpecContext) {
		manifest := mustRenderManifest("ovsdpdkconfig-test.yaml.tmpl",
			ovsDpdkConfigData{"e2e-selinux-invalid-plain", "badlabel"})
		Expect(tryApplyYAML(manifest)).To(HaveOccurred())
	})
})

var _ = Describe("Single claim with multiple requests", func() {
	const (
		claimName = "e2e-multi-request"
		podName   = "e2e-pod-multi-request"
		bridge    = "br-dpdk0"
		nPorts    = 2
	)

	portNames := func() []string {
		p := make([]string, nPorts)
		for i := range nPorts {
			p[i] = fmt.Sprintf("port-%d", i)
		}
		return p
	}()

	var pod *corev1.Pod

	BeforeEach(func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-multi-req-policy", workers[0], []string{bridge}}))
		waitForDeviceInSlice(ctx, workers[0], bridge)
		applyAndCleanup(mustRenderManifest("claim-multi-request.yaml.tmpl",
			multiRequestClaimData{claimName, testNamespace, bridge, portNames}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl", podData{podName, testNamespace, claimName}))
		pod = waitForPodRunning(ctx, testNamespace, podName)
	})

	It("pod reaches Running", func(_ SpecContext) {
		// Assertion is in BeforeEach.
	})

	It("all request socket directories are present on the host", func(ctx SpecContext) {
		for _, reqName := range portNames {
			socketDir := filepath.Join(hostSocketRoot, string(pod.UID)+"_"+claimName+"_"+reqName)
			Eventually(func() bool { return dirExists(ctx, pod.Spec.NodeName, socketDir) }).
				WithTimeout(60*time.Second).WithPolling(3*time.Second).Should(BeTrue(),
				"socket dir for request %s", reqName)
		}
	})

	It("all request mounts are injected into the container", func(ctx SpecContext) {
		for _, reqName := range portNames {
			containerDir := filepath.Join(hostSocketRoot, claimName, reqName)
			_, err := kubectlExec(ctx, testNamespace, podName, "consumer", "test", "-d", containerDir)
			Expect(err).NotTo(HaveOccurred(), "container dir %s for request %s not found", containerDir, reqName)
		}
	})

	It("all OVS ports exist tagged with the claim UID", func(ctx SpecContext) {
		claim, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claimName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			p, err := ovsPortsForClaim(ctx, pod.Spec.NodeName, string(claim.UID))
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(len(p)).To(BeNumerically(">=", nPorts))
		}).WithTimeout(30 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
	})

	It("all socket dirs and OVS ports removed on pod deletion", func(ctx SpecContext) {
		nodeName := pod.Spec.NodeName
		claim, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claimName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		deletePodAndWait(ctx, testNamespace, podName)

		for _, reqName := range portNames {
			socketDir := filepath.Join(hostSocketRoot, string(pod.UID)+"_"+claimName+"_"+reqName)
			Eventually(func() bool { return dirExists(ctx, nodeName, socketDir) }).
				WithTimeout(60 * time.Second).WithPolling(3 * time.Second).Should(BeFalse())
		}
		Eventually(func(g Gomega) {
			p, err := ovsPortsForClaim(ctx, nodeName, string(claim.UID))
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p).To(BeEmpty())
		}).WithTimeout(30 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
	})
})

var _ = Describe("Multiple ports from same bridge in one pod", func() {
	const podName = "e2e-pod-multi-port"
	claimNames := []string{"e2e-multi-port-0", "e2e-multi-port-1"}

	var pod *corev1.Pod

	BeforeEach(func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-multi-port-policy", workers[0], []string{"br-dpdk0"}}))
		waitForDeviceInSlice(ctx, workers[0], "br-dpdk0")
		for _, name := range claimNames {
			applyAndCleanup(mustRenderManifest("claim.yaml.tmpl", claimData{name, testNamespace, "br-dpdk0"}))
		}
		applyAndCleanup(mustRenderManifest("pod-multi-claim.yaml.tmpl",
			multiClaimPodData{podName, testNamespace, claimNames}))
		pod = waitForPodRunning(ctx, testNamespace, podName)
	})

	It("both claims allocated and pod reaches Running", func(_ SpecContext) {
		// Assertion is in BeforeEach.
	})

	It("each claim gets a distinct socket directory", func(ctx SpecContext) {
		for _, name := range claimNames {
			socketDir := filepath.Join(hostSocketRoot, string(pod.UID)+"_"+name+"_vhost-port")
			Eventually(func() bool { return dirExists(ctx, pod.Spec.NodeName, socketDir) }).
				WithTimeout(60*time.Second).WithPolling(3*time.Second).Should(BeTrue(),
				"socket dir for claim %s", name)
		}
	})

	It("each claim has status data with its own claim name", func(ctx SpecContext) {
		for _, name := range claimNames {
			Eventually(func(g Gomega) {
				c, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, name, metav1.GetOptions{})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(c.Status.Devices).NotTo(BeEmpty())
				g.Expect(c.Status.Devices[0].Data).NotTo(BeNil())
				g.Expect(string(c.Status.Devices[0].Data.Raw)).To(ContainSubstring(name))
			}).WithTimeout(30 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
		}
	})

	It("both socket directories removed on pod deletion", func(ctx SpecContext) {
		nodeName := pod.Spec.NodeName
		var socketDirs []string
		for _, name := range claimNames {
			sd := filepath.Join(hostSocketRoot, string(pod.UID)+"_"+name+"_vhost-port")
			Eventually(func() bool { return dirExists(ctx, nodeName, sd) }).
				WithTimeout(60 * time.Second).WithPolling(3 * time.Second).Should(BeTrue())
			socketDirs = append(socketDirs, sd)
		}

		deletePodAndWait(ctx, testNamespace, podName)
		for _, sd := range socketDirs {
			Eventually(func() bool { return dirExists(ctx, nodeName, sd) }).
				WithTimeout(60 * time.Second).WithPolling(3 * time.Second).Should(BeFalse())
		}
	})
})

var _ = Describe("Bridge hot-plug", func() {
	const (
		bridge     = "br-hotplug"
		policyName = "e2e-hotplug-policy"
		claimName  = "e2e-hotplug-claim"
		podName    = "e2e-hotplug-pod"
	)

	It("bridge absent from ResourceSlice before OVS bridge is created", func(ctx SpecContext) {
		nodeName := workers[0]
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{policyName, nodeName, []string{bridge}}))

		nodeSlices, err := resourceSlicesForNode(ctx, nodeName)
		Expect(err).NotTo(HaveOccurred())
		Expect(deviceNamesFromSlices(nodeSlices)).NotTo(ContainElement(bridge))
	})

	It("bridge appears in ResourceSlice after OVS bridge is created", func(ctx SpecContext) {
		nodeName := workers[0]
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{policyName, nodeName, []string{bridge}}))

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
			policyData{policyName, nodeName, []string{bridge}}))

		addBridgeToOVS(ctx, nodeName, bridge)
		DeferCleanup(deleteBridgeFromOVS, context.Background(), nodeName, bridge)

		Eventually(func(g Gomega) {
			nodeSlices, err := resourceSlicesForNode(ctx, nodeName)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(deviceNamesFromSlices(nodeSlices)).To(ContainElement(bridge))
		}).WithTimeout(60 * time.Second).WithPolling(3 * time.Second).Should(Succeed())

		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl", claimData{claimName, testNamespace, bridge}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl", podData{podName, testNamespace, claimName}))
		waitForPodRunning(ctx, testNamespace, podName)
	})

	It("bridge disappears from ResourceSlice after OVS bridge is deleted", func(ctx SpecContext) {
		nodeName := workers[0]
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{policyName, nodeName, []string{bridge}}))

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
})

var _ = Describe("Topology Device Plugin", func() {
	const (
		bridge           = "br-dpdk0"
		dpdkPort         = "dpdk-topo0"
		topologyResource = "ovsdpdk.k8snetworkplumbingwg.io/topology-br-dpdk0"
		policyName       = "e2e-topology-policy"
	)

	BeforeEach(func() {
		if topologyPCI == "" {
			Skip("topology tests require TOPOLOGY_PCI env var")
		}
	})

	It("no extended resource before DPDK interface exists", func(ctx SpecContext) {
		nodeName := workers[0]
		applyAndCleanup(mustRenderManifest("policy-topology.yaml.tmpl",
			topologyPolicyData{policyName, nodeName, bridge, topologyResource}))

		Consistently(func() int64 {
			return nodeAllocatableQuantity(ctx, nodeName, topologyResource)
		}).WithTimeout(10 * time.Second).WithPolling(2 * time.Second).Should(BeZero())
	})

	It("extended resource appears after adding DPDK interface", func(ctx SpecContext) {
		nodeName := workers[0]
		applyAndCleanup(mustRenderManifest("policy-topology.yaml.tmpl",
			topologyPolicyData{policyName, nodeName, bridge, topologyResource}))

		addDPDKPort(ctx, nodeName, bridge, dpdkPort, topologyPCI)
		DeferCleanup(removeDPDKPort, context.Background(), nodeName, dpdkPort)

		waitForNodeResource(ctx, nodeName, topologyResource)
		Expect(nodeAllocatableQuantity(ctx, nodeName, topologyResource)).To(
			BeNumerically("==", 1024), "DefaultTopologyDeviceCount")
	})

	It("extended resource disappears when DPDK interface is removed", func(ctx SpecContext) {
		nodeName := workers[0]
		applyAndCleanup(mustRenderManifest("policy-topology.yaml.tmpl",
			topologyPolicyData{policyName, nodeName, bridge, topologyResource}))

		addDPDKPort(ctx, nodeName, bridge, dpdkPort, topologyPCI)
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
		applyAndCleanup(mustRenderManifest("policy-topology.yaml.tmpl",
			topologyPolicyData{policyName, nodeName, bridge, topologyResource}))

		addDPDKPort(ctx, nodeName, bridge, dpdkPort, topologyPCI)
		DeferCleanup(removeDPDKPort, context.Background(), nodeName, dpdkPort)
		waitForNodeResource(ctx, nodeName, topologyResource)

		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl", claimData{claimName, testNamespace, bridge}))
		applyAndCleanup(mustRenderManifest("pod-topology.yaml.tmpl",
			topologyPodData{podName, testNamespace, claimName, topologyResource}))

		pod := waitForPodRunning(ctx, testNamespace, podName)
		Expect(pod.Spec.NodeName).To(Equal(nodeName))
	})

	It("DPDK interface removed and re-added — DP recovers", func(ctx SpecContext) {
		const (
			claimName = "e2e-topo-recover-claim"
			podName   = "e2e-topo-recover-pod"
		)
		nodeName := workers[0]
		applyAndCleanup(mustRenderManifest("policy-topology.yaml.tmpl",
			topologyPolicyData{policyName, nodeName, bridge, topologyResource}))

		addDPDKPort(ctx, nodeName, bridge, dpdkPort, topologyPCI)
		waitForNodeResource(ctx, nodeName, topologyResource)

		removeDPDKPort(ctx, nodeName, dpdkPort)
		waitForNodeResourceGone(ctx, nodeName, topologyResource)

		addDPDKPort(ctx, nodeName, bridge, dpdkPort, topologyPCI)
		DeferCleanup(removeDPDKPort, context.Background(), nodeName, dpdkPort)
		waitForNodeResource(ctx, nodeName, topologyResource)

		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl", claimData{claimName, testNamespace, bridge}))
		applyAndCleanup(mustRenderManifest("pod-topology.yaml.tmpl",
			topologyPodData{podName, testNamespace, claimName, topologyResource}))

		pod := waitForPodRunning(ctx, testNamespace, podName)
		Expect(pod.Spec.NodeName).To(Equal(nodeName))
	})
})

var _ = Describe("MTU CRD validation", func() {
	It("valid mtu is accepted by the API server", func(_ SpecContext) {
		manifest := mustRenderManifest("policy-with-mtu.yaml.tmpl",
			mtuPolicyData{"e2e-mtu-valid", workers[0], "br-dpdk0", 9000})
		applyAndCleanup(manifest)
	})

	It("mtu below 68 is rejected by the API server", func(_ SpecContext) {
		manifest := mustRenderManifest("policy-with-mtu.yaml.tmpl",
			mtuPolicyData{"e2e-mtu-too-small", workers[0], "br-dpdk0", 67})
		Expect(tryApplyYAML(manifest)).To(HaveOccurred())
	})

	It("mtu above 65535 is rejected by the API server", func(_ SpecContext) {
		manifest := mustRenderManifest("policy-with-mtu.yaml.tmpl",
			mtuPolicyData{"e2e-mtu-too-large", workers[0], "br-dpdk0", 65536})
		Expect(tryApplyYAML(manifest)).To(HaveOccurred())
	})
})

var _ = Describe("MTU", Ordered, func() {
	const (
		claimName = "e2e-mtu-claim"
		podName   = "e2e-mtu-pod"
		bridge    = "br-dpdk0"
		mtu       = 9000
	)

	var pod *corev1.Pod
	var ports []string
	var claimUID string

	BeforeAll(func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy-with-mtu.yaml.tmpl",
			mtuPolicyData{"e2e-mtu-policy", workers[0], bridge, mtu}))
		waitForDeviceInSlice(ctx, workers[0], bridge)

		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{claimName, testNamespace, bridge}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{podName, testNamespace, claimName}))
		pod = waitForPodRunning(ctx, testNamespace, podName)

		c, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claimName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		claimUID = string(c.UID)

		Eventually(func(g Gomega) {
			p, err := ovsPortsForClaim(ctx, pod.Spec.NodeName, claimUID)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p).NotTo(BeEmpty())
			ports = p
		}).WithTimeout(30 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
	})

	It("mtu attribute is present in the ResourceSlice device", func(ctx SpecContext) {
		attrKey := resourceapi.QualifiedName(driverName + "/mtu")
		nodeSlices, err := resourceSlicesForNode(ctx, workers[0])
		Expect(err).NotTo(HaveOccurred())
		var found bool
		for _, s := range nodeSlices {
			for _, d := range s.Spec.Devices {
				if d.Name == bridge {
					attr, ok := d.Attributes[attrKey]
					Expect(ok).To(BeTrue(), "mtu attribute missing on device %s", bridge)
					Expect(attr.IntValue).NotTo(BeNil())
					Expect(*attr.IntValue).To(Equal(int64(mtu)))
					found = true
				}
			}
		}
		Expect(found).To(BeTrue(), "device %s not found in ResourceSlices", bridge)
	})

	It("mtu_request is set on the OVS interface", func(ctx SpecContext) {
		got, err := ovsInterfaceGet(ctx, pod.Spec.NodeName, ports[0], "mtu_request")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(fmt.Sprintf("%d", mtu)))
	})

	It("mtu is present in the in-container device metadata file", func(ctx SpecContext) {
		Eventually(func(g Gomega) {
			md, err := readDeviceMetadataFile(ctx, testNamespace, podName, "consumer",
				claimName, "vhost-port")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(md.Requests).To(HaveLen(1))
			g.Expect(md.Requests[0].Devices).To(HaveLen(1))
			dev := md.Requests[0].Devices[0]
			mtuAttr, ok := dev.Attributes["mtu"]
			g.Expect(ok).To(BeTrue(), "mtu attribute missing from device metadata, got: %v", dev.Attributes)
			g.Expect(mtuAttr.IntValue).NotTo(BeNil())
			g.Expect(*mtuAttr.IntValue).To(Equal(int64(mtu)))
		}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
	})
})

var _ = Describe("MTU absent", func() {
	It("mtu_request is absent when no mtu is configured", func(ctx SpecContext) {
		const (
			plainClaimName = "e2e-mtu-absent-claim"
			plainPodName   = "e2e-mtu-absent-pod"
			bridge         = "br-dpdk1"
		)
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-mtu-absent-policy", workers[0], []string{bridge}}))
		waitForDeviceInSlice(ctx, workers[0], bridge)
		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{plainClaimName, testNamespace, bridge}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{plainPodName, testNamespace, plainClaimName}))
		plainPod := waitForPodRunning(ctx, testNamespace, plainPodName)

		c, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, plainClaimName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		var plainPorts []string
		Eventually(func(g Gomega) {
			p, err := ovsPortsForClaim(ctx, plainPod.Spec.NodeName, string(c.UID))
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p).NotTo(BeEmpty())
			plainPorts = p
		}).WithTimeout(30 * time.Second).WithPolling(3 * time.Second).Should(Succeed())

		got, err := ovsInterfaceGet(ctx, plainPod.Spec.NodeName, plainPorts[0], "mtu_request")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("[]")) // OVS returns [] for unset optional integer columns
	})
})

var _ = Describe("Ingress policing", Ordered, func() {
	const (
		claimName = "e2e-policing"
		podName   = "e2e-pod-policing"
		bridge    = "br-dpdk0"
	)

	var pod *corev1.Pod
	var ports []string
	var claimUID string

	BeforeAll(func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-policing-policy", workers[0], []string{bridge}}))
		waitForDeviceInSlice(ctx, workers[0], bridge)
		applyAndCleanup(mustRenderManifest("claim-with-policing.yaml.tmpl",
			policingClaimData{
				Name:       claimName,
				Namespace:  testNamespace,
				BridgeName: bridge,
				MaxRate:    100000, // 100 Mbps in kbps
				Burst:      10000,  // 10 Mb in kb
			}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl", podData{podName, testNamespace, claimName}))
		pod = waitForPodRunning(ctx, testNamespace, podName)

		c, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claimName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		claimUID = string(c.UID)

		Eventually(func(g Gomega) {
			p, err := ovsPortsForClaim(ctx, pod.Spec.NodeName, claimUID)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p).NotTo(BeEmpty())
			ports = p
		}).WithTimeout(30 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
	})

	It("ingress_policing_rate is set on the OVS interface", func(ctx SpecContext) {
		got, err := ovsInterfaceGet(ctx, pod.Spec.NodeName, ports[0], "ingress_policing_rate")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("100000"))
	})

	It("ingress_policing_burst is set on the OVS interface", func(ctx SpecContext) {
		got, err := ovsInterfaceGet(ctx, pod.Spec.NodeName, ports[0], "ingress_policing_burst")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("10000"))
	})

	It("policing config is reflected in ResourceClaim status data", func(ctx SpecContext) {
		Eventually(func(g Gomega) {
			c, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claimName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(c.Status.Devices).NotTo(BeEmpty())
			g.Expect(c.Status.Devices[0].Data).NotTo(BeNil())
			data := string(c.Status.Devices[0].Data.Raw)
			g.Expect(data).To(ContainSubstring(`"policing"`))
			g.Expect(data).To(ContainSubstring(`"max_rate":100000`))
			g.Expect(data).To(ContainSubstring(`"burst":10000`))
		}).WithTimeout(30 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
	})
})

var _ = Describe("Ingress policing absent", func() {
	It("policing is absent from OVS interface when not configured", func(ctx SpecContext) {
		const (
			plainClaimName = "e2e-policing-absent"
			plainPodName   = "e2e-pod-policing-absent"
			bridge         = "br-dpdk0"
		)
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-policing-absent-policy", workers[0], []string{bridge}}))
		waitForDeviceInSlice(ctx, workers[0], bridge)
		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{plainClaimName, testNamespace, bridge}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{plainPodName, testNamespace, plainClaimName}))
		plainPod := waitForPodRunning(ctx, testNamespace, plainPodName)

		c, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, plainClaimName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		var plainPorts []string
		Eventually(func(g Gomega) {
			p, err := ovsPortsForClaim(ctx, plainPod.Spec.NodeName, string(c.UID))
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p).NotTo(BeEmpty())
			plainPorts = p
		}).WithTimeout(30 * time.Second).WithPolling(3 * time.Second).Should(Succeed())

		got, err := ovsInterfaceGet(ctx, plainPod.Spec.NodeName, plainPorts[0], "ingress_policing_rate")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("0"))
	})
})
