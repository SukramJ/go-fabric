// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build linux

package bluez

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/transport/btp"
)

func newTestPeripheral(t *testing.T) (*peripheral, *fakeBus) {
	t.Helper()
	b := newFakeBus()
	p, err := newPeripheral(b, testAdapter)
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

// served is a Serve that runs until the test ends or cancel is called;
// done closes when it returned, with its error in err.
type served struct {
	cancel func()
	done   chan struct{}
	err    error
}

func serve(t *testing.T, p *peripheral, c3 []byte, accept func(contract.BLEConnection)) *served {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	s := &served{cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(s.done)
		s.err = p.Serve(ctx, c3, accept)
	}()
	t.Cleanup(func() { cancel(); <-s.done })
	return s
}

// TestNewPeripheralNeedsTheAdapter: an adapter BlueZ does not have.
func TestNewPeripheralNeedsTheAdapter(t *testing.T) {
	if _, err := newPeripheral(newFakeBus(), "/org/bluez/hci7"); !errors.Is(err, ErrNoAdapter) {
		t.Fatalf("err = %v, want ErrNoAdapter", err)
	}
}

// TestPeripheralRegistersApplication: the GATT application connectedhomeip
// registers (src/platform/Linux/bluez/BluezEndpoint.cpp:448-534 at
// 6170af84) — service 0xFFF6, primary; C1 "write" through AcquireWrite, C2
// "indicate" through AcquireNotify, C3 "read" with the additional data —
// registered with GattManager1.RegisterApplication under its
// ObjectManager root, unregistered and withdrawn when Serve returns.
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
	read, ok := b.method(p.charPath(3), ifaceGattChar, "ReadValue").(func(map[string]dbus.Variant) ([]byte, *dbus.Error))
	if !ok {
		t.Fatal("C3 ReadValue not exported")
	}
	if v, derr := read(map[string]dbus.Variant{"offset": dbus.MakeVariant(uint16(1))}); derr != nil || !bytes.Equal(v, []byte{0x30}) {
		t.Fatalf("C3 read at 1: %x, %v", v, derr)
	}
	if _, derr := read(map[string]dbus.Variant{"offset": dbus.MakeVariant(uint16(3))}); derr == nil {
		t.Fatal("C3 read beyond its value succeeded")
	}
	if err := p.Serve(context.Background(), nil, func(contract.BLEConnection) {}); !errors.Is(err, ErrServing) {
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
	confirm, ok := b.method(p.charPath(2), ifaceGattChar, "Confirm").(func() *dbus.Error)
	if !ok {
		t.Fatal("Confirm not exported")
	}
	if derr := confirm(); derr != nil {
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
	confirm, ok := b.method(p.charPath(2), ifaceGattChar, "Confirm").(func() *dbus.Error)
	if !ok {
		t.Fatal("Confirm not exported")
	}
	if derr := confirm(); derr != nil {
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

// acquiredFlag reads C1's WriteAcquired or C2's NotifyAcquired as BlueZ
// reads it, through the characteristic's Properties.Get.
func acquiredFlag(t *testing.T, b *fakeBus, p *peripheral, n int, name string) bool {
	t.Helper()
	get, ok := b.method(p.charPath(n), methodProps, "Get").(func(string, string) (dbus.Variant, *dbus.Error))
	if !ok {
		t.Fatal("Properties.Get not exported")
	}
	v, derr := get(ifaceGattChar, name)
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
