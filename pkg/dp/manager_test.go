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

package dp

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	ovsdpdkdrav1alpha1 "github.com/amorenoz/dra-driver-ovsdpdk/pkg/api/ovsdpdkdra/v1alpha1"
)

// bridgeSpec is a helper to build a BridgeSpec with a TopologyResource.
func bridgeSpec(name, topologyResource string) ovsdpdkdrav1alpha1.BridgeSpec {
	return ovsdpdkdrav1alpha1.BridgeSpec{Name: name, TopologyResource: topologyResource}
}

var _ = Describe("Manager", func() {
	var (
		ctx         context.Context
		numa        *MocknumaProvider
		mgr         *Manager
		createdSrvs []*MockTopologyDPServer
		origFactory func(string, int, int) TopologyDPServer
	)

	BeforeEach(func() {
		ctx = context.Background()
		numa = NewMocknumaProvider(GinkgoT())
		createdSrvs = nil

		numa.EXPECT().SetInterfaceNotifier(mock.Anything).Once()

		// Save and restore the global factory around each test.
		origFactory = newServerFunc
		// Default factory panics if called unexpectedly — tests that expect
		// server creation must override newServerFunc themselves.
		newServerFunc = func(resourceName string, numaNode, deviceCount int) TopologyDPServer {
			Fail("unexpected call to newServerFunc — set a factory in the test")
			return nil
		}

		mgr = NewManager(ctx, numa)
	})

	AfterEach(func() {
		newServerFunc = origFactory
	})

	Describe("UpdateResources", func() {
		Context("topology map management", func() {
			It("builds topology from bridges with TopologyResource", func() {
				numa.EXPECT().BridgeNUMANodes("br0").Return([]int{0}).Once()
				newServerFunc = func(resourceName string, numaNode, _ int) TopologyDPServer {
					srv := NewMockTopologyDPServer(GinkgoT())
					srv.EXPECT().GetNUMA().Return(numaNode).Maybe()
					srv.EXPECT().start(mock.Anything).Return(nil).Once()
					createdSrvs = append(createdSrvs, srv)
					return srv
				}

				mgr.UpdateResources(ctx, []ovsdpdkdrav1alpha1.BridgeSpec{
					bridgeSpec("br0", "example.com/topo-br0"),
					{Name: "br1"}, // no TopologyResource
				})

				mgr.mutex.Lock()
				defer mgr.mutex.Unlock()
				Expect(mgr.topology).To(HaveKeyWithValue("br0", "example.com/topo-br0"))
				Expect(mgr.topology).NotTo(HaveKey("br1"))
			})

			It("clears topology when called with an empty bridge list", func() {
				numa.EXPECT().BridgeNUMANodes("br0").Return([]int{0}).Once()
				newServerFunc = func(resourceName string, numaNode, _ int) TopologyDPServer {
					srv := NewMockTopologyDPServer(GinkgoT())
					srv.EXPECT().GetNUMA().Return(numaNode).Maybe()
					srv.EXPECT().start(mock.Anything).Return(nil).Once()
					srv.EXPECT().stop().Once()
					createdSrvs = append(createdSrvs, srv)
					return srv
				}
				mgr.UpdateResources(ctx, []ovsdpdkdrav1alpha1.BridgeSpec{
					bridgeSpec("br0", "example.com/topo-br0"),
				})

				mgr.UpdateResources(ctx, nil)

				mgr.mutex.Lock()
				defer mgr.mutex.Unlock()
				Expect(mgr.topology).To(BeEmpty())
				Expect(mgr.servers).To(BeEmpty())
			})
		})

		Context("server lifecycle", func() {
			It("starts a server when the bridge has a valid single NUMA node", func() {
				numa.EXPECT().BridgeNUMANodes("br0").Return([]int{1}).Once()
				newServerFunc = func(resourceName string, numaNode, _ int) TopologyDPServer {
					srv := NewMockTopologyDPServer(GinkgoT())
					srv.EXPECT().GetNUMA().Return(numaNode).Maybe()
					srv.EXPECT().start(mock.Anything).Return(nil).Once()
					createdSrvs = append(createdSrvs, srv)
					return srv
				}

				mgr.UpdateResources(ctx, []ovsdpdkdrav1alpha1.BridgeSpec{
					bridgeSpec("br0", "example.com/topo-br0"),
				})

				Expect(createdSrvs).To(HaveLen(1))
				mgr.mutex.Lock()
				defer mgr.mutex.Unlock()
				Expect(mgr.servers).To(HaveKey("br0"))
			})

			It("does not start a server when NUMA is empty (no DPDK interfaces)", func() {
				numa.EXPECT().BridgeNUMANodes("br0").Return(nil).Once()

				mgr.UpdateResources(ctx, []ovsdpdkdrav1alpha1.BridgeSpec{
					bridgeSpec("br0", "example.com/topo-br0"),
				})

				Expect(createdSrvs).To(BeEmpty())
			})

			It("does not start a server when NUMA affinity is unknown (-1)", func() {
				numa.EXPECT().BridgeNUMANodes("br0").Return([]int{-1}).Once()

				mgr.UpdateResources(ctx, []ovsdpdkdrav1alpha1.BridgeSpec{
					bridgeSpec("br0", "example.com/topo-br0"),
				})

				Expect(createdSrvs).To(BeEmpty())
			})

			It("does not start a server when NUMA spans multiple nodes", func() {
				numa.EXPECT().BridgeNUMANodes("br0").Return([]int{0, 1}).Once()

				mgr.UpdateResources(ctx, []ovsdpdkdrav1alpha1.BridgeSpec{
					bridgeSpec("br0", "example.com/topo-br0"),
				})

				Expect(createdSrvs).To(BeEmpty())
			})

			It("stops a server when the bridge is removed from the list", func() {
				numa.EXPECT().BridgeNUMANodes("br0").Return([]int{0}).Once()
				newServerFunc = func(resourceName string, numaNode, _ int) TopologyDPServer {
					srv := NewMockTopologyDPServer(GinkgoT())
					srv.EXPECT().GetNUMA().Return(numaNode).Maybe()
					srv.EXPECT().start(mock.Anything).Return(nil).Once()
					srv.EXPECT().stop().Once()
					createdSrvs = append(createdSrvs, srv)
					return srv
				}
				mgr.UpdateResources(ctx, []ovsdpdkdrav1alpha1.BridgeSpec{
					bridgeSpec("br0", "example.com/topo-br0"),
				})

				mgr.UpdateResources(ctx, nil)

				mgr.mutex.Lock()
				defer mgr.mutex.Unlock()
				Expect(mgr.servers).NotTo(HaveKey("br0"))
			})

			It("does not restart a server when NUMA is unchanged", func() {
				numa.EXPECT().BridgeNUMANodes("br0").Return([]int{0}).Twice()
				newServerFunc = func(resourceName string, numaNode, _ int) TopologyDPServer {
					srv := NewMockTopologyDPServer(GinkgoT())
					srv.EXPECT().GetNUMA().Return(numaNode).Maybe()
					srv.EXPECT().start(mock.Anything).Return(nil).Once()
					// stop must NOT be called
					createdSrvs = append(createdSrvs, srv)
					return srv
				}

				mgr.UpdateResources(ctx, []ovsdpdkdrav1alpha1.BridgeSpec{
					bridgeSpec("br0", "example.com/topo-br0"),
				})
				mgr.UpdateResources(ctx, []ovsdpdkdrav1alpha1.BridgeSpec{
					bridgeSpec("br0", "example.com/topo-br0"),
				})

				Expect(createdSrvs).To(HaveLen(1))
			})

			It("recreates the server when the NUMA node changes", func() {
				numa.EXPECT().BridgeNUMANodes("br0").Return([]int{0}).Once()
				numa.EXPECT().BridgeNUMANodes("br0").Return([]int{1}).Once()

				callCount := 0
				newServerFunc = func(resourceName string, numaNode, _ int) TopologyDPServer {
					callCount++
					srv := NewMockTopologyDPServer(GinkgoT())
					srv.EXPECT().GetNUMA().Return(numaNode).Maybe()
					srv.EXPECT().start(mock.Anything).Return(nil).Once()
					if callCount == 1 {
						srv.EXPECT().stop().Once()
					}
					createdSrvs = append(createdSrvs, srv)
					return srv
				}

				mgr.UpdateResources(ctx, []ovsdpdkdrav1alpha1.BridgeSpec{
					bridgeSpec("br0", "example.com/topo-br0"),
				})
				mgr.UpdateResources(ctx, []ovsdpdkdrav1alpha1.BridgeSpec{
					bridgeSpec("br0", "example.com/topo-br0"),
				})

				Expect(createdSrvs).To(HaveLen(2))
				mgr.mutex.Lock()
				defer mgr.mutex.Unlock()
				Expect(mgr.servers["br0"].GetNUMA()).To(Equal(1))
			})

			It("stops the server when NUMA becomes invalid", func() {
				numa.EXPECT().BridgeNUMANodes("br0").Return([]int{0}).Once()
				numa.EXPECT().BridgeNUMANodes("br0").Return([]int{-1}).Once()
				newServerFunc = func(resourceName string, numaNode, _ int) TopologyDPServer {
					srv := NewMockTopologyDPServer(GinkgoT())
					srv.EXPECT().GetNUMA().Return(numaNode).Maybe()
					srv.EXPECT().start(mock.Anything).Return(nil).Once()
					srv.EXPECT().stop().Once()
					createdSrvs = append(createdSrvs, srv)
					return srv
				}

				mgr.UpdateResources(ctx, []ovsdpdkdrav1alpha1.BridgeSpec{
					bridgeSpec("br0", "example.com/topo-br0"),
				})
				mgr.UpdateResources(ctx, []ovsdpdkdrav1alpha1.BridgeSpec{
					bridgeSpec("br0", "example.com/topo-br0"),
				})

				mgr.mutex.Lock()
				defer mgr.mutex.Unlock()
				Expect(mgr.servers).NotTo(HaveKey("br0"))
			})

			It("handles start failure gracefully without storing the server", func() {
				numa.EXPECT().BridgeNUMANodes("br0").Return([]int{0}).Once()
				newServerFunc = func(resourceName string, numaNode, _ int) TopologyDPServer {
					srv := NewMockTopologyDPServer(GinkgoT())
					srv.EXPECT().GetNUMA().Return(numaNode).Maybe()
					srv.EXPECT().start(mock.Anything).Return(fmt.Errorf("kubelet not available")).Once()
					createdSrvs = append(createdSrvs, srv)
					return srv
				}

				mgr.UpdateResources(ctx, []ovsdpdkdrav1alpha1.BridgeSpec{
					bridgeSpec("br0", "example.com/topo-br0"),
				})

				mgr.mutex.Lock()
				defer mgr.mutex.Unlock()
				Expect(mgr.servers).NotTo(HaveKey("br0"))
			})
		})
	})

	Describe("OnInterfaceChange", func() {
		It("starts a server when NUMA becomes available for a tracked bridge", func() {
			// Register bridge in topology with no NUMA yet.
			numa.EXPECT().BridgeNUMANodes("br0").Return(nil).Once()
			mgr.UpdateResources(ctx, []ovsdpdkdrav1alpha1.BridgeSpec{
				bridgeSpec("br0", "example.com/topo-br0"),
			})
			Expect(createdSrvs).To(BeEmpty())

			// Interface appears — NUMA now available.
			numa.EXPECT().BridgeNUMANodes("br0").Return([]int{0}).Once()
			newServerFunc = func(resourceName string, numaNode, _ int) TopologyDPServer {
				srv := NewMockTopologyDPServer(GinkgoT())
				srv.EXPECT().GetNUMA().Return(numaNode).Maybe()
				srv.EXPECT().start(mock.Anything).Return(nil).Once()
				createdSrvs = append(createdSrvs, srv)
				return srv
			}
			mgr.OnInterfaceChange("br0")

			Expect(createdSrvs).To(HaveLen(1))
		})

		It("is a no-op for a bridge not in topology", func() {
			mgr.OnInterfaceChange("br-unknown")

			Expect(createdSrvs).To(BeEmpty())
		})

		It("stops the server when NUMA becomes invalid", func() {
			numa.EXPECT().BridgeNUMANodes("br0").Return([]int{0}).Once()
			newServerFunc = func(resourceName string, numaNode, _ int) TopologyDPServer {
				srv := NewMockTopologyDPServer(GinkgoT())
				srv.EXPECT().GetNUMA().Return(numaNode).Maybe()
				srv.EXPECT().start(mock.Anything).Return(nil).Once()
				srv.EXPECT().stop().Once()
				createdSrvs = append(createdSrvs, srv)
				return srv
			}
			mgr.UpdateResources(ctx, []ovsdpdkdrav1alpha1.BridgeSpec{
				bridgeSpec("br0", "example.com/topo-br0"),
			})

			numa.EXPECT().BridgeNUMANodes("br0").Return(nil).Once()
			mgr.OnInterfaceChange("br0")

			mgr.mutex.Lock()
			defer mgr.mutex.Unlock()
			Expect(mgr.servers).NotTo(HaveKey("br0"))
		})
	})

	Describe("StopAll", func() {
		It("stops all running servers", func() {
			numa.EXPECT().BridgeNUMANodes("br0").Return([]int{0}).Once()
			numa.EXPECT().BridgeNUMANodes("br1").Return([]int{1}).Once()
			newServerFunc = func(resourceName string, numaNode, _ int) TopologyDPServer {
				srv := NewMockTopologyDPServer(GinkgoT())
				srv.EXPECT().GetNUMA().Return(numaNode).Maybe()
				srv.EXPECT().start(mock.Anything).Return(nil).Once()
				srv.EXPECT().stop().Once()
				createdSrvs = append(createdSrvs, srv)
				return srv
			}
			mgr.UpdateResources(ctx, []ovsdpdkdrav1alpha1.BridgeSpec{
				bridgeSpec("br0", "example.com/topo-br0"),
				bridgeSpec("br1", "example.com/topo-br1"),
			})
			Expect(createdSrvs).To(HaveLen(2))

			mgr.StopAll()

			mgr.mutex.Lock()
			defer mgr.mutex.Unlock()
			Expect(mgr.servers).To(BeEmpty())
		})

		It("is a no-op when no servers are running", func() {
			mgr.StopAll()
			Expect(createdSrvs).To(BeEmpty())
		})
	})
})
