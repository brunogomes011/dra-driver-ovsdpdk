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
	"errors"
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

	// CreatePort creates an OVS port.
	CreatePort(ctx context.Context, bridgeName, portName, socketPath string) error

	// DeletePort delets an OVS port.
	DeletePort(ctx context.Context, bridgeName, portName string) error
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
			"Port":         &Port{},
			"Interface":    &Interface{},
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

// CreatePort creates a dpdkvhostuserclient OVS port and its associated Interface.
func (c *ovsClient) CreatePort(ctx context.Context, bridgeName, portName, socketPath string) error {
	iface := &Interface{
		UUID:    "newiface",
		Name:    portName,
		Type:    "dpdkvhostuserclient",
		Options: map[string]string{"vhost-server-path": socketPath},
	}
	ifaceOps, err := c.client.Create(iface)
	if err != nil {
		return fmt.Errorf("build interface create op: %w", err)
	}

	port := &Port{
		UUID:       "newport",
		Name:       portName,
		Interfaces: []string{"newiface"},
	}
	portOps, err := c.client.Create(port)
	if err != nil {
		return fmt.Errorf("build port create op: %w", err)
	}

	bridge := &Bridge{Name: bridgeName}
	mutateOps, err := c.client.Where(bridge).Mutate(bridge, model.Mutation{
		Field:   &bridge.Ports,
		Mutator: ovsdb.MutateOperationInsert,
		Value:   []string{"newport"},
	})
	if err != nil {
		return fmt.Errorf("build bridge mutate op: %w", err)
	}

	ops := append(append(ifaceOps, portOps...), mutateOps...)
	results, err := c.client.Transact(ctx, ops...)
	if err != nil {
		return fmt.Errorf("create port transaction: %w", err)
	}
	if opErrs, err := ovsdb.CheckOperationResults(results, ops); err != nil {
		return fmt.Errorf("create port %q on bridge %q: %w", portName, bridgeName, joinOpErrors(opErrs))
	}

	c.log.Info("Created OVS port", "bridge", bridgeName, "port", portName, "socket", socketPath)
	return nil
}

// DeletePort removes the named OVS port (and its Interface) from the named bridge.
// Returns ErrPortNotFound (wrapped) if the port does not exist in OVSDB.
func (c *ovsClient) DeletePort(ctx context.Context, bridgeName, portName string) error {
	portUUID, err := c.findPortUUID(ctx, portName)
	if err != nil {
		return err
	}
	if portUUID == "" {
		return ErrPortNotFound
	}

	// Remove port from Bridge.Ports; OVSDB GC handles the rest.
	bridge := &Bridge{Name: bridgeName}
	ops, err := c.client.Where(bridge).Mutate(bridge, model.Mutation{
		Field:   &bridge.Ports,
		Mutator: ovsdb.MutateOperationDelete,
		Value:   []string{portUUID},
	})
	if err != nil {
		return fmt.Errorf("build bridge mutate op: %w", err)
	}

	results, err := c.client.Transact(ctx, ops...)
	if err != nil {
		return fmt.Errorf("delete port transaction: %w", err)
	}
	if opErrs, err := ovsdb.CheckOperationResults(results, ops); err != nil {
		return fmt.Errorf("delete port %q from bridge %q: %w", portName, bridgeName, joinOpErrors(opErrs))
	}

	c.log.Info("Deleted OVS port", "bridge", bridgeName, "port", portName)
	return nil
}

// findPortUUID returns the OVSDB UUID of the named port via a Select
// query to avoid monitoring all ports.
func (c *ovsClient) findPortUUID(ctx context.Context, portName string) (string, error) {
	port := &Port{Name: portName}
	ops, err := c.client.Where(port).Select(port, &port.UUID)
	if err != nil {
		return "", fmt.Errorf("build select for port %q: %w", portName, err)
	}
	results, err := c.client.Transact(ctx, ops...)
	if err != nil {
		return "", fmt.Errorf("select port %q: %w", portName, err)
	}
	var ports []*Port
	if err := c.client.GetSelectResults(ops, results, &ports); err != nil {
		return "", fmt.Errorf("parse select results for port %q: %w", portName, err)
	}
	if len(ports) == 0 {
		return "", nil
	}
	return ports[0].UUID, nil
}

// joinOpErrors converts a slice of ovsdb.OperationError to a single error
// containing all individual error messages joined together.
func joinOpErrors(opErrs []ovsdb.OperationError) error {
	errs := make([]error, len(opErrs))
	for i, e := range opErrs {
		errs[i] = e
	}
	return errors.Join(errs...)
}
