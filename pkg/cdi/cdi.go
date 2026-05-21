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
	log     klog.Logger
	cdiRoot string
}

// New creates a new CDI Handler that writes spec files under cdiRoot.
func New(cdiRoot string) *Handler {
	return &Handler{
		log:     klog.Background().WithName("CDIHandler"),
		cdiRoot: cdiRoot,
	}
}

// DeviceID returns the fully-qualified CDI device ID for the given claim UID.
func DeviceID(claimUID k8stypes.UID) string {
	return cdiparser.QualifiedName(cdiVendor, cdiClass, shortenUID(claimUID))
}

// CreateClaimSpecFile writes a CDI spec file for the given claim. The spec
// contains a single device with one bind-mount of the per-claim socket directory.
func (h *Handler) CreateClaimSpecFile(pd *dratypes.PreparedDevice) error {
	logger := h.log.WithValues("claimUID", pd.ClaimUID)

	raw := &cdispecs.Spec{
		Version: "0.6.0",
		Kind:    cdiKind,
		Devices: []cdispecs.Device{
			{
				Name: shortenUID(pd.ClaimUID),
				ContainerEdits: cdispecs.ContainerEdits{
					Mounts: []*cdispecs.Mount{
						{
							HostPath:      pd.Mount.HostDir,
							ContainerPath: pd.Mount.ContainerDir,
							Options:       []string{"bind", "rw"},
						},
					},
				},
			},
		},
	}

	cache, err := cdiapi.NewCache(cdiapi.WithSpecDirs(h.cdiRoot))
	if err != nil {
		return fmt.Errorf("create CDI cache: %w", err)
	}

	name := specName(pd.ClaimUID)
	if err := cache.WriteSpec(raw, name); err != nil {
		return fmt.Errorf("write CDI spec for claim %s: %w", pd.ClaimUID, err)
	}

	logger.Info("CDI spec written", "deviceID", pd.CDIDeviceID, "specName", name)
	return nil
}

// DeleteClaimSpecFile removes the CDI spec file for the given claim UID.
func (h *Handler) DeleteClaimSpecFile(claimUID k8stypes.UID) error {
	cache, err := cdiapi.NewCache(cdiapi.WithSpecDirs(h.cdiRoot))
	if err != nil {
		return fmt.Errorf("create CDI cache: %w", err)
	}

	name := specName(claimUID)
	if err := cache.RemoveSpec(name); err != nil {
		return fmt.Errorf("remove CDI spec for claim %s: %w", claimUID, err)
	}

	h.log.Info("CDI spec removed", "claimUID", claimUID, "specName", name)
	return nil
}

// specName returns the CDI spec file name for the given claim UID.
func specName(claimUID k8stypes.UID) string {
	return fmt.Sprintf("%s-%s-%s", cdiVendor, cdiClass, shortenUID(claimUID))
}

func shortenUID(uid k8stypes.UID) string {
	s := string(uid)
	if len(s) > 8 {
		return s[:8]
	}
	return s
}
