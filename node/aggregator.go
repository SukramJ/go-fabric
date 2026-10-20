// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/bridge"
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
// assembler consumes (ADR 0018 decision 4): one scope, [Scope]. Before
// [Node.Start] an Add or Remove only changes the set (Start's assembly is
// the first); after it, each one reassembles the running bridge.
//
// Call [Aggregator.MarkComplete] once the host has added every device it
// knows about; before that, numbers of stored endpoints are retained. The
// snapshots carry ModelComplete false until then and true afterwards, when
// a stored endpoint absent from the set is garbage-collected
// (endpoint.Snapshot.ModelComplete, BD-Matter-EndpointGC-ModelCompleteGate).
// Start does not mark the set complete.
type Aggregator struct {
	n *Node

	// mu guards complete, eps and started.
	mu       sync.Mutex
	eps      []Endpoint
	started  bool
	complete bool

	// reassembleMu serialises reassembly: each snapshot is taken after the
	// previous topology was installed, so of two concurrent Add/Remove
	// calls the later install always reflects both — without it an older
	// snapshot could install last, dropping a just-added device and, on a
	// model-complete set, releasing its endpoint number.
	reassembleMu sync.Mutex
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

// Remove drops the endpoint with key; a running node reassembles. Once the
// set is complete ([Aggregator.MarkComplete]) the assembler releases the
// endpoint's number then, as matter.js erases a deleted endpoint's storage
// (ServerEndpointInitializer.ts eraseDescendant, cited at
// endpoint.Snapshot.ModelComplete); a re-add gets a fresh number.
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

// MarkComplete declares the set authoritative: every later snapshot is
// model-complete, so stored endpoints absent from it are garbage-collected.
// Call it once the host has added every device it knows about; before
// that, numbers of stored endpoints are retained. On a running node it
// reassembles at once; a failure is logged and the next Add, Remove or
// Reassemble retries.
func (a *Aggregator) MarkComplete() {
	a.mu.Lock()
	already := a.complete
	a.complete = true
	started := a.started
	a.mu.Unlock()
	if already || !started {
		return
	}
	if err := a.reassemble(context.Background()); err != nil {
		a.n.logger.Warn("node.aggregator.mark_complete", slog.String("err", err.Error()))
	}
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

// IncreaseConfigurationVersions raises, as one change of the node, the
// ConfigurationVersion of the bridged endpoints with keys — each physical
// device once — and the node's own once, after them, and persists every
// version raised in one pass. With no key it raises the node's alone. An
// unknown key fails with [ErrUnknownKey] before anything is raised. Mirrors
// matter.js BasicInformationServer.increaseConfigurationVersion(change)
// (BasicInformationServer.ts:132-138: the change first, one increment
// afterwards) with the bridged devices raised under the shared context
// (BridgedDeviceBasicInformationServer.ts:72-90); the bridge's
// IncreaseNodeConfigurationVersionWith does the raising.
func (a *Aggregator) IncreaseConfigurationVersions(keys ...endpoint.SourceKey) error {
	topo := a.n.br.Topology()
	if topo == nil {
		return errors.New("node: no topology yet")
	}
	type device struct{ scope, address string }
	var devices []device
	for _, key := range keys {
		found := false
		for _, ep := range topo.Bridged() {
			if ep.SourceKey == nil || key == nil || ep.SourceKey.String() != key.String() {
				continue
			}
			found = true
			d := device{ep.Scope, ep.DeviceAddress}
			if !slices.Contains(devices, d) {
				devices = append(devices, d)
			}
			break
		}
		if !found {
			return fmt.Errorf("%w: %v", ErrUnknownKey, key)
		}
	}
	versions := a.n.br.IncreaseNodeConfigurationVersionWith(func(c *bridge.ConfigurationChange) {
		for _, d := range devices {
			c.IncreaseConfigurationVersion(d.scope, d.address)
		}
	})
	a.n.persisted.storeConfigVersions(topo, versions)
	return nil
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
	a.reassembleMu.Lock()
	defer a.reassembleMu.Unlock()
	if err := a.n.br.Reassemble(ctx); err != nil {
		return fmt.Errorf("node: reassemble: %w", err)
	}
	return nil
}

// snapshot is the bridge's Snapshotter: the set as one snapshot,
// model-complete once MarkComplete was called, each endpoint's persisted NodeLabel and ConfigurationVersion
// restored as the daemon's fleet does (fleet.go:1625-1647). A persisted
// label replaces the Spec's; the version is the larger of the two.
func (a *Aggregator) snapshot(ctx context.Context) (*endpoint.Topology, error) {
	a.mu.Lock()
	eps := slices.Clone(a.eps)
	complete := a.complete
	a.mu.Unlock()
	specs := make([]endpoint.Spec, 0, len(eps))
	for _, ep := range eps {
		spec := ep.Spec()
		if label := a.n.endpoints.NodeLabel(ctx, spec.StableKey); label != "" {
			spec.NodeLabel = label
		}
		spec.ConfigurationVersion = max(spec.ConfigurationVersion, a.n.persisted.configVersion(ctx, spec.StableKey))
		specs = append(specs, spec)
	}
	// The OTA provider's endpoint is numbered in the same scope and placed
	// under the root (ota_provider.go).
	if a.n.otaProvider != nil {
		specs = append(specs, otaProviderEndpoint{a.n.otaProvider}.spec())
	}
	topo, err := a.n.assembler.Assemble(ctx, []endpoint.Snapshot{{
		Scope:         Scope,
		Endpoints:     specs,
		ModelComplete: complete,
	}})
	if err != nil {
		return nil, err
	}
	placeOTAProvider(topo)
	return topo, nil
}
