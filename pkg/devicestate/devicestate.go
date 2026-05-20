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
	"fmt"
	"maps"
	"slices"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	ovsdpdkdrav1alpha1 "github.com/amorenoz/dra-driver-ovsdpdk/pkg/api/ovsdpdkdra/v1alpha1"
	"github.com/amorenoz/dra-driver-ovsdpdk/pkg/consts"
)

// AllocatableDevices maps device names to their DRA device specifications.
type AllocatableDevices map[string]resourceapi.Device

// DeviceState manages the set of vhost-user devices advertised by this node.
type DeviceState struct {
	log               klog.Logger
	republishCallback func(ctx context.Context) error
	allocatable       AllocatableDevices
}

// New creates a new DeviceState.
func New() *DeviceState {
	return &DeviceState{
		log:         klog.Background().WithName("DeviceState"),
		allocatable: AllocatableDevices{},
	}
}

// UpdateConfig is called by the controller whenever the OvsDpdkConfig object changes.
// spec is nil when the config object does not exist.
func (d *DeviceState) UpdateConfig(ctx context.Context, spec *ovsdpdkdrav1alpha1.OvsDpdkConfigSpec) error {
	logger := klog.FromContext(ctx).WithName("UpdateConfig")
	if spec == nil {
		logger.Info("No OvsDpdkConfig found, using defaults")
		return nil
	}
	logger.Info("Config updated")
	return nil
}

// SetRepublishCallback sets a callback that is invoked after UpdatePolicyDevices
// successfully updates the set of allocatable devices.
func (d *DeviceState) SetRepublishCallback(callback func(ctx context.Context) error) {
	d.republishCallback = callback
}

// GetAllocatableDevices returns a copy of the current set of allocatable devices.
func (d *DeviceState) GetAllocatableDevices() AllocatableDevices {
	return maps.Clone(d.allocatable)
}

// UpdatePolicyDevices is called by the controller whenever the set of matching
// OvsDpdkResourcePolicy objects changes. bridges is the consolidated list of
// bridge specs that apply to this node.
func (d *DeviceState) UpdatePolicyDevices(ctx context.Context, bridges []ovsdpdkdrav1alpha1.BridgeSpec) error {
	logger := klog.FromContext(ctx).WithName("UpdatePolicyDevices")
	logger.Info("Updating policy devices", "bridges", len(bridges))

	seen := make(map[string]struct{}, len(bridges))
	for _, b := range bridges {
		if _, dup := seen[b.Name]; dup {
			return fmt.Errorf("duplicate bridge name %q across OvsDpdkResourcePolicy objects", b.Name)
		}
		seen[b.Name] = struct{}{}
	}

	d.allocatable = computeAllocatableDevices(bridges)
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
				Value: *resource.NewQuantity(consts.DefaultBridgeCapacity, resource.DecimalSI),
				RequestPolicy: &resourceapi.CapacityRequestPolicy{
					Default: one,
					ValidRange: &resourceapi.CapacityRequestPolicyRange{
						Min:  resource.NewQuantity(1, resource.DecimalSI),
						Step: one,
					},
				},
			},
		},
	}
}
