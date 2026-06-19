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

// Package cdi manages CDI spec files for OVS-DPDK vhost-user devices.
package cdi

import (
	"fmt"

	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
	cdiparser "tags.cncf.io/container-device-interface/pkg/parser"
	cdispecs "tags.cncf.io/container-device-interface/specs-go"

	"github.com/amorenoz/dra-driver-ovsdpdk/pkg/consts"
	dratypes "github.com/amorenoz/dra-driver-ovsdpdk/pkg/types"
)

const (
	cdiVendor = consts.DriverName
	cdiClass  = "vhost-user"
	cdiKind   = cdiVendor + "/" + cdiClass
)

// Handler manages CDI spec files for prepared OVS-DPDK claims.
type Handler struct {
	log   klog.Logger
	cache *cdiapi.Cache
}

// New creates a new CDI Handler that writes spec files under cdiRoot.
func New(cdiRoot string) (*Handler, error) {
	cache, err := cdiapi.NewCache(cdiapi.WithSpecDirs(cdiRoot))
	if err != nil {
		return nil, fmt.Errorf("unable to create a new CDI cache: %w", err)
	}

	return &Handler{
		log:   klog.Background().WithName("CDIHandler"),
		cache: cache,
	}, nil
}

// DeviceID returns the fully-qualified CDI device ID for the given claim UID.
func DeviceID(claimUID k8stypes.UID, device, request string) string {
	return cdiparser.QualifiedName(cdiVendor, cdiClass, fmt.Sprintf("%s-%s-%s", string(claimUID), device, request))
}

// CreateClaimSpecFile writes a CDI spec file for the list of PreparedDevices. The spec
// contains a single device with one bind-mount of the per-request socket directory.
func (h *Handler) CreateClaimSpecFile(preparedDevices []*dratypes.PreparedDevice) error {
	claim := preparedDevices[0].ClaimNamespacedName
	claimUID := string(claim.UID)
	logger := h.log.WithValues("claimName", claim.Name, "claimNamespace", claim.Namespace, "claimUID", claimUID)

	specName := cdiapi.GenerateTransientSpecName(cdiVendor, cdiClass, claimUID)

	// Combine CDI specs of all prepared devices.
	cdiDevices := []cdispecs.Device{}
	for _, pd := range preparedDevices {
		cdiDev := cdispecs.Device{
			Name: fmt.Sprintf("%s-%s-%s", claimUID, pd.Device.DeviceName, pd.Device.Requests[0]),
			ContainerEdits: cdispecs.ContainerEdits{
				Mounts: []*cdispecs.Mount{
					{
						HostPath:      pd.Mount.HostDir,
						ContainerPath: pd.Mount.ContainerDir,
						Options:       []string{"bind", "rw"},
					},
				},
			},
		}
		cdiDevices = append(cdiDevices, cdiDev)
	}

	raw := &cdispecs.Spec{
		Version: "0.6.0",
		Kind:    cdiKind,
		Devices: cdiDevices,
	}

	logger.V(2).Info("writing CDI spec", "specName", specName, "spec", raw)
	if err := h.cache.WriteSpec(raw, specName); err != nil {
		return fmt.Errorf("write CDI: %w", err)
	}

	logger.Info("CDI spec written", "specName", specName)
	return nil
}

// DeleteClaimSpecFile removes the CDI spec file for the given claim UID.
func (h *Handler) DeleteClaimSpecFile(claimUID k8stypes.UID) error {
	specName := cdiapi.GenerateTransientSpecName(cdiVendor, cdiClass, string(claimUID))
	if err := h.cache.RemoveSpec(specName); err != nil {
		return fmt.Errorf("remove CDI spec for claim %s: %w", claimUID, err)
	}

	h.log.Info("CDI spec removed", "claimUID", claimUID, "specName", specName)
	return nil
}
