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

package devicestate_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	ovsdpdkdrav1alpha1 "github.com/amorenoz/dra-driver-ovsdpdk/pkg/api/ovsdpdkdra/v1alpha1"
	"github.com/amorenoz/dra-driver-ovsdpdk/pkg/devicestate"
)

func TestDeviceState(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "DeviceState Suite")
}

var _ = Describe("DeviceState", func() {
	var ds *devicestate.DeviceState

	BeforeEach(func() {
		ds = devicestate.New()
	})

	Describe("UpdatePolicyDevices", func() {
		It("should succeed with an empty bridge list", func(ctx SpecContext) {
			Expect(ds.UpdatePolicyDevices(ctx, nil)).To(Succeed())
		})

		It("should succeed with unique bridge names", func(ctx SpecContext) {
			bridges := []ovsdpdkdrav1alpha1.BridgeSpec{
				{Name: "br0"},
				{Name: "br1"},
				{Name: "br2"},
			}
			Expect(ds.UpdatePolicyDevices(ctx, bridges)).To(Succeed())
		})

		It("should return an error when two bridges share the same name", func(ctx SpecContext) {
			bridges := []ovsdpdkdrav1alpha1.BridgeSpec{
				{Name: "br0"},
				{Name: "br1"},
				{Name: "br0"},
			}
			Expect(ds.UpdatePolicyDevices(ctx, bridges)).To(
				MatchError(ContainSubstring("duplicate bridge name")),
			)
		})

		It("should return an error when all bridges share the same name", func(ctx SpecContext) {
			bridges := []ovsdpdkdrav1alpha1.BridgeSpec{
				{Name: "br-phy0"},
				{Name: "br-phy0"},
			}
			Expect(ds.UpdatePolicyDevices(ctx, bridges)).To(
				MatchError(ContainSubstring(`"br-phy0"`)),
			)
		})
	})
})
