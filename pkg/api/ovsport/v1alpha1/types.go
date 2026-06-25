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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/amorenoz/dra-driver-ovsdpdk/pkg/consts"
)

const (
	// KindOvsPortConfig is the Kind value for OvsPortConfig.
	KindOvsPortConfig = "OvsPortConfig"

	// APIVersion is the apiVersion value for types in this package.
	APIVersion = consts.GroupName + "/v1alpha1"
)

// OvsPortConfig is the opaque per-allocation configuration embedded in a
// ResourceClaim. It carries user-specified values for OVS port properties.
type OvsPortConfig struct {
	metav1.TypeMeta `json:",inline"`
}

func DefaultOvsPortConfig() *OvsPortConfig {
	return &OvsPortConfig{}
}
