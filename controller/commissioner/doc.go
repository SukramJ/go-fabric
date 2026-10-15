// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package commissioner commissions Matter nodes onto a fabric the
// controller owns (ADR 0020, stage 2): the port of matter.js
// packages/protocol/src/peer/ControllerCommissioner.ts,
// CommissioningConnection.ts and ControllerCommissioningFlow.ts at
// 0ceb202d for nodes on an IP network.
//
// [Commissioner.Commission] establishes PASE to the node's addresses (the
// connection of CommissioningConnection.ts: one address after the other,
// 10 s apart, never racing an address of a device whose responder is
// already engaged), then runs the commissioning flow over it — the
// initial reads, ArmFailSafe, the regulatory configuration for a Wi-Fi or
// Thread node, device attestation (controller/attestation), the CSR, the
// root certificate and the NOC (controller/ca), the operational CASE
// connection through controller/peer, CommissioningComplete and the
// fabric label — with matter.js's failsafe bookkeeping and error
// classes. Out of scope by ADR 0020's stage split: BLE, Wi-Fi and Thread
// network commissioning, time synchronisation, terms and conditions, the
// default OTA provider and the DCL.
package commissioner
