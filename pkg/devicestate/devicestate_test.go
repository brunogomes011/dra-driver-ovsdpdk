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

package devicestate_test

import (
	"context"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	ovsdpdkdrav1alpha1 "github.com/amorenoz/dra-driver-ovsdpdk/pkg/api/ovsdpdkdra/v1alpha1"
	"github.com/amorenoz/dra-driver-ovsdpdk/pkg/cdi"
	"github.com/amorenoz/dra-driver-ovsdpdk/pkg/consts"
	"github.com/amorenoz/dra-driver-ovsdpdk/pkg/devicestate"
)

func TestDeviceState(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "DeviceState Suite")
}

var _ = Describe("DeviceState", func() {
	var ds *devicestate.DeviceState

	BeforeEach(func() {
		ds = devicestate.New(nil)
	})

	Describe("GetAllocatableDevices", func() {
		It("should return an empty non-nil map when no devices are set", func() {
			devices := ds.GetAllocatableDevices()
			Expect(devices).NotTo(BeNil())
			Expect(devices).To(BeEmpty())
		})

		It("should return a copy that does not affect internal state when modified", func() {
			devices := ds.GetAllocatableDevices()
			devices["injected"] = resourceapi.Device{}
			Expect(ds.GetAllocatableDevices()).To(BeEmpty())
		})
	})

	Describe("SetRepublishCallback", func() {
		It("should not call the callback during UpdatePolicyDevices if not set", func(ctx SpecContext) {
			Expect(ds.UpdatePolicyDevices(ctx, nil, nil)).To(Succeed())
		})

		It("should call the callback after a successful UpdatePolicyDevices", func(ctx SpecContext) {
			called := false
			ds.SetRepublishCallback(func(_ context.Context) error {
				called = true
				return nil
			})
			Expect(ds.UpdatePolicyDevices(ctx, nil, nil)).To(Succeed())
			Expect(called).To(BeTrue())
		})

		It("should propagate callback errors back to the caller", func(ctx SpecContext) {
			callbackErr := errors.New("publish failed")
			ds.SetRepublishCallback(func(_ context.Context) error {
				return callbackErr
			})
			Expect(ds.UpdatePolicyDevices(ctx, nil, nil)).To(MatchError(ContainSubstring("publish failed")))
		})

		It("should not call the callback when bridge validation fails", func(ctx SpecContext) {
			called := false
			ds.SetRepublishCallback(func(_ context.Context) error {
				called = true
				return nil
			})
			bridges := []ovsdpdkdrav1alpha1.BridgeSpec{
				{Name: "br0"},
				{Name: "br0"},
			}
			Expect(ds.UpdatePolicyDevices(ctx, bridges, nil)).NotTo(Succeed())
			Expect(called).To(BeFalse())
		})
	})

	Describe("UpdatePolicyDevices", func() {
		It("should succeed with an empty bridge list", func(ctx SpecContext) {
			Expect(ds.UpdatePolicyDevices(ctx, nil, nil)).To(Succeed())
		})

		It("should succeed with unique bridge names", func(ctx SpecContext) {
			bridges := []ovsdpdkdrav1alpha1.BridgeSpec{
				{Name: "br0"},
				{Name: "br1"},
				{Name: "br2"},
			}
			Expect(ds.UpdatePolicyDevices(ctx, bridges, nil)).To(Succeed())
		})

		It("should return an error when two bridges share the same name", func(ctx SpecContext) {
			bridges := []ovsdpdkdrav1alpha1.BridgeSpec{
				{Name: "br0"},
				{Name: "br1"},
				{Name: "br0"},
			}
			Expect(ds.UpdatePolicyDevices(ctx, bridges, nil)).To(
				MatchError(ContainSubstring("duplicate bridge name")),
			)
		})

		It("should return an error when all bridges share the same name", func(ctx SpecContext) {
			bridges := []ovsdpdkdrav1alpha1.BridgeSpec{
				{Name: "br-phy0"},
				{Name: "br-phy0"},
			}
			Expect(ds.UpdatePolicyDevices(ctx, bridges, nil)).To(
				MatchError(ContainSubstring(`"br-phy0"`)),
			)
		})

		It("should produce one device per bridge with the correct name", func(ctx SpecContext) {
			bridges := []ovsdpdkdrav1alpha1.BridgeSpec{
				{Name: "br0"},
				{Name: "br1"},
			}
			Expect(ds.UpdatePolicyDevices(ctx, bridges, nil)).To(Succeed())
			devices := ds.GetAllocatableDevices()
			Expect(devices).To(HaveLen(2))
			Expect(devices).To(HaveKey("br0"))
			Expect(devices).To(HaveKey("br1"))
			Expect(devices["br0"].Name).To(Equal("br0"))
			Expect(devices["br1"].Name).To(Equal("br1"))
		})

		It("should set consumable capacity to DefaultBridgeCapacity and allow multiple allocations", func(ctx SpecContext) {
			bridges := []ovsdpdkdrav1alpha1.BridgeSpec{{Name: "br0"}}
			Expect(ds.UpdatePolicyDevices(ctx, bridges, nil)).To(Succeed())
			device := ds.GetAllocatableDevices()["br0"]
			Expect(device.AllowMultipleAllocations).To(Equal(ptr.To(true)))
			cap, ok := device.Capacity["ovsdpdk.k8snetworkplumbingwg.io/ports"]
			Expect(ok).To(BeTrue())
			Expect(cap.Value.Value()).To(Equal(int64(ovsdpdkdrav1alpha1.DefaultBridgeCapacity)))
		})

		It("should replace allocatable devices on successive calls", func(ctx SpecContext) {
			Expect(ds.UpdatePolicyDevices(ctx, []ovsdpdkdrav1alpha1.BridgeSpec{
				{Name: "br0"}, {Name: "br1"},
			}, nil)).To(Succeed())
			Expect(ds.GetAllocatableDevices()).To(HaveLen(2))

			Expect(ds.UpdatePolicyDevices(ctx, []ovsdpdkdrav1alpha1.BridgeSpec{
				{Name: "br2"},
			}, nil)).To(Succeed())
			devices := ds.GetAllocatableDevices()
			Expect(devices).To(HaveLen(1))
			Expect(devices).To(HaveKey("br2"))
		})

		It("should leave allocatable devices unchanged when validation fails", func(ctx SpecContext) {
			Expect(ds.UpdatePolicyDevices(ctx, []ovsdpdkdrav1alpha1.BridgeSpec{
				{Name: "br0"},
			}, nil)).To(Succeed())

			Expect(ds.UpdatePolicyDevices(ctx, []ovsdpdkdrav1alpha1.BridgeSpec{
				{Name: "br1"}, {Name: "br1"},
			}, nil)).NotTo(Succeed())
			devices := ds.GetAllocatableDevices()
			Expect(devices).To(HaveLen(1))
			Expect(devices).To(HaveKey("br0"))
		})
	})

	Describe("GetVhostUserConfig", func() {
		It("should return defaults when no VhostUserSpec has been set", func() {
			cfg := ds.GetVhostUserConfig()
			Expect(cfg.HostRootPath).To(Equal(ovsdpdkdrav1alpha1.DefaultHostRootPath))
			Expect(cfg.ContainerRootPath).To(Equal(ovsdpdkdrav1alpha1.DefaultContainerRootPath))
		})

		It("should return the configured spec after UpdatePolicyDevices", func(ctx SpecContext) {
			spec := &ovsdpdkdrav1alpha1.VhostUserSpec{
				HostRootPath:      "/custom/host",
				ContainerRootPath: "/custom/container",
			}
			Expect(ds.UpdatePolicyDevices(ctx, nil, spec)).To(Succeed())
			cfg := ds.GetVhostUserConfig()
			Expect(cfg.HostRootPath).To(Equal("/custom/host"))
			Expect(cfg.ContainerRootPath).To(Equal("/custom/container"))
		})
	})
})

// newDeviceStateWithDirs creates a DeviceState backed by real temp directories
// for both the CDI root and the vhost-user socket root. It registers a Ginkgo
// DeferCleanup to remove both directories after the spec.
func newDeviceStateWithDirs() (ds *devicestate.DeviceState, hostRoot string) {
	ds, _, hostRoot = newDeviceStateWithAllDirs()
	return ds, hostRoot
}

// newDeviceStateWithAllDirs is like newDeviceStateWithDirs but also returns the
// CDI root directory, needed by tests that inspect metadata files.
func newDeviceStateWithAllDirs() (ds *devicestate.DeviceState, cdiRoot, hostRoot string) {
	GinkgoHelper()

	var err error
	cdiRoot, err = os.MkdirTemp("", "cdi-root-*")
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(os.RemoveAll, cdiRoot)

	hostRoot, err = os.MkdirTemp("", "host-root-*")
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(os.RemoveAll, hostRoot)

	cdiHandler := cdi.New(cdiRoot)
	ds = devicestate.New(cdiHandler)
	return ds, cdiRoot, hostRoot
}

// makeClaim builds a minimal ResourceClaim that satisfies PrepareResourceClaim.
// claimName is the auto-generated ResourceClaim name; podClaimName is the
// pod-local claim name stored in the standard annotation.
func makeClaim(claimUID, podUID k8stypes.UID, claimName, podClaimName, bridgeName string) *resourceapi.ResourceClaim {
	return &resourceapi.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      claimName,
			Namespace: "default",
			UID:       claimUID,
			Annotations: map[string]string{
				consts.PodClaimNameAnnotation: podClaimName,
			},
		},
		Status: resourceapi.ResourceClaimStatus{
			Allocation: &resourceapi.AllocationResult{
				Devices: resourceapi.DeviceAllocationResult{
					Results: []resourceapi.DeviceRequestAllocationResult{
						{
							Request: "req-0",
							Pool:    "pool-0",
							Device:  bridgeName,
						},
					},
				},
			},
			ReservedFor: []resourceapi.ResourceClaimConsumerReference{
				{Resource: "pods", Name: "test-pod", UID: podUID},
			},
		},
	}
}

var _ = Describe("DeviceState prepare/unprepare", func() {
	Describe("PrepareResourceClaim", func() {
		It("should return an error when the claim has no allocation", func(ctx SpecContext) {
			ds, _ := newDeviceStateWithDirs()
			claim := &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "default", UID: "uid-1"},
				Status:     resourceapi.ResourceClaimStatus{},
			}
			_, err := ds.PrepareResourceClaim(ctx, claim)
			Expect(err).To(MatchError(ContainSubstring("no allocation")))
		})

		It("should return an error when the claim has no ReservedFor entry", func(ctx SpecContext) {
			ds, _ := newDeviceStateWithDirs()
			claim := &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "default", UID: "uid-2"},
				Status: resourceapi.ResourceClaimStatus{
					Allocation: &resourceapi.AllocationResult{},
				},
			}
			_, err := ds.PrepareResourceClaim(ctx, claim)
			Expect(err).To(MatchError(ContainSubstring("no ReservedFor")))
		})

		It("should return an error when the claim has multiple ReservedFor entries", func(ctx SpecContext) {
			ds, _ := newDeviceStateWithDirs()
			claim := &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "default", UID: "uid-3"},
				Status: resourceapi.ResourceClaimStatus{
					Allocation: &resourceapi.AllocationResult{},
					ReservedFor: []resourceapi.ResourceClaimConsumerReference{
						{Resource: "pods", Name: "pod-a", UID: "pod-uid-a"},
						{Resource: "pods", Name: "pod-b", UID: "pod-uid-b"},
					},
				},
			}
			_, err := ds.PrepareResourceClaim(ctx, claim)
			Expect(err).To(MatchError(ContainSubstring("multiple pods")))
		})

		It("should return an error when the allocation has no results", func(ctx SpecContext) {
			ds, hostRoot := newDeviceStateWithDirs()
			Expect(ds.UpdatePolicyDevices(ctx, nil, &ovsdpdkdrav1alpha1.VhostUserSpec{
				HostRootPath:      hostRoot,
				ContainerRootPath: "/container",
			})).To(Succeed())

			claim := makeClaim("uid-4", "pod-uid-4", "claim-4", "vhost0", "br0")
			claim.Status.Allocation.Devices.Results = nil
			_, err := ds.PrepareResourceClaim(ctx, claim)
			Expect(err).To(MatchError(ContainSubstring("expected exactly 1 allocation result")))
		})

		It("should fall back to claim.Name when the pod-claim-name annotation is absent", func(ctx SpecContext) {
			ds, hostRoot := newDeviceStateWithDirs()
			podUID := k8stypes.UID("pod-uid-5")
			Expect(ds.UpdatePolicyDevices(ctx, nil, &ovsdpdkdrav1alpha1.VhostUserSpec{
				HostRootPath:      hostRoot,
				ContainerRootPath: "/container",
			})).To(Succeed())

			claim := makeClaim("uid-5", podUID, "my-hand-written-claim", "vhost0", "br0")
			delete(claim.Annotations, consts.PodClaimNameAnnotation)
			pd, err := ds.PrepareResourceClaim(ctx, claim)
			Expect(err).NotTo(HaveOccurred())
			Expect(pd.Mount.HostDir).To(Equal(filepath.Join(hostRoot, string(podUID)+"_"+"my-hand-written-claim")))
			Expect(pd.Mount.ContainerDir).To(Equal("/container/my-hand-written-claim"))
		})

		It("should use the pod-local claim name for host and container paths", func(ctx SpecContext) {
			ds, hostRoot := newDeviceStateWithDirs()
			podUID := k8stypes.UID("pod-uid-ok")
			podClaimName := "vhost1"
			Expect(ds.UpdatePolicyDevices(ctx, nil, &ovsdpdkdrav1alpha1.VhostUserSpec{
				HostRootPath:      hostRoot,
				ContainerRootPath: "/container",
			})).To(Succeed())

			claim := makeClaim("abcdef12-0000-0000-0000-000000000000", podUID, "my-pod-vhost1-xz123", podClaimName, "br0")
			pd, err := ds.PrepareResourceClaim(ctx, claim)
			Expect(err).NotTo(HaveOccurred())

			expectedHostDir := filepath.Join(hostRoot, string(podUID)+"_"+podClaimName)
			Expect(pd.Mount.HostDir).To(Equal(expectedHostDir))
			Expect(pd.Mount.ContainerDir).To(Equal("/container/" + podClaimName))
			_, statErr := os.Stat(expectedHostDir)
			Expect(statErr).NotTo(HaveOccurred())
		})

		It("should set Socket.HostPath to vhost.sock inside Mount.HostDir", func(ctx SpecContext) {
			ds, hostRoot := newDeviceStateWithDirs()
			Expect(ds.UpdatePolicyDevices(ctx, nil, &ovsdpdkdrav1alpha1.VhostUserSpec{
				HostRootPath:      hostRoot,
				ContainerRootPath: "/container",
			})).To(Succeed())

			claim := makeClaim("abcdef12-0000-0000-0000-000000000001", "pod-uid-sp", "claim-sp", "vhost-sp", "br0")
			pd, err := ds.PrepareResourceClaim(ctx, claim)
			Expect(err).NotTo(HaveOccurred())
			Expect(pd.Socket.HostPath).To(Equal(filepath.Join(pd.Mount.HostDir, "vhost.sock")))
			Expect(pd.Socket.ContainerPath).To(Equal(filepath.Join(pd.Mount.ContainerDir, "vhost.sock")))
		})

		It("should set Mount.ContainerDir from ContainerRootPath and pod-local claim name", func(ctx SpecContext) {
			ds, hostRoot := newDeviceStateWithDirs()
			Expect(ds.UpdatePolicyDevices(ctx, nil, &ovsdpdkdrav1alpha1.VhostUserSpec{
				HostRootPath:      hostRoot,
				ContainerRootPath: "/container/root",
			})).To(Succeed())

			claim := makeClaim("abcdef12-0000-0000-0000-000000000002", "pod-uid-cm", "claim-cm-xz456", "vhost2", "br0")
			pd, err := ds.PrepareResourceClaim(ctx, claim)
			Expect(err).NotTo(HaveOccurred())
			Expect(pd.Mount.ContainerDir).To(Equal("/container/root/vhost2"))
		})

		It("should populate BridgeName from the allocation result device", func(ctx SpecContext) {
			ds, hostRoot := newDeviceStateWithDirs()
			Expect(ds.UpdatePolicyDevices(ctx, nil, &ovsdpdkdrav1alpha1.VhostUserSpec{
				HostRootPath:      hostRoot,
				ContainerRootPath: "/container",
			})).To(Succeed())

			claim := makeClaim("abcdef12-0000-0000-0000-000000000003", "pod-uid-bn", "claim-bn", "vhost-bn", "br-dpdk0")
			pd, err := ds.PrepareResourceClaim(ctx, claim)
			Expect(err).NotTo(HaveOccurred())
			Expect(pd.BridgeName).To(Equal("br-dpdk0"))
		})

		It("should populate Device with the correct CDI device ID", func(ctx SpecContext) {
			ds, hostRoot := newDeviceStateWithDirs()
			claimUID := k8stypes.UID("abcdef12-0000-0000-0000-000000000004")
			Expect(ds.UpdatePolicyDevices(ctx, nil, &ovsdpdkdrav1alpha1.VhostUserSpec{
				HostRootPath:      hostRoot,
				ContainerRootPath: "/container",
			})).To(Succeed())

			claim := makeClaim(claimUID, "pod-uid-dev", "claim-dev", "vhost-dev", "br0")
			pd, err := ds.PrepareResourceClaim(ctx, claim)
			Expect(err).NotTo(HaveOccurred())
			Expect(pd.Device.CDIDeviceIDs).To(HaveLen(1))
			Expect(pd.Device.CDIDeviceIDs[0]).To(Equal(cdi.DeviceID(claimUID, "br0")))
		})

		It("should write a CDI spec file on success", func(ctx SpecContext) {
			ds, hostRoot := newDeviceStateWithDirs()
			claimUID := k8stypes.UID("abcdef12-0000-0000-0000-000000000005")
			Expect(ds.UpdatePolicyDevices(ctx, nil, &ovsdpdkdrav1alpha1.VhostUserSpec{
				HostRootPath:      hostRoot,
				ContainerRootPath: "/container",
			})).To(Succeed())

			claim := makeClaim(claimUID, "pod-uid-cdi", "claim-cdi", "vhost-cdi", "br0")
			pd, err := ds.PrepareResourceClaim(ctx, claim)
			Expect(err).NotTo(HaveOccurred())
			Expect(pd.Device.CDIDeviceIDs).To(HaveLen(1))
			Expect(pd.Device.CDIDeviceIDs[0]).To(ContainSubstring("abcdef12"))
		})

		It("should clean up the socket directory when CDI spec creation fails", func(ctx SpecContext) {
			// Use a nonexistent CDI root to force CreateClaimSpecFile to fail.
			badCDI := cdi.New("/nonexistent/cdi/root")
			ds := devicestate.New(badCDI)

			hostRoot, err := os.MkdirTemp("", "host-root-bad-*")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(os.RemoveAll, hostRoot)

			Expect(ds.UpdatePolicyDevices(ctx, nil, &ovsdpdkdrav1alpha1.VhostUserSpec{
				HostRootPath:      hostRoot,
				ContainerRootPath: "/container",
			})).To(Succeed())

			podUID := k8stypes.UID("pod-uid-cleanup")
			podClaimName := "vhost-cleanup"
			claim := makeClaim("abcdef12-0000-0000-0000-000000000006", podUID, "claim-cleanup-xz789", podClaimName, "br0")
			_, err = ds.PrepareResourceClaim(ctx, claim)
			Expect(err).To(HaveOccurred())

			// The socket directory must have been removed.
			socketDir := filepath.Join(hostRoot, string(podUID)+"_"+podClaimName)
			_, statErr := os.Stat(socketDir)
			Expect(os.IsNotExist(statErr)).To(BeTrue())
		})
	})

	Describe("UnprepareResourceClaim", func() {
		It("should remove the socket directory and CDI spec on success", func(ctx SpecContext) {
			ds, hostRoot := newDeviceStateWithDirs()
			Expect(ds.UpdatePolicyDevices(ctx, nil, &ovsdpdkdrav1alpha1.VhostUserSpec{
				HostRootPath:      hostRoot,
				ContainerRootPath: "/container",
			})).To(Succeed())

			claim := makeClaim("abcdef12-0000-0000-0000-000000000010", "pod-uid-up", "claim-up", "vhost-up", "br0")
			pd, err := ds.PrepareResourceClaim(ctx, claim)
			Expect(err).NotTo(HaveOccurred())
			Expect(pd.Mount.HostDir).To(BeADirectory())

			Expect(ds.UnprepareResourceClaim(ctx, pd)).To(Succeed())
			_, statErr := os.Stat(pd.Mount.HostDir)
			Expect(os.IsNotExist(statErr)).To(BeTrue())
		})

		It("should return an error when the socket directory removal fails", func(ctx SpecContext) {
			ds, hostRoot := newDeviceStateWithDirs()
			Expect(ds.UpdatePolicyDevices(ctx, nil, &ovsdpdkdrav1alpha1.VhostUserSpec{
				HostRootPath:      hostRoot,
				ContainerRootPath: "/container",
			})).To(Succeed())

			claim := makeClaim("abcdef12-0000-0000-0000-000000000011", "pod-uid-fail", "claim-fail", "vhost-fail", "br0")
			pd, err := ds.PrepareResourceClaim(ctx, claim)
			Expect(err).NotTo(HaveOccurred())

			// Make the hostRoot unwritable so os.RemoveAll cannot remove the
			// flat socket directory inside it.
			Expect(os.Chmod(hostRoot, 0o555)).To(Succeed())
			DeferCleanup(func() { _ = os.Chmod(hostRoot, 0o755) })

			err = ds.UnprepareResourceClaim(ctx, pd)
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(ContainSubstring("remove socket directory")))
		})
	})
})

var _ = Describe("DeviceState permissions", func() {
	Describe("PrepareResourceClaim with permissions", func() {
		It("should apply ownership to the socket directory when user and group are specified by name", func(ctx SpecContext) {
			ds, hostRoot := newDeviceStateWithDirs()

			u, err := user.Current()
			Expect(err).NotTo(HaveOccurred())
			g, err := user.LookupGroupId(u.Gid)
			Expect(err).NotTo(HaveOccurred())

			userID := ovsdpdkdrav1alpha1.NewUserGroupIDFromName(u.Username)
			groupID := ovsdpdkdrav1alpha1.NewUserGroupIDFromName(g.Name)
			Expect(ds.UpdatePolicyDevices(ctx, nil, &ovsdpdkdrav1alpha1.VhostUserSpec{
				HostRootPath:      hostRoot,
				ContainerRootPath: "/container",
				User:              &userID,
				Group:             &groupID,
			})).To(Succeed())

			claim := makeClaim("abcdef12-0000-0000-0000-000000000020", "pod-uid-perm", "claim-perm", "vhost-perm", "br0")
			pd, err := ds.PrepareResourceClaim(ctx, claim)
			Expect(err).NotTo(HaveOccurred())

			info, err := os.Stat(pd.Mount.HostDir)
			Expect(err).NotTo(HaveOccurred())
			stat, ok := info.Sys().(*syscall.Stat_t)
			Expect(ok).To(BeTrue())

			expectedUID, _ := strconv.Atoi(u.Uid)
			expectedGID, _ := strconv.Atoi(g.Gid)
			Expect(int(stat.Uid)).To(Equal(expectedUID))
			Expect(int(stat.Gid)).To(Equal(expectedGID))
		})

		It("should apply ownership when user and group are specified numerically", func(ctx SpecContext) {
			ds, hostRoot := newDeviceStateWithDirs()

			u, err := user.Current()
			Expect(err).NotTo(HaveOccurred())
			expectedUID, _ := strconv.Atoi(u.Uid)
			expectedGID, _ := strconv.Atoi(u.Gid)

			userID := ovsdpdkdrav1alpha1.NewUserGroupIDFromID(expectedUID)
			groupID := ovsdpdkdrav1alpha1.NewUserGroupIDFromID(expectedGID)
			Expect(ds.UpdatePolicyDevices(ctx, nil, &ovsdpdkdrav1alpha1.VhostUserSpec{
				HostRootPath:      hostRoot,
				ContainerRootPath: "/container",
				User:              &userID,
				Group:             &groupID,
			})).To(Succeed())

			claim := makeClaim("abcdef12-0000-0000-0000-000000000021", "pod-uid-num", "claim-num", "vhost-num", "br0")
			pd, err := ds.PrepareResourceClaim(ctx, claim)
			Expect(err).NotTo(HaveOccurred())

			info, err := os.Stat(pd.Mount.HostDir)
			Expect(err).NotTo(HaveOccurred())
			stat, ok := info.Sys().(*syscall.Stat_t)
			Expect(ok).To(BeTrue())
			Expect(int(stat.Uid)).To(Equal(expectedUID))
			Expect(int(stat.Gid)).To(Equal(expectedGID))
		})

		It("should create the socket directory with mode 0775 regardless of process umask", func(ctx SpecContext) {
			ds, hostRoot := newDeviceStateWithDirs()
			Expect(ds.UpdatePolicyDevices(ctx, nil, &ovsdpdkdrav1alpha1.VhostUserSpec{
				HostRootPath:      hostRoot,
				ContainerRootPath: "/container",
			})).To(Succeed())

			// Set a restrictive umask that would strip group-write if chmod were not called.
			old := syscall.Umask(0o022)
			DeferCleanup(func() { syscall.Umask(old) })

			claim := makeClaim("abcdef12-0000-0000-0000-000000000030", "pod-uid-umask", "claim-umask", "vhost-umask", "br0")
			pd, err := ds.PrepareResourceClaim(ctx, claim)
			Expect(err).NotTo(HaveOccurred())

			info, err := os.Stat(pd.Mount.HostDir)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o775)))
		})

		It("should fail and clean up the socket directory when the user name does not exist", func(ctx SpecContext) {
			ds, hostRoot := newDeviceStateWithDirs()

			userID := ovsdpdkdrav1alpha1.NewUserGroupIDFromName("no-such-user-xyz")
			Expect(ds.UpdatePolicyDevices(ctx, nil, &ovsdpdkdrav1alpha1.VhostUserSpec{
				HostRootPath:      hostRoot,
				ContainerRootPath: "/container",
				User:              &userID,
			})).To(Succeed())

			podUID := k8stypes.UID("pod-uid-baduser")
			claim := makeClaim("abcdef12-0000-0000-0000-000000000022", podUID, "claim-baduser", "vhost-baduser", "br0")
			_, err := ds.PrepareResourceClaim(ctx, claim)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("resolve user"))

			// Socket directory must have been cleaned up.
			socketDir := filepath.Join(hostRoot, string(podUID), "vhost-baduser")
			_, statErr := os.Stat(socketDir)
			Expect(os.IsNotExist(statErr)).To(BeTrue())
		})

		It("should fail with an invalid SELinux label format", func(ctx SpecContext) {
			ds, hostRoot := newDeviceStateWithDirs()

			label := "not-a-valid-label"
			Expect(ds.UpdatePolicyDevices(ctx, nil, &ovsdpdkdrav1alpha1.VhostUserSpec{
				HostRootPath:      hostRoot,
				ContainerRootPath: "/container",
				SelinuxLabel:      &label,
			})).To(Succeed())

			claim := makeClaim("abcdef12-0000-0000-0000-000000000023", "pod-uid-sel", "claim-sel", "vhost-sel", "br0")
			_, err := ds.PrepareResourceClaim(ctx, claim)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid SELinux label"))
		})
	})
})

var _ = Describe("DeviceState metadata", func() {
	Describe("PrepareResourceClaim Device.Metadata", func() {
		It("should always populate Device.Metadata with vhost-user-path", func(ctx SpecContext) {
			ds, hostRoot := newDeviceStateWithDirs()
			Expect(ds.UpdatePolicyDevices(ctx, nil, &ovsdpdkdrav1alpha1.VhostUserSpec{
				HostRootPath:      hostRoot,
				ContainerRootPath: "/container",
			})).To(Succeed())

			claim := makeClaim("abcdef12-0000-0000-0000-000000000030", "pod-uid-meta", "claim-meta", "vhost-meta", "br-dpdk0")
			pd, err := ds.PrepareResourceClaim(ctx, claim)
			Expect(err).NotTo(HaveOccurred())

			meta := pd.Device.Metadata
			Expect(meta).NotTo(BeNil())

			socketAttr, ok := meta.Attributes["vhost-user-path"]
			Expect(ok).To(BeTrue())
			Expect(socketAttr.StringValue).NotTo(BeNil())
			Expect(*socketAttr.StringValue).To(Equal(pd.Socket.ContainerPath))
		})
	})
})
