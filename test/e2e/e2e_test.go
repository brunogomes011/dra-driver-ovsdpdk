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

// ResourceSlice Advertisement and node selector


var _ = Describe("ResourceSlice advertisement", func() {
	BeforeEach(func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-rs-policy-worker0", []string{workers[0]}, []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
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
			policyData{"e2e-ns-policy-worker1", []string{workers[0]}, []string{plat.bridge0, plat.bridge1}}))
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-ns-policy-worker2", []string{workers[1]}, []string{plat.bridge2}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		waitForDeviceInSlice(ctx, workers[0], plat.bridge1)
		waitForDeviceInSlice(ctx, workers[1], plat.bridge2)
	})

	It("worker1 advertises its configured bridges", func(ctx SpecContext) {
		nodeSlices, err := resourceSlicesForNode(ctx, workers[0])
		Expect(err).NotTo(HaveOccurred())
		Expect(deviceNamesFromSlices(nodeSlices)).To(ContainElements(plat.bridge0, plat.bridge1))
	})

	It("worker2 advertises only its configured bridge", func(ctx SpecContext) {
		nodeSlices, err := resourceSlicesForNode(ctx, workers[1])
		Expect(err).NotTo(HaveOccurred())
		devices := deviceNamesFromSlices(nodeSlices)
		Expect(devices).To(ContainElement(plat.bridge2))
		Expect(devices).NotTo(ContainElements(plat.bridge0, plat.bridge1))
	})

	It("worker1 does not advertise worker2 bridges", func(ctx SpecContext) {
		nodeSlices, err := resourceSlicesForNode(ctx, workers[0])
		Expect(err).NotTo(HaveOccurred())
		Expect(deviceNamesFromSlices(nodeSlices)).NotTo(ContainElement(plat.bridge2))
	})
})

var _ = Describe("Policy overlap", func() {
	BeforeEach(func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-mp-policy-shared", []string{workers[0], workers[1]}, []string{plat.bridge0}}))
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-mp-policy-worker1", []string{workers[1]}, []string{plat.bridge1}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		waitForDeviceInSlice(ctx, workers[1], plat.bridge0)
		waitForDeviceInSlice(ctx, workers[1], plat.bridge1)
	})

	It("worker1 advertises bridge from shared policy", func(ctx SpecContext) {
		nodeSlices, err := resourceSlicesForNode(ctx, workers[0])
		Expect(err).NotTo(HaveOccurred())
		Expect(deviceNamesFromSlices(nodeSlices)).To(ContainElement(plat.bridge0))
	})

	It("worker2 advertises bridges from both policies", func(ctx SpecContext) {
		nodeSlices, err := resourceSlicesForNode(ctx, workers[1])
		Expect(err).NotTo(HaveOccurred())
		devices := deviceNamesFromSlices(nodeSlices)
		Expect(devices).To(ContainElements(plat.bridge0, plat.bridge1))
	})

	It("worker1 does not advertise bridge only assigned to worker2", func(ctx SpecContext) {
		nodeSlices, err := resourceSlicesForNode(ctx, workers[0])
		Expect(err).NotTo(HaveOccurred())
		Expect(deviceNamesFromSlices(nodeSlices)).NotTo(ContainElement(plat.bridge1))
	})
})

var _ = Describe("Policy update - Replace bridge", func() {
	It("replacing a bridge in the policy updates ResourceSlices accordingly", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-policy-replace", []string{workers[0], workers[1]}, []string{plat.bridge0, plat.bridge1}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		waitForDeviceInSlice(ctx, workers[0], plat.bridge1)
		waitForDeviceInSlice(ctx, workers[1], plat.bridge0)
		waitForDeviceInSlice(ctx, workers[1], plat.bridge1)

		applyYAML(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-policy-replace", []string{workers[0], workers[1]}, []string{plat.bridge0, plat.bridge2}}))

		Eventually(func(g Gomega) {
			w0Slices, err := resourceSlicesForNode(ctx, workers[0])
			g.Expect(err).NotTo(HaveOccurred())
			w1Slices, err := resourceSlicesForNode(ctx, workers[1])
			g.Expect(err).NotTo(HaveOccurred())
			w0Devices := deviceNamesFromSlices(w0Slices)
			w1Devices := deviceNamesFromSlices(w1Slices)
			g.Expect(w0Devices).To(ContainElements(plat.bridge0, plat.bridge2))
			g.Expect(w0Devices).NotTo(ContainElement(plat.bridge1))
			g.Expect(w1Devices).To(ContainElements(plat.bridge0, plat.bridge2))
			g.Expect(w1Devices).NotTo(ContainElement(plat.bridge1))
		}).WithTimeout(60 * time.Second).WithPolling(5 * time.Second).Should(Succeed())
	})
})

var _ = Describe("Duplicate detection", func() {
	It("second policy with a duplicate bridge does not advertise any of its bridges", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-dup-base", []string{workers[0], workers[1]}, []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		waitForDeviceInSlice(ctx, workers[1], plat.bridge0)

		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-dup-extra", []string{workers[1]}, []string{plat.bridge0, plat.bridge1}}))

		Consistently(func(g Gomega) {
			nodeSlices, err := resourceSlicesForNode(ctx, workers[1])
			g.Expect(err).NotTo(HaveOccurred())
			devices := deviceNamesFromSlices(nodeSlices)
			g.Expect(devices).To(ContainElement(plat.bridge0))
			g.Expect(devices).NotTo(ContainElement(plat.bridge1))
		}).WithTimeout(15 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
	})
})

var _ = Describe("Policy update - Remove bridge", func() {
	It("removing a bridge from the policy removes it from ResourceSlices", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-policy-update", []string{workers[0], workers[1]}, []string{plat.bridge0, plat.bridge1}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		waitForDeviceInSlice(ctx, workers[0], plat.bridge1)
		waitForDeviceInSlice(ctx, workers[1], plat.bridge0)
		waitForDeviceInSlice(ctx, workers[1], plat.bridge1)

		applyYAML(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-policy-update", []string{workers[0], workers[1]}, []string{plat.bridge0}}))

		Eventually(func(g Gomega) {
			w0Slices, err := resourceSlicesForNode(ctx, workers[0])
			g.Expect(err).NotTo(HaveOccurred())
			w1Slices, err := resourceSlicesForNode(ctx, workers[1])
			g.Expect(err).NotTo(HaveOccurred())
			w0Devices := deviceNamesFromSlices(w0Slices)
			w1Devices := deviceNamesFromSlices(w1Slices)
			g.Expect(w0Devices).To(ContainElement(plat.bridge0))
			g.Expect(w0Devices).NotTo(ContainElement(plat.bridge1))
			g.Expect(w1Devices).To(ContainElement(plat.bridge0))
			g.Expect(w1Devices).NotTo(ContainElement(plat.bridge1))
		}).WithTimeout(60 * time.Second).WithPolling(5 * time.Second).Should(Succeed())
	})
})

var _ = Describe("Policy API validation", func() {
	It("policy without bridges is rejected by the API server", func(_ SpecContext) {
		manifest := mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-policy-no-bridge", []string{workers[0]}, []string{}})
		Expect(tryApplyYAML(manifest)).To(HaveOccurred())
	})
})

//Claim Lifecycle


var _ = Describe("Claim lifecycle on worker1", func() {
	const (
		claimName = "e2e-claim-lifecycle"
		podName   = "e2e-pod-lifecycle"
	)

	var pod *corev1.Pod
	var socketDir string

	BeforeEach(func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-lifecycle-policy", []string{workers[0]}, []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl", claimData{claimName, testNamespace, plat.bridge0}))
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

var _ = Describe("Socket created using pod-claim-name annotation name", func() {
	const (
		templateName = "e2e-claim-tmpl"
		podClaimName = "vhost-test-name"
		podName      = "e2e-pod-claim-tmpl"
	)

	It("socket directory uses the pod-local claim name, not the generated claim name", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-tmpl-policy", []string{workers[0]}, []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)

		applyAndCleanup(mustRenderManifest("claim-template.yaml.tmpl",
			claimData{templateName, testNamespace, plat.bridge0}))
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

		socketDir := filepath.Join(hostSocketRoot, string(pod.UID)+"_"+podClaimName+"_vhost-port")
		Eventually(func() bool { return dirExists(ctx, pod.Spec.NodeName, socketDir) }).
			WithTimeout(60 * time.Second).WithPolling(3 * time.Second).Should(BeTrue(),
			"socket dir should use pod-local claim name %q, not generated name %q", podClaimName, generatedClaimName)

		wrongDir := filepath.Join(hostSocketRoot, string(pod.UID)+"_"+generatedClaimName+"_vhost-port")
		Expect(dirExists(ctx, pod.Spec.NodeName, wrongDir)).To(BeFalse(),
			"socket dir should NOT use the generated claim name %q", generatedClaimName)
	})
})

var _ = Describe("Claim targeting non-existent bridge", func() {
	const (
		claimName = "e2e-nomatch-claim"
		podName   = "e2e-pod-nomatch"
	)

	It("pod stays Pending when claim selects a bridge not in any ResourceSlice", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-nomatch-policy", []string{workers[0]}, []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)

		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{claimName, testNamespace, "br-nonexistent"}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{podName, testNamespace, claimName}))

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

var _ = Describe("Early pod deletion cleanup", func() {
	const (
		claimName = "e2e-early-del-claim"
		podName   = "e2e-pod-early-del"
	)

	It("no orphaned socket dir or OVS port when pod is deleted immediately", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-early-del-policy", []string{workers[0]}, []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)

		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{claimName, testNamespace, plat.bridge0}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{podName, testNamespace, claimName}))

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

		socketDir := filepath.Join(hostSocketRoot, podUID+"_"+claimName+"_vhost-port")
		Expect(dirExists(ctx, workers[0], socketDir)).To(BeFalse(), "orphaned socket dir %s", socketDir)
	})
})

var _ = Describe("Claim with unknown device class name", func() {
	const (
		claimName = "e2e-unknown-class-claim"
		podName   = "e2e-pod-unknown-class"
	)

	It("pod stays Pending when claim references a non-existent DeviceClass", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-unknown-class-policy", []string{workers[0]}, []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)

		applyAndCleanup(mustRenderManifest("claim-unknown-class.yaml.tmpl",
			unknownClassClaimData{claimName, testNamespace, "nonexistent-class"}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{podName, testNamespace, claimName}))

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

//Claim Status


var _ = Describe("Claim status", func() {
	const (
		claimName = "e2e-claim-status"
		podName   = "e2e-pod-status"
	)

	It("ResourceClaim.Status.Devices[0].Data is populated after prepare", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-status-policy", []string{workers[0]}, []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl", claimData{claimName, testNamespace, plat.bridge0}))
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

var _ = Describe("OvsPortConfig propagation to claim status", func() {
	const (
		claimName = "e2e-portconfig-claim"
		podName   = "e2e-pod-portconfig"
	)

	It("vlan and policing values are reflected in ResourceClaim status data", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-portconfig-policy", []string{workers[0]}, []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		applyAndCleanup(mustRenderManifest("claim-with-config.yaml.tmpl",
			portConfigClaimData{
				Name:       claimName,
				Namespace:  testNamespace,
				BridgeName: plat.bridge0,
				Vlan:       42,
				MaxRate:    50000, // 50 Mbps in kbps
				Burst:      5000,  // 5 Mb in kb
			}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl", podData{podName, testNamespace, claimName}))
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


//todo: Review this test
var _ = Describe("Claim status timing", func() {
	const (
		claimName = "e2e-status-timing-claim"
		podName   = "e2e-pod-status-timing"
	)

	It("status.devices has no data before Running and has data once Running", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-status-timing-policy", []string{workers[0]}, []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)

		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{claimName, testNamespace, plat.bridge0}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{podName, testNamespace, claimName}))

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

//Multi claims on same bridge

var _ = Describe("Multiple ports from same bridge in one pod", func() {
	const podName = "e2e-pod-multi-port"
	claimNames := []string{"e2e-multi-port-0", "e2e-multi-port-1"}

	var pod *corev1.Pod

	BeforeEach(func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-multi-port-policy", []string{workers[0]}, []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		for _, name := range claimNames {
			applyAndCleanup(mustRenderManifest("claim.yaml.tmpl", claimData{name, testNamespace, plat.bridge0}))
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

var _ = Describe("Two claims, two Pods", func() {
	const (
		claim0 = "e2e-two-claim-0"
		claim1 = "e2e-two-claim-1"
		pod0   = "e2e-two-pod-0"
		pod1   = "e2e-two-pod-1"
	)

	It("each pod gets its own socket dir and deleting one does not affect the other", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-two-claim-policy", []string{workers[0]}, []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)

		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl", claimData{claim0, testNamespace, plat.bridge0}))
		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl", claimData{claim1, testNamespace, plat.bridge0}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl", podData{pod0, testNamespace, claim0}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl", podData{pod1, testNamespace, claim1}))

		p0 := waitForPodRunning(ctx, testNamespace, pod0)
		p1 := waitForPodRunning(ctx, testNamespace, pod1)

		socketDir0 := filepath.Join(hostSocketRoot, string(p0.UID)+"_"+claim0+"_vhost-port")
		socketDir1 := filepath.Join(hostSocketRoot, string(p1.UID)+"_"+claim1+"_vhost-port")
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

var _ = Describe("Same claim referenced by two Pods", func() {
	const (
		claimName = "e2e-shared-claim"
		pod0      = "e2e-shared-pod-0"
		pod1      = "e2e-shared-pod-1"
	)

	It("only one pod reaches Running", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-shared-claim-policy", []string{workers[0]}, []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)

		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{claimName, testNamespace, plat.bridge0}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{pod0, testNamespace, claimName}))
		waitForPodRunning(ctx, testNamespace, pod0)

		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{pod1, testNamespace, claimName}))

		Consistently(func(g Gomega) {
			p, err := cs.CoreV1().Pods(testNamespace).Get(ctx, pod1, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p.Status.Phase).NotTo(Equal(corev1.PodRunning))
		}).WithTimeout(20 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
	})
})

//Single claim with multiple requests


var _ = Describe("Single claim with multiple requests", func() {
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
			policyData{"e2e-multi-req-policy", []string{workers[0]}, []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		applyAndCleanup(mustRenderManifest("claim-multi-request.yaml.tmpl",
			multiRequestClaimData{claimName, testNamespace, plat.bridge0, portNames}))
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

var _ = Describe("Partial failure rollback", func() {
	const (
		claimName = "e2e-rollback-claim"
		podName   = "e2e-pod-rollback"
	)

	It("resources from successful request are removed when another request fails", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-rollback-policy", []string{workers[0]}, []string{plat.bridge0}}))
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
			podData{podName, testNamespace, claimName}))

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

var _ = Describe("Requests on different bridges", func() {
	const (
		claimName = "e2e-diff-bridge-claim"
		podName   = "e2e-pod-diff-bridge"
	)

	It("each request creates a port on its own bridge and cleanup removes both", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-diff-bridge-policy", []string{workers[0]}, []string{plat.bridge0, plat.bridge1}}))
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
			podData{podName, testNamespace, claimName}))
		pod := waitForPodRunning(ctx, testNamespace, podName)

		socketDir0 := filepath.Join(hostSocketRoot, string(pod.UID)+"_"+claimName+"_port-on-br0")
		socketDir1 := filepath.Join(hostSocketRoot, string(pod.UID)+"_"+claimName+"_port-on-br1")
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

var _ = Describe("Cannot request 2 ports on the same request", func() {
	const (
		claimName = "e2e-count-claim"
		podName   = "e2e-pod-count"
	)

	It("pod stays Pending when a single request asks for count 2", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-count-policy", []string{workers[0]}, []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)

		applyAndCleanup(mustRenderManifest("claim-count.yaml.tmpl",
			countClaimData{claimName, testNamespace, plat.bridge0, 2}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{podName, testNamespace, claimName}))

		Consistently(func(g Gomega) {
			p, err := cs.CoreV1().Pods(testNamespace).Get(ctx, podName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p.Status.Phase).To(Equal(corev1.PodPending))
		}).WithTimeout(20 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
	})
})

//Two claims get distinct ports

var _ = Describe("Two claims get distinct ports — different nodes", func() {
	const (
		claim0 = "e2e-distinct-diff-0"
		claim1 = "e2e-distinct-diff-1"
		pod0   = "e2e-pod-distinct-diff-0"
		pod1   = "e2e-pod-distinct-diff-1"
	)

	It("each node has exactly one port on its local bridge", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-distinct-diff-policy", []string{workers[0], workers[1]}, []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		waitForDeviceInSlice(ctx, workers[1], plat.bridge0)

		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl", claimData{claim0, testNamespace, plat.bridge0}))
		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl", claimData{claim1, testNamespace, plat.bridge0}))
		applyAndCleanup(mustRenderManifest("pod-with-node.yaml.tmpl",
			podWithNodeData{pod0, testNamespace, claim0, workers[0]}))
		applyAndCleanup(mustRenderManifest("pod-with-node.yaml.tmpl",
			podWithNodeData{pod1, testNamespace, claim1, workers[1]}))

		waitForPodRunning(ctx, testNamespace, pod0)
		waitForPodRunning(ctx, testNamespace, pod1)

		rc0, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claim0, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		rc1, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claim1, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		var ports0, ports1 []string
		Eventually(func(g Gomega) {
			p, err := ovsPortsForClaim(ctx, workers[0], string(rc0.UID))
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p).NotTo(BeEmpty())
			ports0 = p
		}).WithTimeout(30 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
		Eventually(func(g Gomega) {
			p, err := ovsPortsForClaim(ctx, workers[1], string(rc1.UID))
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p).NotTo(BeEmpty())
			ports1 = p
		}).WithTimeout(30 * time.Second).WithPolling(3 * time.Second).Should(Succeed())

		Expect(len(ports0)).To(Equal(1), "worker0 should have exactly one port")
		Expect(len(ports1)).To(Equal(1), "worker1 should have exactly one port")
	})
})

//OVS port lifecycle

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
			policyData{policyName, []string{nodeName}, []string{bridge}}))

		nodeSlices, err := resourceSlicesForNode(ctx, nodeName)
		Expect(err).NotTo(HaveOccurred())
		Expect(deviceNamesFromSlices(nodeSlices)).NotTo(ContainElement(bridge))
	})

	It("bridge appears in ResourceSlice after OVS bridge is created", func(ctx SpecContext) {
		nodeName := workers[0]
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{policyName, []string{nodeName}, []string{bridge}}))

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
			policyData{policyName, []string{nodeName}, []string{bridge}}))

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
			policyData{policyName, []string{nodeName}, []string{bridge}}))

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



var _ = Describe("Vhost-user port lifecycle", func() {
	const (
		claimName = "e2e-vhost-port"
		podName   = "e2e-pod-vhost-port"
	)

	var pod *corev1.Pod
	var ports []string
	var uid string

	BeforeEach(func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-vhost-policy", []string{workers[0]}, []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl", claimData{claimName, testNamespace, plat.bridge0}))
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
			applyAndCleanup(mustRenderManifest("claim.yaml.tmpl", claimData{name, testNamespace, plat.bridge0}))
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

//OVS Port Creation

var _ = Describe("Port on non-existent bridge (race)", func() {
	const (
		bridge    = "br-race-create"
		claimName = "e2e-race-create-claim"
		podName   = "e2e-pod-race-create"
	)

	It("pod does not reach Running and no socket dir is leaked when the bridge disappears before prepare", func(ctx SpecContext) {
		nodeName := workers[0]
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-race-create-policy", []string{nodeName}, []string{bridge}}))
		addBridgeToOVS(ctx, nodeName, bridge)
		DeferCleanup(deleteBridgeFromOVS, context.Background(), nodeName, bridge)
		waitForDeviceInSlice(ctx, nodeName, bridge)

		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{claimName, testNamespace, bridge}))
		applyAndCleanup(mustRenderManifest("pod-with-node.yaml.tmpl",
			podWithNodeData{podName, testNamespace, claimName, nodeName}))

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

		socketDir := filepath.Join(hostSocketRoot, podUID+"_"+claimName+"_vhost-port")
		Expect(dirExists(ctx, nodeName, socketDir)).To(BeFalse(),
			"socket dir must be rolled back after a failed prepare")
	})
})

//OVS Port Deletion

var _ = Describe("Port already gone before deletion", func() {
	const (
		claimName = "e2e-port-gone-claim"
		podName   = "e2e-pod-port-gone"
	)

	It("unprepare succeeds and the socket dir is still removed when the OVS port is already gone", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-port-gone-policy", []string{workers[0]}, []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{claimName, testNamespace, plat.bridge0}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{podName, testNamespace, claimName}))
		pod := waitForPodRunning(ctx, testNamespace, podName)

		claim, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claimName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		var ports []string
		Eventually(func(g Gomega) {
			p, err := ovsPortsForClaim(ctx, pod.Spec.NodeName, string(claim.UID))
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p).NotTo(BeEmpty())
			ports = p
		}).WithTimeout(30 * time.Second).WithPolling(3 * time.Second).Should(Succeed())

		removeDPDKPort(ctx, pod.Spec.NodeName, ports[0])

		socketDir := filepath.Join(hostSocketRoot, string(pod.UID)+"_"+claimName+"_vhost-port")
		deletePodAndWait(ctx, testNamespace, podName)

		Expect(dirExists(ctx, pod.Spec.NodeName, socketDir)).To(BeFalse(),
			"socket dir must be removed even though the OVS port was already gone")
	})
})

//Vlan

var _ = Describe("VLAN tag applied", func() {
	const (
		claimName = "e2e-vlan-tag-claim"
		podName   = "e2e-pod-vlan-tag"
	)

	It("ovs-vsctl get port tag reflects the configured vlan", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-vlan-tag-policy", []string{workers[0]}, []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		applyAndCleanup(mustRenderManifest("claim-with-vlan.yaml.tmpl",
			vlanClaimData{claimName, testNamespace, plat.bridge0, 100}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl", podData{podName, testNamespace, claimName}))
		pod := waitForPodRunning(ctx, testNamespace, podName)

		claim, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claimName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		var ports []string
		Eventually(func(g Gomega) {
			p, err := ovsPortsForClaim(ctx, pod.Spec.NodeName, string(claim.UID))
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p).NotTo(BeEmpty())
			ports = p
		}).WithTimeout(30 * time.Second).WithPolling(3 * time.Second).Should(Succeed())

		got, err := ovsExec(ctx, pod.Spec.NodeName, "ovs-vsctl", "get", "port", ports[0], "tag")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("100"))
	})
})

var _ = Describe("No VLAN when unset", func() {
	const (
		claimName = "e2e-vlan-unset-claim"
		podName   = "e2e-pod-vlan-unset"
	)

	It("ovs-vsctl get port tag returns [] (trunk) when vlan is not configured", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-vlan-unset-policy", []string{workers[0]}, []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl", claimData{claimName, testNamespace, plat.bridge0}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl", podData{podName, testNamespace, claimName}))
		pod := waitForPodRunning(ctx, testNamespace, podName)

		claim, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claimName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		var ports []string
		Eventually(func(g Gomega) {
			p, err := ovsPortsForClaim(ctx, pod.Spec.NodeName, string(claim.UID))
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p).NotTo(BeEmpty())
			ports = p
		}).WithTimeout(30 * time.Second).WithPolling(3 * time.Second).Should(Succeed())

		got, err := ovsExec(ctx, pod.Spec.NodeName, "ovs-vsctl", "get", "port", ports[0], "tag")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("[]"))
	})
})

var _ = Describe("VLAN 0 is valid", func() {
	const (
		claimName = "e2e-vlan-zero-claim"
		podName   = "e2e-pod-vlan-zero"
	)

	It("ovs-vsctl get port tag returns 0 for the native VLAN", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-vlan-zero-policy", []string{workers[0]}, []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		applyAndCleanup(mustRenderManifest("claim-with-vlan.yaml.tmpl",
			vlanClaimData{claimName, testNamespace, plat.bridge0, 0}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl", podData{podName, testNamespace, claimName}))
		pod := waitForPodRunning(ctx, testNamespace, podName)

		claim, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claimName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		var ports []string
		Eventually(func(g Gomega) {
			p, err := ovsPortsForClaim(ctx, pod.Spec.NodeName, string(claim.UID))
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p).NotTo(BeEmpty())
			ports = p
		}).WithTimeout(30 * time.Second).WithPolling(3 * time.Second).Should(Succeed())

		got, err := ovsExec(ctx, pod.Spec.NodeName, "ovs-vsctl", "get", "port", ports[0], "tag")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("0"))
	})
})

var _ = Describe("Invalid VLAN rejected", func() {
	const (
		claimName = "e2e-vlan-invalid-claim"
		podName   = "e2e-pod-vlan-invalid"
	)

	It("pod does not reach Running when vlan is out of range [0, 4095]", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-vlan-invalid-policy", []string{workers[0]}, []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		applyAndCleanup(mustRenderManifest("claim-with-vlan.yaml.tmpl",
			vlanClaimData{claimName, testNamespace, plat.bridge0, 5000}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl", podData{podName, testNamespace, claimName}))

		Consistently(func(g Gomega) {
			p, err := cs.CoreV1().Pods(testNamespace).Get(ctx, podName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p.Status.Phase).NotTo(Equal(corev1.PodRunning))
		}).WithTimeout(20 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
	})
})

var _ = Describe("Negative VLAN rejected", func() {
	const (
		claimName = "e2e-vlan-negative-claim"
		podName   = "e2e-pod-vlan-negative"
	)

	It("pod does not reach Running when vlan is negative", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-vlan-negative-policy", []string{workers[0]}, []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		applyAndCleanup(mustRenderManifest("claim-with-vlan.yaml.tmpl",
			vlanClaimData{claimName, testNamespace, plat.bridge0, -1}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl", podData{podName, testNamespace, claimName}))

		Consistently(func(g Gomega) {
			p, err := cs.CoreV1().Pods(testNamespace).Get(ctx, podName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p.Status.Phase).NotTo(Equal(corev1.PodRunning))
		}).WithTimeout(20 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
	})
})

var _ = Describe("VLAN apply to all", func() {
	const (
		claimName = "e2e-vlan-apply-all-claim"
		podName   = "e2e-pod-vlan-apply-all"
	)

	It("a config entry with no requests list applies the vlan to all requests", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-vlan-apply-all-policy", []string{workers[0]}, []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		applyAndCleanup(mustRenderManifest("claim-vlan-apply-all.yaml.tmpl",
			vlanClaimData{claimName, testNamespace, plat.bridge0, 100}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl", podData{podName, testNamespace, claimName}))
		pod := waitForPodRunning(ctx, testNamespace, podName)

		claim, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claimName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		var ports []string
		Eventually(func(g Gomega) {
			p, err := ovsPortsForClaim(ctx, pod.Spec.NodeName, string(claim.UID))
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p).NotTo(BeEmpty())
			ports = p
		}).WithTimeout(30 * time.Second).WithPolling(3 * time.Second).Should(Succeed())

		got, err := ovsExec(ctx, pod.Spec.NodeName, "ovs-vsctl", "get", "port", ports[0], "tag")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("100"))
	})
})

var _ = Describe("VLAN apply to all with request-specific override", func() {
	const (
		claimName = "e2e-vlan-override-claim"
		podName   = "e2e-pod-vlan-override"
	)

	It("the request-specific config wins when listed before the apply-to-all entry", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-vlan-override-policy", []string{workers[0]}, []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		applyAndCleanup(mustRenderManifest("claim-vlan-override.yaml.tmpl",
			vlanOverrideClaimData{
				Name:         claimName,
				Namespace:    testNamespace,
				BridgeName:   plat.bridge0,
				SpecificVlan: 200,
				GlobalVlan:   100,
			}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl", podData{podName, testNamespace, claimName}))
		pod := waitForPodRunning(ctx, testNamespace, podName)

		claim, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claimName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		var ports []string
		Eventually(func(g Gomega) {
			p, err := ovsPortsForClaim(ctx, pod.Spec.NodeName, string(claim.UID))
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p).NotTo(BeEmpty())
			ports = p
		}).WithTimeout(30 * time.Second).WithPolling(3 * time.Second).Should(Succeed())

		got, err := ovsExec(ctx, pod.Spec.NodeName, "ovs-vsctl", "get", "port", ports[0], "tag")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("200"))
	})
})




var _ = Describe("SELinux label CRD validation", func() {
	It("valid label is accepted by the API server", func(_ SpecContext) {
		manifest := mustRenderManifest("ovsdpdkconfig-test.yaml.tmpl",
			ovsDpdkConfigData{"e2e-selinux-valid", "system_u:object_r:container_file_t:s0", plat.testCfgUser, plat.testCfgGroup})
		applyAndCleanup(manifest)
	})

	It("label missing a component is rejected", func(_ SpecContext) {
		manifest := mustRenderManifest("ovsdpdkconfig-test.yaml.tmpl",
			ovsDpdkConfigData{"e2e-selinux-invalid-short", "system_u:object_r:container_file_t", plat.testCfgUser, plat.testCfgGroup})
		Expect(tryApplyYAML(manifest)).To(HaveOccurred())
	})

	It("label with an empty component is rejected", func(_ SpecContext) {
		manifest := mustRenderManifest("ovsdpdkconfig-test.yaml.tmpl",
			ovsDpdkConfigData{"e2e-selinux-invalid-empty", "system_u::container_file_t:s0", plat.testCfgUser, plat.testCfgGroup})
		Expect(tryApplyYAML(manifest)).To(HaveOccurred())
	})

	It("label with no colons is rejected", func(_ SpecContext) {
		manifest := mustRenderManifest("ovsdpdkconfig-test.yaml.tmpl",
			ovsDpdkConfigData{"e2e-selinux-invalid-plain", "badlabel", plat.testCfgUser, plat.testCfgGroup})
		Expect(tryApplyYAML(manifest)).To(HaveOccurred())
	})
})

var _ = Describe("Topology Device Plugin", func() {
	const (
		dpdkPort   = "dpdk-topo0"
		policyName = "e2e-topology-policy"
	)

	var bridge, topologyResource string

	BeforeEach(func() {
		bridge = plat.topoBridge
		topologyResource = driverName + "/topology-" + plat.topoBridge
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
			mtuPolicyData{"e2e-mtu-valid", workers[0], plat.bridge0, 9000})
		applyAndCleanup(manifest)
	})

	It("mtu below 68 is rejected by the API server", func(_ SpecContext) {
		manifest := mustRenderManifest("policy-with-mtu.yaml.tmpl",
			mtuPolicyData{"e2e-mtu-too-small", workers[0], plat.bridge0, 67})
		Expect(tryApplyYAML(manifest)).To(HaveOccurred())
	})

	It("mtu above 65535 is rejected by the API server", func(_ SpecContext) {
		manifest := mustRenderManifest("policy-with-mtu.yaml.tmpl",
			mtuPolicyData{"e2e-mtu-too-large", workers[0], plat.bridge0, 65536})
		Expect(tryApplyYAML(manifest)).To(HaveOccurred())
	})
})

var _ = Describe("MTU", Ordered, func() {
	const (
		claimName = "e2e-mtu-claim"
		podName   = "e2e-mtu-pod"
		mtu       = 9000
	)

	var pod *corev1.Pod
	var ports []string
	var claimUID string

	BeforeAll(func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy-with-mtu.yaml.tmpl",
			mtuPolicyData{"e2e-mtu-policy", workers[0], plat.bridge0, mtu}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)

		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{claimName, testNamespace, plat.bridge0}))
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
				if d.Name == plat.bridge0 {
					attr, ok := d.Attributes[attrKey]
					Expect(ok).To(BeTrue(), "mtu attribute missing on device %s", plat.bridge0)
					Expect(attr.IntValue).NotTo(BeNil())
					Expect(*attr.IntValue).To(Equal(int64(mtu)))
					found = true
				}
			}
		}
		Expect(found).To(BeTrue(), "device %s not found in ResourceSlices", plat.bridge0)
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
		)
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-mtu-absent-policy", []string{workers[0]}, []string{plat.bridge1}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge1)
		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{plainClaimName, testNamespace, plat.bridge1}))
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
	)

	var pod *corev1.Pod
	var ports []string
	var claimUID string

	BeforeAll(func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-policing-policy", []string{workers[0]}, []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		applyAndCleanup(mustRenderManifest("claim-with-policing.yaml.tmpl",
			policingClaimData{
				Name:       claimName,
				Namespace:  testNamespace,
				BridgeName: plat.bridge0,
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
		)
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-policing-absent-policy", []string{workers[0]}, []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{plainClaimName, testNamespace, plat.bridge0}))
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

var _ = Describe("Checkpoint persistence across driver restart", func() {
	const (
		claimName = "e2e-persist-claim"
		podName   = "e2e-persist-pod"
	)

	It("unprepare cleans up OVS port and socket dir after driver restart", func(ctx SpecContext) {
		nodeName := workers[0]

		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{"e2e-persist-policy", []string{nodeName}, []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, nodeName, plat.bridge0)
		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{claimName, testNamespace, plat.bridge0}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{podName, testNamespace, claimName}))
		pod := waitForPodRunning(ctx, testNamespace, podName)

		claim, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claimName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		claimUID := string(claim.UID)

		Eventually(func(g Gomega) {
			p, err := ovsPortsForClaim(ctx, nodeName, claimUID)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p).NotTo(BeEmpty())
		}).WithTimeout(30 * time.Second).WithPolling(3 * time.Second).Should(Succeed())

		socketDir := filepath.Join(hostSocketRoot, string(pod.UID)+"_"+claimName+"_vhost-port")

		By("Restarting the driver pod on " + nodeName)
		restartDriverOnNode(ctx, nodeName)
		waitForDeviceInSlice(ctx, nodeName, plat.bridge0)

		By("Deleting the test pod to trigger unprepare after restart")
		deletePodAndWait(ctx, testNamespace, podName)

		Eventually(func(g Gomega) {
			p, err := ovsPortsForClaim(ctx, nodeName, claimUID)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p).To(BeEmpty())
		}).WithTimeout(30 * time.Second).WithPolling(3 * time.Second).Should(Succeed())

		Eventually(func() bool { return dirExists(ctx, nodeName, socketDir) }).
			WithTimeout(30 * time.Second).WithPolling(3 * time.Second).Should(BeFalse())
	})
})
