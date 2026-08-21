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

var _ = Describe("Checkpoint persistence across driver restart", Label(tier2), func() {
	const (
		claimName = "e2e-persist-claim"
		podName   = "e2e-persist-pod"
	)

	It("unprepare cleans up OVS port and socket dir after driver restart", func(ctx SpecContext) {
		nodeName := workers[0]

		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-persist-policy", NodeNames: []string{nodeName}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, nodeName, plat.bridge0)
		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{Name: claimName, Namespace: testNamespace, BridgeName: plat.bridge0}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{Name: podName, Namespace: testNamespace, ClaimName: claimName}))
		pod := waitForPodRunning(ctx, testNamespace, podName)

		claim, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claimName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		claimUID := string(claim.UID)

		waitForOvsPorts(ctx, nodeName, claimUID)

		socketDir := socketDirPath(string(pod.UID), claimName, "vhost-port")

		By("Restarting the driver pod on " + nodeName)
		restartDriverOnNode(ctx, nodeName)
		waitForDeviceInSlice(ctx, nodeName, plat.bridge0)

		By("Deleting the test pod to trigger unprepare after restart")
		deletePodAndWait(ctx, testNamespace, podName)

		Eventually(func(g Gomega) {
			p, err := ovsPortsForClaim(ctx, nodeName, claimUID)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p).To(BeEmpty())
		}).WithTimeout(30 * time.Second).WithPolling(3 * time.Second).Should(Succeed())

		Eventually(func() bool { return dirExists(ctx, nodeName, socketDir) }).
			WithTimeout(30 * time.Second).WithPolling(3 * time.Second).Should(BeFalse())
	})
})

var _ = Describe("Driver restart persistence", Label(tier2), func() {
	It("existing pods are unaffected by a driver restart", func(ctx SpecContext) {
		const (
			claimName = "e2e-persist-unaffected-claim"
			podName   = "e2e-persist-unaffected-pod"
		)
		nodeName := workers[0]

		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-persist-unaffected-policy", NodeNames: []string{nodeName}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, nodeName, plat.bridge0)
		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{Name: claimName, Namespace: testNamespace, BridgeName: plat.bridge0}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{Name: podName, Namespace: testNamespace, ClaimName: claimName}))
		pod := waitForPodRunning(ctx, testNamespace, podName)

		claim, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claimName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		claimUID := string(claim.UID)

		waitForOvsPorts(ctx, nodeName, claimUID)
		socketDir := socketDirPath(string(pod.UID), claimName, "vhost-port")
		Expect(dirExists(ctx, nodeName, socketDir)).To(BeTrue(), "socket dir should exist before restart")

		By("Restarting the driver pod on " + nodeName)
		restartDriverOnNode(ctx, nodeName)
		waitForDeviceInSlice(ctx, nodeName, plat.bridge0)

		By("Confirming the consumer pod was never disrupted")
		p, err := cs.CoreV1().Pods(testNamespace).Get(ctx, podName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(p.Status.Phase).To(Equal(corev1.PodRunning), "consumer pod should remain Running across driver restart")

		ports, err := ovsPortsForClaim(ctx, nodeName, claimUID)
		Expect(err).NotTo(HaveOccurred())
		Expect(ports).NotTo(BeEmpty(), "OVS port should remain intact across driver restart")

		Expect(dirExists(ctx, nodeName, socketDir)).To(BeTrue(), "socket dir should remain intact across driver restart")
	})

	It("new claims work after a driver restart", func(ctx SpecContext) {
		const (
			claimName = "e2e-persist-postrestart-claim"
			podName   = "e2e-persist-postrestart-pod"
		)
		nodeName := workers[0]

		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-persist-postrestart-policy", NodeNames: []string{nodeName}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, nodeName, plat.bridge0)

		By("Restarting the driver pod on " + nodeName)
		restartDriverOnNode(ctx, nodeName)
		waitForDeviceInSlice(ctx, nodeName, plat.bridge0)

		By("Creating a brand new claim after the restart")
		applyAndCleanup(mustRenderManifest("claim.yaml.tmpl",
			claimData{Name: claimName, Namespace: testNamespace, BridgeName: plat.bridge0}))
		applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
			podData{Name: podName, Namespace: testNamespace, ClaimName: claimName}))
		pod := waitForPodRunning(ctx, testNamespace, podName)

		claim, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, claimName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		waitForOvsPorts(ctx, nodeName, string(claim.UID))
		socketDir := socketDirPath(string(pod.UID), claimName, "vhost-port")
		Expect(dirExists(ctx, nodeName, socketDir)).To(BeTrue(), "socket dir should be created for a claim prepared after restart")
	})
})
