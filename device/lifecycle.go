// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/internal/deviceseam"
)

// The lifecycle of a device (ADR 0025 principle 2: add, reachable, rename,
// replace, remove): the node's Aggregator adds, replaces and removes it;
// the device itself says whether it is reachable and what it is called,
// and knows its endpoint number once it is added. Mirrors matter.js's
// bridged endpoint, whose BridgedDeviceBasicInformation state the host
// sets and whose number the node assigns when it is added; its server
// reacts to a Reachable change with the event
// (packages/node/src/behaviors/bridged-device-basic-information/
// BridgedDeviceBasicInformationServer.ts:37 reactTo, :100-102
// #emitReachableChange).

// ErrAddedElsewhere is the refusal to add a device a node holds already to
// another, or to make it a part ([Device.AddPart]) while a node holds it
// as a bridged device.
var ErrAddedElsewhere = errors.New("device: the device is added to a node already")

// ErrNotBridged is the refusal of [Device.SetReachable], [Device.Rename]
// and [Device.Update] on a part ([Device.AddPart]): reachability, the
// NodeLabel and the host's statements are the bridged device's
// BridgedDeviceBasicInformation, which only the endpoint its node adds
// under the Aggregator serves — a part serves none of its own
// (R2-P6-api-2503).
var ErrNotBridged = errors.New("device: a part has no BridgedDeviceBasicInformation of its own")

func init() {
	deviceseam.AheadRows = aheadRows
	deviceseam.Composing = func(fn func()) {
		composeMu.Lock()
		defer composeMu.Unlock()
		fn()
	}
	deviceseam.Attach = func(ep any, h deviceseam.Host) (func(), bool, error) {
		owner, ok := ep.(interface{ base() *Device })
		if !ok {
			return nil, false, nil
		}
		d := owner.base()
		if err := d.attach(h); err != nil {
			return nil, true, err
		}
		return d.detach, true, nil
	}
}

// base returns the device; a generated type that embeds it promotes it,
// which is how the node finds the device behind any of them.
func (d *Device) base() *Device { return d }

// composeMu serialises every change of a device tree's structure — a
// part's owner, a device's parts, the node a tree is bound to — so a check
// and the change it guards are one step: no cycle forms between two
// concurrent AddPart calls (R2-P6-conc-2521), and an attach sees the tree
// it binds whole (R2-P6-fsm-2501). It is held only while the structure
// changes, never while a host is called or a queue drains. Lock order:
// composeMu before a device's mu.
var composeMu sync.Mutex

// attach binds d and its parts to h: all of them, or — when one is bound
// to a node already — none, leaving every device as it was (R2-P6-fsm-2501:
// a refused attach unbound the parts another node held). A part is bound
// with its owner, never on its own.
func (d *Device) attach(h deviceseam.Host) error {
	composeMu.Lock()
	if o := d.owner.Load(); o != nil {
		composeMu.Unlock()
		return fmt.Errorf("%w: %s is a part of %s; a part is added with its owner", ErrComposition, d.opts.StableKey, o.opts.StableKey)
	}
	tree := d.treeLocked()
	for _, x := range tree {
		if x.hostOf() != nil {
			composeMu.Unlock()
			return fmt.Errorf("%w: %s", ErrAddedElsewhere, x.opts.StableKey)
		}
	}
	for _, x := range tree {
		x.setHost(h)
	}
	composeMu.Unlock()
	for _, x := range tree {
		x.start()
	}
	return nil
}

// detach unbinds d and the parts it holds now, then stops their queues —
// each once it ran what it holds — and their subscriptions on host ports.
// A part moved to another owner since ([Device.AddPart]) is no longer in
// d's tree and is not touched.
func (d *Device) detach() {
	composeMu.Lock()
	tree := d.treeLocked()
	for _, x := range tree {
		x.setHost(nil)
	}
	composeMu.Unlock()
	for _, x := range tree {
		x.stop()
	}
}

// treeLocked is d and its descendants, d first; composeMu is held.
func (d *Device) treeLocked() []*Device {
	out := []*Device{d}
	for _, p := range d.Parts() {
		out = append(out, p.treeLocked()...)
	}
	return out
}

// setHost records the node d is bound to; nil unbinds it.
func (d *Device) setHost(h deviceseam.Host) {
	d.mu.Lock()
	d.host = h
	d.mu.Unlock()
}

// start runs d on the node it is bound to: the node's clock, the device's
// queue and the subscriptions its servers hold on host ports
// (defaultBuild.bind). A device bound again while it runs only takes the
// new node's clock; one unbound meanwhile is not started.
func (d *Device) start() {
	d.life.Lock()
	defer d.life.Unlock()
	h := d.hostOf()
	if h == nil {
		return
	}
	d.clock.setNode(h.Clock())
	if d.running {
		return
	}
	d.running = true
	d.queue.Start()
	d.subscribeLocked()
}

// stop ends what start began, unless d is bound again meanwhile: the
// subscriptions first, then the queue, which runs what it holds.
func (d *Device) stop() {
	d.life.Lock()
	defer d.life.Unlock()
	if !d.running || d.hostOf() != nil {
		return
	}
	d.running = false
	unbind := d.unbind
	d.unbind, d.subscribed = nil, false
	for _, u := range unbind {
		if u != nil {
			u()
		}
	}
	d.queue.Stop()
}

// subscribe makes the subscriptions the device's servers hold on host
// ports (defaultBuild.bind): New calls it once the device is built — a
// refused device makes none — and start again for a device a detach
// ended them for (R2-P6-store-2515).
func (d *Device) subscribe() {
	d.life.Lock()
	defer d.life.Unlock()
	d.subscribeLocked()
}

func (d *Device) subscribeLocked() {
	if d.subscribed {
		return
	}
	d.subscribed = true
	unbind := make([]func(), 0, len(d.bindings))
	for _, subscribe := range d.bindings {
		unbind = append(unbind, subscribe())
	}
	d.unbind = unbind
}

// isPart reports whether d is a part of another device.
func (d *Device) isPart() bool { return d.owner.Load() != nil }

// hostOf returns the node the device is added to; nil before.
func (d *Device) hostOf() deviceseam.Host {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.host
}

// EndpointNumber returns the endpoint number the node serves the device
// under — matter.js's `endpoint.number` — and false before the device is
// added and the node assembled it (node.Aggregator.Add on a started node,
// or node.Node.Start after the Add). A part has a number of its own.
func (d *Device) EndpointNumber() (uint16, bool) {
	h := d.hostOf()
	if h == nil {
		return 0, false
	}
	return h.EndpointNumber(d.opts.StableKey)
}

// SetReachable says whether the bridged device is reachable: its
// BridgedDeviceBasicInformation Reachable attribute, and on a change the
// ReachableChanged event a subscribed controller receives — as a matter.js
// host sets the state and the server emits the event
// (BridgedDeviceBasicInformationServer.ts:37 reactTo, :100-102
// #emitReachableChange). A device starts reachable, as matter.js's State
// does (BridgedDeviceBasicInformationServer.ts:129-132). It is the one
// reachability input: a host whose source goes away calls it, as a
// matter.js host sets the one reachable state (ADR 0025 principle 2).
// Before the device is added only the value changes. Concurrent calls tell
// the host in the order they change the state, so the last value the host
// hears is the device's; no lock is held while the host is told, so a host
// whose notification sets the reachability again (a node observer of the
// ReachableChanged event) returns. A call that finds another telling the
// host leaves its notification to that one, in order and under that call's
// ctx, and returns before the host heard it (review II, R2-P1-dr-rest-06).
//
// On a part ([Device.AddPart]) it returns [ErrNotBridged] and changes
// nothing: a part serves no Reachable of its own (R2-P6-api-2503).
func (d *Device) SetReachable(ctx context.Context, reachable bool) error {
	if d.isPart() {
		return fmt.Errorf("%w: SetReachable of %s", ErrNotBridged, d.opts.StableKey)
	}
	d.reachMu.Lock()
	if d.reachable.Swap(reachable) == reachable {
		d.reachMu.Unlock()
		return nil
	}
	d.reachPending = append(d.reachPending, reachable)
	if d.reachPublishing {
		d.reachMu.Unlock()
		return nil
	}
	d.reachPublishing = true
	locked := true
	defer func() { // a host's panic leaves the next call to tell the host
		if !locked {
			d.reachMu.Lock()
		}
		d.reachPublishing = false
		d.reachMu.Unlock()
	}()
	for len(d.reachPending) > 0 {
		next := d.reachPending[0]
		d.reachPending = d.reachPending[1:]
		locked = false
		d.reachMu.Unlock()
		if h := d.hostOf(); h != nil {
			h.NotifyReachable(ctx, d.opts.StableKey, next)
		}
		d.reachMu.Lock()
		locked = true
	}
	return nil
}

// Reachable reports whether the device is reachable: what
// [Device.SetReachable] set last, true before the first call.
func (d *Device) Reachable() bool { return d.available() }

// available is what the endpoint's availability probe reads at each
// dispatch: the state [Device.SetReachable] set.
func (d *Device) available() bool { return d.reachable.Load() }

// Rename names the device name: its BridgedDeviceBasicInformation
// NodeLabel, reported to a subscribed controller and persisted by the node
// as a controller's write of it is, so it survives a restart — a written
// label, which a later [Device.Update] of the host's name does not
// replace. The Name of the device's [Options] becomes name, capped at the
// NodeLabel maximum ([ComposeNodeLabel]). Before the device is added only
// the name changes.
//
// The node stores the label before it serves it, as it stores a
// controller's write: when its store refuses, Rename returns the refusal,
// the label is not served and the device keeps its name — a label the
// host believes saved is never lost on the next restart (R2-P5-apidev-16).
// On a part ([Device.AddPart]) Rename returns [ErrNotBridged] and changes
// nothing (R2-P6-api-2503).
func (d *Device) Rename(ctx context.Context, name string) error {
	if d.isPart() {
		return fmt.Errorf("%w: Rename of %s", ErrNotBridged, d.opts.StableKey)
	}
	name = ComposeNodeLabel(name, "")
	if h := d.hostOf(); h != nil {
		if err := h.Rename(ctx, d.opts.StableKey, name); err != nil {
			return fmt.Errorf("device: rename %s: %w", d.opts.StableKey, err)
		}
	}
	d.mu.Lock()
	d.opts.Name = name
	d.mu.Unlock()
	return nil
}

// HostStatement are the facts of a device's [Options] the host states and
// may change while the device is bound — what the host's own model says
// about the device, as against what a controller writes to it:
//
//   - Name: the name the device's NodeLabel serves as long as no label is
//     written. A controller's write of NodeLabel, and [Device.Rename],
//     persist a label that wins over it, across restarts.
//   - DeviceAddress, ChannelAddress: the host's addresses of the physical
//     device and of this function of it.
//
// Reachability is not a statement: [Device.SetReachable] is its one input.
//
// StableKey, Vendor and Conditions are the device's identity and type and
// do not change while it is bound; a host that changes them replaces the
// device (node.Aggregator.Replace).
type HostStatement struct {
	Name                          string
	DeviceAddress, ChannelAddress string
}

// Update applies a change of the host's statements: fn gets the current
// ones and changes what changed. The new Name (capped at the NodeLabel
// maximum, [ComposeNodeLabel]) is served at once to a subscribed
// controller unless a written label wins, and persisted nowhere, so the
// next restart starts from the host's statement again. The addresses
// reach the endpoint at the node's next reassembly. The device stays bound: [Device.EndpointNumber] and
// [Device.SetReachable] keep working. Before the device is added only the
// statements change.
//
// fn runs without a lock of the device, so it may ask the device anything
// (R2-P6-conc-2504); only the statements fn changed are written back, so a
// [Device.Rename] or another Update that lands while fn runs keeps what it
// set unless fn changed the same statement. On a part ([Device.AddPart])
// Update returns [ErrNotBridged], calls no fn and changes nothing
// (R2-P6-api-2503).
//
// matter.js has no such split: every change of a persistent attribute is
// stored, whoever made it (packages/node/src/behavior/state/managed/Datasource.ts:1442-1453
// at db021da0; NodeLabel is nonvolatile, basic-information.element.ts:36-37),
// and the stored value replaces the endpoint's configured state when the
// node restarts (Datasource.ts:580-583). The host's statement is the
// configured state here — changed without becoming the stored one.
func (d *Device) Update(ctx context.Context, fn func(*HostStatement)) error {
	if fn == nil {
		return nil
	}
	if d.isPart() {
		return fmt.Errorf("%w: Update of %s", ErrNotBridged, d.opts.StableKey)
	}
	d.mu.Lock()
	was := HostStatement{
		Name: d.opts.Name, DeviceAddress: d.opts.DeviceAddress,
		ChannelAddress: d.opts.ChannelAddress,
	}
	d.mu.Unlock()
	st := was
	fn(&st)
	st.Name = ComposeNodeLabel(st.Name, "")
	renamed := st.Name != was.Name
	d.mu.Lock()
	if renamed {
		d.opts.Name = st.Name
	}
	if st.DeviceAddress != was.DeviceAddress {
		d.opts.DeviceAddress = st.DeviceAddress
	}
	if st.ChannelAddress != was.ChannelAddress {
		d.opts.ChannelAddress = st.ChannelAddress
	}
	h := d.host
	d.mu.Unlock()
	if h != nil && renamed {
		h.HostName(ctx, d.opts.StableKey, st.Name)
	}
	return nil
}

// ComposeNodeLabel is the NodeLabel of a bridged endpoint named base with
// the parameter suffix — "base (suffix)", or base alone for an empty
// suffix — capped at the NodeLabel maximum: 32 UTF-8 bytes, the generated
// definition's constraint, cut at a rune boundary, the suffix dropped
// first. [New], [Device.Rename] and [Device.Update] cap a name with it,
// so a host composes a label with it and never re-derives the cap.
func ComposeNodeLabel(base, suffix string) string { return endpoint.ComposeNodeLabel(base, suffix) }

// OnChanged calls fn with every change of the device's clusterID server —
// matter.js's `events.<cluster>.<attribute>$Changed` (endpoint/Endpoint.ts:457)
// with the origin of the change (contract.Change; Datasource.ts:1486-1492
// at db021da0 hands the observer the session that made it) — delivered
// through the
// server's [contract.AttributeChangeNotifier], or through its
// [contract.ChangeNotifier] with every attribute it reports. fn runs
// inline, on the goroutine that made the change and before the write,
// command or Set that made it returns; [Device.OnChangedAsync] queues it.
// It is the untyped form of the typed accessor's OnChanged — one hook, two
// signatures. It returns the unsubscribe function. A cluster the device
// does not mount never calls fn (Binding reports each change of its list,
// the per-fabric entries through core.Binding.OnChange); a server that reads
// its state from a host port ([contract.ChangeNotifier] only — valve,
// fan, pump, SmokeCoAlarm, ModeSelect) calls fn when that port notifies,
// with [contract.OriginUnknown].
func (d *Device) OnChanged(clusterID uint32, fn func(change contract.Change)) (unsubscribe func()) {
	srv := d.Server(clusterID)
	if srv == nil || fn == nil {
		return func() {}
	}
	return spec.ChangesOf(srv, fn)
}

// OnChangedAsync is [Device.OnChanged] with fn queued on the device's
// goroutine: the change is taken when the server reports it, and fn runs
// later, one call after the other, while the device is added to a running
// node. A change reported while it is not added is not delivered.
func (d *Device) OnChangedAsync(clusterID uint32, fn func(change contract.Change)) (unsubscribe func()) {
	if fn == nil {
		return func() {}
	}
	q := d.queue
	return d.OnChanged(clusterID, func(c contract.Change) { q.Enqueue(func() { fn(c) }) })
}
