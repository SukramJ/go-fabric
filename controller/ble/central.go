// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package ble

import (
	"context"
	"time"
)

// Peripheral is a BLE peripheral as the platform names it — matter.js
// BlePeripheral (packages/protocol/src/common/BleScanner.ts:37-39).
type Peripheral struct {
	// Address is the platform's address of the peripheral (a MAC address
	// under BlueZ).
	Address string
}

// Central is what a platform BLE stack offers the commissioner: the scan
// of Matter advertisements — matter.js BleScannerClient
// (common/BleScanner.ts:41-64) — and the GATT connection to one peripheral.
// Implementations: the BlueZ module (phase 13) and ble/bletest.
type Central interface {
	// SetDiscoveryCallback installs the receiver of every advertisement
	// of the Matter service: the peripheral and the service data of the
	// Matter service UUID (btp.ServiceUUID).
	SetDiscoveryCallback(func(p Peripheral, serviceData []byte))
	// StartScanning starts the scan. It is asked again while a scan runs,
	// so a radio that stopped on its own resumes (common/BleScanner.ts:186-189).
	StartScanning(ctx context.Context) error
	// StopScanning stops the scan.
	StopScanning(ctx context.Context) error
	// Connect opens a GATT connection to the peripheral and resolves the
	// Matter service's C1, C2 and C3 characteristics.
	Connect(ctx context.Context, p Peripheral) (Connection, error)
}

// Reachability is a [Central] that can lose access to a peripheral it
// reported, such as one routing through proxies (common/BleScanner.ts:46-52).
// A Central without it keeps every discovered peripheral available.
type Reachability interface {
	IsPeripheralReachable(address string) bool
}

// ListeningClock is a [Central] that reports how long it listened: its
// radio scanned and it reported every advertisement it received
// (common/BleScanner.ts:54-63). A record states that its device went silent
// only when its age is measured in this time.
type ListeningClock interface {
	// ListeningTime is the total time listened so far.
	ListeningTime() time.Duration
}

// Connection is a GATT connection to one peripheral's Matter service.
type Connection interface {
	// ATTMTU is the negotiated ATT_MTU; 0 when still unknown, which
	// selects the minimum BTP segment size (NobleBleChannel.ts:679-681).
	ATTMTU() int
	// WriteC1 writes C1 (a write with response). A write that fails
	// because the peripheral is gone returns an error wrapping
	// btp.ErrDisconnected (NobleBleChannel.ts:770-781). It must not
	// deliver C2 data on the calling goroutine.
	WriteC1(ctx context.Context, data []byte) error
	// SubscribeC2 subscribes to C2's indications; on receives each one,
	// in order, on a goroutine of the implementation's.
	SubscribeC2(ctx context.Context, on func(data []byte)) error
	// UnsubscribeC2 ends the subscription, which is how a GATT client
	// closes a BTP session (§4.19.4.10, NobleBleChannel.ts:571-574).
	UnsubscribeC2(ctx context.Context) error
	// ReadC3 reads the additional commissioning data; an error when the
	// peripheral has none.
	ReadC3(ctx context.Context) ([]byte, error)
	// Disconnect drops the GATT connection.
	Disconnect() error
	// Disconnected is closed when the connection is gone, from either side.
	Disconnected() <-chan struct{}
}
