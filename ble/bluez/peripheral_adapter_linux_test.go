// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bluez

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/transport/btp"
)

// adapterIfaces are the adapter's interfaces the peripheral registers with.
var adapterIfaces = []string{ifaceAdapter, ifaceAdvertising, ifaceGattManager}

// removedSignal and addedSignal are the ObjectManager signals of the
// adapter object at path.
func removedSignal(path dbus.ObjectPath) *dbus.Signal {
	return &dbus.Signal{Path: "/", Name: sigIfacesRemoved, Body: []any{path, slices.Clone(adapterIfaces)}}
}

func addedSignal(path dbus.ObjectPath) *dbus.Signal {
	ifaces := map[string]map[string]dbus.Variant{}
	for _, i := range adapterIfaces {
		ifaces[i] = map[string]dbus.Variant{}
	}
	return &dbus.Signal{Path: "/", Name: sigIfacesAdded, Body: []any{path, ifaces}}
}

// adapterRemoved drops what bluetoothd held with the adapter and sends
// InterfacesRemoved for the adapter object.
func adapterRemoved(b *fakeBus, path dbus.ObjectPath) {
	b.mu.Lock()
	if path == testAdapter {
		b.app, b.adv = nil, nil
	}
	b.mu.Unlock()
	b.emit(removedSignal(path))
}

// adapterAdded sends InterfacesAdded for the adapter object.
func adapterAdded(b *fakeBus, path dbus.ObjectPath) {
	b.emit(addedSignal(path))
}

// callsOf counts the calls of method so far.
func callsOf(b *fakeBus, method string) int {
	n := 0
	for _, c := range b.called() {
		if strings.HasSuffix(c, " "+method) {
			n++
		}
	}
	return n
}

// lastCall is the index of the last call of method, -1 without one.
func lastCall(b *fakeBus, method string) int {
	calls := b.called()
	for i := len(calls) - 1; i >= 0; i-- {
		if strings.HasSuffix(calls[i], " "+method) {
			return i
		}
	}
	return -1
}

// advertiseAndServe advertises and serves the Matter service, and waits
// until bluetoothd holds both.
func advertiseAndServe(t *testing.T, p *peripheral, b *fakeBus) {
	t.Helper()
	data := btp.EncodeAdvertisementData(3840, 0xFFF1, 0x8001, false, false)
	if err := p.Advertise(context.Background(), contract.BLEAdvertisement{Data: data}); err != nil {
		t.Fatal(err)
	}
	serve(t, p, nil, func(contract.BLEConnection) {})
	waitUntil(t, "the application registered", func() bool { app, _ := b.registered(); return app != nil })
}

// heldByBluetoothd reports whether bluetoothd holds the application and
// the advertisement.
func heldByBluetoothd(b *fakeBus) (app, adv bool) {
	a, d := b.registered()
	return a != nil, d != nil
}

const (
	regApp = ifaceGattManager + ".RegisterApplication"
	regAdv = ifaceAdvertising + ".RegisterAdvertisement"
)

// TestPeripheralFollowsTheAdapter holds R3-fsm-3301: the adapter object
// leaves and comes back (a re-plugged controller, a bluetoothd restart)
// while the peripheral advertises and serves. The peripheral registers
// the application again, then the advertisement.
func TestPeripheralFollowsTheAdapter(t *testing.T) {
	p, b := newTestPeripheral(t)
	advertiseAndServe(t, p, b)

	adapterRemoved(b, testAdapter)
	waitUntil(t, "the advertisement marked unregistered", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return !p.registered
	})
	adapterAdded(b, testAdapter)
	waitUntil(t, "the application and the advertisement registered again", func() bool {
		app, adv := heldByBluetoothd(b)
		return app && adv && advertised(p, b)
	})
	if n := callsOf(b, regApp); n != 2 {
		t.Fatalf("RegisterApplication calls = %d, want 2", n)
	}
	if n := callsOf(b, regAdv); n != 2 {
		t.Fatalf("RegisterAdvertisement calls = %d, want 2", n)
	}
	if lastCall(b, regApp) > lastCall(b, regAdv) {
		t.Fatalf("the advertisement was registered before the application: %q", b.called())
	}
	_, adv := b.registered()
	if adv["LocalName"].Value() != "MATTER-3840" {
		t.Fatalf("re-registered advertisement %v", adv)
	}
}

// TestPeripheralAdapterAddedAlone: a bluetoothd that ended without
// InterfacesRemoved (a crash) and came back says only InterfacesAdded;
// the peripheral, which still believes both registered, registers them
// again (R3-fsm-3301).
func TestPeripheralAdapterAddedAlone(t *testing.T) {
	p, b := newTestPeripheral(t)
	advertiseAndServe(t, p, b)

	b.mu.Lock()
	b.app, b.adv = nil, nil
	b.mu.Unlock()
	adapterAdded(b, testAdapter)
	waitUntil(t, "the application and the advertisement registered again", func() bool {
		app, adv := heldByBluetoothd(b)
		return app && adv && advertised(p, b)
	})
	if lastCall(b, regApp) > lastCall(b, regAdv) {
		t.Fatalf("the advertisement was registered before the application: %q", b.called())
	}
}

// TestPeripheralReleaseThenAdapterAdded: bluetoothd says Release as it
// goes down, the peripheral registers the advertisement again (on the
// daemon that comes back), and the application is gone. The adapter's
// InterfacesAdded brings the application back, and the advertisement is
// registered after it (R3-fsm-3301, the second facet).
func TestPeripheralReleaseThenAdapterAdded(t *testing.T) {
	p, b := newTestPeripheral(t)
	advertiseAndServe(t, p, b)

	b.mu.Lock()
	b.app = nil
	b.mu.Unlock()
	releaseAsBluetoothd(t, p, b)
	waitUntil(t, "the released advertisement registered again", func() bool { return callsOf(b, regAdv) == 2 && advertised(p, b) })

	adapterAdded(b, testAdapter)
	waitUntil(t, "the application registered again", func() bool {
		app, adv := heldByBluetoothd(b)
		return app && adv && advertised(p, b) && callsOf(b, regAdv) == 3
	})
	unregAdv := lastCall(b, ifaceAdvertising+".UnregisterAdvertisement")
	if unregAdv < 0 || unregAdv > lastCall(b, regApp) || lastCall(b, regApp) > lastCall(b, regAdv) {
		t.Fatalf("want the advertisement unregistered, the application registered, then the advertisement: %q", b.called())
	}
}

// TestPeripheralAdvertisementWaitsForTheApplication: while the
// application's registration on the returned adapter fails, the
// advertisement stays unregistered — a commissioner would find a node
// without the Matter service — and both follow once it holds
// (R3-fsm-3301).
func TestPeripheralAdvertisementWaitsForTheApplication(t *testing.T) {
	p, b := newTestPeripheral(t)
	advertiseAndServe(t, p, b)

	b.failWith(regApp, dbus.Error{Name: "org.bluez.Error.Failed"})
	adapterRemoved(b, testAdapter)
	adapterAdded(b, testAdapter)
	waitUntil(t, "a refused and a retried RegisterApplication", func() bool { return callsOf(b, regApp) >= 3 })
	if _, adv := heldByBluetoothd(b); adv {
		t.Fatal("the advertisement was registered while the application was not")
	}
	b.clearErr(regApp)
	waitUntil(t, "the application and the advertisement registered again", func() bool {
		app, adv := heldByBluetoothd(b)
		return app && adv && advertised(p, b)
	})
}

// TestPeripheralAdapterRemovedMarksUnregistered: after the adapter left,
// the next Advertise registers the advertisement afresh instead of
// changing one BlueZ no longer holds (R3-fsm-3301).
func TestPeripheralAdapterRemovedMarksUnregistered(t *testing.T) {
	p, b := newTestPeripheral(t)
	advertiseAndServe(t, p, b)

	adapterRemoved(b, testAdapter)
	waitUntil(t, "the advertisement marked unregistered", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return !p.registered
	})
	if b.exported(p.advPath()) {
		t.Fatal("the lost advertisement is still exported")
	}
	data := btp.EncodeAdvertisementData(3840, 0xFFF1, 0x8001, false, false)
	if err := p.Advertise(context.Background(), contract.BLEAdvertisement{Data: data, Interval: 150 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	if n := callsOf(b, regAdv); n != 2 || !advertised(p, b) {
		t.Fatalf("after the adapter left, Advertise: RegisterAdvertisement calls %d (want 2), advertised %v", n, advertised(p, b))
	}
}

// TestPeripheralIgnoresAnotherAdapter: another adapter's objects leaving
// and coming back change nothing.
func TestPeripheralIgnoresAnotherAdapter(t *testing.T) {
	p, b := newTestPeripheral(t)
	advertiseAndServe(t, p, b)
	calls := len(b.called())

	// Handed to the signal handler directly, so the work they would start
	// is started when handle returns.
	other := dbus.ObjectPath("/org/bluez/hci1")
	p.handle(removedSignal(other))
	p.handle(addedSignal(other))
	p.readvertising.Wait()
	if !advertised(p, b) || len(b.called()) != calls {
		t.Fatalf("another adapter's signals: advertised %v, calls %q", advertised(p, b), b.called()[calls:])
	}
}

// TestPeripheralAdapterAddedWhileSilent: an adapter that comes back
// while nothing is advertised nor served registers nothing.
func TestPeripheralAdapterAddedWhileSilent(t *testing.T) {
	p, b := newTestPeripheral(t)
	p.handle(removedSignal(testAdapter))
	p.handle(addedSignal(testAdapter))
	p.readvertising.Wait()
	if n := callsOf(b, regApp) + callsOf(b, regAdv); n != 0 {
		t.Fatalf("registrations without an advertisement or a service: %q", b.called())
	}
}
