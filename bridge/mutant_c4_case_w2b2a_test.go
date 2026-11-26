// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/secure/sigma"
	"thingfabric.dev/go-fabric/transport/mrp"
)

// twoAddrResolverW2B2a resolves every peer to two addresses.
type twoAddrResolverW2B2a struct{}

func (twoAddrResolverW2B2a) ResolveOperational(context.Context, [8]byte, uint64) ([]*net.UDPAddr, error) {
	return []*net.UDPAddr{
		{IP: net.IPv6loopback, Port: 5541},
		{IP: net.IPv6loopback, Port: 5542},
	}, nil
}

// TestCaseInitiatorStopsWhenTheNextInitiatorFailsW2B2a (C4, case_initiator.go
// initiateCASE, the provider guard of a further address): a handshake to
// the first address that fails for a reason other than ctx moves on to the
// second address with a fresh initiator; when the provider cannot give
// one, the attempt ends with the first address's error — no handshake runs
// without an initiator.
func TestCaseInitiatorStopsWhenTheNextInitiatorFailsW2B2a(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	// An initiator that has sent its Sigma1 already refuses a second
	// GenerateSigma1: the first address fails at once, before any send.
	used := sigma.NewInitiator(nil, nil, 1, [32]byte{})
	if _, err := used.GenerateSigma1(); err != nil {
		t.Fatal(err)
	}
	errNoInitiator := errors.New("bridge: test: no second initiator")
	var calls atomic.Int32
	provider := func(context.Context, uint8, uint64) (*CaseInitiation, error) {
		if calls.Add(1) == 1 {
			return &CaseInitiation{Initiator: used}, nil
		}
		return nil, errNoInitiator
	}
	defer func() {
		if p := recover(); p != nil {
			t.Fatalf("initiateCASE panicked: %v — a handshake ran without an initiator", p)
		}
	}()
	_, err := b.initiateCASE(context.Background(), provider, twoAddrResolverW2B2a{}, 1, 0x1234)
	if !errors.Is(err, sigma.ErrSessionState) {
		t.Fatalf("initiateCASE = %v, want the first address's %v", err, sigma.ErrSessionState)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("provider called %d times, want 2 (the first address, then the second's fresh initiator)", got)
	}
}

// TestAwaitInitiatedSkipsAStrayMessageW2B2a (C4, case_initiator.go
// awaitInitiated): with opcodes given, a message of another opcode is a
// stray retransmission and skipped; the wait returns the one it waits for.
func TestAwaitInitiatedSkipsAStrayMessageW2B2a(t *testing.T) {
	t.Parallel()
	ex := &initiatedExchange{ch: make(chan initiatedMessage, 4)}
	ex.ch <- initiatedMessage{opcode: mrp.SCOpcodeSigma1}
	ex.ch <- initiatedMessage{opcode: mrp.SCOpcodeSigma2, counter: 7}
	msg, err := awaitInitiated(context.Background(), ex, mrp.SCOpcodeSigma2, mrp.SCOpcodeSigma2Resume)
	if err != nil {
		t.Fatal(err)
	}
	if msg.opcode != mrp.SCOpcodeSigma2 || msg.counter != 7 {
		t.Fatalf("awaitInitiated returned opcode %#x (counter %d), want the Sigma2 after the stray Sigma1", msg.opcode, msg.counter)
	}
}

// TestSigma3EstablisherIsNilForAStepThatEstablishedNothingW2B2a (C4,
// securechannel.go sigma3EstablisherOf): a Sigma3 step of a [CaseAdapter]
// that established nothing — failed, or a Sigma3 of a completed handshake
// — names no establisher, so the Success's send completes no session.
func TestSigma3EstablisherIsNilForAStepThatEstablishedNothingW2B2a(t *testing.T) {
	t.Parallel()
	for _, outcome := range []sigma3Outcome{sigma3Failed, sigma3OfCompleted} {
		step := &sigma3Step{handler: &CaseAdapter{}, outcome: outcome}
		if a, id := sigma3EstablisherOf(step); a != nil || id != 0 {
			t.Errorf("outcome %v: establisher %p names session %#x, want none", outcome, a, id)
		}
	}
}

// resumingHandlerW2B2a is a [CaseHandler] other than [CaseAdapter] that
// answers every step with Sigma2_Resume and reports the session it
// established ([establishedSessionReporter]).
type resumingHandlerW2B2a struct{ session uint16 }

func (h resumingHandlerW2B2a) ProcessSigma1([]byte) (opcode uint8, reply []byte, err error) {
	return mrp.SCOpcodeSigma2Resume, []byte{0x15, 0x18}, nil
}

func (h resumingHandlerW2B2a) ProcessSigma3([]byte) (opcode uint8, reply []byte, err error) {
	return mrp.SCOpcodeSigma2Resume, []byte{0x15, 0x18}, nil
}

func (h resumingHandlerW2B2a) ProcessSigma2Resume([]byte) (opcode uint8, reply []byte, err error) {
	return 0, nil, errors.New("bridge: test: unused")
}

func (h resumingHandlerW2B2a) establishedSession() (uint16, bool) { return h.session, true }

// TestForeignSigma1ResumeNamesItsSessionW2B2a (C4, securechannel.go
// processSigma1With): a handler other than [CaseAdapter] that answers
// Sigma1 with Sigma2_Resume established a session, and the Sigma1 step
// names it, taken at the step.
func TestForeignSigma1ResumeNamesItsSessionW2B2a(t *testing.T) {
	t.Parallel()
	const sid = 0x4711
	h := resumingHandlerW2B2a{session: sid}
	step := &sigma1Step{handler: h}
	if _, _, err := processSigma1With(h, []byte{0x15, 0x18}, step); err != nil {
		t.Fatal(err)
	}
	if id, ok := step.establishedSession(); !ok || id != sid {
		t.Fatalf("the Sigma1 step names session %#x (%v), want the resumed %#x", id, ok, uint16(sid))
	}
}

// TestForeignSigma3AnsweringResumeIsToleratedW2B2a (C4, marginal,
// securechannel.go handleCase): only a Sigma1 step of a [CaseAdapter]
// installs a resumed session; a handler other than [CaseAdapter] whose
// ProcessSigma3 answers with Sigma2_Resume — outside its contract — is
// sent on and does not take the router down.
func TestForeignSigma3AnsweringResumeIsToleratedW2B2a(t *testing.T) {
	t.Parallel()
	r := newUnsecuredRig(t)
	const x = uint16(0x3301)
	step := &sigma3Step{handler: resumingHandlerW2B2a{session: 0x0A0A}}
	hdr, proto := r.request(mrp.SCOpcodeSigma3, x)
	defer func() {
		if p := recover(); p != nil {
			t.Fatalf("handleCase panicked on a foreign Sigma3's Sigma2_Resume: %v", p)
		}
	}()
	process := func(p []byte) (uint8, []byte, error) { return processSigma3With(step.handler, p, step) }
	if err := r.b.handleCase(r.peer.LocalAddr(), hdr, proto, nil, "sigma3", process, step); err != nil {
		t.Fatalf("handleCase: %v", err)
	}
}
