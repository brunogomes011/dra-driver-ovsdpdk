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
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/sys/unix"

	ovsdpdkdrav1alpha1 "github.com/amorenoz/dra-driver-ovsdpdk/pkg/api/ovsdpdkdra/v1alpha1"
	"github.com/amorenoz/dra-driver-ovsdpdk/pkg/socketfs"
)

func TestSocketFS(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "SocketFS Suite")
}

// statUID returns the UID of the given path from os.Stat.
func statUID(path string) int {
	GinkgoHelper()
	info, err := os.Stat(path)
	Expect(err).NotTo(HaveOccurred())
	stat, ok := info.Sys().(*syscall.Stat_t)
	Expect(ok).To(BeTrue())
	return int(stat.Uid)
}

// statGID returns the GID of the given path from os.Stat.
func statGID(path string) int {
	GinkgoHelper()
	info, err := os.Stat(path)
	Expect(err).NotTo(HaveOccurred())
	stat, ok := info.Sys().(*syscall.Stat_t)
	Expect(ok).To(BeTrue())
	return int(stat.Gid)
}

// currentUserIDs returns the current user and its parsed UID and GID.
func currentUserIDs() (u *user.User, uid, gid int) {
	GinkgoHelper()
	u, err := user.Current()
	Expect(err).NotTo(HaveOccurred())
	uid, err = strconv.Atoi(u.Uid)
	Expect(err).NotTo(HaveOccurred())
	gid, err = strconv.Atoi(u.Gid)
	Expect(err).NotTo(HaveOccurred())
	return u, uid, gid
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
			Expect(sfs.CreateSocketDir(context.Background(), path, nil)).To(Succeed())
			Expect(path).To(BeADirectory())
		})

		It("should create nested directories", func() {
			path := filepath.Join(root, "a", "b", "c")
			Expect(sfs.CreateSocketDir(context.Background(), path, nil)).To(Succeed())
			Expect(path).To(BeADirectory())
		})

		It("should set mode 0775 regardless of process umask", func() {
			old := syscall.Umask(0o077)
			DeferCleanup(func() { syscall.Umask(old) })

			path := filepath.Join(root, "sock")
			Expect(sfs.CreateSocketDir(context.Background(), path, nil)).To(Succeed())

			info, err := os.Stat(path)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o775)))
		})

		It("should return an error when the path cannot be created", func() {
			Expect(os.Chmod(root, 0o555)).To(Succeed())
			DeferCleanup(func() { _ = os.Chmod(root, 0o755) })

			path := filepath.Join(root, "sock")
			Expect(sfs.CreateSocketDir(context.Background(), path, nil)).NotTo(Succeed())
		})

		Context("ownership", func() {
			It("should chown to the current user and group when specified by name", func() {
				u, uid, gid := currentUserIDs()
				g, err := user.LookupGroupId(u.Gid)
				Expect(err).NotTo(HaveOccurred())

				spec := &ovsdpdkdrav1alpha1.VhostUserSpec{
					User:  ovsdpdkdrav1alpha1.NewUserGroupIDFromName(u.Username),
					Group: ovsdpdkdrav1alpha1.NewUserGroupIDFromName(g.Name),
				}
				path := filepath.Join(root, "sock")
				Expect(sfs.CreateSocketDir(context.Background(), path, spec)).To(Succeed())
				Expect(statUID(path)).To(Equal(uid))
				Expect(statGID(path)).To(Equal(gid))
			})

			It("should chown to the current user and group when specified numerically", func() {
				_, uid, gid := currentUserIDs()

				spec := &ovsdpdkdrav1alpha1.VhostUserSpec{
					User:  ovsdpdkdrav1alpha1.NewUserGroupIDFromID(uid),
					Group: ovsdpdkdrav1alpha1.NewUserGroupIDFromID(gid),
				}
				path := filepath.Join(root, "sock")
				Expect(sfs.CreateSocketDir(context.Background(), path, spec)).To(Succeed())
				Expect(statUID(path)).To(Equal(uid))
				Expect(statGID(path)).To(Equal(gid))
			})

			DescribeTable("should return an error and clean up for an unknown identity",
				func(makeSpec func(u *user.User, gid int) *ovsdpdkdrav1alpha1.VhostUserSpec, errSubstring string) {
					u, _, gid := currentUserIDs()
					path := filepath.Join(root, "sock")
					err := sfs.CreateSocketDir(context.Background(), path, makeSpec(u, gid))
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring(errSubstring))
					_, statErr := os.Stat(path)
					Expect(os.IsNotExist(statErr)).To(BeTrue())
				},
				Entry("unknown user name",
					func(u *user.User, gid int) *ovsdpdkdrav1alpha1.VhostUserSpec {
						return &ovsdpdkdrav1alpha1.VhostUserSpec{
							User:  ovsdpdkdrav1alpha1.NewUserGroupIDFromName("no-such-user-xyz"),
							Group: ovsdpdkdrav1alpha1.NewUserGroupIDFromID(gid),
						}
					},
					"resolve user",
				),
				Entry("unknown group name",
					func(u *user.User, gid int) *ovsdpdkdrav1alpha1.VhostUserSpec {
						return &ovsdpdkdrav1alpha1.VhostUserSpec{
							User:  ovsdpdkdrav1alpha1.NewUserGroupIDFromName(u.Username),
							Group: ovsdpdkdrav1alpha1.NewUserGroupIDFromName("no-such-group-xyz"),
						}
					},
					"resolve group",
				),
			)
		})

		Context("SELinux label", func() {
			DescribeTable("should return an error for a malformed SELinux label",
				func(label string) {
					_, uid, gid := currentUserIDs()
					spec := &ovsdpdkdrav1alpha1.VhostUserSpec{
						User:         ovsdpdkdrav1alpha1.NewUserGroupIDFromID(uid),
						Group:        ovsdpdkdrav1alpha1.NewUserGroupIDFromID(gid),
						SelinuxLabel: &label,
					}
					path := filepath.Join(root, "sock")
					Expect(sfs.CreateSocketDir(context.Background(), path, spec)).NotTo(Succeed())
				},
				Entry("single component", "bad-label"),
				Entry("empty component", "system_u::container_t:s0"),
			)

			It("should succeed with a valid 4-part label", func() {
				_, uid, gid := currentUserIDs()
				label := "system_u:object_r:tmp_t:s0"
				spec := &ovsdpdkdrav1alpha1.VhostUserSpec{
					User:         ovsdpdkdrav1alpha1.NewUserGroupIDFromID(uid),
					Group:        ovsdpdkdrav1alpha1.NewUserGroupIDFromID(gid),
					SelinuxLabel: &label,
				}
				// Skip if the process cannot set SELinux labels.
				if err := unix.Setxattr(root, "security.selinux", []byte(label), 0); err != nil {
					Skip(fmt.Sprintf("cannot set SELinux labels: %v", err))
				}
				path := filepath.Join(root, "sock")
				Expect(sfs.CreateSocketDir(context.Background(), path, spec)).To(Succeed())
			})
		})

		Context("ACLs", func() {
			BeforeEach(func() {
				if _, err := exec.LookPath("getfacl"); err != nil {
					Skip("acl utilities are not available on this system")
				}
			})

			It("should apply ACLs for a single user", func() {
				u, uid, gid := currentUserIDs()
				spec := &ovsdpdkdrav1alpha1.VhostUserSpec{
					User:     ovsdpdkdrav1alpha1.NewUserGroupIDFromID(uid),
					Group:    ovsdpdkdrav1alpha1.NewUserGroupIDFromID(gid),
					ACLUsers: []string{u.Username},
				}
				path := filepath.Join(root, "sock")
				Expect(sfs.CreateSocketDir(context.Background(), path, spec)).To(Succeed())

				out, err := exec.Command("getfacl", path).CombinedOutput()
				Expect(err).NotTo(HaveOccurred())
				Expect(string(out)).To(ContainSubstring("user:" + u.Username + ":rwx"))
			})

			It("should apply ACLs for multiple users in a single setfacl call", func() {
				u, uid, gid := currentUserIDs()
				spec := &ovsdpdkdrav1alpha1.VhostUserSpec{
					User:     ovsdpdkdrav1alpha1.NewUserGroupIDFromID(uid),
					Group:    ovsdpdkdrav1alpha1.NewUserGroupIDFromID(gid),
					ACLUsers: []string{u.Username, "root"},
				}
				path := filepath.Join(root, "sock")
				Expect(sfs.CreateSocketDir(context.Background(), path, spec)).To(Succeed())

				out, err := exec.Command("getfacl", path).CombinedOutput()
				Expect(err).NotTo(HaveOccurred())
				aclOutput := string(out)
				Expect(aclOutput).To(ContainSubstring("user:" + u.Username + ":rwx"))
				Expect(aclOutput).To(ContainSubstring("user:root:rwx"))
				Expect(aclOutput).To(ContainSubstring("default:user:" + u.Username + ":rwx"))
				Expect(aclOutput).To(ContainSubstring("default:user:root:rwx"))
			})
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

			Expect(os.Chmod(root, 0o555)).To(Succeed())
			DeferCleanup(func() { _ = os.Chmod(root, 0o755) })

			Expect(sfs.RemoveSocketDir(path)).NotTo(Succeed())
		})
	})
})
