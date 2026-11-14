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
}

func newFakeBus() *fakeBus {
	return &fakeBus{
		sigs:     make(chan *dbus.Signal, 256),
		props:    map[string]dbus.Variant{"State": dbus.MakeVariant("disconnected"), "CurrentNetwork": dbus.MakeVariant(dbus.ObjectPath("/"))},
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
		return []any{fakeNetwork}, nil
	case ifaceInterface + ".RemoveNetwork":
		if cur, _ := b.props["CurrentNetwork"].Value().(dbus.ObjectPath); cur == args[0] {
			b.props["CurrentNetwork"] = dbus.MakeVariant(dbus.ObjectPath("/"))
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
