// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package exchange is the controller's message layer: the sessions and
// the exchanges of the initiator role over one UDP socket, with the
// Message Reliability Protocol (MRP) on every exchange. It is the port
// of matter.js packages/protocol/src/protocol/ExchangeManager.ts and
// MessageExchange.ts for the client role (ADR 0020, stage 1), and the
// layer the PASE and CASE clients of controller/handshake run on.
//
// A [Manager] owns the socket's send side, the session table and the
// exchange table, and is fed every received datagram through
// [Manager.HandleDatagram] (a transport/udp Handler). A [Session] is
// either the unsecured session of a handshake ([Manager.NewUnsecuredSession])
// or a secure PASE / CASE session ([Manager.NewSecureSession]) whose
// encryption is a secure/channel Session. An [Exchange] is one exchange
// this node initiated ([Manager.Initiate]): [Exchange.Send] transmits
// reliably and returns once the peer acknowledged, [Exchange.Next]
// waits for the peer's next message, [Exchange.Close] ends it.
//
// Scope: UDP only (no TCP, which is stage 3 material) and unicast only
// (no group sessions). Of the exchanges a peer opens only the Secure
// Channel's are served — a CloseSession ends the session, as matter.js's
// StatusReportOnlySecureChannelProtocol does; any other protocol's
// (an IM report before stage 3) is acknowledged when it asks for an
// acknowledgement and is otherwise dropped.
package exchange
