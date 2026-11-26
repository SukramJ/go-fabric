// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build linux

package bluez

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/transport/btp"
)

func newTestPeripheral(t *testing.T) (*peripheral, *fakeBus) {
	t.Helper()
	b := newFakeBus()
	// The retry pause in milliseconds (R5-test-5505): the retry tests run
	// on it.
	p, err := newPeripheral(b, testAdapter, peripheralConfig{pause: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p, b
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// served is a registered service whose Serve runs until the test ends or
// cancel is called; done closes when it returned, with its error in err.
type served struct {
	svc    contract.GATTService
	cancel func()
	done   chan struct{}
	err    error
}

// serve registers the service and serves it.
func serve(t *testing.T, p *peripheral, c3 []byte, accept func(contract.BLEConnection)) *served {
	t.Helper()
	svc, err := p.RegisterService(t.Context(), c3)
	if err != nil {
		t.Fatalf("RegisterService: %v", err)
	}
	return serveRegistered(t, svc, accept)
}

// serveRegistered serves a registered service.
func serveRegistered(t *testing.T, svc contract.GATTService, accept func(contract.BLEConnection)) *served {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	s := &served{svc: svc, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(s.done)
		s.err = svc.Serve(ctx, accept)
	}()
	t.Cleanup(func() { cancel(); <-s.done })
	return s
}

// TestNewPeripheralNeedsTheAdapter: an adapter BlueZ does not have.
func TestNewPeripheralNeedsTheAdapter(t *testing.T) {
	if _, err := newPeripheral(newFakeBus(), "/org/bluez/hci7", peripheralConfig{}); !errors.Is(err, ErrNoAdapter) {
		t.Fatalf("err = %v, want ErrNoAdapter", err)
	}
}

// TestPeripheralRegistersApplication: the GATT application connectedhomeip
// registers (src/platform/Linux/bluez/BluezEndpoint.cpp:448-534 at
// 6170af84) — service 0xFFF6, primary; C1 "write" through AcquireWrite, C2
// "indicate" through AcquireNotify, C3 "read" with the additional data —
// registered with GattManager1.RegisterApplication under its
// ObjectManager root by RegisterService, unregistered and withdrawn when
// Serve returns. A second registration while it is registered, and a
// second Serve, are refused with ErrServing.
func TestPeripheralRegistersApplication(t *testing.T) {
	p, b := newTestPeripheral(t)
	s := serve(t, p, []byte{0x15, 0x30}, func(contract.BLEConnection) {})
	waitUntil(t, "the registration", func() bool { app, _ := b.registered(); return app != nil })
	if !slices.Contains(b.called(), string(testAdapter)+" "+ifaceGattManager+".RegisterApplication") {
		t.Fatalf("calls %q", b.called())
	}
	if args := b.lastArgs(ifaceGattManager + ".RegisterApplication"); len(args) != 2 || args[0] != p.root {
		t.Fatalf("RegisterApplication args %v", args)
	}
	app, _ := b.registered()
	svc := app[p.servicePath()][ifaceGattService]
	if svc["UUID"].Value() != "fff6" || svc["Primary"].Value() != true {
		t.Fatalf("service %v", svc)
	}
	for n, want := range map[int]struct {
		uuid, flag, acquired string
	}{
		1: {strings.ToLower(btp.C1CharacteristicUUID), "write", "WriteAcquired"},
		2: {strings.ToLower(btp.C2CharacteristicUUID), "indicate", "NotifyAcquired"},
		3: {strings.ToLower(btp.C3CharacteristicUUID), "read", ""},
	} {
		c := app[p.charPath(n)][ifaceGattChar]
		if c["UUID"].Value() != want.uuid || c["Service"].Value() != p.servicePath() || !slices.Equal(c["Flags"].Value().([]string), []string{want.flag}) {
			t.Fatalf("C%d %v", n, c)
		}
		if want.acquired != "" {
			if _, ok := c[want.acquired]; !ok {
				t.Fatalf("C%d without %s: BlueZ would not acquire it", n, want.acquired)
			}
		}
	}
	read, ok := b.method(p.charPath(3), ifaceGattChar, "ReadValue").(func(dbus.Sender, map[string]dbus.Variant) ([]byte, *dbus.Error))
	if !ok {
		t.Fatal("C3 ReadValue not exported")
	}
	if v, derr := read(bluezSender, map[string]dbus.Variant{"offset": dbus.MakeVariant(uint16(1))}); derr != nil || !bytes.Equal(v, []byte{0x30}) {
		t.Fatalf("C3 read at 1: %x, %v", v, derr)
	}
	if _, derr := read(bluezSender, map[string]dbus.Variant{"offset": dbus.MakeVariant(uint16(3))}); derr == nil {
		t.Fatal("C3 read beyond its value succeeded")
	}
	if _, err := p.RegisterService(t.Context(), nil); !errors.Is(err, ErrServing) {
		t.Fatalf("second RegisterService: %v", err)
	}
	<-s.svc.(*gattService).served // the first Serve runs
	if err := s.svc.Serve(t.Context(), func(contract.BLEConnection) {}); !errors.Is(err, ErrServing) {
		t.Fatalf("second Serve: %v", err)
	}
	s.cancel()
	<-s.done
	if s.err != nil {
		t.Fatalf("Serve: %v", s.err)
	}
	if app, _ := b.registered(); app != nil {
		t.Fatal("the application is still registered")
	}
	if b.exported(p.root) || b.exported(p.charPath(1)) {
		t.Fatal("the application is still exported")
	}
	// The registration ended with Serve: the next one is taken.
	again := serve(t, p, nil, func(contract.BLEConnection) {})
	again.cancel()
	<-again.done
	if again.err != nil {
		t.Fatalf("Serve of the next registration: %v", again.err)
	}
}

// TestPeripheralAdvertisement: the LEAdvertisement1 connectedhomeip
// registers (BluezAdvertisement.cpp:43-90, :144-189 at 6170af84) —
// peripheral, the service UUID, the service data, Discoverable, the name
// MATTER-<discriminator> (BLEManagerImpl.cpp:377), the interval — and a
// second Advertise that changes it in place (:144-152), then the
// unregistration.
func TestPeripheralAdvertisement(t *testing.T) {
	p, b := newTestPeripheral(t)
	ctx := context.Background()
	data := btp.EncodeAdvertisementData(3840, 0xFFF1, 0x8001, false, false)
	if err := p.Advertise(ctx, contract.BLEAdvertisement{Data: data, Interval: 20 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	_, adv := b.registered()
	if adv == nil {
		t.Fatal("no advertisement registered")
	}
	sd := adv["ServiceData"].Value().(map[string]dbus.Variant)["fff6"].Value().([]byte)
	if adv["Type"].Value() != "peripheral" || !slices.Equal(adv["ServiceUUIDs"].Value().([]string), []string{"fff6"}) ||
		!bytes.Equal(sd, data[7:]) || adv["Discoverable"].Value() != true || adv["LocalName"].Value() != "MATTER-3840" ||
		adv["MinInterval"].Value() != uint32(20) || adv["MaxInterval"].Value() != uint32(20) {
		t.Fatalf("advertisement %v", adv)
	}
	ext := btp.EncodeAdvertisementData(3840, 0, 0, false, true)
	if err := p.Advertise(ctx, contract.BLEAdvertisement{Data: ext, Interval: 1285 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, c := range b.called() {
		if strings.HasSuffix(c, ".RegisterAdvertisement") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d registrations, want the running advertisement changed in place", n)
	}
	_, adv = b.registered()
	sd = adv["ServiceData"].Value().(map[string]dbus.Variant)["fff6"].Value().([]byte)
	if !bytes.Equal(sd, ext[7:]) || adv["MinInterval"].Value() != uint32(1285) || adv["MaxInterval"].Value() != uint32(1285) {
		t.Fatalf("updated advertisement %v", adv)
	}
	if err := p.Advertise(ctx, contract.BLEAdvertisement{Data: []byte{0x02, 0x01, 0x06}}); err == nil {
		t.Fatal("an advertisement without Matter service data was taken")
	}
	if err := p.StopAdvertising(ctx); err != nil {
		t.Fatal(err)
	}
	if _, adv := b.registered(); adv != nil || b.exported(p.advPath()) {
		t.Fatal("the advertisement is still registered or exported")
	}
	if err := p.StopAdvertising(ctx); err != nil {
		t.Fatalf("a second StopAdvertising: %v", err)
	}
}

// acceptedConn records the connection Serve accepted and its events.
type acceptedConn struct {
	mu     sync.Mutex
	conns  []contract.BLEConnection
	events []string
}

func (a *acceptedConn) accept(c contract.BLEConnection) {
	a.mu.Lock()
	a.conns = append(a.conns, c)
	a.mu.Unlock()
	c.OnWrite(func(v []byte) { a.add("write " + string(v)) })
	c.OnSubscribe(func(mtu int) { a.add("subscribe " + strconv.Itoa(mtu)) })
	c.OnUnsubscribe(func() { a.add("unsubscribe") })
}

func (a *acceptedConn) add(e string) { a.mu.Lock(); a.events = append(a.events, e); a.mu.Unlock() }

func (a *acceptedConn) seen() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.events...)
}

func (a *acceptedConn) conn(t *testing.T) contract.BLEConnection {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.conns) != 1 {
		t.Fatalf("%d connections accepted", len(a.conns))
	}
	return a.conns[0]
}

// TestPeripheralConnectionEvents: a central's first AcquireWrite accepts
// its connection with its address; C1 writes arrive over the socket in
// order (chip WriteHandlerCallback, BluezConnection.cpp:172-192); a
// subscription that bluetoothd reports before the handshake write was
// read is held until it was (the central's order); the end of the C2
// socket is the unsubscription; an indication waits for its confirmation
// — a byte 1 on the socket (BlueZ 5.80) or C2's Confirm (up to 5.73)
// (BluezConnection.cpp:210-238, BluezEndpoint.cpp:207-217).
func TestPeripheralConnectionEvents(t *testing.T) {
	p, b := newTestPeripheral(t)
	a := &acceptedConn{}
	serve(t, p, nil, a.accept)
	waitUntil(t, "the registration", func() bool { app, _ := b.registered(); return app != nil })

	fc := b.connectCentral("AA:BB:CC:DD:EE:01")
	// bluetoothd acquires C2 before this process read the C1 write that
	// came first.
	if err := fc.acquireNotify(p.charPath(2), 100); err != nil {
		t.Fatal(err)
	}
	if err := fc.acquireWrite(p.charPath(1), 100); err != nil {
		t.Fatal(err)
	}
	c := a.conn(t)
	if c.Remote() != "AA:BB:CC:DD:EE:01" {
		t.Fatalf("remote %q", c.Remote())
	}
	// No event before the first write: the ordered events below start
	// with it.
	if err := fc.writeC1([]byte("hs")); err != nil {
		t.Fatal(err)
	}
	if err := fc.writeC1([]byte("d1")); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "three events", func() bool { return len(a.seen()) == 3 })
	if got := a.seen(); got[0] != "write hs" || got[1] != "subscribe 100" || got[2] != "write d1" {
		t.Fatalf("events %q", got)
	}
	if err := fc.acquireNotify(p.charPath(2), 100); err == nil {
		t.Fatal("a second AcquireNotify was granted")
	}

	// Confirmed on the socket.
	errc := make(chan error, 1)
	go func() { errc <- c.Indicate(context.Background(), []byte("ind1")) }()
	v, err := fc.indication()
	if err != nil || string(v) != "ind1" {
		t.Fatalf("indication %q, %v", v, err)
	}
	select {
	case err := <-errc:
		t.Fatalf("Indicate returned before the confirmation: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	if err := fc.confirm(); err != nil {
		t.Fatal(err)
	}
	if err := <-errc; err != nil {
		t.Fatalf("Indicate: %v", err)
	}
	// Confirmed by C2's Confirm.
	go func() { errc <- c.Indicate(context.Background(), []byte("ind2")) }()
	if v, err := fc.indication(); err != nil || string(v) != "ind2" {
		t.Fatalf("indication %q, %v", v, err)
	}
	confirm, ok := b.method(p.charPath(2), ifaceGattChar, "Confirm").(func(dbus.Sender) *dbus.Error)
	if !ok {
		t.Fatal("Confirm not exported")
	}
	if derr := confirm(bluezSender); derr != nil {
		t.Fatal(derr)
	}
	if err := <-errc; err != nil {
		t.Fatalf("Indicate: %v", err)
	}

	// The end of the C2 socket is the unsubscription.
	if err := fc.unsubscribe(); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the unsubscription", func() bool { return slices.Contains(a.seen(), "unsubscribe") })
	if err := c.Indicate(context.Background(), []byte("x")); !errors.Is(err, btp.ErrDisconnected) {
		t.Fatalf("Indicate without a subscription: %v", err)
	}
}

// TestPeripheralDisconnect: Connected=false on the central's device ends
// its connection (chip UpdateConnectionTable, BluezEndpoint.cpp:296-323);
// Disconnect asks BlueZ to drop the device (BluezConnection.cpp:148-160);
// an indication waiting for its confirmation returns when the link goes.
func TestPeripheralDisconnect(t *testing.T) {
	p, b := newTestPeripheral(t)
	a := &acceptedConn{}
	serve(t, p, nil, a.accept)
	waitUntil(t, "the registration", func() bool { app, _ := b.registered(); return app != nil })

	fc := b.connectCentral("AA:BB:CC:DD:EE:02")
	if err := fc.acquireWrite(p.charPath(1), 50); err != nil {
		t.Fatal(err)
	}
	if err := fc.writeC1([]byte("hs")); err != nil {
		t.Fatal(err)
	}
	if err := fc.acquireNotify(p.charPath(2), 50); err != nil {
		t.Fatal(err)
	}
	c := a.conn(t)
	errc := make(chan error, 1)
	go func() { errc <- c.Indicate(context.Background(), []byte("ind")) }()
	if _, err := fc.indication(); err != nil {
		t.Fatal(err)
	}
	fc.disconnect()
	select {
	case <-c.Disconnected():
	case <-time.After(5 * time.Second):
		t.Fatal("Connected=false did not end the connection")
	}
	if err := <-errc; err == nil {
		t.Fatal("an unconfirmed indication of a lost link succeeded")
	}

	// A new connection of another central, dropped by the node.
	fc2 := b.connectCentral("AA:BB:CC:DD:EE:03")
	if err := fc2.acquireWrite(p.charPath(1), 50); err != nil {
		t.Fatal(err)
	}
	var c2 contract.BLEConnection
	waitUntil(t, "the second connection", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		if len(a.conns) == 2 {
			c2 = a.conns[1]
		}
		return c2 != nil
	})
	if err := c2.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(b.called(), string(fc2.device)+" "+ifaceDevice+".Disconnect") {
		t.Fatalf("no Device1.Disconnect: %q", b.called())
	}
	<-c2.Disconnected()
}

// TestPeripheralServeEndDropsCentrals: when Serve's context ends every
// central is disconnected before the application is unregistered.
func TestPeripheralServeEndDropsCentrals(t *testing.T) {
	p, b := newTestPeripheral(t)
	a := &acceptedConn{}
	s := serve(t, p, nil, a.accept)
	waitUntil(t, "the registration", func() bool { app, _ := b.registered(); return app != nil })
	fc := b.connectCentral("AA:BB:CC:DD:EE:04")
	if err := fc.acquireWrite(p.charPath(1), 50); err != nil {
		t.Fatal(err)
	}
	c := a.conn(t)
	s.cancel()
	<-s.done
	select {
	case <-c.Disconnected():
	default:
		t.Fatal("the central is still connected after Serve returned")
	}
	calls := b.called()
	disc := slices.Index(calls, string(fc.device)+" "+ifaceDevice+".Disconnect")
	unreg := slices.Index(calls, string(testAdapter)+" "+ifaceGattManager+".UnregisterApplication")
	if disc < 0 || unreg < 0 || disc > unreg {
		t.Fatalf("calls %q: want the disconnect before the unregistration", calls)
	}
	// AcquireWrite after Serve is refused.
	if err := fc.acquireWrite(p.charPath(1), 50); err == nil {
		t.Fatal("AcquireWrite after Serve was granted")
	}
}

// TestPeripheralServesBTP: the peripheral under transport/btp's server —
// the handshake request on C1, the subscription with the ATT_MTU, the
// handshake response indicated and confirmed, then a Matter message
// reassembled from C1 — the node's production path over BlueZ.
func TestPeripheralServesBTP(t *testing.T) {
	p, b := newTestPeripheral(t)
	got := make(chan []byte, 1)
	opened := make(chan *btp.Conn, 1)
	serve(t, p, nil, func(c contract.BLEConnection) {
		if err := btp.Accept(c, func(conn *btp.Conn) func([]byte) {
			opened <- conn
			return func(m []byte) { got <- m }
		}, btp.Options{}); err != nil {
			t.Error(err)
		}
	})
	waitUntil(t, "the registration", func() bool { app, _ := b.registered(); return app != nil })
	fc := b.connectCentral("AA:BB:CC:DD:EE:05")
	if err := fc.acquireWrite(p.charPath(1), 100); err != nil {
		t.Fatal(err)
	}
	if err := fc.writeC1(btp.EncodeHandshakeRequest(btp.HandshakeRequest{Versions: []int{4}, ATTMTU: 244, ClientWindowSize: 6})); err != nil {
		t.Fatal(err)
	}
	if err := fc.acquireNotify(p.charPath(2), 100); err != nil {
		t.Fatal(err)
	}
	ind, err := fc.indication()
	if err != nil {
		t.Fatal(err)
	}
	resp, err := btp.DecodeHandshakeResponse(ind)
	if err != nil || resp.ATTMTU != 97 || resp.WindowSize != 6 {
		t.Fatalf("handshake response %+v, %v (want ATT_MTU 97 = 100-3)", resp, err)
	}
	if err := fc.confirm(); err != nil {
		t.Fatal(err)
	}
	var conn *btp.Conn
	select {
	case conn = <-opened:
	case <-time.After(5 * time.Second):
		t.Fatal("no session opened")
	}
	if conn.Remote().String() != "AA:BB:CC:DD:EE:05" {
		t.Fatalf("remote %s", conn.Remote())
	}
	ack := uint8(0)
	l := uint16(3)
	pkt, err := btp.EncodePacket(btp.Packet{
		Header:  btp.Header{HasAckNumber: true, IsBeginningSegment: true, IsEndingSegment: true},
		Payload: btp.Payload{AckNumber: &ack, SequenceNumber: 0, MessageLength: &l, SegmentPayload: []byte{7, 8, 9}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := fc.writeC1(pkt); err != nil {
		t.Fatal(err)
	}
	select {
	case m := <-got:
		if !bytes.Equal(m, []byte{7, 8, 9}) {
			t.Fatalf("message %x", m)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no message")
	}
	// The central's unsubscription closes the session and drops the device.
	if err := fc.unsubscribe(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-conn.Closed():
	case <-time.After(5 * time.Second):
		t.Fatal("the session outlived the unsubscription")
	}
	waitUntil(t, "Device1.Disconnect", func() bool {
		return slices.Contains(b.called(), string(fc.device)+" "+ifaceDevice+".Disconnect")
	})
}

// TestPeripheralConfirmAfterRefusedCentral: Confirm names no device (BlueZ
// up to 5.73); after a second central was refused and dropped, it still
// completes the served central's indication.
func TestPeripheralConfirmAfterRefusedCentral(t *testing.T) {
	p, b := newTestPeripheral(t)
	a := &acceptedConn{}
	serve(t, p, nil, a.accept)
	waitUntil(t, "the registration", func() bool { app, _ := b.registered(); return app != nil })
	fc := b.connectCentral("AA:BB:CC:DD:EE:06")
	if err := fc.acquireWrite(p.charPath(1), 50); err != nil {
		t.Fatal(err)
	}
	if err := fc.writeC1([]byte("hs")); err != nil {
		t.Fatal(err)
	}
	if err := fc.acquireNotify(p.charPath(2), 50); err != nil {
		t.Fatal(err)
	}
	served := a.conn(t)
	second := b.connectCentral("AA:BB:CC:DD:EE:07")
	if err := second.acquireWrite(p.charPath(1), 50); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	refused := a.conns[1]
	a.mu.Unlock()
	if err := refused.Disconnect(); err != nil { // as the bridge refuses it
		t.Fatal(err)
	}
	errc := make(chan error, 1)
	ictx, icancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer icancel()
	go func() { errc <- served.Indicate(ictx, []byte("ind")) }()
	if _, err := fc.indication(); err != nil {
		t.Fatal(err)
	}
	confirm, ok := b.method(p.charPath(2), ifaceGattChar, "Confirm").(func(dbus.Sender) *dbus.Error)
	if !ok {
		t.Fatal("Confirm not exported")
	}
	if derr := confirm(bluezSender); derr != nil {
		t.Fatal(derr)
	}
	if err := <-errc; err != nil {
		t.Fatalf("the served central's indication: %v", err)
	}
}

// TestPeripheralRefusesCentralWhileDropping: a central that acquires C1
// while Serve drops the connected ones (its context ended) is refused, not
// accepted after the drop.
func TestPeripheralRefusesCentralWhileDropping(t *testing.T) {
	p, b := newTestPeripheral(t)
	a := &acceptedConn{}
	s := serve(t, p, nil, a.accept)
	waitUntil(t, "the registration", func() bool { app, _ := b.registered(); return app != nil })
	fc := b.connectCentral("AA:BB:CC:DD:EE:08")
	if err := fc.acquireWrite(p.charPath(1), 50); err != nil {
		t.Fatal(err)
	}
	late := b.connectCentral("AA:BB:CC:DD:EE:09")
	var lateErr error
	b.mu.Lock()
	b.onDisconnect = func() { lateErr = late.acquireWrite(p.charPath(1), 50) }
	b.mu.Unlock()
	s.cancel()
	<-s.done
	if lateErr == nil {
		t.Fatal("a central was accepted while Serve dropped the connected ones")
	}
	a.mu.Lock()
	n := len(a.conns)
	a.mu.Unlock()
	if n != 1 {
		t.Fatalf("%d connections accepted, want 1", n)
	}
}

// TestPeripheralCentralBeforeServe: a central that acquires C1 after
// RegisterService returned but before Serve installed accept is held, not
// refused, and accepted once Serve runs (connFor).
func TestPeripheralCentralBeforeServe(t *testing.T) {
	p, b := newTestPeripheral(t)
	svc, err := p.RegisterService(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	fc := b.connectCentral("AA:BB:CC:DD:EE:0A")
	errc := make(chan error, 1)
	go func() { errc <- fc.acquireWrite(p.charPath(1), 50) }()
	// Not answered yet (a refusal would also show below, as an error).
	select {
	case err := <-errc:
		t.Fatalf("AcquireWrite before Serve returned: %v", err)
	default:
	}
	a := &acceptedConn{}
	serveRegistered(t, svc, a.accept)
	if err := <-errc; err != nil {
		t.Fatalf("AcquireWrite held until Serve: %v", err)
	}
	if got := a.conn(t).Remote(); got != "AA:BB:CC:DD:EE:0A" {
		t.Fatalf("accepted %q", got)
	}
}

// TestPeripheralCloseEndsUnservedRegistration: a registration whose Serve
// never comes is unregistered by Close — RegisterService's ctx bounds the
// registration only — a central held for it is refused, and a Serve after
// it returns at once.
func TestPeripheralCloseEndsUnservedRegistration(t *testing.T) {
	p, b := newTestPeripheral(t)
	ctx, cancel := context.WithCancel(t.Context())
	svc, err := p.RegisterService(ctx, []byte{1})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if app, _ := b.registered(); app == nil {
		t.Fatal("the registration ended with RegisterService's ctx")
	}
	fc := b.connectCentral("AA:BB:CC:DD:EE:0B")
	acquire, ok := b.method(p.charPath(1), ifaceGattChar, "AcquireWrite").(acquireFunc)
	if !ok {
		t.Fatal("AcquireWrite not exported")
	}
	errc := make(chan *dbus.Error, 1)
	go func() { _, _, derr := acquire(bluezSender, fc.options(50)); errc <- derr }()
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if derr := <-errc; derr == nil {
		t.Fatal("a central held for an unserved registration was accepted after Close")
	}
	if !slices.Contains(b.called(), string(testAdapter)+" "+ifaceGattManager+".UnregisterApplication") {
		t.Fatalf("Close did not unregister the unserved application: %q", b.called())
	}
	if app, _ := b.registered(); app != nil || b.exported(p.root) {
		t.Fatal("the unserved application outlived Close")
	}
	// A Serve that blocked here would hang the test until its deadline.
	if err := svc.Serve(context.Background(), func(contract.BLEConnection) { t.Error("accepted after Close") }); err == nil {
		t.Fatal("Serve after Close returned nil")
	}
}

// TestPeripheralServeOnEndedContext: a Serve on a context that has ended —
// the bridge's path for a service registered after its BLE side was taken
// down — returns nil at once and unregisters the service.
func TestPeripheralServeOnEndedContext(t *testing.T) {
	p, b := newTestPeripheral(t)
	svc, err := p.RegisterService(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := svc.Serve(ctx, func(contract.BLEConnection) { t.Error("accepted on an ended context") }); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if app, _ := b.registered(); app != nil || b.exported(p.root) {
		t.Fatal("the application outlived Serve")
	}
	if _, err := p.RegisterService(t.Context(), nil); err != nil {
		t.Fatalf("RegisterService after Serve returned: %v", err)
	}
}

// TestPeripheralAcceptDoesNotOverlap: two centrals' first acquires at once
// — godbus runs each exported call on a goroutine of its own — reach
// accept one after the other.
func TestPeripheralAcceptDoesNotOverlap(t *testing.T) {
	p, b := newTestPeripheral(t)
	var mu sync.Mutex
	inFlight, most, accepted := 0, 0, 0
	serve(t, p, nil, func(contract.BLEConnection) {
		mu.Lock()
		inFlight++
		most = max(most, inFlight)
		mu.Unlock()
		for range 1000 { // give an overlapping accept the chance to enter
			runtime.Gosched()
		}
		mu.Lock()
		inFlight--
		accepted++
		mu.Unlock()
	})
	var wg sync.WaitGroup
	for i := range 3 {
		fc := b.connectCentral(fmt.Sprintf("AA:BB:CC:DD:EE:%02X", 0x10+i))
		wg.Go(func() {
			if err := fc.acquireWrite(p.charPath(1), 50); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if accepted != 3 || most != 1 {
		t.Fatalf("accepted %d, at most %d at once; want 3, one at a time", accepted, most)
	}
}

// acquiredFlag reads C1's WriteAcquired or C2's NotifyAcquired as BlueZ
// reads it, through the characteristic's Properties.Get.
func acquiredFlag(t *testing.T, b *fakeBus, p *peripheral, n int, name string) bool {
	t.Helper()
	get, ok := b.method(p.charPath(n), methodProps, "Get").(func(dbus.Sender, string, string) (dbus.Variant, *dbus.Error))
	if !ok {
		t.Fatal("Properties.Get not exported")
	}
	v, derr := get(bluezSender, ifaceGattChar, name)
	if derr != nil {
		t.Fatalf("%s: %v", name, derr)
	}
	on, _ := v.Value().(bool)
	return on
}

// acquiredSignals are the PropertiesChanged signals of the
// characteristics' acquired flags, as "<prop>=<value>" in order.
func acquiredSignals(b *fakeBus) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []string
	for _, s := range b.emitted {
		if s.Name != sigPropsChanged || len(s.Body) < 2 || s.Body[0] != ifaceGattChar {
			continue
		}
		changed, _ := s.Body[1].(map[string]dbus.Variant)
		for _, k := range []string{"WriteAcquired", "NotifyAcquired"} {
			if v, ok := changed[k]; ok {
				out = append(out, fmt.Sprintf("%s=%v", k, v.Value()))
			}
		}
	}
	return out
}

// TestPeripheralAcquiredFlags: a granted AcquireWrite and AcquireNotify
// set WriteAcquired and NotifyAcquired true, announced as
// PropertiesChanged, as connectedhomeip's peripheral sets them
// (BluezEndpoint.cpp:129, :186 at 6170af84); the end of a socket sets its
// flag back, so the next central acquires its own; a second AcquireWrite
// that replaces the socket keeps C1 acquired.
func TestPeripheralAcquiredFlags(t *testing.T) {
	p, b := newTestPeripheral(t)
	a := &acceptedConn{}
	serve(t, p, nil, a.accept)
	waitUntil(t, "the registration", func() bool { app, _ := b.registered(); return app != nil })
	if acquiredFlag(t, b, p, 1, "WriteAcquired") || acquiredFlag(t, b, p, 2, "NotifyAcquired") {
		t.Fatal("a characteristic is acquired before any central")
	}

	fc := b.connectCentral("AA:BB:CC:DD:EE:07")
	if err := fc.acquireWrite(p.charPath(1), 100); err != nil {
		t.Fatal(err)
	}
	if err := fc.acquireNotify(p.charPath(2), 100); err != nil {
		t.Fatal(err)
	}
	if !acquiredFlag(t, b, p, 1, "WriteAcquired") || !acquiredFlag(t, b, p, 2, "NotifyAcquired") {
		t.Fatal("the flags are not set after the acquires")
	}
	// A second AcquireWrite replaces the socket; C1 stays acquired.
	first := fc.write
	if err := fc.acquireWrite(p.charPath(1), 100); err != nil {
		t.Fatal(err)
	}
	_ = first.Close()
	if err := fc.writeC1([]byte("hs")); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the write", func() bool { return slices.Contains(a.seen(), "write hs") })
	if !acquiredFlag(t, b, p, 1, "WriteAcquired") {
		t.Fatal("a replaced C1 socket cleared WriteAcquired")
	}

	// The link goes: both sockets end, both flags return to false.
	fc.disconnect()
	// The flag is set before its PropertiesChanged goes out: wait for
	// both signals, then read the flags.
	waitUntil(t, "the flags cleared", func() bool { return len(acquiredSignals(b)) == 4 })
	if acquiredFlag(t, b, p, 1, "WriteAcquired") || acquiredFlag(t, b, p, 2, "NotifyAcquired") {
		t.Fatal("a flag is still set after the link went")
	}
	got := acquiredSignals(b)
	want := []string{"WriteAcquired=true", "NotifyAcquired=true"}
	if len(got) != 4 || !slices.Equal(got[:2], want) || !slices.Contains(got[2:], "WriteAcquired=false") || !slices.Contains(got[2:], "NotifyAcquired=false") {
		t.Fatalf("PropertiesChanged of the flags: %q", got)
	}

	// The next central acquires afresh.
	fc2 := b.connectCentral("AA:BB:CC:DD:EE:08")
	if err := fc2.acquireWrite(p.charPath(1), 100); err != nil {
		t.Fatal(err)
	}
	if !acquiredFlag(t, b, p, 1, "WriteAcquired") {
		t.Fatal("the next central's AcquireWrite did not set WriteAcquired")
	}
}

// lastAcquiredSignal is the value the last PropertiesChanged of name
// carried ("" before any).
func lastAcquiredSignal(b *fakeBus, name string) string {
	last := ""
	for _, s := range acquiredSignals(b) {
		if v, ok := strings.CutPrefix(s, name+"="); ok {
			last = v
		}
	}
	return last
}

// settledAcquired waits until both flags read as want and the last signal
// of each says the same: the published state and its announcement agree.
func settledAcquired(t *testing.T, b *fakeBus, p *peripheral, want bool) {
	t.Helper()
	w := strconv.FormatBool(want)
	// No signal yet is false: the flags start false.
	agrees := func(name string) bool {
		last := lastAcquiredSignal(b, name)
		return last == w || (last == "" && !want)
	}
	waitUntil(t, "the flags settled at "+w, func() bool {
		return acquiredFlag(t, b, p, 1, "WriteAcquired") == want && acquiredFlag(t, b, p, 2, "NotifyAcquired") == want &&
			agrees("WriteAcquired") && agrees("NotifyAcquired")
	})
}

// TestPeripheralAcquiredFlagsAcrossCentrals: the flags follow every
// connection's sockets. A central that goes while another holds its
// sockets does not clear them — its sockets' end comes after the other's
// acquire — and they clear once no central holds one.
func TestPeripheralAcquiredFlagsAcrossCentrals(t *testing.T) {
	p, b := newTestPeripheral(t)
	serve(t, p, nil, (&acceptedConn{}).accept)
	waitUntil(t, "the registration", func() bool { app, _ := b.registered(); return app != nil })

	first := b.connectCentral("AA:BB:CC:DD:EE:21")
	second := b.connectCentral("AA:BB:CC:DD:EE:22")
	for _, fc := range []*fakeCentral{first, second} {
		if err := fc.acquireWrite(p.charPath(1), 100); err != nil {
			t.Fatal(err)
		}
		if err := fc.acquireNotify(p.charPath(2), 100); err != nil {
			t.Fatal(err)
		}
	}
	settledAcquired(t, b, p, true)
	first.disconnect()
	waitUntil(t, "the first central gone", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return len(p.conns) == 1
	})
	// The first central's readers end after its sockets close; the
	// second's sockets keep both flags.
	time.Sleep(20 * time.Millisecond)
	settledAcquired(t, b, p, true)
	second.disconnect()
	settledAcquired(t, b, p, false)
}

// TestPeripheralAcquiredFlagsWhenTheLinkDrops: a link that drops while its
// AcquireWrite and AcquireNotify are being granted leaves no flag set —
// the publication reads the connections, not the acquire's intent.
func TestPeripheralAcquiredFlagsWhenTheLinkDrops(t *testing.T) {
	p, b := newTestPeripheral(t)
	serve(t, p, nil, (&acceptedConn{}).accept)
	waitUntil(t, "the registration", func() bool { app, _ := b.registered(); return app != nil })
	for i := range 10 {
		fc := b.connectCentral(fmt.Sprintf("AA:BB:CC:DD:EF:%02X", i))
		var wg sync.WaitGroup
		wg.Go(func() {
			_ = fc.acquireWrite(p.charPath(1), 100)
			_ = fc.acquireNotify(p.charPath(2), 100)
		})
		wg.Go(func() { p.gone(fc.device) })
		wg.Wait()
		fc.disconnect()
		settledAcquired(t, b, p, false)
	}
}

// TestPeripheralAcquiredSignalsInOrder: concurrent acquires and drops of
// several centrals announce the flags in the order of the states they
// report — the last signal of each matches the published value.
func TestPeripheralAcquiredSignalsInOrder(t *testing.T) {
	p, b := newTestPeripheral(t)
	serve(t, p, nil, (&acceptedConn{}).accept)
	waitUntil(t, "the registration", func() bool { app, _ := b.registered(); return app != nil })
	var wg sync.WaitGroup
	for i := range 8 {
		fc := b.connectCentral(fmt.Sprintf("AA:BB:CC:DD:F0:%02X", i))
		wg.Go(func() {
			_ = fc.acquireWrite(p.charPath(1), 100)
			_ = fc.acquireNotify(p.charPath(2), 100)
			if i%2 == 0 {
				fc.disconnect()
			}
		})
	}
	wg.Wait()
	settledAcquired(t, b, p, true)
}

// TestPeripheralRegistrationWithoutAReply: a RegisterAdvertisement or
// RegisterApplication whose reply the caller stopped waiting for leaves
// its outcome unknown — bluetoothd may complete it. The peripheral then
// unregisters before it unexports, so it does not believe nothing is
// registered while BlueZ holds the path, and the next registration starts
// clean (review II, R2-P1-drv-03; whether bluetoothd does complete such a
// registration is not verified).
func TestPeripheralRegistrationWithoutAReply(t *testing.T) {
	p, b := newTestPeripheral(t)
	data := btp.EncodeAdvertisementData(3840, 0xFFF1, 0x8001, false, false)
	b.failWith(ifaceAdvertising+".RegisterAdvertisement", errLateReply)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if err := p.Advertise(ctx, contract.BLEAdvertisement{Data: data}); err == nil {
		t.Fatal("Advertise without a reply succeeded")
	}
	if _, adv := b.registered(); adv != nil || b.exported(p.advPath()) {
		t.Fatalf("after a registration without a reply: registered %v, exported %v", adv != nil, b.exported(p.advPath()))
	}
	b.mu.Lock()
	delete(b.errs, ifaceAdvertising+".RegisterAdvertisement")
	b.mu.Unlock()
	if err := p.Advertise(t.Context(), contract.BLEAdvertisement{Data: data}); err != nil {
		t.Fatalf("Advertise after it: %v", err)
	}
	if _, adv := b.registered(); adv == nil {
		t.Fatal("the next advertisement is not registered")
	}

	b.failWith(ifaceGattManager+".RegisterApplication", errLateReply)
	sctx, scancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer scancel()
	if _, err := p.RegisterService(sctx, nil); err == nil {
		t.Fatal("RegisterService without a registration reply succeeded")
	}
	if app, _ := b.registered(); app != nil || b.exported(p.root) {
		t.Fatalf("after an application registration without a reply: registered %v, exported %v", app != nil, b.exported(p.root))
	}
}

// TestNewPeripheralIsBounded: the construction read of BlueZ's objects is
// bounded by peripheralTimeout as every call the peripheral makes on its
// own (review II, R2-P1-drv-07).
func TestNewPeripheralIsBounded(t *testing.T) {
	b := newFakeBus()
	b.failWith(methodGetManaged, errHang)
	done := make(chan error, 1)
	go func() {
		_, err := newPeripheral(b, testAdapter, peripheralConfig{})
		done <- err
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 2*peripheralTimeout)
	defer cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("newPeripheral over a silent bluetoothd = %v, want its bound", err)
		}
	case <-ctx.Done():
		t.Fatal("newPeripheral over a silent bluetoothd is unbounded")
	}
}

// TestPeripheralC1ReaderEndClosesItsSocket: when C1's reader ends while
// bluetoothd's end is still open — os.File reads a zero-length packet as
// io.EOF — the peripheral closes its end, so bluetoothd sees the hang-up
// instead of writing into a socket nobody reads (review II, R2-P1-drv-04;
// whether bluetoothd forwards an empty C1 write is not verified).
func TestPeripheralC1ReaderEndClosesItsSocket(t *testing.T) {
	p, b := newTestPeripheral(t)
	a := &acceptedConn{}
	serve(t, p, nil, a.accept)
	waitUntil(t, "the registration", func() bool { app, _ := b.registered(); return app != nil })
	fc := b.connectCentral("AA:BB:CC:DD:EE:0D")
	if err := fc.acquireWrite(p.charPath(1), 100); err != nil {
		t.Fatal(err)
	}
	fd := int(fc.write.Fd())
	if _, err := syscall.Write(fd, nil); err != nil { // an empty packet
		t.Fatal(err)
	}
	waitUntil(t, "WriteAcquired to clear", func() bool { return !acquiredFlag(t, b, p, 1, "WriteAcquired") })
	waitUntil(t, "the peripheral's C1 end to close", func() bool {
		_, err := syscall.Write(fd, []byte{1})
		return err != nil
	})
}

// TestPeripheralCentralGoneBeforeItsAcquire: a central whose
// Connected=false was handled before its AcquireWrite reached the
// peripheral — godbus runs method calls on goroutines of their own,
// signals in order on its dispatch loop — is not left as the accepted
// central: the peripheral reads Device1.Connected after it took the
// central in and ends the connection when it is false (review II,
// R2-P1-drv-05).
func TestPeripheralCentralGoneBeforeItsAcquire(t *testing.T) {
	p, b := newTestPeripheral(t)
	a := &acceptedConn{}
	serve(t, p, nil, a.accept)
	waitUntil(t, "the registration", func() bool { app, _ := b.registered(); return app != nil })
	fc := b.connectCentral("AA:BB:CC:DD:EE:0E")
	b.mu.Lock()
	b.objs[fc.device][ifaceDevice]["Connected"] = dbus.MakeVariant(false) // its signal already handled
	b.mu.Unlock()
	_ = fc.acquireWrite(p.charPath(1), 100)
	conn := a.conn(t)
	waitUntil(t, "the gone central's connection to end", func() bool {
		select {
		case <-conn.Disconnected():
			return true
		default:
			return false
		}
	})
	// markGone closes Disconnected before it forgets the connection, so
	// the forgetting is waited for, not read once.
	waitUntil(t, "the gone central's connection to be forgotten", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return len(p.conns) == 0
	})
}

// TestPeripheralFailedUnregisterKeepsTheRegistration: an
// UnregisterAdvertisement or UnregisterApplication that fails keeps the
// registration, its objects and the owed unregister; the next Advertise,
// StopAdvertising or RegisterService retries it before anything else (review II,
// R2-P6-err-2012).
func TestPeripheralFailedUnregisterKeepsTheRegistration(t *testing.T) {
	p, b := newTestPeripheral(t)
	ctx := context.Background()
	data := btp.EncodeAdvertisementData(3840, 0xFFF1, 0x8001, false, false)
	if err := p.Advertise(ctx, contract.BLEAdvertisement{Data: data}); err != nil {
		t.Fatal(err)
	}
	unregAdv := ifaceAdvertising + ".UnregisterAdvertisement"
	b.failWith(unregAdv, dbus.Error{Name: "org.freedesktop.DBus.Error.NoReply"})
	if err := p.StopAdvertising(ctx); err == nil {
		t.Fatal("StopAdvertising with a failed unregister succeeded")
	}
	if _, adv := b.registered(); adv == nil || !b.exported(p.advPath()) {
		t.Fatalf("after the failed unregister: held by BlueZ %v, exported %v", adv != nil, b.exported(p.advPath()))
	}
	// The retry fails again: Advertise reports it and changes nothing.
	ext := btp.EncodeAdvertisementData(3840, 0, 0, false, true)
	if err := p.Advertise(ctx, contract.BLEAdvertisement{Data: ext}); err == nil {
		t.Fatal("Advertise over an owed unregister that failed again succeeded")
	}
	if !b.exported(p.advPath()) {
		t.Fatal("the advertisement was unexported while BlueZ may hold it")
	}
	// The retry succeeds: the old registration is released, a fresh one made.
	b.mu.Lock()
	delete(b.errs, unregAdv)
	b.mu.Unlock()
	if err := p.Advertise(ctx, contract.BLEAdvertisement{Data: ext}); err != nil {
		t.Fatal(err)
	}
	_, adv := b.registered()
	if sd := adv["ServiceData"].Value().(map[string]dbus.Variant)["fff6"].Value().([]byte); !bytes.Equal(sd, ext[7:]) {
		t.Fatalf("advertisement %v", adv)
	}
	var seq []string
	for _, c := range b.called() {
		if strings.HasSuffix(c, "Advertisement") {
			seq = append(seq, c[strings.LastIndex(c, ".")+1:])
		}
	}
	want := []string{"RegisterAdvertisement", "UnregisterAdvertisement", "UnregisterAdvertisement", "UnregisterAdvertisement", "RegisterAdvertisement"}
	if !slices.Equal(seq, want) {
		t.Fatalf("calls %q, want %q", seq, want)
	}
	if err := p.StopAdvertising(ctx); err != nil {
		t.Fatal(err)
	}
	if _, adv := b.registered(); adv != nil || b.exported(p.advPath()) {
		t.Fatal("the advertisement is still registered or exported")
	}

	// The application: Serve's unregister fails, the objects stay, and
	// the next RegisterService retries it first.
	unregApp := ifaceGattManager + ".UnregisterApplication"
	b.failWith(unregApp, dbus.Error{Name: "org.bluez.Error.Failed", Body: []any{"busy"}})
	s := serve(t, p, nil, func(contract.BLEConnection) {})
	waitUntil(t, "the registration", func() bool { app, _ := b.registered(); return app != nil })
	s.cancel()
	<-s.done
	if s.err == nil {
		t.Fatal("Serve with a failed unregister returned nil")
	}
	if !b.exported(p.root) {
		t.Fatal("the application was unexported while BlueZ may hold it")
	}
	b.mu.Lock()
	delete(b.errs, unregApp)
	b.mu.Unlock()
	before := len(b.called())
	s = serve(t, p, nil, func(contract.BLEConnection) {})
	waitUntil(t, "the second registration", func() bool {
		for _, c := range b.called()[before:] {
			if strings.HasSuffix(c, ".RegisterApplication") {
				return true
			}
		}
		return false
	})
	var app []string
	for _, c := range b.called()[before:] {
		if strings.Contains(c, "Application") {
			app = append(app, c[strings.LastIndex(c, ".")+1:])
		}
	}
	if !slices.Equal(app, []string{"UnregisterApplication", "RegisterApplication"}) {
		t.Fatalf("calls %q, want the owed unregister before the registration", app)
	}
	s.cancel()
	<-s.done
	if s.err != nil || b.exported(p.root) {
		t.Fatalf("Serve: %v, exported %v", s.err, b.exported(p.root))
	}
}

// TestPeripheralStopDuringAdvertise: a StopAdvertising while the first
// Advertise's registration is in flight is not overtaken by it — nothing
// is registered after both returned (review II, R2-P6-conc-2013).
func TestPeripheralStopDuringAdvertise(t *testing.T) {
	p, b := newTestPeripheral(t)
	release := b.hold(ifaceAdvertising + ".RegisterAdvertisement")
	defer release()
	data := btp.EncodeAdvertisementData(3840, 0xFFF1, 0x8001, false, false)
	advErr := make(chan error, 1)
	go func() { advErr <- p.Advertise(context.Background(), contract.BLEAdvertisement{Data: data}) }()
	waitUntil(t, "the registration in flight", func() bool {
		return slices.Contains(b.called(), string(testAdapter)+" "+ifaceAdvertising+".RegisterAdvertisement")
	})
	stopErr := make(chan error, 1)
	go func() { stopErr <- p.StopAdvertising(context.Background()) }()
	time.Sleep(20 * time.Millisecond) // the stop runs into the registration
	release()
	if err := <-advErr; err != nil {
		t.Fatal(err)
	}
	if err := <-stopErr; err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	registered := p.registered
	p.mu.Unlock()
	if _, adv := b.registered(); adv != nil || registered || b.exported(p.advPath()) {
		t.Fatalf("after the stop: held by BlueZ %v, registered %v, exported %v", adv != nil, registered, b.exported(p.advPath()))
	}
}

// TestPeripheralServesBluetoothdOnly: the exported methods answer the
// owner of org.bluez only; another caller is refused with NotPermitted and
// creates no connection, completes no indication and drops no
// advertisement (review II, R2-P6-sec-2014).
func TestPeripheralServesBluetoothdOnly(t *testing.T) {
	p, b := newTestPeripheral(t)
	a := &acceptedConn{}
	serve(t, p, []byte{0x15}, a.accept)
	waitUntil(t, "the registration", func() bool { app, _ := b.registered(); return app != nil })
	data := btp.EncodeAdvertisementData(3840, 0xFFF1, 0x8001, false, false)
	if err := p.Advertise(context.Background(), contract.BLEAdvertisement{Data: data}); err != nil {
		t.Fatal(err)
	}
	const intruder = dbus.Sender(":1.99")
	notPermitted := func(what string, derr *dbus.Error) {
		t.Helper()
		if derr == nil || derr.Name != errNotPermitted {
			t.Fatalf("%s from another sender: %v, want %s", what, derr, errNotPermitted)
		}
	}
	opts := map[string]dbus.Variant{"device": dbus.MakeVariant(devPath("AA:BB:CC:DD:EE:42")), "mtu": dbus.MakeVariant(uint16(100))}
	for _, m := range []struct {
		char   int
		member string
	}{{1, "AcquireWrite"}, {2, "AcquireNotify"}} {
		f, ok := b.method(p.charPath(m.char), ifaceGattChar, m.member).(acquireFunc)
		if !ok {
			t.Fatalf("%s not exported with a sender", m.member)
		}
		_, _, derr := f(intruder, opts)
		notPermitted(m.member, derr)
	}
	read, ok := b.method(p.charPath(3), ifaceGattChar, "ReadValue").(func(dbus.Sender, map[string]dbus.Variant) ([]byte, *dbus.Error))
	if !ok {
		t.Fatal("ReadValue not exported with a sender")
	}
	_, derr := read(intruder, map[string]dbus.Variant{})
	notPermitted("ReadValue", derr)
	get, ok := b.method(p.charPath(3), methodProps, "Get").(func(dbus.Sender, string, string) (dbus.Variant, *dbus.Error))
	if !ok {
		t.Fatal("Properties.Get not exported with a sender")
	}
	_, derr = get(intruder, ifaceGattChar, "Value")
	notPermitted("Properties.Get", derr)
	getAll, ok := b.method(p.charPath(3), methodProps, "GetAll").(func(dbus.Sender, string) (map[string]dbus.Variant, *dbus.Error))
	if !ok {
		t.Fatal("Properties.GetAll not exported with a sender")
	}
	_, derr = getAll(intruder, ifaceGattChar)
	notPermitted("Properties.GetAll", derr)
	managed, ok := b.method(p.root, ifaceObjectMgr, "GetManagedObjects").(func(dbus.Sender) (map[dbus.ObjectPath]map[string]map[string]dbus.Variant, *dbus.Error))
	if !ok {
		t.Fatal("GetManagedObjects not exported with a sender")
	}
	_, derr = managed(intruder)
	notPermitted("GetManagedObjects", derr)
	if v, derr := get(bluezSender, ifaceGattChar, "Value"); derr != nil || !bytes.Equal(v.Value().([]byte), []byte{0x15}) {
		t.Fatalf("C3's Value for bluetoothd: %v, %v", v, derr)
	}
	release, ok := b.method(p.advPath(), ifaceAdvertise, "Release").(func(dbus.Sender) *dbus.Error)
	if !ok {
		t.Fatal("Release not exported with a sender")
	}
	notPermitted("Release", release(intruder))
	p.mu.Lock()
	conns, registered := len(p.conns), p.registered
	p.mu.Unlock()
	a.mu.Lock()
	accepted := len(a.conns)
	a.mu.Unlock()
	if conns != 0 || accepted != 0 || !registered || !b.exported(p.advPath()) {
		t.Fatalf("after the refused calls: %d connections, %d accepted, registered %v", conns, accepted, registered)
	}

	// Confirm from another sender does not complete the indication.
	fc := b.connectCentral("AA:BB:CC:DD:EE:43")
	if err := fc.acquireWrite(p.charPath(1), 100); err != nil {
		t.Fatal(err)
	}
	if err := fc.writeC1([]byte("hs")); err != nil {
		t.Fatal(err)
	}
	if err := fc.acquireNotify(p.charPath(2), 100); err != nil {
		t.Fatal(err)
	}
	c := a.conn(t)
	errc := make(chan error, 1)
	go func() { errc <- c.Indicate(context.Background(), []byte("ind")) }()
	if _, err := fc.indication(); err != nil {
		t.Fatal(err)
	}
	confirm, ok := b.method(p.charPath(2), ifaceGattChar, "Confirm").(func(dbus.Sender) *dbus.Error)
	if !ok {
		t.Fatal("Confirm not exported with a sender")
	}
	notPermitted("Confirm", confirm(intruder))
	select {
	case err := <-errc:
		t.Fatalf("another sender's Confirm completed the indication: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	if derr := confirm(bluezSender); derr != nil {
		t.Fatal(derr)
	}
	if err := <-errc; err != nil {
		t.Fatal(err)
	}

	// bluetoothd restarted under a new unique name: followed.
	b.mu.Lock()
	b.owner = ":1.8"
	b.mu.Unlock()
	if _, derr := read(":1.8", map[string]dbus.Variant{}); derr != nil {
		t.Fatalf("ReadValue from the new owner: %v", derr)
	}
	_, derr = read(bluezSender, map[string]dbus.Variant{})
	notPermitted("ReadValue from the old owner", derr)
}

// TestPeripheralLateConfirmationOfAnAbandonedIndication: the confirmation
// of an indication whose Indicate gave up is counted against it, not taken
// for the next indication's (review II, R2-P6-fsm-2015).
func TestPeripheralLateConfirmationOfAnAbandonedIndication(t *testing.T) {
	for _, via := range []string{"socket", "Confirm"} {
		t.Run(via, func(t *testing.T) {
			p, b := newTestPeripheral(t)
			a := &acceptedConn{}
			serve(t, p, nil, a.accept)
			waitUntil(t, "the registration", func() bool { app, _ := b.registered(); return app != nil })
			fc := b.connectCentral("AA:BB:CC:DD:EE:44")
			if err := fc.acquireWrite(p.charPath(1), 100); err != nil {
				t.Fatal(err)
			}
			if err := fc.writeC1([]byte("hs")); err != nil {
				t.Fatal(err)
			}
			if err := fc.acquireNotify(p.charPath(2), 100); err != nil {
				t.Fatal(err)
			}
			c := a.conn(t)
			confirm := func() {
				t.Helper()
				if via == "socket" {
					if err := fc.confirm(); err != nil {
						t.Fatal(err)
					}
					return
				}
				f, ok := b.method(p.charPath(2), ifaceGattChar, "Confirm").(func(dbus.Sender) *dbus.Error)
				if !ok {
					t.Fatal("Confirm not exported")
				}
				if derr := f(bluezSender); derr != nil {
					t.Fatal(derr)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			errc := make(chan error, 1)
			go func() { errc <- c.Indicate(ctx, []byte("first")) }()
			if v, err := fc.indication(); err != nil || string(v) != "first" {
				t.Fatalf("indication %q, %v", v, err)
			}
			cancel()
			if err := <-errc; !errors.Is(err, context.Canceled) {
				t.Fatalf("the abandoned Indicate: %v", err)
			}
			go func() { errc <- c.Indicate(context.Background(), []byte("second")) }()
			if v, err := fc.indication(); err != nil || string(v) != "second" {
				t.Fatalf("indication %q, %v", v, err)
			}
			confirm() // the first indication's, late
			select {
			case err := <-errc:
				t.Fatalf("the first indication's confirmation completed the second: %v", err)
			case <-time.After(50 * time.Millisecond):
			}
			confirm()
			select {
			case err := <-errc:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("the second indication's own confirmation did not complete it")
			}
		})
	}
}
