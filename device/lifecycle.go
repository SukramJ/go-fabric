// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"context"
	"errors"
	"fmt"

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
// another.
var ErrAddedElsewhere = errors.New("device: the device is added to a node already")

func init() {
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

// attach binds d and its parts to h.
func (d *Device) attach(h deviceseam.Host) error {
	d.mu.Lock()
	if d.host != nil {
		d.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrAddedElsewhere, d.opts.StableKey)
	}
	d.host = h
	parts := d.parts
	d.mu.Unlock()
	d.clock.setNode(h.Clock())
	d.queue.start()
	for _, p := range parts {
		if err := p.attach(h); err != nil {
			d.detach()
			return err
		}
	}
	return nil
}

// detach stops d's and its parts' queues, each once it ran what it holds,
// and unbinds them.
func (d *Device) detach() {
	d.mu.Lock()
	d.host = nil
	parts := d.parts
	d.mu.Unlock()
	d.queue.stop()
	for _, p := range parts {
		p.detach()
	}
}

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
// does (BridgedDeviceBasicInformationServer.ts:129-132). [Options].Reachable, when set, still decides as
// well: the device is reachable while both say so. Before the device is
// added only the value changes.
func (d *Device) SetReachable(ctx context.Context, reachable bool) {
	if d.reachable.Swap(reachable) == reachable {
		return
	}
	if h := d.hostOf(); h != nil {
		h.NotifyReachable(ctx, d.opts.StableKey, d.available())
	}
}

// Reachable reports whether the device is reachable: what [Device.SetReachable]
// set, and [Options].Reachable when it is set.
func (d *Device) Reachable() bool { return d.available() }

// available is the device's availability probe.
func (d *Device) available() bool {
	if !d.reachable.Load() {
		return false
	}
	d.mu.Lock()
	probe := d.opts.Reachable
	d.mu.Unlock()
	return probe == nil || probe()
}

// Rename names the device name: its BridgedDeviceBasicInformation
// NodeLabel, reported to a subscribed controller and persisted by the node
// as a controller's write of it is, so it survives a restart — a written
// label, which a later [Device.Update] of the host's name does not
// replace. The Name of the device's [Options] becomes name, capped at the
// NodeLabel maximum ([ComposeNodeLabel]). Before the device is added only
// the name changes.
func (d *Device) Rename(ctx context.Context, name string) {
	name = ComposeNodeLabel(name, "")
	d.mu.Lock()
	d.opts.Name = name
	h := d.host
	d.mu.Unlock()
	if h != nil {
		h.Rename(ctx, d.opts.StableKey, name)
	}
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
//   - Reachable: the availability probe.
//
// StableKey, Vendor and Conditions are the device's identity and type and
// do not change while it is bound; a host that changes them replaces the
// device (node.Aggregator.Replace).
type HostStatement struct {
	Name                          string
	DeviceAddress, ChannelAddress string
	Reachable                     AvailabilityProbe
}

// Update applies a change of the host's statements: fn gets the current
// ones and changes what changed. The new Name (capped at the NodeLabel
// maximum, [ComposeNodeLabel]) is served at once to a subscribed
// controller unless a written label wins, and persisted nowhere, so the
// next restart starts from the host's statement again. The addresses
// reach the endpoint at the node's next reassembly, the probe at its next
// read. The device stays bound: [Device.EndpointNumber] and
// [Device.SetReachable] keep working. Before the device is added only the
// statements change.
//
// matter.js has no such split: every change of a persistent attribute is
// stored, whoever made it (packages/node/src/behavior/state/managed/Datasource.ts:1427-1438
// at 862ad75f; NodeLabel is nonvolatile, basic-information.element.ts:36-37),
// and the stored value replaces the endpoint's configured state when the
// node restarts (Datasource.ts:580-583). The host's statement is the
// configured state here — changed without becoming the stored one.
func (d *Device) Update(ctx context.Context, fn func(*HostStatement)) {
	if fn == nil {
		return
	}
	d.mu.Lock()
	st := HostStatement{
		Name: d.opts.Name, DeviceAddress: d.opts.DeviceAddress,
		ChannelAddress: d.opts.ChannelAddress, Reachable: d.opts.Reachable,
	}
	fn(&st)
	st.Name = ComposeNodeLabel(st.Name, "")
	renamed := st.Name != d.opts.Name
	d.opts.Name, d.opts.DeviceAddress, d.opts.ChannelAddress, d.opts.Reachable = st.Name, st.DeviceAddress, st.ChannelAddress, st.Reachable
	h := d.host
	d.mu.Unlock()
	if h != nil && renamed {
		h.HostName(ctx, d.opts.StableKey, st.Name)
	}
}

// ComposeNodeLabel is the NodeLabel of a bridged endpoint named base with
// the parameter suffix — "base (suffix)", or base alone for an empty
// suffix — capped at the NodeLabel maximum: 32 UTF-8 bytes, the generated
// definition's constraint, cut at a rune boundary, the suffix dropped
// first. [New], [Device.Rename] and [Device.Update] cap a name with it,
// so a host composes a label with it and never re-derives the cap.
func ComposeNodeLabel(base, suffix string) string { return endpoint.ComposeNodeLabel(base, suffix) }

// OnChanged calls fn with every change of the device's clusterID server —
// matter.js's `events.<cluster>.<attribute>$Changed` (endpoint/Endpoint.ts:456)
// with the origin of the change (contract.Change; Datasource.ts:1473-1476
// hands the observer the session that made it) — delivered through the
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
