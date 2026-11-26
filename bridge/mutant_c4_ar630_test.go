// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Kill tests for the C4 survivors of the repeat at 160079617 outside
// #602's rows (#630): mutants of bridge/ that neither the bridge tests nor
// ./node/ killed there. Each test names the row it kills.

package bridge

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/clock"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/im/subscription"
	"thingfabric.dev/go-fabric/mdns"
	"thingfabric.dev/go-fabric/secure/sigma"
	"thingfabric.dev/go-fabric/tlv"
	"thingfabric.dev/go-fabric/transport/message"
	"thingfabric.dev/go-fabric/transport/mrp"
)

// unencodableAR630 is a host value without EncodeTLV, of its own Go type.
type unencodableAR630 struct{ A int }

// TestUnencodableValueNamesHowItIsServedAR630: the once-per-type warning
// of an unencodable attribute value says how it was served — a non-empty
// []any as an empty list, any other type as null. Kills C4 reply.go:1263
// (== → !=, the served_as attribute), #630.
func TestUnencodableValueNamesHowItIsServedAR630(t *testing.T) {
	// Not parallel: it swaps the process logger. The once is per process,
	// so both types start unlogged.
	anyList := fmt.Sprintf("%T", []any{})
	own := fmt.Sprintf("%T", unencodableAR630{})
	unencodableTypes.Delete(anyList)
	unencodableTypes.Delete(own)
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	for _, v := range []any{[]any{uint8(1)}, unencodableAR630{A: 1}} {
		enc := tlv.NewEncoder()
		defaultAttributeValueWriter(enc, tlv.AnonymousTag(), im.AttributeValue{Value: v})
		if _, err := enc.Bytes(); err != nil {
			t.Fatal(err)
		}
	}
	got := map[string]string{}
	for line := range bytes.Lines(buf.Bytes()) {
		var rec struct {
			Msg      string `json:"msg"`
			GoType   string `json:"go_type"`
			ServedAs string `json:"served_as"`
		}
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		if rec.Msg == "matter.attribute.unencodable_value" {
			got[rec.GoType] = rec.ServedAs
		}
	}
	if got[anyList] != "empty list" || got[own] != "null" {
		t.Errorf("served_as by Go type = %v, want %s: empty list, %s: null", got, anyList, own)
	}
}

// TestSubscriptionTargetOfANilRouteIsNoneAR630: a captured target whose
// route is nil — no address, a typed-nil UDP address, a connection route
// without its connection — is no target. Kills C4 subscribe.go:419
// (|| → &&), #630. Defensive: no caller stores such a target today.
func TestSubscriptionTargetOfANilRouteIsNoneAR630(t *testing.T) {
	t.Parallel()
	b := unstartedBridgeW2B2b(t)
	for i, src := range []net.Addr{nil, (*net.UDPAddr)(nil), &connRoute{}} {
		sub := &subscription.Subscription{ID: uint32(100 + i)}
		b.routing.subTargets.Store(sub.ID, subTarget{src: src, sessionID: 7})
		if got, ok := b.subscriptionTargetOf(sub); ok {
			t.Errorf("route %#v: target %+v, want none", src, got)
		}
	}
	live := &subscription.Subscription{ID: 200}
	b.routing.subTargets.Store(live.ID, subTarget{src: &net.UDPAddr{IP: net.IPv6loopback, Port: 5540}, sessionID: 7})
	if _, ok := b.subscriptionTargetOf(live); !ok {
		t.Error("a target with a route is none")
	}
}

// capturingNotifierAR630 is a root cluster server that notifies, keeping
// the callback it was given, and reports no attribute.
type capturingNotifierAR630 struct {
	plainServerW2B2b
	mu   *sync.Mutex
	fire *func()
}

func (s capturingNotifierAR630) OnMatterValueChanged(fn func()) func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	*s.fire = fn
	return func() {}
}

// TestRootNotifierWithoutReportableAttributesAdvancesTheVersionAR630: a
// root server's change with no reportable attribute to mark still
// advances its cluster's DataVersion — the write-hold check applies only
// to paths there are. Kills C4 subscribe.go:2679 (> → >=), #630.
func TestRootNotifierWithoutReportableAttributesAdvancesTheVersionAR630(t *testing.T) {
	t.Parallel()
	const cluster = 0xFFF5
	var (
		mu   sync.Mutex
		fire func()
	)
	root := &endpoint.Endpoint{ID: 0}
	root.PublishClusterServers([]contract.ClusterServer{capturingNotifierAR630{plainServerW2B2b{id: cluster}, &mu, &fire}})
	b := unstartedBridgeW2B2b(t)
	b.wireRootNotifiers(&listenerSet{}, subscription.NewManager(subscription.Config{}, nil, nil), &endpoint.Topology{Endpoints: []*endpoint.Endpoint{root}})
	mu.Lock()
	fn := fire
	mu.Unlock()
	if fn == nil {
		t.Fatal("the root's notifier was not wired: the test checks nothing")
	}
	v0 := root.ClusterDataVersion(cluster)
	fn()
	if v := root.ClusterDataVersion(cluster); v == v0 {
		t.Errorf("DataVersion stayed %d after the change, want it advanced", v)
	}
}

// TestReleaseHandshakeOnANilConnRouteAR630: ending a handshake's hold on
// a typed-nil connection route does nothing. Kills C4 conn.go:382
// (|| → &&), #630. Defensive: no caller passes a typed-nil route today.
func TestReleaseHandshakeOnANilConnRouteAR630(t *testing.T) {
	t.Parallel()
	b := unstartedBridgeW2B2b(t)
	b.releaseHandshake((*connRoute)(nil), HandshakeKey{InitiatorNodeID: 1, ExchangeID: 2})
}

// TestReleaseCASEHandshakeEverywhereSkipsANilConnRouteAR630: a typed-nil
// connection route among the connections is skipped, and the hold on the
// others still ends. Kills C4 conn.go:399 (|| → &&), #630. Defensive: no
// caller stores a typed-nil route today.
func TestReleaseCASEHandshakeEverywhereSkipsANilConnRouteAR630(t *testing.T) {
	t.Parallel()
	b := unstartedBridgeW2B2b(t)
	key := HandshakeKey{InitiatorNodeID: 1, ExchangeID: 2}
	held := &connRoute{handshakes: map[uint16]handshakeHold{2: {initiator: 1}}}
	b.connRoutes.Store("nil", (*connRoute)(nil))
	b.connRoutes.Store("held", held)
	b.releaseCASEHandshakeEverywhere(key)
	b.routeBindMu.Lock()
	defer b.routeBindMu.Unlock()
	if _, ok := held.handshakes[2]; ok {
		t.Error("the hold of the handshake was kept")
	}
}

// readPayloadAR630 is the next message from the bridge to r's peer that is
// not a standalone acknowledgement: its headers and payload.
func readPayloadAR630(t *testing.T, r *unsecuredRig, what string) (message.Header, message.ProtocolHeader, []byte) {
	t.Helper()
	buf := make([]byte, 1500)
	for {
		_ = r.peer.SetReadDeadline(time.Now().Add(3 * time.Second))
		n, from, err := r.peer.ReadFromUDP(buf)
		if err != nil {
			t.Fatalf("%s: nothing reached the peer: %v", what, err)
		}
		if !unsecuredFromBridge(r.b, from, buf[:n]) {
			continue
		}
		hdr, hl, err := message.UnmarshalHeader(buf[:n])
		if err != nil {
			t.Fatalf("%s: header: %v", what, err)
		}
		proto, pl, err := message.UnmarshalProtocolHeader(buf[hl:n])
		if err != nil {
			t.Fatalf("%s: protocol header: %v", what, err)
		}
		if proto.Opcode != mrp.StandaloneAckOpcode {
			return hdr, proto, append([]byte(nil), buf[hl+pl:n]...)
		}
	}
}

// TestCASEInitiatorRefusesAFailureReportAfterSigma3AR630: a handshake this
// node opens that gets a failure StatusReport in answer to its Sigma3
// ends with ErrCaseRejected and establishes nothing. Kills C4
// case_initiator.go:410 (guard off (err)), #630.
func TestCASEInitiatorRefusesAFailureReportAfterSigma3AR630(t *testing.T) {
	t.Parallel()
	r := newUnsecuredRig(t)
	root, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rootPub := elliptic.Marshal(elliptic.P256(), root.X, root.Y) //nolint:staticcheck // the tests' encoding of the root key
	ipk := newCaseTestIPK(t)
	peerID := newCaseTestIdentity(t, 0xAAAA, 1, ipk)
	responder := sigma.NewResponder(peerID, caseTestVerifier{}, 0x2001)
	responder.SetIdentityResolver(oneFabricResolver{ours: peerID, rootPub: rootPub})
	adapter := NewCaseAdapter(responder)
	initiator, err := sigma.NewPeerInitiator(sigma.InitiatorConfig{
		Identity: newCaseTestIdentity(t, 0xBBBB, 1, ipk), Verifier: caseTestVerifier{},
		SessionID: 0x1003, PeerNodeID: 0xAAAA, RootPublicKey: rootPub,
	})
	if err != nil {
		t.Fatalf("NewPeerInitiator: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var established atomic.Bool
	peerAddr, _ := r.peer.LocalAddr().(*net.UDPAddr)
	done := make(chan error, 1)
	go func() {
		_, err := r.b.runCASEInitiator(ctx, peerAddr, &CaseInitiation{
			Initiator:     initiator,
			OnEstablished: func(context.Context, sigma.InitiatorResult) error { established.Store(true); return nil },
		})
		done <- err
	}()
	s1Hdr, s1Proto, s1 := readPayloadAR630(t, r, "the Sigma1 this node sends")
	if s1Proto.Opcode != mrp.SCOpcodeSigma1 {
		t.Fatalf("the peer got opcode %#x, want Sigma1", s1Proto.Opcode)
	}
	op, s2, err := adapter.ProcessSigma1(s1)
	if err != nil || op != mrp.SCOpcodeSigma2 {
		t.Fatalf("the responder answered opcode %#x, err %v; want a Sigma2", op, err)
	}
	sigma2 := message.ProtocolHeader{
		Opcode: mrp.SCOpcodeSigma2, ExchangeID: s1Proto.ExchangeID, ProtocolID: mrp.SecureChannelProtocolID,
		NeedsAck: true, HasAck: true, AckCounter: s1Hdr.MessageCounter,
	}
	if err := r.b.dispatch(ctx, responderDatagram(77, s1Hdr.SourceNodeID, sigma2, s2), r.peer.LocalAddr()); err != nil {
		t.Fatalf("dispatch(Sigma2): %v", err)
	}
	s3Hdr, s3Proto, _ := readPayloadAR630(t, r, "the Sigma3 this node sends")
	if s3Proto.Opcode != mrp.SCOpcodeSigma3 {
		t.Fatalf("the peer got opcode %#x, want Sigma3", s3Proto.Opcode)
	}
	failure := mrp.EncodeStatusReport(mrp.SCStatusGeneralFailure, uint32(mrp.SecureChannelProtocolID), mrp.SCStatusProtocolInvalidParameter, nil)
	report := message.ProtocolHeader{
		Opcode: mrp.SCOpcodeStatusReport, ExchangeID: s1Proto.ExchangeID, ProtocolID: mrp.SecureChannelProtocolID,
		NeedsAck: true, HasAck: true, AckCounter: s3Hdr.MessageCounter,
	}
	if err := r.b.dispatch(ctx, responderDatagram(78, s1Hdr.SourceNodeID, report, failure), r.peer.LocalAddr()); err != nil {
		t.Fatalf("dispatch(StatusReport): %v", err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrCaseRejected) {
			t.Errorf("the handshake ended with %v, want ErrCaseRejected", err)
		}
	case <-ctx.Done():
		t.Fatal("the handshake did not end")
	}
	if established.Load() {
		t.Error("a handshake the peer refused was registered as established")
	}
}

// countingClockAR630 is the system clock, counting the timers scheduled.
type countingClockAR630 struct {
	clock.System
	afterFuncs atomic.Int32
}

func (c *countingClockAR630) AfterFunc(d time.Duration, f func()) clock.Timer {
	c.afterFuncs.Add(1)
	return c.System.AfterFunc(d, f)
}

// TestRecheckPASECloseWithoutADeferredCloseSchedulesNothingAR630: the
// recheck of a session with no deferred close does nothing, even while the
// session has a timed window open — no recheck timer is scheduled for it.
// Kills C4 pase_deferred_close.go:206 (guard off), #630.
func TestRecheckPASECloseWithoutADeferredCloseSchedulesNothingAR630(t *testing.T) {
	t.Parallel()
	clk := &countingClockAR630{}
	b, err := New(wbEmptySnapshotter, mdns.NewNoop(), Config{Listen: "127.0.0.1:0", Clock: clk}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	const sessionID = 5
	b.routing.timedDeadlines.Store(timedKey{sessionID: sessionID, exchangeID: 1}, time.Now().Add(time.Minute))
	if active, until := b.paseSessionActive(sessionID); !active || until.IsZero() {
		t.Fatal("the session has no open timed window: the test checks nothing")
	}
	before := clk.afterFuncs.Load()
	b.recheckPASEClose(sessionID)
	if n := clk.afterFuncs.Load() - before; n != 0 {
		t.Errorf("the recheck scheduled %d timers, want none", n)
	}
}

// TestEstablishFormerSubscriptionNeedsADispatcherAR630: re-establishing a
// former subscription on a bridge with a subscription manager and no
// dispatcher is refused before anything is restored. Kills C4
// subscription_reestablish.go:245 (|| → &&), #630.
func TestEstablishFormerSubscriptionNeedsADispatcherAR630(t *testing.T) {
	t.Parallel()
	b := unstartedBridgeW2B2b(t)
	mgr := subscription.NewManager(subscription.Config{}, nil, nil)
	b.AttachSubscriptionManager(mgr)
	rec := subscription.PeerSubscription{
		SubscriptionID: 9, FabricIndex: 1, PeerNodeID: 0xAAAA, MaxIntervalCeiling: 60, MaxInterval: 60,
		AttributeRequests: []im.ConcreteAttributePath{{Endpoint: 0, Cluster: 0x28, Attribute: 0, HasEndpoint: true, HasCluster: true, HasAttribute: true}},
	}
	err := b.establishFormerSubscription(t.Context(), rec, peerLink{sessionID: 7, addr: &net.UDPAddr{IP: net.IPv6loopback, Port: 5540}})
	if err == nil || err.Error() != "bridge: no subscription manager or dispatcher" {
		t.Errorf("establishFormerSubscription = %v, want the refusal for a missing dispatcher", err)
	}
	if n := mgr.Active(); n != 0 {
		t.Errorf("%d subscriptions active, want none", n)
	}
}

// TestCaseAdapterWithoutStatedParamsHasNoneAR630: a CASE adapter whose
// responder has no session parameters from a Sigma1 — none processed yet
// — reports none, not zero values. Kills C4 handlers.go:1011 (guard off),
// #630; the PASE twin is TestPaseAdapter_AbsentMRPParamsNil.
func TestCaseAdapterWithoutStatedParamsHasNoneAR630(t *testing.T) {
	t.Parallel()
	a, _, _ := pairedCaseAdapter(t)
	if p := a.PeerSessionParams(); p != nil {
		t.Errorf("PeerSessionParams = %+v before any Sigma1, want nil", p)
	}
}

// TestTrackingTheSameMessageAgainIsNoRefusalAR630: a message of a secure
// exchange tracked again under its own counter takes a fresh place — it is
// not the earlier message of the exchange that still waits for its
// acknowledgement. Kills C4 outbound_reliable.go:689 (&& → ||), #630.
func TestTrackingTheSameMessageAgainIsNoRefusalAR630(t *testing.T) {
	t.Parallel()
	tr := newOutboundReliableTracker(nil, clock.System{})
	dest := &net.UDPAddr{IP: net.IPv6loopback, Port: 5540}
	now := time.Now()
	if err := tr.TrackOnExchange(1, 7, 0x10, false, []byte{1}, dest, now); err != nil {
		t.Fatalf("first track: %v", err)
	}
	if err := tr.TrackOnExchange(1, 7, 0x10, false, []byte{1}, dest, now); err != nil {
		t.Errorf("tracking the same message again: %v, want it taken", err)
	}
	if n := tr.Pending(); n != 1 {
		t.Errorf("%d entries pending, want 1", n)
	}
}
