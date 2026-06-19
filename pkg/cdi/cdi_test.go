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

package cdi_test

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
	cdispecs "tags.cncf.io/container-device-interface/specs-go"

	"github.com/amorenoz/dra-driver-ovsdpdk/pkg/cdi"
	dratypes "github.com/amorenoz/dra-driver-ovsdpdk/pkg/types"
)

func TestCDI(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CDI Suite")
}

// readSpec reads back the CDI spec file written under cdiRoot for the given claim UID.
func readSpec(cdiRoot string, claimUID k8stypes.UID) *cdispecs.Spec {
	GinkgoHelper()
	// GenerateTransientSpecName produces "{vendor}-{class}_{transientID}"
	specName := cdiapi.GenerateTransientSpecName(
		"ovsdpdk.k8snetworkplumbingwg.io", "vhost-user", string(claimUID))
	path := filepath.Join(cdiRoot, specName+".yaml")

	cache, err := cdiapi.NewCache(cdiapi.WithSpecDirs(cdiRoot))
	Expect(err).NotTo(HaveOccurred())

	spec, err := cdiapi.ReadSpec(path, 0)
	Expect(err).NotTo(HaveOccurred(), "reading CDI spec from %s", path)
	_ = cache
	return spec.Spec
}

var _ = Describe("CDI Handler", func() {
	var (
		cdiRoot string
		handler *cdi.Handler
	)

	BeforeEach(func() {
		var err error
		cdiRoot, err = os.MkdirTemp("", "cdi-test-*")
		Expect(err).NotTo(HaveOccurred())
		handler, err = cdi.New(cdiRoot)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		Expect(os.RemoveAll(cdiRoot)).To(Succeed())
	})

	Describe("CreateClaimSpecFile", func() {
		const (
			testClaimUID = k8stypes.UID("abcdef12-1111-2222-3333-444444444444")
			testBridge   = "br0"
		)
		var pd *dratypes.PreparedDevice

		BeforeEach(func() {
			pd = &dratypes.PreparedDevice{
				Device: kubeletplugin.Device{
					Requests:     []string{"req-0"},
					PoolName:     "pool-0",
					DeviceName:   testBridge,
					CDIDeviceIDs: []string{cdi.DeviceID(testClaimUID, testBridge, "req-0")},
				},
				ClaimNamespacedName: kubeletplugin.NamespacedObject{
					NamespacedName: k8stypes.NamespacedName{
						Name:      "my-claim",
						Namespace: "default",
					},
					UID: testClaimUID,
				},
				BridgeName: testBridge,
				Mount: dratypes.MountInfo{
					HostDir:      "/var/run/ovsdpdk/pod-uid_my-claim",
					ContainerDir: "/var/run/ovsdpdk/vhost-user/my-claim",
				},
				Socket: dratypes.SocketInfo{
					HostPath:      "/var/run/ovsdpdk/pod-uid_my-claim/vhost.sock",
					ContainerPath: "/var/run/ovsdpdk/vhost-user/my-claim/vhost.sock",
				},
			}
		})

		It("should write a spec file that can be read back", func() {
			Expect(handler.CreateClaimSpecFile([]*dratypes.PreparedDevice{pd})).To(Succeed())
			spec := readSpec(cdiRoot, testClaimUID)
			Expect(spec).NotTo(BeNil())
		})

		It("should write the correct CDI version", func() {
			Expect(handler.CreateClaimSpecFile([]*dratypes.PreparedDevice{pd})).To(Succeed())
			spec := readSpec(cdiRoot, testClaimUID)
			Expect(spec.Version).To(Equal("0.6.0"))
		})

		It("should write the correct CDI kind", func() {
			Expect(handler.CreateClaimSpecFile([]*dratypes.PreparedDevice{pd})).To(Succeed())
			spec := readSpec(cdiRoot, testClaimUID)
			Expect(spec.Kind).To(Equal("ovsdpdk.k8snetworkplumbingwg.io/vhost-user"))
		})

		It("should write exactly one device named after the claim UID and device name", func() {
			Expect(handler.CreateClaimSpecFile([]*dratypes.PreparedDevice{pd})).To(Succeed())
			spec := readSpec(cdiRoot, testClaimUID)
			Expect(spec.Devices).To(HaveLen(1))
			Expect(spec.Devices[0].Name).To(Equal(string(testClaimUID) + "-" + testBridge + "-req-0"))
		})

		It("should write exactly one bind mount per device", func() {
			Expect(handler.CreateClaimSpecFile([]*dratypes.PreparedDevice{pd})).To(Succeed())
			spec := readSpec(cdiRoot, testClaimUID)
			mounts := spec.Devices[0].ContainerEdits.Mounts
			Expect(mounts).To(HaveLen(1))
		})

		It("should bind-mount Mount.HostDir to Mount.ContainerDir", func() {
			Expect(handler.CreateClaimSpecFile([]*dratypes.PreparedDevice{pd})).To(Succeed())
			spec := readSpec(cdiRoot, testClaimUID)
			mount := spec.Devices[0].ContainerEdits.Mounts[0]
			Expect(mount.HostPath).To(Equal(pd.Mount.HostDir))
			Expect(mount.ContainerPath).To(Equal(pd.Mount.ContainerDir))
		})

		It("should set bind and rw mount options", func() {
			Expect(handler.CreateClaimSpecFile([]*dratypes.PreparedDevice{pd})).To(Succeed())
			spec := readSpec(cdiRoot, testClaimUID)
			opts := spec.Devices[0].ContainerEdits.Mounts[0].Options
			Expect(opts).To(ContainElements("bind", "rw"))
		})

		It("should write one device and mount per PreparedDevice when multiple are provided", func() {
			const testBridge2 = "br1"
			pd2 := &dratypes.PreparedDevice{
				Device: kubeletplugin.Device{
					Requests:     []string{"req-1"},
					PoolName:     "pool-0",
					DeviceName:   testBridge2,
					CDIDeviceIDs: []string{cdi.DeviceID(testClaimUID, testBridge2, "req-1")},
				},
				ClaimNamespacedName: pd.ClaimNamespacedName,
				BridgeName:          testBridge2,
				Mount: dratypes.MountInfo{
					HostDir:      "/var/run/ovsdpdk/pod-uid_my-claim-br1",
					ContainerDir: "/var/run/ovsdpdk/my-claim-br1",
				},
				Socket: dratypes.SocketInfo{
					HostPath:      "/var/run/ovsdpdk/pod-uid_my-claim-br1/vhost.sock",
					ContainerPath: "/var/run/ovsdpdk/my-claim-br1/vhost.sock",
				},
			}

			Expect(handler.CreateClaimSpecFile([]*dratypes.PreparedDevice{pd, pd2})).To(Succeed())

			spec := readSpec(cdiRoot, testClaimUID)
			Expect(spec.Devices).To(HaveLen(2))

			Expect(spec.Devices[0].Name).To(Equal(string(testClaimUID) + "-" + testBridge + "-req-0"))
			Expect(spec.Devices[0].ContainerEdits.Mounts[0].HostPath).To(Equal(pd.Mount.HostDir))
			Expect(spec.Devices[0].ContainerEdits.Mounts[0].ContainerPath).To(Equal(pd.Mount.ContainerDir))

			Expect(spec.Devices[1].Name).To(Equal(string(testClaimUID) + "-" + testBridge2 + "-req-1"))
			Expect(spec.Devices[1].ContainerEdits.Mounts[0].HostPath).To(Equal(pd2.Mount.HostDir))
			Expect(spec.Devices[1].ContainerEdits.Mounts[0].ContainerPath).To(Equal(pd2.Mount.ContainerDir))
		})

		It("should produce unique device names when multiple requests use the same bridge", func() {
			pd2 := &dratypes.PreparedDevice{
				Device: kubeletplugin.Device{
					Requests:     []string{"req-1"},
					PoolName:     "pool-0",
					DeviceName:   testBridge, // same bridge as pd
					CDIDeviceIDs: []string{cdi.DeviceID(testClaimUID, testBridge, "req-1")},
				},
				ClaimNamespacedName: pd.ClaimNamespacedName,
				BridgeName:          testBridge,
				Mount: dratypes.MountInfo{
					HostDir:      "/var/run/ovsdpdk/pod-uid_my-claim_req-1",
					ContainerDir: "/var/run/ovsdpdk/my-claim/req-1",
				},
				Socket: dratypes.SocketInfo{
					HostPath:      "/var/run/ovsdpdk/pod-uid_my-claim_req-1/vhost.sock",
					ContainerPath: "/var/run/ovsdpdk/my-claim/req-1/vhost.sock",
				},
			}

			Expect(handler.CreateClaimSpecFile([]*dratypes.PreparedDevice{pd, pd2})).To(Succeed())

			spec := readSpec(cdiRoot, testClaimUID)
			Expect(spec.Devices).To(HaveLen(2))

			Expect(spec.Devices[0].Name).To(Equal(string(testClaimUID) + "-" + testBridge + "-req-0"))
			Expect(spec.Devices[1].Name).To(Equal(string(testClaimUID) + "-" + testBridge + "-req-1"))
			Expect(spec.Devices[0].Name).NotTo(Equal(spec.Devices[1].Name))

			Expect(spec.Devices[0].ContainerEdits.Mounts[0].HostPath).To(Equal(pd.Mount.HostDir))
			Expect(spec.Devices[1].ContainerEdits.Mounts[0].HostPath).To(Equal(pd2.Mount.HostDir))
		})

		It("should overwrite an existing spec file on a second call", func() {
			Expect(handler.CreateClaimSpecFile([]*dratypes.PreparedDevice{pd})).To(Succeed())

			pd2 := *pd
			pd2.Mount.HostDir = "/new/socket/dir"
			pd2.Mount.ContainerDir = "/new/container/path"
			Expect(handler.CreateClaimSpecFile([]*dratypes.PreparedDevice{&pd2})).To(Succeed())

			spec := readSpec(cdiRoot, testClaimUID)
			mount := spec.Devices[0].ContainerEdits.Mounts[0]
			Expect(mount.HostPath).To(Equal("/new/socket/dir"))
			Expect(mount.ContainerPath).To(Equal("/new/container/path"))
		})

		It("should return an error when the CDI root does not exist", func() {
			h, err := cdi.New("/nonexistent/cdi/root")
			Expect(err).NotTo(HaveOccurred())
			err = h.CreateClaimSpecFile([]*dratypes.PreparedDevice{pd})
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("DeleteClaimSpecFile", func() {
		const (
			delClaimUID = k8stypes.UID("deadbeef-1111-2222-3333-444444444444")
			delBridge   = "br0"
		)
		var pd *dratypes.PreparedDevice

		BeforeEach(func() {
			pd = &dratypes.PreparedDevice{
				Device: kubeletplugin.Device{
					Requests:     []string{"req-0"},
					PoolName:     "pool-0",
					DeviceName:   delBridge,
					CDIDeviceIDs: []string{cdi.DeviceID(delClaimUID, delBridge, "req-0")},
				},
				ClaimNamespacedName: kubeletplugin.NamespacedObject{
					NamespacedName: k8stypes.NamespacedName{
						Name:      "del-claim",
						Namespace: "default",
					},
					UID: delClaimUID,
				},
				Mount: dratypes.MountInfo{
					HostDir:      "/var/run/ovsdpdk/pod_del-claim",
					ContainerDir: "/var/run/ovsdpdk/vhost-user/del-claim",
				},
			}
			Expect(handler.CreateClaimSpecFile([]*dratypes.PreparedDevice{pd})).To(Succeed())
		})

		It("should remove the spec file from disk", func() {
			Expect(handler.DeleteClaimSpecFile(delClaimUID)).To(Succeed())

			specName := cdiapi.GenerateTransientSpecName(
				"ovsdpdk.k8snetworkplumbingwg.io", "vhost-user", string(delClaimUID))
			_, err := os.Stat(filepath.Join(cdiRoot, specName+".yaml"))
			Expect(os.IsNotExist(err)).To(BeTrue())
		})

		It("should succeed when the spec file does not exist (idempotent)", func() {
			Expect(handler.DeleteClaimSpecFile(delClaimUID)).To(Succeed())
			// second delete — file already gone
			Expect(handler.DeleteClaimSpecFile(delClaimUID)).To(Succeed())
		})

		It("should succeed when the CDI root does not exist (file simply not found)", func() {
			h, err := cdi.New("/nonexistent/cdi/root")
			Expect(err).NotTo(HaveOccurred())
			// RemoveSpec silences ErrNotExist, so this is idempotent.
			Expect(h.DeleteClaimSpecFile(delClaimUID)).To(Succeed())
		})
	})
})
