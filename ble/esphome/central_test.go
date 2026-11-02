// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package esphome_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/ble"
	"github.com/SukramJ/go-fabric/controller/ble/bletest"
	"github.com/SukramJ/go-fabric/transport/btp"

	"github.com/SukramJ/go-fabric/ble/esphome"
	"github.com/SukramJ/go-fabric/ble/esphome/esphometest"
	"github.com/SukramJ/go-fabric/ble/esphome/internal/wire"
)

var testKey = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))

func fake(t *testing.T, opts esphometest.Options, ps ...*bletest.Peripheral) *esphometest.Proxy {
	t.Helper()
	f, err := esphometest.New(opts, ps...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.Close)
	return f
}

func central(t *testing.T, opts esphome.Options, proxies ...esphome.Proxy) *esphome.Central {
	t.Helper()
	c, err := esphome.NewCentral(proxies, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func peripheral(addr string) *bletest.Peripheral {
	return &bletest.Peripheral{Address: addr, Discriminator: 3840, VendorID: 0xFFF1, ProductID: 0x8001}
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func count(events []string, prefix string) int {
	n := 0
	for _, e := range events {
		if strings.HasPrefix(e, prefix) {
			n++
		}
	}
	return n
}

// seen collects the discovery callback's reports.
type seen struct {
	mu   sync.Mutex
	adds map[string][]byte
}

func watch(c *esphome.Central) *seen {
	s := &seen{adds: map[string][]byte{}}
	c.SetDiscoveryCallback(func(p ble.Peripheral, data []byte) {
		s.mu.Lock()
		s.adds[p.Address] = data
		s.mu.Unlock()
	})
	return s
}

func (s *seen) get(addr string) (data []byte, reports int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.adds[addr], len(s.adds)
}

// scanFor starts the scan and waits until addr was reported.
func scanFor(t *testing.T, c *esphome.Central, addr string) *seen {
	t.Helper()
	s := watch(c)
	if err := c.StartScanning(context.Background()); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the advertisement of "+addr, func() bool { d, _ := s.get(addr); return d != nil })
	return s
}

// TestScanReportsMatterAdvertisements: the raw advertisements are parsed,
// the Matter service data of 0xFFF6 is reported, the other device's
// advertisement is dropped, and the scan's end unsubscribes.
func TestScanReportsMatterAdvertisements(t *testing.T) {
	for _, raw := range []bool{true, false} {
		opts := esphometest.Options{}
		if !raw {
			opts.FeatureFlags = esphometest.DefaultFeatureFlags &^ wire.FeatureRawAdvertisements
		}
		p := peripheral("C0:FF:EE:00:0B:1E")
		f := fake(t, opts, p)
		c := central(t, esphome.Options{}, esphome.Proxy{Addr: f.Addr()})
		s := scanFor(t, c, p.Address)
		data, n := s.get(p.Address)
		if n != 1 || !bytes.Equal(data, p.ServiceData()) {
			t.Fatalf("raw=%v: %d reports, data %x, want %x", raw, n, data, p.ServiceData())
		}
		if adv, err := btp.DecodeAdvertisementServiceData(data); err != nil || adv.Discriminator != 3840 {
			t.Fatalf("service data %+v %v", adv, err)
		}
		if !c.IsPeripheralReachable(p.Address) || c.IsPeripheralReachable("00:00:00:00:00:01") || c.IsPeripheralReachable("bad") {
			t.Fatal("reachability")
		}
		if err := c.StopScanning(context.Background()); err != nil {
			t.Fatal(err)
		}
		eventually(t, "the unsubscribe", func() bool { return count(f.Events(), "unsubscribe") == 1 })
	}
}

// TestConnectAndGATT: the connect carries the advertisement's address
// type, the MTU sets the BTP segment size, the GATT database is mapped (in
// the short and the 128-bit UUID forms), C2's CCCD is written before the
// first indication arrives, C3 reads, and a disconnect from either side
// reaches Disconnected.
func TestConnectAndGATT(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		got := make(chan []byte, 4)
		p := peripheral("C0:FF:EE:00:0B:1E")
		f := fake(t, esphometest.Options{LegacyUUIDs: legacy, MTU: 100, C3: []byte{0x15, 0x30}}, p)
		c := central(t, esphome.Options{}, esphome.Proxy{Addr: f.Addr(), Name: "lab"})
		scanFor(t, c, p.Address)
		ctx := context.Background()
		conn, err := c.Connect(ctx, ble.Peripheral{Address: p.Address})
		if err != nil {
			t.Fatal(err)
		}
		if conn.ATTMTU() != 100 || btp.SegmentSizeFromATTMTU(conn.ATTMTU()) != 97 {
			t.Fatalf("ATT_MTU %d", conn.ATTMTU())
		}
		c3, err := conn.ReadC3(ctx)
		if err != nil || !bytes.Equal(c3, []byte{0x15, 0x30}) {
			t.Fatalf("C3 %x %v", c3, err)
		}
		req := btp.EncodeHandshakeRequest(btp.HandshakeRequest{Versions: btp.SupportedVersions, ATTMTU: 97, ClientWindowSize: btp.MaximumWindowSize})
		if err := conn.WriteC1(ctx, req); err != nil {
			t.Fatal(err)
		}
		if err := conn.SubscribeC2(ctx, func(b []byte) { got <- b }); err != nil {
			t.Fatal(err)
		}
		select {
		case resp := <-got:
			if !btp.IsHandshakeResponse(resp) {
				t.Fatalf("not a handshake response: %x", resp)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("no indication")
		}
		ev := f.Events()
		want := []string{"connect C0:FF:EE:00:0B:1E type=1", "services", "read 0x0013", "write 0x000e", "notify 0x0010 on", "cccd 0x0011 0200", "indicate"}
		var order []string
		for _, e := range ev {
			if slices.Contains(want, e) && !slices.Contains(order, e) {
				order = append(order, e)
			}
		}
		if !slices.Equal(order, want) {
			t.Fatalf("legacy=%v: events %q", legacy, ev)
		}
		if err := conn.UnsubscribeC2(ctx); err != nil {
			t.Fatal(err)
		}
		eventually(t, "the CCCD cleared", func() bool { return slices.Contains(f.Events(), "cccd 0x0011 0000") })
		f.DisconnectPeripheral(p.Address)
		select {
		case <-conn.Disconnected():
		case <-time.After(5 * time.Second):
			t.Fatal("the disconnect did not propagate")
		}
		if err := conn.WriteC1(ctx, []byte{1}); !errors.Is(err, btp.ErrDisconnected) {
			t.Fatalf("write after the disconnect: %v", err)
		}
		if err := conn.Disconnect(); err != nil {
			t.Fatal(err)
		}
	}
}

// TestCancelledWriteEndsTheConnection (W4-ctlint-05): a GATT write whose
// context ends while its reply is outstanding ends the connection, so the
// late reply — the API carries no request id — cannot be taken for the
// next write's on the same handle.
func TestCancelledWriteEndsTheConnection(t *testing.T) {
	p := peripheral("C0:FF:EE:00:0B:1E")
	f := fake(t, esphometest.Options{}, p)
	c := central(t, esphome.Options{}, esphome.Proxy{Addr: f.Addr(), Name: "lab"})
	scanFor(t, c, p.Address)
	for range 20 {
		conn, err := c.Connect(context.Background(), ble.Peripheral{Address: p.Address})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err = conn.WriteC1(ctx, []byte{1})
		if err == nil { // the reply won the race with the cancel: try again
			_ = conn.Disconnect()
			continue
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("WriteC1: %v", err)
		}
		select {
		case <-conn.Disconnected():
		case <-time.After(5 * time.Second):
			t.Fatal("a cancelled write left the connection up")
		}
		if err := conn.WriteC1(context.Background(), []byte{1}); !errors.Is(err, btp.ErrDisconnected) {
			t.Fatalf("write after the cancel: %v", err)
		}
		_ = conn.Disconnect()
		return
	}
	t.Skip("every reply beat the cancel")
}

func TestConnectErrors(t *testing.T) {
	p := peripheral("C0:FF:EE:00:0B:1E")
	f := fake(t, esphometest.Options{Slots: 1}, p)
	c := central(t, esphome.Options{}, esphome.Proxy{Addr: f.Addr()})
	ctx := context.Background()
	if _, err := c.Connect(ctx, ble.Peripheral{Address: p.Address}); !errors.Is(err, esphome.ErrUnknownPeripheral) {
		t.Fatalf("unseen: %v", err)
	}
	if _, err := c.Connect(ctx, ble.Peripheral{Address: "nope"}); err == nil {
		t.Fatal("a bad address connects")
	}
	other := peripheral("C0:FF:EE:00:0B:1F")
	f.Add(other)
	scanFor(t, c, other.Address)
	eventually(t, "both", func() bool { return c.IsPeripheralReachable(p.Address) })
	conn, err := c.Connect(ctx, ble.Peripheral{Address: p.Address})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ReadC3(ctx); !errors.Is(err, esphome.ErrNoC3) {
		t.Fatalf("C3: %v", err)
	}
	// The one slot is taken: the other peripheral is not reachable, and a
	// connect to it is refused.
	eventually(t, "the slot report", func() bool { return !c.IsPeripheralReachable(other.Address) })
	var ge *esphome.GATTError
	if _, err := c.Connect(ctx, ble.Peripheral{Address: other.Address}); !errors.As(err, &ge) || ge.Op != "connect" || !strings.Contains(ge.Error(), "connect failed") {
		t.Fatalf("connect without a slot: %v", err)
	}
	if err := conn.Disconnect(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-conn.Disconnected():
	case <-time.After(5 * time.Second):
		t.Fatal("Disconnect did not end the connection")
	}
	if (&esphome.GATTError{Op: "write", Handle: 14, Code: 5}).Error() != "esphome: GATT write on handle 0x000e failed (error 0x5)" {
		t.Fatal("GATTError text")
	}
}

// TestBestRSSIWins: two proxies see one peripheral; Connect goes through the
// one with the stronger signal.
func TestBestRSSIWins(t *testing.T) {
	p := peripheral("C0:FF:EE:00:0B:1E")
	weak := fake(t, esphometest.Options{RSSI: -85}, p)
	strong := fake(t, esphometest.Options{RSSI: -50}, p)
	c := central(t, esphome.Options{}, esphome.Proxy{Addr: weak.Addr(), Name: "weak"}, esphome.Proxy{Addr: strong.Addr(), Name: "strong"})
	scanFor(t, c, p.Address)
	eventually(t, "both proxies' reports", func() bool {
		return count(weak.Events(), "subscribe") == 1 && count(strong.Events(), "subscribe") == 1
	})
	time.Sleep(200 * time.Millisecond)
	conn, err := c.Connect(context.Background(), ble.Peripheral{Address: p.Address})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Disconnect() }()
	if count(strong.Events(), "connect ") != 1 || count(weak.Events(), "connect ") != 0 {
		t.Fatalf("strong %q weak %q", strong.Events(), weak.Events())
	}
	// The signals swap: the next connect goes through the other proxy.
	_ = conn.Disconnect()
	weak.SetRSSI(p.Address, -40)
	strong.SetRSSI(p.Address, -90)
	time.Sleep(300 * time.Millisecond)
	conn, err = c.Connect(context.Background(), ble.Peripheral{Address: p.Address})
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Disconnect()
	if count(weak.Events(), "connect ") != 1 {
		t.Fatalf("weak %q", weak.Events())
	}
}

// TestProxyLossDuringSession: the proxy's API connection drops while a BTP
// channel runs: the channel closes with btp.ErrDisconnected, and the
// central reconnects and subscribes again.
func TestProxyLossDuringSession(t *testing.T) {
	got := make(chan struct{}, 4)
	p := peripheral("C0:FF:EE:00:0B:1E")
	p.Serve = func(*btp.Conn) (func([]byte), func()) { return func([]byte) { got <- struct{}{} }, nil }
	f := fake(t, esphometest.Options{}, p)
	c := central(t, esphome.Options{}, esphome.Proxy{Addr: f.Addr()})
	scanFor(t, c, p.Address)
	ch, err := ble.Dial(context.Background(), c, ble.Peripheral{Address: p.Address}, ble.DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	f.DropClients()
	select {
	case <-ch.Closed():
	case <-time.After(10 * time.Second):
		t.Fatal("the channel survived the proxy")
	}
	if !errors.Is(ch.ClosedCause(), btp.ErrDisconnected) {
		t.Fatalf("cause %v", ch.ClosedCause())
	}
	if f.Connections() != 0 {
		// The subscriber left, so the proxy dropped its connections.
		eventually(t, "the slots freed", func() bool { return f.Connections() == 0 })
	}
	eventually(t, "the resubscribe", func() bool { return count(f.Events(), "subscribe") == 2 })
	eventually(t, "the peripheral again", func() bool { return c.IsPeripheralReachable(p.Address) })
}

// TestProxyTakenThenBusy: another client takes the subscription; the
// central reconnects once and takes it back; taken again, the proxy is
// busy until a fresh scan.
func TestProxyTakenThenBusy(t *testing.T) {
	p := peripheral("C0:FF:EE:00:0B:1E")
	f := fake(t, esphometest.Options{}, p)
	c := central(t, esphome.Options{SilenceTimeout: 300 * time.Millisecond}, esphome.Proxy{Addr: f.Addr()})
	scanFor(t, c, p.Address)
	f.Take()
	eventually(t, "the take-back", func() bool { return count(f.Events(), "subscribe") == 2 })
	f.Take()
	eventually(t, "busy", func() bool {
		_, err := c.Connect(context.Background(), ble.Peripheral{Address: p.Address})
		return errors.Is(err, esphome.ErrProxyBusy) || errors.Is(err, esphome.ErrUnknownPeripheral)
	})
	eventually(t, "busy on a re-ask", func() bool { return errors.Is(c.StartScanning(context.Background()), esphome.ErrProxyBusy) })
	if c.IsPeripheralReachable(p.Address) {
		t.Fatal("a busy proxy's peripheral is reachable")
	}
	// A fresh scan may take the proxy again.
	_ = c.StopScanning(context.Background())
	if err := c.StartScanning(context.Background()); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the fresh subscribe", func() bool { return count(f.Events(), "subscribe") == 3 })
}

func TestRefusals(t *testing.T) {
	if _, err := esphome.NewCentral(nil, esphome.Options{}); !errors.Is(err, esphome.ErrNoProxies) {
		t.Fatal(err)
	}
	if _, err := esphome.NewCentral([]esphome.Proxy{{Addr: "x", Key: "short"}}, esphome.Options{}); !errors.Is(err, esphome.ErrMalformedKey) {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		opts  esphometest.Options
		proxy func(addr string) esphome.Proxy
		want  error
	}{
		{"advertisements only", esphometest.Options{FeatureFlags: wire.FeaturePassiveScan | wire.FeatureRawAdvertisements}, func(a string) esphome.Proxy { return esphome.Proxy{Addr: a} }, esphome.ErrNoActiveConnections},
		{"no remote caching", esphometest.Options{FeatureFlags: wire.FeaturePassiveScan | wire.FeatureActiveConnections}, func(a string) esphome.Proxy { return esphome.Proxy{Addr: a} }, esphome.ErrNoRemoteCaching},
		{"capabilities without active connections", esphometest.Options{APIVersionMinor: 15, FeatureFlags: wire.FeaturePassiveScan}, func(a string) esphome.Proxy { return esphome.Proxy{Addr: a} }, esphome.ErrNoActiveConnections},
		{"wrong key", esphometest.Options{Key: testKey}, func(a string) esphome.Proxy {
			return esphome.Proxy{Addr: a, Key: base64.StdEncoding.EncodeToString(make([]byte, 32))}
		}, esphome.ErrInvalidKey},
		{"plaintext at an encrypted proxy", esphometest.Options{Key: testKey}, func(a string) esphome.Proxy { return esphome.Proxy{Addr: a} }, esphome.ErrRequiresEncryption},
		{"key at a plaintext proxy", esphometest.Options{}, func(a string) esphome.Proxy { return esphome.Proxy{Addr: a, Key: testKey} }, esphome.ErrPlaintextDevice},
	}
	for _, tc := range cases {
		f := fake(t, tc.opts)
		good := fake(t, esphometest.Options{})
		_, err := esphome.NewCentral([]esphome.Proxy{tc.proxy(f.Addr()), {Addr: good.Addr()}}, esphome.Options{})
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
	// Every proxy unreachable fails; one of two unreachable does not.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	dead := ln.Addr().String()
	_ = ln.Close()
	if _, err := esphome.NewCentral([]esphome.Proxy{{Addr: dead}}, esphome.Options{ConnectTimeout: 2 * time.Second}); err == nil {
		t.Fatal("an unreachable proxy")
	}
	good := fake(t, esphometest.Options{APIVersionMinor: 15})
	c := central(t, esphome.Options{}, esphome.Proxy{Addr: dead}, esphome.Proxy{Addr: good.Addr()})
	if err := c.StartScanning(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestNoiseProxy(t *testing.T) {
	p := peripheral("C0:FF:EE:00:0B:1E")
	f := fake(t, esphometest.Options{Key: testKey, APIVersionMinor: 16}, p)
	c := central(t, esphome.Options{}, esphome.Proxy{Addr: f.Addr(), Key: testKey})
	scanFor(t, c, p.Address)
	conn, err := c.Connect(context.Background(), ble.Peripheral{Address: p.Address})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Disconnect(); err != nil {
		t.Fatal(err)
	}
}

func TestParseProxy(t *testing.T) {
	for in, want := range map[string]esphome.Proxy{
		"proxy.local":                  {Addr: "proxy.local"},
		"10.0.0.5:6054,a2V5":           {Addr: "10.0.0.5:6054", Key: "a2V5"},
		"proxy.local,":                 {Addr: "proxy.local"},
		"bluetooth-proxy-kitchen,KEY=": {Addr: "bluetooth-proxy-kitchen", Key: "KEY="},
	} {
		got, err := esphome.ParseProxy(in)
		if err != nil || got != want {
			t.Errorf("%q: %+v %v", in, got, err)
		}
	}
	if got, err := esphome.ParseProxy("\t[fe80::1]:6053 , a2V5 "); err != nil || got.Addr != "[fe80::1]:6053" || got.Key != "a2V5" {
		t.Errorf("padded: %+v %v", got, err)
	}
	if _, err := esphome.ParseProxy(",key"); err == nil {
		t.Fatal("an empty address parses")
	}
}
