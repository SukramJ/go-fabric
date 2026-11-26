// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Kill tests for the C4 survivors in the gates of subscribe.go's
// subscribeWith / handleSubscribeRequest / rejectSubscribeStatus
// (phase 8 wave 2b, #602, decision 692). Every reply is captured on a
// recording connection route, so each assertion is synchronous: no
// socket read deadline, no wait.

package bridge

import (
	"context"
	"net"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/im/subscription"
	"thingfabric.dev/go-fabric/mdns"
	"thingfabric.dev/go-fabric/transport/message"
	"thingfabric.dev/go-fabric/transport/mrp"
)

// noMatchClusterW2B2b is a cluster id no server of wbEmptySnapshotter's
// topology hosts: a wildcard-endpoint path naming it matches nothing.
const noMatchClusterW2B2b uint32 = 0xFFFF0001

// subscribeOnConnW2B2b drives req through handleSubscribeRequest on the
// unsecured session from a recording connection and returns what the
// bridge sent on it, plus the handler's error.
func subscribeOnConnW2B2b(t *testing.T, b *Bridge, exchange uint16, req im.SubscribeRequest) (*fakeConn, error) {
	t.Helper()
	c := &fakeConn{}
	hdr := &message.Header{SessionID: 0, MessageCounter: uint32(exchange), HasSourceNodeID: true, SourceNodeID: 0x5A5A}
	proto := message.ProtocolHeader{ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeSubscribeRequest, ExchangeID: exchange, Initiator: true}
	err := b.handleSubscribeRequest(context.Background(), &connRoute{conn: c}, hdr, proto, req)
	return c, err
}

// onlyStatusW2B2b asserts c carried exactly one datagram, a
// StatusResponse, and returns its status.
func onlyStatusW2B2b(t *testing.T, c *fakeConn) im.StatusCode {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.sent) != 1 {
		t.Fatalf("%d datagrams sent, want exactly one StatusResponse", len(c.sent))
	}
	return decodeSubscribeStatusResponse(t, c.sent[0])
}

// noMatchPathsW2B2b returns n copies of a path that matches nothing.
func noMatchPathsW2B2b(n int) []im.ConcreteAttributePath {
	out := make([]im.ConcreteAttributePath, n)
	for i := range out {
		out[i] = im.ConcreteAttributePath{HasCluster: true, Cluster: noMatchClusterW2B2b}
	}
	return out
}

// senderlessBridgeW2B2b is a started bridge whose subscription manager
// has no report sender: every registration fails with
// subscription.ErrNoReportSender, answered Failure. A request refused by
// a gate before the registration is answered with that gate's status
// instead, so the status names the gate that answered.
func senderlessBridgeW2B2b(t *testing.T) (*Bridge, *subscription.Manager) {
	t.Helper()
	b := newStartedBridge(t)
	mgr := subscription.NewManager(subscription.Config{}, nil, nil)
	b.AttachSubscriptionManager(mgr)
	mgr.SetReportSender(nil)
	return b, mgr
}

// TestSubscribeGatesAnswerBeforeTheRegistrationW2B2b: the path-count, the
// empty-request and the illegal-path gates answer before the manager is
// asked, and a refusal of the manager is answered with its own status.
// Kills subscribe.go:2150 (> → >=), :2170, :2180 and :2236 (guard off).
func TestSubscribeGatesAnswerBeforeTheRegistrationW2B2b(t *testing.T) {
	t.Parallel()
	b, mgr := senderlessBridgeW2B2b(t)
	cases := []struct {
		name string
		req  im.SubscribeRequest
		want im.StatusCode
	}{
		// No path at all: InvalidAction from the empty gate, not the
		// manager's Failure (:2170).
		{"empty", im.SubscribeRequest{MaxIntervalCeiling: 60}, im.StatusInvalidAction},
		// Wildcard cluster with a concrete non-global attribute: the
		// illegal-path gate's InvalidAction (:2180).
		{"illegal", im.SubscribeRequest{
			AttributeRequests:  []im.ConcreteAttributePath{{HasEndpoint: true, Endpoint: 0, HasAttribute: true, Attribute: 0}},
			MaxIntervalCeiling: 60,
		}, im.StatusInvalidAction},
		// Exactly MaxReadPaths paths is within the bound: the request
		// reaches the manager, whose refusal is Failure (:2150, :2236).
		{"at the path bound", im.SubscribeRequest{AttributeRequests: noMatchPathsW2B2b(im.MaxReadPaths), MaxIntervalCeiling: 60}, im.StatusFailure},
		// One more is PathsExhausted.
		{"beyond the path bound", im.SubscribeRequest{AttributeRequests: noMatchPathsW2B2b(im.MaxReadPaths + 1), MaxIntervalCeiling: 60}, im.StatusPathsExhausted},
		// The manager's refusal is answered with its status, not by
		// carrying on to the no-match gate's InvalidAction (:2236).
		{"manager refuses", im.SubscribeRequest{AttributeRequests: noMatchPathsW2B2b(1), MaxIntervalCeiling: 60}, im.StatusFailure},
	}
	for i, tc := range cases {
		c, err := subscribeOnConnW2B2b(t, b, uint16(0x4100+i), tc.req)
		if err != nil {
			t.Fatalf("%s: handleSubscribeRequest: %v", tc.name, err)
		}
		if got := onlyStatusW2B2b(t, c); got != tc.want {
			t.Errorf("%s: answered %v, want %v", tc.name, got, tc.want)
		}
	}
	if n := mgr.Active(); n != 0 {
		t.Errorf("mgr.Active() = %d, want 0", n)
	}
}

// TestSubscribeBeyondTheChunkWaitBoundIsBusyW2B2b: a session already
// holding maxChunkWaitsPerSession chunk waits is answered Busy before any
// other gate. Kills subscribe.go:2160 (guard off).
func TestSubscribeBeyondTheChunkWaitBoundIsBusyW2B2b(t *testing.T) {
	t.Parallel()
	b, _ := senderlessBridgeW2B2b(t)
	for range maxChunkWaitsPerSession {
		release, ok := b.claimChunkWait(context.Background(), 0, true)
		if !ok {
			t.Fatal("claim below the bound refused")
		}
		t.Cleanup(release)
	}
	c, err := subscribeOnConnW2B2b(t, b, 0x4200, im.SubscribeRequest{MaxIntervalCeiling: 60})
	if err != nil {
		t.Fatalf("handleSubscribeRequest: %v", err)
	}
	if got := onlyStatusW2B2b(t, c); got != im.StatusBusy {
		t.Fatalf("answered %v, want Busy", got)
	}
}

// TestSubscribeWithoutDispatcherSendsNothingW2B2b: a bridge whose topology
// is not assembled drops a Subscribe without an answer (the controller
// retries over MRP). Kills subscribe.go:2125 (guard off), which would
// answer the empty request InvalidAction.
func TestSubscribeWithoutDispatcherSendsNothingW2B2b(t *testing.T) {
	t.Parallel()
	b, err := New(wbEmptySnapshotter, mdns.NewNoop(), Config{Listen: "127.0.0.1:0"}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if b.Dispatcher() != nil {
		t.Fatal("a bridge that never started has a dispatcher; the test needs none")
	}
	c, err := subscribeOnConnW2B2b(t, b, 0x4300, im.SubscribeRequest{MaxIntervalCeiling: 60})
	if err != nil {
		t.Fatalf("handleSubscribeRequest: %v", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.sent) != 0 {
		t.Fatalf("%d datagrams sent without a dispatcher, want none", len(c.sent))
	}
}

// TestRejectSubscribeStatusDischargesTheOwedAckW2B2b: a refusal that went
// out discharges the acknowledgement owed for the request's exchange —
// the peer-initiated one — and one that could not be sent returns the
// error. Kills subscribe.go:2338 (guard off (err), != → ==) and :2342
// (drop !).
func TestRejectSubscribeStatusDischargesTheOwedAckW2B2b(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	b.mu.Lock()
	b.ackTracker = mrp.NewAckTracker(time.Hour) // obligations only; no pump
	tracker := b.ackTracker
	b.mu.Unlock()
	const exchange = 0x4400
	hdr := &message.Header{SessionID: 0, MessageCounter: 7, HasSourceNodeID: true, SourceNodeID: 0x5A5A}
	proto := message.ProtocolHeader{ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeSubscribeRequest, ExchangeID: exchange, Initiator: true}

	tracker.Owe(7, 0, exchange, false, time.Now())
	if err := b.rejectSubscribeStatus(&connRoute{conn: &fakeConn{}}, hdr, proto, "test", im.StatusInvalidAction); err != nil {
		t.Fatalf("rejectSubscribeStatus: %v", err)
	}
	if tracker.Owes(0, exchange, false) {
		t.Error("the acknowledgement owed for the request is still owed after its refusal went out")
	}

	// No route: the refusal is not sent, the error is returned and the
	// acknowledgement stays owed.
	tracker.Owe(8, 0, exchange+1, false, time.Now())
	proto.ExchangeID = exchange + 1
	if err := b.rejectSubscribeStatus(&connRoute{}, hdr, proto, "test", im.StatusInvalidAction); err == nil {
		t.Error("a refusal with no route to the peer returned no error")
	}
	if !tracker.Owes(0, exchange+1, false) {
		t.Error("an unsent refusal discharged the owed acknowledgement")
	}
}

// TestAbandonPrimingSubscriptionWithoutManagerW2B2b: abandoning a priming
// subscription after the manager was detached does nothing. Kills
// subscribe.go:2112 (&& → ||), which calls Release on the nil manager.
func TestAbandonPrimingSubscriptionWithoutManagerW2B2b(t *testing.T) {
	t.Parallel()
	b, err := New(wbEmptySnapshotter, mdns.NewNoop(), Config{Listen: "127.0.0.1:0"}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	b.abandonPrimingSubscription(7)
}

// TestCurrentPeerAddrSkipsANilRouteW2B2b: a session whose recorded route
// names no peer falls back as one with none recorded. Kills
// subscribe.go:2082 (&& → ||).
func TestCurrentPeerAddrSkipsANilRouteW2B2b(t *testing.T) {
	t.Parallel()
	b, err := New(wbEmptySnapshotter, mdns.NewNoop(), Config{Listen: "127.0.0.1:0"}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	fallback := &net.UDPAddr{IP: net.IPv6loopback, Port: 5540}
	b.sessionPeerAddrs.Store(uint16(9), net.Addr(&connRoute{}))
	if got := b.currentPeerAddr(9, fallback); got != net.Addr(fallback) {
		t.Fatalf("currentPeerAddr = %v, want the fallback for a route without a connection", got)
	}
}

// TestSubscribeAtQuotaMatchingNothingEvictsNobodyW2B2b: at its fabric's
// quota a Subscribe whose paths match nothing is refused InvalidAction
// before the eviction that would make room, so the fabric's oldest
// subscription survives. Kills subscribe.go:2230 (guard off).
func TestSubscribeAtQuotaMatchingNothingEvictsNobodyW2B2b(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	mgr := subscription.NewManager(subscription.Config{MaxSubscriptionsPerFabric: 1}, nil, nil)
	b.AttachSubscriptionManager(mgr)
	oldest, err := mgr.Subscribe(subscription.SubscribeArgs{
		FabricIndex: 0, PeerNodeID: 0x5A5A, SessionID: 0, MaxIntervalCeiling: 60,
		AttributePaths: noMatchPathsW2B2b(1),
	})
	if err != nil {
		t.Fatalf("filling the quota: %v", err)
	}
	c, err := subscribeOnConnW2B2b(t, b, 0x4500, im.SubscribeRequest{AttributeRequests: noMatchPathsW2B2b(1), MaxIntervalCeiling: 60, KeepSubscriptions: true})
	if err != nil {
		t.Fatalf("handleSubscribeRequest: %v", err)
	}
	if got := onlyStatusW2B2b(t, c); got != im.StatusInvalidAction {
		t.Errorf("answered %v, want InvalidAction", got)
	}
	if _, err := mgr.Get(oldest.ID); err != nil {
		t.Fatalf("the fabric's oldest subscription was evicted by a request matching nothing: %v", err)
	}
}

// failingSecondSendConnW2B2b answers the priming report's one chunk with
// the peer's StatusResponse(Success) and fails every later send — the
// SubscribeResponse.
type failingSecondSendConnW2B2b struct {
	fakeConn
	b        *Bridge
	exchange uint16
}

func (c *failingSecondSendConnW2B2b) Send(msg []byte) error {
	c.mu.Lock()
	n := len(c.sent)
	c.mu.Unlock()
	if n > 0 {
		return errNoRoute
	}
	_ = c.fakeConn.Send(msg)
	c.b.signalStatusResponseRX(paseTestSessionID, c.exchange, false, im.StatusSuccess)
	return nil
}

// TestSubscribeResponseNotSentAbandonsTheSubscriptionW2B2b: a subscription
// whose SubscribeResponse could not be sent is abandoned, not left active.
// Kills subscribe.go:2300 (guard off (err)).
func TestSubscribeResponseNotSentAbandonsTheSubscriptionW2B2b(t *testing.T) {
	t.Parallel()
	b := newACLTestBridge(t, &aclStoreFake{})
	mgr := subscription.NewManager(subscription.Config{}, nil, nil)
	b.AttachSubscriptionManager(mgr)
	attachPASETestPeer(t, b)
	const exchange = 0x4600
	c := &failingSecondSendConnW2B2b{b: b, exchange: exchange}
	hdr := &message.Header{SessionID: paseTestSessionID, MessageCounter: 103}
	proto := message.ProtocolHeader{ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeSubscribeRequest, ExchangeID: exchange, Initiator: true}
	req := im.SubscribeRequest{AttributeRequests: []im.ConcreteAttributePath{accessControlACLPath()}, MaxIntervalCeiling: 60}
	if err := b.handleSubscribeRequest(context.Background(), &connRoute{conn: c}, hdr, proto, req); err == nil {
		t.Fatal("handleSubscribeRequest returned no error for an unsent SubscribeResponse")
	}
	c.mu.Lock()
	sent := len(c.sent)
	c.mu.Unlock()
	if sent != 1 {
		t.Fatalf("%d priming chunks went out, want 1 (the test needs a one-chunk priming report)", sent)
	}
	if n := mgr.Active(); n != 0 {
		t.Fatalf("mgr.Active() = %d after the SubscribeResponse failed, want 0", n)
	}
}
