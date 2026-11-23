// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bluez

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"

	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/transport/btp"
)

// registrations counts the RegisterAdvertisement calls so far.
func registrations(b *fakeBus) int {
	n := 0
	for _, c := range b.called() {
		if strings.HasSuffix(c, ".RegisterAdvertisement") {
			n++
		}
	}
	return n
}

// releaseAsBluetoothd drops the advertisement on bluetoothd's side and
// calls the exported LEAdvertisement1.Release as bluetoothd does.
func releaseAsBluetoothd(t *testing.T, p *peripheral, b *fakeBus) {
	t.Helper()
	release, ok := b.method(p.advPath(), ifaceAdvertise, "Release").(func(dbus.Sender) *dbus.Error)
	if !ok {
		t.Fatal("Release not exported")
	}
	b.mu.Lock()
	b.adv = nil
	b.mu.Unlock()
	if derr := release(b.ownerSender()); derr != nil {
		t.Fatalf("Release: %v", derr)
	}
}

func advertised(p *peripheral, b *fakeBus) bool {
	_, adv := b.registered()
	p.mu.Lock()
	registered := p.registered
	p.mu.Unlock()
	return adv != nil && registered && b.exported(p.advPath())
}

// TestPeripheralReleasedAdvertisementIsRegisteredAgain holds
// R2-C2-fsm-2001: an advertisement BlueZ releases is registered again by
// the peripheral, with the properties of the last Advertise, until
// StopAdvertising; a registration that fails is retried.
func TestPeripheralReleasedAdvertisementIsRegisteredAgain(t *testing.T) {
	p, b := newTestPeripheral(t)
	ctx := context.Background()
	data := btp.EncodeAdvertisementData(3840, 0xFFF1, 0x8001, false, false)
	if err := p.Advertise(ctx, contract.BLEAdvertisement{Data: data}); err != nil {
		t.Fatal(err)
	}
	releaseAsBluetoothd(t, p, b)
	waitUntil(t, "the advertisement registered again", func() bool { return registrations(b) == 2 && advertised(p, b) })
	_, adv := b.registered()
	if adv["LocalName"].Value() != "MATTER-3840" {
		t.Fatalf("re-registered advertisement %v", adv)
	}

	// A refused re-registration is retried.
	b.failWith(ifaceAdvertising+".RegisterAdvertisement", dbus.Error{Name: "org.bluez.Error.Failed"})
	releaseAsBluetoothd(t, p, b)
	waitUntil(t, "the refused re-registration", func() bool { return registrations(b) == 3 })
	b.clearErr(ifaceAdvertising + ".RegisterAdvertisement")
	waitUntil(t, "the retried re-registration", func() bool { return advertised(p, b) })

	// After StopAdvertising a Release brings nothing back.
	if err := p.StopAdvertising(ctx); err != nil {
		t.Fatal(err)
	}
	n := registrations(b)
	_ = p.release(b.ownerSender()) // a late Release, as bluetoothd may send one
	p.readvertising.Wait()
	if registrations(b) != n || advertised(p, b) {
		t.Fatal("an advertisement was registered after StopAdvertising")
	}
}

// TestPeripheralReleaseDuringAdvertise: a Release that lands while an
// Advertise updates the running advertisement — after it read the
// registration, before its PropertiesChanged — leaves the advertisement
// registered once both returned (R2-C2-fsm-2001, the race half).
func TestPeripheralReleaseDuringAdvertise(t *testing.T) {
	p, b := newTestPeripheral(t)
	ctx := context.Background()
	data := btp.EncodeAdvertisementData(3840, 0xFFF1, 0x8001, false, false)
	if err := p.Advertise(ctx, contract.BLEAdvertisement{Data: data}); err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	b.beforeEmit = func() { releaseAsBluetoothd(t, p, b) }
	b.mu.Unlock()
	if err := p.Advertise(ctx, contract.BLEAdvertisement{Data: data}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the advertisement registered after the Release", func() bool { return advertised(p, b) })
}

// TestPeripheralCloseDisconnectsServedCentrals holds R2-C2-err-2002: Close
// of a peripheral whose service is served ends the serving first — the
// central gets Device1.Disconnect and the application is unregistered —
// before the bus is released, as Serve's doc states.
func TestPeripheralCloseDisconnectsServedCentrals(t *testing.T) {
	p, b := newTestPeripheral(t)
	a := &acceptedConn{}
	s := serve(t, p, nil, a.accept)
	waitUntil(t, "the registration", func() bool { app, _ := b.registered(); return app != nil })
	fc := b.connectCentral("AA:BB:CC:DD:EE:05")
	if err := fc.acquireWrite(p.charPath(1), 50); err != nil {
		t.Fatal(err)
	}
	c := a.conn(t)
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	<-s.done
	select {
	case <-c.Disconnected():
	default:
		t.Fatal("the central is still connected after Close")
	}
	calls := b.calledBeforeClose()
	disc := slices.Index(calls, string(fc.device)+" "+ifaceDevice+".Disconnect")
	unreg := slices.Index(calls, string(testAdapter)+" "+ifaceGattManager+".UnregisterApplication")
	if disc < 0 || unreg < 0 {
		t.Fatalf("calls before the bus closed %q: want the central's Disconnect and the UnregisterApplication", calls)
	}
}

// TestPeripheralReleaseAfterOwedUnregisterStaysSilent: a Release that
// follows a StopAdvertising whose UnregisterAdvertisement went unanswered
// does not register the advertisement again — the host asked for silence,
// and BlueZ releasing the advertisement settles the owed unregister. A
// later Advertise registers afresh (Fable diff read of C3-A, B2).
func TestPeripheralReleaseAfterOwedUnregisterStaysSilent(t *testing.T) {
	p, b := newTestPeripheral(t)
	ctx := context.Background()
	data := btp.EncodeAdvertisementData(3840, 0xFFF1, 0x8001, false, false)
	if err := p.Advertise(ctx, contract.BLEAdvertisement{Data: data}); err != nil {
		t.Fatal(err)
	}
	unregAdv := ifaceAdvertising + ".UnregisterAdvertisement"
	b.failWith(unregAdv, dbus.Error{Name: "org.freedesktop.DBus.Error.NoReply"})
	if err := p.StopAdvertising(ctx); err == nil {
		t.Fatal("StopAdvertising with an unanswered unregister succeeded")
	}
	n := registrations(b)
	releaseAsBluetoothd(t, p, b)
	p.readvertising.Wait()
	if registrations(b) != n || advertised(p, b) {
		t.Fatal("a Release after StopAdvertising registered the advertisement again")
	}
	if b.exported(p.advPath()) {
		t.Fatal("the released advertisement is still exported")
	}
	p.mu.Lock()
	registered, owed, adv := p.registered, p.advOwed, p.adv
	p.mu.Unlock()
	if registered || owed || adv != nil {
		t.Fatalf("after the Release: registered %v, owed %v, adv kept %v", registered, owed, adv != nil)
	}
	b.clearErr(unregAdv)
	if err := p.Advertise(ctx, contract.BLEAdvertisement{Data: data}); err != nil {
		t.Fatal(err)
	}
	if registrations(b) != n+1 || !advertised(p, b) {
		t.Fatalf("a later Advertise: registrations %d (want %d), advertised %v", registrations(b), n+1, advertised(p, b))
	}
}
