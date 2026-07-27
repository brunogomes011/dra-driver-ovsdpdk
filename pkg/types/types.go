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

package types

import (
	"context"

	coreclientset "k8s.io/client-go/kubernetes"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/amorenoz/dra-driver-ovsdpdk/pkg/consts"
	"github.com/amorenoz/dra-driver-ovsdpdk/pkg/flags"
)

// Flags holds all parsed CLI flags.
type Flags struct {
	NodeName                      string
	Namespace                     string
	ConfigName                    string
	CdiRoot                       string
	KubeletRegistrarDirectoryPath string
	KubeletPluginsDirectoryPath   string
	EnableDeviceMetadata          bool
	LoggingConfig                 *flags.LoggingConfig
	KubeClientConfig              flags.KubeClientConfig
}

// Config is the top-level runtime configuration passed between components.
type Config struct {
	Flags         *Flags
	K8sClient     coreclientset.Interface
	Manager       ctrl.Manager
	CancelMainCtx context.CancelCauseFunc
}

// DriverPluginPath returns the path where the driver registers its kubelet plugin socket.
func (c *Config) DriverPluginPath() string {
	return c.Flags.KubeletPluginsDirectoryPath + "/" + consts.DriverName
}

// MountInfo describes the vhost-user socket directory on both sides of the CDI mount.
type MountInfo struct {
	HostDir      string
	ContainerDir string
}

// SocketInfo describes the vhost-user socket path on both sides of the CDI mount.
type SocketInfo struct {
	HostPath      string
	ContainerPath string
}

// PreparedDevice is the unit of prepared state for a single ResourceClaim.
type PreparedDevice struct {
	Device              kubeletplugin.Device
	ClaimNamespacedName kubeletplugin.NamespacedObject
	BridgeName          string
	Mount               MountInfo
	Socket              SocketInfo
}
