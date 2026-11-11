// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package contract

import (
	"context"
	"time"
)

// BLEPeripheral is the host port of a node commissioned over Bluetooth
// LE: the radio's peripheral role, which the module cannot open itself —
// matter.js BlePeripheralInterface (packages/protocol/src/ble/Ble.ts:31-34
// at db021da0) and the GATT server of its BLE transport
// (packages/nodejs-ble/src/BlenoBleServer.ts). With [WiFiDriver] it is
// one of the two radios a device-side host supplies.
//
// What goes on the air stays in the module: the advertisement's bytes and
// its schedule, the BTP session over the GATT characteristics
// (transport/btp), and when the node is on the radio at all (an
// uncommissioned node only). An implementation is the radio and the GATT
// stack, nothing more.
type BLEPeripheral interface {
	// Advertise starts advertising adv, or replaces the advertisement that
	// runs: the payload and the interval take effect at once. It returns
	// once the radio advertises it.
	Advertise(ctx context.Context, adv BLEAdvertisement) error
	// StopAdvertising ends the advertisement; without one it does nothing.
	StopAdvertising(ctx context.Context) error
	// Serve registers the Matter GATT service — service UUID 0xFFF6 with
	// C1 (write), C2 (indicate) and C3 (read, answered with c3) — and
	// calls accept once for every central that connects to it. accept
	// runs before any of the connection's events is delivered, so the
	// handlers it registers see every one. Serve blocks: when ctx ends it
	// drops every connection, unregisters the service and returns nil.
	// An error is a service that could not be registered (or that the
	// platform withdrew); Serve returns it at once.
	Serve(ctx context.Context, c3 []byte, accept func(BLEConnection)) error
}

// BLEAdvertisement is what a [BLEPeripheral] advertises.
type BLEAdvertisement struct {
	// Data is the complete advertising payload, AD structures included:
	// the flags and the service data of the Matter service — what
	// matter.js hands its peripheral (BtpCodec.encodeBleAdvertisementData,
	// packages/protocol/src/codec/BtpCodec.ts:296-316 at db021da0). A
	// platform that builds the payload itself from fields takes them from
	// these structures.
	Data []byte
	// Interval is the advertising interval.
	Interval time.Duration
}

// BLEConnection is one central's GATT connection to the Matter service,
// as the peripheral sees it. Its method names follow the central's
// connection (controller/ble.Connection) where the two roles meet. The
// handlers are registered during [BLEPeripheral.Serve]'s accept; each is
// called on a goroutine of the implementation's, one event at a time, in
// the order the central caused them — a C1 write the central made before
// it subscribed C2 arrives before the subscription. An event whose
// handler is not registered is dropped.
type BLEConnection interface {
	// Remote names the central (its address, as the platform names it).
	Remote() string
	// OnWrite receives every value the central writes to C1, in order.
	OnWrite(func(value []byte))
	// OnSubscribe receives the central's subscription to C2's
	// indications, with the connection's ATT_MTU (0 when unknown).
	OnSubscribe(func(attMTU int))
	// OnUnsubscribe receives the end of the central's C2 subscription.
	OnUnsubscribe(func())
	// Indicate sends value as an indication on C2 and returns once the
	// central confirmed it. A connection that is gone, or a ctx that
	// ends, returns an error.
	Indicate(ctx context.Context, value []byte) error
	// Disconnect drops the connection. A second call does nothing.
	Disconnect() error
	// Disconnected is closed when the connection is gone, from either
	// side.
	Disconnected() <-chan struct{}
}
