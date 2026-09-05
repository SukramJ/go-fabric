// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package bridgetest provides the affordances a test outside package bridge
// needs to put a [bridge.Bridge] into a chosen state without sending
// anything over the wire.
//
// It exists for the same reason net/http/httptest does: the production API
// is shaped for the daemon that runs the bridge, and a test driving it from
// outside needs a few things the daemon never asks for. Adding those to
// bridge.Bridge would make every consumer see — and eventually depend on —
// entry points that exist only to serve tests, so they live here instead and
// reach the bridge through a module-internal seam.
//
// The scaffolding a test also needs to CONSTRUCT a bridge — the in-memory
// endpoint store and the empty-fleet snapshotter — lives one level down in
// [github.com/SukramJ/go-fabric/endpoint/endpointtest], because it refers to
// no bridge and so stays importable from the bridge's own white-box tests.
package bridgetest

import (
	"errors"
	"net"
	"time"

	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/internal/bridgeseam"
	"github.com/SukramJ/go-fabric/transport/message"
)

// Errors reported by this package.
var (
	// ErrNoBridge is returned when the bridge argument is nil.
	ErrNoBridge = errors.New("bridgetest: nil bridge")

	// ErrNoPeer is returned when a [SubscriptionTarget] carries no peer
	// address, which leaves the reports nowhere to go.
	ErrNoPeer = errors.New("bridgetest: subscription target has no peer address")

	// ErrNoSubscriptionID is returned for subscription ID 0, which the
	// bridge uses as "no subscription" and therefore never routes.
	ErrNoSubscriptionID = errors.New("bridgetest: subscription id 0")

	// ErrNoPeerCounter is returned when a [SubscriptionTarget] names a
	// secure session but leaves PeerCounter at 0. See
	// [SubscriptionTarget.PeerCounter] for why the value cannot be derived
	// and why a secure target without it is only half-established.
	ErrNoPeerCounter = errors.New("bridgetest: subscription target on a secure session has no peer counter")

	// ErrPeerCounterNotAnchored is returned when the peer's receive window
	// could not be anchored on [SubscriptionTarget.PeerCounter]: the target
	// names a session the bridge cannot resolve, an unsecured target names
	// no peer node id to key the window on, or the window had already
	// recorded that counter — none of which is the untouched
	// post-handshake state this models.
	ErrPeerCounterNotAnchored = errors.New("bridgetest: peer receive window not anchored")

	// ErrNotEstablished is returned when the bridge accepted the call but
	// no route is in place afterwards.
	ErrNotEstablished = errors.New("bridgetest: subscription target not established")
)

// SubscriptionTarget describes the peer that a subscription's ongoing
// ReportData datagrams travel to: the values a SubscribeRequest would have
// carried in its message and protocol headers.
type SubscriptionTarget struct {
	// Peer is the address reports are sent to. Required.
	Peer *net.UDPAddr

	// SessionID is the session the reports travel under. 0 means PASE
	// (unencrypted); a non-zero value must name a session the bridge can
	// look up, or the send fails when the first report is due.
	SessionID uint16

	// ExchangeID is the exchange the SubscribeRequest arrived on. Ongoing
	// ATTRIBUTE reports do not use it — the bridge opens a fresh exchange
	// for each of those — but ongoing EVENT reports stay on this one.
	ExchangeID uint16

	// PeerNodeID is the node id the peer stamped as SourceNodeID on its
	// SubscribeRequest; the bridge echoes it as DestNodeID on every report
	// (Matter §4.4.1.2). Honoured only when HasPeerNodeID is set — 0 is a
	// legitimate value, so it cannot double as "absent".
	PeerNodeID    uint64
	HasPeerNodeID bool

	// PeerCounter is the MRP message counter the SubscribeRequest itself
	// would have carried, i.e. the last counter the peer consumed before
	// the messages the test is about. Establishing the target anchors the
	// peer's inbound duplicate-detection window on it.
	//
	// It has to come from the caller: the anchor is whatever counter the
	// peer's own outbound counter had reached, and nothing on the bridge
	// side can observe or derive that from a handshake that never happened.
	//
	// It matters because a secure session's window anchors with an all-ones
	// bitmap — every counter below the first one it ever sees is a
	// duplicate. An unanchored window therefore anchors on whichever of the
	// peer's next two messages arrives first, and if they arrive out of
	// order the earlier one is dropped. Required whenever SessionID is
	// non-zero (see [ErrNoPeerCounter]).
	//
	// Optional for SessionID 0: unsecured traffic is deduplicated per
	// source node id in a window whose bitmap starts empty, so a counter
	// below the first one seen stays acceptable and nothing is lost by
	// leaving the window unanchored. Setting it there anchors that
	// per-source window anyway, and then needs HasPeerNodeID with a
	// non-zero PeerNodeID to key on.
	PeerCounter uint32

	// FabricFiltered mirrors the SubscribeRequest's FabricFiltered flag.
	// It is applied to every ongoing read, so a target that sets it
	// differently from the request being modelled will report a different
	// set of fabric-scoped rows than the real subscription would.
	FabricFiltered bool
}

// EstablishSubscriptionTarget registers target as the route for reports of
// subscription subID, as if a SubscribeRequest with those header values had
// arrived and been accepted. No datagram is sent and none is expected.
//
// It closes the gap that otherwise forces a test to issue a real
// SubscribeRequest purely to make the bridge learn where reports go: that
// setup traffic lands in whatever window the test then measures, and on a
// loaded machine it arrives inside it.
//
// The subscription itself is not created here. A subscription has two
// halves — the manager's bookkeeping and this route — and the manager half
// is already public: create the subscription with
// subscription.Manager.Subscribe, wire the manager with
// bridge.Bridge.AttachSubscriptionManager, and pass the ID it allocated.
//
// The identity the ongoing reports authorize against is resolved from
// target.SessionID through the bridge's own session lookups, exactly as an
// on-the-wire Subscribe resolves it. A session the bridge does not know
// yields the unauthenticated identity (fabric 0), so a test that means to
// exercise ACL enforcement has to establish the session first.
//
// The peer is recorded as the exchange initiator, which is what a
// SubscribeRequest always makes it.
//
// A real SubscribeRequest does a second thing that is easy to miss: its
// message counter anchors the peer's inbound duplicate-detection window. A
// target that only carried the route would leave that window unanchored,
// and the first two messages the peer sends afterwards would then have to
// arrive in order or lose one to the duplicate path. So the anchor happens
// here too, from [SubscriptionTarget.PeerCounter] — the one value of the
// modelled handshake that the caller has to supply.
func EstablishSubscriptionTarget(b *bridge.Bridge, subID uint32, target SubscriptionTarget) error {
	switch {
	case b == nil:
		return ErrNoBridge
	case subID == 0:
		return ErrNoSubscriptionID
	case target.Peer == nil:
		return ErrNoPeer
	case target.SessionID != 0 && target.PeerCounter == 0:
		return ErrNoPeerCounter
	}

	// Anchor before the route: on the wire the counter is consumed as the
	// SubscribeRequest is decrypted, before anything is captured from it —
	// and a failure here then leaves nothing half-established.
	if target.PeerCounter != 0 {
		// PeerNodeID keys the unsecured window only; it is meaningless
		// unless HasPeerNodeID marks it as carried, and the secure path
		// ignores it entirely (the session already knows its peer).
		var peerNodeID uint64
		if target.HasPeerNodeID {
			peerNodeID = target.PeerNodeID
		}
		if !bridgeseam.AnchorPeerCounter(b, target.SessionID, peerNodeID, target.PeerCounter) {
			return ErrPeerCounterNotAnchored
		}
	}

	hdr := &message.Header{
		SessionID:       target.SessionID,
		HasSourceNodeID: target.HasPeerNodeID,
		SourceNodeID:    target.PeerNodeID,
	}
	proto := message.ProtocolHeader{
		ExchangeID: target.ExchangeID,
		Initiator:  true,
	}
	if !bridgeseam.CaptureSubTarget(b, subID, target.Peer, hdr, proto, target.FabricFiltered) {
		return ErrNotEstablished
	}
	return nil
}

// AckPumpTick runs one iteration of the bridge's ACK-pump tick at time now
// and reports how many StandaloneAck datagrams it emitted.
//
// This is the whole tick: the due StandaloneAcks, the outbound-reliable
// retransmits, and the timed-deadline sweep. bridge.Bridge.RunAckPumpOnce
// covers only the first of the three, so a test that needs a retransmit to
// fire deterministically had to start the real pump goroutine and wait for
// its 50 ms ticker instead.
//
// It runs the same code the pump goroutine runs, and resolves the trackers
// per call rather than once at pump start, so a tracker attached after
// bridge.Bridge.Start is driven too.
func AckPumpTick(b *bridge.Bridge, now time.Time) (int, error) {
	if b == nil {
		return 0, ErrNoBridge
	}
	emitted, ok := bridgeseam.AckPumpTick(b, now)
	if !ok {
		return 0, ErrNoBridge
	}
	return emitted, nil
}
