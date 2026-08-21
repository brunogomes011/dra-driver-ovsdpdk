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

var _ = Describe("SELinux label CRD validation", Label(tier2), func() {
	It("valid label is accepted by the API server", func(_ SpecContext) {
		manifest := mustRenderManifest("ovsdpdk-config.yaml.tmpl",
			ovsDpdkConfigData{Name: "e2e-selinux-valid", SelinuxLabel: "system_u:object_r:container_file_t:s0", User: plat.configUser, Group: plat.configGroup, AclUsers: []string{plat.configAclUser}})
		applyAndCleanup(manifest)
	})

	It("label missing a component is rejected", func(_ SpecContext) {
		manifest := mustRenderManifest("ovsdpdk-config.yaml.tmpl",
			ovsDpdkConfigData{Name: "e2e-selinux-invalid-short", SelinuxLabel: "system_u:object_r:container_file_t", User: plat.configUser, Group: plat.configGroup, AclUsers: []string{plat.configAclUser}})
		Expect(tryApplyYAML(manifest)).To(HaveOccurred())
	})

	It("label with an empty component is rejected", func(_ SpecContext) {
		manifest := mustRenderManifest("ovsdpdk-config.yaml.tmpl",
			ovsDpdkConfigData{Name: "e2e-selinux-invalid-empty", SelinuxLabel: "system_u::container_file_t:s0", User: plat.configUser, Group: plat.configGroup, AclUsers: []string{plat.configAclUser}})
		Expect(tryApplyYAML(manifest)).To(HaveOccurred())
	})

	It("label with no colons is rejected", func(_ SpecContext) {
		manifest := mustRenderManifest("ovsdpdk-config.yaml.tmpl",
			ovsDpdkConfigData{Name: "e2e-selinux-invalid-plain", SelinuxLabel: "badlabel", User: plat.configUser, Group: plat.configGroup, AclUsers: []string{plat.configAclUser}})
		Expect(tryApplyYAML(manifest)).To(HaveOccurred())
	})
})

var _ = Describe("MTU CRD validation", Label(tier2), func() {
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

var _ = Describe("Opaque config validation", Label(tier2), func() {
	It("prepare fails when the opaque config kind is wrong", func(ctx SpecContext) {
		const (
			claimName = "e2e-config-wrong-kind"
			podName   = "e2e-pod-config-wrong-kind"
		)
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-config-wrong-kind-policy", NodeNames: []string{workers[0]}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{Name: claimName, Namespace: testNamespace, BridgeName: plat.bridge0, ConfigKind: "WrongKind"}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl", podData{Name: podName, Namespace: testNamespace, ClaimName: claimName}))

		Consistently(func(g Gomega) {
			p, err := cs.CoreV1().Pods(testNamespace).Get(ctx, podName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p.Status.Phase).NotTo(Equal(corev1.PodRunning))
		}).WithTimeout(20 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
	})

	It("prepare fails when the opaque config apiVersion is wrong", func(ctx SpecContext) {
		const (
			claimName = "e2e-config-wrong-apiversion"
			podName   = "e2e-pod-config-wrong-apiversion"
		)
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-config-wrong-apiversion-policy", NodeNames: []string{workers[0]}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, workers[0], plat.bridge0)
		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{Name: claimName, Namespace: testNamespace, BridgeName: plat.bridge0, ConfigAPIVersion: "wrong/v1"}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl", podData{Name: podName, Namespace: testNamespace, ClaimName: claimName}))

		Consistently(func(g Gomega) {
			p, err := cs.CoreV1().Pods(testNamespace).Get(ctx, podName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p.Status.Phase).NotTo(Equal(corev1.PodRunning))
		}).WithTimeout(20 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
	})
})
