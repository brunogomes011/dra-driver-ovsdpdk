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

package permissions_test

import (
	"context"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"syscall"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ovsdpdkdrav1alpha1 "github.com/amorenoz/dra-driver-ovsdpdk/pkg/api/ovsdpdkdra/v1alpha1"
	"github.com/amorenoz/dra-driver-ovsdpdk/pkg/permissions"
)

// newApplier creates a fresh Applier backed by a new UserResolver.
func newApplier() *permissions.Applier {
	return permissions.NewApplier(permissions.NewUserResolver())
}

// tempDir creates a temporary directory and registers cleanup.
func tempDir() string {
	GinkgoHelper()
	dir, err := os.MkdirTemp("", "perm-test-*")
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(os.RemoveAll, dir)
	return dir
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

var _ = Describe("Applier", func() {
	var (
		applier *permissions.Applier
		ctx     context.Context
	)

	BeforeEach(func() {
		applier = newApplier()
		ctx = context.Background()
	})

	Describe("ApplyPermissions", func() {
		It("should be a no-op for a nil spec", func() {
			dir := tempDir()
			Expect(applier.ApplyPermissions(ctx, dir, nil)).To(Succeed())
		})

		It("should be a no-op when no permission fields are set", func() {
			dir := tempDir()
			spec := &ovsdpdkdrav1alpha1.VhostUserSpec{
				HostRootPath:      "/var/run/ovsdpdk",
				ContainerRootPath: "/var/run/ovsdpdk/vhost-user",
			}
			Expect(applier.ApplyPermissions(ctx, dir, spec)).To(Succeed())
		})

		Context("ownership", func() {
			It("should chown to the current user's UID when specified by name", func() {
				u, err := user.Current()
				Expect(err).NotTo(HaveOccurred())
				expectedUID, err := strconv.Atoi(u.Uid)
				Expect(err).NotTo(HaveOccurred())

				dir := tempDir()
				userID := ovsdpdkdrav1alpha1.NewUserGroupIDFromName(u.Username)
				spec := &ovsdpdkdrav1alpha1.VhostUserSpec{User: &userID}

				Expect(applier.ApplyPermissions(ctx, dir, spec)).To(Succeed())
				Expect(statUID(dir)).To(Equal(expectedUID))
			})

			It("should chown to the current user's UID when specified numerically", func() {
				u, err := user.Current()
				Expect(err).NotTo(HaveOccurred())
				expectedUID, err := strconv.Atoi(u.Uid)
				Expect(err).NotTo(HaveOccurred())

				dir := tempDir()
				userID := ovsdpdkdrav1alpha1.NewUserGroupIDFromID(expectedUID)
				spec := &ovsdpdkdrav1alpha1.VhostUserSpec{User: &userID}

				Expect(applier.ApplyPermissions(ctx, dir, spec)).To(Succeed())
				Expect(statUID(dir)).To(Equal(expectedUID))
			})

			It("should chown to the current group's GID when specified by name", func() {
				u, err := user.Current()
				Expect(err).NotTo(HaveOccurred())
				g, err := user.LookupGroupId(u.Gid)
				Expect(err).NotTo(HaveOccurred())
				expectedGID, err := strconv.Atoi(g.Gid)
				Expect(err).NotTo(HaveOccurred())

				dir := tempDir()
				groupID := ovsdpdkdrav1alpha1.NewUserGroupIDFromName(g.Name)
				spec := &ovsdpdkdrav1alpha1.VhostUserSpec{Group: &groupID}

				Expect(applier.ApplyPermissions(ctx, dir, spec)).To(Succeed())
				Expect(statGID(dir)).To(Equal(expectedGID))
			})

			It("should return an error for an unknown user name", func() {
				dir := tempDir()
				userID := ovsdpdkdrav1alpha1.NewUserGroupIDFromName("no-such-user-xyz")
				spec := &ovsdpdkdrav1alpha1.VhostUserSpec{User: &userID}

				err := applier.ApplyPermissions(ctx, dir, spec)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("resolve user"))
			})

			It("should return an error for an unknown group name", func() {
				dir := tempDir()
				groupID := ovsdpdkdrav1alpha1.NewUserGroupIDFromName("no-such-group-xyz")
				spec := &ovsdpdkdrav1alpha1.VhostUserSpec{Group: &groupID}

				err := applier.ApplyPermissions(ctx, dir, spec)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("resolve group"))
			})
		})

		Context("SELinux label", func() {
			It("should return an error for a malformed SELinux label", func() {
				dir := tempDir()
				label := "bad-label"
				spec := &ovsdpdkdrav1alpha1.VhostUserSpec{SelinuxLabel: &label}

				err := applier.ApplyPermissions(ctx, dir, spec)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid SELinux label"))
			})

			It("should return an error for a label with an empty component", func() {
				dir := tempDir()
				label := "system_u::container_t:s0"
				spec := &ovsdpdkdrav1alpha1.VhostUserSpec{SelinuxLabel: &label}

				err := applier.ApplyPermissions(ctx, dir, spec)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid SELinux label"))
			})

			It("should succeed with a valid 3-part label", func() {
				dir := tempDir()
				label := "system_u:object_r:container_file_t"
				spec := &ovsdpdkdrav1alpha1.VhostUserSpec{SelinuxLabel: &label}

				Expect(applier.ApplyPermissions(ctx, dir, spec)).To(Succeed())
			})

			It("should succeed with a valid 4-part label", func() {
				dir := tempDir()
				label := "system_u:object_r:container_file_t:s0"
				spec := &ovsdpdkdrav1alpha1.VhostUserSpec{SelinuxLabel: &label}

				Expect(applier.ApplyPermissions(ctx, dir, spec)).To(Succeed())
			})
		})

		Context("ACLs", func() {
			It("should succeed and skip ACLs when setfacl is not available", func() {
				if _, err := exec.LookPath("setfacl"); err == nil {
					Skip("setfacl is available on this system")
				}
				dir := tempDir()
				spec := &ovsdpdkdrav1alpha1.VhostUserSpec{ACLUsers: []string{"root"}}

				Expect(applier.ApplyPermissions(ctx, dir, spec)).To(Succeed())
			})

			It("should apply ACLs when setfacl is available", func() {
				if _, err := exec.LookPath("setfacl"); err != nil {
					Skip("setfacl is not available on this system")
				}
				getfaclPath, err := exec.LookPath("getfacl")
				if err != nil {
					Skip("getfacl is not available on this system")
				}

				u, err := user.Current()
				Expect(err).NotTo(HaveOccurred())

				dir := tempDir()
				spec := &ovsdpdkdrav1alpha1.VhostUserSpec{ACLUsers: []string{u.Username}}

				Expect(applier.ApplyPermissions(ctx, dir, spec)).To(Succeed())

				out, err := exec.Command(getfaclPath, dir).CombinedOutput()
				Expect(err).NotTo(HaveOccurred())
				Expect(string(out)).To(ContainSubstring("user:" + u.Username + ":rwx"))
			})

			It("should apply ACLs for multiple users in a single setfacl call", func() {
				if _, err := exec.LookPath("setfacl"); err != nil {
					Skip("setfacl is not available on this system")
				}
				getfaclPath, err := exec.LookPath("getfacl")
				if err != nil {
					Skip("getfacl is not available on this system")
				}

				u, err := user.Current()
				Expect(err).NotTo(HaveOccurred())

				dir := tempDir()
				// Use the current user twice under different names is not possible,
				// so use the current user and root — both must appear in the ACL.
				spec := &ovsdpdkdrav1alpha1.VhostUserSpec{ACLUsers: []string{u.Username, "root"}}

				Expect(applier.ApplyPermissions(ctx, dir, spec)).To(Succeed())

				out, err := exec.Command(getfaclPath, dir).CombinedOutput()
				Expect(err).NotTo(HaveOccurred())
				aclOutput := string(out)
				Expect(aclOutput).To(ContainSubstring("user:" + u.Username + ":rwx"))
				Expect(aclOutput).To(ContainSubstring("user:root:rwx"))
				// Default ACLs must also carry both entries.
				Expect(aclOutput).To(ContainSubstring("default:user:" + u.Username + ":rwx"))
				Expect(aclOutput).To(ContainSubstring("default:user:root:rwx"))
			})
		})
	})
})
