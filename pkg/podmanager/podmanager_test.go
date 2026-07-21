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

package podmanager_test

import (
	"sync"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"

	"github.com/amorenoz/dra-driver-ovsdpdk/pkg/podmanager"
	dratypes "github.com/amorenoz/dra-driver-ovsdpdk/pkg/types"
)

func TestPodManager(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "PodManager Suite")
}

var _ = Describe("PodManager", func() {
	var pm *podmanager.PodManager

	BeforeEach(func() {
		pm = podmanager.New()
	})

	Describe("Get", func() {
		It("should return false for an unknown claim UID", func() {
			_, found := pm.Get("unknown-uid")
			Expect(found).To(BeFalse())
		})

		It("should return the stored PreparedDevice and true for a known claim UID", func() {
			uid := k8stypes.UID("uid-1")
			pd := makePD(uid, "claim-1")
			pm.Set(uid, pd)

			got, found := pm.Get(uid)
			Expect(found).To(BeTrue())
			Expect(got).To(Equal(pd))
		})

		It("should not remove the entry on Get", func() {
			uid := k8stypes.UID("uid-2")
			pd := makePD(uid, "claim-2")
			pm.Set(uid, pd)

			pm.Get(uid)
			_, found := pm.Get(uid)
			Expect(found).To(BeTrue())
		})
	})

	Describe("Set", func() {
		It("should overwrite an existing entry", func() {
			uid := k8stypes.UID("uid-3")
			pd1 := makePD(uid, "first")
			pd2 := makePD(uid, "second")

			pm.Set(uid, pd1)
			pm.Set(uid, pd2)

			got, found := pm.Get(uid)
			Expect(found).To(BeTrue())
			Expect(got.ClaimNamespacedName.Name).To(Equal("second"))
		})

		It("should store independent entries for different UIDs", func() {
			uid1 := k8stypes.UID("uid-a")
			uid2 := k8stypes.UID("uid-b")
			pd1 := makePD(uid1, "claim-a")
			pd2 := makePD(uid2, "claim-b")

			pm.Set(uid1, pd1)
			pm.Set(uid2, pd2)

			got1, _ := pm.Get(uid1)
			got2, _ := pm.Get(uid2)
			Expect(got1).To(Equal(pd1))
			Expect(got2).To(Equal(pd2))
		})
	})

	Describe("Delete", func() {
		It("should return nil for an unknown claim UID", func() {
			Expect(pm.Delete("nonexistent")).To(BeNil())
		})

		It("should return the PreparedDevice and remove it from the cache", func() {
			uid := k8stypes.UID("uid-4")
			pd := makePD(uid, "to-delete")
			pm.Set(uid, pd)

			got := pm.Delete(uid)
			Expect(got).To(Equal(pd))

			_, found := pm.Get(uid)
			Expect(found).To(BeFalse())
		})

		It("should return nil on a second delete of the same UID", func() {
			uid := k8stypes.UID("uid-5")
			pm.Set(uid, makePD(uid, "claim-5"))
			pm.Delete(uid)
			Expect(pm.Delete(uid)).To(BeNil())
		})
	})

	Describe("thread safety", func() {
		It("should handle concurrent Set and Get without data races", func() {
			const goroutines = 50
			var wg sync.WaitGroup
			wg.Add(goroutines * 2)

			for i := range goroutines {
				uid := k8stypes.UID(k8stypes.UID("uid-concurrent-" + string(rune('A'+i))))
				pd := makePD(uid, "claim-concurrent")

				go func() {
					defer wg.Done()
					pm.Set(uid, pd)
				}()
				go func() {
					defer wg.Done()
					pm.Get(uid)
				}()
			}
			wg.Wait()
		})

		It("should handle concurrent Set and Delete without data races", func() {
			const goroutines = 50
			var wg sync.WaitGroup
			wg.Add(goroutines * 2)

			for i := range goroutines {
				uid := k8stypes.UID("uid-del-" + string(rune('A'+i)))
				pd := makePD(uid, "claim-del")
				pm.Set(uid, pd)

				go func() {
					defer wg.Done()
					pm.Set(uid, pd)
				}()
				go func() {
					defer wg.Done()
					pm.Delete(uid)
				}()
			}
			wg.Wait()
		})
	})
})

// makePD builds a minimal PreparedDevice for testing the pod manager cache.
func makePD(uid k8stypes.UID, name string) *dratypes.PreparedDevice {
	return &dratypes.PreparedDevice{
		ClaimNamespacedName: kubeletplugin.NamespacedObject{
			NamespacedName: k8stypes.NamespacedName{
				Name:      name,
				Namespace: "default",
			},
			UID: uid,
		},
	}
}
