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

var _ = Describe("Ingress policing", Ordered, Label("tier1"), func() {
	const (
		claimName = "e2e-policing"
		podName   = "e2e-pod-policing"
	)

	var pod *corev1.Pod
	var ports []string
	var claimUID string

	BeforeAll(func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-policing-policy", NodeNames: []string{workers[0]}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{
				Name:       claimName,
				Namespace:  testNamespace,
				BridgeName: plat.bridge0,
				MaxRate:    100000, // 100 Mbps in kbps
				Burst:      10000,  // 10 Mb in kb
			}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl", podData{Name: podName, Namespace: testNamespace, ClaimName: claimName}))
		pod = waitForPodRunning(ctx, testNamespace, podName)

		c, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claimName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		claimUID = string(c.UID)

		ports = waitForOvsPorts(ctx, pod.Spec.NodeName, claimUID)
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

var _ = Describe("Ingress policing absent", Label("tier2"), func() {
	It("policing is absent from OVS interface when not configured", func(ctx SpecContext) {
		const (
			plainClaimName = "e2e-policing-absent"
			plainPodName   = "e2e-pod-policing-absent"
		)
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-policing-absent-policy", NodeNames: []string{workers[0]}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{Name: plainClaimName, Namespace: testNamespace, BridgeName: plat.bridge0}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{Name: plainPodName, Namespace: testNamespace, ClaimName: plainClaimName}))
		plainPod := waitForPodRunning(ctx, testNamespace, plainPodName)

		c, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, plainClaimName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		plainPorts := waitForOvsPorts(ctx, plainPod.Spec.NodeName, string(c.UID))

		got, err := ovsInterfaceGet(ctx, plainPod.Spec.NodeName, plainPorts[0], "ingress_policing_rate")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("0"))
	})
})
