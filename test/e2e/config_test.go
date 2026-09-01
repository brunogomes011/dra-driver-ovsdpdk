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

package e2e_test

import (
	"context"
	"os"
)

type platformConfig struct {
	bridge0    string
	bridge1    string
	bridge2    string
	topoBridge string

	ovsUID   string
	aclEntry string

	configUser    string
	configGroup   string
	configAclUser string

	selinuxLabel string
}

// This variable is applied to determine if the test is done
// toward an upstream platform or Openshift/Downstream
var isOpenShift = os.Getenv("E2E_PLATFORM") == "openshift"

var plat = newPlatform()

var ovsExec = newOvsExec()

func newPlatform() platformConfig {
	if isOpenShift {
		return platformConfig{
			bridge0:    "br-phys",
			bridge1:    "br-phys1",
			bridge2:    "br-phys2",
			topoBridge: "br-tdptest",

			ovsUID:   "800",
			aclEntry: "user:openvswitch",

			configUser:    "openvswitch",
			configGroup:   "107",
			configAclUser: "openvswitch",
			selinuxLabel:  "system_u:object_r:container_file_t:s0",
		}
	}
	return platformConfig{
		bridge0:    "br-dpdk0",
		bridge1:    "br-dpdk1",
		bridge2:    "br-dpdk2",
		topoBridge: "br-dpdk0",

		ovsUID:   "1001",
		aclEntry: "user:1001",

		configUser:    "1001",
		configGroup:   "107",
		configAclUser: "1001",
		selinuxLabel:  "system_u:object_r:container_file_t:s0",
	}
}

func newOvsExec() func(ctx context.Context, nodeName string, args ...string) (string, error) {
	if isOpenShift {
		return ovsNodeExec
	}
	return ovsPodExec
}
