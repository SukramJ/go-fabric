// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package wpasupplicant

import (
	"context"
	"runtime"
	"slices"
	"sync"

	"github.com/godbus/dbus/v5"
)

const (
	fakeIface   = dbus.ObjectPath("/fi/w1/wpa_supplicant1/Interfaces/0")
	fakeNetwork = dbus.ObjectPath("/fi/w1/wpa_supplicant1/Interfaces/0/Networks/0")
)

// fakeCall is one method call the driver made.
type fakeCall struct {
	path   dbus.ObjectPath
	method string
	args   []any
	// afterClose: the bus was closed when the call came.
	afterClose bool
}

// fakeBus plays wpa_supplicant over the bus seam: one interface, its
// properties, its BSSs, and the State walk of a join, emitted as
// org.freedesktop.DBus.Properties.PropertiesChanged.
type fakeBus struct {
	sigs chan *dbus.Signal

	mu    sync.Mutex
	calls []fakeCall
	// props are the interface's properties; a missing one is not served.
	props map[string]dbus.Variant
	// bss are the BSS objects' properties.
	bss map[dbus.ObjectPath]map[string]dbus.Variant
	// networks are the network objects' Properties property.
	networks map[dbus.ObjectPath]map[string]dbus.Variant
	// fail answers a method (interface.member) with an error.
	fail map[string]error
	// noScanDone: a scan never ends.
	noScanDone bool
	// scanFailed: ScanDone reports success false.
	scanFailed bool
	// immediateScanDone: Scan's ScanDone is queued with its reply, without
	// waiting for the driver's arming — as chip's wpa_supplicant mock emits
	// it from a task scheduled right behind the reply
	// (matter/testing/linux/wifi.py:403-412 at 6170af84), and a fast real
	// scan can.
	immediateScanDone bool
	// scanArmed reports whether the driver waits for ScanDone; the
	// ScanDone of a scan follows it, as wpa_supplicant's follows the Scan
	// reply (startDriver sets it).
	scanArmed func() bool
	// beforeRead runs before a Get or GetAll answers (outside mu).
	beforeRead func()
	// onDisconnect runs on Disconnect before its own state change.
	onDisconnect func(b *fakeBus)
	// onSelect runs on SelectNetwork; nil walks associating, associated,
	// completed.
	onSelect func(b *fakeBus, n int)
	selects  int
	// selected is signalled on every SelectNetwork.
	selected chan struct{}
	closed   bool
	// watched is every path Watch subscribed to, in order; watchedAt the
	// number of calls made before each.
	watched   []dbus.ObjectPath
	watchedAt []int
	// nextNetworks, while not empty, are the paths AddNetwork answers, in
	// order; fakeNetwork after them.
	nextNetworks []dbus.ObjectPath
	// addLate: AddNetwork waits until its caller's context ends, then
	// lists the network all the same and fails with the context's error —
	// a request wpa_supplicant carries out after the caller stopped
	// waiting. addEntered, when set, is closed once the call has come.
	addLate    bool
	addEntered chan struct{}
	// onAdd runs on AddNetwork under mu, before a fail answers it.
	onAdd func(b *fakeBus)
	// failReads is the number of the next Gets that fail with the bus
	// daemon's NoReply — a wpa_supplicant that does not answer in time.
	failReads int
	// unknownProperty, when set, answers a Get of a property the fake does
	// not serve, in place of InvalidArgs — chip's wpa_supplicant mock
	// answers with org.freedesktop.DBus.Error.UnknownProperty (decision
	// 637).
	unknownProperty *dbus.Error
}

// Watch implements bus: the subscription is recorded, with the calls.
func (b *fakeBus) Watch(_ context.Context, iface dbus.ObjectPath) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.watched = append(b.watched, iface)
	b.watchedAt = append(b.watchedAt, len(b.calls))
	return nil
}

func newFakeBus() *fakeBus {
	return &fakeBus{
		sigs:     make(chan *dbus.Signal, 256),
		props:    map[string]dbus.Variant{"State": dbus.MakeVariant("disconnected"), "CurrentNetwork": dbus.MakeVariant(dbus.ObjectPath("/")), "Networks": dbus.MakeVariant([]dbus.ObjectPath{})},
		bss:      map[dbus.ObjectPath]map[string]dbus.Variant{},
		networks: map[dbus.ObjectPath]map[string]dbus.Variant{},
		fail:     map[string]error{},
		selected: make(chan struct{}, 64),
	}
}

func (b *fakeBus) Signals() <-chan *dbus.Signal { return b.sigs }

func (b *fakeBus) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	return nil
}

// setState sets State and emits its change; b.mu is held.
func (b *fakeBus) setStateLocked(s string) {
	b.props["State"] = dbus.MakeVariant(s)
	b.sigs <- &dbus.Signal{Path: fakeIface, Name: sigPropsChanged, Body: []any{ifaceInterface, map[string]dbus.Variant{"State": dbus.MakeVariant(s)}, []string{}}}
}

func (b *fakeBus) setState(s string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.setStateLocked(s)
}

// removed is the RemoveNetwork calls of the added network, and whether
// one came after the bus closed.
func (b *fakeBus) removed() (n int, afterClose bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, c := range b.calls {
		if c.method == ifaceInterface+".RemoveNetwork" && c.args[0] == fakeNetwork {
			n++
			afterClose = afterClose || c.afterClose
		}
	}
	return n, afterClose
}

// called is the methods called, in order, without the property reads.
func (b *fakeBus) called() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []string
	for _, c := range b.calls {
		if c.method != methodGet && c.method != methodGetAll {
			out = append(out, c.method)
		}
	}
	return out
}

func (b *fakeBus) callsCopy() []fakeCall {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.calls)
}

func (b *fakeBus) Call(ctx context.Context, path dbus.ObjectPath, method string, args ...any) ([]any, error) {
	if method == methodGet || method == methodGetAll {
		b.mu.Lock()
		hook := b.beforeRead
		b.mu.Unlock()
		if hook != nil {
			hook()
		}
	}
	if method == ifaceInterface+".AddNetwork" {
		b.mu.Lock()
		late, entered := b.addLate, b.addEntered
		b.mu.Unlock()
		if late {
			if entered != nil {
				close(entered)
			}
			<-ctx.Done()
			b.mu.Lock()
			defer b.mu.Unlock()
			b.calls = append(b.calls, fakeCall{path: path, method: method, args: args, afterClose: b.closed})
			b.addLocked()
			return nil, ctx.Err()
		}
	}
	// A call on an ended context fails, as godbus's CallWithContext does.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, fakeCall{path: path, method: method, args: args, afterClose: b.closed})
	if b.closed {
		return nil, dbus.ErrClosed
	}
	if method == ifaceInterface+".AddNetwork" && b.onAdd != nil {
		b.onAdd(b)
	}
	if method == methodGet && b.failReads > 0 {
		b.failReads--
		return nil, dbus.Error{Name: "org.freedesktop.DBus.Error.NoReply"}
	}
	if err := b.fail[method]; err != nil {
		return nil, err
	}
	switch method {
	case ifaceRoot + ".GetInterface", ifaceRoot + ".CreateInterface":
		return []any{fakeIface}, nil
	case methodGet:
		if props, ok := b.networks[path]; ok && args[0] == ifaceNetwork && args[1] == "Properties" {
			return []any{dbus.MakeVariant(props)}, nil
		}
		if path != fakeIface {
			return nil, dbus.NewError("org.freedesktop.DBus.Error.UnknownObject", nil)
		}
		v, ok := b.props[args[1].(string)]
		if !ok && b.unknownProperty != nil {
			return nil, *b.unknownProperty
		}
		if !ok {
			return nil, dbus.NewError("org.freedesktop.DBus.Error.InvalidArgs", []any{"No such property"})
		}
		return []any{v}, nil
	case methodGetAll:
		props, ok := b.bss[path]
		if !ok {
			return nil, dbus.NewError("org.freedesktop.DBus.Error.UnknownObject", nil)
		}
		return []any{props}, nil
	case ifaceInterface + ".Scan":
		done := &dbus.Signal{Path: fakeIface, Name: sigScanDone, Body: []any{!b.scanFailed}}
		switch {
		case b.immediateScanDone:
			b.sigs <- done
		case !b.noScanDone:
			armed := b.scanArmed
			go func() {
				for armed != nil && !armed() {
					b.mu.Lock()
					closed := b.closed
					b.mu.Unlock()
					if closed {
						return
					}
					runtime.Gosched()
				}
				b.sigs <- done
			}()
		}
	case ifaceInterface + ".AddNetwork":
		return []any{b.addLocked()}, nil
	case ifaceInterface + ".RemoveNetwork":
		if cur, _ := b.props["CurrentNetwork"].Value().(dbus.ObjectPath); cur == args[0] {
			b.props["CurrentNetwork"] = dbus.MakeVariant(dbus.ObjectPath("/"))
		}
		if v, ok := b.props["Networks"]; ok {
			listed, _ := v.Value().([]dbus.ObjectPath)
			b.props["Networks"] = dbus.MakeVariant(slices.DeleteFunc(slices.Clone(listed), func(p dbus.ObjectPath) bool { return p == args[0] }))
		}
	case ifaceInterface + ".Disconnect":
		if b.onDisconnect != nil {
			b.onDisconnect(b)
		}
		if s, _ := b.props["State"].Value().(string); s != stateDisconnected {
			b.setStateLocked(stateDisconnected)
		}
	case ifaceInterface + ".SelectNetwork":
		b.selects++
		b.props["CurrentNetwork"] = dbus.MakeVariant(args[0])
		if b.onSelect != nil {
			b.onSelect(b, b.selects)
		} else {
			b.setStateLocked("associating")
			b.setStateLocked("associated")
			b.setStateLocked(stateCompleted)
		}
		b.selected <- struct{}{}
	}
	return nil, nil
}

// addLocked adds a network — the next of nextNetworks, else fakeNetwork —
// to Networks when that is served, and returns its path; b.mu is held.
func (b *fakeBus) addLocked() dbus.ObjectPath {
	n := fakeNetwork
	if len(b.nextNetworks) > 0 {
		n, b.nextNetworks = b.nextNetworks[0], b.nextNetworks[1:]
	}
	if v, ok := b.props["Networks"]; ok {
		listed, _ := v.Value().([]dbus.ObjectPath)
		b.props["Networks"] = dbus.MakeVariant(append(slices.Clone(listed), n))
	}
	return n
}
