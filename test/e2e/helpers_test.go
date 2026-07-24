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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/template"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/dynamic-resource-allocation/api/metadata"
	"k8s.io/dynamic-resource-allocation/devicemetadata"
)

// --- Template rendering ---

func renderManifest(name string, data any) (string, error) {
	raw, err := os.ReadFile(manifestPath(name))
	if err != nil {
		return "", fmt.Errorf("read template %s: %w", name, err)
	}
	tmpl, err := template.New(name).Parse(string(raw))
	if err != nil {
		return "", fmt.Errorf("parse template %s: %w", name, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template %s: %w", name, err)
	}
	return buf.String(), nil
}

func mustRenderManifest(name string, data any) string {
	s, err := renderManifest(name, data)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "render %s", name)
	return s
}

// --- Template data types ---

type claimData struct {
	Name, Namespace, BridgeName string
}

type podData struct {
	Name, Namespace, ClaimName string
}

type ovsDpdkConfigData struct {
	Name, SelinuxLabel string
}

type multiClaimPodData struct {
	Name, Namespace string
	ClaimNames      []string
}

type multiRequestClaimData struct {
	Name, Namespace, BridgeName string
	Ports                       []string
}

type policingClaimData struct {
	Name, Namespace, BridgeName string
	MaxRate                     uint32
	Burst                       uint32 // 0 means omit from manifest
}

type policyData struct {
	Name, NodeName string
	Bridges        []string
}

type topologyPolicyData struct {
	Name, NodeName, BridgeName, TopologyResource string
}

type mtuPolicyData struct {
	Name, NodeName, BridgeName string
	Mtu                        int
}

type topologyPodData struct {
	Name, Namespace, ClaimName, TopologyResource string
}

// --- kubectl manifest apply/delete ---
//
// All helpers use the package-level kubeconfig var set in BeforeSuite.

// applyManifest applies a static YAML file from manifests/.
func applyManifest(name string) {
	GinkgoHelper()
	runKubectl("apply", "-f", manifestPath(name))
}

// deleteManifest deletes a static YAML file from manifests/ (ignores not-found).
func deleteManifest(name string) {
	GinkgoHelper()
	runKubectl("delete", "--ignore-not-found", "-f", manifestPath(name))
}

// applyYAML applies a rendered YAML string via kubectl.
func applyYAML(manifest string) {
	GinkgoHelper()
	runKubectlStdin(manifest, "apply", "-f", "-")
}

// deleteYAML deletes resources described by a rendered YAML string and waits
// for them to be fully removed (finalizers included).  The 30s timeout ensures
// we detect stuck unprepare rather than hanging indefinitely.
func deleteYAML(manifest string) {
	GinkgoHelper()
	runKubectlStdin(manifest, "delete", "--ignore-not-found", "--timeout=30s", "-f", "-")
}

// tryApplyYAML applies a rendered YAML string and returns an error instead of
// failing the test.  Use for tests that expect the API server to reject input.
func tryApplyYAML(manifest string) error {
	allArgs := []string{"--kubeconfig", kubeconfig, "apply", "-f", "-"}
	cmd := exec.Command("kubectl", allArgs...)
	cmd.Stdin = strings.NewReader(manifest)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// applyAndCleanup applies a rendered YAML string and registers DeferCleanup
// to delete it when the current test/suite scope exits.
func applyAndCleanup(manifest string) {
	GinkgoHelper()
	applyYAML(manifest)
	DeferCleanup(deleteYAML, manifest)
}

func runKubectl(args ...string) {
	GinkgoHelper()
	allArgs := append([]string{"--kubeconfig", kubeconfig}, args...)
	out, err := exec.Command("kubectl", allArgs...).CombinedOutput()
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "kubectl %v:\n%s", args, out)
}

func runKubectlStdin(stdin string, args ...string) {
	GinkgoHelper()
	allArgs := append([]string{"--kubeconfig", kubeconfig}, args...)
	cmd := exec.Command("kubectl", allArgs...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "kubectl %v:\n%s", args, out)
}

// --- kubectl exec helpers ---

// kubectlExec runs a command inside a pod.  If container is empty, the
// default container is used.
func kubectlExec(ctx context.Context, namespace, podName, container string, args ...string) (string, error) {
	kubectlArgs := []string{"--kubeconfig", kubeconfig, "-n", namespace, "exec"}
	if container != "" {
		kubectlArgs = append(kubectlArgs, "-c", container)
	}
	kubectlArgs = append(kubectlArgs, podName, "--")
	kubectlArgs = append(kubectlArgs, args...)

	var out, errOut bytes.Buffer
	cmd := exec.CommandContext(ctx, "kubectl", kubectlArgs...)
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("kubectl exec %s %v: %w\n%s", podName, args, err, errOut.String())
	}
	return strings.TrimSpace(out.String()), nil
}

// driverPodExec runs a command inside the driver pod on nodeName.
func driverPodExec(ctx context.Context, nodeName string, args ...string) (string, error) {
	podName, err := driverPodOnNode(ctx, nodeName)
	if err != nil {
		return "", err
	}
	return kubectlExec(ctx, driverNamespace, podName, "", args...)
}

func driverPodOnNode(ctx context.Context, nodeName string) (string, error) {
	pods, err := cs.CoreV1().Pods(driverNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app=dra-driver-ovsdpdk",
		FieldSelector: "spec.nodeName=" + nodeName,
	})
	if err != nil {
		return "", fmt.Errorf("list driver pods on %s: %w", nodeName, err)
	}
	if len(pods.Items) == 0 {
		return "", fmt.Errorf("no driver pod found on node %s", nodeName)
	}
	return pods.Items[0].Name, nil
}

// ovsPodExec runs a command inside the ovs-vswitchd container of the OVS pod
// on nodeName.
func ovsPodExec(ctx context.Context, nodeName string, args ...string) (string, error) {
	pods, err := cs.CoreV1().Pods(driverNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app=ovs",
		FieldSelector: "spec.nodeName=" + nodeName,
	})
	if err != nil {
		return "", fmt.Errorf("find OVS pod on %s: %w", nodeName, err)
	}
	if len(pods.Items) == 0 {
		return "", fmt.Errorf("no OVS pod on node %s", nodeName)
	}
	return kubectlExec(ctx, driverNamespace, pods.Items[0].Name, "ovs-vswitchd", args...)
}

// --- OVS helpers ---

func ovsPortsForClaim(ctx context.Context, nodeName, claimUID string) ([]string, error) {
	out, err := ovsPodExec(ctx, nodeName,
		"ovs-vsctl", "--columns=name", "--bare",
		"find", "port", "external_ids:claim-uid="+claimUID)
	if err != nil {
		return nil, err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}
	var ports []string
	for _, line := range strings.Split(out, "\n") {
		if name := strings.TrimSpace(line); name != "" {
			ports = append(ports, name)
		}
	}
	return ports, nil
}

// ovsInterfaceGet returns the value of a single column on an OVS Interface row.
func ovsInterfaceGet(ctx context.Context, nodeName, portName, column string) (string, error) {
	return ovsPodExec(ctx, nodeName, "ovs-vsctl", "get", "interface", portName, column)
}

func addBridgeToOVS(ctx context.Context, nodeName, bridgeName string) {
	GinkgoHelper()
	_, err := ovsPodExec(ctx, nodeName,
		"ovs-vsctl", "--may-exist", "add-br", bridgeName,
		"--", "set", "bridge", bridgeName, "datapath_type=netdev")
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "add bridge %s", bridgeName)
}

func deleteBridgeFromOVS(ctx context.Context, nodeName, bridgeName string) {
	_, _ = ovsPodExec(ctx, nodeName,
		"ovs-vsctl", "--if-exists", "del-br", bridgeName)
}

// --- Host filesystem helpers (via driver pod) ---

func dirExists(ctx context.Context, nodeName, path string) bool {
	_, err := driverPodExec(ctx, nodeName, "test", "-d", path)
	return err == nil
}

func statOwnership(ctx context.Context, nodeName, path string) (uid, gid string) {
	GinkgoHelper()
	out, err := driverPodExec(ctx, nodeName, "stat", "-c", "%u %g", path)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "stat %s on %s", path, nodeName)
	parts := strings.Fields(out)
	ExpectWithOffset(1, parts).To(HaveLen(2), "unexpected stat output: %q", out)
	return parts[0], parts[1]
}

func hasACLEntry(ctx context.Context, nodeName, path, entry string) bool {
	out, err := driverPodExec(ctx, nodeName, "getfacl", path)
	if err != nil {
		return false
	}
	return strings.Contains(out, entry)
}

// --- Pod helpers ---

func waitForPodRunning(ctx context.Context, namespace, name string) *corev1.Pod {
	GinkgoHelper()
	var pod *corev1.Pod
	EventuallyWithOffset(1, func(g Gomega) {
		p, err := cs.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		// Fail fast on terminal states.
		g.Expect(p.Status.Phase).NotTo(Equal(corev1.PodFailed), "pod %s failed", name)
		if p.Status.Phase != corev1.PodRunning {
			// Log why the pod is not running yet for diagnostics.
			events, _ := cs.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
				FieldSelector: "involvedObject.name=" + name + ",involvedObject.kind=Pod",
			})
			if events != nil {
				for _, ev := range events.Items {
					fmt.Fprintf(GinkgoWriter, "[wait] pod %s: %s: %s\n", name, ev.Reason, ev.Message)
				}
			}
		}
		g.Expect(p.Status.Phase).To(Equal(corev1.PodRunning), "pod %s phase", name)
		pod = p
	}).WithTimeout(90 * time.Second).WithPolling(5 * time.Second).Should(Succeed())
	return pod
}

func deletePodAndWait(ctx context.Context, namespace, name string) {
	GinkgoHelper()
	_ = cs.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	EventuallyWithOffset(1, func(g Gomega) {
		p, err := cs.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return // pod is gone — success
		}
		if p.DeletionTimestamp != nil {
			// Check for DRA unprepare failures in pod events.
			events, _ := cs.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
				FieldSelector: "involvedObject.name=" + name + ",involvedObject.kind=Pod",
			})
			if events != nil {
				for _, ev := range events.Items {
					if strings.Contains(ev.Reason, "FailedUnprepareDynamicResources") ||
						strings.Contains(ev.Reason, "FailedPrepareDynamicResources") {
						fmt.Fprintf(GinkgoWriter, "[ERROR] pod %s: %s: %s\n", name, ev.Reason, ev.Message)
					}
				}
			}
			fmt.Fprintf(GinkgoWriter, "[wait] pod %s still terminating (phase=%s)\n", name, p.Status.Phase)
		}
		g.Expect(err).To(HaveOccurred(), "pod %s still exists (phase=%s)", name, p.Status.Phase)
	}).WithTimeout(120 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
}

// --- Topology / Device Plugin helpers ---

func addDPDKPort(ctx context.Context, nodeName, bridge, portName, pciAddr string) {
	GinkgoHelper()
	_, err := ovsPodExec(ctx, nodeName,
		"ovs-vsctl", "add-port", bridge, portName,
		"--", "set", "interface", portName,
		"type=dpdk", "options:dpdk-devargs="+pciAddr)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "add DPDK port %s", portName)
}

func removeDPDKPort(ctx context.Context, nodeName, portName string) {
	_, _ = ovsPodExec(ctx, nodeName,
		"ovs-vsctl", "--if-exists", "del-port", portName)
}

func nodeAllocatableQuantity(ctx context.Context, nodeName, resourceName string) int64 {
	GinkgoHelper()
	node, err := cs.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	qty, ok := node.Status.Allocatable[corev1.ResourceName(resourceName)]
	if !ok {
		return 0
	}
	return qty.Value()
}

func waitForNodeResource(ctx context.Context, nodeName, resourceName string) {
	GinkgoHelper()
	resName := corev1.ResourceName(resourceName)
	EventuallyWithOffset(1, func(g Gomega) {
		node, err := cs.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		qty, ok := node.Status.Allocatable[resName]
		g.Expect(ok).To(BeTrue(), "resource %s not in allocatable", resourceName)
		g.Expect(qty.Cmp(resource.MustParse("0"))).To(BeNumerically(">", 0))
	}).WithTimeout(90 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
}

func waitForNodeResourceGone(ctx context.Context, nodeName, resourceName string) {
	GinkgoHelper()
	resName := corev1.ResourceName(resourceName)
	EventuallyWithOffset(1, func(g Gomega) {
		node, err := cs.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		_, ok := node.Status.Allocatable[resName]
		g.Expect(ok).To(BeFalse(), "resource %s still in allocatable", resourceName)
	}).WithTimeout(90 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
}

// readDeviceMetadataFile reads and parses the DRA device metadata file
// (KEP-5304) for a directly referenced ResourceClaim from inside the named
// container. The file is mounted automatically by the kubelet at:
//
//	/var/run/kubernetes.io/dra-device-attributes/resourceclaims/
//	  <claimName>/<requestName>/<driverName>-metadata.json
//
// It returns the decoded internal DeviceMetadata object.
func readDeviceMetadataFile(ctx context.Context, namespace, podName, container, claimName, requestName string) (*metadata.DeviceMetadata, error) {
	metadataPath := strings.Join([]string{
		"/var/run/kubernetes.io/dra-device-attributes",
		"resourceclaims",
		claimName,
		requestName,
		driverName + "-metadata.json",
	}, "/")
	raw, err := kubectlExec(ctx, namespace, podName, container, "cat", metadataPath)
	if err != nil {
		return nil, err
	}
	var md metadata.DeviceMetadata
	if err := devicemetadata.DecodeMetadataFromStream(json.NewDecoder(strings.NewReader(raw)), &md); err != nil {
		return nil, fmt.Errorf("decode metadata file %s: %w", metadataPath, err)
	}
	return &md, nil
}

// waitForDeviceInSlice polls until the named device appears in the
// ResourceSlices for the given node.
func waitForDeviceInSlice(ctx context.Context, nodeName, deviceName string) {
	GinkgoHelper()
	EventuallyWithOffset(1, func(g Gomega) {
		nodeSlices, err := resourceSlicesForNode(ctx, nodeName)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(deviceNamesFromSlices(nodeSlices)).To(ContainElement(deviceName))
	}).WithTimeout(60 * time.Second).WithPolling(5 * time.Second).Should(Succeed())
}

// restartDriverOnNode deletes the driver pod on the given node and waits for
// the DaemonSet to recreate it with a new Running pod.
func restartDriverOnNode(ctx context.Context, nodeName string) {
	GinkgoHelper()

	oldPodName, err := driverPodOnNode(ctx, nodeName)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "find driver pod on %s", nodeName)

	err = cs.CoreV1().Pods(driverNamespace).Delete(ctx, oldPodName, metav1.DeleteOptions{})
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "delete driver pod %s", oldPodName)

	EventuallyWithOffset(1, func(g Gomega) {
		pods, err := cs.CoreV1().Pods(driverNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: "app=dra-driver-ovsdpdk",
			FieldSelector: "spec.nodeName=" + nodeName,
		})
		g.Expect(err).NotTo(HaveOccurred())
		var found bool
		for _, p := range pods.Items {
			if p.Name != oldPodName && p.Status.Phase == corev1.PodRunning {
				found = true
			}
		}
		g.Expect(found).To(BeTrue(), "new driver pod not yet Running on %s", nodeName)
	}).WithTimeout(120 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
}
