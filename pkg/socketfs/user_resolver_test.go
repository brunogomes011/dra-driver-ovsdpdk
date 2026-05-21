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

package socketfs

import (
	"os/user"
	"strconv"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	ovsdpdkdrav1alpha1 "github.com/amorenoz/dra-driver-ovsdpdk/pkg/api/ovsdpdkdra/v1alpha1"
)

// currentUser returns the os/user entry for the process owner.
func currentUser() *user.User {
	GinkgoHelper()
	u, err := user.Current()
	Expect(err).NotTo(HaveOccurred())
	return u
}

// currentGroup returns the primary group of the process owner.
func currentGroup() *user.Group {
	GinkgoHelper()
	u := currentUser()
	g, err := user.LookupGroupId(u.Gid)
	Expect(err).NotTo(HaveOccurred())
	return g
}

var _ = Describe("userResolver", func() {
	var resolver *userResolver

	BeforeEach(func() {
		resolver = newUserResolver()
	})

	Describe("ResolveUID", func() {
		It("should return nil for a nil UserGroupID", func() {
			uid, err := resolver.ResolveUID(nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(uid).To(BeNil())
		})

		It("should return the numeric value directly for a numeric UserGroupID", func() {
			ugid := ovsdpdkdrav1alpha1.NewUserGroupIDFromID(42)
			uid, err := resolver.ResolveUID(&ugid)
			Expect(err).NotTo(HaveOccurred())
			Expect(uid).NotTo(BeNil())
			Expect(*uid).To(Equal(42))
		})

		It("should resolve the current process user name to its UID", func() {
			u := currentUser()
			expectedUID, err := strconv.Atoi(u.Uid)
			Expect(err).NotTo(HaveOccurred())

			ugid := ovsdpdkdrav1alpha1.NewUserGroupIDFromName(u.Username)
			uid, err := resolver.ResolveUID(&ugid)
			Expect(err).NotTo(HaveOccurred())
			Expect(uid).NotTo(BeNil())
			Expect(*uid).To(Equal(expectedUID))
		})

		It("should return an error for an unknown user name", func() {
			ugid := ovsdpdkdrav1alpha1.NewUserGroupIDFromName("this-user-does-not-exist-xyz")
			_, err := resolver.ResolveUID(&ugid)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("lookup user"))
		})

		It("should return the cached value on a second lookup of the same name", func() {
			u := currentUser()
			ugid := ovsdpdkdrav1alpha1.NewUserGroupIDFromName(u.Username)

			uid1, err := resolver.ResolveUID(&ugid)
			Expect(err).NotTo(HaveOccurred())

			uid2, err := resolver.ResolveUID(&ugid)
			Expect(err).NotTo(HaveOccurred())

			Expect(*uid1).To(Equal(*uid2))
		})
	})

	Describe("ResolveGID", func() {
		It("should return nil for a nil UserGroupID", func() {
			gid, err := resolver.ResolveGID(nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(gid).To(BeNil())
		})

		It("should return the numeric value directly for a numeric UserGroupID", func() {
			ugid := ovsdpdkdrav1alpha1.NewUserGroupIDFromID(99)
			gid, err := resolver.ResolveGID(&ugid)
			Expect(err).NotTo(HaveOccurred())
			Expect(gid).NotTo(BeNil())
			Expect(*gid).To(Equal(99))
		})

		It("should resolve the current process primary group name to its GID", func() {
			g := currentGroup()
			expectedGID, err := strconv.Atoi(g.Gid)
			Expect(err).NotTo(HaveOccurred())

			ugid := ovsdpdkdrav1alpha1.NewUserGroupIDFromName(g.Name)
			gid, err := resolver.ResolveGID(&ugid)
			Expect(err).NotTo(HaveOccurred())
			Expect(gid).NotTo(BeNil())
			Expect(*gid).To(Equal(expectedGID))
		})

		It("should return an error for an unknown group name", func() {
			ugid := ovsdpdkdrav1alpha1.NewUserGroupIDFromName("this-group-does-not-exist-xyz")
			_, err := resolver.ResolveGID(&ugid)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("lookup group"))
		})

		It("should return the cached value on a second lookup of the same name", func() {
			g := currentGroup()
			ugid := ovsdpdkdrav1alpha1.NewUserGroupIDFromName(g.Name)

			gid1, err := resolver.ResolveGID(&ugid)
			Expect(err).NotTo(HaveOccurred())

			gid2, err := resolver.ResolveGID(&ugid)
			Expect(err).NotTo(HaveOccurred())

			Expect(*gid1).To(Equal(*gid2))
		})
	})

	Describe("thread safety", func() {
		It("should handle concurrent UID resolutions without data races", func() {
			u := currentUser()
			ugid := ovsdpdkdrav1alpha1.NewUserGroupIDFromName(u.Username)

			const goroutines = 50
			var wg sync.WaitGroup
			wg.Add(goroutines)
			for range goroutines {
				go func() {
					defer wg.Done()
					_, _ = resolver.ResolveUID(&ugid)
				}()
			}
			wg.Wait()
		})

		It("should handle concurrent GID resolutions without data races", func() {
			g := currentGroup()
			ugid := ovsdpdkdrav1alpha1.NewUserGroupIDFromName(g.Name)

			const goroutines = 50
			var wg sync.WaitGroup
			wg.Add(goroutines)
			for range goroutines {
				go func() {
					defer wg.Done()
					_, _ = resolver.ResolveGID(&ugid)
				}()
			}
			wg.Wait()
		})
	})
})
