// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package exchangeseam carries the one-way seam through which a test
// outside client/exchange stamps traffic on a client session: the activity
// a message received from or sent to the peer records
// (exchange.Session.notifyActivity, matter.js Session.ts:141-147 at
// 862ad75f). The stamps decide which of a peer's sessions is the newest
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
package exchangeseam

// NotifyActivity records traffic on sess (an *exchange.Session, threaded
// through as any because this package must not import client/exchange):
// received true is a message from the peer, which also moves the stamp the
// peer's active threshold and the newest-session order are judged on.
// Set by client/exchange's init; nil until that package is linked.
var NotifyActivity func(sess any, received bool)
