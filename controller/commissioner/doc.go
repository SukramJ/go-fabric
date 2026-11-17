// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package commissioner commissions Matter nodes onto a fabric the
// controller owns (ADR 0020, stage 2): the port of matter.js
// packages/protocol/src/peer/ControllerCommissioner.ts,
// CommissioningConnection.ts and ControllerCommissioningFlow.ts at
// 0ceb202d, for nodes on an IP network and, over [Options].Channel, over
// BLE (ADR 0024).
//
// [Commissioner.Commission] establishes PASE to the node's addresses (the
// connection of CommissioningConnection.ts: one address after the other,
// 10 s apart, an engaged responder holding the next address for at most
// those 10 s, a wrong passcode ending one address and not the device —
// the last two ahead of matter.js, review II R2-P6-fsm-1806) or over the
// channel, then runs the commissioning flow
// over it — the initial reads, ArmFailSafe, the regulatory configuration
// for a Wi-Fi or Thread node, time synchronisation, the terms and
// conditions acknowledgements, device attestation (controller/attestation),
// the CSR, the root certificate and the NOC (controller/ca), the Wi-Fi or
// Thread network steps, the operational CASE connection through
// client/peer, CommissioningComplete and the fabric label — with
// matter.js's failsafe bookkeeping and error classes. Not ported: the
// default OTA provider (review II, R2-P6-doc-1857). The ledger's trust anchors and
// revocation reach the flow through Options.Attestation from
// controller/dcl (ADR 0020 stage 6a), which this package does not import.
package commissioner
