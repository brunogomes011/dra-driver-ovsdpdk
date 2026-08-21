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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("VLAN tag applied", Label(tier1), func() {
	const base = "e2e-vlan-tag"

	It("ovs-vsctl get port tag reflects the configured vlan", func(ctx SpecContext) {
		applyPolicy(ctx, base, workers[0], plat.bridge0)
		pod, uid := applyClaimPod(ctx, base, claimData{BridgeName: plat.bridge0, Vlan: intPtr(100)})
		ports := waitForOvsPorts(ctx, pod.Spec.NodeName, uid)

		got, err := ovsPortGet(ctx, pod.Spec.NodeName, ports[0], "tag")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("100"))
	})
})

var _ = Describe("No VLAN when unset", Label(tier2), func() {
	const base = "e2e-vlan-unset"

	It("ovs-vsctl get port tag returns [] (trunk) when vlan is not configured", func(ctx SpecContext) {
		applyPolicy(ctx, base, workers[0], plat.bridge0)
		pod, uid := applyClaimPod(ctx, base, claimData{BridgeName: plat.bridge0})
		ports := waitForOvsPorts(ctx, pod.Spec.NodeName, uid)

		got, err := ovsPortGet(ctx, pod.Spec.NodeName, ports[0], "tag")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("[]"))
	})
})

var _ = Describe("VLAN 0 is valid", Label(tier2), func() {
	const base = "e2e-vlan-zero"

	It("ovs-vsctl get port tag returns 0 for the native VLAN", func(ctx SpecContext) {
		applyPolicy(ctx, base, workers[0], plat.bridge0)
		pod, uid := applyClaimPod(ctx, base, claimData{BridgeName: plat.bridge0, Vlan: intPtr(0)})
		ports := waitForOvsPorts(ctx, pod.Spec.NodeName, uid)

		got, err := ovsPortGet(ctx, pod.Spec.NodeName, ports[0], "tag")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("0"))
	})
})

var _ = Describe("Invalid VLAN rejected", Label(tier2), func() {
	const base = "e2e-vlan-invalid"

	It("pod does not reach Running when vlan is out of range [0, 4095]", func(ctx SpecContext) {
		applyPolicy(ctx, base, workers[0], plat.bridge0)
		applyClaimAndPod(base, claimData{BridgeName: plat.bridge0, Vlan: intPtr(5000)})
		consistentlyNotRunning(ctx, base+"-pod")
	})
})

var _ = Describe("Negative VLAN rejected", Label(tier2), func() {
	const base = "e2e-vlan-negative"

	It("pod does not reach Running when vlan is negative", func(ctx SpecContext) {
		applyPolicy(ctx, base, workers[0], plat.bridge0)
		applyClaimAndPod(base, claimData{BridgeName: plat.bridge0, Vlan: intPtr(-1)})
		consistentlyNotRunning(ctx, base+"-pod")
	})
})

var _ = Describe("VLAN apply to all", Label(tier2), func() {
	const base = "e2e-vlan-apply-all"

	It("a config entry with no requests list applies the vlan to all requests", func(ctx SpecContext) {
		applyPolicy(ctx, base, workers[0], plat.bridge0)
		pod, uid := applyClaimPod(ctx, base,
			claimData{BridgeName: plat.bridge0, Vlan: intPtr(100), ApplyToAll: true})
		ports := waitForOvsPorts(ctx, pod.Spec.NodeName, uid)

		got, err := ovsPortGet(ctx, pod.Spec.NodeName, ports[0], "tag")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("100"))
	})
})

var _ = Describe("VLAN apply to all with request-specific override", Label(tier2), func() {
	const base = "e2e-vlan-override"

	// This spec uses a bespoke claim template (two ordered config entries) that
	// claimData/applyClaimPod don't model, so the claim is rendered and the pod
	// applied explicitly — applyPolicy still covers the shared setup.
	It("the request-specific config wins when listed before the apply-to-all entry", func(ctx SpecContext) {
		applyPolicy(ctx, base, workers[0], plat.bridge0)

		applyAndCleanup(mustRenderManifest("claim-vlan-override.yaml.tmpl",
			vlanOverrideClaimData{
				Name:         base + "-claim",
				Namespace:    testNamespace,
				BridgeName:   plat.bridge0,
				SpecificVlan: 200,
				GlobalVlan:   100,
			}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{Name: base + "-pod", Namespace: testNamespace, ClaimName: base + "-claim"}))
		pod := waitForPodRunning(ctx, testNamespace, base+"-pod")
		uid := claimUIDByName(ctx, base+"-claim")

		ports := waitForOvsPorts(ctx, pod.Spec.NodeName, uid)
		got, err := ovsPortGet(ctx, pod.Spec.NodeName, ports[0], "tag")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("200"))
	})
})
