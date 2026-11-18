// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package esphometest

import (
	"encoding/base64"
	"io"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/ble/bletest"

	"github.com/SukramJ/go-fabric/ble/esphome/internal/wire"
)

// eventually waits up to five seconds for cond.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// TestSubscribeWhileAdvertising (R2-P6-conc-2050): a client that
// subscribes again on one connection — raw, then parsed, as the central
// does after a resubscribe — while the advertise loop forwards a batch is
// no data race. Run under -race (make esphome-check).
func TestSubscribeWhileAdvertising(t *testing.T) {
	per := &bletest.Peripheral{Address: "AA:BB:CC:00:00:01", Discriminator: 1}
	p, err := New(Options{AdvertiseInterval: 50 * time.Microsecond}, per)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	nc, err := net.Dial("tcp", p.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = nc.Close() }()
	go func() { _, _ = io.Copy(io.Discard, nc) }()
	f := wire.NewPlaintext(nc)
	const n = 3000
	for i := range n {
		flags := uint32(0)
		if i%2 == 0 {
			flags = wire.SubscriptionFlagRawAdvertisements
		}
		if err := f.WriteMessage(&wire.SubscribeBluetoothLEAdvertisementsRequest{Flags: flags}); err != nil {
			t.Fatal(err)
		}
	}
	eventually(t, "every subscribe handled", func() bool {
		c := 0
		for _, e := range p.Events() {
			if e == "subscribe" {
				c++
			}
		}
		return c == n
	})
}

// TestCloseReachesAHandshakingClient (R2-P6-err-2052): Close returns while a
// client of a keyed proxy is still in the Noise handshake, and so does
// DropClients' reach — Move leaves no handshaking client on the old port.
func TestCloseReachesAHandshakingClient(t *testing.T) {
	for _, step := range []string{"Close", "Move"} {
		t.Run(step, func(t *testing.T) {
			p, err := New(Options{Key: base64.StdEncoding.EncodeToString(make([]byte, 32))})
			if err != nil {
				t.Fatal(err)
			}
			nc, err := net.Dial("tcp", p.Addr())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = nc.Close() }()
			// The proxy accepted and waits for the client's handshake.
			eventually(t, "the accept", func() bool {
				p.mu.Lock()
				defer p.mu.Unlock()
				return len(p.conns) == 1
			})
			done := make(chan struct{})
			go func() {
				if step == "Move" {
					_ = p.Move()
					eventually(t, "the handshaking client dropped", func() bool {
						p.mu.Lock()
						defer p.mu.Unlock()
						return len(p.conns) == 0
					})
				}
				p.Close()
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatalf("%s left a client in the handshake: Close still blocked", step)
			}
		})
	}
}

// TestSecondConnectReplacesTheSlot (R2-P6-test-2055): a connect for an
// address that already holds a slot leaves one slot and one live link —
// the earlier link ends — and the earlier link's end sends the client
// nothing about the address the new link holds. What the firmware
// answers to such a connect is not modelled; this is the kit's own rule.
func TestSecondConnectReplacesTheSlot(t *testing.T) {
	per := &bletest.Peripheral{Address: "AA:BB:CC:00:00:01", Discriminator: 1}
	p, err := New(Options{AdvertiseInterval: time.Hour}, per)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	nc, err := net.Dial("tcp", p.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = nc.Close() }()
	f := wire.NewPlaintext(nc)
	addr, _ := parseMAC(per.Address)
	// The connection responses for addr, in order.
	responses := make(chan wire.BluetoothDeviceConnectionResponse, 16)
	go func() {
		for {
			typ, payload, err := f.ReadMessage()
			if err != nil {
				return
			}
			if typ == wire.TypeBluetoothDeviceConnectionResponse {
				var m wire.BluetoothDeviceConnectionResponse
				if m.Unmarshal(payload) == nil && m.Address == addr {
					responses <- m
				}
			}
		}
	}()
	if err := f.WriteMessage(&wire.SubscribeBluetoothLEAdvertisementsRequest{}); err != nil {
		t.Fatal(err)
	}
	connect := func() {
		t.Helper()
		if err := f.WriteMessage(&wire.BluetoothDeviceRequest{Address: addr, RequestType: wire.DeviceRequestConnectV3WithoutCache, HasAddressType: true, AddressType: 1}); err != nil {
			t.Fatal(err)
		}
		select {
		case m := <-responses:
			if !m.Connected {
				t.Fatalf("connect answered %+v", m)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("no connection response")
		}
	}
	connect()
	p.mu.Lock()
	first := p.slots[addr]
	p.mu.Unlock()
	connect()
	if n := p.Connections(); n != 1 {
		t.Fatalf("%d slots, want 1", n)
	}
	select {
	case <-first.conn.Disconnected():
	case <-time.After(5 * time.Second):
		t.Fatal("the replaced link is still up")
	}
	select {
	case m := <-responses:
		t.Fatalf("the replaced link's end reported %+v for the address the new link holds", m)
	case <-time.After(200 * time.Millisecond):
	}
	if n := p.Connections(); n != 1 {
		t.Fatalf("%d slots after the replaced link ended, want 1", n)
	}
}
