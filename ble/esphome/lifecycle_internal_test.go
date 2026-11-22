// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package esphome

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/controller/ble"
	"thingfabric.dev/go-fabric/transport/btp"

	"thingfabric.dev/go-fabric/ble/esphome/internal/wire"
)

// flood writes m every pace until a write fails.
func flood(f wire.Framer, m wire.Message, pace time.Duration) {
	for f.WriteMessage(m) == nil {
		time.Sleep(pace)
	}
}

// callbacks counts calls of a callback and the ones still running.
type callbacks struct{ calls, running atomic.Int64 }

func (c *callbacks) call() {
	c.running.Add(1)
	time.Sleep(time.Millisecond)
	c.calls.Add(1)
	c.running.Add(-1)
}

// settledAfterClose closes c while cb is being called and checks that no
// call runs once Close returned, and none starts.
func settledAfterClose(t *testing.T, c *Central, cb *callbacks) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for cb.calls.Load() < 5 {
		if time.Now().After(deadline) {
			t.Fatal("the callback is not being called")
		}
		time.Sleep(time.Millisecond)
	}
	_ = c.Close()
	if n := cb.running.Load(); n != 0 {
		t.Fatalf("%d calls still running after Close returned", n)
	}
	after := cb.calls.Load()
	time.Sleep(50 * time.Millisecond)
	if n := cb.calls.Load(); n != after {
		t.Fatalf("%d calls after Close returned", n-after)
	}
}

// TestCloseWaitsForCallbacks (R2-P6-conc-2011): once Close returns, no
// discovery callback and no C2 handler runs, nor starts: Close waits for
// each API connection's reader and for the deliveries of the connections
// through it.
func TestCloseWaitsForCallbacks(t *testing.T) {
	const addr = 0x0a0b0c0d0e0f
	ad := btp.EncodeAdvertisementData(0xf00, 0xfff1, 0x8000, false, false)
	t.Run("discovery callback", func(t *testing.T) {
		rp := newRawProxy(t, func(f wire.Framer, _ int, typ uint16, _ []byte) bool {
			if typ == wire.TypeSubscribeBluetoothLEAdvertisementsRequest {
				flood(f, &wire.BluetoothLERawAdvertisementsResponse{Advertisements: []wire.RawAdvertisement{{Address: addr, RSSI: -50, AddressType: 1, Data: ad}}}, 100*time.Microsecond)
				return false
			}
			return true
		})
		c, err := NewCentral([]Proxy{{Addr: rp.addr()}}, Options{})
		if err != nil {
			t.Fatal(err)
		}
		var cb callbacks
		c.SetDiscoveryCallback(func(ble.Peripheral, []byte) { cb.call() })
		if err := c.StartScanning(t.Context()); err != nil {
			t.Fatal(err)
		}
		settledAfterClose(t, c, &cb)
	})
	t.Run("C2 handler", func(t *testing.T) {
		rp := newRawProxy(t, func(f wire.Framer, _ int, typ uint16, payload []byte) bool {
			switch typ {
			case wire.TypeBluetoothDeviceRequest:
				var m wire.BluetoothDeviceRequest
				_ = m.Unmarshal(payload)
				_ = f.WriteMessage(&wire.BluetoothDeviceConnectionResponse{Address: addr, Connected: m.RequestType != wire.DeviceRequestDisconnect, MTU: 247})
			case wire.TypeBluetoothGATTGetServicesRequest:
				_ = f.WriteMessage(matterDatabase(addr))
				_ = f.WriteMessage(&wire.AddressHandle{MessageType: wire.TypeBluetoothGATTGetServicesDoneResponse, Address: addr})
			case wire.TypeBluetoothGATTNotifyRequest:
				_ = f.WriteMessage(&wire.AddressHandle{MessageType: wire.TypeBluetoothGATTNotifyResponse, Address: addr, Handle: 0x10})
			case wire.TypeBluetoothGATTWriteDescriptorRequest:
				_ = f.WriteMessage(&wire.AddressHandle{MessageType: wire.TypeBluetoothGATTWriteResponse, Address: addr, Handle: 0x11})
				flood(f, &wire.AddressHandle{MessageType: wire.TypeBluetoothGATTNotifyDataResponse, Address: addr, Handle: 0x10, Data: []byte{1}}, 2*time.Millisecond)
				return false
			default:
			}
			return true
		})
		c, err := NewCentral([]Proxy{{Addr: rp.addr()}}, Options{})
		if err != nil {
			t.Fatal(err)
		}
		k, err := connectThrough(t, c, addr)
		if err != nil {
			t.Fatal(err)
		}
		var cb callbacks
		if err := k.SubscribeC2(context.Background(), func([]byte) { cb.call() }); err != nil {
			t.Fatal(err)
		}
		settledAfterClose(t, c, &cb)
	})
}
