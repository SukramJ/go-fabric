// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package icd holds the parts of the Intermittently Connected Device
// protocol both roles share and each role's runtime state: the Check-In
// counter, the device's active/idle mode machine, the Check-In back-off,
// the suppression of a Check-In a subscription covers, the device's
// registration table and its Check-In sender; and, for a client, the peer
// table that trial-decrypts a received Check-In, the schedule of a peer's
// active mode and the peer's wakefulness a held interaction or a parked
// subscription waits on. The message codec is [checkin].
//
// It is the Go reading of matter.js packages/protocol/src/icd at 862ad75f
// (the eight modules index.ts:7-14 re-exports) and of the device's mode
// machine node/src/behaviors/icd-management/IcdMode.ts, one file per
// matter.js file, each citing it. The package knows no node, bridge or
// controller: the device's ICD Management server (cluster/core) and the
// controller's ICD client (controller/client) own the persistence and the
// wiring, as matter.js's behaviors own them around FabricIcd (FabricIcd.ts:16-23).
//
// Every timer runs on the injected [clock.Clock]; every callback runs
// without a lock of this package held.
package icd
