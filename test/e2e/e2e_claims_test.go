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
	"fmt"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)



var _ = Describe("Claim lifecycle on worker1", Label(tier1), func() {
	const (
		claimName = "e2e-claim-lifecycle"
		podName   = "e2e-pod-lifecycle"
	)

	var pod *corev1.Pod
	var socketDir string

	BeforeEach(func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-lifecycle-policy", NodeNames: []string{workers[0]}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl", claimData{Name: claimName, Namespace: testNamespace, BridgeName: plat.bridge0}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl", podData{Name: podName, Namespace: testNamespace, ClaimName: claimName}))
		pod = waitForPodRunning(ctx, testNamespace, podName)
		socketDir = socketDirPath(string(pod.UID), claimName, "vhost-port")
		Eventually(func() bool { return dirExists(ctx, pod.Spec.NodeName, socketDir) }).
			WithTimeout(60 * time.Second).WithPolling(3 * time.Second).Should(BeTrue())
	})

	It("socket directory is created", func(_ SpecContext) {
		// Assertion is in BeforeEach — if we got here, the dir exists.
	})

	It("socket directory has correct ownership per OvsDpdkConfig", func(ctx SpecContext) {
		uid, gid := statOwnership(ctx, pod.Spec.NodeName, socketDir)
		Expect(uid).To(Equal(plat.ovsUID), "UID mismatch")
		Expect(gid).To(Equal("107"), "GID should be 107 (qemu)")
	})

	It("socket directory has ACL entry for ovsdpdk user", func(ctx SpecContext) {
		Expect(hasACLEntry(ctx, pod.Spec.NodeName, socketDir, plat.aclEntry)).To(BeTrue())
	})

	It("socket directory is removed when pod is deleted", func(ctx SpecContext) {
		nodeName := pod.Spec.NodeName
		deletePodAndWait(ctx, testNamespace, podName)
		Eventually(func() bool { return dirExists(ctx, nodeName, socketDir) }).
			WithTimeout(60 * time.Second).WithPolling(3 * time.Second).Should(BeFalse())
	})
})

var _ = Describe("Socket created using pod-claim-name annotation name", Label(tier2), func() {
	const (
		templateName = "e2e-claim-tmpl"
		podClaimName = "vhost-test-name"
		podName      = "e2e-pod-claim-tmpl"
	)

	It("socket directory uses the pod-local claim name, not the generated claim name", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-tmpl-policy", NodeNames: []string{workers[0]}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)

		applyAndCleanup(mustRenderManifest("claim-template.yaml.tmpl",
			claimData{Name: templateName, Namespace: testNamespace, BridgeName: plat.bridge0}))
		applyAndCleanup(mustRenderManifest("pod-claim-template.yaml.tmpl",
			claimTemplatePodData{podName, testNamespace, podClaimName, templateName}))
		pod := waitForPodRunning(ctx, testNamespace, podName)

		claims, err := cs.ResourceV1().ResourceClaims(testNamespace).List(ctx, metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		var generatedClaimName string
		for _, c := range claims.Items {
			if ann := c.Annotations["resource.kubernetes.io/pod-claim-name"]; ann == podClaimName {
				if ref := metav1.GetControllerOf(&c); ref != nil && ref.Name == podName {
					generatedClaimName = c.Name
					break
				}
			}
		}
		Expect(generatedClaimName).NotTo(BeEmpty(), "generated ResourceClaim not found")
		Expect(generatedClaimName).NotTo(Equal(podClaimName), "claim name should be auto-generated")

		socketDir := socketDirPath(string(pod.UID), podClaimName, "vhost-port")
		Eventually(func() bool { return dirExists(ctx, pod.Spec.NodeName, socketDir) }).
			WithTimeout(60*time.Second).WithPolling(3*time.Second).Should(BeTrue(),
			"socket dir should use pod-local claim name %q, not generated name %q", podClaimName, generatedClaimName)

		wrongDir := socketDirPath(string(pod.UID), generatedClaimName, "vhost-port")
		Expect(dirExists(ctx, pod.Spec.NodeName, wrongDir)).To(BeFalse(),
			"socket dir should NOT use the generated claim name %q", generatedClaimName)
	})
})

var _ = Describe("Claim targeting non-existent bridge", Label(tier2), func() {
	const (
		claimName = "e2e-nomatch-claim"
		podName   = "e2e-pod-nomatch"
	)

	It("pod stays Pending when claim selects a bridge not in any ResourceSlice", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-nomatch-policy", NodeNames: []string{workers[0]}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)

		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{Name: claimName, Namespace: testNamespace, BridgeName: "br-nonexistent"}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{Name: podName, Namespace: testNamespace, ClaimName: claimName}))

		Consistently(func(g Gomega) {
			p, err := cs.CoreV1().Pods(testNamespace).Get(ctx, podName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p.Status.Phase).To(Equal(corev1.PodPending))
		}).WithTimeout(20 * time.Second).WithPolling(3 * time.Second).Should(Succeed())

		claim, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claimName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		claimUID := string(claim.UID)

		ports, err := ovsPortsForClaim(ctx, workers[0], claimUID)
		Expect(err).NotTo(HaveOccurred())
		Expect(ports).To(BeEmpty())
	})
})

var _ = Describe("Pod deleted while still preparing", Label(tier2), func() {
	const (
		claimName = "e2e-early-del-claim"
		podName   = "e2e-pod-early-del"
	)

	It("no orphaned socket dir or OVS port when pod is deleted immediately", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-early-del-policy", NodeNames: []string{workers[0]}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)

		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{Name: claimName, Namespace: testNamespace, BridgeName: plat.bridge0}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{Name: podName, Namespace: testNamespace, ClaimName: claimName}))

		pod, err := cs.CoreV1().Pods(testNamespace).Get(ctx, podName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		podUID := string(pod.UID)

		claim, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claimName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		claimUID := string(claim.UID)

		deletePodAndWait(ctx, testNamespace, podName)

		Eventually(func(g Gomega) {
			ports, err := ovsPortsForClaim(ctx, workers[0], claimUID)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(ports).To(BeEmpty(), "orphaned OVS ports for claim %s", claimUID)
		}).WithTimeout(30 * time.Second).WithPolling(3 * time.Second).Should(Succeed())

		socketDir := socketDirPath(podUID, claimName, "vhost-port")
		Expect(dirExists(ctx, workers[0], socketDir)).To(BeFalse(), "orphaned socket dir %s", socketDir)
	})
})

var _ = Describe("Claim with unknown device class name", Label(tier2), func() {
	const (
		claimName = "e2e-unknown-class-claim"
		podName   = "e2e-pod-unknown-class"
	)

	It("pod stays Pending when claim references a non-existent DeviceClass", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-unknown-class-policy", NodeNames: []string{workers[0]}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)

		applyAndCleanup(mustRenderManifest("claim-unknown-class.yaml.tmpl",
			unknownClassClaimData{claimName, testNamespace, "nonexistent-class"}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{Name: podName, Namespace: testNamespace, ClaimName: claimName}))

		Consistently(func(g Gomega) {
			p, err := cs.CoreV1().Pods(testNamespace).Get(ctx, podName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p.Status.Phase).To(Equal(corev1.PodPending))
		}).WithTimeout(20 * time.Second).WithPolling(3 * time.Second).Should(Succeed())

		claim, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claimName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		claimUID := string(claim.UID)

		ports, err := ovsPortsForClaim(ctx, workers[0], claimUID)
		Expect(err).NotTo(HaveOccurred())
		Expect(ports).To(BeEmpty())
	})
})


var _ = Describe("Claim status", Label(tier1), func() {
	const (
		claimName = "e2e-claim-status"
		podName   = "e2e-pod-status"
	)

	It("ResourceClaim.Status.Devices[0].Data is populated after prepare", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-status-policy", NodeNames: []string{workers[0]}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl", claimData{Name: claimName, Namespace: testNamespace, BridgeName: plat.bridge0}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl", podData{Name: podName, Namespace: testNamespace, ClaimName: claimName}))
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

var _ = Describe("OvsPortConfig propagation to claim status", Label(tier2), func() {
	const (
		claimName = "e2e-portconfig-claim"
		podName   = "e2e-pod-portconfig"
	)

	It("vlan and policing values are reflected in ResourceClaim status data", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-portconfig-policy", NodeNames: []string{workers[0]}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{
				Name:       claimName,
				Namespace:  testNamespace,
				BridgeName: plat.bridge0,
				Vlan:       intPtr(42),
				MaxRate:    uint32Ptr(50000), // 50 Mbps in kbps
				Burst:      5000,             // 5 Mb in kb
			}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl", podData{Name: podName, Namespace: testNamespace, ClaimName: claimName}))
		waitForPodRunning(ctx, testNamespace, podName)

		Eventually(func(g Gomega) {
			c, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claimName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(c.Status.Devices).NotTo(BeEmpty())
			g.Expect(c.Status.Devices[0].Data).NotTo(BeNil())
			data := string(c.Status.Devices[0].Data.Raw)
			g.Expect(data).To(ContainSubstring(`"config"`))
			g.Expect(data).To(ContainSubstring(`"vlan":42`))
			g.Expect(data).To(ContainSubstring(`"policing"`))
			g.Expect(data).To(ContainSubstring(`"max_rate":50000`))
			g.Expect(data).To(ContainSubstring(`"burst":5000`))
		}).WithTimeout(30 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
	})
})

var _ = Describe("Claim status timing", Label("troubleshoot"), func() {
	const (
		claimName = "e2e-status-timing-claim"
		podName   = "e2e-pod-status-timing"
	)

	It("status.devices has no data before Running and has data once Running", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-status-timing-policy", NodeNames: []string{workers[0]}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)

		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{Name: claimName, Namespace: testNamespace, BridgeName: plat.bridge0}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{Name: podName, Namespace: testNamespace, ClaimName: claimName}))

		// Phase 1: while the pod is not yet Running (Pending /
		// ContainerCreating), the claim must not carry prepared data.
		// Each Consistently iteration skips the data assertion if the
		// pod already transitioned to Running, so a fast pod does not
		// cause a false failure.
		Consistently(func(g Gomega) {
			p, err := cs.CoreV1().Pods(testNamespace).Get(ctx, podName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			if p.Status.Phase == corev1.PodRunning {
				return
			}
			c, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claimName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			for _, d := range c.Status.Devices {
				g.Expect(d.Data).To(BeNil(), "claim data must not be set before the pod is Running")
			}
		}).WithTimeout(10 * time.Second).WithPolling(50 * time.Millisecond).Should(Succeed())

		// Phase 2: once the pod is Running, the claim must carry the
		// prepared device data.
		waitForPodRunning(ctx, testNamespace, podName)
		Eventually(func(g Gomega) {
			c, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claimName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(c.Status.Devices).NotTo(BeEmpty())
			g.Expect(c.Status.Devices[0].Data).NotTo(BeNil())
		}).WithTimeout(30 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
	})
})


var _ = Describe("Multiple ports from same bridge in one pod", Label(tier2), func() {
	const podName = "e2e-pod-multi-port"
	claimNames := []string{"e2e-multi-port-0", "e2e-multi-port-1"}

	var pod *corev1.Pod

	BeforeEach(func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-multi-port-policy", NodeNames: []string{workers[0]}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		for _, name := range claimNames {
			applyAndCleanup(mustRenderManifest("claim.yaml.tmpl", claimData{Name: name, Namespace: testNamespace, BridgeName: plat.bridge0}))
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
			socketDir := socketDirPath(string(pod.UID), name, "vhost-port")
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
			sd := socketDirPath(string(pod.UID), name, "vhost-port")
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

var _ = Describe("Two claims, two Pods", Label(tier2), func() {
	const (
		claim0 = "e2e-two-claim-0"
		claim1 = "e2e-two-claim-1"
		pod0   = "e2e-two-pod-0"
		pod1   = "e2e-two-pod-1"
	)

	It("each pod gets its own socket dir and deleting one does not affect the other", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-two-claim-policy", NodeNames: []string{workers[0]}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)

		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl", claimData{Name: claim0, Namespace: testNamespace, BridgeName: plat.bridge0}))
		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl", claimData{Name: claim1, Namespace: testNamespace, BridgeName: plat.bridge0}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl", podData{Name: pod0, Namespace: testNamespace, ClaimName: claim0}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl", podData{Name: pod1, Namespace: testNamespace, ClaimName: claim1}))

		p0 := waitForPodRunning(ctx, testNamespace, pod0)
		p1 := waitForPodRunning(ctx, testNamespace, pod1)

		socketDir0 := socketDirPath(string(p0.UID), claim0, "vhost-port")
		socketDir1 := socketDirPath(string(p1.UID), claim1, "vhost-port")
		Eventually(func() bool { return dirExists(ctx, p0.Spec.NodeName, socketDir0) }).
			WithTimeout(60 * time.Second).WithPolling(3 * time.Second).Should(BeTrue())
		Eventually(func() bool { return dirExists(ctx, p1.Spec.NodeName, socketDir1) }).
			WithTimeout(60 * time.Second).WithPolling(3 * time.Second).Should(BeTrue())

		nodeName1 := p1.Spec.NodeName
		deletePodAndWait(ctx, testNamespace, pod0)

		Expect(dirExists(ctx, nodeName1, socketDir1)).To(BeTrue(),
			"pod1 socket dir must survive pod0 deletion")
	})
})

var _ = Describe("Same claim referenced by two Pods", Label("troubleshoot"), func() {
	const (
		claimName = "e2e-shared-claim"
		pod0      = "e2e-shared-pod-0"
		pod1      = "e2e-shared-pod-1"
	)

	It("only one pod reaches Running", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-shared-claim-policy", NodeNames: []string{workers[0]}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)

		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{Name: claimName, Namespace: testNamespace, BridgeName: plat.bridge0}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{Name: pod0, Namespace: testNamespace, ClaimName: claimName}))
		waitForPodRunning(ctx, testNamespace, pod0)

		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{Name: pod1, Namespace: testNamespace, ClaimName: claimName}))

		Consistently(func(g Gomega) {
			p, err := cs.CoreV1().Pods(testNamespace).Get(ctx, pod1, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p.Status.Phase).NotTo(Equal(corev1.PodRunning))
		}).WithTimeout(20 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
	})
})


var _ = Describe("Single claim with multiple requests", Label(tier1), func() {
	const (
		claimName = "e2e-multi-request"
		podName   = "e2e-pod-multi-request"
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
			policyData{Name: "e2e-multi-req-policy", NodeNames: []string{workers[0]}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		applyAndCleanup(mustRenderManifest("claim-multi-request.yaml.tmpl",
			multiRequestClaimData{claimName, testNamespace, plat.bridge0, portNames}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl", podData{Name: podName, Namespace: testNamespace, ClaimName: claimName}))
		pod = waitForPodRunning(ctx, testNamespace, podName)
	})

	It("pod reaches Running", func(_ SpecContext) {
		// Assertion is in BeforeEach.
	})

	It("all request socket directories are present on the host", func(ctx SpecContext) {
		for _, reqName := range portNames {
			socketDir := socketDirPath(string(pod.UID), claimName, reqName)
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
			socketDir := socketDirPath(string(pod.UID), claimName, reqName)
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

var _ = Describe("Partial failure rollback", Label(tier2), func() {
	const (
		claimName = "e2e-rollback-claim"
		podName   = "e2e-pod-rollback"
	)

	It("resources from successful request are removed when another request fails", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-rollback-policy", NodeNames: []string{workers[0]}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)

		applyAndCleanup(mustRenderManifest("claim-multi-bridge-request.yaml.tmpl",
			multiBridgeClaimData{
				Name:      claimName,
				Namespace: testNamespace,
				Requests: []requestBridgePair{
					{"valid-port", plat.bridge0},
					{"bad-port", "br-nonexistent"},
				},
			}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{Name: podName, Namespace: testNamespace, ClaimName: claimName}))

		Consistently(func(g Gomega) {
			p, err := cs.CoreV1().Pods(testNamespace).Get(ctx, podName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p.Status.Phase).NotTo(Equal(corev1.PodRunning))
		}).WithTimeout(20 * time.Second).WithPolling(3 * time.Second).Should(Succeed())

		claim, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claimName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		ports, err := ovsPortsForClaim(ctx, workers[0], string(claim.UID))
		Expect(err).NotTo(HaveOccurred())
		Expect(ports).To(BeEmpty(), "no OVS ports should remain after rollback")
	})
})

var _ = Describe("Requests on different bridges", Label(tier2), func() {
	const (
		claimName = "e2e-diff-bridge-claim"
		podName   = "e2e-pod-diff-bridge"
	)

	It("each request creates a port on its own bridge and cleanup removes both", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-diff-bridge-policy", NodeNames: []string{workers[0]}, Bridges: []string{plat.bridge0, plat.bridge1}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		waitForDeviceInSlice(ctx, workers[0], plat.bridge1)

		applyAndCleanup(mustRenderManifest("claim-multi-bridge-request.yaml.tmpl",
			multiBridgeClaimData{
				Name:      claimName,
				Namespace: testNamespace,
				Requests: []requestBridgePair{
					{"port-on-br0", plat.bridge0},
					{"port-on-br1", plat.bridge1},
				},
			}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{Name: podName, Namespace: testNamespace, ClaimName: claimName}))
		pod := waitForPodRunning(ctx, testNamespace, podName)

		socketDir0 := socketDirPath(string(pod.UID), claimName, "port-on-br0")
		socketDir1 := socketDirPath(string(pod.UID), claimName, "port-on-br1")
		Eventually(func() bool { return dirExists(ctx, pod.Spec.NodeName, socketDir0) }).
			WithTimeout(60 * time.Second).WithPolling(3 * time.Second).Should(BeTrue())
		Eventually(func() bool { return dirExists(ctx, pod.Spec.NodeName, socketDir1) }).
			WithTimeout(60 * time.Second).WithPolling(3 * time.Second).Should(BeTrue())

		claim, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claimName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		claimUID := string(claim.UID)

		Eventually(func(g Gomega) {
			ports, err := ovsPortsForClaim(ctx, pod.Spec.NodeName, claimUID)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(len(ports)).To(BeNumerically(">=", 2))
		}).WithTimeout(30 * time.Second).WithPolling(3 * time.Second).Should(Succeed())

		Expect(claim.Status.Devices).To(HaveLen(2))
		for _, d := range claim.Status.Devices {
			Expect(d.Data).NotTo(BeNil())
			raw := string(d.Data.Raw)
			Expect(raw).To(ContainSubstring("bridgeName"))
		}

		nodeName := pod.Spec.NodeName
		deletePodAndWait(ctx, testNamespace, podName)

		Eventually(func(g Gomega) {
			ports, err := ovsPortsForClaim(ctx, nodeName, claimUID)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(ports).To(BeEmpty())
		}).WithTimeout(30 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
	})
})

var _ = Describe("Cannot request 2 ports on the same request", Label(tier2), func() {
	const (
		claimName = "e2e-count-claim"
		podName   = "e2e-pod-count"
	)

	It("pod stays Pending when a single request asks for count 2", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-count-policy", NodeNames: []string{workers[0]}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)

		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{Name: claimName, Namespace: testNamespace, BridgeName: plat.bridge0, Count: 2}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{Name: podName, Namespace: testNamespace, ClaimName: claimName}))

		Consistently(func(g Gomega) {
			p, err := cs.CoreV1().Pods(testNamespace).Get(ctx, podName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p.Status.Phase).To(Equal(corev1.PodPending))
		}).WithTimeout(20 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
	})
})
