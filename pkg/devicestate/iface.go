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
	"context"

	resourceapi "k8s.io/api/resource/v1"

	dratypes "github.com/amorenoz/dra-driver-ovsdpdk/pkg/types"
)

// DeviceStateIface is the interface consumed by the driver layer.
// *DeviceState satisfies this interface.
type DeviceStateIface interface {
	SetRepublishCallback(callback func(ctx context.Context) error)
	GetAllocatableDevices() AllocatableDevices
	PrepareResourceClaim(ctx context.Context, claim *resourceapi.ResourceClaim) ([]*dratypes.PreparedDevice, error)
	UnprepareResourceClaim(ctx context.Context, preparedDevices []*dratypes.PreparedDevice) error
}
