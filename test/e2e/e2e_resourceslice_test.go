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

	resourceapi "k8s.io/api/resource/v1"
)

var _ = Describe("ResourceSlice advertisement", Label(tier1), func() {
	BeforeEach(func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-rs-policy-workers", NodeNames: []string{workers[0], workers[1]}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		waitForDeviceInSlice(ctx, workers[1], plat.bridge0)

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

var _ = Describe("Node selector", Label(tier2), func() {
	BeforeEach(func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-ns-policy-worker1", NodeNames: []string{workers[0]}, Bridges: []string{plat.bridge0, plat.bridge1}}))
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-ns-policy-worker2", NodeNames: []string{workers[1]}, Bridges: []string{plat.bridge2}}))
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

var _ = Describe("Policy overlap", Label(tier2), func() {
	BeforeEach(func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-mp-policy-shared", NodeNames: []string{workers[0], workers[1]}, Bridges: []string{plat.bridge0}}))
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-mp-policy-worker1", NodeNames: []string{workers[1]}, Bridges: []string{plat.bridge1}}))
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

var _ = Describe("Policy update - Replace bridge", Label(tier2), func() {
	It("replacing a bridge in the policy updates ResourceSlices accordingly", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-policy-replace", NodeNames: []string{workers[0], workers[1]}, Bridges: []string{plat.bridge0, plat.bridge1}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		waitForDeviceInSlice(ctx, workers[0], plat.bridge1)
		waitForDeviceInSlice(ctx, workers[1], plat.bridge0)
		waitForDeviceInSlice(ctx, workers[1], plat.bridge1)

		applyYAML(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-policy-replace", NodeNames: []string{workers[0], workers[1]}, Bridges: []string{plat.bridge0, plat.bridge2}}))

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

var _ = Describe("Duplicate detection", Label(tier2), func() {
	It("second policy with a duplicate bridge does not advertise any of its bridges", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-dup-base", NodeNames: []string{workers[0], workers[1]}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		waitForDeviceInSlice(ctx, workers[1], plat.bridge0)

		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-dup-extra", NodeNames: []string{workers[1]}, Bridges: []string{plat.bridge0, plat.bridge1}}))

		Consistently(func(g Gomega) {
			nodeSlices, err := resourceSlicesForNode(ctx, workers[1])
			g.Expect(err).NotTo(HaveOccurred())
			devices := deviceNamesFromSlices(nodeSlices)
			g.Expect(devices).To(ContainElement(plat.bridge0))
			g.Expect(devices).NotTo(ContainElement(plat.bridge1))
		}).WithTimeout(15 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
	})
})

var _ = Describe("Policy update - Remove bridge", Label(tier2), func() {
	It("removing a bridge from the policy removes it from ResourceSlices", func(ctx SpecContext) {
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-policy-update", NodeNames: []string{workers[0], workers[1]}, Bridges: []string{plat.bridge0, plat.bridge1}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		waitForDeviceInSlice(ctx, workers[0], plat.bridge1)
		waitForDeviceInSlice(ctx, workers[1], plat.bridge0)
		waitForDeviceInSlice(ctx, workers[1], plat.bridge1)

		applyYAML(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-policy-update", NodeNames: []string{workers[0], workers[1]}, Bridges: []string{plat.bridge0}}))

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

var _ = Describe("Policy API validation", Label(tier1), func() {
	It("policy without bridges is rejected by the API server", func(_ SpecContext) {
		manifest := mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-policy-no-bridge", NodeNames: []string{workers[0]}, Bridges: []string{}})
		Expect(tryApplyYAML(manifest)).To(HaveOccurred())
	})

	It("a bridge entry without a name is rejected by the API server", func(_ SpecContext) {
		manifest := mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-policy-bridge-no-name", NodeNames: []string{workers[0]}, Bridges: []string{""}})
		Expect(tryApplyYAML(manifest)).To(HaveOccurred())
	})
})
