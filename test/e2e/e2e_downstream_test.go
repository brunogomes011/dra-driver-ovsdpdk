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
	"fmt"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VM vhost-user connectivity (OpenShift Virtualization)
//
// These specs walk two long-lived VMs through a sequence of interface
// topology changes — single vhost-user port, two ports on one RCT, two
// ports on separate RCTs, a VLAN-tagged port — verifying connectivity after
// each change. Restarting a VM's VMI to pick up an interface change (via
// updateVMInterfaces) is far cheaper than deleting/recreating the VM: the
// containerDisk image already has the SSH key, iperf3 and ethtool baked in,
// so a restart is just a fast reboot, not a re-provisioning step. So the two
// VMs are created once in BeforeAll and reused by every It below.
var _ = Describe("VM vhost-user interfaces", Ordered, Label(tier2_openshift), func() {
	const (
		vm1Name                = "e2e-dra-vm1"
		vm2Name                = "e2e-dra-vm2"
		rctName                = "e2e-dra-vm-rct"
		rctAltName             = "e2e-dra-vm-rct-alt"
		rctVlanName            = "e2e-dra-vm-rct-vlan"
		rctVlanAltName         = "e2e-dra-vm-rct-vlan-alt"
		rctMtuName             = "e2e-dra-vm-rct-mtu"
		rctPolicingName        = "e2e-dra-vm-rct-policing"
		rctPolicingNoBurstName = "e2e-dra-vm-rct-policing-no-burst"
		vlan                   = 100
		vlanAlt                = 200
		mtu                    = 9000
		multiqueueCPUs         = 4
		policingMaxRate        = 100000 // 100 Mbps in kbps
		policingBurst          = 10000  // 10 Mb in kb
		policingDefaultBurst   = 8000   // driver default burst (kb) when unset
		dpdkVCPUs              = 4
		dpdkHugepageSize       = "2Mi"

		net1Prefix = "192.168.101"
		net2Prefix = "192.168.102"
	)

	var (
		node             string
		identityFile     string
		vm1Base, vm2Base vmData
	)

	// ip and cidr number the two VMs consistently across every subnet:
	// vm1Name always takes host 1, vm2Name always takes host 2.
	ip := func(prefix string, host int) string { return fmt.Sprintf("%s.%d", prefix, host) }
	cidr := func(prefix string, host int) string { return ip(prefix, host) + "/24" }

	// setInterfaces returns copies of vm1Base/vm2Base with the given
	// interface topology, ready to pass to updateVMInterfaces.
	setInterfaces := func(interfaces []vmInterfaceData) (vmData, vmData) {
		vm1, vm2 := vm1Base, vm2Base
		vm1.Interfaces, vm2.Interfaces = interfaces, interfaces
		return vm1, vm2
	}

	configureBothInterfaces := func(ctx SpecContext) {
		mustConfigureInterfaceIP(ctx, testNamespace, vm1Name, identityFile, guestInterfaceName(1), cidr(net2Prefix, 1))
		mustConfigureInterfaceIP(ctx, testNamespace, vm2Name, identityFile, guestInterfaceName(1), cidr(net2Prefix, 2))
		mustConfigureInterfaceIP(ctx, testNamespace, vm1Name, identityFile, guestInterfaceName(0), cidr(net1Prefix, 1))
		mustConfigureInterfaceIP(ctx, testNamespace, vm2Name, identityFile, guestInterfaceName(0), cidr(net1Prefix, 2))

	}

	// resourceClaimFor returns the UID of the ResourceClaim generated for
	// vmName's claimRefName (e.g. "net1-claim") together with the UID of the
	// virt-launcher pod that owns it, resolved the same way
	// e2e_claims_test.go maps a pod-claim-name to its generated claim —
	// except here the owning pod is vmName's virt-launcher pod.
	resourceClaimFor := func(ctx SpecContext, vmName, claimRefName string) (claimUID, podUID string) {
		claims, err := cs.ResourceV1().ResourceClaims(testNamespace).List(ctx, metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		for _, c := range claims.Items {
			if c.Annotations["resource.kubernetes.io/pod-claim-name"] != claimRefName {
				continue
			}
			if ref := metav1.GetControllerOf(&c); ref != nil && strings.HasPrefix(ref.Name, "virt-launcher-"+vmName) {
				return string(c.UID), string(ref.UID)
			}
		}
		Fail(fmt.Sprintf("generated ResourceClaim for %s's %s not found", vmName, claimRefName))
		return "", ""
	}

	pingBothInterfaces := func(ctx SpecContext) {
		pingFromVM(ctx, testNamespace, vm1Name, identityFile, ip(net1Prefix, 2))
		pingFromVM(ctx, testNamespace, vm2Name, identityFile, ip(net1Prefix, 1))
		pingFromVM(ctx, testNamespace, vm1Name, identityFile, ip(net2Prefix, 2))
		pingFromVM(ctx, testNamespace, vm2Name, identityFile, ip(net2Prefix, 1))
	}

	BeforeAll(func(ctx SpecContext) {
		if !isOpenShift {
			Skip("VM tests require OpenShift Virtualization (E2E_PLATFORM=openshift)")
		}
		if _, err := exec.LookPath("virtctl"); err != nil {
			Skip("virtctl not found in PATH")
		}
		if vmImage == "" {
			Skip("VM tests require a containerDisk image (set E2E_VM_IMAGE)")
		}

		node = workers[0]
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-dra-vm-policy", NodeNames: []string{node}, Bridges: []string{plat.bridge0}}))
		waitForDeviceInSlice(ctx, node, plat.bridge0)

		// A second bridge with a non-default MTU, for ID6.
		applyAndCleanup(mustRenderManifest("policy.yaml.tmpl",
			policyData{Name: "e2e-dra-vm-mtu-policy", NodeNames: []string{node}, Bridges: []string{plat.bridge1}, Mtu: intPtr(mtu)}))
		waitForDeviceInSlice(ctx, node, plat.bridge1)

		identityFile = loadSSHKeyPair()

		// One plain RCT (reused twice for ID1/ID2), a second RCT with
		// identical arguments (ID3), a VLAN-tagged RCT (ID4), a second
		// VLAN-tagged RCT with a different tag (ID5), an RCT on the
		// jumbo-MTU bridge (ID6), and RCTs with ingress policing set with
		// and without a burst (ID10/ID11).
		applyAndCleanup(mustRenderManifest("claim-template.yaml.tmpl",
			claimData{Name: rctName, Namespace: testNamespace, BridgeName: plat.bridge0}))
		applyAndCleanup(mustRenderManifest("claim-template.yaml.tmpl",
			claimData{Name: rctAltName, Namespace: testNamespace, BridgeName: plat.bridge0}))
		applyAndCleanup(mustRenderManifest("claim-template.yaml.tmpl",
			claimData{Name: rctVlanName, Namespace: testNamespace, BridgeName: plat.bridge0, Vlan: intPtr(vlan)}))
		applyAndCleanup(mustRenderManifest("claim-template.yaml.tmpl",
			claimData{Name: rctVlanAltName, Namespace: testNamespace, BridgeName: plat.bridge0, Vlan: intPtr(vlanAlt)}))
		applyAndCleanup(mustRenderManifest("claim-template.yaml.tmpl",
			claimData{Name: rctMtuName, Namespace: testNamespace, BridgeName: plat.bridge1}))
		applyAndCleanup(mustRenderManifest("claim-template.yaml.tmpl",
			claimData{Name: rctPolicingName, Namespace: testNamespace, BridgeName: plat.bridge0, MaxRate: uint32Ptr(policingMaxRate), Burst: policingBurst}))
		applyAndCleanup(mustRenderManifest("claim-template.yaml.tmpl",
			claimData{Name: rctPolicingNoBurstName, Namespace: testNamespace, BridgeName: plat.bridge0, MaxRate: uint32Ptr(policingMaxRate)}))

		vm1Base = vmData{Name: vm1Name, Namespace: testNamespace, NodeName: node, Image: vmImage}
		vm2Base = vmData{Name: vm2Name, Namespace: testNamespace, NodeName: node, Image: vmImage}
		DeferCleanup(runKubectl, "delete", "vm", vm1Name, "-n", testNamespace, "--ignore-not-found")
		DeferCleanup(runKubectl, "delete", "vm", vm2Name, "-n", testNamespace, "--ignore-not-found")

		vm1, vm2 := setInterfaces([]vmInterfaceData{{Name: "net1", ClaimRefName: "net1-claim", TemplateName: rctName}})
		createVMs(ctx, vm1, vm2, identityFile)
	})

	// // ID1: VMs can be created with a vhost-user port and traffic flows.
	It("single vhost-user port: ping and iperf3 both work", func(ctx SpecContext) {
		mustConfigureInterfaceIP(ctx, testNamespace, vm1Name, identityFile, guestInterfaceName(0), cidr(net1Prefix, 1))
		mustConfigureInterfaceIP(ctx, testNamespace, vm2Name, identityFile, guestInterfaceName(0), cidr(net1Prefix, 2))

		pingFromVM(ctx, testNamespace, vm1Name, identityFile, ip(net1Prefix, 2))
		pingFromVM(ctx, testNamespace, vm2Name, identityFile, ip(net1Prefix, 1))
		iperf3Between(ctx, testNamespace, vm2Name, vm1Name, identityFile, ip(net1Prefix, 2))
	})

	// ID2: VMs can be created with multiple vhost-user ports on the same RCT.
	It("multiple vhost-user ports on the same RCT: both interfaces ping", func(ctx SpecContext) {
		vm1, vm2 := setInterfaces([]vmInterfaceData{
			{Name: "net1", ClaimRefName: "net1-claim", TemplateName: rctName},
			{Name: "net2", ClaimRefName: "net2-claim", TemplateName: rctName},
		})
		updateVMInterfaces(ctx, vm1, vm2, identityFile)

		configureBothInterfaces(ctx)
		pingBothInterfaces(ctx)
	})

	// ID3: VMs can be created with multiple vhost-user ports backed by
	// different RCTs (same arguments).
	It("multiple vhost-user ports on separate RCTs: both interfaces ping", func(ctx SpecContext) {
		vm1, vm2 := setInterfaces([]vmInterfaceData{
			{Name: "net1", ClaimRefName: "net1-claim", TemplateName: rctName},
			{Name: "net2", ClaimRefName: "net2-claim", TemplateName: rctAltName},
		})
		updateVMInterfaces(ctx, vm1, vm2, identityFile)

		configureBothInterfaces(ctx)
		pingBothInterfaces(ctx)
	})

	// ID4: VMs can be created with a VLAN-tagged vhost-user port and still
	// have connectivity.
	It("vhost-user port on a VLAN-tagged RCT: ping works", func(ctx SpecContext) {
		vm1, vm2 := setInterfaces([]vmInterfaceData{
			{Name: "net1", ClaimRefName: "net1-claim", TemplateName: rctVlanName},
		})
		updateVMInterfaces(ctx, vm1, vm2, identityFile)

		mustConfigureInterfaceIP(ctx, testNamespace, vm1Name, identityFile, guestInterfaceName(0), cidr(net1Prefix, 1))
		mustConfigureInterfaceIP(ctx, testNamespace, vm2Name, identityFile, guestInterfaceName(0), cidr(net1Prefix, 2))

		pingFromVM(ctx, testNamespace, vm1Name, identityFile, ip(net1Prefix, 2))
		pingFromVM(ctx, testNamespace, vm2Name, identityFile, ip(net1Prefix, 1))
	})

	// ID5: VMs on RCTs with different VLANs cannot reach each other, even on
	// the same IP subnet — they land in different OVS broadcast domains.
	It("vhost-user ports on different VLANs: ping does not work", func(ctx SpecContext) {
		vm1 := vm1Base
		vm1.Interfaces = []vmInterfaceData{{Name: "net1", ClaimRefName: "net1-claim", TemplateName: rctVlanName}}
		vm2 := vm2Base
		vm2.Interfaces = []vmInterfaceData{{Name: "net1", ClaimRefName: "net1-claim", TemplateName: rctVlanAltName}}
		updateVMInterfaces(ctx, vm1, vm2, identityFile)

		mustConfigureInterfaceIP(ctx, testNamespace, vm1Name, identityFile, guestInterfaceName(0), cidr(net1Prefix, 1))
		mustConfigureInterfaceIP(ctx, testNamespace, vm2Name, identityFile, guestInterfaceName(0), cidr(net1Prefix, 2))

		Consistently(func(g Gomega) {
			out, err := virtctlSSH(ctx, testNamespace, vm1Name, identityFile, fmt.Sprintf("ping -c 2 -W 1 %s", ip(net1Prefix, 2)))
			g.Expect(err).To(HaveOccurred(), "ping unexpectedly succeeded:\n%s", out)
		}).WithTimeout(10 * time.Second).WithPolling(2 * time.Second).Should(Succeed())
	})

	// ID6: VMs can be created on bridges with a non-default (jumbo) MTU, and
	// a DF-flagged ping sized to that MTU still gets through end to end.
	It("vhost-user port on a jumbo-MTU bridge: large DF ping works", func(ctx SpecContext) {
		vm1, vm2 := setInterfaces([]vmInterfaceData{{Name: "net1", ClaimRefName: "net1-claim", TemplateName: rctMtuName}})
		updateVMInterfaces(ctx, vm1, vm2, identityFile)

		mustConfigureInterfaceIP(ctx, testNamespace, vm1Name, identityFile, guestInterfaceName(0), cidr(net1Prefix, 1), mtu)
		mustConfigureInterfaceIP(ctx, testNamespace, vm2Name, identityFile, guestInterfaceName(0), cidr(net1Prefix, 2), mtu)

		// 8972 = 9000 MTU - 20 (IPv4) - 8 (ICMP) bytes of payload.
		pingFromVM(ctx, testNamespace, vm1Name, identityFile, ip(net1Prefix, 2), "-M", "do", "-s", "8972")
	})

	// ID7: vhost-user ports keep working for newly created VMs after the DRA
	// driver restarts — repeats ID1 against a freshly restarted driver.
	It("vhost-user ports after a DRA driver restart: ping and iperf3 both work", func(ctx SpecContext) {
		restartDriverOnNode(ctx, node)

		vm1, vm2 := setInterfaces([]vmInterfaceData{{Name: "net1", ClaimRefName: "net1-claim", TemplateName: rctName}})
		updateVMInterfaces(ctx, vm1, vm2, identityFile)

		mustConfigureInterfaceIP(ctx, testNamespace, vm1Name, identityFile, guestInterfaceName(0), cidr(net1Prefix, 1))
		mustConfigureInterfaceIP(ctx, testNamespace, vm2Name, identityFile, guestInterfaceName(0), cidr(net1Prefix, 2))

		pingFromVM(ctx, testNamespace, vm1Name, identityFile, ip(net1Prefix, 2))
		pingFromVM(ctx, testNamespace, vm2Name, identityFile, ip(net1Prefix, 1))
		iperf3Between(ctx, testNamespace, vm2Name, vm1Name, identityFile, ip(net1Prefix, 2))
	})

	// ID8: with networkInterfaceMultiqueue enabled and more than one vCPU,
	// the guest and the OVS port both end up with a matching queue count.
	It("vhost-user port with multiqueue: queue count matches vCPU count", func(ctx SpecContext) {
		vm1 := vm1Base
		vm1.CPUCount = multiqueueCPUs
		vm1.Interfaces = []vmInterfaceData{{Name: "net1", ClaimRefName: "net1-claim", TemplateName: rctName}}
		vm2 := vm2Base
		vm2.Interfaces = vm1.Interfaces
		updateVMInterfaces(ctx, vm1, vm2, identityFile)

		claimUID, _ := resourceClaimFor(ctx, vm1Name, "net1-claim")
		ports := waitForOvsPorts(ctx, node, claimUID)
		status, err := ovsInterfaceGet(ctx, node, ports[0], "status")
		Expect(err).NotTo(HaveOccurred())
		fmt.Fprintf(GinkgoWriter, "OVS interface %s status: %s\n", ports[0], status)

		out, err := virtctlSSH(ctx, testNamespace, vm1Name, identityFile,
			fmt.Sprintf("ethtool -l %s | grep Combined | tail -1 | awk '{print $2}'", guestInterfaceName(0)))
		Expect(err).NotTo(HaveOccurred(), "ethtool -l on %s:\n%s", vm1Name, out)
		Expect(strings.TrimSpace(out)).To(Equal(fmt.Sprintf("%d", multiqueueCPUs)),
			"guest queue count should match vCPU count")
	})

	// ID10: VMs with vhost-user ports can be created with ingress policing
	// (rate and burst) — the OVS port reflects both values, and iperf3
	// throughput between the VMs is capped accordingly.
	It("vhost-user port with ingress policing (rate and burst): OVS port and iperf3 throughput are capped", func(ctx SpecContext) {
		vm1, vm2 := setInterfaces([]vmInterfaceData{{Name: "net1", ClaimRefName: "net1-claim", TemplateName: rctPolicingName}})
		updateVMInterfaces(ctx, vm1, vm2, identityFile)

		mustConfigureInterfaceIP(ctx, testNamespace, vm1Name, identityFile, guestInterfaceName(0), cidr(net1Prefix, 1))
		mustConfigureInterfaceIP(ctx, testNamespace, vm2Name, identityFile, guestInterfaceName(0), cidr(net1Prefix, 2))

		claimUID, _ := resourceClaimFor(ctx, vm1Name, "net1-claim")
		ports := waitForOvsPorts(ctx, node, claimUID)

		rate, err := ovsInterfaceGet(ctx, node, ports[0], "ingress_policing_rate")
		Expect(err).NotTo(HaveOccurred())
		Expect(rate).To(Equal(fmt.Sprintf("%d", policingMaxRate)))

		burst, err := ovsInterfaceGet(ctx, node, ports[0], "ingress_policing_burst")
		Expect(err).NotTo(HaveOccurred())
		Expect(burst).To(Equal(fmt.Sprintf("%d", policingBurst)))

		// vm1 is the iperf3 client (sender) so the traffic being capped is
		// the one entering the bridge through the port checked above.
		bps := iperf3Between(ctx, testNamespace, vm2Name, vm1Name, identityFile, ip(net1Prefix, 2))
		Expect(bps).To(BeNumerically("<", float64(policingMaxRate)*1000*1.5),
			"iperf3 throughput %.0f bps should be capped near the %d kbps ingress policing rate", bps, policingMaxRate)
	})

	// ID11: same as ID10 but without a burst — the driver defaults the OVS
	// burst to policingDefaultBurst.
	// It wont work properly as burst is programmable on ovs-vswitchd side and it is not present in OVSDB
	It("vhost-user port with ingress policing (rate only): burst defaults and throughput is capped", Label("troubleshoot"), func(ctx SpecContext) {
		vm1, vm2 := setInterfaces([]vmInterfaceData{{Name: "net1", ClaimRefName: "net1-claim", TemplateName: rctPolicingNoBurstName}})
		updateVMInterfaces(ctx, vm1, vm2, identityFile)

		mustConfigureInterfaceIP(ctx, testNamespace, vm1Name, identityFile, guestInterfaceName(0), cidr(net1Prefix, 1))
		mustConfigureInterfaceIP(ctx, testNamespace, vm2Name, identityFile, guestInterfaceName(0), cidr(net1Prefix, 2))

		claimUID, _ := resourceClaimFor(ctx, vm1Name, "net1-claim")
		ports := waitForOvsPorts(ctx, node, claimUID)

		rate, err := ovsInterfaceGet(ctx, node, ports[0], "ingress_policing_rate")
		Expect(err).NotTo(HaveOccurred())
		Expect(rate).To(Equal(fmt.Sprintf("%d", policingMaxRate)))

		burst, err := ovsInterfaceGet(ctx, node, ports[0], "ingress_policing_burst")
		Expect(err).NotTo(HaveOccurred())
		Expect(burst).To(Equal(fmt.Sprintf("%d", policingDefaultBurst)), "burst should default when unset")

		bps := iperf3Between(ctx, testNamespace, vm2Name, vm1Name, identityFile, ip(net1Prefix, 2))
		Expect(bps).To(BeNumerically("<", float64(policingMaxRate)*1000*1.5),
			"iperf3 throughput %.0f bps should be capped near the %d kbps ingress policing rate", bps, policingMaxRate)
	})
	// DPDK-in-guest: VMs with vhost-user ports can run DPDK apps (testpmd)
	// against them. There's no stable KubeVirt field for a virtual IOMMU on
	// virtio-net/vhost-user interfaces, so the interfaces are bound to
	// vfio-pci in no-IOMMU mode instead — the standard, documented path for
	// DPDK against paravirtual NICs with no real IOMMU behind them. VM1 runs
	// one testpmd per port (p0 txonly, p1 rxonly); VM2 runs a single testpmd
	// forwarding between its two ports (io mode), so a packet generated by
	// VM1's p0 loops through VM2 and back to VM1's p1.
	It("vhost-user ports can carry DPDK traffic (testpmd) between VMs", Label("troubleshoot"), func(ctx SpecContext) {
		vm1, vm2 := setInterfaces([]vmInterfaceData{
			{Name: "net1", ClaimRefName: "net1-claim", TemplateName: rctName},
			{Name: "net2", ClaimRefName: "net2-claim", TemplateName: rctName},
		})
		vm1.CPUCount, vm2.CPUCount = dpdkVCPUs, dpdkVCPUs
		vm1.HugepageSize, vm2.HugepageSize = dpdkHugepageSize, dpdkHugepageSize
		updateVMInterfaces(ctx, vm1, vm2, identityFile)

		vm1P0 := bindInterfaceToVFIO(ctx, testNamespace, vm1Name, identityFile, guestInterfaceName(0))
		vm1P1 := bindInterfaceToVFIO(ctx, testNamespace, vm1Name, identityFile, guestInterfaceName(1))
		vm2P0 := bindInterfaceToVFIO(ctx, testNamespace, vm2Name, identityFile, guestInterfaceName(0))
		vm2P1 := bindInterfaceToVFIO(ctx, testNamespace, vm2Name, identityFile, guestInterfaceName(1))

		const (
			vm1TxLog = "/tmp/testpmd-vm1-tx.log"
			vm1RxLog = "/tmp/testpmd-vm1-rx.log"
			vm2IoLog = "/tmp/testpmd-vm2-io.log"
		)
		startTestpmd(ctx, testNamespace, vm1Name, identityFile, "tx", "0", "txonly", []string{vm1P0}, vm1TxLog)
		startTestpmd(ctx, testNamespace, vm1Name, identityFile, "rx", "1", "rxonly", []string{vm1P1}, vm1RxLog)
		startTestpmd(ctx, testNamespace, vm2Name, identityFile, "io", "0-1", "io", []string{vm2P0, vm2P1}, vm2IoLog)

		// VM1 p0 generates traffic, VM2 sees it on its p0 (rx) and forwards
		// it out its p1 (tx), which VM1 sees arrive on p1 (rx) — proving the
		// packet made the full VM1 -> VM2 -> VM1 loop.
		waitForTestpmdCounter(ctx, testNamespace, vm1Name, identityFile, vm1TxLog, "TX-packets")
		waitForTestpmdCounter(ctx, testNamespace, vm2Name, identityFile, vm2IoLog, "RX-packets")
		waitForTestpmdCounter(ctx, testNamespace, vm2Name, identityFile, vm2IoLog, "TX-packets")
		waitForTestpmdCounter(ctx, testNamespace, vm1Name, identityFile, vm1RxLog, "RX-packets")
	})

	// ID12/ID13 both delete the VMs, so each needs them freshly (re)created
	// and verified reachable first. BeforeAll only runs once per Ordered
	// container, so it can't do that for both — BeforeEach is what re-runs
	// before every It, including after ID12 deletes the VMs and ID13 needs
	// them back.
	Context("deleting VMs cleans up driver-side resources", func() {
		BeforeEach(func(ctx SpecContext) {

			vm1, vm2 := setInterfaces([]vmInterfaceData{{Name: "net1", ClaimRefName: "net1-claim", TemplateName: rctName}})
			createVMs(ctx, vm1, vm2, identityFile)
			mustConfigureInterfaceIP(ctx, testNamespace, vm1Name, identityFile, guestInterfaceName(0), cidr(net1Prefix, 1))
			mustConfigureInterfaceIP(ctx, testNamespace, vm2Name, identityFile, guestInterfaceName(0), cidr(net1Prefix, 2))

			pingFromVM(ctx, testNamespace, vm1Name, identityFile, ip(net1Prefix, 2))
			pingFromVM(ctx, testNamespace, vm2Name, identityFile, ip(net1Prefix, 1))
		})

		// ID12: deleting the VMs cleans up every driver-side resource — the
		// host socket directory and the OVS port.
		It("VMs with multiple vhost-user ports can be deleted and all resources are cleaned", func(ctx SpecContext) {
			iperf3Between(ctx, testNamespace, vm2Name, vm1Name, identityFile, ip(net1Prefix, 2))

			claimUID, podUID := resourceClaimFor(ctx, vm1Name, "net1-claim")
			socketDir := socketDirPath(podUID, "net1-claim", "vhost-port")
			Expect(dirExists(ctx, node, socketDir)).To(BeTrue(), "socket dir should exist before deletion")
			waitForOvsPorts(ctx, node, claimUID)

			deleteVMAndWait(testNamespace, vm1Name)
			deleteVMAndWait(testNamespace, vm2Name)

			Eventually(func() bool { return dirExists(ctx, node, socketDir) }).
				WithTimeout(60 * time.Second).WithPolling(3 * time.Second).Should(BeFalse())
			waitForOvsPortsGone(ctx, node, claimUID)
		})

		// ID13: a DRA driver restart doesn't disrupt already-prepared
		// vhost-user ports — traffic keeps flowing afterwards — and the VMs
		// still delete cleanly.
		It("VMs with vhost-user ports can be deleted after DRA restarts", func(ctx SpecContext) {
			iperf3Between(ctx, testNamespace, vm2Name, vm1Name, identityFile, ip(net1Prefix, 2))

			restartDriverOnNode(ctx, node)

			// Traffic should still flow after the driver restart, without
			// touching the VMs themselves.
			pingFromVM(ctx, testNamespace, vm1Name, identityFile, ip(net1Prefix, 2))
			pingFromVM(ctx, testNamespace, vm2Name, identityFile, ip(net1Prefix, 1))
			iperf3Between(ctx, testNamespace, vm2Name, vm1Name, identityFile, ip(net1Prefix, 2))

			claimUID, podUID := resourceClaimFor(ctx, vm1Name, "net1-claim")
			socketDir := socketDirPath(podUID, "net1-claim", "vhost-port")

			deleteVMAndWait(testNamespace, vm1Name)
			deleteVMAndWait(testNamespace, vm2Name)

			Eventually(func() bool { return dirExists(ctx, node, socketDir) }).
				WithTimeout(60 * time.Second).WithPolling(3 * time.Second).Should(BeFalse())
			waitForOvsPortsGone(ctx, node, claimUID)
		})
	})
})
