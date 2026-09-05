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
func EstablishSubscriptionTarget(b *bridge.Bridge, subID uint32, target SubscriptionTarget) error {
	switch {
	case b == nil:
		return ErrNoBridge
	case subID == 0:
		return ErrNoSubscriptionID
	case target.Peer == nil:
		return ErrNoPeer
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
