// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"

	"thingfabric.dev/go-fabric/bridge"
	"thingfabric.dev/go-fabric/device"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/internal/deviceseam"
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
	// any partition, or with a key — its own or a part's — that another
	// endpoint serves, itself or as a part (decision 580). In a [Batch]
	// such a collision is judged against the batch's end state and the
	// error arrives from [Partition.Batch], not from the call (decisions
	// 582, 585).
	ErrDuplicateKey = errors.New("node: an endpoint with this StableKey is already added")
	// ErrUnknownKey: no endpoint with this key is added (to the partition
	// asked), or a Forget of a part key another endpoint serves (decision
	// 567; in a [Batch] judged against its end state and returned from
	// [Partition.Batch], decision 585).
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
	// ErrBatchDone: a call on a [Batch] after its fn returned. A Batch is
	// valid only during the fn of [Partition.Batch] (or
	// [Aggregator.Batch]); a later call changes nothing (decision 585).
	ErrBatchDone = errors.New("node: the batch's fn has returned")
)

// Aggregator is the Aggregator endpoint's set of bridged endpoints —
// matter.js `server.add(aggregator)` and `aggregator.add(endpoint)`
// (examples/device-bridge-onoff/src/BridgedDevicesNode.ts:77-78). It keeps
// the set in insertion order and produces the [endpoint.Snapshot]s the
// assembler consumes (ADR 0018 decision 4). Before [Node.Start] an Add or
// Remove only changes the set (Start's assembly is the first); after it,
// each one reassembles the running bridge, and a [Aggregator.Batch]
// reassembles once for many changes. A batch is judged against its end
// state: a device whose key the set holds or another endpoint serves, or
// a Forget of a part key another endpoint serves, is refused only when
// the collision is still there once the batch's calls ran, whatever their
// order ([Batch], decisions 582, 585).
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
// ServerEndpointStores.ts:33-45 load, :109-130 assignNumber at db021da0).
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
	// forgetMu serialises the record of the forgets in progress
	// (markForget).
	forgetMu sync.Mutex
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

// findPartLocked returns the partition of scope, nil for none; unlike
// partLocked it creates nothing and takes no scope off the retired list.
// a.mu is held.
func (a *Aggregator) findPartLocked(scope string) *partition {
	for _, p := range a.parts {
		if p.scope == scope {
			return p
		}
	}
	return nil
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
// eraseDescendant at b25ed960) — and the name is then forgotten. A running node reassembles at once; before [Node.Start]
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
	if a.closed {
		a.mu.Unlock()
		return ErrStopped
	}
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
// ([device.ErrAddedElsewhere]), and so is one whose key, or a part's, an
// endpoint of any partition already serves ([ErrDuplicateKey]). It adds
// to the default partition.
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
// A replacement that fails validation ([ErrKeyMismatch],
// [ErrPolicyMismatch], a part key another endpoint serves:
// [ErrDuplicateKey], …) changes nothing. A replacement the
// specification level withholds (ep's report lists withheld elements)
// takes the endpoint off the wire as [Aggregator.Remove] does — the key
// keeps its endpoint number, and its parts theirs — records ep as
// withheld in [Node.Surface], and returns the *surface.WithheldError; an
// Add of a declaration the level serves gets the number back.
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
// 67-69 at b25ed960) and hands it back to the endpoint that returns
// (packages/node/src/storage/server/ServerEndpointStores.ts:117-123). It
// acts on the default partition.
func (a *Aggregator) Remove(ctx context.Context, key device.SourceKey) error {
	return a.Partition(Scope).Remove(ctx, key)
}

// Endpoints returns the default partition's set, in insertion order.
func (a *Aggregator) Endpoints() []Endpoint { return a.Partition(Scope).Endpoints() }

// Batch runs fn with a [Batch] on the default partition and reassembles
// once for every change fn made; see [Partition.Batch], which names every
// error that arrives here rather than from a call of the batch
// ([ErrDuplicateKey], [ErrUnknownKey], [ErrReservedKey],
// [ErrPolicyMismatch], *surface.WithheldError, a device's attach error).
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

// Partitions are the names of the partitions, the default ([Scope])
// first, then in creation order; a retired one is gone.
func (a *Aggregator) Partitions() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.partLocked(Scope)
	out := make([]string, len(a.parts))
	for i, p := range a.parts {
		out[i] = p.scope
	}
	return out
}

// allEndpoints is the set of every partition, partition by partition.
func (a *Aggregator) allEndpoints() []Endpoint {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []Endpoint
	for _, p := range a.parts {
		out = append(out, p.eps...)
	}
	return out
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
//
// A batch is judged against its end state (decisions 582, 585): a call
// that collides with a key another endpoint serves is not refused at the
// call. It returns nil, and the change is held until fn returned; then
// the held calls are judged, in call order, against the set fn left. The
// calls held are:
//
//   - an Add of a key the set holds, or an Add or a Replace whose device —
//     its own key or a part key — another endpoint serves: applied if no
//     other endpoint serves any of its keys any more, refused with
//     [ErrDuplicateKey] otherwise (decisions 580, 584);
//   - a Forget of a part key another endpoint serves: applied if no other
//     endpoint serves it any more, refused with [ErrUnknownKey] otherwise
//     (decision 567).
//
// The device of a held Add or Replace is judged as it is then — a part
// added to it during fn counts. So is the device of an Add or a Replace
// applied at the call: one that gained during fn a part whose key another
// endpoint serves is refused with [ErrDuplicateKey] and undone — an Add
// leaves the set, a Replace puts the replaced endpoint back — so the
// batch never assembles two Specs under one key (decision 585). Every
// error of a held call arrives in the result of [Partition.Batch]: those
// refusals, and what the applied call itself returns ([ErrReservedKey], [ErrPolicyMismatch], a
// *surface.WithheldError, a device's attach error). So a batch that takes
// a key — or forgets a part key — and removes or forgets the endpoint
// serving it gives the same outcome in either call order. A held change
// takes part in the batch's reassembly like any other. A later Replace,
// Remove or Forget of a key the set holds acts on the set's endpoint, and
// a held Add of that key stays held; of a key the set does not hold, it
// acts on the held Add: a Replace replaces it, a Remove or Forget drops
// it.
//
// A Batch is valid only during fn: a call after fn returned is refused
// with [ErrBatchDone] and changes nothing.
type Batch struct {
	a     *Aggregator
	scope string
	// ctx is the context of [Partition.Batch]: the store work its calls
	// do (a withheld endpoint's surface dropped) runs under it.
	ctx     context.Context
	changed bool
	// detach are the devices removed or replaced, unbound after the
	// reassembly.
	detach []func()
	// ops are the reservation changes of the batch's Add, Remove and
	// Forget calls, applied in order after fn (reserve.go).
	ops []reservationOp
	// pending are the Add, Replace and Forget calls that collided at the
	// call with a key another endpoint served: judged after fn against
	// the batch's end state, in call order (decisions 582, 585).
	pending []pendingCall
	// taken are the Adds and Replaces the batch applied, at the call or
	// at commit, in that order: judged again at the end of commitPending,
	// as a part added to the device during fn may collide (decision 585,
	// fable-diff-g5g N1).
	taken []takenCall
	// done is set once fn returned: every later call is [ErrBatchDone].
	// a.mu guards it.
	done bool
}

// pendingKind is the call a [pendingCall] holds.
type pendingKind uint8

const (
	heldAdd pendingKind = iota
	heldReplace
	heldForget
)

// pendingCall is a call a [Batch] judges after fn. An Add or a Replace:
// its device's own key or a part key collided, at the call, with a key
// the set held, another endpoint served or another held device declared;
// entries are its surface at the call, used to hold later takes behind
// it. A Forget: another endpoint served its key; reserved is the tree
// the store reserved for the key at the call.
type pendingCall struct {
	kind     pendingKind
	key      string
	ep       Endpoint
	entries  []surfaceEntry
	reserved []string
}

// takenCall is an Add or a Replace a [Batch] applied: ep under key in the
// partition; for a Replace, old is the endpoint it replaced, oldDetach
// that endpoint's detach and oldAt its index in the batch's detach list
// (-1: none).
type takenCall struct {
	key       string
	ep        Endpoint
	old       Endpoint
	oldDetach func()
	oldAt     int
}

// Batch runs fn with a [Batch] bound to the partition. Each of its calls
// changes the set at once, under the Aggregator's lock as a single Add
// does, and returns its own error; none reassembles. fn runs without any
// Aggregator lock held, so it may call other Aggregator methods. After fn,
// a started node with at least one change applied reassembles exactly
// once, and the devices removed or replaced are then unbound. What applied
// stays applied whatever fails: one faulty device does not block a fleet.
// Before the reassembly, the calls the batch held (a key another endpoint
// served at the call; see [Batch]) are judged against the set fn left:
// each one is applied, or refused — an Add or a Replace with
// [ErrDuplicateKey] when another endpoint still serves one of its keys,
// a Forget with [ErrUnknownKey] (decisions 582, 585). The result is
// errors.Join of fn's error, every error of a held call — those
// refusals, [ErrUnknownKey] for a held Replace whose key left the
// partition, [ErrReservedKey] or [ErrPolicyMismatch] of a device changed
// during fn, the *surface.WithheldError of an applied declaration the
// specification level withholds, a device's attach error — the store's
// and the reassembly's. Before [Node.Start] a batch only changes the set.
// The [Batch] is valid only during fn ([ErrBatchDone]).
//
// A batch is not a transaction: an Add or Remove from another goroutine
// during fn reassembles on its own, and that assembly shows the changes
// the batch has applied so far.
func (p *Partition) Batch(ctx context.Context, fn func(b *Batch) error) error {
	// A stopped node takes no change: [Node.Start] after Stop is
	// ErrStopped, and so is every change of its set — the device would
	// bind to nothing and the reassembly fail on a stopped bridge.
	p.a.mu.Lock()
	closed := p.a.closed
	p.a.mu.Unlock()
	if closed {
		return ErrStopped
	}
	b := &Batch{a: p.a, scope: p.scope, ctx: ctx}
	fnErr := fn(b)
	// The held calls are judged against the set fn left, before the
	// reservations, so an applied one's ops are among them; the batch
	// takes no call from here on.
	if takeErr := b.commitPending(); takeErr != nil {
		fnErr = errors.Join(fnErr, takeErr)
	}
	// The reservations first, so the reassembly keeps a removed key's
	// number — but a forgotten key's erasure last: the reassembly takes
	// its endpoint off the wire and the device is unbound before what is
	// stored under its keys is erased, so no write a controller or the
	// host makes in between stores under an erased key again (review II,
	// R2-P6-conc-2405, N-finp-3). A key forgotten and added again in the
	// batch is held out of the first reassembly and served by a second
	// one, after the erasure, so it starts as a new device would — and so
	// is every served endpoint, of any partition, whose own key or a part
	// key the erasure reaches, however it came to serve it (review II,
	// R2-C2-store-2401, R3-store-3702); the erasure leaves such an
	// endpoint's records, so it is served again at the number it had
	// (decision 560) — only a key the batch forgot and serves anew starts
	// with a fresh one.
	pre, erase := splitErasures(b.ops)
	resErr := p.a.applyReservations(ctx, pre, nil)
	served, cancel, keep, findErr := p.a.servedInErasure(ctx, erase)
	resErr = errors.Join(resErr, findErr)
	p.a.mu.Lock()
	started := p.a.started
	p.a.mu.Unlock()
	var err error
	var restore func() bool
	if (b.changed || len(served) > 0) && started {
		restore = p.a.holdBack(served)
		err = p.a.reassemble(ctx)
	}
	// Unbound after the reassembly: no controller reaches them any more.
	for _, d := range b.detach {
		d()
	}
	resErr = errors.Join(resErr, p.a.applyReservations(ctx, erase, keep))
	// A key forgotten and added again in this batch is the new device's
	// once served: an erasure that did not finish must not take it at the
	// next New.
	for _, k := range cancel {
		resErr = errors.Join(resErr, p.a.cancelForget(ctx, k))
	}
	if restore != nil && restore() {
		err = errors.Join(err, p.a.reassemble(ctx))
	}
	err = errors.Join(resErr, err)
	switch {
	case fnErr == nil:
		return err
	case err == nil:
		return fnErr
	default:
		return errors.Join(fnErr, err)
	}
}

// Add adds ep to the batch's partition; see [Aggregator.Add]. An Add of
// a key the set holds, or of a device another endpoint serves a key of,
// is held and judged once fn returned ([Batch], decisions 582, 585).
func (b *Batch) Add(ep Endpoint) error {
	if b.isDone() {
		return ErrBatchDone
	}
	key, err := keyOf(ep.Spec())
	if err != nil {
		return err
	}
	a := b.a
	entries, err := a.n.checkSurface(ep)
	if err != nil {
		return err
	}
	if err := a.n.checkMatterTime(ep); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if b.done {
		return ErrBatchDone
	}
	if b.pendingIndex(key, heldAdd) >= 0 {
		return fmt.Errorf("%w: %s", ErrDuplicateKey, key)
	}
	// A key the set holds is judged at the end like a key another
	// endpoint serves: a Remove or Forget of it in the batch, before or
	// after, lets the device in (decision 585).
	if p, _ := a.findLocked(key); p != nil || b.collidesLocked(entries, nil) {
		b.pending = append(b.pending, pendingCall{kind: heldAdd, key: key, ep: ep, entries: entries})
		return nil
	}
	return b.addLocked(key, ep, entries)
}

// addLocked puts ep under key into the batch's partition: entries are
// its surface, checked; a.mu is held.
func (b *Batch) addLocked(key string, ep Endpoint, entries []surfaceEntry) error {
	a := b.a
	// Withheld only once the set takes the key, and never for a key it
	// serves (review II, R2-P6-store-2414).
	if err := a.n.recordWithheld(b.ctx, entries, a.servesLocked); err != nil {
		return err
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
	b.taken = append(b.taken, takenCall{key: key, ep: ep, oldAt: -1})
	return nil
}

// Replace replaces the endpoint with key in the batch's partition; see
// [Aggregator.Replace]. The old endpoint is unbound after the batch's
// reassembly. A replacement with a part key another endpoint serves is
// held and judged against the batch's end state: refused
// ([ErrDuplicateKey], from [Partition.Batch]) and changing nothing when
// another endpoint still serves the key once fn returned (decisions 580,
// 582); the replaced endpoint's own parts are not another's. A Replace of
// a key the set does not hold but the batch's Add holds replaces the held
// device; of a key the set holds, it replaces the set's endpoint and a
// held Add of the key stays held ([Batch], decision 585).
func (b *Batch) Replace(key device.SourceKey, ep Endpoint) error {
	if b.isDone() {
		return ErrBatchDone
	}
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
	entries, err := a.n.checkSurface(ep)
	if err != nil {
		return err
	}
	if err := a.n.checkMatterTime(ep); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if b.done {
		return ErrBatchDone
	}
	part, i := a.findLocked(newKey)
	inSet := part != nil && part.scope == b.scope
	if j := b.pendingIndex(newKey, heldAdd); j >= 0 && part == nil {
		// The batch's held Add of a key the set does not hold: the
		// replacement is held in its place.
		b.pending[j].ep, b.pending[j].entries = ep, entries
		return nil
	}
	if j := b.pendingIndex(newKey, heldReplace); j >= 0 {
		// A later Replace supersedes the held one.
		b.pending = slices.Delete(b.pending, j, j+1)
	}
	if !inSet {
		return fmt.Errorf("%w: %s", ErrUnknownKey, key)
	}
	if part.eps[i] == ep {
		return nil
	}
	if b.collidesLocked(entries, part.eps[i]) {
		b.pending = append(b.pending, pendingCall{kind: heldReplace, key: newKey, ep: ep, entries: entries})
		return nil
	}
	return b.replaceLocked(part, i, newKey, ep, entries)
}

// replaceLocked puts ep in the place of the endpoint at i of part, under
// key: entries are its surface, checked; a.mu is held.
func (b *Batch) replaceLocked(part *partition, i int, newKey string, ep Endpoint, entries []surfaceEntry) error {
	a := b.a
	if len(withheldKeys(entries)) > 0 {
		return b.replaceWithheldLocked(part, i, newKey, entries)
	}
	if err := a.n.recordWithheld(b.ctx, entries, a.servesLocked); err != nil {
		return err
	}
	old := a.detach[newKey]
	detach, err := a.attach(ep)
	if err != nil {
		return err
	}
	t := takenCall{key: newKey, ep: ep, old: part.eps[i], oldDetach: old, oldAt: -1}
	part.eps[i] = ep
	a.detachSetLocked(newKey, detach)
	if old != nil {
		t.oldAt = len(b.detach)
		b.detach = append(b.detach, old)
	}
	b.changed = true
	b.taken = append(b.taken, t)
	return nil
}

// replaceWithheldLocked is Replace by a declaration the specification
// level withholds (decision 550, #585): the served endpoint at i is
// dropped as [Batch.Remove] drops it — its number and its parts' kept for
// the key's return — and the declaration recorded as withheld for
// [Node.Surface], in one step. A key another endpoint serves, withheld or
// not, never reaches it: [Batch.Replace] holds it and [Partition.Batch]
// refuses it with [ErrDuplicateKey] while another endpoint serves the key
// (decisions 580, 582; before, a withheld one refused here, review II,
// R2-P6-store-2414). It returns the *surface.WithheldError; a.mu is held.
func (b *Batch) replaceWithheldLocked(part *partition, i int, key string, entries []surfaceEntry) error {
	a := b.a
	old := part.eps[i]
	err := a.n.recordWithheld(b.ctx, entries, func(k string) bool { return a.servesExceptLocked(k, old) })
	b.dropLocked(part, i, key)
	b.ops = append(b.ops, reservationOp{kind: opReserve, key: key, tree: reservationTree(old.Spec())})
	return err
}

// withheldKeys is the keys of the entries the specification level
// withheld.
func withheldKeys(entries []surfaceEntry) []string {
	var out []string
	for i := range entries {
		if r := entries[i].reporter; r != nil && len(r.Surface().Withheld) > 0 {
			out = append(out, entries[i].key)
		}
	}
	return out
}

// Remove drops the endpoint with key from the batch's partition; see
// [Aggregator.Remove]. The device is unbound after the batch's
// reassembly. A key the specification level withheld (its Add returned
// [surface.ErrWithheld]; no partition holds it) is known: Remove drops
// its withheld record from [Node.Surface] and returns nil. A Remove of a
// key the set holds removes the set's endpoint, and a held Add of the key
// stays held; of a key only the batch's held Add holds, it drops that Add
// ([Batch], decision 585).
func (b *Batch) Remove(key device.SourceKey) error {
	if key == nil {
		return ErrNoStableKey
	}
	a := b.a
	a.mu.Lock()
	if b.done {
		a.mu.Unlock()
		return ErrBatchDone
	}
	part, i := a.findLocked(key.String())
	if part == nil && b.dropPendingLocked(key.String(), heldAdd) {
		// The batch's own held Add: dropped, the set never held it; a
		// withheld record of the key from before the batch goes too, as
		// an applied Add's served declaration would have cleared it
		// (fable-diff-g5f N5).
		a.n.dropWithheld(key.String())
		a.mu.Unlock()
		return nil
	}
	if part == nil && a.n.dropWithheld(key.String()) {
		a.mu.Unlock()
		return nil
	}
	if part == nil || part.scope != b.scope {
		a.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrUnknownKey, key)
	}
	removed := part.eps[i]
	// A held Replace of the key goes with the endpoint it replaces.
	b.dropPendingLocked(key.String(), heldReplace)
	b.dropLocked(part, i, key.String())
	// Appended under a.mu, as Add and Forget do: a.mu guards b.ops
	// against a call racing the end of fn (fable-diff-g5g N4).
	b.ops = append(b.ops, reservationOp{kind: opReserve, key: key.String(), tree: reservationTree(removed.Spec())})
	a.mu.Unlock()
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
	// A take of the key is not the set's any more: not judged again.
	b.taken = slices.DeleteFunc(b.taken, func(t takenCall) bool { return t.key == key })
}

// pendingIndex is the index of the batch's held call of kind on key, or
// -1; a.mu is held.
func (b *Batch) pendingIndex(key string, kind pendingKind) int {
	return slices.IndexFunc(b.pending, func(t pendingCall) bool { return t.key == key && t.kind == kind })
}

// dropPendingLocked drops the batch's held call of kind on key, if any,
// and reports whether there was one; a.mu is held.
func (b *Batch) dropPendingLocked(key string, kind pendingKind) bool {
	j := b.pendingIndex(key, kind)
	if j < 0 {
		return false
	}
	b.pending = slices.Delete(b.pending, j, j+1)
	return true
}

// isDone reports whether fn returned ([ErrBatchDone]).
func (b *Batch) isDone() bool {
	b.a.mu.Lock()
	defer b.a.mu.Unlock()
	return b.done
}

// collidesLocked reports whether a key of entries — an endpoint and its
// parts — is served by an endpoint of the set other than except (and its
// parts), or declared by a take the batch holds: such a take is held to
// the end of the batch (decision 582). a.mu is held.
func (b *Batch) collidesLocked(entries []surfaceEntry, except Endpoint) bool {
	if _, ok := b.a.servedOfLocked(entries, except); ok {
		return true
	}
	for _, t := range b.pending {
		if t.kind == heldForget {
			continue
		}
		for i := range t.entries {
			for j := range entries {
				if t.entries[i].key == entries[j].key {
					return true
				}
			}
		}
	}
	return false
}

// commitPending ends the batch's calls ([ErrBatchDone] from here on) and
// judges the calls the batch held against the set fn left, in call order
// (decisions 582, 585). A held Add or Replace is judged by its device as
// it is now — its surface computed and checked again, so a part added
// during fn counts (fable-diff-g5f N1): applied as at its call when none
// of its keys is the set's or another endpoint's, refused with
// [ErrDuplicateKey] otherwise and nothing of it changed (a set that took
// it would assemble two Specs under one key, decision 580). A held
// Replace whose key the partition no longer holds is [ErrUnknownKey]. A
// held Forget is applied when no other endpoint serves its key any more,
// refused with [ErrUnknownKey] otherwise (decision 567).
func (b *Batch) commitPending() error {
	a := b.a
	a.mu.Lock()
	defer a.mu.Unlock()
	b.done = true
	pending := b.pending
	b.pending = nil
	var errs []error
	for _, t := range pending {
		switch t.kind {
		case heldForget:
			errs = append(errs, b.commitForgetLocked(t))
		case heldAdd:
			entries, err := b.recheckLocked(t.ep)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if p, _ := a.findLocked(t.key); p != nil {
				errs = append(errs, fmt.Errorf("%w: %s", ErrDuplicateKey, t.key))
				continue
			}
			if k, ok := a.servedOfLocked(entries, nil); ok {
				errs = append(errs, fmt.Errorf("%w: %s", ErrDuplicateKey, k))
				continue
			}
			errs = append(errs, b.addLocked(t.key, t.ep, entries))
		case heldReplace:
			part, i := a.findLocked(t.key)
			if part == nil || part.scope != b.scope {
				errs = append(errs, fmt.Errorf("%w: %s", ErrUnknownKey, t.key))
				continue
			}
			if part.eps[i] == t.ep {
				continue
			}
			entries, err := b.recheckLocked(t.ep)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if k, ok := a.servedOfLocked(entries, part.eps[i]); ok {
				errs = append(errs, fmt.Errorf("%w: %s", ErrDuplicateKey, k))
				continue
			}
			errs = append(errs, b.replaceLocked(part, i, t.key, t.ep, entries))
		}
	}
	errs = append(errs, b.refuseLateCollisionsLocked()...)
	return errors.Join(errs...)
}

// refuseLateCollisionsLocked judges every Add and Replace the batch
// applied once more, its device as it is at the end of fn: a part added
// to it during fn whose key — or its own key — another endpoint serves
// would make the batch's reassembly, and every later one, assemble two
// Specs under one key (decision 580). Such a take is refused with
// [ErrDuplicateKey] and undone: an Add leaves the set and keeps the key's
// reservation, a Replace puts the endpoint it replaced back (decision 585:
// no path wedges; fable-diff-g5g N1). The takes are judged from the last
// to the first, so of two that collide the later one goes. A take the
// batch removed or replaced again is not the set's and not judged. a.mu
// is held.
func (b *Batch) refuseLateCollisionsLocked() []error {
	a := b.a
	var errs []error
	for j := len(b.taken) - 1; j >= 0; j-- {
		t := b.taken[j]
		part, i := a.findLocked(t.key)
		if part == nil || !sameEndpoint(part.eps[i], t.ep) {
			continue
		}
		k, ok := a.servedOfExceptAtLocked(surfaceEntries(t.ep), part, i)
		if !ok {
			continue
		}
		errs = append(errs, fmt.Errorf("%w: %s", ErrDuplicateKey, k))
		if d := a.detach[t.key]; d != nil {
			b.detach = append(b.detach, d)
		}
		if t.old == nil {
			part.eps = slices.Delete(part.eps, i, i+1)
			a.detachSetLocked(t.key, nil)
			// Its release goes too: the key's reservation stays.
			for r := len(b.ops) - 1; r >= 0; r-- {
				if b.ops[r].kind == opRelease && b.ops[r].key == t.key {
					b.ops = slices.Delete(b.ops, r, r+1)
					break
				}
			}
			continue
		}
		part.eps[i] = t.old
		a.detachSetLocked(t.key, t.oldDetach)
		if t.oldAt >= 0 {
			// The replaced device stays bound: it is served again.
			b.detach[t.oldAt] = func() {}
		}
	}
	return errs
}

// recheckLocked runs the checks of a take's call again on ep as it is at
// commit and returns its surface entries; a.mu is held.
func (b *Batch) recheckLocked(ep Endpoint) ([]surfaceEntry, error) {
	entries, err := b.a.n.checkSurface(ep)
	if err != nil {
		return nil, err
	}
	if err := b.a.n.checkMatterTime(ep); err != nil {
		return nil, err
	}
	return entries, nil
}

// servesLocked reports whether the set holds an endpoint, or a part of
// one, under key; a.mu is held.
func (a *Aggregator) servesLocked(key string) bool {
	for _, p := range a.parts {
		for _, ep := range p.eps {
			entries := surfaceEntries(ep)
			for i := range entries {
				if entries[i].key == key {
					return true
				}
			}
		}
	}
	return false
}

// servedOfLocked returns the first key of entries — an endpoint and its
// parts — that an endpoint of the set serves, itself or as a part, with
// except and its parts left out (nil: none). A set that took such an
// endpoint would assemble two Specs under one key, and that assembly and
// every later one would fail until the endpoint went (decision 580).
// a.mu is held.
func (a *Aggregator) servedOfLocked(entries []surfaceEntry, except Endpoint) (string, bool) {
	for i := range entries {
		if a.servesExceptLocked(entries[i].key, except) {
			return entries[i].key, true
		}
	}
	return "", false
}

// servedOfExceptAtLocked is servedOfLocked with the endpoint at i of part
// (and its parts) left out, named by its place rather than compared, so
// an endpoint of an uncomparable type is judged as any other; a.mu is
// held.
func (a *Aggregator) servedOfExceptAtLocked(entries []surfaceEntry, part *partition, i int) (string, bool) {
	for e := range entries {
		for _, p := range a.parts {
			for j, ep := range p.eps {
				if p == part && j == i {
					continue
				}
				served := surfaceEntries(ep)
				for s := range served {
					if served[s].key == entries[e].key {
						return entries[e].key, true
					}
				}
			}
		}
	}
	return "", false
}

// sameEndpoint reports whether x and y are the same endpoint. An
// endpoint of an uncomparable type cannot be told from another of its
// type and counts as the same: the caller has matched it by its key.
func sameEndpoint(x, y Endpoint) bool {
	tx := reflect.TypeOf(x)
	if tx != reflect.TypeOf(y) {
		return false
	}
	if tx == nil || !tx.Comparable() {
		return true
	}
	return x == y
}

// servesExceptLocked is servesLocked with the endpoint except (and its
// parts) left out; a.mu is held.
func (a *Aggregator) servesExceptLocked(key string, except Endpoint) bool {
	for _, p := range a.parts {
		for _, ep := range p.eps {
			if ep == except {
				continue
			}
			entries := surfaceEntries(ep)
			for i := range entries {
				if entries[i].key == key {
					return true
				}
			}
		}
	}
	return false
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

// IncreaseConfigurationVersions raises, as one change of the node, the
// ConfigurationVersion of the bridged endpoints with keys — each physical
// device once — and the node's own once, after them, and persists every
// version raised in one pass, so a version never decreases across a
// restart. The write runs on ctx's values without its cancellation — a
// version served is stored even when ctx ends first — and a write the
// store refuses is returned, after the versions were raised and served.
// With no key it raises the node's alone. Before the
// node assembled its topology ([Node.Start]) it fails with
// [ErrNotStarted]; an unknown key fails with [ErrUnknownKey] before
// anything is raised. Mirrors
// matter.js BasicInformationServer.increaseConfigurationVersion(change)
// (BasicInformationServer.ts:132-138: the change first, one increment
// afterwards) with the bridged devices raised under the shared context
// (BridgedDeviceBasicInformationServer.ts:72-90); the bridge's
// IncreaseNodeConfigurationVersionWith does the raising.
func (a *Aggregator) IncreaseConfigurationVersions(ctx context.Context, keys ...device.SourceKey) error {
	topo := a.n.br.Topology()
	if topo == nil {
		return ErrNotStarted
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
	// The versions are served already: their write must not depend on
	// the caller's context staying alive, or a version served now would
	// go back down after a restart.
	return a.n.persisted.storeConfigVersions(context.WithoutCancel(ctx), topo, versions)
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
	a.n.attachCameraPeers()
	a.n.announceSurface(ctx)
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
			// A store that fails a read fails the assembly: the default
			// served in the stored value's place would be stored over it at
			// the next write, a version read as 0 would go down (review II,
			// R2-P6-store-2403). The topology before stays served.
			label, err := a.n.endpoints.LoadNodeLabel(ctx, spec.StableKey)
			if err != nil {
				return nil, err
			}
			if label != "" {
				spec.NodeLabel = label
			}
			version, err := a.n.persisted.configVersion(ctx, spec.StableKey)
			if err != nil {
				return nil, err
			}
			spec.ConfigurationVersion = max(spec.ConfigurationVersion, version)
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
