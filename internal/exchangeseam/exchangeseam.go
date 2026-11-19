// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package exchangeseam carries the one-way seams into client/exchange that
// are not public API. The first is the one through which a test
// outside client/exchange stamps traffic on a client session: the activity
// a message received from or sent to the peer records
// (exchange.Session.notifyActivity, matter.js Session.ts:142-148 at
// db021da0). The stamps decide which of a peer's sessions is the newest
// and whether the peer counts active, and they are written by the wire
// and by nothing else — a test of the peer set (client/peer) that stands
// in for traffic it did not send has no other way to reach them.
//
// Direction of the edge: package client/exchange installs the hook from
// its init; a test reads it. It mirrors
// [github.com/SukramJ/go-fabric/internal/channelseam]: exposing the stamp
// on exchange.Session would make it a production affordance every consumer
// sees, when the only honest caller is a test. The seam is
// module-internal, so it cannot become a consumer-visible surface.
//
// The second is the establishing CASE session (client/handshake), in the
// two forms the session's role selects. The initiator's: a secure session
// registered before Sigma3 goes out, which holds what the responder sends
// on it until the initiator has the responder's Success, and its
// activation then. Ahead of matter.js (BD-Matter-CASEClient-SessionBeforeSigma3).
// The responder's: a secure session registered establishing at its
// creation, which processes what the initiator sends and is seen by peer
// loss and close, and its completion when the Success went out (full
// handshake) or arrived (resumption) — matter.js NodeSession.isEstablishing
// and completeEstablishment (NodeSession.ts:205-251 at b25ed960). The
// establishing state is the handshake's affair, not a consumer's, so it is
// not a field of exchange.SecureSessionConfig.
package exchangeseam

import "context"

// NotifyActivity records traffic on sess (an *exchange.Session, threaded
// through as any because this package must not import client/exchange):
// received true is a message from the peer, which also moves the stamp the
// peer's active threshold and the newest-session order are judged on.
// Set by client/exchange's init; nil until that package is linked.
var NotifyActivity func(sess any, received bool)

// NewEstablishingSecureSession registers in m (an *exchange.Manager) the
// secure session cfg (an exchange.SecureSessionConfig) describes, before
// its handshake finished. For an initiator (cfg.Responder false) messages
// that decrypt on it are held, and exchange.Manager.SessionsFor does not
// list it, until ActivateSecureSession. For a responder (cfg.Responder
// true) its messages are processed and SessionsFor lists it; the per-peer
// cap ignores it until CompleteSecureSessionEstablishment. It returns the
// *exchange.Session. Set by client/exchange's init.
// fabric:reachable:reason="assigned by client/exchange at init and called by client/handshake; a function variable installed through a seam has no call edge the analyzer can follow"
var NewEstablishingSecureSession func(m, cfg any) (any, error)

// ActivateSecureSession ends the establishment of sess (an
// *exchange.Session from NewEstablishingSecureSession): it is listed, and
// the messages it held are delivered in the order received. Set by
// client/exchange's init.
// fabric:reachable:reason="assigned by client/exchange at init and called by client/handshake; a function variable installed through a seam has no call edge the analyzer can follow"
var ActivateSecureSession func(sess any)

// SendNotifyingTransmission is exchange.Exchange.Send on x (an
// *exchange.Exchange) with opts (an exchange.SendOptions) that also runs
// onTransmitted once, when the first transmission of the message went
// out, before the wait for its acknowledgement — matter.js
// ExchangeSendOptions.onTransmitted (MessageExchange.ts:132-137 at
// b25ed960); the CASE responder's Success completes its session's
// establishment with it. Set by client/exchange's init.
// fabric:reachable:reason="assigned by client/exchange at init and called by client/handshake; a function variable installed through a seam has no call edge the analyzer can follow"
var SendNotifyingTransmission func(ctx context.Context, x any, opcode uint8, payload []byte, opts any, onTransmitted func()) error

// CompleteSecureSessionEstablishment ends the establishment of sess (a
// CASE responder's *exchange.Session from NewEstablishingSecureSession)
// and reports whether it is established: false when the session is
// closing and so never established. Set by client/exchange's init.
// fabric:reachable:reason="assigned by client/exchange at init and called by client/handshake; a function variable installed through a seam has no call edge the analyzer can follow"
var CompleteSecureSessionEstablishment func(sess any) bool
