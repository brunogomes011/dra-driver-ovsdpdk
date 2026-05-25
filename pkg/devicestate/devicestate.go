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

// Package devicestate manages the lifecycle of vhost-user socket directories
// and their associated OVS ports on a given node.
package devicestate

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	ovsdpdkdrav1alpha1 "github.com/amorenoz/dra-driver-ovsdpdk/pkg/api/ovsdpdkdra/v1alpha1"
	dracdi "github.com/amorenoz/dra-driver-ovsdpdk/pkg/cdi"
	"github.com/amorenoz/dra-driver-ovsdpdk/pkg/consts"
	"github.com/amorenoz/dra-driver-ovsdpdk/pkg/permissions"
	dratypes "github.com/amorenoz/dra-driver-ovsdpdk/pkg/types"
)

// AllocatableDevices maps device names to their DRA device specifications.
type AllocatableDevices map[string]resourceapi.Device

// DeviceState manages the set of vhost-user devices advertised by this node
// and owns the prepare/unprepare lifecycle for resource claims.
type DeviceState struct {
	mutex             sync.RWMutex
	log               klog.Logger
	republishCallback func(ctx context.Context) error
	allocatable       AllocatableDevices
	vhostUserConfig   *ovsdpdkdrav1alpha1.VhostUserSpec
	cdi               *dracdi.Handler
	permApplier       *permissions.Applier
}

// deviceStatusData is the driver-specific debug payload written into
// ResourceClaim.Status.Devices[].Data after a successful prepare.
type deviceStatusData struct {
	Mount        dratypes.MountInfo  `json:"mount"`
	Socket       dratypes.SocketInfo `json:"socket"`
	BridgeName   string              `json:"bridgeName"`
	CDIDeviceIDs []string            `json:"cdiDeviceID"`
}

// New creates a new DeviceState with the given CDI handler.
func New(cdi *dracdi.Handler) *DeviceState {
	resolver := permissions.NewUserResolver()
	ds := &DeviceState{
		log:         klog.Background().WithName("DeviceState"),
		cdi:         cdi,
		permApplier: permissions.NewApplier(resolver),
	}
	ds.updateConfig(make([]ovsdpdkdrav1alpha1.BridgeSpec, 0), nil)
	return ds
}

// SetRepublishCallback sets a callback that is invoked after UpdatePolicyDevices
// successfully updates the set of allocatable devices.
func (d *DeviceState) SetRepublishCallback(callback func(ctx context.Context) error) {
	d.republishCallback = callback
}

// GetAllocatableDevices returns a copy of the current set of allocatable devices.
func (d *DeviceState) GetAllocatableDevices() AllocatableDevices {
	d.mutex.RLock()
	defer d.mutex.RUnlock()
	result := make(AllocatableDevices, len(d.allocatable))
	for k, v := range d.allocatable {
		result[k] = v
	}
	return result
}

// GetVhostUserConfig returns the effective vhost-user configuration. If no
// policy has set one, defaults are returned.
func (d *DeviceState) GetVhostUserConfig() *ovsdpdkdrav1alpha1.VhostUserSpec {
	d.mutex.RLock()
	defer d.mutex.RUnlock()
	return d.vhostUserConfig
}

func (d *DeviceState) updateConfig(bridges []ovsdpdkdrav1alpha1.BridgeSpec, vhostUser *ovsdpdkdrav1alpha1.VhostUserSpec) {
	// Apply VhostUserConfig defaults
	var vhostUserConfig *ovsdpdkdrav1alpha1.VhostUserSpec
	if vhostUser == nil {
		vhostUserConfig = &ovsdpdkdrav1alpha1.VhostUserSpec{}
	} else {
		vhostUserConfig = vhostUser.DeepCopy()
	}
	if vhostUserConfig.HostRootPath == "" {
		vhostUserConfig.HostRootPath = ovsdpdkdrav1alpha1.DefaultHostRootPath
	}
	if vhostUserConfig.ContainerRootPath == "" {
		vhostUserConfig.ContainerRootPath = ovsdpdkdrav1alpha1.DefaultContainerRootPath
	}

	d.mutex.Lock()
	d.allocatable = computeAllocatableDevices(bridges)
	d.vhostUserConfig = vhostUserConfig
	d.mutex.Unlock()
}

// UpdatePolicyDevices is called by the controller whenever the set of matching
// OvsDpdkResourcePolicy objects changes. bridges is the consolidated list of
// bridge specs that apply to this node. vhostUser is the first non-nil
// VhostUserSpec found across matching policies (nil means use defaults).
func (d *DeviceState) UpdatePolicyDevices(ctx context.Context, bridges []ovsdpdkdrav1alpha1.BridgeSpec, vhostUser *ovsdpdkdrav1alpha1.VhostUserSpec) error {
	logger := klog.FromContext(ctx).WithName("UpdatePolicyDevices")
	logger.Info("Updating policy devices", "bridges", len(bridges))

	seen := make(map[string]struct{}, len(bridges))
	for _, b := range bridges {
		if _, dup := seen[b.Name]; dup {
			return fmt.Errorf("duplicate bridge name %q across OvsDpdkResourcePolicy objects", b.Name)
		}
		seen[b.Name] = struct{}{}
	}

	d.updateConfig(bridges, vhostUser)

	logger.Info("Allocatable devices updated", "bridges", slices.Collect(maps.Keys(d.allocatable)))
	logger.V(2).Info("Allocatable devices updated", "devices", d.allocatable)

	if d.republishCallback != nil {
		if err := d.republishCallback(ctx); err != nil {
			logger.Error(err, "Republish callback failed")
			return fmt.Errorf("republish callback: %w", err)
		}
	}

	return nil
}

// PrepareResourceClaim prepares a single resource claim. It creates the
// per-claim socket directory and writes the CDI spec.
func (d *DeviceState) PrepareResourceClaim(ctx context.Context, claim *resourceapi.ResourceClaim) (*dratypes.PreparedDevice, error) {
	logger := klog.FromContext(ctx).WithName("PrepareResourceClaim")

	if claim.Status.Allocation == nil {
		return nil, fmt.Errorf("claim %s/%s has no allocation", claim.Namespace, claim.Name)
	}
	if len(claim.Status.ReservedFor) == 0 {
		return nil, fmt.Errorf("claim %s/%s has no ReservedFor entry", claim.Namespace, claim.Name)
	} else if len(claim.Status.ReservedFor) > 1 {
		return nil, fmt.Errorf("multiple pods found for claim %s/%s not supported", claim.Namespace, claim.Name)
	}
	podUID := k8stypes.UID(claim.Status.ReservedFor[0].UID)
	podClaimName := getPodClaimName(claim)

	results := claim.Status.Allocation.Devices.Results
	if len(results) != 1 {
		return nil, fmt.Errorf("claim %s/%s: expected exactly 1 allocation result, got %d", claim.Namespace, claim.Name, len(results))
	}
	allocResult := results[0]

	socketDir, err := d.createSocketDir(ctx, d.getSocketDir(podUID, claim))
	if err != nil {
		return nil, err
	}

	// TODO: Create OVS port

	cdiDeviceID := dracdi.DeviceID(claim.UID, allocResult.Device)
	containerDir := filepath.Join(d.GetVhostUserConfig().ContainerRootPath, podClaimName)

	pd := &dratypes.PreparedDevice{
		Device: kubeletplugin.Device{
			Requests:     []string{allocResult.Request},
			PoolName:     allocResult.Pool,
			DeviceName:   allocResult.Device,
			CDIDeviceIDs: []string{cdiDeviceID},
		},
		ClaimNamespacedName: kubeletplugin.NamespacedObject{
			NamespacedName: k8stypes.NamespacedName{
				Name:      claim.Name,
				Namespace: claim.Namespace,
			},
			UID: claim.UID,
		},
		BridgeName: allocResult.Device,
		Mount: dratypes.MountInfo{
			HostDir:      socketDir,
			ContainerDir: containerDir,
		},
		Socket: dratypes.SocketInfo{
			HostPath:      filepath.Join(socketDir, "vhost.sock"),
			ContainerPath: filepath.Join(containerDir, "vhost.sock"),
		},
	}

	logger.Info("Prepared vhost-user socket",
		"podUID", podUID,
		"claimName", claim.Name,
		"bridgeName", pd.BridgeName,
		"mount", pd.Mount,
		"socket", pd.Socket,
	)

	if err := d.cdi.CreateClaimSpecFile(pd); err != nil {
		_ = d.removeSocketDir(pd.Mount.HostDir)
		// TODO: delete OVS port
		return nil, fmt.Errorf("create CDI spec for claim %s: %w", claim.UID, err)
	}

	d.updateClaimStatus(ctx, claim, allocResult, pd)

	return pd, nil
}

// UnprepareResourceClaim removes the CDI spec and socket directory for a claim.
func (d *DeviceState) UnprepareResourceClaim(ctx context.Context, pd *dratypes.PreparedDevice) error {
	logger := klog.FromContext(ctx).WithName("UnprepareResourceClaim")
	claimUID := pd.ClaimNamespacedName.UID

	if err := d.cdi.DeleteClaimSpecFile(claimUID); err != nil {
		logger.Error(err, "Failed to delete CDI spec", "claimUID", claimUID)
		return fmt.Errorf("delete CDI spec for claim %s: %w", claimUID, err)
	}

	// TODO: delete OVS port

	if err := d.removeSocketDir(pd.Mount.HostDir); err != nil {
		return err
	}

	logger.Info("Cleaned up claim resources", "claimUID", claimUID, "socketDir", pd.Mount.HostDir)
	return nil
}

func (d *DeviceState) getSocketDir(podUID k8stypes.UID, claim *resourceapi.ResourceClaim) string {
	return filepath.Join(d.GetVhostUserConfig().HostRootPath, string(podUID)+"_"+getPodClaimName(claim))
}

func (d *DeviceState) createSocketDir(ctx context.Context, socketDir string) (string, error) {
	if err := os.MkdirAll(socketDir, 0o775); err != nil {
		return "", fmt.Errorf("create socket directory %q: %w", socketDir, err)
	}
	if err := os.Chmod(socketDir, 0o775); err != nil {
		return "", fmt.Errorf("chmod socket directory %q: %w", socketDir, err)
	}

	if err := d.permApplier.ApplyPermissions(ctx, socketDir, d.GetVhostUserConfig()); err != nil {
		_ = d.removeSocketDir(socketDir)
		return "", fmt.Errorf("apply permissions to %q: %w", socketDir, err)
	}

	return socketDir, nil
}

func (d *DeviceState) removeSocketDir(socketDir string) error {
	if err := os.RemoveAll(socketDir); err != nil {
		d.log.Error(err, "Failed to remove socket directory", "socketDir", socketDir)
		return fmt.Errorf("remove socket directory %q: %w", socketDir, err)
	}
	return nil
}

// updateClaimStatus writes driver debug data into ResourceClaim.Status.Devices
// after a successful prepare.
func (d *DeviceState) updateClaimStatus(
	ctx context.Context,
	claim *resourceapi.ResourceClaim,
	allocResult resourceapi.DeviceRequestAllocationResult,
	pd *dratypes.PreparedDevice,
) {
	logger := klog.FromContext(ctx).WithName("updateClaimStatus")

	payload, err := json.Marshal(deviceStatusData{
		Mount:        pd.Mount,
		Socket:       pd.Socket,
		BridgeName:   pd.BridgeName,
		CDIDeviceIDs: pd.Device.CDIDeviceIDs,
	})
	if err != nil {
		logger.Error(err, "Failed to marshal claim status data", "claimUID", claim.UID)
		return
	}

	claim.Status.Devices = append(claim.Status.Devices, resourceapi.AllocatedDeviceStatus{
		Driver:  allocResult.Driver,
		Pool:    allocResult.Pool,
		Device:  allocResult.Device,
		ShareID: (*string)(allocResult.ShareID),
		Data:    &runtime.RawExtension{Raw: payload},
	})
}

// computeAllocatableDevices converts a list of bridge specs into DRA device specifications.
func computeAllocatableDevices(bridges []ovsdpdkdrav1alpha1.BridgeSpec) AllocatableDevices {
	devices := make(AllocatableDevices, len(bridges))
	for _, bridge := range bridges {
		devices[bridge.Name] = bridgeToDevice(bridge)
	}
	return devices
}

func bridgeToDevice(bridge ovsdpdkdrav1alpha1.BridgeSpec) resourceapi.Device {
	one := resource.NewQuantity(1, resource.DecimalSI)
	return resourceapi.Device{
		Name:                     bridge.Name,
		AllowMultipleAllocations: ptr.To(true),
		Capacity: map[resourceapi.QualifiedName]resourceapi.DeviceCapacity{
			"ovsdpdk.k8snetworkplumbingwg.io/ports": {
				Value: *resource.NewQuantity(ovsdpdkdrav1alpha1.DefaultBridgeCapacity, resource.DecimalSI),
				RequestPolicy: &resourceapi.CapacityRequestPolicy{
					Default: one,
					ValidRange: &resourceapi.CapacityRequestPolicyRange{
						Min:  resource.NewQuantity(0, resource.DecimalSI),
						Step: one,
					},
				},
			},
		},
	}
}

func getPodClaimName(claim *resourceapi.ResourceClaim) string {
	// For claims created from a ResourceClaimTemplate the kubelet sets the
	// pod-local claim name in a standard annotation. For hand-written claims
	// the annotation is absent and claim.Name is already stable.
	podClaimName := claim.Annotations[consts.PodClaimNameAnnotation]
	if podClaimName == "" {
		podClaimName = claim.Name
	}
	return podClaimName
}
