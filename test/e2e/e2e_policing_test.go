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

var _ = Describe("Ingress policing", Ordered, Label(tier2), func() {
	const base = "e2e-policing"

	var pod *corev1.Pod
	var ports []string

	BeforeAll(func(ctx SpecContext) {
		applyPolicy(ctx, base, workers[0], plat.bridge0)
		var uid string
		pod, uid = applyClaimPod(ctx, base, claimData{
			BridgeName: plat.bridge0,
			MaxRate:    uint32Ptr(100000), // 100 Mbps in kbps
			Burst:      10000,             // 10 Mb in kb
		})
		ports = waitForOvsPorts(ctx, pod.Spec.NodeName, uid)
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
			c, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, base+"-claim", metav1.GetOptions{})
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

var _ = Describe("Ingress policing absent", Label(tier2), func() {
	const base = "e2e-policing-absent"

	It("policing is absent from OVS interface when not configured", func(ctx SpecContext) {
		applyPolicy(ctx, base, workers[0], plat.bridge0)
		pod, uid := applyClaimPod(ctx, base, claimData{BridgeName: plat.bridge0})
		ports := waitForOvsPorts(ctx, pod.Spec.NodeName, uid)

		got, err := ovsInterfaceGet(ctx, pod.Spec.NodeName, ports[0], "ingress_policing_rate")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("0"))
	})
})

var _ = Describe("Ingress policing rate only", Label(tier2), func() {
	const base = "e2e-policing-rate-only"

	It("burst is 0 on the OVS interface when max_rate is set without a burst", func(ctx SpecContext) {
		applyPolicy(ctx, base, workers[0], plat.bridge0)
		pod, uid := applyClaimPod(ctx, base, claimData{BridgeName: plat.bridge0, MaxRate: uint32Ptr(50000)})
		ports := waitForOvsPorts(ctx, pod.Spec.NodeName, uid)

		rate, err := ovsInterfaceGet(ctx, pod.Spec.NodeName, ports[0], "ingress_policing_rate")
		Expect(err).NotTo(HaveOccurred())
		Expect(rate).To(Equal("50000"))

		burst, err := ovsInterfaceGet(ctx, pod.Spec.NodeName, ports[0], "ingress_policing_burst")
		Expect(err).NotTo(HaveOccurred())
		Expect(burst).To(Equal("0"))
	})
})

var _ = Describe("Ingress policing without max_rate rejected", Label(tier2), func() {
	const base = "e2e-policing-no-rate"

	It("pod does not reach Running when burst is set without max_rate", func(ctx SpecContext) {
		applyPolicy(ctx, base, workers[0], plat.bridge0)
		applyClaimAndPod(base, claimData{BridgeName: plat.bridge0, Burst: 1000})
		consistentlyNotRunning(ctx, base+"-pod")
	})
})

var _ = Describe("Ingress policing rate zero", Label(tier2), func() {
	const base = "e2e-policing-rate-zero"

	It("ingress_policing_rate is 0 (unlimited, no policing enforced) when max_rate is explicitly 0", func(ctx SpecContext) {
		applyPolicy(ctx, base, workers[0], plat.bridge0)
		pod, uid := applyClaimPod(ctx, base, claimData{BridgeName: plat.bridge0, MaxRate: uint32Ptr(0)})
		ports := waitForOvsPorts(ctx, pod.Spec.NodeName, uid)

		got, err := ovsInterfaceGet(ctx, pod.Spec.NodeName, ports[0], "ingress_policing_rate")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("0"))
	})
})
