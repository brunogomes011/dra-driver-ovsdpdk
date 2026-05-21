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
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestV1alpha1(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ovsdpdkdra/v1alpha1")
}

// helper struct to test UserGroupID embedded in a realistic context.
type userGroupWrapper struct {
	Value UserGroupID `json:"value"`
}

var _ = Describe("UserGroupID", func() {
	Describe("UnmarshalJSON", func() {
		It("unmarshals a string name", func() {
			var w userGroupWrapper
			Expect(json.Unmarshal([]byte(`{"value":"openvswitch"}`), &w)).To(Succeed())
			Expect(w.Value.IsName()).To(BeTrue())
			Expect(w.Value.GetName()).To(Equal("openvswitch"))
		})

		It("unmarshals a numeric ID", func() {
			var w userGroupWrapper
			Expect(json.Unmarshal([]byte(`{"value":107}`), &w)).To(Succeed())
			Expect(w.Value.IsName()).To(BeFalse())
			Expect(w.Value.GetID()).To(Equal(107))
		})

		It("unmarshals zero as a numeric ID", func() {
			var w userGroupWrapper
			// Note: 0 is indistinguishable from the zero value of intstr.IntOrString
			// (which defaults to Type=Int, IntVal=0), so zero is always treated as an ID.
			Expect(json.Unmarshal([]byte(`{"value":0}`), &w)).To(Succeed())
			Expect(w.Value.IsName()).To(BeFalse())
			Expect(w.Value.GetID()).To(Equal(0))
		})

		It("returns an error for invalid input", func() {
			var w userGroupWrapper
			Expect(json.Unmarshal([]byte(`{"value":true}`), &w)).NotTo(Succeed())
		})

		It("returns an error for a boolean", func() {
			var w userGroupWrapper
			Expect(json.Unmarshal([]byte(`{"value":true}`), &w)).NotTo(Succeed())
		})
	})

	Describe("MarshalJSON", func() {
		It("marshals a name back as a string", func() {
			w := userGroupWrapper{Value: NewUserGroupIDFromName("openvswitch")}
			data, err := json.Marshal(w)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).To(Equal(`{"value":"openvswitch"}`))
		})

		It("marshals an ID back as a number", func() {
			w := userGroupWrapper{Value: NewUserGroupIDFromID(107)}
			data, err := json.Marshal(w)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).To(Equal(`{"value":107}`))
		})

		It("round-trips a string name", func() {
			original := `{"value":"qemu"}`
			var w userGroupWrapper
			Expect(json.Unmarshal([]byte(original), &w)).To(Succeed())
			data, err := json.Marshal(w)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).To(Equal(original))
		})

		It("round-trips a numeric ID", func() {
			original := `{"value":718}`
			var w userGroupWrapper
			Expect(json.Unmarshal([]byte(original), &w)).To(Succeed())
			data, err := json.Marshal(w)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).To(Equal(original))
		})
	})

	Describe("VhostUserSpec unmarshalling", func() {
		It("accepts a full spec with mixed user/group types", func() {
			raw := `{
				"user": "openvswitch",
				"group": 107,
				"selinuxLabel": "system_u:object_r:container_file_t:s0",
				"aclUsers": ["openvswitch"]
			}`
			var spec VhostUserSpec
			Expect(json.Unmarshal([]byte(raw), &spec)).To(Succeed())
			Expect(spec.User).NotTo(BeNil())
			Expect(spec.User.IsName()).To(BeTrue())
			Expect(spec.User.GetName()).To(Equal("openvswitch"))
			Expect(spec.Group).NotTo(BeNil())
			Expect(spec.Group.IsName()).To(BeFalse())
			Expect(spec.Group.GetID()).To(Equal(107)) //nolint:gomnd
			Expect(spec.SelinuxLabel).NotTo(BeNil())
			Expect(*spec.SelinuxLabel).To(Equal("system_u:object_r:container_file_t:s0"))
			Expect(spec.ACLUsers).To(ConsistOf("openvswitch"))
		})

		It("accepts a spec with no user or group (zero values)", func() {
			raw := `{}`
			var spec VhostUserSpec
			Expect(json.Unmarshal([]byte(raw), &spec)).To(Succeed())
			Expect(spec.User).To(Equal(UserGroupID{}))
			Expect(spec.Group).To(Equal(UserGroupID{}))
		})
	})
})
