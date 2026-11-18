// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"net"
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// resumeRig is a started bridge on a fake clock whose CASE handler — the
// singleton, or a per-exchange provider's adapter — resumes every Sigma1
// of [buildResumeSigma1] into session 0x2001, with a session registry that
// records what it is told to close.
type resumeRig struct {
	r   *unsecuredRig
	clk *clocktest.Fake
	reg *fakeSessionRegistry
	p   *pipelinePeer
}

func newResumeRig(t *testing.T, provided bool) (rr *resumeRig, sigma1 []byte) {
	t.Helper()
	clk := clocktest.New(time.Unix(1_700_000_000, 0))
	r := newUnsecuredRigOn(t, clk)
	reg := &fakeSessionRegistry{}
	r.b.AttachSessionRegistry(reg)
	ipk := newCaseTestIPK(t)
	id := newCaseTestIdentity(t, 0xBBBB, 1, ipk)
	sigma1Bytes, secret, rid := buildResumeSigma1(t)
	adapter := func() *CaseAdapter {
		responder := sigma.NewResponder(id, caseTestVerifier{}, 0x2001)
		responder.SetResumptionStore(&fakeResumptionStore{id: rid, secret: secret})
		a := NewCaseAdapter(responder)
		a.SetOnSessionEstablished(func(sigma.SessionKeys, uint16) error { return nil })
		return a
	}
	if provided {
		r.b.AttachCaseHandlerProvider(NewPerExchangeCaseProvider(adapter).Resolve)
	} else {
		r.b.AttachCaseHandler(adapter())
	}
	return &resumeRig{r: r, clk: clk, reg: reg, p: newPipelinePeer(t, r.b, 0xD00D)}, sigma1Bytes
}

// giveUp moves the clock past every retransmission of what the bridge
// still waits on, ticking the retransmit table at each step.
func (rr *resumeRig) giveUp() {
	for range 2 * mrp.MaxRetransmissions {
		rr.clk.Advance(30 * time.Second)
		rr.r.b.tickOutboundReliable(rr.r.b.outboundReliable, rr.clk.Now())
	}
}

func (rr *resumeRig) closed() []uint16 {
	rr.reg.mu.Lock()
	defer rr.reg.mu.Unlock()
	return slices.Clone(rr.reg.closed)
}

// TestR2P6Sec3320Closes: review II, R2-P6-sec-3320 — the session a
// resumption installed is closed when its Sigma2_Resume is given up
// unacknowledged, with the singleton handler and with a provider's.
func TestR2P6Sec3320Closes(t *testing.T) {
	t.Parallel()
	for name, provided := range map[string]bool{"singleton": false, "provider": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rr, sigma1 := newResumeRig(t, provided)
			if err := rr.p.send(mrp.SCOpcodeSigma1, 0x0400, sigma1, nil); err != nil {
				t.Fatal(err)
			}
			rr.p.recv(mrp.SCOpcodeSigma2Resume, "Sigma2_Resume")
			if got := rr.closed(); len(got) != 0 {
				t.Fatalf("closed %v before the Sigma2_Resume was given up", got)
			}
			rr.giveUp()
			if got := rr.closed(); !slices.Equal(got, []uint16{0x2001}) {
				t.Fatalf("closed %v after the Sigma2_Resume was given up, want [0x2001]", got)
			}
		})
	}
}

// TestR2P6Sec3320KeepsAcknowledged: review II, R2-P6-sec-3320 — a
// Sigma2_Resume the initiator acknowledged keeps its session; nor is a
// session that closed before the give-up closed again by its id.
func TestR2P6Sec3320KeepsAcknowledged(t *testing.T) {
	t.Parallel()
	t.Run("acknowledged", func(t *testing.T) {
		t.Parallel()
		rr, sigma1 := newResumeRig(t, false)
		if err := rr.p.send(mrp.SCOpcodeSigma1, 0x0400, sigma1, nil); err != nil {
			t.Fatal(err)
		}
		_, counter, _ := rr.p.recv(mrp.SCOpcodeSigma2Resume, "Sigma2_Resume")
		report := mrp.EncodeStatusReport(mrp.SCStatusGeneralSuccess, uint32(mrp.SecureChannelProtocolID), mrp.SCStatusProtocolSessionEstablishmentSuccess, nil)
		if err := rr.p.send(mrp.SCOpcodeStatusReport, 0x0400, report, &counter); err != nil {
			t.Fatal(err)
		}
		rr.giveUp()
		if got := rr.closed(); len(got) != 0 {
			t.Fatalf("closed %v after the Sigma2_Resume was acknowledged", got)
		}
	})
	t.Run("closed first", func(t *testing.T) {
		t.Parallel()
		rr, sigma1 := newResumeRig(t, false)
		if err := rr.p.send(mrp.SCOpcodeSigma1, 0x0400, sigma1, nil); err != nil {
			t.Fatal(err)
		}
		rr.p.recv(mrp.SCOpcodeSigma2Resume, "Sigma2_Resume")
		rr.r.b.SessionClosed(0x2001)
		rr.giveUp()
		if got := rr.closed(); len(got) != 0 {
			t.Fatalf("closed %v: the session had closed before its Sigma2_Resume was given up", got)
		}
	})
}

// TestR2P6Sec3320Ends: review II, R2-P6-sec-3320 — the other ends of a
// Sigma2_Resume the initiator did not acknowledge: its time on the
// unsecured session running out, its send failing, and its handshake ended
// from outside (the provider letting the adapter go) each close the
// installed session; an entry that lost its place in the retransmit table
// to newer ones closes nothing — its datagram went out and the
// acknowledgement may still come.
func TestR2P6Sec3320Ends(t *testing.T) {
	t.Parallel()
	resumed := func(t *testing.T) *resumeRig {
		t.Helper()
		rr, sigma1 := newResumeRig(t, false)
		if err := rr.p.send(mrp.SCOpcodeSigma1, 0x0400, sigma1, nil); err != nil {
			t.Fatal(err)
		}
		rr.p.recv(mrp.SCOpcodeSigma2Resume, "Sigma2_Resume")
		return rr
	}
	t.Run("expired", func(t *testing.T) {
		t.Parallel()
		rr := resumed(t)
		rr.clk.Advance(unsecuredEntryLifetime + time.Second)
		rr.r.b.tickOutboundReliable(rr.r.b.outboundReliable, rr.clk.Now())
		if got := rr.closed(); !slices.Equal(got, []uint16{0x2001}) {
			t.Fatalf("closed %v after the entry's lifetime, want [0x2001]", got)
		}
	})
	t.Run("ended from outside", func(t *testing.T) {
		t.Parallel()
		rr := resumed(t)
		rr.r.b.ForgetSigma1Replied(HandshakeKey{InitiatorNodeID: 0xD00D, ExchangeID: 0x0400})
		if got := rr.closed(); !slices.Equal(got, []uint16{0x2001}) {
			t.Fatalf("closed %v after the handshake was ended, want [0x2001]", got)
		}
	})
	t.Run("evicted", func(t *testing.T) {
		t.Parallel()
		rr := resumed(t)
		for i := range unsecuredEntryLimit {
			rr.r.b.outboundReliable.trackUnsecured(uint32(0x9000+i), HandshakeKey{InitiatorNodeID: 0xE00E, ExchangeID: uint16(0x7000 + i)}, []byte{1}, rr.p.conn.LocalAddr(), rr.clk.Now(), nil)
		}
		rr.giveUp()
		if got := rr.closed(); len(got) != 0 {
			t.Fatalf("closed %v after the entry lost its place", got)
		}
	})
	t.Run("send failed", func(t *testing.T) {
		t.Parallel()
		rr, sigma1 := newResumeRig(t, false)
		hdr := message.Header{MessageCounter: 1, HasSourceNodeID: true, SourceNodeID: 0xD00D}
		proto := message.ProtocolHeader{Initiator: true, NeedsAck: true, Opcode: mrp.SCOpcodeSigma1, ExchangeID: 0x0400, ProtocolID: mrp.SecureChannelProtocolID}
		datagram := append(append(hdr.Marshal(), proto.Marshal()...), sigma1...)
		// A route the bridge cannot send on: the reply's send fails.
		_ = rr.r.b.dispatch(t.Context(), datagram, &net.IPAddr{IP: net.IPv6loopback})
		if got := rr.closed(); !slices.Equal(got, []uint16{0x2001}) {
			t.Fatalf("closed %v after the Sigma2_Resume's send failed, want [0x2001]", got)
		}
	})
}
