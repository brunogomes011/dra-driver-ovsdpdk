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

package socketfs_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/amorenoz/dra-driver-ovsdpdk/pkg/socketfs"
)

func TestSocketFS(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "SocketFS Suite")
}

var _ = Describe("SocketFS", func() {
	var (
		sfs  socketfs.SocketFS
		root string
	)

	BeforeEach(func() {
		var err error
		root, err = os.MkdirTemp("", "socketfs-test-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(os.RemoveAll, root)

		sfs = socketfs.New()
	})

	Describe("CreateSocketDir", func() {
		It("should create the directory", func() {
			path := filepath.Join(root, "sock")
			Expect(sfs.CreateSocketDir(path)).To(Succeed())
			Expect(path).To(BeADirectory())
		})

		It("should create nested directories", func() {
			path := filepath.Join(root, "a", "b", "c")
			Expect(sfs.CreateSocketDir(path)).To(Succeed())
			Expect(path).To(BeADirectory())
		})

		It("should set mode 0775 regardless of process umask", func() {
			old := syscall.Umask(0o077)
			DeferCleanup(func() { syscall.Umask(old) })

			path := filepath.Join(root, "sock")
			Expect(sfs.CreateSocketDir(path)).To(Succeed())

			info, err := os.Stat(path)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o775)))
		})

		It("should return an error when the path cannot be created", func() {
			// Make root read-only so MkdirAll fails.
			Expect(os.Chmod(root, 0o555)).To(Succeed())
			DeferCleanup(func() { _ = os.Chmod(root, 0o755) })

			path := filepath.Join(root, "sock")
			Expect(sfs.CreateSocketDir(path)).NotTo(Succeed())
		})
	})

	Describe("RemoveSocketDir", func() {
		It("should remove an existing directory", func() {
			path := filepath.Join(root, "sock")
			Expect(os.MkdirAll(path, 0o775)).To(Succeed())

			Expect(sfs.RemoveSocketDir(path)).To(Succeed())
			Expect(path).NotTo(BeADirectory())
		})

		It("should succeed when the directory does not exist", func() {
			path := filepath.Join(root, "nonexistent")
			Expect(sfs.RemoveSocketDir(path)).To(Succeed())
		})

		It("should return an error when removal fails", func() {
			path := filepath.Join(root, "sock")
			Expect(os.MkdirAll(path, 0o775)).To(Succeed())

			// Make root read-only so RemoveAll cannot remove the child.
			Expect(os.Chmod(root, 0o555)).To(Succeed())
			DeferCleanup(func() { _ = os.Chmod(root, 0o755) })

			Expect(sfs.RemoveSocketDir(path)).NotTo(Succeed())
		})
	})
})
