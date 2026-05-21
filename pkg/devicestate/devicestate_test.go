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
	"fmt"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	ovsdpdkdrav1alpha1 "github.com/amorenoz/dra-driver-ovsdpdk/pkg/api/ovsdpdkdra/v1alpha1"
	"github.com/amorenoz/dra-driver-ovsdpdk/pkg/cdi"
	"github.com/amorenoz/dra-driver-ovsdpdk/pkg/consts"
	"github.com/amorenoz/dra-driver-ovsdpdk/pkg/devicestate"
	socketfsmocks "github.com/amorenoz/dra-driver-ovsdpdk/pkg/socketfs/mocks"
)

func TestDeviceState(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "DeviceState Suite")
}

var _ = Describe("DeviceState", func() {
	var ds *devicestate.DeviceState

	BeforeEach(func() {
		ds = devicestate.New(nil, socketfsmocks.NewMockSocketFS(GinkgoT()))
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
			Expect(ds.UpdatePolicyDevices(ctx, nil)).To(Succeed())
		})

		It("should call the callback after a successful UpdatePolicyDevices", func(ctx SpecContext) {
			called := false
			ds.SetRepublishCallback(func(_ context.Context) error {
				called = true
				return nil
			})
			Expect(ds.UpdatePolicyDevices(ctx, nil)).To(Succeed())
			Expect(called).To(BeTrue())
		})

		It("should propagate callback errors back to the caller", func(ctx SpecContext) {
			callbackErr := errors.New("publish failed")
			ds.SetRepublishCallback(func(_ context.Context) error {
				return callbackErr
			})
			Expect(ds.UpdatePolicyDevices(ctx, nil)).To(MatchError(ContainSubstring("publish failed")))
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
			Expect(ds.UpdatePolicyDevices(ctx, bridges)).NotTo(Succeed())
			Expect(called).To(BeFalse())
		})
	})

	Describe("UpdatePolicyDevices", func() {
		It("should succeed with an empty bridge list", func(ctx SpecContext) {
			Expect(ds.UpdatePolicyDevices(ctx, nil)).To(Succeed())
		})

		It("should succeed with unique bridge names", func(ctx SpecContext) {
			bridges := []ovsdpdkdrav1alpha1.BridgeSpec{
				{Name: "br0"},
				{Name: "br1"},
				{Name: "br2"},
			}
			Expect(ds.UpdatePolicyDevices(ctx, bridges)).To(Succeed())
		})

		It("should return an error when two bridges share the same name", func(ctx SpecContext) {
			bridges := []ovsdpdkdrav1alpha1.BridgeSpec{
				{Name: "br0"},
				{Name: "br1"},
				{Name: "br0"},
			}
			Expect(ds.UpdatePolicyDevices(ctx, bridges)).To(
				MatchError(ContainSubstring("duplicate bridge name")),
			)
		})

		It("should return an error when all bridges share the same name", func(ctx SpecContext) {
			bridges := []ovsdpdkdrav1alpha1.BridgeSpec{
				{Name: "br-phy0"},
				{Name: "br-phy0"},
			}
			Expect(ds.UpdatePolicyDevices(ctx, bridges)).To(
				MatchError(ContainSubstring(`"br-phy0"`)),
			)
		})

		It("should produce one device per bridge with the correct name", func(ctx SpecContext) {
			bridges := []ovsdpdkdrav1alpha1.BridgeSpec{
				{Name: "br0"},
				{Name: "br1"},
			}
			Expect(ds.UpdatePolicyDevices(ctx, bridges)).To(Succeed())
			devices := ds.GetAllocatableDevices()
			Expect(devices).To(HaveLen(2))
			Expect(devices).To(HaveKey("br0"))
			Expect(devices).To(HaveKey("br1"))
			Expect(devices["br0"].Name).To(Equal("br0"))
			Expect(devices["br1"].Name).To(Equal("br1"))
		})

		It("should set consumable capacity to DefaultBridgeCapacity and allow multiple allocations", func(ctx SpecContext) {
			bridges := []ovsdpdkdrav1alpha1.BridgeSpec{{Name: "br0"}}
			Expect(ds.UpdatePolicyDevices(ctx, bridges)).To(Succeed())
			device := ds.GetAllocatableDevices()["br0"]
			Expect(device.AllowMultipleAllocations).To(Equal(ptr.To(true)))
			cap, ok := device.Capacity["ovsdpdk.k8snetworkplumbingwg.io/ports"]
			Expect(ok).To(BeTrue())
			Expect(cap.Value.Value()).To(Equal(int64(consts.DefaultBridgeCapacity)))
		})

		It("should replace allocatable devices on successive calls", func(ctx SpecContext) {
			Expect(ds.UpdatePolicyDevices(ctx, []ovsdpdkdrav1alpha1.BridgeSpec{
				{Name: "br0"}, {Name: "br1"},
			})).To(Succeed())
			Expect(ds.GetAllocatableDevices()).To(HaveLen(2))

			Expect(ds.UpdatePolicyDevices(ctx, []ovsdpdkdrav1alpha1.BridgeSpec{
				{Name: "br2"},
			})).To(Succeed())
			devices := ds.GetAllocatableDevices()
			Expect(devices).To(HaveLen(1))
			Expect(devices).To(HaveKey("br2"))
		})

		It("should leave allocatable devices unchanged when validation fails", func(ctx SpecContext) {
			Expect(ds.UpdatePolicyDevices(ctx, []ovsdpdkdrav1alpha1.BridgeSpec{
				{Name: "br0"},
			})).To(Succeed())

			Expect(ds.UpdatePolicyDevices(ctx, []ovsdpdkdrav1alpha1.BridgeSpec{
				{Name: "br1"}, {Name: "br1"},
			})).NotTo(Succeed())
			devices := ds.GetAllocatableDevices()
			Expect(devices).To(HaveLen(1))
			Expect(devices).To(HaveKey("br0"))
		})
	})

	Describe("GetVhostUserConfig", func() {
		It("should return the configured spec after UpdateConfig", func(ctx SpecContext) {
			spec := &ovsdpdkdrav1alpha1.VhostUserSpec{
				ContainerRootPath: "/custom/container",
			}
			Expect(ds.UpdateConfig(ctx, &ovsdpdkdrav1alpha1.OvsDpdkConfigSpec{VhostUser: spec})).To(Succeed())
			cfg := ds.GetVhostUserConfig()
			Expect(cfg.ContainerRootPath).To(Equal("/custom/container"))
		})
	})
})

var _ = Describe("DeviceState prepare/unprepare", func() {
	Describe("PrepareResourceClaim", func() {
		It("should return an error when the claim has no allocation", func(ctx SpecContext) {
			ds, _, _ := newDeviceStateWithMockFS(ctx, nil)
			claim := &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "default", UID: "uid-1"},
				Status:     resourceapi.ResourceClaimStatus{},
			}
			_, err := ds.PrepareResourceClaim(ctx, claim)
			Expect(err).To(MatchError(ContainSubstring("no allocation")))
		})

		It("should return an error when the claim has no ReservedFor entry", func(ctx SpecContext) {
			ds, _, _ := newDeviceStateWithMockFS(ctx, nil)
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
			ds, _, _ := newDeviceStateWithMockFS(ctx, nil)
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
			ds, mockFS, _ := newDeviceStateWithMockFS(ctx, nil)
			mockFS.EXPECT().CreateSocketDir(mock.Anything).Return(nil).Maybe()
			mockFS.EXPECT().RemoveSocketDir(mock.Anything).Return(nil).Maybe()

			claim := makeClaim("uid-4", "pod-uid-4", "claim-4", "vhost0", "br0")
			claim.Status.Allocation.Devices.Results = nil
			_, err := ds.PrepareResourceClaim(ctx, claim)
			Expect(err).To(MatchError(ContainSubstring("expected exactly 1 allocation result")))
		})

		It("should fall back to claim.Name when the pod-claim-name annotation is absent", func(ctx SpecContext) {
			ds, mockFS, _ := newDeviceStateWithMockFS(ctx, &ovsdpdkdrav1alpha1.VhostUserSpec{
				ContainerRootPath: "/container",
			})
			podUID := k8stypes.UID("pod-uid-5")

			expectedHostDir := filepath.Join(consts.HostRootPath, string(podUID)+"_"+"my-hand-written-claim")
			mockFS.EXPECT().CreateSocketDir(expectedHostDir).Return(nil).Once()

			claim := makeClaim("uid-5", podUID, "my-hand-written-claim", "vhost0", "br0")
			delete(claim.Annotations, resourceapi.PodResourceClaimAnnotation)
			pd, err := ds.PrepareResourceClaim(ctx, claim)
			Expect(err).NotTo(HaveOccurred())
			Expect(pd.Mount.HostDir).To(Equal(expectedHostDir))
			Expect(pd.Mount.ContainerDir).To(Equal("/container/my-hand-written-claim"))
		})

		It("should use the pod-local claim name for host and container paths", func(ctx SpecContext) {
			ds, mockFS, _ := newDeviceStateWithMockFS(ctx, &ovsdpdkdrav1alpha1.VhostUserSpec{
				ContainerRootPath: "/container",
			})
			podUID := k8stypes.UID("pod-uid-ok")
			podClaimName := "vhost1"

			expectedHostDir := filepath.Join(consts.HostRootPath, string(podUID)+"_"+podClaimName)
			mockFS.EXPECT().CreateSocketDir(expectedHostDir).Return(nil).Once()

			claim := makeClaim("abcdef12-0000-0000-0000-000000000000", podUID, "my-pod-vhost1-xz123", podClaimName, "br0")
			pd, err := ds.PrepareResourceClaim(ctx, claim)
			Expect(err).NotTo(HaveOccurred())
			Expect(pd.Mount.HostDir).To(Equal(expectedHostDir))
			Expect(pd.Mount.ContainerDir).To(Equal("/container/" + podClaimName))
		})

		It("should set Socket.HostPath to vhost.sock inside Mount.HostDir", func(ctx SpecContext) {
			ds, mockFS, _ := newDeviceStateWithMockFS(ctx, nil)
			mockFS.EXPECT().CreateSocketDir(mock.Anything).Return(nil).Once()

			claim := makeClaim("abcdef12-0000-0000-0000-000000000001", "pod-uid-sp", "claim-sp", "vhost-sp", "br0")
			pd, err := ds.PrepareResourceClaim(ctx, claim)
			Expect(err).NotTo(HaveOccurred())
			Expect(pd.Socket.HostPath).To(Equal(filepath.Join(pd.Mount.HostDir, "vhost.sock")))
			Expect(pd.Socket.ContainerPath).To(Equal(filepath.Join(pd.Mount.ContainerDir, "vhost.sock")))
		})

		It("should set Mount.ContainerDir from ContainerRootPath and pod-local claim name", func(ctx SpecContext) {
			ds, mockFS, _ := newDeviceStateWithMockFS(ctx, &ovsdpdkdrav1alpha1.VhostUserSpec{
				ContainerRootPath: "/container/root",
			})
			mockFS.EXPECT().CreateSocketDir(mock.Anything).Return(nil).Once()

			claim := makeClaim("abcdef12-0000-0000-0000-000000000002", "pod-uid-cm", "claim-cm-xz456", "vhost2", "br0")
			pd, err := ds.PrepareResourceClaim(ctx, claim)
			Expect(err).NotTo(HaveOccurred())
			Expect(pd.Mount.ContainerDir).To(Equal("/container/root/vhost2"))
		})

		It("should populate BridgeName from the allocation result device", func(ctx SpecContext) {
			ds, mockFS, _ := newDeviceStateWithMockFS(ctx, nil)
			mockFS.EXPECT().CreateSocketDir(mock.Anything).Return(nil).Once()

			claim := makeClaim("abcdef12-0000-0000-0000-000000000003", "pod-uid-bn", "claim-bn", "vhost-bn", "br-dpdk0")
			pd, err := ds.PrepareResourceClaim(ctx, claim)
			Expect(err).NotTo(HaveOccurred())
			Expect(pd.BridgeName).To(Equal("br-dpdk0"))
		})

		It("should populate Device with the correct CDI device ID", func(ctx SpecContext) {
			ds, mockFS, _ := newDeviceStateWithMockFS(ctx, nil)
			claimUID := k8stypes.UID("abcdef12-0000-0000-0000-000000000004")
			mockFS.EXPECT().CreateSocketDir(mock.Anything).Return(nil).Once()

			claim := makeClaim(claimUID, "pod-uid-dev", "claim-dev", "vhost-dev", "br0")
			pd, err := ds.PrepareResourceClaim(ctx, claim)
			Expect(err).NotTo(HaveOccurred())
			Expect(pd.Device.CDIDeviceIDs).To(HaveLen(1))
			Expect(pd.Device.CDIDeviceIDs[0]).To(Equal(cdi.DeviceID(claimUID, "br0")))
		})

		It("should write a CDI spec file on success", func(ctx SpecContext) {
			ds, mockFS, _ := newDeviceStateWithMockFS(ctx, nil)
			claimUID := k8stypes.UID("abcdef12-0000-0000-0000-000000000005")
			mockFS.EXPECT().CreateSocketDir(mock.Anything).Return(nil).Once()

			claim := makeClaim(claimUID, "pod-uid-cdi", "claim-cdi", "vhost-cdi", "br0")
			pd, err := ds.PrepareResourceClaim(ctx, claim)
			Expect(err).NotTo(HaveOccurred())
			Expect(pd.Device.CDIDeviceIDs).To(HaveLen(1))
			Expect(pd.Device.CDIDeviceIDs[0]).To(ContainSubstring("abcdef12"))
		})

		It("should clean up the socket directory when CDI spec creation fails", func(ctx SpecContext) {
			// Use a read-only CDI root to force CreateClaimSpecFile to fail.
			ds, mockFS, cdiRoot := newDeviceStateWithMockFS(ctx, nil)
			Expect(os.Chmod(cdiRoot, 0o555)).To(Succeed())

			mockFS.EXPECT().CreateSocketDir(mock.Anything).Return(nil).Once()
			mockFS.EXPECT().RemoveSocketDir(mock.Anything).Return(nil).Once()

			claim := makeClaim("abcdef12-0000-0000-0000-000000000006", "pod-uid-cleanup", "claim-cleanup-xz789", "vhost-cleanup", "br0")
			_, err := ds.PrepareResourceClaim(ctx, claim)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("UnprepareResourceClaim", func() {
		It("should remove the socket directory and CDI spec on success", func(ctx SpecContext) {
			ds, mockFS, _ := newDeviceStateWithMockFS(ctx, nil)
			mockFS.EXPECT().CreateSocketDir(mock.Anything).Return(nil).Once()
			mockFS.EXPECT().RemoveSocketDir(mock.Anything).Return(nil).Once()

			claim := makeClaim("abcdef12-0000-0000-0000-000000000010", "pod-uid-up", "claim-up", "vhost-up", "br0")
			pd, err := ds.PrepareResourceClaim(ctx, claim)
			Expect(err).NotTo(HaveOccurred())

			Expect(ds.UnprepareResourceClaim(ctx, pd)).To(Succeed())
		})

		It("should return an error when the socket directory removal fails", func(ctx SpecContext) {
			ds, mockFS, _ := newDeviceStateWithMockFS(ctx, nil)
			mockFS.EXPECT().CreateSocketDir(mock.Anything).Return(nil).Once()
			mockFS.EXPECT().RemoveSocketDir(mock.Anything).Return(fmt.Errorf("remove socket directory: permission denied")).Once()

			claim := makeClaim("abcdef12-0000-0000-0000-000000000011", "pod-uid-fail", "claim-fail", "vhost-fail", "br0")
			pd, err := ds.PrepareResourceClaim(ctx, claim)
			Expect(err).NotTo(HaveOccurred())

			err = ds.UnprepareResourceClaim(ctx, pd)
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(ContainSubstring("remove socket directory")))
		})
	})
})

// newDeviceStateWithMockFS creates a DeviceState with a real CDI temp directory
// and a mock SocketFS. The CDI temp dir is cleaned up via DeferCleanup.
func newDeviceStateWithMockFS(ctx SpecContext, vhostUser *ovsdpdkdrav1alpha1.VhostUserSpec) (*devicestate.DeviceState, *socketfsmocks.MockSocketFS, string) {
	GinkgoHelper()

	if vhostUser == nil {
		vhostUser = &ovsdpdkdrav1alpha1.VhostUserSpec{}
	}

	cdiRoot, err := os.MkdirTemp("", "cdi-root-*")
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(os.RemoveAll, cdiRoot)

	mockFS := socketfsmocks.NewMockSocketFS(GinkgoT())
	cdi, err := cdi.New(cdiRoot)
	Expect(err).NotTo(HaveOccurred())
	ds := devicestate.New(cdi, mockFS)
	Expect(ds.UpdateConfig(ctx, &ovsdpdkdrav1alpha1.OvsDpdkConfigSpec{VhostUser: vhostUser})).NotTo(HaveOccurred())
	return ds, mockFS, cdiRoot
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
				resourceapi.PodResourceClaimAnnotation: podClaimName,
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
