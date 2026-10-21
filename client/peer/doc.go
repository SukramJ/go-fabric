// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package peer is the controller's view of the nodes on its fabrics: a
// [Set] of [Peer]s, each with its [Descriptor], its sessions, the
// connection process that establishes a CASE session when none is live,
// and the address monitor that follows the node's operational address
// (ADR 0020, stage 1). It is the port of matter.js
// packages/protocol/src/peer/ (PeerSet.ts, Peer.ts, PeerConnection.ts,
// PeerAddressMonitor.ts, PeerDescriptor.ts, PeerTimingParameters.ts) at
// 0ceb202d, composed of client/discovery (the node's operational
// DNS-SD service and its status), client/exchange (the sessions and
// exchanges) and client/handshake (the CASE client).
//
// Out of scope here, by ADR 0020's stage split: TCP and transport
// preference, group peers, network profiles (the LAN is the one network;
// MRP margins are 0), the BasicInformation and LIT ICD inputs of the
// session parameters (stage 4), the ICD branch of the address monitor,
// and the client interaction (stage 3) beyond the liveness probe.
package peer
