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
// filesystem.
package socketfs

import (
	"fmt"
	"os"
)

// SocketFS manages creation and removal of socket directories.
type SocketFS interface {
	// CreateSocketDir creates the socket directory at path.
	CreateSocketDir(path string) error

	// RemoveSocketDir removes the socket directory at path.
	RemoveSocketDir(path string) error
}

type socketFS struct{}

// New returns a SocketFS backed by real filesystem operations.
func New() SocketFS {
	return &socketFS{}
}

func (s *socketFS) CreateSocketDir(path string) error {
	if err := os.MkdirAll(path, 0o775); err != nil {
		return fmt.Errorf("create socket directory %q: %w", path, err)
	}

	// Re-run chmod to bypass umask.
	if err := os.Chmod(path, 0o775); err != nil {
		_ = s.RemoveSocketDir(path)
		return fmt.Errorf("chmod socket directory %q: %w", path, err)
	}
	return nil
}

func (s *socketFS) RemoveSocketDir(path string) error {
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove socket directory %q: %w", path, err)
	}
	return nil
}
