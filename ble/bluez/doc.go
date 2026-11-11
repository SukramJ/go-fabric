// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package bluez is the BlueZ platform of both BLE roles over the D-Bus
// system bus, without cgo (ADR 0024 decision 3): the commissioner's
// [ble.Central] and its [ble.Connection]s ([NewCentral]), and the node's
// contract.BLEPeripheral ([NewPeripheral]). It is a module of its own, so
// the main module keeps its dependencies and a host without BLE never
// pulls godbus.
//
// The peripheral follows connectedhomeip's Linux platform
// (src/platform/Linux/bluez/ at 6170af84) — matter.js's peripheral is
// bleno on a raw HCI socket, which has no D-Bus to mirror: a GATT
// application under an ObjectManager root registered with
// GattManager1.RegisterApplication, C1 and C2 acquired as socket pairs
// (AcquireWrite, AcquireNotify), the advertisement an LEAdvertisement1
// registered with LEAdvertisingManager1 and updated in place. What it
// carries — the payload, the schedule, the BTP session — is the main
// module's (bridge, transport/btp).
//
// The central's sequence is matter.js's (packages/nodejs-ble/src/NobleBleClient.ts,
// NobleBleChannel.ts at 0ceb202d): scan for the Matter service with
// duplicates reported, connect, find the Matter service and its C1, C2 and
// C3 characteristics, take the ATT_MTU, subscribe C2, write C1. matter.js
// speaks to the radio through noble, so the D-Bus mapping has no matter.js
// line; it cites the BlueZ API documents
// (https://github.com/bluez/bluez/tree/master/doc) by file and section:
//
//   - org.bluez.Adapter.rst — SetDiscoveryFilter (Transport,
//     DuplicateData), StartDiscovery, StopDiscovery, the Discovering
//     property;
//   - org.bluez.Device.rst — Connect, Disconnect, the Address, Connected,
//     ServicesResolved and ServiceData properties;
//   - org.bluez.GattService.rst — the UUID and Device properties;
//   - org.bluez.GattCharacteristic.rst — ReadValue, WriteValue (option
//     type "request"), StartNotify, StopNotify, the UUID, Service, Value
//     and MTU properties. The MTU property exists since BlueZ 5.62 and is
//     the link's current ATT_MTU, 23 before an exchange; its
//     PropertiesChanged on C1 is the exchange landing, which the
//     connection reports as [ble.MTUExchange], so controller/ble waits for
//     a pending exchange as matter.js waits for noble's "mtu" event.
//
// The UUIDs are transport/btp's (ServiceUUID, C1/C2/C3CharacteristicUUID).
// Every D-Bus failure is an [*Error] naming the D-Bus error
// (org.bluez.Error.NotReady, org.freedesktop.DBus.Error.AccessDenied …), so
// a missing right on the system bus reads as one. Linux only; elsewhere
// [NewCentral] and [NewPeripheral] return [ErrUnsupported].
package bluez

import (
	"errors"
	"fmt"
)

// ErrUnsupported is NewCentral or NewPeripheral on a platform without
// BlueZ.
var ErrUnsupported = errors.New("bluez: BlueZ is only available on Linux")

// ErrNoMatterService is a Connect to a peripheral whose GATT database has
// no Matter service with C1 and C2 — matter.js's "Missing required Matter
// characteristics" (NobleBleChannel.ts:378-384).
var ErrNoMatterService = errors.New("bluez: no Matter service with C1 and C2")

// Error is a D-Bus call that failed: the operation, the object, and the
// D-Bus error's name and message.
type Error struct {
	// Op is the BlueZ method, such as "org.bluez.Device1.Connect".
	Op string
	// Path is the object the call went to.
	Path string
	// Name is the D-Bus error name, such as
	// "org.freedesktop.DBus.Error.AccessDenied"; empty for a failure that
	// is not a D-Bus error reply (a closed bus, a cancelled context).
	Name string
	// Err is the underlying error.
	Err error
}

func (e *Error) Error() string {
	if e.Name != "" {
		return fmt.Sprintf("bluez: %s on %s: %s: %v", e.Op, e.Path, e.Name, e.Err)
	}
	return fmt.Sprintf("bluez: %s on %s: %v", e.Op, e.Path, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }
