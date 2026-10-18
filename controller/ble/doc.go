// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package ble is the commissioner's seam to Bluetooth LE (ADR 0024,
// decision 2): the interfaces a platform BLE stack implements
// ([Central], [Connection]), the scanner that turns Matter
// advertisements into commissionable devices ([Scanner]) and the
// commissioning channel that runs PASE over a BTP session to one
// peripheral ([Dial], [Channel]). It carries no dependency and opens no
// radio: the BlueZ implementation is the separate module
// github.com/SukramJ/go-fabric/ble/bluez (ADR 0024, decision 3), an
// in-memory one is in ble/bletest.
//
// A port of matter.js at 0ceb202d: the scanner of
// packages/protocol/src/common/BleScanner.ts (records, deduplication by
// service data, aging, discoveries by identifier) over the advertisement
// codec of packages/protocol/src/codec/BtpCodec.ts, and the central's
// channel of packages/nodejs-ble/src/NobleBleChannel.ts (the BTP
// handshake over C1 and C2, the session's writes on C1, C2 data into the
// session, the disconnect by unsubscribing C2).
//
// A BTP session is reliable, so the channel's exchange manager runs no
// MRP (exchange.Config.Reliable; matter.js Ble.ts:38, Session.ts:355-356).
package ble
