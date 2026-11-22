// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/clock"
	"thingfabric.dev/go-fabric/clock/clocktest"
	"thingfabric.dev/go-fabric/diagevent"
	"thingfabric.dev/go-fabric/secure/sigma"
	"thingfabric.dev/go-fabric/transport/message"
	"thingfabric.dev/go-fabric/transport/mrp"
)

const (
	// completedCaseExchange and completedCaseSessionID are the exchange
	// the handshake of [completeCASE] runs on and the session id its
	// responder announces.
	completedCaseExchange  = uint16(0x0202)
	completedCaseSessionID = uint16(0x2001)
	// completedCaseFabricIndex is the fabric the responder's identity is
	// on.
	completedCaseFabricIndex = uint8(3)
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
	sigma2     []byte
	sigma3     []byte
	// sigma2Counter is the message counter of the Sigma2.
	sigma2Counter uint32
	// report is the message counter of the success report that answered
	// the completing Sigma3.
	report   uint32
	ring     *diagevent.Ring
	sessions *atomic.Int32
}

// completeCASE runs one CASE handshake to completion, through a
// PerExchangeCaseProvider on clk when viaProvider, else through one
// CaseAdapter attached as the singleton handler.
func completeCASE(t *testing.T, clk clock.Clock, viaProvider bool) *completedCase {
	t.Helper()
	c := beginCASE(t, newUnsecuredRig(t), clk, viaProvider)
	if err := c.controller.send(mrp.SCOpcodeSigma3, completedCaseExchange, c.sigma3, &c.sigma2Counter); err != nil {
		t.Fatal(err)
	}
	c.report = c.expectReport(t, c.controller, mrp.SCStatusGeneralSuccess, "the report on Sigma3")
	if got := c.route(t); got != c.controller.conn.LocalAddr().String() {
		t.Fatalf("the established session's route is %s, want the controller's %s", got, c.controller.conn.LocalAddr())
	}
	return c
}

// beginCASE runs [completeCASE]'s handshake on r up to the controller's
// receipt of the Sigma2: the handshake is in progress, its Sigma3 not sent.
func beginCASE(t *testing.T, r *unsecuredRig, clk clock.Clock, viaProvider bool) *completedCase {
	t.Helper()
	ring := diagevent.NewRing(64)
	r.b.AttachDiagnosticEvents(ring)
	root, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rootPub := elliptic.Marshal(elliptic.P256(), root.X, root.Y) //nolint:staticcheck // the uncompressed point the CASE destination id hashes
	ipk := newCaseTestIPK(t)
	ours := newCaseTestIdentity(t, 0xBBBB, 1, ipk)
	ours.FabricIndex = completedCaseFabricIndex
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
	c.sigma2, c.sigma2Counter, _ = c.controller.recv(mrp.SCOpcodeSigma2, "Sigma2")
	if c.sigma3, err = initiator.ProcessSigma2Bytes(c.sigma2); err != nil {
		t.Fatal(err)
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

// expectReport reads the StatusReport p receives, checks its general
// status and returns its message counter.
func (c *completedCase) expectReport(t *testing.T, p *pipelinePeer, want uint16, what string) uint32 {
	t.Helper()
	report, counter, _ := p.recv(mrp.SCOpcodeStatusReport, what)
	general, _, protocol, ok := decodeStatusReport(report)
	if !ok || general != want {
		t.Fatalf("%s: general status %d (protocol %d), want %d", what, general, protocol, want)
	}
	return counter
}

// expectAcknowledgedOnly fails t unless the Sigma3 just sent on the
// handshake's exchange was acknowledged and no more: its acknowledgement is
// still owed — no reply carried it — and the replies waiting for the peer's
// acknowledgement are the ones that waited before it, none sent and none
// taken away.
func (c *completedCase) expectAcknowledgedOnly(t *testing.T, pendingBefore int, what string) {
	t.Helper()
	c.expectNoReply(t, pendingBefore, what)
	if got := pendingOnUnsecuredExchange(c.r.b, completedCaseExchange); got != pendingBefore {
		t.Fatalf("%s: %d replies wait for the peer's acknowledgement, %d before — one was taken away", what, got, pendingBefore)
	}
}

// expectNoReply fails t unless the Sigma3 just sent on the handshake's
// exchange went unanswered: its acknowledgement is still owed and no reply
// was added to those waiting for the peer's acknowledgement before it.
func (c *completedCase) expectNoReply(t *testing.T, pendingBefore int, what string) {
	t.Helper()
	if !c.r.b.ackTracker.Owes(0, completedCaseExchange, false) {
		t.Fatalf("%s: no acknowledgement owed — a reply carried it", what)
	}
	if got := pendingOnUnsecuredExchange(c.r.b, completedCaseExchange); got > pendingBefore {
		t.Fatalf("%s: %d replies wait for the peer's acknowledgement, %d before — a reply was sent", what, got, pendingBefore)
	}
}

// expectRefused fails t unless err is want — the receive path's refusal of
// a message that does not acknowledge the reply its exchange waits on — and
// the message was neither acknowledged nor answered: no acknowledgement is
// owed for it and the replies waiting for the peer's acknowledgement are
// the ones that waited before it. matter.js throws for such a message
// before it registers its acknowledgement or delivers it
// (packages/protocol/src/protocol/MessageExchange.ts:521-534, :553-562 at
// db021da0); review II, phase 7, decisions 276, 277.
func (c *completedCase) expectRefused(t *testing.T, err, want error, pendingBefore int, what string) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("%s: dispatch %v, want %v", what, err, want)
	}
	if c.r.b.ackTracker.Owes(0, completedCaseExchange, false) {
		t.Fatalf("%s: an acknowledgement is owed for the refused message", what)
	}
	if got := pendingOnUnsecuredExchange(c.r.b, completedCaseExchange); got != pendingBefore {
		t.Fatalf("%s: %d replies wait for the peer's acknowledgement, %d before", what, got, pendingBefore)
	}
}

// acknowledgeReport sends the controller's standalone acknowledgement of
// the success report.
func (c *completedCase) acknowledgeReport(t *testing.T) {
	t.Helper()
	if err := c.controller.send(mrp.StandaloneAckOpcode, completedCaseExchange, nil, &c.report); err != nil {
		t.Fatal(err)
	}
	if c.successReportPending() {
		t.Fatal("the success report still waits for its acknowledgement after it")
	}
}

// successReportPending reports whether the success report that completed
// the handshake still waits for the controller's acknowledgement.
func (c *completedCase) successReportPending() bool {
	tr := c.r.b.outboundReliable
	tr.mu.Lock()
	defer tr.mu.Unlock()
	for key, e := range tr.pending {
		if key.sessionID == 0 && e.exchangeID == completedCaseExchange && e.dest != nil && e.dest.String() == c.controller.conn.LocalAddr().String() {
			return true
		}
	}
	return false
}

// TestSigma3AfterCompletionChangesNothingAboutTheSession: once a CASE
// handshake completed, a Sigma3 under its initiator node id and exchange —
// any payload, the accepted one included, from another address — leaves
// the session that handshake established as it was: its route, the
// diagnostic that records its establishment, its installation. While the
// success report waits for its acknowledgement such a Sigma3 does not
// acknowledge it and is refused on receive: not acknowledged, not answered
// (review II, phase 7, decision 277). That holds through a
// PerExchangeCaseProvider and through the singleton handler (review II,
// R2-P3-rr-dev-01, R2-P4-nw-01); the same Sigma1 again once the record is
// gone is TestSigma1AgainAfterCompletionBeginsAfresh's.
func TestSigma3AfterCompletionChangesNothingAboutTheSession(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		viaProvider bool
	}{
		{"through a provider", true},
		{"through the singleton handler", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := completeCASE(t, clocktest.New(time.Unix(1_700_000_000, 0)), tc.viaProvider)
			controllerRoute := c.route(t)
			if !c.successReportPending() {
				t.Fatal("the success report is not waiting for the controller's acknowledgement")
			}
			for _, payload := range [][]byte{{0x15, 0x18}, c.sigma3} {
				other := c.otherPeer(t)
				pending := pendingOnUnsecuredExchange(c.r.b, completedCaseExchange)
				err := other.send(mrp.SCOpcodeSigma3, completedCaseExchange, payload, nil)
				c.expectRefused(t, err, errPreviousAckMissing, pending, "a Sigma3 from another sender")
				if !c.successReportPending() {
					t.Fatalf("a Sigma3 of %d bytes from another sender took the success report from the retransmission", len(payload))
				}
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

// TestSigma3OfACompletedHandshakeIsAcknowledgedOnly: once a CASE
// handshake completed and its success report was acknowledged, a Sigma3
// under its initiator and exchange — other bytes, the completing bytes from
// another address, and the initiator's retransmission of the completing
// Sigma3 over its route — is acknowledged and not answered while the
// provider still holds the handshake, and installs no second session; it
// was answered with the success report again until review II, phase 7,
// decision 284 (R2-P3-dr-dev-02, R2-P3-rr-dev-01, R2-P4-nv-03). While the
// report waits for its acknowledgement the retransmission, which
// acknowledges the Sigma2 and not the report, is refused on receive, and
// the report's own retransmission serves the initiator (decision 277).
// Neither reference answers it: in matter.js it is a duplicate on the
// handshake's live exchange, an error that is only logged, or an opening
// message SecureChannel does not take (packages/protocol/src/protocol/
// MessageExchange.ts:501-511 and :522-534, ExchangeManager.ts:435-437 and
// :491-494, packages/protocol/src/securechannel/
// SecureChannelProtocol.ts:152-153 at db021da0); connectedhomeip takes only
// Sigma1 unsolicited (src/protocols/secure_channel/CASEServer.cpp:54-55 at
// 6170af84) and acknowledges the rest (src/messaging/ExchangeMgr.cpp:437).
func TestSigma3OfACompletedHandshakeIsAcknowledgedOnly(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		viaProvider bool
	}{
		{"through a provider, its record standing", true},
		{"through the singleton handler, its record ended", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := completeCASE(t, clocktest.New(time.Unix(1_700_000_000, 0)), tc.viaProvider)

			err := c.controller.send(mrp.SCOpcodeSigma3, completedCaseExchange, c.sigma3, &c.sigma2Counter)
			c.expectRefused(t, err, errIncorrectAck, 1, "the retransmission while the success report waits for its acknowledgement")
			c.acknowledgeReport(t)
			if got := c.recordHeld(); got != tc.viaProvider {
				t.Fatalf("after the report's acknowledgement the record is held: %v, want %v", got, tc.viaProvider)
			}

			pending := pendingOnUnsecuredExchange(c.r.b, completedCaseExchange)
			if err := c.controller.send(mrp.SCOpcodeSigma3, completedCaseExchange, append([]byte(nil), c.sigma3[:len(c.sigma3)-1]...), nil); err != nil {
				t.Fatal(err)
			}
			c.expectAcknowledgedOnly(t, pending, "other bytes over the completing route")

			other := c.otherPeer(t)
			if err := other.send(mrp.SCOpcodeSigma3, completedCaseExchange, c.sigma3, nil); err != nil {
				t.Fatal(err)
			}
			c.expectAcknowledgedOnly(t, pending, "the same bytes from another address")

			if err := c.controller.send(mrp.SCOpcodeSigma3, completedCaseExchange, c.sigma3, &c.sigma2Counter); err != nil {
				t.Fatal(err)
			}
			c.expectAcknowledgedOnly(t, pending, "the retransmission over the completing route")
			if got := c.recordHeld(); got != tc.viaProvider {
				t.Fatalf("after the Sigma3s the record is held: %v, want %v", got, tc.viaProvider)
			}
			if got := c.sessions.Load(); got != 1 {
				t.Fatalf("the handshake installed %d sessions, want 1", got)
			}
		})
	}
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
	// While the report waits for its acknowledgement a retransmission is
	// refused on receive (decision 277); after, it is acknowledged only
	// (decision 284).
	c.acknowledgeReport(t)
	pending := pendingOnUnsecuredExchange(c.r.b, completedCaseExchange)
	if err := c.controller.send(mrp.SCOpcodeSigma3, completedCaseExchange, c.sigma3, &c.sigma2Counter); err != nil {
		t.Fatal(err)
	}
	c.expectAcknowledgedOnly(t, pending, "the retransmission before the TTL")
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

// pendingDestsOnUnsecuredExchange lists where the unsecured-session
// messages on exchangeID that wait for their acknowledgement go.
func pendingDestsOnUnsecuredExchange(b *Bridge, exchangeID uint16) []string {
	tr := b.outboundReliable
	tr.mu.Lock()
	defer tr.mu.Unlock()
	var dests []string
	for key, e := range tr.pending {
		if key.sessionID == 0 && e.exchangeID == exchangeID {
			dests = append(dests, e.dest.String())
		}
	}
	return dests
}

// TestSigma3AfterCompletionKeepsTheSuccessReportPending: once a CASE
// handshake completed, the success report that waits for the initiator's
// acknowledgement stays tracked to the initiator through any Sigma3 that is
// not that acknowledgement — other bytes, or the completing bytes from
// another address — and through any Sigma2_Resume on the handshake's key,
// which is acknowledged and not answered as such a Sigma3 is, through a
// PerExchangeCaseProvider and through the singleton handler alike (review
// II, R2-P4-nw-01, R2-P4-nw-02, R2-P4-rr-sec-03). Each such message —
// the initiator's own retransmission of the completing Sigma3 included,
// which acknowledges the Sigma2 — is refused on receive, not acknowledged
// (review II, phase 7, decision 277). In matter.js a new
// message on the exchange while the report is unacknowledged throws before
// the retransmission timer is touched; only the matching acknowledgement
// stops it (packages/protocol/src/protocol/MessageExchange.ts:521-538 at
// db021da0); a Sigma2_Resume opening an exchange is not taken
// (packages/protocol/src/securechannel/SecureChannelProtocol.ts:146-153).
func TestSigma3AfterCompletionKeepsTheSuccessReportPending(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		viaProvider bool
		opcode      uint8
	}{
		{"a Sigma3 through a provider", true, mrp.SCOpcodeSigma3},
		{"a Sigma3 through the singleton handler", false, mrp.SCOpcodeSigma3},
		{"a Sigma2_Resume through a provider", true, mrp.SCOpcodeSigma2Resume},
		{"a Sigma2_Resume through the singleton handler", false, mrp.SCOpcodeSigma2Resume},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := completeCASE(t, clocktest.New(time.Unix(1_700_000_000, 0)), tc.viaProvider)
			want := c.controller.conn.LocalAddr().String()
			before := pendingDestsOnUnsecuredExchange(c.r.b, completedCaseExchange)
			if len(before) != 1 || before[0] != want {
				t.Fatalf("after completion the replies waiting for an acknowledgement go to %v, want the report to %s", before, want)
			}
			for _, payload := range [][]byte{{0x15, 0x18}, c.sigma3} {
				other := c.otherPeer(t)
				err := other.send(tc.opcode, completedCaseExchange, payload, nil)
				c.expectRefused(t, err, errPreviousAckMissing, 1, "a message from another sender")
				if got := pendingDestsOnUnsecuredExchange(c.r.b, completedCaseExchange); len(got) != 1 || got[0] != want {
					t.Fatalf("opcode %#x of %d bytes from another sender left the replies waiting for an acknowledgement going to %v, want the report to %s", tc.opcode, len(payload), got, want)
				}
			}
			if ok := c.r.b.handshakeCompleted(HandshakeKey{InitiatorNodeID: completedCaseInitiator, ExchangeID: completedCaseExchange}); !ok {
				t.Fatal("the completed handshake's record did not outlive the messages from another sender")
			}
			err := c.controller.send(mrp.SCOpcodeSigma3, completedCaseExchange, c.sigma3, &c.sigma2Counter)
			c.expectRefused(t, err, errIncorrectAck, 1, "the retransmission of the completing Sigma3 over its route")
			if got := pendingDestsOnUnsecuredExchange(c.r.b, completedCaseExchange); len(got) != 1 || got[0] != want {
				t.Fatalf("the refused retransmission left the replies waiting for an acknowledgement going to %v, want the report to %s", got, want)
			}
		})
	}
}

// TestSingletonCompletedRecordEndsWithItsHandler: the record of a handshake
// the singleton handler completed ends when AttachCaseHandler replaces that
// handler (review II, R2-P4-nw-01). The retransmission of its Sigma3 over
// its route, before and after, does not acknowledge the success report,
// which is still retransmitted, and is refused on receive (review II, phase
// 7, decision 277); the record would answer it only once the report was
// acknowledged, and the singleton's record ends with that acknowledgement
// (TestSingletonCompletedRecordEndsWithItsSuccessReport).
func TestSingletonCompletedRecordEndsWithItsHandler(t *testing.T) {
	t.Parallel()
	c := completeCASE(t, clocktest.New(time.Unix(1_700_000_000, 0)), false)
	err := c.controller.send(mrp.SCOpcodeSigma3, completedCaseExchange, c.sigma3, &c.sigma2Counter)
	c.expectRefused(t, err, errIncorrectAck, 1, "the retransmission over the completing route")
	if ok := c.r.b.handshakeCompleted(HandshakeKey{InitiatorNodeID: completedCaseInitiator, ExchangeID: completedCaseExchange}); !ok {
		t.Fatal("the completed handshake's record did not outlive the refused retransmission")
	}

	c.r.b.AttachCaseHandler(nil)
	if ok := c.r.b.handshakeCompleted(HandshakeKey{InitiatorNodeID: completedCaseInitiator, ExchangeID: completedCaseExchange}); ok {
		t.Fatal("the completed handshake's record outlived the handler that completed it")
	}
	pending := pendingOnUnsecuredExchange(c.r.b, completedCaseExchange)
	err = c.controller.send(mrp.SCOpcodeSigma3, completedCaseExchange, c.sigma3, &c.sigma2Counter)
	c.expectRefused(t, err, errIncorrectAck, pending, "the retransmission after the handler was replaced")
}

// TestSigma1ThatBeginsNothingKeepsACompletedHandshake: once a CASE handshake
// completed, a Sigma1 under its initiator node id and exchange that begins
// no handshake — other bytes, from another address — and does not
// acknowledge the success report is refused on receive, not acknowledged
// and not answered (review II, phase 7, decision 277), and leaves the
// completed handshake as it was: its record stands, and the success report
// waiting for the initiator's acknowledgement stays in retransmission;
// once that is acknowledged, the provider's record still stands, and the
// retransmission of the completing Sigma3 over its route is acknowledged
// and not answered (decision 284). Through a PerExchangeCaseProvider and through the singleton
// handler alike (review II, R2-P4-rr-sec-04; l1 item 3). In matter.js a message on the
// handshake's live exchange that does not acknowledge the pending report
// fails before the retransmission timer is touched
// (packages/protocol/src/protocol/MessageExchange.ts:521-534 at db021da0),
// and nothing is sent for it: the exchange manager catches the error and
// logs it (ExchangeManager.ts:435-438, :538-549).
func TestSigma1ThatBeginsNothingKeepsACompletedHandshake(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		viaProvider bool
	}{
		{"through a provider", true},
		{"through the singleton handler", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := completeCASE(t, clocktest.New(time.Unix(1_700_000_000, 0)), tc.viaProvider)
			want := c.controller.conn.LocalAddr().String()
			other := c.otherPeer(t)
			err := other.send(mrp.SCOpcodeSigma1, completedCaseExchange, []byte{0x15, 0x18}, nil)
			c.expectRefused(t, err, errPreviousAckMissing, 1, "a Sigma1 that began nothing")
			if ok := c.r.b.handshakeCompleted(HandshakeKey{InitiatorNodeID: completedCaseInitiator, ExchangeID: completedCaseExchange}); !ok {
				t.Fatal("a Sigma1 that began nothing ended the completed handshake's record")
			}
			if !c.successReportPending() {
				t.Fatalf("a Sigma1 that began nothing took the success report from the retransmission; the replies waiting go to %v, want the report to %s",
					pendingDestsOnUnsecuredExchange(c.r.b, completedCaseExchange), want)
			}
			if tc.viaProvider {
				c.acknowledgeReport(t)
				pending := pendingOnUnsecuredExchange(c.r.b, completedCaseExchange)
				if err := c.controller.send(mrp.SCOpcodeSigma3, completedCaseExchange, c.sigma3, &c.sigma2Counter); err != nil {
					t.Fatal(err)
				}
				c.expectAcknowledgedOnly(t, pending, "the retransmission of the completing Sigma3 over its route")
				if !c.recordHeld() {
					t.Fatal("the retransmission of the completing Sigma3 ended the provider's record")
				}
			}
			if got := c.sessions.Load(); got != 1 {
				t.Fatalf("the handshake installed %d sessions, want 1", got)
			}
		})
	}
}

// recordHeld reports whether the router holds a Sigma1 record of the
// completed handshake's key.
func (c *completedCase) recordHeld() bool {
	c.r.b.mu.RLock()
	defer c.r.b.mu.RUnlock()
	_, held := c.r.b.sigma1Replied[HandshakeKey{InitiatorNodeID: completedCaseInitiator, ExchangeID: completedCaseExchange}]
	return held
}

// TestSingletonCompletedRecordEndsWithItsSuccessReport: the record of a CASE
// handshake the singleton handler completed, and the unsecured duplicate
// window it holds, end when the success report is acknowledged or given up
// — the retransmission the record serves is over then — and a Sigma3 of the
// handshake that arrives after is acknowledged and not answered. Through a
// PerExchangeCaseProvider the record ends with the adapter, at the
// provider's TTL, as before (review II, R2-P3-rr-dev-01, R2-P4-rr-sec-02).
// matter.js's CASE server waits for the success report's acknowledgement
// (packages/protocol/src/session/case/CaseServer.ts:332;
// packages/protocol/src/protocol/MessageExchange.ts:830, given up at
// :973-976 and thrown at :840 at db021da0) and then removes the handshake's
// unsecured session, reception state included
// (CaseServer.ts:95-98, packages/protocol/src/session/UnsecuredSession.ts:94-102).
func TestSingletonCompletedRecordEndsWithItsSuccessReport(t *testing.T) {
	t.Parallel()
	t.Run("acknowledged, through the singleton handler", func(t *testing.T) {
		t.Parallel()
		c := completeCASE(t, clocktest.New(time.Unix(1_700_000_000, 0)), false)
		if err := c.controller.send(mrp.StandaloneAckOpcode, completedCaseExchange, nil, &c.report); err != nil {
			t.Fatal(err)
		}
		if c.recordHeld() {
			t.Fatal("the completed handshake's record outlived the acknowledgement of its success report")
		}
		if got := c.r.b.unsecuredWindows.len(); got != 0 {
			t.Fatalf("%d unsecured windows held after the success report was acknowledged, want 0", got)
		}
		if err := c.controller.send(mrp.SCOpcodeSigma3, completedCaseExchange, c.sigma3, nil); err != nil {
			t.Fatal(err)
		}
		c.expectNoReply(t, 0, "the completing Sigma3 after its report was acknowledged")
	})
	t.Run("given up, through the singleton handler", func(t *testing.T) {
		t.Parallel()
		c := completeCASE(t, clocktest.New(time.Unix(1_700_000_000, 0)), false)
		c.r.b.outboundReliable.Tick(c.r.b.clk().Now().Add(unsecuredEntryLifetime), func(net.Addr, []byte) error { return nil })
		if c.successReportPending() {
			t.Fatal("the success report is still in retransmission past its lifetime")
		}
		if w := c.r.b.acquireUnsecuredWindow(0xFEED, false); w == nil {
			t.Fatal("a new unsecured source got no window")
		}
		if c.recordHeld() {
			t.Fatal("the completed handshake's record outlived its success report")
		}
		if got := c.r.b.unsecuredWindows.len(); got != 1 {
			t.Fatalf("%d unsecured windows held, want only the new source's", got)
		}
	})
	t.Run("acknowledged, through a provider", func(t *testing.T) {
		t.Parallel()
		clk := clocktest.New(time.Unix(1_700_000_000, 0))
		c := completeCASE(t, clk, true)
		if err := c.controller.send(mrp.StandaloneAckOpcode, completedCaseExchange, nil, &c.report); err != nil {
			t.Fatal(err)
		}
		if !c.recordHeld() {
			t.Fatal("the record of a handshake a provider completed ended before its adapter")
		}
		const ttl = 30 * time.Second
		clk.Advance(ttl + time.Second)
		c.provider.reapLocked(ttl)
		if c.recordHeld() {
			t.Fatal("the completed handshake's record outlived its adapter")
		}
		c.r.b.sweepUnsecuredWindows()
		if got := c.r.b.unsecuredWindows.len(); got != 0 {
			t.Fatalf("%d unsecured windows held after the adapter went, want 0", got)
		}
	})
}

// TestSingletonCompletedHandshakesHoldNoWindows: more CASE handshakes than
// [maxUnsecuredWindows], each completed through one singleton CaseAdapter
// and its success report acknowledged, leave no record and no unsecured
// window behind, so a new unsecured source still gets its duplicate window
// (review II, R2-P4-rr-sec-02).
func TestSingletonCompletedHandshakesHoldNoWindows(t *testing.T) {
	t.Parallel()
	r := newUnsecuredRig(t)
	root, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rootPub := elliptic.Marshal(elliptic.P256(), root.X, root.Y) //nolint:staticcheck // the uncompressed point the CASE destination id hashes
	ipk := newCaseTestIPK(t)
	ours := newCaseTestIdentity(t, 0xBBBB, 1, ipk)
	responder := sigma.NewResponder(ours, caseTestVerifier{}, completedCaseSessionID)
	responder.SetIdentityResolver(oneFabricResolver{ours: ours, rootPub: rootPub})
	a := NewCaseAdapter(responder)
	established := 0
	a.SetOnSessionEstablished(func(sigma.SessionKeys, uint16) error { established++; return nil })
	r.b.AttachCaseHandler(a)
	peerID := newCaseTestIdentity(t, 0xAAAA, 1, ipk)
	n := maxUnsecuredWindows + 4
	for i := range n {
		p := newPipelinePeer(t, r.b, uint64(0x9000+i))
		ini, err := sigma.NewPeerInitiator(sigma.InitiatorConfig{
			Identity: peerID, Verifier: caseTestVerifier{},
			SessionID: uint16(0x1002 + i), PeerNodeID: 0xBBBB, RootPublicKey: rootPub,
		})
		if err != nil {
			t.Fatal(err)
		}
		s1, err := ini.GenerateSigma1()
		if err != nil {
			t.Fatal(err)
		}
		if err := p.send(mrp.SCOpcodeSigma1, 0x0300, s1, nil); err != nil {
			t.Fatalf("handshake %d, Sigma1: %v", i, err)
		}
		s2, c2, _ := p.recv(mrp.SCOpcodeSigma2, "Sigma2")
		s3, err := ini.ProcessSigma2Bytes(s2)
		if err != nil {
			t.Fatalf("handshake %d: %v", i, err)
		}
		if err := p.send(mrp.SCOpcodeSigma3, 0x0300, s3, &c2); err != nil {
			t.Fatal(err)
		}
		report, c3, _ := p.recv(mrp.SCOpcodeStatusReport, "the report on Sigma3")
		if g, _, _, ok := decodeStatusReport(report); !ok || g != mrp.SCStatusGeneralSuccess {
			t.Fatalf("handshake %d: report general status %d", i, g)
		}
		if err := p.send(mrp.StandaloneAckOpcode, 0x0300, nil, &c3); err != nil {
			t.Fatal(err)
		}
	}
	if established != n {
		t.Fatalf("%d handshakes established a session, want %d", established, n)
	}
	r.b.mu.RLock()
	records := len(r.b.sigma1Replied)
	r.b.mu.RUnlock()
	if records != 0 || r.b.unsecuredWindows.len() != 0 {
		t.Fatalf("%d completed handshakes with acknowledged reports left %d records and %d unsecured windows, want none", n, records, r.b.unsecuredWindows.len())
	}
	if w := r.b.acquireUnsecuredWindow(0xFEED, false); w == nil {
		t.Fatal("a new unsecured source got no window")
	}
}

// TestAttachCaseHandlerEndsTheReplacedHandlersHandshakes: a CASE handshake
// the singleton handler began and did not complete ends when
// AttachCaseHandler replaces that handler: its Sigma2 is no longer
// retransmitted, and its Sigma3 is acknowledged and not answered, as one no
// Sigma1 began, instead of reaching a handler that never saw its Sigma1
// (review II, R2-P4-rr-sec-02).
func TestAttachCaseHandlerEndsTheReplacedHandlersHandshakes(t *testing.T) {
	t.Parallel()
	r := newUnsecuredRig(t)
	root, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rootPub := elliptic.Marshal(elliptic.P256(), root.X, root.Y) //nolint:staticcheck // the uncompressed point the CASE destination id hashes
	ipk := newCaseTestIPK(t)
	ours := newCaseTestIdentity(t, 0xBBBB, 1, ipk)
	responder := sigma.NewResponder(ours, caseTestVerifier{}, completedCaseSessionID)
	responder.SetIdentityResolver(oneFabricResolver{ours: ours, rootPub: rootPub})
	r.b.AttachCaseHandler(NewCaseAdapter(responder))
	p := newPipelinePeer(t, r.b, completedCaseInitiator)
	ini, err := sigma.NewPeerInitiator(sigma.InitiatorConfig{
		Identity: newCaseTestIdentity(t, 0xAAAA, 1, ipk), Verifier: caseTestVerifier{},
		SessionID: 0x1002, PeerNodeID: 0xBBBB, RootPublicKey: rootPub,
	})
	if err != nil {
		t.Fatal(err)
	}
	s1, err := ini.GenerateSigma1()
	if err != nil {
		t.Fatal(err)
	}
	if err := p.send(mrp.SCOpcodeSigma1, completedCaseExchange, s1, nil); err != nil {
		t.Fatal(err)
	}
	s2, c2, _ := p.recv(mrp.SCOpcodeSigma2, "Sigma2")
	s3, err := ini.ProcessSigma2Bytes(s2)
	if err != nil {
		t.Fatal(err)
	}

	replacement := &recordingCaseHandler{}
	r.b.AttachCaseHandler(replacement)
	if got := pendingOnUnsecuredExchange(r.b, completedCaseExchange); got != 0 {
		t.Fatalf("%d replies of the replaced handler's handshake wait for an acknowledgement, want 0", got)
	}
	if err := p.send(mrp.SCOpcodeSigma3, completedCaseExchange, s3, &c2); err != nil {
		t.Fatal(err)
	}
	if got := replacement.sigma3Calls.Load(); got != 0 {
		t.Fatalf("the replacing handler was handed %d Sigma3s of a handshake it never began", got)
	}
	if !r.b.ackTracker.Owes(0, completedCaseExchange, false) {
		t.Fatal("the Sigma3 was answered: no acknowledgement owed")
	}
}

// handshakeState is what the bridge holds of the unsecured session's
// handshakes: its Sigma1 records, its duplicate windows and the replies
// that wait for an acknowledgement.
func handshakeState(b *Bridge) (records, windows, pending int) {
	b.mu.RLock()
	records = len(b.sigma1Replied)
	b.mu.RUnlock()
	return records, b.unsecuredWindows.len(), b.outboundReliable.Pending()
}

// expectNoHandshakeState fails t unless the bridge holds no Sigma1 record,
// no unsecured window and no reply waiting for an acknowledgement.
func expectNoHandshakeState(t *testing.T, b *Bridge, when string) {
	t.Helper()
	if records, windows, pending := handshakeState(b); records != 0 || windows != 0 || pending != 0 {
		t.Fatalf("%s: %d Sigma1 records, %d unsecured windows and %d replies waiting for an acknowledgement held, want none", when, records, windows, pending)
	}
}

// TestSigma1AgainAfterCompletionBeginsAfresh: once a CASE handshake
// completed and its record is gone — the success report was acknowledged
// (singleton handler), or the record lost its place at the bound while the
// provider still holds the finished adapter — the same Sigma1 again is
// taken as any other Sigma1: it is answered with a fresh Sigma2, not the
// finished handshake's, and the finished handshake's Sigma3 fails against
// it with a failure report, which ends the handshake. The established
// session stays as it was, and once the failure report is acknowledged the
// bridge holds no record, window, reply or (over TCP) connection hold of
// the handshake (review II, R2-P4-fr-sec-01). matter.js never sends the
// first Sigma2 again: on the handshake's live exchange the message is a
// duplicate or a flow error
// (packages/protocol/src/protocol/MessageExchange.ts:501-511, :521-534 at
// db021da0); once the exchange is gone (CaseServer.ts:95-98) the CASE
// server begins afresh with a new random and key pair (CaseServer.ts:61-63,
// :102-146, :236, :241).
func TestSigma1AgainAfterCompletionBeginsAfresh(t *testing.T) {
	t.Parallel()
	key := HandshakeKey{InitiatorNodeID: completedCaseInitiator, ExchangeID: completedCaseExchange}
	for _, tc := range []struct {
		name        string
		viaProvider bool
	}{
		{"through a provider", true},
		{"through the singleton handler", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := completeCASE(t, clocktest.New(time.Unix(1_700_000_000, 0)), tc.viaProvider)
			controllerRoute := c.route(t)
			if err := c.controller.send(mrp.StandaloneAckOpcode, completedCaseExchange, nil, &c.report); err != nil {
				t.Fatal(err)
			}
			if tc.viaProvider {
				// The record loses its place at the bound
				// ([Bridge.boundSigma1Records]); the adapter stays.
				c.r.b.forgetSigma1Replied(key)
			}
			if c.recordHeld() {
				t.Fatal("the completed handshake's record is still held")
			}
			if err := c.controller.send(mrp.SCOpcodeSigma1, completedCaseExchange, c.sigma1, nil); err != nil {
				t.Fatal(err)
			}
			sigma2, counter, _ := c.controller.recv(mrp.SCOpcodeSigma2, "the same Sigma1 again")
			if bytes.Equal(sigma2, c.sigma2) {
				t.Fatal("the same Sigma1 again was answered with the finished handshake's Sigma2, want a fresh one")
			}
			if err := c.controller.send(mrp.SCOpcodeSigma3, completedCaseExchange, c.sigma3, &counter); err != nil {
				t.Fatal(err)
			}
			failure := c.expectReport(t, c.controller, mrp.SCStatusGeneralFailure, "the finished handshake's Sigma3 after the fresh Sigma2")
			if err := c.controller.send(mrp.StandaloneAckOpcode, completedCaseExchange, nil, &failure); err != nil {
				t.Fatal(err)
			}
			if got := c.route(t); got != controllerRoute {
				t.Fatalf("the established session's route moved from %s to %s", controllerRoute, got)
			}
			if got := c.completedEvents(); got != 1 {
				t.Fatalf("the completed-handshake diagnostic was recorded %d times, want once", got)
			}
			if got := c.sessions.Load(); got != 1 {
				t.Fatalf("%d sessions installed, want 1", got)
			}
			if c.provider != nil && c.provider.Active() != 0 {
				t.Fatalf("the provider holds %d adapters after the handshake failed, want 0", c.provider.Active())
			}
			expectNoHandshakeState(t, c.r.b, "after the failure report was acknowledged")
		})
	}
	t.Run("over TCP, through the singleton handler", func(t *testing.T) {
		t.Parallel()
		c := completeCASE(t, clocktest.New(time.Unix(1_700_000_000, 0)), false)
		if err := c.controller.send(mrp.StandaloneAckOpcode, completedCaseExchange, nil, &c.report); err != nil {
			t.Fatal(err)
		}
		conn := &fakeConn{}
		route := c.r.b.routeFor(conn)
		r, ok := route.(*connRoute)
		if !ok {
			t.Fatalf("route %T", route)
		}
		holds := func() int {
			c.r.b.routeBindMu.Lock()
			defer c.r.b.routeBindMu.Unlock()
			return len(r.handshakes)
		}
		for i, step := range []struct {
			opcode  uint8
			payload []byte
		}{{mrp.SCOpcodeSigma1, c.sigma1}, {mrp.SCOpcodeSigma3, c.sigma3}} {
			hdr := &message.Header{MessageCounter: uint32(0x7000 + i), HasSourceNodeID: true, SourceNodeID: completedCaseInitiator}
			proto := message.ProtocolHeader{Initiator: true, Opcode: step.opcode, ExchangeID: completedCaseExchange, ProtocolID: mrp.SecureChannelProtocolID}
			if err := c.r.b.dispatchSecureChannel(route, hdr, proto, step.payload); err != nil {
				t.Fatal(err)
			}
			if i == 0 && holds() != 1 {
				t.Fatalf("%d connection holds after the same Sigma1 again, want 1", holds())
			}
		}
		if n := holds(); n != 0 {
			t.Fatalf("%d connection holds after the finished handshake's Sigma3, want 0", n)
		}
		if c.recordHeld() {
			t.Fatal("the handshake's record outlived its failed Sigma3")
		}
		if got := c.sessions.Load(); got != 1 {
			t.Fatalf("%d sessions installed, want 1", got)
		}
	})
}
