// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package ble

import (
	"context"

	"github.com/SukramJ/go-fabric/client/discovery"
)

// Transport is the scanner s as a discovery transport — matter.js's
// BleScanner in the node's ScannerSet (its ChannelType.BLE,
// packages/protocol/src/common/BleScanner.ts at 862ad75f). Each device it
// reports is [Device.Commissionable]: its InstanceID is the peripheral's
// address, which [Scanner.Device] looks the peripheral up by.
func Transport(s *Scanner) discovery.Transport { return transport{s} }

type transport struct{ s *Scanner }

func (transport) Kind() discovery.TransportKind { return discovery.TransportBLE }

func (t transport) FindContinuously(ctx context.Context, id discovery.Identifier, found func(discovery.Device)) error {
	_, err := t.s.FindContinuously(ctx, id, func(d Device) { found(d.Commissionable()) }, 0)
	if err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}
