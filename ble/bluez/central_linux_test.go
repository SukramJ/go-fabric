// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build linux

package bluez

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/SukramJ/go-fabric/controller/ble"
	"github.com/SukramJ/go-fabric/controller/ble/bletest"
	"github.com/SukramJ/go-fabric/controller/discovery"
	"github.com/SukramJ/go-fabric/transport/btp"
)

func startCentral(t *testing.T, b *fakeBus) *central {
	t.Helper()
	c, err := newCentral(b, testAdapter)
	if err != nil {
		t.Fatalf("newCentral: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// ads collects advertisements.
type ads struct {
	mu  sync.Mutex
	got []string
	ch  chan struct{}
}

func newAds() *ads { return &ads{ch: make(chan struct{}, 64)} }

func (a *ads) on(p ble.Peripheral, d []byte) {
	a.mu.Lock()
	a.got = append(a.got, p.Address+"="+string(d))
	a.mu.Unlock()
	a.ch <- struct{}{}
}

func (a *ads) wait(t *testing.T, n int) []string {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		a.mu.Lock()
		got := append([]string(nil), a.got...)
		a.mu.Unlock()
		if len(got) >= n {
			return got
		}
		select {
		case <-a.ch:
		case <-deadline:
			t.Fatalf("%d advertisements, want %d: %q", len(got), n, got)
		}
	}
}

func TestNewCentralNeedsTheAdapter(t *testing.T) {
	b := newFakeBus()
	if _, err := newCentral(b, "/org/bluez/hci7"); !errors.Is(err, ErrNoAdapter) {
		t.Fatalf("err %v, want ErrNoAdapter", err)
	}
}

func TestDBusFailuresNameTheDBusError(t *testing.T) {
	b := newFakeBus()
	b.failWith(methodGetManaged, dbus.Error{Name: "org.freedesktop.DBus.Error.AccessDenied", Body: []any{"Rejected send message"}})
	_, err := newCentral(b, testAdapter)
	var e *Error
	if !errors.As(err, &e) || e.Name != "org.freedesktop.DBus.Error.AccessDenied" || !strings.Contains(err.Error(), "AccessDenied") || !strings.Contains(err.Error(), "Rejected send message") {
		t.Fatalf("err %v", err)
	}

	b = newFakeBus()
	c := startCentral(t, b)
	b.failWith(ifaceAdapter+".StartDiscovery", &dbus.Error{Name: "org.bluez.Error.NotReady", Body: []any{"Resource Not Ready"}})
	if err := c.StartScanning(context.Background()); dbusName(err) != "org.bluez.Error.NotReady" || !strings.Contains(err.Error(), "org.bluez.Adapter1.StartDiscovery on /org/bluez/hci0") {
		t.Fatalf("StartScanning: %v", err)
	}
}

func TestScanFilterAndDiscovery(t *testing.T) {
	b := newFakeBus()
	c := startCentral(t, b)
	a := newAds()
	c.SetDiscoveryCallback(a.on)
	ctx := context.Background()
	if err := c.StartScanning(ctx); err != nil {
		t.Fatal(err)
	}
	filter, _ := b.lastArgs(ifaceAdapter + ".SetDiscoveryFilter")[0].(map[string]dbus.Variant)
	// No UUIDs: the kernel's service discovery would drop a Matter
	// advertisement, which has no UUID field (ChipDeviceScanner.cpp:227-236).
	if _, has := filter["UUIDs"]; has ||
		filter["Transport"].Value() != "le" || filter["DuplicateData"].Value() != true || len(filter) != 2 {
		t.Fatalf("discovery filter %v", filter)
	}
	if got := b.called(); !slices.Equal(got[len(got)-2:], []string{"/org/bluez/hci0 org.bluez.Adapter1.SetDiscoveryFilter", "/org/bluez/hci0 org.bluez.Adapter1.StartDiscovery"}) {
		t.Fatalf("calls %q", got)
	}
	// A second start while BlueZ discovers: InProgress is the discovery running.
	b.failWith(ifaceAdapter+".StartDiscovery", dbus.Error{Name: "org.bluez.Error.InProgress", Body: []any{"Operation already in progress"}})
	if err := c.StartScanning(ctx); err != nil {
		t.Fatalf("restart: %v", err)
	}

	d := b.addDevice(&bletest.Peripheral{Address: "AA:BB:CC:DD:EE:01", Discriminator: 3840, VendorID: 0xFFF1, ProductID: 0x8000}, 0)
	sd := []byte{0, 0, 0x0f, 0xf1, 0xff, 0x00, 0x80, 0}
	d.advertise(sd) // InterfacesAdded
	d.advertise(sd) // PropertiesChanged ServiceData (DuplicateData)
	// Data of another length, another UUID, another adapter: ignored.
	d.advertise([]byte{1, 2, 3})
	b.addObject("/org/bluez/hci0/dev_AA_BB_CC_DD_EE_02", map[string]map[string]dbus.Variant{ifaceDevice: {
		"Address":     dbus.MakeVariant("AA:BB:CC:DD:EE:02"),
		"ServiceData": dbus.MakeVariant(map[string]dbus.Variant{"0000feaa-0000-1000-8000-00805f9b34fb": dbus.MakeVariant(sd)}),
	}})
	b.addObject("/org/bluez/hci1/dev_AA_BB_CC_DD_EE_03", map[string]map[string]dbus.Variant{ifaceDevice: {
		"Address":     dbus.MakeVariant("AA:BB:CC:DD:EE:03"),
		"ServiceData": dbus.MakeVariant(map[string]dbus.Variant{btp.ServiceUUID: dbus.MakeVariant(sd)}),
	}})
	d.advertise(sd)
	got := a.wait(t, 3)
	time.Sleep(50 * time.Millisecond)
	a.mu.Lock()
	n := len(a.got)
	a.mu.Unlock()
	if n != 3 || got[0] != "AA:BB:CC:DD:EE:01="+string(sd) || got[2] != got[0] {
		t.Fatalf("advertisements %q", a.got)
	}

	// A new callback hears the peripherals discovered so far.
	again := newAds()
	c.SetDiscoveryCallback(again.on)
	if got := again.wait(t, 1); got[0] != "AA:BB:CC:DD:EE:01="+string(sd) {
		t.Fatalf("replay %q", got)
	}

	if err := c.StopScanning(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.StopScanning(ctx); err != nil {
		t.Fatal(err)
	}
	stops := 0
	for _, call := range b.called() {
		if strings.HasSuffix(call, ".StopDiscovery") {
			stops++
		}
	}
	if stops != 1 {
		t.Fatalf("%d StopDiscovery calls, want 1", stops)
	}
}

// TestDialOverBlueZ runs the scanner and the BTP channel of controller/ble
// over the central, against a bletest peripheral behind the fake bus.
func TestDialOverBlueZ(t *testing.T) {
	for _, tc := range []struct {
		name string
		mtu  uint16
		late bool
	}{{"mtu 185", 185, false}, {"no MTU property, late resolve", 0, true}} {
		t.Run(tc.name, func(t *testing.T) {
			b := newFakeBus()
			got := make(chan []byte, 1)
			closed := make(chan struct{})
			p := &bletest.Peripheral{
				Address: "AA:BB:CC:DD:EE:01", Discriminator: 3840, VendorID: 0xFFF1, ProductID: 0x8000,
				Serve: func(conn *btp.Conn) (func([]byte), func()) {
					return func(m []byte) { got <- m }, func() { close(closed) }
				},
			}
			d := b.addDevice(p, tc.mtu)
			d.lateResolve = tc.late
			c := startCentral(t, b)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			sc := ble.NewScanner(c, ble.ScannerOptions{})
			defer func() { _ = sc.Close(ctx) }()
			go func() {
				time.Sleep(20 * time.Millisecond)
				d.advertise(p.ServiceData())
			}()
			devs, err := sc.Find(ctx, discovery.ByLongDiscriminator(3840), 5*time.Second, false)
			if err != nil || len(devs) != 1 || devs[0].Peripheral.Address != p.Address {
				t.Fatalf("Find: %v %v", devs, err)
			}
			ch, err := ble.Dial(ctx, c, devs[0].Peripheral, ble.DialOptions{})
			if err != nil {
				t.Fatalf("Dial: %v", err)
			}
			msg := bytes.Repeat([]byte{0xA5}, 400) // several segments either way
			if err := ch.Conn().Send(msg); err != nil {
				t.Fatal(err)
			}
			select {
			case m := <-got:
				if !bytes.Equal(m, msg) {
					t.Fatalf("peripheral got %d bytes", len(m))
				}
			case <-time.After(10 * time.Second):
				t.Fatal("no message at the peripheral")
			}
			if err := ch.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
			select {
			case <-closed:
			case <-time.After(5 * time.Second):
				t.Fatal("the peripheral's session did not end")
			}
			want := []string{
				string(d.path) + " org.bluez.Device1.Connect",
				// The handshake request on C1, then C2 subscribed
				// (NobleBleChannel.ts:592-662).
				string(d.char(0x0b)) + " org.bluez.GattCharacteristic1.WriteValue",
				string(d.char(0x0d)) + " org.bluez.GattCharacteristic1.StartNotify",
				string(d.char(0x0d)) + " org.bluez.GattCharacteristic1.StopNotify",
				string(d.path) + " org.bluez.Device1.Disconnect",
			}
			var seen []string
			for _, call := range b.called() {
				if slices.Contains(want, call) && !slices.Contains(seen, call) {
					seen = append(seen, call)
				}
			}
			if !slices.Equal(seen, want) {
				t.Fatalf("call order %q, want %q", seen, want)
			}
			if opts, _ := b.lastArgs(ifaceGattChar + ".WriteValue")[1].(map[string]dbus.Variant); opts["type"].Value() != "request" {
				t.Fatalf("WriteValue options %v", opts)
			}
		})
	}
}

// connectTo connects the central to a device behind the bus.
func connectTo(t *testing.T, mtu uint16, noC2 bool) (*fakeBus, *fakeDevice, *central, ble.Connection, error) {
	t.Helper()
	b := newFakeBus()
	p := &bletest.Peripheral{Address: "AA:BB:CC:DD:EE:01", Discriminator: 3840}
	d := b.addDevice(p, mtu)
	d.noC2 = noC2
	c := startCentral(t, b)
	d.advertise(p.ServiceData())
	time.Sleep(20 * time.Millisecond)
	conn, err := c.Connect(context.Background(), ble.Peripheral{Address: p.Address})
	return b, d, c, conn, err
}

func TestConnectNeedsC1AndC2(t *testing.T) {
	b, d, _, _, err := connectTo(t, 0, true)
	if !errors.Is(err, ErrNoMatterService) {
		t.Fatalf("err %v, want ErrNoMatterService", err)
	}
	if !slices.Contains(b.called(), string(d.path)+" org.bluez.Device1.Disconnect") {
		t.Fatal("the connection was not dropped")
	}
}

func TestConnectionEndsOnConnectedFalse(t *testing.T) {
	_, d, c, conn, err := connectTo(t, 247, false)
	if err != nil {
		t.Fatal(err)
	}
	if conn.ATTMTU() != 247 {
		t.Fatalf("ATTMTU %d", conn.ATTMTU())
	}
	if _, err := c.Connect(context.Background(), ble.Peripheral{Address: d.addr}); err == nil {
		t.Fatal("a second connection to the same device was opened")
	}
	if v, err := conn.ReadC3(context.Background()); err != nil || !bytes.Equal(v, []byte{0x15, 0x30}) {
		t.Fatalf("ReadC3 %x %v", v, err)
	}
	d.setConnected(false)
	select {
	case <-conn.Disconnected():
	case <-time.After(5 * time.Second):
		t.Fatal("Disconnected not closed on Connected=false")
	}
	if err := conn.WriteC1(context.Background(), []byte{1}); !errors.Is(err, btp.ErrDisconnected) {
		t.Fatalf("WriteC1 after the drop: %v", err)
	}
	if err := conn.SubscribeC2(context.Background(), func([]byte) {}); !errors.Is(err, btp.ErrDisconnected) {
		t.Fatalf("SubscribeC2 after the drop: %v", err)
	}
	if err := conn.UnsubscribeC2(context.Background()); err != nil {
		t.Fatalf("UnsubscribeC2 after the drop: %v", err)
	}
	if err := conn.Disconnect(); err != nil {
		t.Fatalf("Disconnect after the drop: %v", err)
	}
}

func TestWriteToANotConnectedDeviceIsADisconnect(t *testing.T) {
	b, d, _, conn, err := connectTo(t, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if conn.ATTMTU() != 0 {
		t.Fatalf("ATTMTU %d without the MTU property", conn.ATTMTU())
	}
	b.failWith(string(d.char(0x0b))+" "+ifaceGattChar+".WriteValue", dbus.Error{Name: "org.bluez.Error.NotConnected", Body: []any{"Not connected"}})
	if err := conn.WriteC1(context.Background(), []byte{1}); !errors.Is(err, btp.ErrDisconnected) || dbusName(err) != "org.bluez.Error.NotConnected" {
		t.Fatalf("WriteC1: %v", err)
	}
}

func TestDeviceRemovedAndBusClosedEndConnections(t *testing.T) {
	b, d, c, conn, err := connectTo(t, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	b.emit(&dbus.Signal{Path: "/", Name: sigIfacesRemoved, Body: []any{d.path, []string{ifaceDevice}}})
	select {
	case <-conn.Disconnected():
	case <-time.After(5 * time.Second):
		t.Fatal("Disconnected not closed on InterfacesRemoved")
	}
	// Reconnect, then the bus goes.
	d.setConnected(false)
	conn, err = c.Connect(context.Background(), ble.Peripheral{Address: d.addr})
	if err != nil {
		t.Fatal(err)
	}
	_ = b.Close()
	select {
	case <-conn.Disconnected():
	case <-time.After(5 * time.Second):
		t.Fatal("Disconnected not closed when the bus closed")
	}
}

func TestConnectFailsWhenTheDeviceDrops(t *testing.T) {
	b := newFakeBus()
	p := &bletest.Peripheral{Address: "AA:BB:CC:DD:EE:01"}
	d := b.addDevice(p, 0)
	d.lateResolve = true
	c := startCentral(t, b)
	d.advertise(p.ServiceData())
	go func() { time.Sleep(10 * time.Millisecond); d.setConnected(false) }()
	if _, err := c.Connect(context.Background(), ble.Peripheral{Address: p.Address}); !errors.Is(err, btp.ErrDisconnected) {
		t.Fatalf("err %v, want ErrDisconnected", err)
	}
	// A refused Connect is the error, not a disconnect.
	b.failWith(string(d.path)+" "+ifaceDevice+".Connect", dbus.Error{Name: "org.bluez.Error.Failed", Body: []any{"le-connection-abort-by-local"}})
	if _, err := c.Connect(context.Background(), ble.Peripheral{Address: p.Address}); dbusName(err) != "org.bluez.Error.Failed" {
		t.Fatalf("err %v", err)
	}
	if _, err := c.Connect(context.Background(), ble.Peripheral{Address: "11:22:33:44:55:66"}); err != nil {
		t.Logf("unknown device: %v", err)
	}
}

func TestClosedCentralRefusesWork(t *testing.T) {
	b := newFakeBus()
	c := startCentral(t, b)
	_ = c.Close()
	_ = c.Close()
	if err := c.StartScanning(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.StopScanning(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Connect(context.Background(), ble.Peripheral{Address: "AA:BB:CC:DD:EE:01"}); err == nil {
		t.Fatal("a closed central connected")
	}
}

func TestDevicePathAndAddress(t *testing.T) {
	b := newFakeBus()
	c := startCentral(t, b)
	if p := c.devicePath("aa:bb:cc:dd:ee:ff"); p != "/org/bluez/hci0/dev_AA_BB_CC_DD_EE_FF" {
		t.Fatalf("path %s", p)
	}
	if a := c.learnAddress("/org/bluez/hci0/dev_11_22_33_44_55_66", nil); a != "11:22:33:44:55:66" {
		t.Fatalf("address %s", a)
	}
	if p := c.devicePath("11:22:33:44:55:66"); p != "/org/bluez/hci0/dev_11_22_33_44_55_66" {
		t.Fatalf("path %s", p)
	}
	if adapterPath("hci1") != "/org/bluez/hci1" {
		t.Fatal("adapter path")
	}
	if wrap("x", "/", nil) != nil || dbusName(errors.New("plain")) != "" {
		t.Fatal("wrap of nil or a plain error")
	}
	if e := wrap("x", "/", context.Canceled); !errors.Is(e, context.Canceled) || strings.Contains(e.Error(), ": :") {
		t.Fatalf("wrap %v", e)
	}
}
