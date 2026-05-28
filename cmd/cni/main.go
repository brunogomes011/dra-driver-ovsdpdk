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

// ovsdpdk-cni is a no-op CNI plugin that satisfies Multus when it calls ADD,
// DEL, CHECK, or VERSION for ovsdpdk-cni networks.  The actual network
// plumbing is handled by the DRA driver; this binary exists only so that
// Multus can find a CNI binary with the matching type name.
package main

import (
	"encoding/json"
	"fmt"
	"os"
)

const cniVersion = "1.0.0"

type cniResult struct {
	CNIVersion string `json:"cniVersion"`
}

type versionResult struct {
	CNIVersion        string   `json:"cniVersion"`
	SupportedVersions []string `json:"supportedVersions"`
}

type cniError struct {
	Code    uint   `json:"code"`
	Msg     string `json:"msg"`
	Details string `json:"details,omitempty"`
}

func main() {
	cmd := os.Getenv("CNI_COMMAND")
	switch cmd {
	case "VERSION":
		respond(versionResult{
			CNIVersion:        cniVersion,
			SupportedVersions: []string{"0.3.0", "0.3.1", "0.4.0", "1.0.0"},
		})
	case "ADD":
		respond(cniResult{CNIVersion: cniVersion})
	case "DEL", "CHECK":
		respond(cniResult{CNIVersion: cniVersion})
	default:
		respondError(cniError{
			Code: 4,
			Msg:  fmt.Sprintf("unknown CNI_COMMAND: %q", cmd),
		})
		os.Exit(1)
	}
}

func respond(v interface{}) {
	if err := json.NewEncoder(os.Stdout).Encode(v); err != nil {
		fmt.Fprintf(os.Stderr, "ovsdpdk-cni: encode response: %v\n", err)
		os.Exit(1)
	}
}

func respondError(e cniError) {
	if err := json.NewEncoder(os.Stdout).Encode(e); err != nil {
		fmt.Fprintf(os.Stderr, "ovsdpdk-cni: encode error response: %v\n", err)
	}
}
