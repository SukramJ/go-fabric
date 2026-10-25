// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/internal/deviceseam"
)

// Scope is the [endpoint.Snapshot] scope of the Aggregator's default
// partition, the one its own methods act on ([Aggregator.Partition] names
// others); the assembler garbage-collects persisted endpoint numbers per
// scope.
const Scope = "node"

// Endpoint is what the Aggregator holds: anything that describes itself
// as an [endpoint.Spec] — a device.Device, or any type with a Spec method
// (ADR 0018 decision 4).
type Endpoint interface {
	Spec() endpoint.Spec
}

// Aggregator errors.
var (
	// ErrNoStableKey: an endpoint whose Spec has no StableKey cannot keep
	// its endpoint number and is refused.
	ErrNoStableKey = errors.New("node: endpoint without a StableKey")
	// ErrDuplicateKey: an endpoint with the key of one already added, in
	// any partition.
	ErrDuplicateKey = errors.New("node: an endpoint with this StableKey is already added")
	// ErrUnknownKey: no endpoint with this key is added (to the partition
	// asked).
	ErrUnknownKey = errors.New("node: no endpoint with this StableKey")
	// ErrKeyMismatch: a replacement whose StableKey is not the key of the
	// endpoint it replaces.
	ErrKeyMismatch = errors.New("node: the replacement has another StableKey")
	// ErrRetireDefault: [Aggregator.RetirePartition] of the default
	// partition, which the node always assembles.
	ErrRetireDefault = errors.New("node: the default partition cannot be retired")
	// ErrPartitionNotEmpty: [Aggregator.RetirePartition] of a partition
	// that still holds endpoints.
	ErrPartitionNotEmpty = errors.New("node: the partition still holds endpoints")
)

// Aggregator is the Aggregator endpoint's set of bridged endpoints —
// matter.js `server.add(aggregator)` and `aggregator.add(endpoint)`
// (examples/device-bridge-onoff/src/BridgedDevicesNode.ts:77-78). It keeps
// the set in insertion order and produces the [endpoint.Snapshot]s the
// assembler consumes (ADR 0018 decision 4). Before [Node.Start] an Add or
// Remove only changes the set (Start's assembly is the first); after it,
// each one reassembles the running bridge, and a [Aggregator.Batch]
// reassembles once for many changes.
//
// Each stable key keeps its endpoint number: [Aggregator.Remove] reserves
// it for the key's return, and only [Aggregator.Forget] (or
// [Aggregator.RetirePartition] for a whole partition) frees it — matter.js
// reserving a number per endpoint store (reserve.go).
//
// A stored key the set does not hold — one a previous run held and this
// run has not added yet, or never will — keeps its number too: nothing is
// collected because it is absent, as matter.js keeps every stored number
// until its store is erased (packages/node/src/storage/server/
// ServerEndpointStores.ts:33-45 load, :109-130 assignNumber at 862ad75f).
// A host that knows a device is gone for good calls Forget.
//
// The set is partitioned for a host whose devices come from several
// sources that load independently: [Aggregator.Partition] names one, with
// its own snapshot scope, and [Aggregator.RetirePartition] erases every
// number stored for a source that left. The Aggregator's own
// methods act on the default partition, scope [Scope]; a host without
// partitions sees one snapshot, as before. A StableKey is unique across
// all partitions.
//
// A device.Device it holds (or a generated type around one) is bound to
// the node while it is held: it takes the node's clock, runs its `…Async`
// callbacks on its own goroutine, and answers [device.Device.EndpointNumber],
// SetReachable and Rename through the node. Remove, Replace and
// [Node.Stop] unbind it, waiting for its queued callbacks.
type Aggregator struct {
	n *Node

	// mu guards parts, handles, retired, detach, started and closed.
	mu sync.Mutex
	// parts are the partitions, the default first, then in creation
	// order; handles the handles Partition gave out.
	parts   []*partition
	handles map[string]*Partition
	// retired are the scopes RetirePartition retired and the next
	// assembly still has to collect.
	retired []string
	started bool
	// detach unbinds the device under each key from the node; closed is
	// set once the node stopped, after which no device is bound.
	detach map[string]func()
	closed bool

	// reassembleMu serialises reassembly: each snapshot is taken after the
	// previous topology was installed, so of two concurrent Add/Remove
	// calls the later install always reflects both — without it an older
	// snapshot could install last, dropping a just-added device.
	reassembleMu sync.Mutex
	// reassemblies counts the successful reassemblies (tests; the
	// aggregator.reassemblies metric counts beside it).
	reassemblies atomic.Int64
}

// partition is one snapshot scope of the set: its endpoints in insertion
// order.
type partition struct {
	scope string
	eps   []Endpoint
}

// Partition is a named part of the Aggregator's set: its own snapshot
// scope. Its methods have the
// Aggregator's semantics, on its endpoints only.
type Partition struct {
	a     *Aggregator
	scope string
}

func keyOf(spec endpoint.Spec) (string, error) {
	if spec.StableKey == nil {
		return "", ErrNoStableKey
	}
	return spec.StableKey.String(), nil
}

// scopeOf is the snapshot scope of a partition name: "" and [Scope] are
// the default partition.
func scopeOf(name string) string {
	if name == "" {
		return Scope
	}
	return name
}

// Partition returns the handle of the partition name, creating it on
// first use; the same name gives the same handle. "" and [Scope] are the
// default partition, the one the Aggregator's own methods act on. A new
// partition is assembled from the next reassembly on, as its own
// [endpoint.Snapshot] with Scope name, after the default one and the
// partitions created before it.
func (a *Aggregator) Partition(name string) *Partition {
	scope := scopeOf(name)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.partLocked(scope)
	if p, ok := a.handles[scope]; ok {
		return p
	}
	if a.handles == nil {
		a.handles = map[string]*Partition{}
	}
	p := &Partition{a: a, scope: scope}
	a.handles[scope] = p
	return p
}

// partLocked returns the partition of scope, creating it; a.mu is held. A
// scope retired and not yet erased is taken back: the new partition's
// own snapshot speaks for it.
func (a *Aggregator) partLocked(scope string) *partition {
	if len(a.parts) == 0 {
		a.parts = []*partition{{scope: Scope}}
	}
	for _, p := range a.parts {
		if p.scope == scope {
			return p
		}
	}
	a.retired = slices.DeleteFunc(a.retired, func(s string) bool { return s == scope })
	p := &partition{scope: scope}
	a.parts = append(a.parts, p)
	return p
}

// findLocked returns the partition and index of the endpoint with key, in
// any partition; nil, -1 for none. a.mu is held.
func (a *Aggregator) findLocked(key string) (part *partition, idx int) {
	for _, p := range a.parts {
		if i := slices.IndexFunc(p.eps, func(ep Endpoint) bool {
			k, err := keyOf(ep.Spec())
			return err == nil && k == key
		}); i >= 0 {
			return p, i
		}
	}
	return nil, -1
}

// RetirePartition gives up the partition name for good: a source that left
// the host's configuration, whose stored endpoint numbers no snapshot would
// ever list again. It needs no handle — after a restart there is none. The
// next assembly first erases every endpoint number stored for scope name
// and their reservations — matter.js erasing each endpoint's store
// (packages/node/src/node/server/ServerEndpointInitializer.ts:59-65
// eraseDescendant at 862ad75f) — and the name is then forgotten. A running node reassembles at once; before [Node.Start]
// the retirement rides on Start's assembly. The default partition is
// refused ([ErrRetireDefault]), and so is a partition that still holds
// endpoints ([ErrPartitionNotEmpty]): Remove them first, or in a
// [Partition.Batch].
func (a *Aggregator) RetirePartition(ctx context.Context, name string) error {
	scope := scopeOf(name)
	if scope == Scope {
		return ErrRetireDefault
	}
	a.mu.Lock()
	for i, p := range a.parts {
		if p.scope != scope {
			continue
		}
		if len(p.eps) > 0 {
			a.mu.Unlock()
			return fmt.Errorf("%w: %s holds %d", ErrPartitionNotEmpty, scope, len(p.eps))
		}
		a.parts = slices.Delete(a.parts, i, i+1)
		break
	}
	if !slices.Contains(a.retired, scope) {
		a.retired = append(a.retired, scope)
	}
	started := a.started
	a.mu.Unlock()
	if !started {
		return nil
	}
	return a.reassemble(ctx)
}

// Add adds ep under the Aggregator — matter.js `aggregator.add(endpoint)`
// (endpoint/Endpoint.ts:755-770) — and binds a device to the node; a running node
// reassembles. A device another node holds is refused
// ([device.ErrAddedElsewhere]). It adds to the default partition.
func (a *Aggregator) Add(ctx context.Context, ep Endpoint) error {
	return a.Partition(Scope).Add(ctx, ep)
}

// Replace puts ep in the place of the endpoint with key: ep must carry the
// same StableKey ([ErrKeyMismatch]), so it keeps the endpoint number the
// endpoint store holds for the key, and the running node reassembles once
// — the Aggregator's PartsList unchanged, the endpoint's Descriptor
// (device types, server list) the one change a controller sees. The old
// endpoint is unbound after the reassembly. It mirrors matter.js replacing
// a part: `close()` keeps the endpoint's number reservation
// (endpoint/Endpoint.ts:1024-1032, ServerEndpointInitializer.ts:67-69,
// where `delete()` would erase it, endpoint/Endpoint.ts:987-992) and the new endpoint
// with the same id is assigned it (ServerEndpointInitializer.ts:34-42).
// It acts on the default partition.
func (a *Aggregator) Replace(ctx context.Context, key device.SourceKey, ep Endpoint) error {
	return a.Partition(Scope).Replace(ctx, key, ep)
}

// Remove drops the endpoint with key; a running node reassembles. The key
// keeps its endpoint number, and its parts theirs, across restarts: an Add
// of the same key gets them back, and
// no other device is given them. [Aggregator.Forget] frees them. It
// mirrors matter.js closing an endpoint, which keeps its number
// reservation (packages/node/src/node/server/ServerEndpointInitializer.ts:
// 67-69 at 862ad75f) and hands it back to the endpoint that returns
// (packages/node/src/storage/server/ServerEndpointStores.ts:117-123). It
// acts on the default partition.
func (a *Aggregator) Remove(ctx context.Context, key device.SourceKey) error {
	return a.Partition(Scope).Remove(ctx, key)
}

// Endpoints returns the default partition's set, in insertion order.
func (a *Aggregator) Endpoints() []Endpoint { return a.Partition(Scope).Endpoints() }

// Batch runs fn with a [Batch] on the default partition and reassembles
// once for every change fn made; see [Partition.Batch].
func (a *Aggregator) Batch(ctx context.Context, fn func(b *Batch) error) error {
	return a.Partition(Scope).Batch(ctx, fn)
}

// Add adds ep to the partition; see [Aggregator.Add]. A key added to any
// partition is refused ([ErrDuplicateKey]).
func (p *Partition) Add(ctx context.Context, ep Endpoint) error {
	return p.Batch(ctx, func(b *Batch) error { return b.Add(ep) })
}

// Replace replaces the partition's endpoint with key; see
// [Aggregator.Replace]. A key of another partition is [ErrUnknownKey].
func (p *Partition) Replace(ctx context.Context, key device.SourceKey, ep Endpoint) error {
	return p.Batch(ctx, func(b *Batch) error { return b.Replace(key, ep) })
}

// Remove drops the partition's endpoint with key; see [Aggregator.Remove].
// A key of another partition is [ErrUnknownKey].
func (p *Partition) Remove(ctx context.Context, key device.SourceKey) error {
	return p.Batch(ctx, func(b *Batch) error { return b.Remove(key) })
}

// Endpoints returns the partition's set, in insertion order.
func (p *Partition) Endpoints() []Endpoint {
	p.a.mu.Lock()
	defer p.a.mu.Unlock()
	return slices.Clone(p.a.partLocked(p.scope).eps)
}

// Batch is a set of changes to one partition that reassembles once: each
// call validates and applies its change at once and returns its own error,
// and [Partition.Batch] reassembles after fn returns.
type Batch struct {
	a       *Aggregator
	scope   string
	changed bool
	// detach are the devices removed or replaced, unbound after the
	// reassembly.
	detach []func()
	// ops are the reservation changes of the batch's Add, Remove and
	// Forget calls, applied in order after fn (reserve.go).
	ops []reservationOp
}

// Batch runs fn with a [Batch] bound to the partition. Each of its calls
// changes the set at once, under the Aggregator's lock as a single Add
// does, and returns its own error; none reassembles. fn runs without any
// Aggregator lock held, so it may call other Aggregator methods. After fn,
// a started node with at least one change applied reassembles exactly
// once, and the devices removed or replaced are then unbound. What applied
// stays applied whatever fails: one faulty device does not block a fleet.
// The result is errors.Join of fn's error and the reassembly's. Before
// [Node.Start] a batch only changes the set.
//
// A batch is not a transaction: an Add or Remove from another goroutine
// during fn reassembles on its own, and that assembly shows the changes
// the batch has applied so far.
func (p *Partition) Batch(ctx context.Context, fn func(b *Batch) error) error {
	b := &Batch{a: p.a, scope: p.scope}
	fnErr := fn(b)
	// The reservations first, so the reassembly keeps a removed key's
	// number and collects a forgotten one's.
	resErr := p.a.applyReservations(ctx, b.ops)
	p.a.mu.Lock()
	started := p.a.started
	p.a.mu.Unlock()
	var err error
	if b.changed && started {
		err = p.a.reassemble(ctx)
	}
	err = errors.Join(resErr, err)
	// Unbound after the reassembly: no controller reaches them any more.
	for _, d := range b.detach {
		d()
	}
	switch {
	case fnErr == nil:
		return err
	case err == nil:
		return fnErr
	default:
		return errors.Join(fnErr, err)
	}
}

// Add adds ep to the batch's partition; see [Aggregator.Add].
func (b *Batch) Add(ep Endpoint) error {
	key, err := keyOf(ep.Spec())
	if err != nil {
		return err
	}
	a := b.a
	a.mu.Lock()
	defer a.mu.Unlock()
	if p, _ := a.findLocked(key); p != nil {
		return fmt.Errorf("%w: %s", ErrDuplicateKey, key)
	}
	detach, err := a.attach(ep)
	if err != nil {
		return err
	}
	part := a.partLocked(b.scope)
	part.eps = append(part.eps, ep)
	a.detachSetLocked(key, detach)
	b.changed = true
	// A returning key takes its reserved number back; the reservation is
	// done with.
	b.ops = append(b.ops, reservationOp{kind: opRelease, key: key})
	return nil
}

// Replace replaces the endpoint with key in the batch's partition; see
// [Aggregator.Replace]. The old endpoint is unbound after the batch's
// reassembly.
func (b *Batch) Replace(key device.SourceKey, ep Endpoint) error {
	if key == nil {
		return ErrNoStableKey
	}
	newKey, err := keyOf(ep.Spec())
	if err != nil {
		return err
	}
	if newKey != key.String() {
		return fmt.Errorf("%w: %s replaces %s", ErrKeyMismatch, newKey, key)
	}
	a := b.a
	a.mu.Lock()
	defer a.mu.Unlock()
	part, i := a.findLocked(newKey)
	if part == nil || part.scope != b.scope {
		return fmt.Errorf("%w: %s", ErrUnknownKey, key)
	}
	if part.eps[i] == ep {
		return nil
	}
	old := a.detach[newKey]
	detach, err := a.attach(ep)
	if err != nil {
		return err
	}
	part.eps[i] = ep
	a.detachSetLocked(newKey, detach)
	if old != nil {
		b.detach = append(b.detach, old)
	}
	b.changed = true
	return nil
}

// Remove drops the endpoint with key from the batch's partition; see
// [Aggregator.Remove]. The device is unbound after the batch's
// reassembly.
func (b *Batch) Remove(key device.SourceKey) error {
	if key == nil {
		return ErrNoStableKey
	}
	a := b.a
	a.mu.Lock()
	part, i := a.findLocked(key.String())
	if part == nil || part.scope != b.scope {
		a.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrUnknownKey, key)
	}
	removed := part.eps[i]
	b.dropLocked(part, i, key.String())
	a.mu.Unlock()
	b.ops = append(b.ops, reservationOp{kind: opReserve, key: key.String(), tree: reservationTree(removed.Spec())})
	return nil
}

// dropLocked takes the endpoint at i out of part, its device unbound after
// the batch's reassembly; a.mu is held.
func (b *Batch) dropLocked(part *partition, i int, key string) {
	a := b.a
	part.eps = slices.Delete(part.eps, i, i+1)
	if d := a.detach[key]; d != nil {
		b.detach = append(b.detach, d)
	}
	a.detachSetLocked(key, nil)
	b.changed = true
}

// attach binds ep to the node when it is a device; the returned detach is
// nil for any other endpoint and on a stopped node. a.mu is held.
func (a *Aggregator) attach(ep Endpoint) (func(), error) {
	if a.closed {
		return nil, nil
	}
	detach, isDevice, err := deviceseam.Attach(ep, nodeHost{a.n})
	if err != nil || !isDevice {
		return nil, err
	}
	return detach, nil
}

// detachSetLocked records detach for key; a.mu is held.
func (a *Aggregator) detachSetLocked(key string, detach func()) {
	if a.detach == nil {
		a.detach = map[string]func(){}
	}
	if detach == nil {
		delete(a.detach, key)
		return
	}
	a.detach[key] = detach
}

// detachAll unbinds every device the Aggregator holds — at [Node.Stop],
// after the bridge stopped — waiting for their queued callbacks. The set
// stays: a device is bound again only by a new Add.
func (a *Aggregator) detachAll() {
	a.mu.Lock()
	detach := a.detach
	a.detach = nil
	a.closed = true
	a.mu.Unlock()
	for _, key := range slices.Sorted(maps.Keys(detach)) {
		detach[key]()
	}
}

// IncreaseConfigurationVersion raises the ConfigurationVersion of the
// bridged endpoint with key (and the node's, as the bridge does) and
// persists both, so the version never decreases across a restart — the
// daemon's configChange (main.go:299-313 at b4ea1f96^).
func (a *Aggregator) IncreaseConfigurationVersion(key device.SourceKey) error {
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
func (a *Aggregator) IncreaseConfigurationVersions(keys ...device.SourceKey) error {
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
	a.reassemblies.Add(1)
	a.n.count(MetricAggregatorReassemblies)
	return nil
}

// snapshot is the bridge's Snapshotter: one snapshot per partition, the
// default first and then the others in creation order, each endpoint's
// persisted NodeLabel and ConfigurationVersion restored as the daemon's
// fleet does (fleet.go:1625-1647 at b4ea1f96^). A persisted label
// replaces the Spec's; the version is the larger of the two. A retired
// partition's stored numbers are erased first, once.
func (a *Aggregator) snapshot(ctx context.Context) (*endpoint.Topology, error) {
	type part struct {
		scope string
		eps   []Endpoint
	}
	a.mu.Lock()
	a.partLocked(Scope)
	parts := make([]part, 0, len(a.parts))
	for _, p := range a.parts {
		parts = append(parts, part{p.scope, slices.Clone(p.eps)})
	}
	retired := slices.Clone(a.retired)
	a.mu.Unlock()
	for _, scope := range retired {
		// Retired for good: the scope's numbers and reservations go.
		if err := a.eraseScope(ctx, scope); err != nil {
			return nil, err
		}
	}
	snaps := make([]endpoint.Snapshot, 0, len(parts))
	for i, p := range parts {
		specs := make([]endpoint.Spec, 0, len(p.eps)+1)
		for _, ep := range p.eps {
			spec := ep.Spec()
			if label := a.n.endpoints.NodeLabel(ctx, spec.StableKey); label != "" {
				spec.NodeLabel = label
			}
			spec.ConfigurationVersion = max(spec.ConfigurationVersion, a.n.persisted.configVersion(ctx, spec.StableKey))
			specs = append(specs, spec)
		}
		// The OTA provider's endpoint is numbered in the default scope
		// and placed under the root (ota_provider.go).
		if i == 0 && a.n.otaProvider != nil {
			specs = append(specs, otaProviderEndpoint{a.n.otaProvider}.spec())
		}
		snaps = append(snaps, endpoint.Snapshot{Scope: p.scope, Endpoints: specs})
	}
	topo, err := a.n.assembler.Assemble(ctx, snaps)
	if err != nil {
		return nil, err
	}
	if len(retired) > 0 {
		// Erased: the names are forgotten.
		a.mu.Lock()
		a.retired = slices.DeleteFunc(a.retired, func(s string) bool { return slices.Contains(retired, s) })
		a.mu.Unlock()
	}
	placeOTAProvider(topo)
	return topo, nil
}
