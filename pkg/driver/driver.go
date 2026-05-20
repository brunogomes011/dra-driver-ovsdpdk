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

// Package driver implements the DRA kubelet plugin for OVS-DPDK vhost-user ports.
package driver

import (
	"context"
	"fmt"

	coreclientset "k8s.io/client-go/kubernetes"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/klog/v2"

	"github.com/amorenoz/dra-driver-ovsdpdk/pkg/consts"
)

// Driver is the DRA kubelet plugin for OVS-DPDK vhost-user ports.
type Driver struct {
	log    klog.Logger
	helper *kubeletplugin.Helper
}

// New creates a new Driver. Call Start to register it with kubelet.
func New(ctx context.Context, kubeClient coreclientset.Interface, nodeName, pluginDataDir string) (*Driver, error) {
	logger := klog.FromContext(ctx).WithName("driver")

	d := &Driver{log: logger}

	helper, err := kubeletplugin.Start(ctx, d,
		kubeletplugin.DriverName(consts.DriverName),
		kubeletplugin.NodeName(nodeName),
		kubeletplugin.KubeClient(kubeClient),
		kubeletplugin.PluginDataDirectoryPath(pluginDataDir),
	)
	if err != nil {
		return nil, fmt.Errorf("start kubelet plugin: %w", err)
	}

	d.helper = helper
	d.log.Info("DRA driver started")
	return d, nil
}

// Stop shuts down the DRA driver and deregisters from kubelet.
func (d *Driver) Stop() {
	d.helper.Stop()
}
