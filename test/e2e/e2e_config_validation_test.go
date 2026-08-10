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

var _ = Describe("SELinux label CRD validation", Label("tier1"), func() {
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

var _ = Describe("MTU CRD validation", Label("tier1"), func() {
	It("valid mtu is accepted by the API server", func(_ SpecContext) {
		manifest := mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-mtu-valid", NodeNames: []string{workers[0]}, Bridges: []string{plat.bridge0}, Mtu: intPtr(9000)})
		applyAndCleanup(manifest)
	})

	It("mtu below 68 is rejected by the API server", func(_ SpecContext) {
		manifest := mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-mtu-too-small", NodeNames: []string{workers[0]}, Bridges: []string{plat.bridge0}, Mtu: intPtr(67)})
		Expect(tryApplyYAML(manifest)).To(HaveOccurred())
	})

	It("mtu above 65535 is rejected by the API server", func(_ SpecContext) {
		manifest := mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-mtu-too-large", NodeNames: []string{workers[0]}, Bridges: []string{plat.bridge0}, Mtu: intPtr(65536)})
		Expect(tryApplyYAML(manifest)).To(HaveOccurred())
	})
})
