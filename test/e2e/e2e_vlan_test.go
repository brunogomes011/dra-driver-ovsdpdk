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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//Vlan

var _ = Describe("VLAN tag applied", Label("tier1"), func() {
	const (
		claimName = "e2e-vlan-tag-claim"
		podName   = "e2e-pod-vlan-tag"
	)

	It("ovs-vsctl get port tag reflects the configured vlan", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-vlan-tag-policy", NodeNames: []string{workers[0]}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{Name: claimName, Namespace: testNamespace, BridgeName: plat.bridge0, Vlan: intPtr(100)}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl", podData{Name: podName, Namespace: testNamespace, ClaimName: claimName}))
		pod := waitForPodRunning(ctx, testNamespace, podName)

		claim, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claimName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		ports := waitForOvsPorts(ctx, pod.Spec.NodeName, string(claim.UID))

		got, err := ovsPortGet(ctx, pod.Spec.NodeName, ports[0], "tag")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("100"))
	})
})

var _ = Describe("No VLAN when unset", Label("tier1"), func() {
	const (
		claimName = "e2e-vlan-unset-claim"
		podName   = "e2e-pod-vlan-unset"
	)

	It("ovs-vsctl get port tag returns [] (trunk) when vlan is not configured", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-vlan-unset-policy", NodeNames: []string{workers[0]}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl", claimData{Name: claimName, Namespace: testNamespace, BridgeName: plat.bridge0}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl", podData{Name: podName, Namespace: testNamespace, ClaimName: claimName}))
		pod := waitForPodRunning(ctx, testNamespace, podName)

		claim, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claimName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		ports := waitForOvsPorts(ctx, pod.Spec.NodeName, string(claim.UID))

		got, err := ovsPortGet(ctx, pod.Spec.NodeName, ports[0], "tag")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("[]"))
	})
})

var _ = Describe("VLAN 0 is valid", Label("tier2"), func() {
	const (
		claimName = "e2e-vlan-zero-claim"
		podName   = "e2e-pod-vlan-zero"
	)

	It("ovs-vsctl get port tag returns 0 for the native VLAN", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-vlan-zero-policy", NodeNames: []string{workers[0]}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{Name: claimName, Namespace: testNamespace, BridgeName: plat.bridge0, Vlan: intPtr(0)}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl", podData{Name: podName, Namespace: testNamespace, ClaimName: claimName}))
		pod := waitForPodRunning(ctx, testNamespace, podName)

		claim, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claimName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		ports := waitForOvsPorts(ctx, pod.Spec.NodeName, string(claim.UID))

		got, err := ovsPortGet(ctx, pod.Spec.NodeName, ports[0], "tag")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("0"))
	})
})

var _ = Describe("Invalid VLAN rejected", Label("tier2"), func() {
	const (
		claimName = "e2e-vlan-invalid-claim"
		podName   = "e2e-pod-vlan-invalid"
	)

	It("pod does not reach Running when vlan is out of range [0, 4095]", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-vlan-invalid-policy", NodeNames: []string{workers[0]}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{Name: claimName, Namespace: testNamespace, BridgeName: plat.bridge0, Vlan: intPtr(5000)}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl", podData{Name: podName, Namespace: testNamespace, ClaimName: claimName}))

		Consistently(func(g Gomega) {
			p, err := cs.CoreV1().Pods(testNamespace).Get(ctx, podName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p.Status.Phase).NotTo(Equal(corev1.PodRunning))
		}).WithTimeout(20 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
	})
})

var _ = Describe("Negative VLAN rejected", Label("tier2"), func() {
	const (
		claimName = "e2e-vlan-negative-claim"
		podName   = "e2e-pod-vlan-negative"
	)

	It("pod does not reach Running when vlan is negative", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-vlan-negative-policy", NodeNames: []string{workers[0]}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{Name: claimName, Namespace: testNamespace, BridgeName: plat.bridge0, Vlan: intPtr(-1)}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl", podData{Name: podName, Namespace: testNamespace, ClaimName: claimName}))

		Consistently(func(g Gomega) {
			p, err := cs.CoreV1().Pods(testNamespace).Get(ctx, podName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p.Status.Phase).NotTo(Equal(corev1.PodRunning))
		}).WithTimeout(20 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
	})
})

var _ = Describe("VLAN apply to all", Label("tier2"), func() {
	const (
		claimName = "e2e-vlan-apply-all-claim"
		podName   = "e2e-pod-vlan-apply-all"
	)

	It("a config entry with no requests list applies the vlan to all requests", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-vlan-apply-all-policy", NodeNames: []string{workers[0]}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{Name: claimName, Namespace: testNamespace, BridgeName: plat.bridge0, Vlan: intPtr(100), ApplyToAll: true}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl", podData{Name: podName, Namespace: testNamespace, ClaimName: claimName}))
		pod := waitForPodRunning(ctx, testNamespace, podName)

		claim, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claimName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		ports := waitForOvsPorts(ctx, pod.Spec.NodeName, string(claim.UID))

		got, err := ovsPortGet(ctx, pod.Spec.NodeName, ports[0], "tag")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("100"))
	})
})

var _ = Describe("VLAN apply to all with request-specific override", Label("tier2"), func() {
	const (
		claimName = "e2e-vlan-override-claim"
		podName   = "e2e-pod-vlan-override"
	)

	It("the request-specific config wins when listed before the apply-to-all entry", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-vlan-override-policy", NodeNames: []string{workers[0]}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		applyAndCleanup(mustRenderManifest("claim-vlan-override.yaml.tmpl",
			vlanOverrideClaimData{
				Name:         claimName,
				Namespace:    testNamespace,
				BridgeName:   plat.bridge0,
				SpecificVlan: 200,
				GlobalVlan:   100,
			}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl", podData{Name: podName, Namespace: testNamespace, ClaimName: claimName}))
		pod := waitForPodRunning(ctx, testNamespace, podName)

		claim, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claimName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		ports := waitForOvsPorts(ctx, pod.Spec.NodeName, string(claim.UID))

		got, err := ovsPortGet(ctx, pod.Spec.NodeName, ports[0], "tag")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("200"))
	})
})
