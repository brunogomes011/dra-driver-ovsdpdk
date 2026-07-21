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
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
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
	// VhostUser configures the vhost-user socket directory options.
	VhostUser *VhostUserSpec `json:"vhostUser,omitempty"`
}

// OvsDpdkConfigList contains a list of OvsDpdkConfig.
//
// +kubebuilder:object:root=true
type OvsDpdkConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OvsDpdkConfig `json:"items"`
}

// VhostUserSpec configures the vhost user configuration parameters.
type VhostUserSpec struct {
	// ContainerRootPath is the path inside the container where the per-pod
	// socket directory is mounted.
	// +optional
	ContainerRootPath string `json:"containerRootPath,omitempty"`

	// User is the owner of the vhost-user socket directory. Can be specified
	// as a user name (e.g. "openvswitch") or a numeric UID (e.g. 107).
	User UserGroupID `json:"user"`

	// Group is the owning group of the vhost-user socket directory. Can be
	// specified as a group name (e.g. "qemu") or a numeric GID (e.g. 718).
	Group UserGroupID `json:"group"`

	// SelinuxLabel is the SELinux label applied to the socket directory.
	// Must be in the standard SELinux context format: user:role:type:level.
	// +optional
	// +kubebuilder:validation:Pattern=`^[^:]+:[^:]+:[^:]+:[^:]+$`
	SelinuxLabel *string `json:"selinuxLabel,omitempty"`

	// ACLUsers is a list of user names granted access to the socket directory
	// via filesystem ACLs (setfacl).
	// +optional
	ACLUsers []string `json:"aclUsers,omitempty"`
}

// UserGroupID represents a user or group identity that can be expressed either
// as a name (e.g. "openvswitch") or as a numeric ID (e.g. 107).
//
// +kubebuilder:validation:XIntOrString
type UserGroupID intstr.IntOrString

// NewUserGroupIDFromName creates a UserGroupID from a string name.
func NewUserGroupIDFromName(name string) UserGroupID {
	return UserGroupID(intstr.FromString(name))
}

// NewUserGroupIDFromID creates a UserGroupID from a numeric ID.
func NewUserGroupIDFromID(id int) UserGroupID {
	return UserGroupID(intstr.FromInt32(int32(id)))
}

// IsName reports whether the identity was specified as a string name.
func (u UserGroupID) IsName() bool {
	return intstr.IntOrString(u).Type == intstr.String
}

// GetName returns the string name. It panics if the identity is not a name;
// callers should check IsName() first.
func (u UserGroupID) GetName() string {
	ios := intstr.IntOrString(u)
	if ios.Type != intstr.String {
		panic("UserGroupID is not a name")
	}
	return ios.StrVal
}

// GetID returns the numeric ID. It panics if the identity is not an ID;
// callers should check IsName() first.
func (u UserGroupID) GetID() int {
	ios := intstr.IntOrString(u)
	if ios.Type != intstr.Int {
		panic("UserGroupID is not an ID")
	}
	return int(ios.IntVal)
}

// UnmarshalJSON implements json.Unmarshaler. It accepts both a JSON number
// (integer) and a JSON string.
func (u *UserGroupID) UnmarshalJSON(data []byte) error {
	// Try integer first.
	var id int32
	if err := json.Unmarshal(data, &id); err == nil {
		*u = UserGroupID(intstr.FromInt32(id))
		return nil
	}

	// Fall back to string.
	var name string
	if err := json.Unmarshal(data, &name); err == nil {
		*u = UserGroupID(intstr.FromString(name))
		return nil
	}

	return fmt.Errorf("userGroupID must be a string name or an integer ID, got: %s", string(data))
}

// MarshalJSON implements json.Marshaler. It preserves the original type.
func (u UserGroupID) MarshalJSON() ([]byte, error) {
	ios := intstr.IntOrString(u)
	if ios.Type == intstr.String {
		return json.Marshal(ios.StrVal)
	}
	return json.Marshal(ios.IntVal)
}

// DeepCopyInto copies the receiver into out. intstr.IntOrString is a value
// type so a shallow copy is sufficient.
func (u *UserGroupID) DeepCopyInto(out *UserGroupID) {
	*out = *u
}
