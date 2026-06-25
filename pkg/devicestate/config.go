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

package devicestate

import (
	"encoding/json"
	"fmt"
	"slices"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/klog/v2"

	ovsportv1alpha1 "github.com/amorenoz/dra-driver-ovsdpdk/pkg/api/ovsport/v1alpha1"
)

// ParseClaimConfig extracts the OvsPortConfig from the allocation config
// entries for the given driver and request.
func ParseClaimConfig(configs []resourceapi.DeviceAllocationConfiguration, driverName, requestName string) (*ovsportv1alpha1.OvsPortConfig, error) {
	logger := klog.Background().WithName("parseClaimConfig")

	for _, cfg := range configs {
		var portConfig ovsportv1alpha1.OvsPortConfig

		if cfg.Opaque == nil || cfg.Opaque.Driver != driverName {
			continue
		}

		if cfg.Source == resourceapi.AllocationConfigSourceClass {
			logger.Info("Ignoring class-sourced config: not yet implemented")
			continue
		} else if cfg.Source != resourceapi.AllocationConfigSourceClaim {
			return nil, fmt.Errorf("unknown config source: %v", cfg.Source)
		}

		if err := json.Unmarshal(cfg.Opaque.Parameters.Raw, &portConfig); err != nil {
			return nil, fmt.Errorf("unmarshal OvsPortConfig: %w", err)
		}

		if portConfig.Kind != ovsportv1alpha1.KindOvsPortConfig {
			return nil, fmt.Errorf("unexpected kind %q in claim config: want %q", portConfig.Kind, ovsportv1alpha1.KindOvsPortConfig)
		}
		if portConfig.APIVersion != ovsportv1alpha1.APIVersion {
			return nil, fmt.Errorf("unexpected apiVersion %q in claim config: want %q", portConfig.APIVersion, ovsportv1alpha1.APIVersion)
		}

		if err := validatePortConfig(&portConfig); err != nil {
			return nil, fmt.Errorf("OvsPortConfig validation failed: %w", err)
		}

		// Empty Requests means "applies to all".
		if len(cfg.Requests) == 0 || slices.Contains(cfg.Requests, requestName) {
			return &portConfig, nil
		}
	}
	return nil, nil
}

func validatePortConfig(config *ovsportv1alpha1.OvsPortConfig) error {
	if config.Vlan != nil && (*config.Vlan < 0 || *config.Vlan > 4095) {
		return fmt.Errorf("vlan %d out of range [0, 4095]", *config.Vlan)
	}
	return nil
}
