// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"net"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/clock"
	"thingfabric.dev/go-fabric/clock/clocktest"
	"thingfabric.dev/go-fabric/mdns"
	"thingfabric.dev/go-fabric/secure/sigma"
	"thingfabric.dev/go-fabric/transport/mrp"
)

// newUnsecuredRigOn is [newUnsecuredRig] with the bridge on clk.
func newUnsecuredRigOn(t *testing.T, clk clock.Clock) *unsecuredRig {
	t.Helper()
	b, err := New(wbEmptySnapshotter, mdns.NewNoop(), Config{Listen: "127.0.0.1:0", Clock: clk}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := b.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer stopCancel()
		_ = b.Stop(stopCtx)
	})
	b.AttachAckTracker(mrp.NewAckTracker(50 * time.Millisecond))
	return &unsecuredRig{b: b}
}

// TestSingletonResumedHandshakeEndsWithItsAcknowledgement: a CASE handshake
// the singleton handler completes by resumption is recorded as completed at
// its Sigma1, and its record and unsecured window end when the
// Sigma2_Resume is acknowledged — by the initiator's success report — as a
// Sigma3-completed handshake's end with its success report (review II,
// R2-P4-fr-sec-02). matter.js's CASE server waits for that success report
// and then closes the handshake's unsecured session
// (packages/protocol/src/session/case/CaseServer.ts:216, :95-98 at
// db021da0).
func TestSingletonResumedHandshakeEndsWithItsAcknowledgement(t *testing.T) {
	t.Parallel()
	r := newUnsecuredRig(t)
	ipk := newCaseTestIPK(t)
	responder := sigma.NewResponder(newCaseTestIdentity(t, 0xBBBB, 1, ipk), caseTestVerifier{}, 0x2001)
	sigma1Bytes, secret, rid := buildResumeSigma1(t)
	responder.SetResumptionStore(&fakeResumptionStore{id: rid, secret: secret})
	a := NewCaseAdapter(responder)
	established := 0
	a.SetOnSessionEstablished(func(sigma.SessionKeys, uint16) error { established++; return nil })
	r.b.AttachCaseHandler(a)

	const exchange = 0x0400
	p := newPipelinePeer(t, r.b, 0xD00D)
	if err := p.send(mrp.SCOpcodeSigma1, exchange, sigma1Bytes, nil); err != nil {
		t.Fatal(err)
	}
	_, counter, _ := p.recv(mrp.SCOpcodeSigma2Resume, "Sigma2_Resume")
	if established != 1 {
		t.Fatalf("the resumption installed %d sessions, want 1", established)
	}
	if ok := r.b.handshakeCompleted(HandshakeKey{InitiatorNodeID: 0xD00D, ExchangeID: exchange}); !ok {
		t.Fatal("the handshake the resumption completed is not recorded as completed")
	}
	report := mrp.EncodeStatusReport(mrp.SCStatusGeneralSuccess, uint32(mrp.SecureChannelProtocolID), mrp.SCStatusProtocolSessionEstablishmentSuccess, nil)
	if err := p.send(mrp.SCOpcodeStatusReport, exchange, report, &counter); err != nil {
		t.Fatal(err)
	}
	expectNoHandshakeState(t, r.b, "after the initiator's success report acknowledged the Sigma2_Resume")
}

// TestSingletonHandshakeErrorEndsItsRecord: a Sigma3 the singleton
// handler fails with an error — here the session's pickup — ends the
// handshake as a failure report does: the record goes, and once the failure
// report is acknowledged so does the unsecured window (review II,
// R2-P4-fr-sec-02).
func TestSingletonHandshakeErrorEndsItsRecord(t *testing.T) {
	t.Parallel()
	c := beginCASE(t, newUnsecuredRig(t), clocktest.New(time.Unix(1_700_000_000, 0)), false)
	a, ok := c.r.b.caseHandler.(*CaseAdapter)
	if !ok {
		t.Fatalf("singleton handler %T", c.r.b.caseHandler)
	}
	a.SetOnSessionEstablished(func(sigma.SessionKeys, uint16) error { return errors.New("pickup refused") })
	if err := c.controller.send(mrp.SCOpcodeSigma3, completedCaseExchange, c.sigma3, &c.sigma2Counter); err == nil {
		t.Fatal("the Sigma3 whose session pickup failed was handled without an error")
	}
	failure := c.expectReport(t, c.controller, mrp.SCStatusGeneralFailure, "the Sigma3 the handler failed")
	if c.recordHeld() {
		t.Fatal("the record outlived the Sigma3 the handler failed")
	}
	if err := c.controller.send(mrp.StandaloneAckOpcode, completedCaseExchange, nil, &failure); err != nil {
		t.Fatal(err)
	}
	expectNoHandshakeState(t, c.r.b, "after the failure report was acknowledged")
}

// TestSingletonUnfinishedHandshakeEndsAtTheSigma3Wait: a CASE handshake the
// singleton handler began and whose initiator sent no Sigma3 keeps its
// record and its unsecured window for the responder's wait for Sigma3
// after the Sigma2's acknowledgement ([Bridge.caseSigma3Wait]), and no
// longer; a Sigma2 that was never acknowledged holds them until
// [unsecuredEntryLifetime] after it was sent (review II, R2-P4-fr-sec-02).
// matter.js's CASE server reads Sigma3 with the exchange's timeout once
// the Sigma2 was acknowledged, and the timeout ends the handshake and its
// unsecured session (packages/protocol/src/session/case/CaseServer.ts:267-279,
// :95-98, packages/protocol/src/securechannel/SecureChannelMessenger.ts:83-89,
// packages/protocol/src/protocol/MessageExchange.ts:872-897 at db021da0).
func TestSingletonUnfinishedHandshakeEndsAtTheSigma3Wait(t *testing.T) {
	t.Parallel()
	t.Run("the Sigma2 acknowledged", func(t *testing.T) {
		t.Parallel()
		clk := clocktest.New(time.Unix(1_700_000_000, 0))
		c := beginCASE(t, newUnsecuredRigOn(t, clk), clk, false)
		if err := c.controller.send(mrp.StandaloneAckOpcode, completedCaseExchange, nil, &c.sigma2Counter); err != nil {
			t.Fatal(err)
		}
		wait := c.r.b.caseSigma3Wait(false)
		clk.Advance(wait - time.Millisecond)
		c.r.b.sweepUnsecuredWindows()
		if !c.recordHeld() || c.r.b.unsecuredWindows.len() != 1 {
			t.Fatalf("before the wait for Sigma3 ran out: record held %v, %d windows; want the record and its window", c.recordHeld(), c.r.b.unsecuredWindows.len())
		}
		clk.Advance(time.Millisecond)
		c.r.b.sweepUnsecuredWindows()
		expectNoHandshakeState(t, c.r.b, "once the wait for Sigma3 ran out")
		if err := c.controller.send(mrp.SCOpcodeSigma3, completedCaseExchange, c.sigma3, nil); err != nil {
			t.Fatal(err)
		}
		c.expectNoReply(t, 0, "a Sigma3 after the wait")
		if got := c.sessions.Load(); got != 0 {
			t.Fatalf("a Sigma3 after the wait installed %d sessions, want 0", got)
		}
	})
	t.Run("the Sigma2 never acknowledged", func(t *testing.T) {
		t.Parallel()
		clk := clocktest.New(time.Unix(1_700_000_000, 0))
		c := beginCASE(t, newUnsecuredRigOn(t, clk), clk, false)
		clk.Advance(unsecuredEntryLifetime)
		c.r.b.outboundReliable.Tick(clk.Now(), func(net.Addr, []byte) error { return nil })
		c.r.b.sweepUnsecuredWindows()
		expectNoHandshakeState(t, c.r.b, "once the Sigma2's lifetime ran out")
	})
	t.Run("more handshakes than windows", func(t *testing.T) {
		t.Parallel()
		clk := clocktest.New(time.Unix(1_700_000_000, 0))
		r := newUnsecuredRigOn(t, clk)
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
		peerID := newCaseTestIdentity(t, 0xAAAA, 1, ipk)
		for i := range maxUnsecuredWindows + 4 {
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
			_, c2, _ := p.recv(mrp.SCOpcodeSigma2, "Sigma2")
			if err := p.send(mrp.StandaloneAckOpcode, 0x0300, nil, &c2); err != nil {
				t.Fatal(err)
			}
		}
		clk.Advance(r.b.caseSigma3Wait(false))
		if w := r.b.acquireUnsecuredWindow(0xFEED, false); w == nil {
			t.Fatal("once the wait for Sigma3 ran out a new unsecured source got no window")
		}
		if records, windows, pending := handshakeState(r.b); records != 0 || windows != 1 || pending != 0 {
			t.Fatalf("once the wait for Sigma3 ran out: %d records, %d windows, %d replies waiting; want 0, the new source's 1, 0", records, windows, pending)
		}
	})
}

// TestSingletonCompletedRecordEndsAtItsAcknowledgement: the record of a
// handshake the singleton handler completed ends at the acknowledgement of
// its success report, on whatever exchange — here the fifth live exchange
// of one source, which its unsecured window is not bound to
// ([maxUnsecuredWindowExchanges]) (review II, R2-P4-fr-sec-05).
func TestSingletonCompletedRecordEndsAtItsAcknowledgement(t *testing.T) {
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
	peerID := newCaseTestIdentity(t, 0xAAAA, 1, ipk)
	p := newPipelinePeer(t, r.b, 0x7777)
	var (
		last   *sigma.Initiator
		lastS2 []byte
		lastC2 uint32
	)
	const exchanges = maxUnsecuredWindowExchanges + 1
	for i := range exchanges {
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
		if err := p.send(mrp.SCOpcodeSigma1, uint16(0x0500+i), s1, nil); err != nil {
			t.Fatal(err)
		}
		lastS2, lastC2, _ = p.recv(mrp.SCOpcodeSigma2, "Sigma2")
		last = ini
	}
	s3, err := last.ProcessSigma2Bytes(lastS2)
	if err != nil {
		t.Fatal(err)
	}
	exchange := uint16(0x0500 + exchanges - 1)
	if err := p.send(mrp.SCOpcodeSigma3, exchange, s3, &lastC2); err != nil {
		t.Fatal(err)
	}
	report, c3, _ := p.recv(mrp.SCOpcodeStatusReport, "the report on Sigma3")
	if g, _, _, ok := decodeStatusReport(report); !ok || g != mrp.SCStatusGeneralSuccess {
		t.Fatalf("report general status %d", g)
	}
	if err := p.send(mrp.StandaloneAckOpcode, exchange, nil, &c3); err != nil {
		t.Fatal(err)
	}
	if held := r.b.handshakeCompleted(HandshakeKey{InitiatorNodeID: 0x7777, ExchangeID: exchange}); held {
		t.Fatal("the completed handshake's record outlived the acknowledgement of its success report")
	}
}

// TestSingletonRecordEndsAtTheGiveUpOfItsReply: through the singleton
// handler, a handshake whose Sigma2 is given up after its retransmissions —
// or whose success report is — ends at the give-up, in the sweep of the
// retransmission table that gave it up, not at the next sweep of the
// unsecured windows (review II, phase 4 fr-sec round 2). matter.js ends the
// handshake when the exchange gives up the message: the send throws
// (packages/protocol/src/protocol/MessageExchange.ts:973-976, :840 at
// db021da0) and the CASE server's error path closes the handshake's
// unsecured session (packages/protocol/src/session/case/CaseServer.ts:61-99).
func TestSingletonRecordEndsAtTheGiveUpOfItsReply(t *testing.T) {
	t.Parallel()
	for _, completed := range []bool{false, true} {
		t.Run(map[bool]string{false: "the Sigma2", true: "the success report"}[completed], func(t *testing.T) {
			t.Parallel()
			clk := clocktest.New(time.Unix(1_700_000_000, 0))
			r := newUnsecuredRigOn(t, clk)
			c := beginCASE(t, r, clk, false)
			if completed {
				if err := c.controller.send(mrp.SCOpcodeSigma3, completedCaseExchange, c.sigma3, &c.sigma2Counter); err != nil {
					t.Fatal(err)
				}
				c.report = c.expectReport(t, c.controller, mrp.SCStatusGeneralSuccess, "the report on Sigma3")
			}
			key := HandshakeKey{InitiatorNodeID: completedCaseInitiator, ExchangeID: completedCaseExchange}
			start := clk.Now()
			for c.r.b.outboundReliable.holdsUnsecuredExchange(key) {
				if clk.Now().Sub(start) >= unsecuredEntryLifetime {
					t.Fatal("the reply was not given up before its lifetime ran out")
				}
				clk.Advance(500 * time.Millisecond)
				c.r.b.tickOutboundReliable(c.r.b.outboundReliable, clk.Now())
			}
			if c.recordHeld() {
				t.Fatalf("the record outlived the give-up of its reply (%v after it was sent)", clk.Now().Sub(start))
			}
		})
	}
}
