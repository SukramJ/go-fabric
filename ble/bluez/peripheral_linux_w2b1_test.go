// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build linux

package bluez

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/godbus/dbus/v5"

	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/transport/btp"
)

// The sequences below are the ones the bluetoothd probe of #610 recorded
// on BlueZ 5.72: a removal arrives as InterfacesRemoved [GattManager1, …],
// then [LEAdvertisingManager1], then Release, then [Adapter1, …]; a return
// as InterfacesAdded [Adapter1, GattManager1, …], then
// [LEAdvertisingManager1] alone.

// removedW2B1 is an InterfacesRemoved of the adapter object at path that
// names ifaces.
func removedW2B1(path dbus.ObjectPath, ifaces ...string) *dbus.Signal {
	return &dbus.Signal{Path: "/", Name: sigIfacesRemoved, Body: []any{path, ifaces}}
}

// addedW2B1 is an InterfacesAdded of the adapter object at path that names
// ifaces.
func addedW2B1(path dbus.ObjectPath, ifaces ...string) *dbus.Signal {
	m := map[string]map[string]dbus.Variant{}
	for _, i := range ifaces {
		m[i] = map[string]dbus.Variant{}
	}
	return &dbus.Signal{Path: "/", Name: sigIfacesAdded, Body: []any{path, m}}
}

// poweredW2B1 is the adapter's PropertiesChanged of Powered.
func poweredW2B1(on bool) *dbus.Signal {
	return &dbus.Signal{Path: testAdapter, Name: sigPropsChanged, Body: []any{ifaceAdapter, map[string]dbus.Variant{"Powered": dbus.MakeVariant(on)}, []string{}}}
}

// removedAsMeasuredW2B1 takes the adapter away as bluetoothd's graceful
// stop did in the probe: bluetoothd forgets both registrations, and the
// three removal signals arrive in their recorded order. The Release that
// came between the second and the third reached an advertisement the
// removal had already unexported.
func removedAsMeasuredW2B1(p *peripheral, b *fakeBus) {
	b.mu.Lock()
	b.app, b.adv = nil, nil
	b.mu.Unlock()
	p.handle(removedW2B1(testAdapter, ifaceGattManager))
	p.handle(removedW2B1(testAdapter, ifaceAdvertising))
	p.readvertising.Wait()
	p.handle(removedW2B1(testAdapter, ifaceAdapter))
	p.readvertising.Wait()
}

// TestPeripheralReturnRegistersTheApplicationOnceW2B1 holds #610 rows 1
// and 4 (design points 3 and 4): the advertising manager's own
// InterfacesAdded, handled while the first signal's RegisterApplication is
// in flight, does not make that registration look superseded. The
// application is registered once, then the advertisement once.
func TestPeripheralReturnRegistersTheApplicationOnceW2B1(t *testing.T) {
	p, b := newTestPeripheral(t)
	advertiseAndServe(t, p, b)
	removedAsMeasuredW2B1(p, b)
	app0, adv0 := callsOf(b, regApp), callsOf(b, regAdv)

	release := b.hold(regApp)
	p.handle(addedW2B1(testAdapter, ifaceAdapter, ifaceGattManager))
	waitUntil(t, "the RegisterApplication in flight", func() bool { return callsOf(b, regApp) > app0 })
	p.handle(addedW2B1(testAdapter, ifaceAdvertising))
	release()
	p.readvertising.Wait()

	if n, m := callsOf(b, regApp)-app0, callsOf(b, regAdv)-adv0; n != 1 || m != 1 {
		t.Fatalf("after the return: RegisterApplication %d, RegisterAdvertisement %d, want 1 each; calls %q", n, m, b.called())
	}
	if app, adv := heldByBluetoothd(b); !app || !adv || !advertised(p, b) {
		t.Fatalf("application %v, advertisement %v; calls %q", app, adv, b.called())
	}
	if lastCall(b, regApp) > lastCall(b, regAdv) {
		t.Fatalf("the advertisement was registered before the application: %q", b.called())
	}
}

// TestPeripheralAdvertisementWaitsForItsManagerW2B1 holds #610 row 1
// (design point 3): the adapter's return names GattManager1 but not yet
// LEAdvertisingManager1, so the application is registered and the
// advertisement is not — until the advertising manager's own signal.
func TestPeripheralAdvertisementWaitsForItsManagerW2B1(t *testing.T) {
	p, b := newTestPeripheral(t)
	advertiseAndServe(t, p, b)
	removedAsMeasuredW2B1(p, b)
	app0, adv0 := callsOf(b, regApp), callsOf(b, regAdv)

	p.handle(addedW2B1(testAdapter, ifaceAdapter, ifaceGattManager))
	p.readvertising.Wait()
	if n, m := callsOf(b, regApp)-app0, callsOf(b, regAdv)-adv0; n != 1 || m != 0 {
		t.Fatalf("before LEAdvertisingManager1: RegisterApplication %d (want 1), RegisterAdvertisement %d (want 0); calls %q", n, m, b.called())
	}

	p.handle(addedW2B1(testAdapter, ifaceAdvertising))
	p.readvertising.Wait()
	if n, m := callsOf(b, regApp)-app0, callsOf(b, regAdv)-adv0; n != 1 || m != 1 || !advertised(p, b) {
		t.Fatalf("after LEAdvertisingManager1: RegisterApplication %d, RegisterAdvertisement %d, want 1 each; calls %q", n, m, b.called())
	}
}

// TestPeripheralReleaseRetryStopsWithoutTheManagerW2B1 holds #610 row 1
// (design point 5): a released advertisement whose re-registration fails
// is retried while the advertising manager is present; once its
// InterfacesRemoved arrived, the retry stops, without a live service too,
// and the manager's return registers the advertisement.
func TestPeripheralReleaseRetryStopsWithoutTheManagerW2B1(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p, b := newTestPeripheral(t)
		data := btp.EncodeAdvertisementData(3840, 0xFFF1, 0x8001, false, false)
		if err := p.Advertise(context.Background(), contract.BLEAdvertisement{Data: data}); err != nil {
			t.Fatal(err)
		}
		b.failWith(regAdv, dbus.Error{Name: "org.bluez.Error.Failed"})
		releaseAsBluetoothd(t, p, b)
		synctest.Wait()
		if n := callsOf(b, regAdv); n != 2 {
			t.Fatalf("RegisterAdvertisement calls %d after the Release, want 2", n)
		}

		p.handle(removedW2B1(testAdapter, ifaceAdvertising))
		synctest.Wait()
		n := callsOf(b, regAdv)
		time.Sleep(2 * readvertiseMax)
		synctest.Wait()
		if m := callsOf(b, regAdv); m != n {
			t.Fatalf("RegisterAdvertisement called %d more times without the advertising manager; calls %q", m-n, b.called())
		}

		b.clearErr(regAdv)
		p.handle(addedW2B1(testAdapter, ifaceAdvertising))
		p.readvertising.Wait()
		if !advertised(p, b) {
			t.Fatalf("not advertised after the manager's return; calls %q", b.called())
		}
	})
}

// TestPeripheralStopAdvertisingHonoursItsContextW2B1 holds #610 row 3:
// StopAdvertising while an Advertise waits for BlueZ's answer returns
// with its own context.
func TestPeripheralStopAdvertisingHonoursItsContextW2B1(t *testing.T) {
	p, b := newTestPeripheral(t)
	release := b.hold(regAdv)
	t.Cleanup(release)
	advertised := make(chan error, 1)
	go func() {
		data := btp.EncodeAdvertisementData(3840, 0xFFF1, 0x8001, false, false)
		advertised <- p.Advertise(context.Background(), contract.BLEAdvertisement{Data: data})
	}()
	waitUntil(t, "the RegisterAdvertisement in flight", func() bool { return callsOf(b, regAdv) == 1 })

	stopped := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		stopped <- p.StopAdvertising(ctx)
	}()
	var err error
	waitUntil(t, "StopAdvertising to return with its context", func() bool {
		select {
		case err = <-stopped:
			return true
		default:
			return false
		}
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("StopAdvertising: %v, want the context's deadline", err)
	}
	release()
	if err := <-advertised; err != nil {
		t.Fatal(err)
	}
}

// TestPeripheralAdapterMovesDuringRegisterServiceW2B1 holds #610 rows 4
// and 5: the adapter leaves and comes back between bluetoothd carrying out
// RegisterService's RegisterApplication and the reply. The application is
// owed again and registered a second time.
func TestPeripheralAdapterMovesDuringRegisterServiceW2B1(t *testing.T) {
	p, b := newTestPeripheral(t)
	b.mu.Lock()
	b.afterCarryW2B1[regApp] = func() {
		b.mu.Lock()
		b.app = nil
		b.mu.Unlock()
		p.handle(removedSignal(testAdapter))
		p.handle(addedSignal(testAdapter))
	}
	b.mu.Unlock()
	if _, err := p.RegisterService(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	p.readvertising.Wait()
	if n := callsOf(b, regApp); n != 2 {
		t.Fatalf("RegisterApplication calls %d, want 2; calls %q", n, b.called())
	}
	if app, _ := heldByBluetoothd(b); !app {
		t.Fatalf("bluetoothd holds no application; calls %q", b.called())
	}
}

// logW2B1 is a slog handler that keeps the records it receives.
type logW2B1 struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *logW2B1) Enabled(context.Context, slog.Level) bool { return true }
func (h *logW2B1) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	h.records = append(h.records, r.Clone())
	h.mu.Unlock()
	return nil
}
func (h *logW2B1) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *logW2B1) WithGroup(string) slog.Handler      { return h }

// at is the messages logged at level, each with its "err" attribute when
// it has one.
func (h *logW2B1) at(level slog.Level) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for i := range h.records {
		r := &h.records[i]
		if r.Level != level {
			continue
		}
		m := r.Message
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == "err" {
				m += ": " + a.Value.String()
			}
			return true
		})
		out = append(out, m)
	}
	return out
}

// newLoggedPeripheralW2B1 is newTestPeripheral with a logger that keeps
// its records.
func newLoggedPeripheralW2B1(t *testing.T) (*peripheral, *fakeBus, *logW2B1) {
	t.Helper()
	h := &logW2B1{}
	b := newFakeBus()
	p, err := newPeripheral(b, testAdapter, peripheralConfig{log: slog.New(h), pause: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p, b, h
}

// TestPeripheralLogsRetriedRegistrationsW2B1 holds #610 row 2: a
// re-registration BlueZ refuses is logged at Warn once per distinct error
// — the first failure and each change — and the registration that then
// held at Info.
func TestPeripheralLogsRetriedRegistrationsW2B1(t *testing.T) {
	p, b, h := newLoggedPeripheralW2B1(t)
	data := btp.EncodeAdvertisementData(3840, 0xFFF1, 0x8001, false, false)
	if err := p.Advertise(context.Background(), contract.BLEAdvertisement{Data: data}); err != nil {
		t.Fatal(err)
	}
	b.failWith(regAdv, dbus.Error{Name: "org.bluez.Error.Failed", Body: []any{"one"}})
	releaseAsBluetoothd(t, p, b)
	waitUntil(t, "two refusals of the first error", func() bool { return callsOf(b, regAdv) >= 3 })
	b.failWith(regAdv, dbus.Error{Name: "org.bluez.Error.AlreadyExists", Body: []any{"two"}})
	n := callsOf(b, regAdv)
	waitUntil(t, "two refusals of the second error", func() bool { return callsOf(b, regAdv) >= n+2 })
	b.clearErr(regAdv)
	waitUntil(t, "the registration held", func() bool { return advertised(p, b) })
	p.readvertising.Wait()

	warn := h.at(slog.LevelWarn)
	if len(warn) != 2 || !strings.Contains(warn[0], "one") || !strings.Contains(warn[1], "AlreadyExists") {
		t.Fatalf("Warn records %q, want one per distinct error", warn)
	}
	if info := h.at(slog.LevelInfo); len(info) != 1 {
		t.Fatalf("Info records %q, want the one that held", info)
	}
}

// TestPeripheralSupersededRetryLogsNoHeldP10 holds P10-doc-6302: a work
// whose earlier round failed, superseded while its retried
// RegisterApplication is in flight, leaves the advertisement to its
// successor and so does not log that the registrations held.
func TestPeripheralSupersededRetryLogsNoHeldP10(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p, b, h := newLoggedPeripheralW2B1(t)
		advertiseAndServe(t, p, b)
		before := callsOf(b, regApp)
		b.failWith(regApp, dbus.Error{Name: "org.bluez.Error.Failed", Body: []any{"one"}})
		p.handle(addedSignal(testAdapter))
		synctest.Wait()
		if n := callsOf(b, regApp) - before; n != 1 {
			t.Fatalf("RegisterApplication calls %d, want the refused one", n)
		}
		release := b.hold(regApp)
		defer release()
		b.clearErr(regApp)
		time.Sleep(p.pause)
		synctest.Wait()
		if n := callsOf(b, regApp) - before; n != 2 {
			t.Fatalf("RegisterApplication calls %d, want the retry in flight", n)
		}
		p.handle(partialAdded(testAdapter, ifaceAdvertising))
		synctest.Wait()
		release()
		p.readvertising.Wait()

		for _, m := range h.at(slog.LevelInfo) {
			if strings.Contains(m, "after a retry") {
				t.Fatalf("Info record %q from the superseded work", m)
			}
		}
		if !advertised(p, b) {
			t.Fatalf("the successor did not register the advertisement; calls %q", b.called())
		}
	})
}

// TestPeripheralLogsPoweredEdgesW2B1 holds #610 rows 1 and 2 (decision
// 678): Powered false is logged at Warn on its edge, its return at Info;
// neither writes Powered nor registers anything.
func TestPeripheralLogsPoweredEdgesW2B1(t *testing.T) {
	p, b, h := newLoggedPeripheralW2B1(t)
	advertiseAndServe(t, p, b)
	calls := len(b.called())

	p.handle(poweredW2B1(false))
	p.handle(poweredW2B1(false))
	p.handle(poweredW2B1(true))
	p.handle(poweredW2B1(true))
	p.readvertising.Wait()

	if warn := h.at(slog.LevelWarn); len(warn) != 1 || !strings.Contains(warn[0], "powered off") {
		t.Fatalf("Warn records %q, want the one false edge", warn)
	}
	if info := h.at(slog.LevelInfo); len(info) != 1 || !strings.Contains(info[0], "powered on") {
		t.Fatalf("Info records %q, want the one return", info)
	}
	if got := b.called()[calls:]; len(got) != 0 {
		t.Fatalf("bus calls on the Powered edges: %q", got)
	}
	if app, adv := heldByBluetoothd(b); !app || !adv || !advertised(p, b) {
		t.Fatalf("application %v, advertisement %v after the Powered edges", app, adv)
	}
}

// heldAdvertiseW2B1 starts a host Advertise without a deadline whose
// RegisterAdvertisement BlueZ does not answer, and returns once it holds
// advMu inside that call; release lets bluetoothd answer.
func heldAdvertiseW2B1(t *testing.T, p *peripheral, b *fakeBus) (release func()) {
	t.Helper()
	release = b.hold(regAdv)
	go func() {
		data := btp.EncodeAdvertisementData(3840, 0xFFF1, 0x8001, false, false)
		_ = p.Advertise(context.Background(), contract.BLEAdvertisement{Data: data})
	}()
	synctest.Wait()
	if n := callsOf(b, regAdv); n != 1 {
		t.Fatalf("RegisterAdvertisement calls %d, want the one in flight", n)
	}
	return release
}

// TestPeripheralCloseBoundsItsWaitForAdvMuW2B1 holds the follow-up of
// #610 row 3 (Fable N3): Close of a registered, unserved service while a
// host Advertise without a deadline waits on an unanswered bus call still
// returns, and unregisters the application.
func TestPeripheralCloseBoundsItsWaitForAdvMuW2B1(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p, b := newTestPeripheral(t)
		if _, err := p.RegisterService(context.Background(), nil); err != nil {
			t.Fatal(err)
		}
		release := heldAdvertiseW2B1(t, p, b)
		defer release()
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(b.calledBeforeClose(), string(testAdapter)+" "+ifaceGattManager+".UnregisterApplication") {
			t.Fatalf("Close did not unregister the application: %q", b.calledBeforeClose())
		}
	})
}

// TestPeripheralServeEndBoundsItsWaitForAdvMuW2B1 holds the follow-up of
// #610 rows 3-5 (Fable N3): Serve's end while a host Advertise without a
// deadline waits on an unanswered bus call still returns, and unregisters
// the application.
func TestPeripheralServeEndBoundsItsWaitForAdvMuW2B1(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p, b := newTestPeripheral(t)
		s := serve(t, p, nil, func(contract.BLEConnection) {})
		synctest.Wait()
		release := heldAdvertiseW2B1(t, p, b)
		defer release()
		s.cancel()
		<-s.done
		if n := callsOf(b, ifaceGattManager+".UnregisterApplication"); n != 1 {
			t.Fatalf("UnregisterApplication calls %d, want 1; calls %q", n, b.called())
		}
	})
}
