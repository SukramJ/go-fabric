// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package exchange is the controller's message layer: the sessions and
// the exchanges of the initiator role over one UDP socket, with the
// Message Reliability Protocol (MRP) on every exchange. It is the port
// of matter.js packages/protocol/src/protocol/ExchangeManager.ts and
// MessageExchange.ts for the client role (ADR 0020, stage 1), and the
// layer the PASE and CASE clients of client/handshake run on.
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
// Scope: UDP only and unicast only (no group sessions). An exchange a
// peer opens reaches a [ProtocolHandler] installed with
// [Manager.SetProtocolHandler] (ADR 0020, stage 3): on a secure session
// any protocol's but the Secure Channel's, whose CloseSession handling is
// built in as matter.js's StatusReportOnlySecureChannelProtocol; on an
// unsecured session the peer opens, a Sigma1 for the CASE responder, a
// PBKDFParamRequest being refused. A message no handler takes is
// acknowledged when it asks for an acknowledgement and otherwise dropped.
package exchange
