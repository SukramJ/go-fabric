// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package btp implements the Bluetooth Transport Protocol (Matter Core
// Specification §4.19): the codec of the handshake and the data
// segments, and the session that fragments Matter messages into
// segments, reassembles them, numbers and acknowledges them inside a
// receive window and closes the session on a protocol violation or a
// missed acknowledgement. It is pure Go with no dependency and opens no
// radio: the GATT characteristics a session writes to and is fed from
// are callbacks (ADR 0024, decision 2).
//
// A port of matter.js at 0ceb202d: packages/protocol/src/ble/BleConsts.ts
// (the constants), packages/protocol/src/codec/BtpCodec.ts (the codec and
// the BLE advertisement's service data) and
// packages/protocol/src/ble/BtpSessionHandler.ts (the session, both
// roles). The central role is what the commissioner uses
// (controller/ble); the peripheral role serves the tests and a later
// device-side BLE transport — a node of this module never advertises
// over BLE (ADR 0024, decision 1).
//
// A BTP session is a reliable channel: Matter's MRP does not run over
// it. matter.js's BleChannel declares `isReliable = true`
// (packages/protocol/src/ble/Ble.ts:38) and a session on a reliable
// channel does not use MRP (packages/protocol/src/session/Session.ts:355-356,
// `usesMrp = supportsMRP && !channel?.isReliable`). [Conn] therefore
// reports [Conn.Reliable] true, as transport/tcp does.
//
// Concurrency: matter.js runs the session on one event loop and yields
// only at its awaits — the GATT write, the disconnect and the hand-over
// of a reassembled message. The Go session keeps its state under one
// mutex and releases it exactly at those points, so the interleavings a
// concurrent incoming segment, a timer and a send can produce are the
// ones matter.js can produce.
package btp
