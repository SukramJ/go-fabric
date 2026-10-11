// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/endpoint"
)

// Scope is the one [endpoint.Snapshot] scope the Aggregator assembles
// under. A node has one partition; the assembler garbage-collects
// persisted endpoint numbers per scope.
const Scope = "node"

// Endpoint is what the Aggregator holds: anything that describes itself
// as an [endpoint.Spec]. Brief P's device.Device satisfies it; until that
// lands, any type with a Spec method does (ADR 0018 decision 4).
type Endpoint interface {
	Spec() endpoint.Spec
}

// Aggregator errors.
var (
	// ErrNoStableKey: an endpoint whose Spec has no StableKey cannot keep
	// its endpoint number and is refused.
	ErrNoStableKey = errors.New("node: endpoint without a StableKey")
	// ErrDuplicateKey: an endpoint with the key of one already added.
	ErrDuplicateKey = errors.New("node: an endpoint with this StableKey is already added")
	// ErrUnknownKey: no endpoint with this key is added.
	ErrUnknownKey = errors.New("node: no endpoint with this StableKey")
)

// Aggregator is the Aggregator endpoint's set of bridged endpoints —
// matter.js `server.add(aggregator)` and `aggregator.add(endpoint)`
// (examples/device-bridge-onoff/src/BridgedDevicesNode.ts:77-78). It keeps
// the set in insertion order and produces the [endpoint.Snapshot] the
// assembler consumes (ADR 0018 decision 4): one scope, [Scope], with
// ModelComplete true, so endpoint-id persistence works as the assembler
// documents it. Before [Node.Start] an Add or Remove only changes the set
// (Start's assembly is the first); after it, each one reassembles the
// running bridge.
type Aggregator struct {
	n *Node

	mu      sync.Mutex
	eps     []Endpoint
	started bool
}

func keyOf(spec endpoint.Spec) (string, error) {
	if spec.StableKey == nil {
		return "", ErrNoStableKey
	}
	return spec.StableKey.String(), nil
}

// Add adds ep under the Aggregator; a running node reassembles.
func (a *Aggregator) Add(ctx context.Context, ep Endpoint) error {
	key, err := keyOf(ep.Spec())
	if err != nil {
		return err
	}
	a.mu.Lock()
	if a.indexLocked(key) >= 0 {
		a.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrDuplicateKey, key)
	}
	a.eps = append(a.eps, ep)
	started := a.started
	a.mu.Unlock()
	if !started {
		return nil
	}
	return a.reassemble(ctx)
}

// Remove drops the endpoint with key; a running node reassembles. The
// assembler releases the endpoint's number then, as matter.js erases a
// deleted endpoint's number (ServerEndpointInitializer.ts eraseDescendant,
// cited at endpoint.Snapshot.ModelComplete).
func (a *Aggregator) Remove(ctx context.Context, key endpoint.SourceKey) error {
	if key == nil {
		return ErrNoStableKey
	}
	a.mu.Lock()
	i := a.indexLocked(key.String())
	if i < 0 {
		a.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrUnknownKey, key)
	}
	a.eps = slices.Delete(a.eps, i, i+1)
	started := a.started
	a.mu.Unlock()
	if !started {
		return nil
	}
	return a.reassemble(ctx)
}

// Endpoints returns the set, in insertion order.
func (a *Aggregator) Endpoints() []Endpoint {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.eps)
}

// IncreaseConfigurationVersion raises the ConfigurationVersion of the
// bridged endpoint with key (and the node's, as the bridge does) and
// persists both, so the version never decreases across a restart — the
// daemon's configChange (main.go:299-313).
func (a *Aggregator) IncreaseConfigurationVersion(key endpoint.SourceKey) error {
	topo := a.n.br.Topology()
	if topo == nil {
		return errors.New("node: no topology yet")
	}
	for _, ep := range topo.Bridged() {
		if ep.SourceKey != nil && key != nil && ep.SourceKey.String() == key.String() {
			a.n.persisted.storeConfigVersions(topo, a.n.br.IncreaseConfigurationVersion(ep.Scope, ep.DeviceAddress))
			return nil
		}
	}
	return fmt.Errorf("%w: %v", ErrUnknownKey, key)
}

func (a *Aggregator) indexLocked(key string) int {
	return slices.IndexFunc(a.eps, func(ep Endpoint) bool {
		k, err := keyOf(ep.Spec())
		return err == nil && k == key
	})
}

func (a *Aggregator) setStarted() {
	a.mu.Lock()
	a.started = true
	a.mu.Unlock()
}

func (a *Aggregator) reassemble(ctx context.Context) error {
	if err := a.n.br.Reassemble(ctx); err != nil {
		return fmt.Errorf("node: reassemble: %w", err)
	}
	return nil
}

// snapshot is the bridge's Snapshotter: the set as one model-complete
// snapshot, each endpoint's persisted NodeLabel and ConfigurationVersion
// restored as the daemon's fleet does (fleet.go:1625-1647). A persisted
// label replaces the Spec's; the version is the larger of the two.
func (a *Aggregator) snapshot(ctx context.Context) (*endpoint.Topology, error) {
	eps := a.Endpoints()
	specs := make([]endpoint.Spec, 0, len(eps))
	for _, ep := range eps {
		spec := ep.Spec()
		if label := a.n.persisted.label(ctx, spec.StableKey); label != "" {
			spec.NodeLabel = label
		}
		spec.ConfigurationVersion = max(spec.ConfigurationVersion, a.n.persisted.configVersion(ctx, spec.StableKey))
		specs = append(specs, spec)
	}
	return a.n.assembler.Assemble(ctx, []endpoint.Snapshot{{
		Scope:         Scope,
		Endpoints:     specs,
		ModelComplete: true,
	}})
}
