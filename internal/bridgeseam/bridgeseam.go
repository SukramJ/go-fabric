// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package bridgeseam carries the one-way seam through which the
// bridge/bridgetest support package reaches bridge internals.
//
// Direction of the edge: package bridge installs the hooks below from its
// init; bridgetest reads them. The seam exists because Go has no visibility
// level between "exported to every consumer" and "this package only" — and
// bridgetest is necessarily a different package from bridge. Without the
// seam, every affordance bridgetest offers would have to be a method on
// bridge.Bridge, i.e. production API that exists only to serve tests.
//
// The seam is module-internal, so it cannot itself become a consumer-visible
// surface: an external module can import bridgetest and nothing else.
package bridgeseam

import (
	"net"
	"time"

	"github.com/SukramJ/go-fabric/transport/message"
)

// The bridge is threaded through as any because this package must not
// import bridge: bridge imports this one to install the hooks, and the
// reverse edge would close an import cycle. Every hook type-asserts back to
// *bridge.Bridge and reports false when handed anything else, so a wrong
// type is a returned error at the bridgetest boundary rather than a panic
// inside it.

// CaptureSubTarget records where reports for subscription subID are to be
// sent, from the same header and protocol-header values a SubscribeRequest
// would have carried on the wire. Installed over the bridge's own capture
// routine, so the fabric and subject the ongoing reports authorize against
// are resolved exactly as an on-the-wire Subscribe resolves them.
//
// Reports true when the route is in place afterwards, false when b is not a
// bridge or the arguments do not describe a routable peer.
var CaptureSubTarget func(
	b any,
	subID uint32,
	src *net.UDPAddr,
	hdr *message.Header,
	proto message.ProtocolHeader,
	fabricFiltered bool,
) bool

// AnchorPeerCounter primes the inbound duplicate-detection window that
// datagrams arriving from the peer are tested against, on the counter the
// message that completed the handshake would have carried.
//
// Without it the window anchors on whichever of the peer's next two
// messages lands first — and a secure session's window anchors with an
// all-ones bitmap, so if they land out of order the earlier counter is
// dropped as a duplicate.
//
// Which window that is follows the session: sessionID != 0 resolves the
// secure session through the bridge's attached lookup and anchors the
// window inside it; sessionID == 0 anchors the per-source unsecured window
// keyed by peerNodeID.
//
// Reports false when b is not a bridge, no window is reachable for the
// arguments, or the window had already recorded counter.
var AnchorPeerCounter func(b any, sessionID uint16, peerNodeID uint64, counter uint32) bool

// AckPumpTick runs one iteration of the bridge's ACK-pump tick — the due
// StandaloneAcks, the outbound-reliable retransmits, and the timed-deadline
// sweep — and reports how many StandaloneAck datagrams it emitted. Unlike
// the long-running pump it resolves the trackers per call, so a caller that
// wires a tracker after Start still drives it.
//
// The second result is false when b is not a bridge.
var AckPumpTick func(b any, now time.Time) (int, bool)
