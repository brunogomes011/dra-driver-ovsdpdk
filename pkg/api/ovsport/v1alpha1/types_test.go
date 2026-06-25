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

package v1alpha1_test

import (
	"encoding/json"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"

	. "github.com/amorenoz/dra-driver-ovsdpdk/pkg/api/ovsport/v1alpha1"
)

func TestOvsPortV1alpha1(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ovsport/v1alpha1")
}

var _ = Describe("OvsPortConfig", func() {
	Describe("JSON round-trip", func() {
		It("preserves apiVersion and kind", func() {
			cfg := OvsPortConfig{}
			cfg.APIVersion = APIVersion
			cfg.Kind = KindOvsPortConfig

			data, err := json.Marshal(cfg)
			Expect(err).NotTo(HaveOccurred())

			var got OvsPortConfig
			Expect(json.Unmarshal(data, &got)).To(Succeed())
			Expect(got.APIVersion).To(Equal(APIVersion))
			Expect(got.Kind).To(Equal(KindOvsPortConfig))
		})

		It("omits vlan when nil", func() {
			cfg := OvsPortConfig{}
			cfg.APIVersion = APIVersion
			cfg.Kind = KindOvsPortConfig

			data, err := json.Marshal(cfg)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).NotTo(ContainSubstring("vlan"))
		})

		It("preserves vlan when set", func() {
			cfg := OvsPortConfig{}
			cfg.APIVersion = APIVersion
			cfg.Kind = KindOvsPortConfig
			cfg.Vlan = ptr.To(100)

			data, err := json.Marshal(cfg)
			Expect(err).NotTo(HaveOccurred())

			var got OvsPortConfig
			Expect(json.Unmarshal(data, &got)).To(Succeed())
			Expect(got.Vlan).NotTo(BeNil())
			Expect(*got.Vlan).To(Equal(100))
		})

		It("preserves vlan = 0", func() {
			cfg := OvsPortConfig{}
			cfg.APIVersion = APIVersion
			cfg.Kind = KindOvsPortConfig
			cfg.Vlan = ptr.To(0)

			data, err := json.Marshal(cfg)
			Expect(err).NotTo(HaveOccurred())

			var got OvsPortConfig
			Expect(json.Unmarshal(data, &got)).To(Succeed())
			Expect(got.Vlan).NotTo(BeNil())
			Expect(*got.Vlan).To(Equal(0))
		})

		It("unmarshals from a raw opaque payload with vlan", func() {
			raw := `{"apiVersion":"ovsdpdk.k8snetworkplumbingwg.io/v1alpha1","kind":"OvsPortConfig","vlan":200}`
			var cfg OvsPortConfig
			Expect(json.Unmarshal([]byte(raw), &cfg)).To(Succeed())
			Expect(cfg.APIVersion).To(Equal(APIVersion))
			Expect(cfg.Kind).To(Equal(KindOvsPortConfig))
			Expect(cfg.Vlan).NotTo(BeNil())
			Expect(*cfg.Vlan).To(Equal(200))
		})
	})
})
