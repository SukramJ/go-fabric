// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/diagevent"
	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

const (
	// completedCaseExchange and completedCaseSessionID are the exchange
	// the handshake of [completeCASE] runs on and the session id its
	// responder announces.
	completedCaseExchange  = uint16(0x0202)
	completedCaseSessionID = uint16(0x2001)
	// completedCaseInitiator is the initiator's ephemeral node id.
	completedCaseInitiator = uint64(0xC0DE)
)

// completedCase is a CASE handshake a controller completed with the bridge
// over UDP, real Sigma1/2/3 through the receive pipeline.
type completedCase struct {
	r          *unsecuredRig
	provider   *PerExchangeCaseProvider // nil for the singleton handler
	controller *pipelinePeer
	sigma1     []byte
	sigma3     []byte
	ring       *diagevent.Ring
	sessions   *atomic.Int32
}

// completeCASE runs one CASE handshake to completion, through a
// PerExchangeCaseProvider on clk when viaProvider, else through one
// CaseAdapter attached as the singleton handler.
func completeCASE(t *testing.T, clk clock.Clock, viaProvider bool) *completedCase {
	t.Helper()
	r := newUnsecuredRig(t)
	ring := diagevent.NewRing(64)
	r.b.AttachDiagnosticEvents(ring)
	root, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rootPub := elliptic.Marshal(elliptic.P256(), root.X, root.Y) //nolint:staticcheck // the uncompressed point the CASE destination id hashes
	ipk := newCaseTestIPK(t)
	ours := newCaseTestIdentity(t, 0xBBBB, 1, ipk)
	sessions := new(atomic.Int32)
	newAdapter := func() *CaseAdapter {
		responder := sigma.NewResponder(ours, caseTestVerifier{}, completedCaseSessionID)
		responder.SetIdentityResolver(oneFabricResolver{ours: ours, rootPub: rootPub})
		a := NewCaseAdapter(responder)
		a.SetOnSessionEstablished(func(sigma.SessionKeys, uint16) error { sessions.Add(1); return nil })
		return a
	}
	c := &completedCase{r: r, ring: ring, sessions: sessions}
	if viaProvider {
		c.provider = NewPerExchangeCaseProvider(newAdapter)
		c.provider.SetClock(clk)
		c.provider.SetOnEvict(r.b.ForgetSigma1Replied)
		r.b.AttachCaseHandlerProvider(c.provider.Resolve)
	} else {
		r.b.AttachCaseHandler(newAdapter())
	}

	c.controller = newPipelinePeer(t, r.b, completedCaseInitiator)
	initiator, err := sigma.NewPeerInitiator(sigma.InitiatorConfig{
		Identity: newCaseTestIdentity(t, 0xAAAA, 1, ipk), Verifier: caseTestVerifier{},
		SessionID: 0x1002, PeerNodeID: 0xBBBB, RootPublicKey: rootPub,
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.sigma1, err = initiator.GenerateSigma1(); err != nil {
		t.Fatal(err)
	}
	if err := c.controller.send(mrp.SCOpcodeSigma1, completedCaseExchange, c.sigma1, nil); err != nil {
		t.Fatal(err)
	}
	sigma2, counter, _ := c.controller.recv(mrp.SCOpcodeSigma2, "Sigma2")
	if c.sigma3, err = initiator.ProcessSigma2Bytes(sigma2); err != nil {
		t.Fatal(err)
	}
	if err := c.controller.send(mrp.SCOpcodeSigma3, completedCaseExchange, c.sigma3, &counter); err != nil {
		t.Fatal(err)
	}
	c.expectReport(t, c.controller, mrp.SCStatusGeneralSuccess, "the report on Sigma3")
	if got := c.route(t); got != c.controller.conn.LocalAddr().String() {
		t.Fatalf("the established session's route is %s, want the controller's %s", got, c.controller.conn.LocalAddr())
	}
	return c
}

// route is the established session's UDP route.
func (c *completedCase) route(t *testing.T) string {
	t.Helper()
	v, ok := c.r.b.sessionPeerAddrs.Load(completedCaseSessionID)
	if !ok {
		t.Fatal("the established session has no route")
	}
	return v.(net.Addr).String()
}

// completedEvents counts the "completed a secure-session handshake"
// diagnostic events.
func (c *completedCase) completedEvents() int {
	n := 0
	for _, e := range c.ring.Snapshot() {
		if e.Kind == diagevent.KindSession && e.Message == "A controller completed a secure-session handshake with the bridge." {
			n++
		}
	}
	return n
}

// otherPeer is a second sender on the link that uses the handshake's
// initiator node id, its counter ahead of the controller's.
func (c *completedCase) otherPeer(t *testing.T) *pipelinePeer {
	t.Helper()
	p := newPipelinePeer(t, c.r.b, completedCaseInitiator)
	p.counter = c.controller.counter + 10_000
	c.controller.counter = p.counter + 10_000
	return p
}

// expectReport reads the StatusReport p receives and checks its general
// status.
func (c *completedCase) expectReport(t *testing.T, p *pipelinePeer, want uint16, what string) {
	t.Helper()
	report, _, _ := p.recv(mrp.SCOpcodeStatusReport, what)
	general, _, protocol, ok := decodeStatusReport(report)
	if !ok || general != want {
		t.Fatalf("%s: general status %d (protocol %d), want %d", what, general, protocol, want)
	}
}

// TestSigma3AfterCompletionChangesNothingAboutTheSession: once a CASE
// handshake completed, a Sigma3 under its initiator node id and exchange —
// any payload, the accepted one included, from another address — leaves
// the session that handshake established as it was: its route, the
// diagnostic that records its establishment, its installation. That holds
// through a PerExchangeCaseProvider, through the singleton handler, and
// when the handshake's record lost its place and the same Sigma1 began it
// again on the finished adapter (review II, R2-P3-rr-dev-01).
func TestSigma3AfterCompletionChangesNothingAboutTheSession(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		viaProvider bool
		rebegin     bool
	}{
		{"through a provider", true, false},
		{"through the singleton handler", false, false},
		{"the record begun again by the same Sigma1", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := completeCASE(t, clocktest.New(time.Unix(1_700_000_000, 0)), tc.viaProvider)
			controllerRoute := c.route(t)
			if tc.rebegin {
				// The record loses its place at the bound, as the oldest,
				// to handshakes begun since.
				for i := range maxCaseHandshakes {
					begun := HandshakeKey{InitiatorNodeID: uint64(0x7000 + i), ExchangeID: 1}
					c.r.b.markSigma1Replied(begun, [32]byte{1})
					c.r.b.boundSigma1Records(begun)
				}
				c.r.b.mu.RLock()
				_, held := c.r.b.sigma1Replied[HandshakeKey{InitiatorNodeID: completedCaseInitiator, ExchangeID: completedCaseExchange}]
				c.r.b.mu.RUnlock()
				if held {
					t.Fatal("the completed handshake's record kept its place at the bound")
				}
				other := c.otherPeer(t)
				if err := other.send(mrp.SCOpcodeSigma1, completedCaseExchange, c.sigma1, nil); err != nil {
					t.Fatal(err)
				}
				other.recv(mrp.SCOpcodeSigma2, "the cached Sigma2")
			}
			for _, payload := range [][]byte{{0x15, 0x18}, c.sigma3} {
				other := c.otherPeer(t)
				if err := other.send(mrp.SCOpcodeSigma3, completedCaseExchange, payload, nil); err != nil {
					t.Fatal(err)
				}
				other.recv(mrp.SCOpcodeStatusReport, "the report to the other sender")
				if got := c.route(t); got != controllerRoute {
					t.Fatalf("a Sigma3 of %d bytes after completion moved the session's route from %s to %s", len(payload), controllerRoute, got)
				}
			}
			if got := c.completedEvents(); got != 1 {
				t.Fatalf("the completed-handshake diagnostic was recorded %d times, want once", got)
			}
			if got := c.sessions.Load(); got != 1 {
				t.Fatalf("the handshake installed %d sessions, want 1", got)
			}
		})
	}
}

// TestSigma3RetransmissionIsAnsweredOnlyForItsBytesAndRoute: after a CASE
// handshake completed, the success report is sent again only for the
// Sigma3 that completed it — the same bytes — arriving over the route it
// completed on; a Sigma3 of other bytes, or the same bytes from another
// address, is answered as a Sigma3 of a handshake that never began, and
// takes nothing from the retransmission that follows (review II,
// R2-P3-rr-dev-01).
func TestSigma3RetransmissionIsAnsweredOnlyForItsBytesAndRoute(t *testing.T) {
	t.Parallel()
	c := completeCASE(t, clocktest.New(time.Unix(1_700_000_000, 0)), true)

	if err := c.controller.send(mrp.SCOpcodeSigma3, completedCaseExchange, append([]byte(nil), c.sigma3[:len(c.sigma3)-1]...), nil); err != nil {
		t.Fatal(err)
	}
	c.expectReport(t, c.controller, mrp.SCStatusGeneralFailure, "other bytes over the completing route")

	other := c.otherPeer(t)
	if err := other.send(mrp.SCOpcodeSigma3, completedCaseExchange, c.sigma3, nil); err != nil {
		t.Fatal(err)
	}
	c.expectReport(t, other, mrp.SCStatusGeneralFailure, "the same bytes from another address")

	if err := c.controller.send(mrp.SCOpcodeSigma3, completedCaseExchange, c.sigma3, nil); err != nil {
		t.Fatal(err)
	}
	c.expectReport(t, c.controller, mrp.SCStatusGeneralSuccess, "the retransmission over the completing route")
}

// TestSigma3RetransmissionKeepsNoRecordAlive: the Sigma3 retransmissions of
// a completed handshake do not keep its adapter or its record past the
// provider's TTL: the reaper takes the adapter at the TTL after the
// handshake's own last step, and its record goes with it (review II,
// R2-P3-rr-dev-01).
func TestSigma3RetransmissionKeepsNoRecordAlive(t *testing.T) {
	t.Parallel()
	clk := clocktest.New(time.Unix(1_700_000_000, 0))
	c := completeCASE(t, clk, true)
	const ttl = 30 * time.Second
	clk.Advance(ttl - time.Second)
	if err := c.controller.send(mrp.SCOpcodeSigma3, completedCaseExchange, c.sigma3, nil); err != nil {
		t.Fatal(err)
	}
	c.expectReport(t, c.controller, mrp.SCStatusGeneralSuccess, "the retransmission before the TTL")
	clk.Advance(2 * time.Second)
	c.provider.reapLocked(ttl)
	if got := c.provider.Active(); got != 0 {
		t.Fatalf("the provider holds %d adapters one TTL after the handshake, want 0", got)
	}
	c.r.b.mu.RLock()
	_, held := c.r.b.sigma1Replied[HandshakeKey{InitiatorNodeID: completedCaseInitiator, ExchangeID: completedCaseExchange}]
	c.r.b.mu.RUnlock()
	if held {
		t.Fatal("the completed handshake's record outlived its adapter")
	}
}
