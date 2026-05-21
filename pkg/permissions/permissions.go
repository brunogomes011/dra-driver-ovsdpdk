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

package permissions

import (
	"context"
	"fmt"
	"os/exec"
	"slices"
	"strings"

	"golang.org/x/sys/unix"
	"k8s.io/klog/v2"

	ovsdpdkdrav1alpha1 "github.com/amorenoz/dra-driver-ovsdpdk/pkg/api/ovsdpdkdra/v1alpha1"
)

// Applier applies ownership, SELinux labels and POSIX ACLs to a directory
// using a UserResolver for name-to-ID translation.
type Applier struct {
	resolver *UserResolver
	log      klog.Logger
}

// NewApplier creates a new Applier backed by the given UserResolver.
func NewApplier(resolver *UserResolver) *Applier {
	return &Applier{
		resolver: resolver,
		log:      klog.Background().WithName("PermissionApplier"),
	}
}

// ApplyPermissions applies the permissions described in spec to dir. It is a
// no-op when spec is nil or all permission fields are unset. Errors are fatal:
// the caller is expected to clean up the directory on failure.
func (a *Applier) ApplyPermissions(ctx context.Context, dir string, spec *ovsdpdkdrav1alpha1.VhostUserSpec) error {
	logger := klog.FromContext(ctx).WithName("ApplyPermissions")

	if spec == nil || !hasCustomPermissions(spec) {
		return nil
	}

	if err := a.applyOwnership(logger, dir, spec); err != nil {
		return err
	}

	if err := a.applySELinuxLabel(logger, dir, spec.SelinuxLabel); err != nil {
		return err
	}

	if err := a.applyACLs(logger, dir, spec.ACLUsers); err != nil {
		return err
	}

	return nil
}

// hasCustomPermissions reports whether spec contains any permission directives.
func hasCustomPermissions(spec *ovsdpdkdrav1alpha1.VhostUserSpec) bool {
	return spec.User != nil || spec.Group != nil ||
		(spec.SelinuxLabel != nil && *spec.SelinuxLabel != "") ||
		len(spec.ACLUsers) > 0
}

func (a *Applier) applyOwnership(logger klog.Logger, dir string, spec *ovsdpdkdrav1alpha1.VhostUserSpec) error {
	uid, err := a.resolver.ResolveUID(spec.User)
	if err != nil {
		return fmt.Errorf("resolve user: %w", err)
	}

	gid, err := a.resolver.ResolveGID(spec.Group)
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

func (a *Applier) applySELinuxLabel(logger klog.Logger, dir string, label *string) error {
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
// user:role:type or user:role:type:level.
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

func (a *Applier) applyACLs(logger klog.Logger, dir string, aclUsers []string) error {
	if len(aclUsers) == 0 {
		return nil
	}

	setfaclPath, err := exec.LookPath("setfacl")
	if err != nil {
		logger.Info("setfacl not found, skipping ACL setup", "dir", dir, "users", aclUsers)
		return nil
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
