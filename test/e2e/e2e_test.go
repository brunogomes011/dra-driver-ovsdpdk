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
