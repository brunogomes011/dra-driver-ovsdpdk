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

// Package socketfs manages the lifecycle of socket directories on the host
// filesystem, including creation, permission application and removal.
package socketfs

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"

	"golang.org/x/sys/unix"
	"k8s.io/klog/v2"

	ovsdpdkdrav1alpha1 "github.com/amorenoz/dra-driver-ovsdpdk/pkg/api/ovsdpdkdra/v1alpha1"
)

// SocketFS manages creation and removal of socket directories.
type SocketFS interface {
	// CreateSocketDir creates the socket directory at path.
	CreateSocketDir(ctx context.Context, path string, spec *ovsdpdkdrav1alpha1.VhostUserSpec) error

	// RemoveSocketDir removes the socket directory at path.
	RemoveSocketDir(path string) error
}

type socketFS struct {
	resolver *userResolver
}

// New returns a SocketFS backed by real filesystem operations.
func New() SocketFS {
	return &socketFS{
		resolver: newUserResolver(),
	}
}

func (s *socketFS) CreateSocketDir(ctx context.Context, path string, spec *ovsdpdkdrav1alpha1.VhostUserSpec) error {
	if err := os.MkdirAll(path, 0o775); err != nil {
		return fmt.Errorf("create socket directory %q: %w", path, err)
	}

	// Re-apply chmod to bypass umask.
	if err := os.Chmod(path, 0o775); err != nil {
		_ = s.RemoveSocketDir(path)
		return fmt.Errorf("chmod socket directory %q: %w", path, err)
	}

	if err := s.applyPermissions(ctx, path, spec); err != nil {
		_ = s.RemoveSocketDir(path)
		return fmt.Errorf("apply permissions to %q: %w", path, err)
	}

	return nil
}

func (s *socketFS) RemoveSocketDir(path string) error {
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove socket directory %q: %w", path, err)
	}
	return nil
}

// applyPermissions applies ownership, SELinux label and ACLs from spec to dir.
func (s *socketFS) applyPermissions(ctx context.Context, dir string, spec *ovsdpdkdrav1alpha1.VhostUserSpec) error {
	if spec == nil {
		return nil
	}
	logger := klog.FromContext(ctx).WithName("applyPermissions")
	if err := s.applyOwnership(logger, dir, spec); err != nil {
		return err
	}
	if err := applySELinuxLabel(logger, dir, spec.SelinuxLabel); err != nil {
		return err
	}
	return applyACLs(logger, dir, spec.ACLUsers)
}

func (s *socketFS) applyOwnership(logger klog.Logger, dir string, spec *ovsdpdkdrav1alpha1.VhostUserSpec) error {
	uid, err := s.resolver.ResolveUID(&spec.User)
	if err != nil {
		return fmt.Errorf("resolve user: %w", err)
	}

	gid, err := s.resolver.ResolveGID(&spec.Group)
	if err != nil {
		return fmt.Errorf("resolve group: %w", err)
	}

	if uid == nil && gid == nil {
		return nil
	}

	// unix.Chown treats -1 as "don't change this ID".
	chownUID, chownGID := -1, -1
	if uid != nil {
		chownUID = *uid
	}
	if gid != nil {
		chownGID = *gid
	}

	if err := unix.Chown(dir, chownUID, chownGID); err != nil {
		return fmt.Errorf("chown %q (uid=%d gid=%d): %w", dir, chownUID, chownGID, err)
	}

	logger.V(1).Info("Applied ownership", "dir", dir, "uid", chownUID, "gid", chownGID)
	return nil
}

func applySELinuxLabel(logger klog.Logger, dir string, label *string) error {
	if label == nil || *label == "" {
		return nil
	}

	if err := validateSELinuxLabel(*label); err != nil {
		return err
	}

	err := unix.Setxattr(dir, "security.selinux", []byte(*label), 0)
	if err == unix.EOPNOTSUPP || err == unix.ENOTSUP {
		logger.V(1).Info("SELinux xattr not supported, skipping label", "dir", dir, "label", *label)
		return nil
	}
	if err != nil {
		return fmt.Errorf("set SELinux label %q on %q: %w", *label, dir, err)
	}

	logger.V(1).Info("Applied SELinux label", "dir", dir, "label", *label)
	return nil
}

// validateSELinuxLabel checks that label has the basic SELinux context format:
// user:role:type:level.
func validateSELinuxLabel(label string) error {
	parts := strings.Split(label, ":")
	if len(parts) != 4 {
		return fmt.Errorf("invalid SELinux label %q: expected user:role:type:level", label)
	}
	if slices.Contains(parts, "") {
		return fmt.Errorf("invalid SELinux label %q: empty component", label)
	}
	return nil
}

func applyACLs(logger klog.Logger, dir string, aclUsers []string) error {
	if len(aclUsers) == 0 {
		return nil
	}

	setfaclPath, err := exec.LookPath("setfacl")
	if err != nil {
		return fmt.Errorf("setfacl binary not found")
	}

	// Build the full ACL spec in a single setfacl call.
	specs := make([]string, 0, len(aclUsers))
	for _, username := range aclUsers {
		specs = append(specs, "u:"+username+":rwx")
	}
	aclSpec := strings.Join(specs, ",")

	// Apply access ACLs.
	if out, err := exec.Command(setfaclPath, "-m", aclSpec, dir).CombinedOutput(); err != nil {
		return fmt.Errorf("setfacl -m %s %q: %w: %s", aclSpec, dir, err, out)
	}

	// Apply default ACLs so new files/dirs inherit the entries.
	if out, err := exec.Command(setfaclPath, "-d", "-m", aclSpec, dir).CombinedOutput(); err != nil {
		return fmt.Errorf("setfacl -d -m %s %q: %w: %s", aclSpec, dir, err, out)
	}

	logger.V(1).Info("Applied ACLs", "dir", dir, "users", aclUsers)
	return nil
}
