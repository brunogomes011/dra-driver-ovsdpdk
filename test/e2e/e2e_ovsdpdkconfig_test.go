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
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// These specs temporarily mutate the cluster-wide "default" OvsDpdkConfig —
// the driver only ever watches the object named "default" — so every one of
// them restores the original content via DeferCleanup(applyYAML, ...)
// before it returns, regardless of pass/fail. Ginkgo runs each spec to
// completion (including its DeferCleanups) before starting the next one, so
// this is safe without a Serial decorator.

var _ = Describe("OvsDpdkConfig named user resolution", Label(tier2), func() {
	It("stat resolves a named user (root) to its numeric UID", func(ctx SpecContext) {
		DeferCleanup(applyYAML, mustRenderManifest("ovsdpdk-config.yaml.tmpl", defaultOvsDpdkConfigData()))

		cfg := defaultOvsDpdkConfigData()
		cfg.User = "root"
		applyYAML(mustRenderManifest("ovsdpdk-config.yaml.tmpl", cfg))

		const (
			claimName = "e2e-config-user-root-claim"
			podName   = "e2e-pod-config-user-root"
		)
		nodeName := workers[0]
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-config-user-root-policy", NodeNames: []string{nodeName}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, nodeName, plat.bridge0)
		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{Name: claimName, Namespace: testNamespace, BridgeName: plat.bridge0}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{Name: podName, Namespace: testNamespace, ClaimName: claimName}))
		pod := waitForPodRunning(ctx, testNamespace, podName)

		socketDir := socketDirPath(string(pod.UID), claimName, "vhost-port")
		Eventually(func() bool { return dirExists(ctx, nodeName, socketDir) }).
			WithTimeout(60 * time.Second).WithPolling(3 * time.Second).Should(BeTrue())

		uid, _ := statOwnership(ctx, nodeName, socketDir)
		Expect(uid).To(Equal("0"), "root should resolve to UID 0")
	})
})

var _ = Describe("OvsDpdkConfig multiple ACL users", Label(tier2), func() {
	It("getfacl shows an entry for every configured ACL user", func(ctx SpecContext) {
		DeferCleanup(applyYAML, mustRenderManifest("ovsdpdk-config.yaml.tmpl", defaultOvsDpdkConfigData()))

		cfg := defaultOvsDpdkConfigData()
		cfg.AclUsers = []string{plat.configAclUser, "root"}
		applyYAML(mustRenderManifest("ovsdpdk-config.yaml.tmpl", cfg))

		const (
			claimName = "e2e-config-multi-acl-claim"
			podName   = "e2e-pod-config-multi-acl"
		)
		nodeName := workers[0]
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-config-multi-acl-policy", NodeNames: []string{nodeName}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, nodeName, plat.bridge0)
		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{Name: claimName, Namespace: testNamespace, BridgeName: plat.bridge0}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{Name: podName, Namespace: testNamespace, ClaimName: claimName}))
		pod := waitForPodRunning(ctx, testNamespace, podName)

		socketDir := socketDirPath(string(pod.UID), claimName, "vhost-port")
		Eventually(func() bool { return dirExists(ctx, nodeName, socketDir) }).
			WithTimeout(60 * time.Second).WithPolling(3 * time.Second).Should(BeTrue())

		Expect(hasACLEntry(ctx, nodeName, socketDir, plat.configAclUser)).To(BeTrue(), "missing ACL entry for %s", plat.configAclUser)
		Expect(hasACLEntry(ctx, nodeName, socketDir, "user:root:rwx")).To(BeTrue(), "missing ACL entry for root")
	})
})

var _ = Describe("OvsDpdkConfig custom containerRootPath", Label(tier2), func() {
	It("the socket directory is mounted at the configured containerRootPath", func(ctx SpecContext) {
		DeferCleanup(applyYAML, mustRenderManifest("ovsdpdk-config.yaml.tmpl", defaultOvsDpdkConfigData()))

		const customRoot = "/custom/ovsdpdk"
		cfg := defaultOvsDpdkConfigData()
		cfg.ContainerRootPath = customRoot
		applyYAML(mustRenderManifest("ovsdpdk-config.yaml.tmpl", cfg))

		const (
			claimName = "e2e-config-rootpath-claim"
			podName   = "e2e-pod-config-rootpath"
		)
		nodeName := workers[0]
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-config-rootpath-policy", NodeNames: []string{nodeName}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, nodeName, plat.bridge0)
		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{Name: claimName, Namespace: testNamespace, BridgeName: plat.bridge0}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{Name: podName, Namespace: testNamespace, ClaimName: claimName}))
		waitForPodRunning(ctx, testNamespace, podName)

		containerDir := filepath.Join(customRoot, claimName, "vhost-port")
		_, err := kubectlExec(ctx, testNamespace, podName, "consumer", "test", "-d", containerDir)
		Expect(err).NotTo(HaveOccurred(), "container dir %s not found", containerDir)
	})
})

var _ = Describe("Missing OvsDpdkConfig", Label("troubleshoot"), func() {
	It("prepare fails when the default OvsDpdkConfig doesn't exist", func(ctx SpecContext) {
		DeferCleanup(applyYAML, mustRenderManifest("ovsdpdk-config.yaml.tmpl", defaultOvsDpdkConfigData()))
		runKubectl("delete", "ovsdpdkconfig", "default", "--ignore-not-found")

		const (
			claimName = "e2e-config-missing-claim"
			podName   = "e2e-pod-config-missing"
		)
		nodeName := workers[0]
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-config-missing-policy", NodeNames: []string{nodeName}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, nodeName, plat.bridge0)
		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{Name: claimName, Namespace: testNamespace, BridgeName: plat.bridge0}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{Name: podName, Namespace: testNamespace, ClaimName: claimName}))

		Consistently(func(g Gomega) {
			p, err := cs.CoreV1().Pods(testNamespace).Get(ctx, podName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p.Status.Phase).NotTo(Equal(corev1.PodRunning))
		}).WithTimeout(20 * time.Second).WithPolling(4 * time.Second).Should(Succeed())
	})
})

var _ = Describe("OvsDpdkConfig updated", Label(tier2), func() {
	It("existing socket dirs keep their ownership; only new ones pick up the new user", func(ctx SpecContext) {
		DeferCleanup(applyYAML, mustRenderManifest("ovsdpdk-config.yaml.tmpl", defaultOvsDpdkConfigData()))

		const (
			claimNameA = "e2e-config-update-claim-a"
			podNameA   = "e2e-pod-config-update-a"
			claimNameB = "e2e-config-update-claim-b"
			podNameB   = "e2e-pod-config-update-b"
			newUser    = "2002"
		)
		nodeName := workers[0]
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-config-update-policy", NodeNames: []string{nodeName}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, nodeName, plat.bridge0)

		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{Name: claimNameA, Namespace: testNamespace, BridgeName: plat.bridge0}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{Name: podNameA, Namespace: testNamespace, ClaimName: claimNameA}))
		podA := waitForPodRunning(ctx, testNamespace, podNameA)

		socketDirA := socketDirPath(string(podA.UID), claimNameA, "vhost-port")
		Eventually(func() bool { return dirExists(ctx, nodeName, socketDirA) }).
			WithTimeout(60 * time.Second).WithPolling(3 * time.Second).Should(BeTrue())

		uidA, _ := statOwnership(ctx, nodeName, socketDirA)
		Expect(uidA).To(Equal(plat.ovsUID), "pod A should have the original UID")

		cfg := defaultOvsDpdkConfigData()
		cfg.User = newUser
		applyYAML(mustRenderManifest("ovsdpdk-config.yaml.tmpl", cfg))

		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{Name: claimNameB, Namespace: testNamespace, BridgeName: plat.bridge0}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{Name: podNameB, Namespace: testNamespace, ClaimName: claimNameB}))
		podB := waitForPodRunning(ctx, testNamespace, podNameB)

		socketDirB := socketDirPath(string(podB.UID), claimNameB, "vhost-port")
		Eventually(func() bool { return dirExists(ctx, nodeName, socketDirB) }).
			WithTimeout(60 * time.Second).WithPolling(3 * time.Second).Should(BeTrue())

		uidB, _ := statOwnership(ctx, nodeName, socketDirB)
		Expect(uidB).To(Equal(newUser), "pod B should have the new UID")

		uidAAfter, _ := statOwnership(ctx, nodeName, socketDirA)
		Expect(uidAAfter).To(Equal(plat.ovsUID), "pod A's dir should not be retroactively modified")
	})
})
