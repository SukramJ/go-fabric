// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package esphome

import (
	"net"
	"sync"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/clock/clocktest"
	"thingfabric.dev/go-fabric/controller/ble"

	"thingfabric.dev/go-fabric/ble/esphome/internal/wire"
)

// rawProxy is a scripted plaintext ESPHome API peer for the package's own
// tests, where esphometest's well-behaved proxy cannot misbehave: it
// answers the hello, the device information (API 1.14, a commissioning
// proxy's flags) and pings itself and hands every other message of
// connection n (0 the first) to script, which writes what it likes; false
// from script closes the connection.
type rawProxy struct {
	ln     net.Listener
	script func(f wire.Framer, n int, typ uint16, payload []byte) bool
	// hello, when set, is called after the device information with the
	// connection's number; false closes the connection there.
	hello func(n int) bool

	mu      sync.Mutex
	accepts int
	conns   []net.Conn
}

func newRawProxy(t *testing.T, script func(f wire.Framer, n int, typ uint16, payload []byte) bool) *rawProxy {
	t.Helper()
	return newRawProxyWith(t, nil, script)
}

// newRawProxyWith is newRawProxy with a hello hook.
func newRawProxyWith(t *testing.T, hello func(n int) bool, script func(f wire.Framer, n int, typ uint16, payload []byte) bool) *rawProxy {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	r := &rawProxy{ln: ln, script: script, hello: hello}
	var wg sync.WaitGroup
	t.Cleanup(func() {
		_ = ln.Close()
		r.mu.Lock()
		for _, nc := range r.conns {
			_ = nc.Close()
		}
		r.mu.Unlock()
		wg.Wait()
	})
	wg.Go(func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			r.mu.Lock()
			n := r.accepts
			r.accepts++
			r.conns = append(r.conns, nc)
			r.mu.Unlock()
			wg.Go(func() { r.serve(nc, n) })
		}
	})
	return r
}

func (r *rawProxy) addr() string { return r.ln.Addr().String() }

func (r *rawProxy) accepted() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.accepts
}

// drop closes every connection accepted so far.
func (r *rawProxy) drop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, nc := range r.conns {
		_ = nc.Close()
	}
}

func (r *rawProxy) serve(nc net.Conn, n int) {
	defer func() { _ = nc.Close() }()
	f := wire.NewPlaintext(nc)
	for {
		typ, payload, err := f.ReadMessage()
		if err != nil {
			return
		}
		switch typ {
		case wire.TypeHelloRequest:
			_ = f.WriteMessage(&wire.HelloResponse{APIVersionMajor: 1, APIVersionMinor: 14, Name: "raw-proxy"})
		case wire.TypeDeviceInfoRequest:
			_ = f.WriteMessage(&wire.DeviceInfoResponse{Name: "raw-proxy", BluetoothProxyFeatureFlags: wire.FeaturePassiveScan | wire.FeatureActiveConnections | wire.FeatureRemoteCaching})
			if r.hello != nil && !r.hello(n) {
				return
			}
		case wire.TypePingRequest:
			_ = f.WriteMessage(wire.Empty(wire.TypePingResponse))
		default:
			if r.script != nil && !r.script(f, n, typ, payload) {
				return
			}
		}
	}
}

// matterDatabase is a services frame with the Matter service: C1, C2
// with its CCCD.
func matterDatabase(addr uint64) *wire.BluetoothGATTGetServicesResponse {
	return &wire.BluetoothGATTGetServicesResponse{Address: addr, Services: []wire.GATTService{{
		UUID: wire.UUID{Words: serviceWords}, Handle: 0x0c, Characteristics: []wire.GATTCharacteristic{
			{UUID: wire.UUID{Words: c1Words}, Handle: 0x0e, Properties: 0x08},
			{UUID: wire.UUID{Words: c2Words}, Handle: 0x10, Properties: propIndicate, Descriptors: []wire.GATTDescriptor{{UUID: wire.UUID{Short: 0x2902}, Handle: 0x11}}},
		},
	}}}
}

// connectThrough is a Connect to addr through the central's one proxy,
// which is made to have reported it.
func connectThrough(t *testing.T, c *Central, addr uint64) (ble.Connection, error) {
	t.Helper()
	ps := c.proxyList()
	if len(ps) != 1 {
		t.Fatalf("%d proxies, want 1", len(ps))
	}
	c.discovered(ps[0], addr, 1, -50, nil)
	return c.Connect(t.Context(), ble.Peripheral{Address: macString(addr)})
}

// TestHandlesSetBeforeIndications (R2-P6-conc-2007): an indication that
// arrives while the connect is still taking the Matter service's handles
// is no data race. Run under -race (make esphome-check).
func TestHandlesSetBeforeIndications(t *testing.T) {
	const addr = 0x0a0b0c0d0e0f
	rp := newRawProxy(t, func(f wire.Framer, _ int, typ uint16, payload []byte) bool {
		switch typ {
		case wire.TypeBluetoothDeviceRequest:
			var m wire.BluetoothDeviceRequest
			_ = m.Unmarshal(payload)
			_ = f.WriteMessage(&wire.BluetoothDeviceConnectionResponse{Address: addr, Connected: m.RequestType != wire.DeviceRequestDisconnect, MTU: 247})
		case wire.TypeBluetoothGATTGetServicesRequest:
			_ = f.WriteMessage(matterDatabase(addr))
			_ = f.WriteMessage(&wire.AddressHandle{MessageType: wire.TypeBluetoothGATTGetServicesDoneResponse, Address: addr})
			// Indications on C2 at once, as the handles are taken.
			for range 64 {
				_ = f.WriteMessage(&wire.AddressHandle{MessageType: wire.TypeBluetoothGATTNotifyDataResponse, Address: addr, Handle: 0x10, Data: []byte{1}})
			}
		default:
		}
		return true
	})
	c, err := NewCentral([]Proxy{{Addr: rp.addr()}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	k, err := connectThrough(t, c, addr)
	if err != nil {
		t.Fatal(err)
	}
	_ = k.Disconnect()
}

// TestReconnectBackoffGrowsWhileConnectionsFail (R2-P6-fsm-2008): a proxy
// that answers the hello and then drops the connection is redialled on a
// growing backoff, not every second: only a connection that held for
// SilenceTimeout starts the ladder afresh.
func TestReconnectBackoffGrowsWhileConnectionsFail(t *testing.T) {
	clk := clocktest.New(time.Unix(1_700_000_000, 0))
	// The first connection holds until the test drops it; every later one
	// closes after the device information.
	rp := newRawProxyWith(t, func(n int) bool { return n == 0 }, nil)
	c, err := NewCentral([]Proxy{{Addr: rp.addr()}}, Options{Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	ladder := map[time.Duration]bool{}
	for d := initialBackoff; d <= maxBackoff; d *= 2 {
		ladder[d] = true
	}
	// armedBackoff is the reconnect wait the run loop armed.
	armedBackoff := func() time.Duration {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			for _, d := range clk.Armed() {
				if ladder[d] {
					return d
				}
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatal("no reconnect wait armed")
		return 0
	}
	rp.drop()
	for _, want := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second} {
		got := armedBackoff()
		if got != want {
			t.Fatalf("reconnect wait %v, want %v", got, want)
		}
		before := rp.accepted()
		clk.Advance(got)
		deadline := time.Now().Add(5 * time.Second)
		for rp.accepted() == before {
			if time.Now().After(deadline) {
				t.Fatal("no redial after the wait")
			}
			time.Sleep(time.Millisecond)
		}
	}
}
