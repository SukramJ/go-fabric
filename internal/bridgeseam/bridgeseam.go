// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package bridgeseam carries the one-way seams through which packages of
// this module reach bridge internals that are not public API: the
// bridge/bridgetest support package and fabrictest (test support), and the
// node facade, which completes the establishment of the CASE responder
// sessions it installs ([SetCaseEstablishmentCompletion], a production
// hook).
//
// Direction of the edge: package bridge installs the hooks below from its
// init; bridgetest reads them, fabrictest and node one each
// ([UseListener], [SetCaseEstablishmentCompletion]). The seam exists
// because Go has no visibility level between "exported to every consumer"
// and "this package only" — and bridgetest and node are necessarily other
// packages than bridge. Without the seam, every affordance bridgetest
// offers would have to be a method on bridge.Bridge, i.e. production API
// that exists only to serve tests, and the facade's completion hook an
// exported CaseAdapter setter no other host needs.
//
// The seam is module-internal, so it cannot itself become a consumer-visible
// surface: an external module can import bridgetest and nothing else.
package bridgeseam

import (
	"net"
	"time"

	"thingfabric.dev/go-fabric/transport"
	"thingfabric.dev/go-fabric/transport/message"
	"thingfabric.dev/go-fabric/transport/udp"
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

// ServeConn returns the bridge's own receiver of the Matter messages of c
// — a connection of a transport the bridge does not listen on itself, such
// as the BTP peripheral of controller/ble/bletest — the one its BLE
// transport serves each central's session with: deliver hands one
// complete message to the transport-neutral receive path, in delivery
// order, each once its predecessor returned or released its turn, replies
// going back on c; closeConn stops delivering and evicts every session
// bound to c, as a dropped TCP connection does; wait waits for the
// delivered messages' handlers. ok is false when b is not a bridge.
var ServeConn func(b any, c transport.Conn) (deliver func(msg []byte), closeConn, wait func(), ok bool)

// UseListener makes b's Start serve the listener open returns instead of
// binding a UDP socket of its own: fabrictest's in-memory network hands a
// node's bridge a transport/udp.Listener over a socket of that network, so
// every datagram the bridge receives, answers or initiates (a CASE to a
// controller, a report, an OTA query) takes the production UDP path. open
// is called on every Start, so a stopped bridge starts again on a new
// listener. Reports false when b is not a bridge, has started, or listens
// on TCP (Config.TCP.Incoming), which the in-memory network does not carry.
var UseListener func(b any, open func() (*udp.Listener, error)) bool

// SubscriptionPersistence reads the subscription persistence's state:
// whether it is on (Bridge.SetSubscriptionPersistence) and how many
// subscriptions of the previous run wait for their re-establishment.
// Reports ok false when b is not a bridge.
var SubscriptionPersistence func(b any) (enabled bool, former int, ok bool)

// SetCaseEstablishmentCompletion installs fn on the CASE adapter a (a
// *bridge.CaseAdapter): the hook through which a host that installs its
// responder sessions establishing — node, which reads this seam beside
// bridgetest — learns when the handshake of a session the adapter's
// onEstablished installed completed (completed true: a new session's
// Success went out for the first time, a resumed one's initiator sent its
// Success) or failed before it did (completed false: the host closes the
// session). fn reports whether the session is established. matter.js
// packages/protocol/src/session/case/CaseServer.ts:204-210, :329-344 at
// b25ed960. Reports false when a is not a *bridge.CaseAdapter. Not public
// API: the adapter's exported callbacks stay the ones a host that installs
// established sessions needs.
var SetCaseEstablishmentCompletion func(a any, fn func(sessionID uint16, completed bool) bool) bool
