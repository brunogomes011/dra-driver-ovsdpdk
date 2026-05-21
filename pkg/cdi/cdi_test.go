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
	// The cache writes to the highest-priority dir; with a single dir that is cdiRoot.
	// specName mirrors the unexported helper: "{vendor}-{class}-{shortUID}.yaml"
	shortUID := string(claimUID)
	if len(shortUID) > 8 {
		shortUID = shortUID[:8]
	}
	name := "ovsdpdk.k8snetworkplumbingwg.io-vhost-user-" + shortUID + ".yaml"
	path := filepath.Join(cdiRoot, name)

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
		handler = cdi.New(cdiRoot)
	})

	AfterEach(func() {
		Expect(os.RemoveAll(cdiRoot)).To(Succeed())
	})

	Describe("DeviceID", func() {
		It("should return a qualified CDI device ID with the first 8 chars of the UID", func() {
			uid := k8stypes.UID("abcdef12-0000-0000-0000-000000000000")
			id := cdi.DeviceID(uid)
			Expect(id).To(Equal("ovsdpdk.k8snetworkplumbingwg.io/vhost-user=abcdef12"))
		})

		It("should use the full UID when it is 8 chars or shorter", func() {
			uid := k8stypes.UID("short")
			id := cdi.DeviceID(uid)
			Expect(id).To(Equal("ovsdpdk.k8snetworkplumbingwg.io/vhost-user=short"))
		})
	})

	Describe("CreateClaimSpecFile", func() {
		var pd *dratypes.PreparedDevice

		BeforeEach(func() {
			pd = &dratypes.PreparedDevice{
				ClaimUID:   "abcdef12-1111-2222-3333-444444444444",
				ClaimName:  "my-claim",
				BridgeName: "br0",
				Mount: dratypes.MountInfo{
					HostDir:      "/var/run/ovsdpdk/pod-uid_my-claim",
					ContainerDir: "/var/run/ovsdpdk/vhost-user/my-claim",
				},
				Socket: dratypes.SocketInfo{
					HostPath:      "/var/run/ovsdpdk/pod-uid_my-claim/vhost.sock",
					ContainerPath: "/var/run/ovsdpdk/vhost-user/my-claim/vhost.sock",
				},
				CDIDeviceID: cdi.DeviceID("abcdef12-1111-2222-3333-444444444444"),
			}
		})

		It("should write a spec file that can be read back", func() {
			Expect(handler.CreateClaimSpecFile(pd)).To(Succeed())
			spec := readSpec(cdiRoot, pd.ClaimUID)
			Expect(spec).NotTo(BeNil())
		})

		It("should write the correct CDI version", func() {
			Expect(handler.CreateClaimSpecFile(pd)).To(Succeed())
			spec := readSpec(cdiRoot, pd.ClaimUID)
			Expect(spec.Version).To(Equal("0.6.0"))
		})

		It("should write the correct CDI kind", func() {
			Expect(handler.CreateClaimSpecFile(pd)).To(Succeed())
			spec := readSpec(cdiRoot, pd.ClaimUID)
			Expect(spec.Kind).To(Equal("ovsdpdk.k8snetworkplumbingwg.io/vhost-user"))
		})

		It("should write exactly one device named after the short claim UID", func() {
			Expect(handler.CreateClaimSpecFile(pd)).To(Succeed())
			spec := readSpec(cdiRoot, pd.ClaimUID)
			Expect(spec.Devices).To(HaveLen(1))
			Expect(spec.Devices[0].Name).To(Equal("abcdef12"))
		})

		It("should write exactly one bind mount per device", func() {
			Expect(handler.CreateClaimSpecFile(pd)).To(Succeed())
			spec := readSpec(cdiRoot, pd.ClaimUID)
			mounts := spec.Devices[0].ContainerEdits.Mounts
			Expect(mounts).To(HaveLen(1))
		})

		It("should bind-mount Mount.HostDir to Mount.ContainerDir", func() {
			Expect(handler.CreateClaimSpecFile(pd)).To(Succeed())
			spec := readSpec(cdiRoot, pd.ClaimUID)
			mount := spec.Devices[0].ContainerEdits.Mounts[0]
			Expect(mount.HostPath).To(Equal(pd.Mount.HostDir))
			Expect(mount.ContainerPath).To(Equal(pd.Mount.ContainerDir))
		})

		It("should set bind and rw mount options", func() {
			Expect(handler.CreateClaimSpecFile(pd)).To(Succeed())
			spec := readSpec(cdiRoot, pd.ClaimUID)
			opts := spec.Devices[0].ContainerEdits.Mounts[0].Options
			Expect(opts).To(ContainElements("bind", "rw"))
		})

		It("should overwrite an existing spec file on a second call", func() {
			Expect(handler.CreateClaimSpecFile(pd)).To(Succeed())

			pd2 := *pd
			pd2.Mount.HostDir = "/new/socket/dir"
			pd2.Mount.ContainerDir = "/new/container/path"
			Expect(handler.CreateClaimSpecFile(&pd2)).To(Succeed())

			spec := readSpec(cdiRoot, pd.ClaimUID)
			mount := spec.Devices[0].ContainerEdits.Mounts[0]
			Expect(mount.HostPath).To(Equal("/new/socket/dir"))
			Expect(mount.ContainerPath).To(Equal("/new/container/path"))
		})

		It("should return an error when the CDI root does not exist", func() {
			h := cdi.New("/nonexistent/cdi/root")
			err := h.CreateClaimSpecFile(pd)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("DeleteClaimSpecFile", func() {
		var pd *dratypes.PreparedDevice

		BeforeEach(func() {
			pd = &dratypes.PreparedDevice{
				ClaimUID:  "deadbeef-1111-2222-3333-444444444444",
				ClaimName: "del-claim",
				Mount: dratypes.MountInfo{
					HostDir:      "/var/run/ovsdpdk/pod_del-claim",
					ContainerDir: "/var/run/ovsdpdk/vhost-user/del-claim",
				},
				CDIDeviceID: cdi.DeviceID("deadbeef-1111-2222-3333-444444444444"),
			}
			Expect(handler.CreateClaimSpecFile(pd)).To(Succeed())
		})

		It("should remove the spec file from disk", func() {
			Expect(handler.DeleteClaimSpecFile(pd.ClaimUID)).To(Succeed())

			shortUID := string(pd.ClaimUID)[:8]
			name := "ovsdpdk.k8snetworkplumbingwg.io-vhost-user-" + shortUID + ".yaml"
			_, err := os.Stat(filepath.Join(cdiRoot, name))
			Expect(os.IsNotExist(err)).To(BeTrue())
		})

		It("should succeed when the spec file does not exist (idempotent)", func() {
			Expect(handler.DeleteClaimSpecFile(pd.ClaimUID)).To(Succeed())
			// second delete — file already gone
			Expect(handler.DeleteClaimSpecFile(pd.ClaimUID)).To(Succeed())
		})

		It("should succeed when the CDI root does not exist (file simply not found)", func() {
			h := cdi.New("/nonexistent/cdi/root")
			// RemoveSpec silences ErrNotExist, so this is idempotent.
			Expect(h.DeleteClaimSpecFile(pd.ClaimUID)).To(Succeed())
		})
	})
})
