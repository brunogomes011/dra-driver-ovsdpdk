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

// Package podmanager caches prepared resource-claim state between prepare and
// unprepare calls in the driver layer.
package podmanager

import (
	"sync"

	k8stypes "k8s.io/apimachinery/pkg/types"

	dratypes "github.com/amorenoz/dra-driver-ovsdpdk/pkg/types"
)

// PodManager is a thread-safe cache of PreparedDevice records keyed by claim UID.
type PodManager struct {
	mu         sync.RWMutex
	byClaimUID map[k8stypes.UID][]*dratypes.PreparedDevice
}

// New creates a new PodManager.
func New() *PodManager {
	return &PodManager{
		byClaimUID: make(map[k8stypes.UID][]*dratypes.PreparedDevice),
	}
}

// Get returns the PreparedDevice for the given claim UID.
func (pm *PodManager) Get(claimUID k8stypes.UID) ([]*dratypes.PreparedDevice, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	sc, ok := pm.byClaimUID[claimUID]
	return sc, ok
}

// Set stores the PreparedDevice for the given claim UID.
func (pm *PodManager) Set(claimUID k8stypes.UID, sc []*dratypes.PreparedDevice) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.byClaimUID[claimUID] = sc
}

// Delete removes and returns the PreparedDevice for the given claim UID.
// Returns nil if not found.
func (pm *PodManager) Delete(claimUID k8stypes.UID) []*dratypes.PreparedDevice {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	sc, ok := pm.byClaimUID[claimUID]
	if !ok {
		return nil
	}
	delete(pm.byClaimUID, claimUID)
	return sc
}
