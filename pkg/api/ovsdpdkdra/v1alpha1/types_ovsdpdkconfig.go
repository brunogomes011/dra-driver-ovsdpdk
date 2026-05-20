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
)

// OvsDpdkConfig defines the global configuration for the OVS-DPDK DRA Driver.
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
type OvsDpdkConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec OvsDpdkConfigSpec `json:"spec"`
}

// OvsDpdkConfig defines the desired configuration of the OVS-DPDK DRA Driver.
type OvsDpdkConfigSpec struct {
}

// OvsDpdkConfigList contains a list of OvsDpdkConfig.
//
// +kubebuilder:object:root=true
type OvsDpdkConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OvsDpdkConfig `json:"items"`
}
