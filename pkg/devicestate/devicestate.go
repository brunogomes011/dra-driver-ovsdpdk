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

	"k8s.io/klog/v2"

	ovsdpdkdrav1alpha1 "github.com/amorenoz/dra-driver-ovsdpdk/pkg/api/ovsdpdkdra/v1alpha1"
)

// DeviceState manages the set of vhost-user devices advertised by this node.
type DeviceState struct {
	log klog.Logger
}

// New creates a new DeviceState.
func New() *DeviceState {
	return &DeviceState{
		log: klog.Background().WithName("DeviceState"),
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

	return nil
}
