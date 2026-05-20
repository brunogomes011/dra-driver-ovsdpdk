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
	"context"
	"errors"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	resourceapi "k8s.io/api/resource/v1beta1"

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

	Describe("GetAllocatableDevices", func() {
		It("should return an empty non-nil map when no devices are set", func() {
			devices := ds.GetAllocatableDevices()
			Expect(devices).NotTo(BeNil())
			Expect(devices).To(BeEmpty())
		})

		It("should return a copy that does not affect internal state when modified", func() {
			devices := ds.GetAllocatableDevices()
			devices["injected"] = resourceapi.Device{}
			Expect(ds.GetAllocatableDevices()).To(BeEmpty())
		})
	})

	Describe("SetRepublishCallback", func() {
		It("should not call the callback during UpdatePolicyDevices if not set", func(ctx SpecContext) {
			Expect(ds.UpdatePolicyDevices(ctx, nil)).To(Succeed())
		})

		It("should call the callback after a successful UpdatePolicyDevices", func(ctx SpecContext) {
			called := false
			ds.SetRepublishCallback(func(_ context.Context) error {
				called = true
				return nil
			})
			Expect(ds.UpdatePolicyDevices(ctx, nil)).To(Succeed())
			Expect(called).To(BeTrue())
		})

		It("should propagate callback errors back to the caller", func(ctx SpecContext) {
			callbackErr := errors.New("publish failed")
			ds.SetRepublishCallback(func(_ context.Context) error {
				return callbackErr
			})
			Expect(ds.UpdatePolicyDevices(ctx, nil)).To(MatchError(ContainSubstring("publish failed")))
		})

		It("should not call the callback when bridge validation fails", func(ctx SpecContext) {
			called := false
			ds.SetRepublishCallback(func(_ context.Context) error {
				called = true
				return nil
			})
			bridges := []ovsdpdkdrav1alpha1.BridgeSpec{
				{Name: "br0"},
				{Name: "br0"},
			}
			Expect(ds.UpdatePolicyDevices(ctx, bridges)).NotTo(Succeed())
			Expect(called).To(BeFalse())
		})
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
