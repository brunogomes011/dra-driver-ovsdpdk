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

package ovs

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/ovn-kubernetes/libovsdb/client"
	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"k8s.io/klog/v2"
)

const (
	// DefaultOVSRunDir is the default OVS run directory.
	DefaultOVSRunDir = "/var/run/openvswitch"
)

// Client defines the interface for interacting with OVSDB.
type Client interface {
	// Connected reports whether the client currently has an active OVSDB connection.
	Connected() bool

	// Close disconnects from OVSDB.
	Close()
}

// ovsClient wraps the libovsdb client for interacting with OVSDB.
type ovsClient struct {
	client client.Client
	log    klog.Logger
}

// New creates an ovsClient and blocks until the initial OVSDB connection
// succeeds.
func New(ctx context.Context, runDir string) (*ovsClient, error) {
	endpoint := "unix:" + filepath.Join(runDir, "db.sock")

	dbModel, err := model.NewClientDBModel("Open_vSwitch",
		map[string]model.Model{
			"Open_vSwitch": &OpenvSwitch{},
			"Bridge":       &Bridge{},
		})
	if err != nil {
		return nil, fmt.Errorf("build OVSDB client model: %w", err)
	}

	ovs, err := client.NewOVSDBClient(dbModel,
		client.WithEndpoint(endpoint),
		client.WithReconnect(30*time.Second, backoff.NewExponentialBackOff()),
	)
	if err != nil {
		return nil, fmt.Errorf("create OVSDB client: %w", err)
	}

	log := klog.Background().WithName("ovsClient")
	log.Info("connecting to OVSDB", "endpoint", endpoint)

	for {
		if err := ovs.Connect(ctx); err == nil {
			break
		} else {
			log.V(2).Info("OVSDB connection failed, retrying in 5s", "endpoint", endpoint, "err", err)
		}

		select {
		case <-ctx.Done():
			ovs.Disconnect()
			return nil, fmt.Errorf("context cancelled while waiting for OVSDB at %q: %w", endpoint, ctx.Err())
		case <-time.After(5 * time.Second):
		}
	}

	log.Info("OVSDB connection established", "endpoint", endpoint)

	c := &ovsClient{
		client: ovs,
		log:    log,
	}

	if err := c.startMonitor(ctx); err != nil {
		ovs.Disconnect()
		return nil, fmt.Errorf("start bridge monitor: %w", err)
	}

	return c, nil
}

// startMonitor starts monitoring relevant tables in the OVSDB.
func (c *ovsClient) startMonitor(ctx context.Context) error {
	bridgeProto := &Bridge{}
	monitor := c.client.NewMonitor(
		// Only monitor bridges with datapath_type == "netdev" (DPDK bridges).
		client.WithConditionalTable(bridgeProto, []model.Condition{{
			Field:    &bridgeProto.DatapathType,
			Function: ovsdb.ConditionEqual,
			Value:    "netdev",
		}}),
	)
	if _, err := c.client.Monitor(ctx, monitor); err != nil {
		return fmt.Errorf("monitor Bridge table: %w", err)
	}
	return nil
}

// Connected reports whether the client currently has an active OVSDB connection.
func (c *ovsClient) Connected() bool {
	return c.client.Connected()
}

// Close disconnects from OVSDB.
func (c *ovsClient) Close() {
	c.log.Info("Closing OVSDB client")
	c.client.Disconnect()
}
