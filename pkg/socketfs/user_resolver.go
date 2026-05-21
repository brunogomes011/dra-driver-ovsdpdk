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
	"fmt"
	"os/user"
	"strconv"
	"sync"

	ovsdpdkdrav1alpha1 "github.com/amorenoz/dra-driver-ovsdpdk/pkg/api/ovsdpdkdra/v1alpha1"
)

// userResolver resolves user and group names to numeric IDs using the standard
// os/user package (which honours NSS). Resolved values are cached indefinitely
// for the lifetime of the resolver.
type userResolver struct {
	mu         sync.RWMutex
	userCache  map[string]int
	groupCache map[string]int
}

// newUserResolver creates a new userResolver with empty caches.
func newUserResolver() *userResolver {
	return &userResolver{
		userCache:  make(map[string]int),
		groupCache: make(map[string]int),
	}
}

// ResolveUID returns the numeric UID for the given UserGroupID, or nil if ugid
// is nil. Numeric IDs are returned as-is; string names are looked up via
// os/user and the result is cached.
func (r *userResolver) ResolveUID(ugid *ovsdpdkdrav1alpha1.UserGroupID) (*int, error) {
	if ugid == nil {
		return nil, nil
	}

	if !ugid.IsName() {
		uid := ugid.GetID()
		return &uid, nil
	}

	name := ugid.GetName()

	r.mu.RLock()
	uid, ok := r.userCache[name]
	r.mu.RUnlock()

	if ok {
		return &uid, nil
	}

	u, err := user.Lookup(name)
	if err != nil {
		return nil, fmt.Errorf("lookup user %q: %w", name, err)
	}

	uid, err = strconv.Atoi(u.Uid)
	if err != nil {
		return nil, fmt.Errorf("parse uid for user %q: %w", name, err)
	}

	r.mu.Lock()
	r.userCache[name] = uid
	r.mu.Unlock()

	return &uid, nil
}

// ResolveGID returns the numeric GID for the given UserGroupID, or nil if ugid
// is nil. Numeric IDs are returned as-is; string names are looked up via
// os/user and the result is cached.
func (r *userResolver) ResolveGID(ugid *ovsdpdkdrav1alpha1.UserGroupID) (*int, error) {
	if ugid == nil {
		return nil, nil
	}

	if !ugid.IsName() {
		gid := ugid.GetID()
		return &gid, nil
	}

	name := ugid.GetName()

	r.mu.RLock()
	gid, ok := r.groupCache[name]
	r.mu.RUnlock()

	if ok {
		return &gid, nil
	}

	g, err := user.LookupGroup(name)
	if err != nil {
		return nil, fmt.Errorf("lookup group %q: %w", name, err)
	}

	gid, err = strconv.Atoi(g.Gid)
	if err != nil {
		return nil, fmt.Errorf("parse gid for group %q: %w", name, err)
	}

	r.mu.Lock()
	r.groupCache[name] = gid
	r.mu.Unlock()

	return &gid, nil
}
