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

	resourceapi "k8s.io/api/resource/v1"
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
