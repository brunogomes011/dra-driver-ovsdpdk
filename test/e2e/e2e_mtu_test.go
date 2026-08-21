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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("MTU", Ordered, Label(tier1), func() {
	const (
		claimName = "e2e-mtu-claim"
		podName   = "e2e-mtu-pod"
		mtu       = 9000
	)

	var pod *corev1.Pod
	var ports []string
	var claimUID string

	BeforeAll(func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-mtu-policy", NodeNames: []string{workers[0]}, Bridges: []string{plat.bridge0}, Mtu: intPtr(mtu)}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)

		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{Name: claimName, Namespace: testNamespace, BridgeName: plat.bridge0}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{Name: podName, Namespace: testNamespace, ClaimName: claimName}))
		pod = waitForPodRunning(ctx, testNamespace, podName)

		c, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claimName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		claimUID = string(c.UID)

		ports = waitForOvsPorts(ctx, pod.Spec.NodeName, claimUID)
	})

	It("mtu attribute is present in the ResourceSlice device", Label(tier1), func(ctx SpecContext) {
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

	It("mtu_request is set on the OVS interface", Label(tier1), func(ctx SpecContext) {
		got, err := ovsInterfaceGet(ctx, pod.Spec.NodeName, ports[0], "mtu_request")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(fmt.Sprintf("%d", mtu)))
	})

	It("mtu is present in the in-container device metadata file", Label(tier1), func(ctx SpecContext) {
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

var _ = Describe("MTU absent", Label(tier2), func() {
	It("mtu_request is absent when no mtu is configured", func(ctx SpecContext) {
		const (
			plainClaimName = "e2e-mtu-absent-claim"
			plainPodName   = "e2e-mtu-absent-pod"
		)
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-mtu-absent-policy", NodeNames: []string{workers[0]}, Bridges: []string{plat.bridge1}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge1)
		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{Name: plainClaimName, Namespace: testNamespace, BridgeName: plat.bridge1}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{Name: plainPodName, Namespace: testNamespace, ClaimName: plainClaimName}))
		plainPod := waitForPodRunning(ctx, testNamespace, plainPodName)

		c, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, plainClaimName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		plainPorts := waitForOvsPorts(ctx, plainPod.Spec.NodeName, string(c.UID))

		got, err := ovsInterfaceGet(ctx, plainPod.Spec.NodeName, plainPorts[0], "mtu_request")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("[]")) // OVS returns [] for unset optional integer columns
	})
})
