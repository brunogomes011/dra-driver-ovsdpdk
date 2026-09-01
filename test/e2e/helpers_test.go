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
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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

// claimData renders claim.yaml.tmpl. Count, Vlan, MaxRate/Burst and
// ApplyToAll are all optional — a zero-value claimData renders a plain
// single-request claim with no config block.
//
// Vlan and MaxRate are pointers (rather than plain int/uint32) so that an
// explicit zero value can be distinguished from "not configured": Go
// templates treat a non-nil pointer as truthy even when it points at the
// zero value, which lets tests exercise "vlan 0" or "max_rate 0" (meaning
// unlimited) as distinct from omitting the field entirely. Burst has no
// such case in practice (0 always means "not set") so it stays a plain
// uint32.
type claimData struct {
	Name, Namespace, BridgeName string
	Count                       int     // 0 omits the count field (defaults to 1)
	Vlan                        *int    // nil omits vlan from the config block
	MaxRate                     *uint32 // nil omits max_rate from the policing block
	Burst                       uint32  // 0 omits burst from the policing block
	ApplyToAll                  bool    // omit the config entry's "requests" list

	// ConfigKind/ConfigAPIVersion override the opaque config's kind/apiVersion
	// (normally "OvsPortConfig"/"ovsdpdk.k8snetworkplumbingwg.io/v1alpha1").
	// Only useful for exercising the driver's rejection of an unexpected
	// kind/apiVersion — leave empty to get the real values.
	ConfigKind, ConfigAPIVersion string
}

// HasPolicing reports whether the rendered claim needs a policing block.
func (c claimData) HasPolicing() bool {
	return c.MaxRate != nil || c.Burst != 0
}

// HasConfig reports whether the rendered claim needs a config block at all.
func (c claimData) HasConfig() bool {
	return c.Vlan != nil || c.HasPolicing() || c.ConfigKind != "" || c.ConfigAPIVersion != ""
}

type unknownClassClaimData struct {
	Name, Namespace, DeviceClassName string
}

// podData renders pod.yaml.tmpl. NodeName and TopologyResource are optional.
type podData struct {
	Name, Namespace, ClaimName string
	NodeName                   string
	NodeSelector               map[string]string
	TopologyResource           string
}

// ovsDpdkConfigData renders ovsdpdk-config.yaml.tmpl. ContainerRootPath is
// optional — empty uses the real default (/var/run/ovsdpdk); only useful
// for exercising a custom mount path. AclUsers is a list since the CRD
// field itself is a list (aclUsers:), even though most tests only ever need
// one entry.
type ovsDpdkConfigData struct {
	Name, SelinuxLabel, User, Group, ContainerRootPath string
	AclUsers                                           []string
}

// defaultOvsDpdkConfigData returns the ovsDpdkConfigData for the "default"
// OvsDpdkConfig every test in this suite relies on (the driver only watches
// the object named "default"). Tests that temporarily mutate it must
// restore this exact value afterwards — see e2e_ovsdpdkconfig_test.go.
func defaultOvsDpdkConfigData() ovsDpdkConfigData {
	return ovsDpdkConfigData{
		Name:         "default",
		SelinuxLabel: plat.selinuxLabel,
		User:         plat.configUser,
		Group:        plat.configGroup,
		AclUsers:     []string{plat.configAclUser},
	}
}

type multiClaimPodData struct {
	Name, Namespace string
	ClaimNames      []string
}

type multiRequestClaimData struct {
	Name, Namespace, BridgeName string
	Ports                       []string
}

// policyData renders policy.yaml.tmpl. Mtu and TopologyResource are
// optional and apply to every bridge in Bridges (in practice only ever
// used with a single bridge).
type policyData struct {
	Name             string
	NodeNames        []string
	Bridges          []string
	Mtu              *int
	TopologyResource string
}

type claimTemplatePodData struct {
	Name, Namespace, PodClaimName, TemplateName string
}

type requestBridgePair struct {
	Name, BridgeName string
}

type multiBridgeClaimData struct {
	Name, Namespace string
	Requests        []requestBridgePair
}

type vlanOverrideClaimData struct {
	Name, Namespace, BridgeName string
	SpecificVlan, GlobalVlan    int
}

// vmInterfaceData describes one additional (non-default) VM network
// interface bound to a DRA ResourceClaimTemplate via the vhostuser binding.
type vmInterfaceData struct {
	Name         string // network/interface name
	ClaimRefName string // spec.resourceClaims[].name referenced by the network
	TemplateName string // ResourceClaimTemplate to bind
}

// vmData renders vm-dra.yaml.tmpl. The default masquerade interface is
// always guest ifname eth0, so Interfaces[i] lands on eth<i+1> — see
// guestInterfaceName. CPUCount is optional; 0 renders as a single vCPU.
type vmData struct {
	Name, Namespace, NodeName string
	Image                     string // containerDisk image; set from $E2E_VM_IMAGE (vmImage)
	CPUCount                  int
	HugepageSize              string // e.g. "2Mi"; empty backs the VM with regular (non-huge) memory
	Interfaces                []vmInterfaceData
}

// guestInterfaceName returns the guest-visible interface name for the i-th
// (0-indexed) entry in vmData.Interfaces.
// todo: Identify the interface name correctly
func guestInterfaceName(i int) string {
	return fmt.Sprintf("enp%ds0", i+2)
}

// intPtr returns a pointer to v, useful for optional *int template fields
// where the zero value is a meaningful, non-omitted setting (e.g. vlan 0).
func intPtr(v int) *int { return &v }

// uint32Ptr returns a pointer to v, useful for optional *uint32 template
// fields where the zero value is a meaningful, non-omitted setting (e.g.
// max_rate 0, meaning unlimited).
func uint32Ptr(v uint32) *uint32 { return &v }

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

// ovsNodeExec runs a command on an OpenShift node via "oc debug node/".
func ovsNodeExec(ctx context.Context, nodeName string, args ...string) (string, error) {
	ocArgs := []string{"debug", "node/" + nodeName, "--"}
	chrootArgs := append([]string{"chroot", "/host"}, args...)
	ocArgs = append(ocArgs, chrootArgs...)

	var out, errOut bytes.Buffer
	cmd := exec.CommandContext(ctx, "oc", ocArgs...)
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("oc debug node/%s %v: %w\n%s", nodeName, args, err, errOut.String())
	}
	return strings.TrimSpace(out.String()), nil
}

// --- OVS helpers ---

func ovsPortsForClaim(ctx context.Context, nodeName, claimUID string) ([]string, error) {
	out, err := ovsExec(ctx, nodeName,
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
	return ovsExec(ctx, nodeName, "ovs-vsctl", "get", "interface", portName, column)
}

// ovsPortGet returns the value of a single column on an OVS Port row.
func ovsPortGet(ctx context.Context, nodeName, portName, column string) (string, error) {
	return ovsExec(ctx, nodeName, "ovs-vsctl", "get", "port", portName, column)
}

// waitForOvsPorts polls until at least one OVS port exists for claimUID and
// returns the port names.
func waitForOvsPorts(ctx context.Context, nodeName, claimUID string) []string {
	GinkgoHelper()
	var ports []string
	EventuallyWithOffset(1, func(g Gomega) {
		p, err := ovsPortsForClaim(ctx, nodeName, claimUID)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(p).NotTo(BeEmpty())
		ports = p
	}).WithTimeout(30 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
	return ports
}

// socketDirPath returns the host path of the per-request vhost-user socket
// directory the driver creates for a claim.
func socketDirPath(podUID, claimName, requestName string) string {
	return filepath.Join(hostSocketRoot, podUID+"_"+claimName+"_"+requestName)
}

// waitForOvsPortsGone polls until no OVS port remains for claimUID.
func waitForOvsPortsGone(ctx context.Context, nodeName, claimUID string) {
	GinkgoHelper()
	EventuallyWithOffset(1, func(g Gomega) {
		p, err := ovsPortsForClaim(ctx, nodeName, claimUID)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(p).To(BeEmpty(), "OVS ports still present for claim %s: %v", claimUID, p)
	}).WithTimeout(60 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
}

func addBridgeToOVS(ctx context.Context, nodeName, bridgeName string) {
	GinkgoHelper()
	_, err := ovsExec(ctx, nodeName,
		"ovs-vsctl", "--may-exist", "add-br", bridgeName,
		"--", "set", "bridge", bridgeName, "datapath_type=netdev")
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "add bridge %s", bridgeName)
}

func deleteBridgeFromOVS(ctx context.Context, nodeName, bridgeName string) {
	_, _ = ovsExec(ctx, nodeName,
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

// --- High-level spec setup helpers ---
//
// These fold the setup sequence shared by most specs (apply a policy and wait
// for its bridge to be advertised; apply a claim+pod and wait for Running) into
// a few named steps, so a spec reads as what it tests rather than the
// scaffolding around it. Object names derive from a single base:
// "<base>-policy", "<base>-claim", "<base>-pod".

// applyPolicy renders and applies an OvsDpdkResourcePolicy ("<base>-policy")
// advertising bridges on node, then blocks until each bridge appears in that
// node's ResourceSlices. The policy is removed via DeferCleanup on scope exit.
func applyPolicy(ctx context.Context, base, node string, bridges ...string) {
	GinkgoHelper()
	applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
		policyData{Name: base + "-policy", NodeNames: []string{node}, Bridges: bridges}))
	for _, b := range bridges {
		waitForDeviceInSlice(ctx, node, b)
	}
}

// applyClaimAndPod renders and applies a claim ("<base>-claim") and a pod
// ("<base>-pod") bound to it, without waiting — for negative-path specs that
// assert the pod never becomes Running (see consistentlyNotRunning). The caller
// fills only the claim's distinguishing fields; Name/Namespace come from base.
// Both objects are removed via DeferCleanup on scope exit.
func applyClaimAndPod(base string, c claimData) {
	GinkgoHelper()
	c.Name = base + "-claim"
	c.Namespace = testNamespace
	applyAndCleanup(mustRenderManifest("claim.yaml.tmpl", c))
	applyAndCleanup(mustRenderManifest("pod.yaml.tmpl",
		podData{Name: base + "-pod", Namespace: testNamespace, ClaimName: c.Name}))
}

// applyClaimPod is applyClaimAndPod plus a wait for the pod to reach Running.
// It returns the running pod and the generated claim's UID — the two values a
// happy-path spec almost always needs next (e.g. to locate the OVS port).
func applyClaimPod(ctx context.Context, base string, c claimData) (*corev1.Pod, string) {
	GinkgoHelper()
	applyClaimAndPod(base, c)
	pod := waitForPodRunning(ctx, testNamespace, base+"-pod")
	return pod, claimUIDByName(ctx, base+"-claim")
}

// claimUIDByName returns the UID of the named ResourceClaim.
func claimUIDByName(ctx context.Context, name string) string {
	GinkgoHelper()
	c, err := cs.ResourceV1().ResourceClaims(testNamespace).Get(ctx, name, metav1.GetOptions{})
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "get claim %s", name)
	return string(c.UID)
}

// consistentlyNotRunning asserts the named pod does not reach Running within a
// short window — the standard check for a claim/config the driver should reject.
func consistentlyNotRunning(ctx context.Context, name string) {
	GinkgoHelper()
	ConsistentlyWithOffset(1, func(g Gomega) {
		p, err := cs.CoreV1().Pods(testNamespace).Get(ctx, name, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(p.Status.Phase).NotTo(Equal(corev1.PodRunning))
	}).WithTimeout(20 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
}

// --- Topology / Device Plugin helpers ---

func addDPDKPort(ctx context.Context, nodeName, bridge, portName, pciAddr string) {
	GinkgoHelper()
	_, err := ovsExec(ctx, nodeName,
		"ovs-vsctl", "add-port", bridge, portName,
		"--", "set", "interface", portName,
		"type=dpdk", "options:dpdk-devargs="+pciAddr)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "add DPDK port %s", portName)
}

func removeDPDKPort(ctx context.Context, nodeName, portName string) {
	_, _ = ovsExec(ctx, nodeName,
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

// waitForNodeResourceGone waits until the resource is no longer usable on the
// node. The kubelet device manager does not remove an extended resource key
// from Allocatable once it has been advertised — it only zeroes its quantity
// when the device plugin unregisters. The key itself sticks around until the
// kubelet is restarted (and even then may be restored from its on-disk
// checkpoint). So "gone" here means "allocatable quantity is zero", not
// "key absent".
func waitForNodeResourceGone(ctx context.Context, nodeName, resourceName string) {
	GinkgoHelper()
	resName := corev1.ResourceName(resourceName)
	EventuallyWithOffset(1, func(g Gomega) {
		node, err := cs.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		qty, ok := node.Status.Allocatable[resName]
		if !ok {
			return
		}
		g.Expect(qty.Cmp(resource.MustParse("0"))).To(BeNumerically("<=", 0), "resource %s still allocatable", resourceName)
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

// --- KubeVirt / VirtualMachine helpers (OpenShift Virtualization) ---
//
// VMs are driven over "virtctl ssh", which tunnels through the API server
// the same way "kubectl exec" does — no direct route to the pod network is
// required, keeping these helpers consistent with the exec-based style used
// for pods and nodes above.

// kubectlOutput runs kubectl and returns stdout without failing the test on
// error. Use in polling loops where the target resource may not exist yet.
func kubectlOutput(args ...string) (string, error) {
	allArgs := append([]string{"--kubeconfig", kubeconfig}, args...)
	out, err := exec.Command("kubectl", allArgs...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("kubectl %v: %w: %s", args, err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

// vmSSHKeyEnvVar names the environment variable holding the path to the
// private key used to authenticate to test VMs over virtctl ssh. Its public
// half is baked into the test VM image's authorized_keys ahead of time (see
// vm-dra.yaml.tmpl) — there's no cloud-init to inject it at boot, so the
// same pre-provisioned key is reused across every run. VM tests fail
// outright if it isn't set or the file doesn't exist, rather than
// generating one.
const vmSSHKeyEnvVar = "E2E_VM_SSH_KEY"

// loadSSHKeyPair returns the private key path from vmSSHKeyEnvVar.
func loadSSHKeyPair() (privateKeyPath string) {
	GinkgoHelper()
	privateKeyPath = os.Getenv(vmSSHKeyEnvVar)
	ExpectWithOffset(1, privateKeyPath).NotTo(BeEmpty(), "%s must be set to the path of an SSH private key", vmSSHKeyEnvVar)

	_, err := os.Stat(privateKeyPath)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "%s=%s", vmSSHKeyEnvVar, privateKeyPath)
	return privateKeyPath
}

// restartVMI deletes the VirtualMachineInstance so the VM controller (via
// runStrategy: Always) recreates it from the VM's current spec. This applies
// interface changes without deleting/recreating the VM object itself — the
// VM's containerDisk image already has everything it needs (SSH key,
// iperf3, ethtool) baked in, so a restart is just a fast reboot, not a
// re-provisioning step.
func restartVMI(namespace, name string) {
	GinkgoHelper()
	runKubectl("delete", "vmi", name, "-n", namespace, "--ignore-not-found", "--wait=true", "--timeout=60s")
}

// deleteVMAndWait deletes the named VirtualMachine and blocks until kubectl
// confirms it (and its VMI/virt-launcher pod) are fully gone. runKubectl
// already fails the test if the delete itself errors or times out, so a
// successful return means the deletion was clean.
func deleteVMAndWait(namespace, name string) {
	GinkgoHelper()
	runKubectl("delete", "vm", name, "-n", namespace, "--wait=true", "--timeout=120s")
}

// waitForVMIRunning polls until the named VirtualMachineInstance reaches the
// Running phase.
func waitForVMIRunning(namespace, name string) {
	GinkgoHelper()
	EventuallyWithOffset(1, func(g Gomega) {
		phase, err := kubectlOutput("get", "vmi", name, "-n", namespace, "-o", "jsonpath={.status.phase}")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(phase).To(Equal("Running"))
	}).WithTimeout(3 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())
}

// virtctlSSH runs command inside vmName over SSH via virtctl.
func virtctlSSH(ctx context.Context, namespace, vmName, identityFile, command string) (string, error) {
	args := []string{
		"--kubeconfig", kubeconfig,
		"ssh",
		"--identity-file=" + identityFile,
		"--local-ssh-opts", "-o StrictHostKeyChecking=no",
		"--local-ssh-opts", "-o UserKnownHostsFile=/dev/null",
		"--username=root",
		"-c", command,
		fmt.Sprintf("vm/%s/%s", vmName, namespace),
	}
	var out, errOut bytes.Buffer
	cmd := exec.CommandContext(ctx, "virtctl", args...)
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("virtctl ssh %s %q: %w\n%s%s", vmName, command, err, out.String(), errOut.String())
	}
	return strings.TrimSpace(out.String()), nil
}

// waitForSSHReady polls until vmName accepts SSH connections.
func waitForSSHReady(ctx context.Context, namespace, vmName, identityFile string) {
	GinkgoHelper()
	EventuallyWithOffset(1, func(g Gomega) {
		_, err := virtctlSSH(ctx, namespace, vmName, identityFile, "true")
		g.Expect(err).NotTo(HaveOccurred())
	}).WithTimeout(3 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())
}

// waitForVMsReady waits, in parallel, for vm1 and vm2 to reach Running and
// accept SSH connections. iperf3 and ethtool ship pre-installed in the VM's
// containerDisk image, so there's no package-install step to additionally
// wait for. Callers can rely on both VMs being fully usable once this
// returns — in particular, safe to reconfigure interface IPs on.
func waitForVMsReady(ctx context.Context, vm1, vm2 vmData, identityFile string) {
	GinkgoHelper()
	var wg sync.WaitGroup
	for _, vm := range []vmData{vm1, vm2} {
		wg.Add(1)
		go func(vm vmData) {
			defer wg.Done()
			defer GinkgoRecover()
			waitForVMIRunning(vm.Namespace, vm.Name)
			waitForSSHReady(ctx, vm.Namespace, vm.Name, identityFile)
		}(vm)
	}
	wg.Wait()
}

// createVMs applies the VM manifests for vm1/vm2 with their interfaces
// already in place and waits for both to boot and become reachable over
// SSH. Use this once, for the initial creation — the VMs are born with the
// right interface topology, no restart involved. Use updateVMInterfaces to
// change a running VM's interface topology afterwards.
func createVMs(ctx context.Context, vm1, vm2 vmData, identityFile string) {
	GinkgoHelper()
	applyYAML(mustRenderManifest("vm-dra.yaml.tmpl", vm1))
	applyYAML(mustRenderManifest("vm-dra.yaml.tmpl", vm2))
	waitForVMsReady(ctx, vm1, vm2, identityFile)
}

// updateVMInterfaces re-applies the VM manifests for vm1/vm2 and restarts
// their VMIs so the new interface topology takes effect, returning once
// both VMs are Running and reachable over SSH again.
func updateVMInterfaces(ctx context.Context, vm1, vm2 vmData, identityFile string) {
	GinkgoHelper()
	applyYAML(mustRenderManifest("vm-dra.yaml.tmpl", vm1))
	applyYAML(mustRenderManifest("vm-dra.yaml.tmpl", vm2))
	restartVMI(vm1.Namespace, vm1.Name)
	restartVMI(vm2.Namespace, vm2.Name)
	waitForVMsReady(ctx, vm1, vm2, identityFile)
}

// waitForGuestInterface polls until iface is visible inside vmName. The
// vhost-user interface is attached by the DRA binding plugin, so it can show
// up in the guest slightly after the VM itself is Running and reachable over
// the (unrelated) default SSH interface — configuring an IP too early fails
// with "Cannot find device".
func waitForGuestInterface(ctx context.Context, namespace, vmName, identityFile, iface string) {
	GinkgoHelper()
	EventuallyWithOffset(1, func(g Gomega) {
		out, err := virtctlSSH(ctx, namespace, vmName, identityFile, "ip link show "+iface)
		g.Expect(err).NotTo(HaveOccurred(), "%s not yet present on %s:\n%s", iface, vmName, out)
	}).WithTimeout(60 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
}

// minGuestMTU and maxGuestMTU bound the values configureInterfaceIP will
// actually apply — an mtu outside this range is left unconfigured (falling
// back to the interface's default MTU) rather than being applied as-is.
const (
	minGuestMTU = 1500
	maxGuestMTU = 9000
)

// configureInterfaceIP sets a static IP on iface inside vmName via
// NetworkManager, waiting for the interface to exist first. Any NetworkManager
// connection profile already bound to iface (e.g. a stale DHCP profile
// NetworkManager auto-created on a previous boot, or the profile from a
// previous test) is deleted first, so the new config is the only one left
// governing that device.
//
// mtu is optional — pass a value to also set an explicit MTU on the
// connection (a jumbo MTU on the OVS bridge doesn't by itself raise the
// guest's virtio-net interface MTU); omit it to leave the MTU untouched. A
// value outside [minGuestMTU, maxGuestMTU] is ignored rather than applied.
// Only the first value passed is used.
//
// Returns any error instead of failing the test itself — callers must gate
// dependent steps (e.g. pingFromVM) on the returned error, see
// mustConfigureInterfaceIP. The containerDisk is ephemeral, so this
// configuration doesn't survive a reboot — it must be re-run after every
// restartVMI.
func configureInterfaceIP(ctx context.Context, namespace, vmName, identityFile, iface, cidr string, mtu ...int) error {
	GinkgoHelper()
	waitForGuestInterface(ctx, namespace, vmName, identityFile, iface)

	setMTU := ""
	if len(mtu) > 0 && mtu[0] >= minGuestMTU && mtu[0] <= maxGuestMTU {
		setMTU = fmt.Sprintf("sudo nmcli connection modify %s ethernet.mtu %d && ", iface, mtu[0])
	}

	cmd := fmt.Sprintf(
		`sudo nmcli device set %s managed yes; `+
			`sudo sh -c 'nmcli -t -f NAME,DEVICE connection show | grep ":%s$" | cut -d: -f1 | xargs -r -I{} nmcli connection delete {}'; `+
			`sudo nmcli connection add type ethernet ifname %s con-name %s ip4 %s && `+setMTU+`sudo nmcli connection up %s`,
		iface, iface, iface, iface, cidr, iface)
	_, err := virtctlSSH(ctx, namespace, vmName, identityFile, cmd)
	if err != nil {
		return fmt.Errorf("configure %s on %s: %w", iface, vmName, err)
	}
	return nil
}

// mustConfigureInterfaceIP calls configureInterfaceIP and fails the test
// immediately if it errors, so that whatever runs after it (e.g.
// pingFromVM) only ever executes once configuration has actually succeeded.
func mustConfigureInterfaceIP(ctx context.Context, namespace, vmName, identityFile, iface, cidr string, mtu ...int) {
	GinkgoHelper()
	ExpectWithOffset(1, configureInterfaceIP(ctx, namespace, vmName, identityFile, iface, cidr, mtu...)).To(Succeed())
}

// --- DPDK-in-guest helpers (testpmd over vfio-pci) ---
//
// There's no stable, documented KubeVirt field for exposing a virtual IOMMU
// to a virtio-net/vhost-user interface, so these use the standard vfio
// no-IOMMU mode instead — the common, documented path for running DPDK
// against paravirtual (virtio) NICs that have no real IOMMU backing them.

// guestInterfacePCIAddress returns the PCI bus address (e.g. "0000:02:00.0")
// of iface, read while it's still bound to its kernel driver. Call this
// before bindInterfaceToVFIO detaches the device from the kernel.
func guestInterfacePCIAddress(ctx context.Context, namespace, vmName, identityFile, iface string) string {
	GinkgoHelper()
	out, err := virtctlSSH(ctx, namespace, vmName, identityFile, "ethtool -i "+iface+" | awk '/bus-info/{print $2}'")
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "ethtool -i %s on %s:\n%s", iface, vmName, out)
	ExpectWithOffset(1, out).NotTo(BeEmpty(), "no bus-info reported for %s on %s", iface, vmName)
	return out
}

// bindInterfaceToVFIO detaches iface from its kernel driver and binds it to
// vfio-pci (in no-IOMMU mode), returning its PCI address for use with
// startTestpmd. dpdk-devbind.py must already be installed in the VM image.
func bindInterfaceToVFIO(ctx context.Context, namespace, vmName, identityFile, iface string) (pciAddr string) {
	GinkgoHelper()
	waitForGuestInterface(ctx, namespace, vmName, identityFile, iface)
	pciAddr = guestInterfacePCIAddress(ctx, namespace, vmName, identityFile, iface)

	cmd := fmt.Sprintf(
		"sudo modprobe vfio enable_unsafe_noiommu_mode=1 2>/dev/null; "+
			"sudo sh -c 'echo Y > /sys/module/vfio/parameters/enable_unsafe_noiommu_mode' 2>/dev/null; "+
			"sudo modprobe vfio-pci && sudo dpdk-devbind.py --bind=vfio-pci %s", pciAddr)
	_, err := virtctlSSH(ctx, namespace, vmName, identityFile, cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "bind %s (%s) to vfio-pci on %s", iface, pciAddr, vmName)
	return pciAddr
}

// startTestpmd launches a backgrounded testpmd instance on vmName over the
// given (already vfio-pci bound, see bindInterfaceToVFIO) PCI devices,
// running in forwardMode, logging its periodic (1s) statistics to logPath.
// filePrefix and cores must be unique per concurrently-running instance on
// the same VM so EAL instances don't collide.
func startTestpmd(ctx context.Context, namespace, vmName, identityFile, filePrefix, cores, forwardMode string, pciAddrs []string, logPath string) {
	GinkgoHelper()
	var allowlist strings.Builder
	for _, pci := range pciAddrs {
		fmt.Fprintf(&allowlist, " -a %s", pci)
	}
	cmd := fmt.Sprintf(
		"setsid nohup testpmd -l %s --file-prefix=%s -m 256%s -- --forward-mode=%s --stats-period=1 "+
			">%s 2>&1 < /dev/null & sleep 1",
		cores, filePrefix, allowlist.String(), forwardMode, logPath)
	_, err := virtctlSSH(ctx, namespace, vmName, identityFile, cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "start testpmd (%s, %s) on %s", filePrefix, forwardMode, vmName)
}

// waitForTestpmdCounter polls logPath (a testpmd --stats-period=1 log) until
// the named NIC statistics counter (e.g. "TX-packets", "RX-packets") last
// reported a non-zero value, proving packets are actually flowing through
// that testpmd instance.
func waitForTestpmdCounter(ctx context.Context, namespace, vmName, identityFile, logPath, counter string) {
	GinkgoHelper()
	cmd := fmt.Sprintf("grep -o '%s: [0-9]*' %s 2>/dev/null | tail -1 | awk '{print $2}'", counter, logPath)
	EventuallyWithOffset(1, func(g Gomega) {
		out, err := virtctlSSH(ctx, namespace, vmName, identityFile, cmd)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(out).NotTo(BeEmpty(), "%s not yet reported in %s on %s", counter, logPath, vmName)
		n, convErr := strconv.Atoi(out)
		g.Expect(convErr).NotTo(HaveOccurred(), "unexpected %s value %q in %s", counter, out, logPath)
		g.Expect(n).To(BeNumerically(">", 0), "%s is still 0 in %s on %s", counter, logPath, vmName)
	}).WithTimeout(60 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
}

// pingFromVM pings targetIP from inside vmName, retrying briefly while the
// peer interface/ARP entry settles. Extra ping flags (e.g. "-M", "do", "-s",
// "8972" for a DF jumbo-frame ping) can be passed via extraArgs.
func pingFromVM(ctx context.Context, namespace, vmName, identityFile, targetIP string, extraArgs ...string) {
	GinkgoHelper()
	args := append([]string{"ping", "-c", "3", "-W", "2"}, extraArgs...)
	args = append(args, targetIP)
	EventuallyWithOffset(1, func(g Gomega) {
		out, err := virtctlSSH(ctx, namespace, vmName, identityFile, strings.Join(args, " "))
		g.Expect(err).NotTo(HaveOccurred(), "ping from %s to %s:\n%s", vmName, targetIP, out)
	}).WithTimeout(30 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
}

// iperf3Between starts a one-shot iperf3 server on serverVM and runs an
// iperf3 client from clientVM against serverIP, returning the achieved
// throughput in bits per second (parsed from the client's JSON summary) so
// callers can assert on it — e.g. to confirm ingress policing caps it.
func iperf3Between(ctx context.Context, namespace, serverVM, clientVM, identityFile, serverIP string) float64 {
	GinkgoHelper()
	_, err := virtctlSSH(ctx, namespace, serverVM, identityFile,
		"pkill iperf3 2>/dev/null; setsid nohup iperf3 -s -1 >/tmp/iperf3-server.log 2>&1 < /dev/null & sleep 1")
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "start iperf3 server on %s", serverVM)

	out, err := virtctlSSH(ctx, namespace, clientVM, identityFile, fmt.Sprintf("iperf3 -c %s -t 3 -J", serverIP))
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "iperf3 client on %s:\n%s", clientVM, out)

	var result struct {
		End struct {
			SumReceived struct {
				BitsPerSecond float64 `json:"bits_per_second"`
			} `json:"sum_received"`
		} `json:"end"`
	}
	ExpectWithOffset(1, json.Unmarshal([]byte(out), &result)).To(Succeed(), "parse iperf3 JSON output:\n%s", out)
	return result.End.SumReceived.BitsPerSecond
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

// applyOVSDaemonSet applies the OVS daemonset manifest with the image from OVS_IMAGE.
func applyOVSDaemonSet() {
	GinkgoHelper()
	raw, err := os.ReadFile(manifestPath("ovs-daemonset.yaml"))
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "read ovs-daemonset.yaml")
	manifest := strings.ReplaceAll(string(raw), "OVS_IMAGE_PLACEHOLDER", ovsImage)
	applyYAML(manifest)
}

// stopOVSDaemonSet deletes the OVS DaemonSet and waits for all OVS pods to be gone.
func stopOVSDaemonSet(ctx context.Context) {
	GinkgoHelper()

	err := cs.AppsV1().DaemonSets(driverNamespace).Delete(ctx, "ovs", metav1.DeleteOptions{})
	if err != nil {
		fmt.Fprintf(GinkgoWriter, "[stopOVS] Warning: failed to delete OVS daemonset: %v\n", err)
	}

	EventuallyWithOffset(1, func(g Gomega) {
		pods, err := cs.CoreV1().Pods(driverNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: ovsAppLabel,
		})
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(pods.Items).To(BeEmpty(), "OVS pods still exist")
	}).WithTimeout(120 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
}

// startOVSDaemonSet recreates the OVS DaemonSet by applying the manifest and waits for pods to be Running.
func startOVSDaemonSet(ctx context.Context) {
	GinkgoHelper()

	if ovsImage == "" {
		Fail("OVS_IMAGE environment variable must be set to start OVS daemonset")
	}

	applyOVSDaemonSet()

	EventuallyWithOffset(1, func(g Gomega) {
		pods, err := cs.CoreV1().Pods(driverNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: ovsAppLabel,
		})
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(pods.Items)).To(BeNumerically(">=", 2), "expected at least 2 OVS pods")

		for _, p := range pods.Items {
			g.Expect(p.Status.Phase).To(Equal(corev1.PodRunning), "OVS pod %s not Running", p.Name)
		}
	}).WithTimeout(120 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
}

// waitForResourceSlicesEmpty waits for all ResourceSlices on a node to have no devices.
func waitForResourceSlicesEmpty(ctx context.Context, nodeName string) {
	GinkgoHelper()
	EventuallyWithOffset(1, func(g Gomega) {
		slices, err := resourceSlicesForNode(ctx, nodeName)
		g.Expect(err).NotTo(HaveOccurred())

		totalDevices := 0
		for _, s := range slices {
			totalDevices += len(s.Spec.Devices)
		}
		g.Expect(totalDevices).To(Equal(0), "ResourceSlices for node %s still have %d devices", nodeName, totalDevices)
	}).WithTimeout(90 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
}

// restartDriverDaemonSet deletes all driver pods and waits for the DaemonSet to recreate them.
func restartDriverDaemonSet(ctx context.Context) {
	GinkgoHelper()

	pods, err := cs.CoreV1().Pods(driverNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: driverAppLabel,
	})
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	oldPodNames := make(map[string]bool)
	for _, p := range pods.Items {
		oldPodNames[p.Name] = true
		err = cs.CoreV1().Pods(driverNamespace).Delete(ctx, p.Name, metav1.DeleteOptions{})
		ExpectWithOffset(1, err).NotTo(HaveOccurred(), "delete driver pod %s", p.Name)
	}

	EventuallyWithOffset(1, func(g Gomega) {
		pods, err := cs.CoreV1().Pods(driverNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: driverAppLabel,
		})
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(pods.Items)).To(BeNumerically(">=", 2), "expected at least 2 driver pods")

		for _, p := range pods.Items {
			if oldPodNames[p.Name] {
				g.Expect(false).To(BeTrue(), "old driver pod %s still exists", p.Name)
			}
			g.Expect(p.Status.Phase).To(Equal(corev1.PodRunning), "new driver pod %s not Running", p.Name)
		}
	}).WithTimeout(120 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
}
