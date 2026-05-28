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

package webhook_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/amorenoz/dra-driver-ovsdpdk/pkg/webhook"
)

var _ = Describe("ParsePodNetworkAnnotation", func() {
	const podNS = "default"

	Describe("JSON format", func() {
		It("parses a single network", func() {
			nets, err := webhook.ParsePodNetworkAnnotation(`[{"name":"net1"}]`, podNS)
			Expect(err).NotTo(HaveOccurred())
			Expect(nets).To(HaveLen(1))
			Expect(nets[0].Name).To(Equal("net1"))
			Expect(nets[0].Namespace).To(Equal(podNS))
			Expect(nets[0].InterfaceRequest).To(BeEmpty())
		})

		It("parses multiple networks", func() {
			nets, err := webhook.ParsePodNetworkAnnotation(`[{"name":"net1"},{"name":"net2","namespace":"other"}]`, podNS)
			Expect(err).NotTo(HaveOccurred())
			Expect(nets).To(HaveLen(2))
			Expect(nets[0].Namespace).To(Equal(podNS))
			Expect(nets[1].Namespace).To(Equal("other"))
		})

		It("preserves explicit interface name", func() {
			nets, err := webhook.ParsePodNetworkAnnotation(`[{"name":"net1","interface":"vhost0"}]`, podNS)
			Expect(err).NotTo(HaveOccurred())
			Expect(nets[0].InterfaceRequest).To(Equal("vhost0"))
		})

		It("preserves explicit namespace", func() {
			nets, err := webhook.ParsePodNetworkAnnotation(`[{"name":"net1","namespace":"ns2"}]`, podNS)
			Expect(err).NotTo(HaveOccurred())
			Expect(nets[0].Namespace).To(Equal("ns2"))
		})

		It("rejects missing name", func() {
			_, err := webhook.ParsePodNetworkAnnotation(`[{"namespace":"ns"}]`, podNS)
			Expect(err).To(HaveOccurred())
		})

		It("rejects invalid name", func() {
			_, err := webhook.ParsePodNetworkAnnotation(`[{"name":"INVALID_NAME"}]`, podNS)
			Expect(err).To(HaveOccurred())
		})

		It("rejects invalid interface name", func() {
			_, err := webhook.ParsePodNetworkAnnotation(`[{"name":"net1","interface":"BAD_IFACE"}]`, podNS)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("text format", func() {
		It("parses a simple name", func() {
			nets, err := webhook.ParsePodNetworkAnnotation("net1", podNS)
			Expect(err).NotTo(HaveOccurred())
			Expect(nets).To(HaveLen(1))
			Expect(nets[0].Name).To(Equal("net1"))
			Expect(nets[0].Namespace).To(Equal(podNS))
			Expect(nets[0].InterfaceRequest).To(BeEmpty())
		})

		It("parses namespace/name", func() {
			nets, err := webhook.ParsePodNetworkAnnotation("ns1/net1", podNS)
			Expect(err).NotTo(HaveOccurred())
			Expect(nets[0].Namespace).To(Equal("ns1"))
			Expect(nets[0].Name).To(Equal("net1"))
		})

		It("parses name@interface", func() {
			nets, err := webhook.ParsePodNetworkAnnotation("net1@vhost0", podNS)
			Expect(err).NotTo(HaveOccurred())
			Expect(nets[0].Name).To(Equal("net1"))
			Expect(nets[0].InterfaceRequest).To(Equal("vhost0"))
		})

		It("parses namespace/name@interface", func() {
			nets, err := webhook.ParsePodNetworkAnnotation("ns1/net1@vhost0", podNS)
			Expect(err).NotTo(HaveOccurred())
			Expect(nets[0].Namespace).To(Equal("ns1"))
			Expect(nets[0].Name).To(Equal("net1"))
			Expect(nets[0].InterfaceRequest).To(Equal("vhost0"))
		})

		It("parses comma-separated list", func() {
			nets, err := webhook.ParsePodNetworkAnnotation("net1,ns2/net2@eth1", podNS)
			Expect(err).NotTo(HaveOccurred())
			Expect(nets).To(HaveLen(2))
			Expect(nets[0].Name).To(Equal("net1"))
			Expect(nets[1].Namespace).To(Equal("ns2"))
			Expect(nets[1].Name).To(Equal("net2"))
			Expect(nets[1].InterfaceRequest).To(Equal("eth1"))
		})

		It("trims whitespace around commas", func() {
			nets, err := webhook.ParsePodNetworkAnnotation("net1 , net2", podNS)
			Expect(err).NotTo(HaveOccurred())
			Expect(nets).To(HaveLen(2))
		})

		It("rejects invalid name", func() {
			_, err := webhook.ParsePodNetworkAnnotation("INVALID", podNS)
			Expect(err).To(HaveOccurred())
		})

		It("rejects invalid namespace", func() {
			_, err := webhook.ParsePodNetworkAnnotation("BAD_NS/net1", podNS)
			Expect(err).To(HaveOccurred())
		})

		It("rejects invalid interface", func() {
			_, err := webhook.ParsePodNetworkAnnotation("net1@BAD_IFACE", podNS)
			Expect(err).To(HaveOccurred())
		})
	})

	It("returns error for empty annotation", func() {
		_, err := webhook.ParsePodNetworkAnnotation("", podNS)
		Expect(err).To(HaveOccurred())
	})
})
