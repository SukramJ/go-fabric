// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// completions records what the bridge told a CASE adapter's host about the
// establishment of its sessions ([CaseAdapter.completeEstablishment]).
type completions struct {
	mu    sync.Mutex
	calls []completion
}

type completion struct {
	sessionID uint16
	completed bool
}

func (c *completions) hook(sessionID uint16, completed bool) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, completion{sessionID: sessionID, completed: completed})
	return completed
}

func (c *completions) snapshot() []completion {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.calls)
}

// TestCASEResponderEstablishment: the bridge tells the host of a CASE
// session's establishment at the moments matter.js completes it — a new
// session once its Success went out for the first time, before the
// initiator acknowledged it; a resumed one once the initiator's Success
// arrived, a failure report instead ending it (CaseServer.ts:204-210,
// :329-344 at b25ed960; findings F-b25ed960-23).
func TestCASEResponderEstablishment(t *testing.T) {
	t.Parallel()
	for name, provided := range map[string]bool{"singleton": false, "provider": true} {
		t.Run("full handshake, "+name, func(t *testing.T) {
			t.Parallel()
			c := beginCASE(t, newUnsecuredRig(t), clock.System{}, provided)
			var a *CaseAdapter
			if provided {
				a, _ = c.provider.Resolve(HandshakeKey{InitiatorNodeID: completedCaseInitiator, ExchangeID: completedCaseExchange}).(*CaseAdapter)
			} else {
				a, _ = c.r.b.caseHandler.(*CaseAdapter)
			}
			if a == nil {
				t.Fatal("no CASE adapter for the handshake")
			}
			rec := &completions{}
			a.setOnCompletion(rec.hook)
			if err := c.controller.send(mrp.SCOpcodeSigma3, completedCaseExchange, c.sigma3, &c.sigma2Counter); err != nil {
				t.Fatal(err)
			}
			c.expectReport(t, c.controller, mrp.SCStatusGeneralSuccess, "the report on Sigma3")
			// The controller has not acknowledged the Success: the host was
			// told at its first transmission.
			if got := rec.snapshot(); len(got) != 1 || got[0].sessionID != completedCaseSessionID || !got[0].completed {
				t.Fatalf("completions %+v, want one for session %#x, completed", got, completedCaseSessionID)
			}
		})
		t.Run("resumption completed by the initiator's Success, "+name, func(t *testing.T) {
			t.Parallel()
			rr, sigma1 := newResumeRig(t, provided)
			rec := &completions{}
			setResumeCompletion(t, rr, provided, rec)
			if err := rr.p.send(mrp.SCOpcodeSigma1, 0x0400, sigma1, nil); err != nil {
				t.Fatal(err)
			}
			_, counter, _ := rr.p.recv(mrp.SCOpcodeSigma2Resume, "Sigma2_Resume")
			if got := rec.snapshot(); len(got) != 0 {
				t.Fatalf("completions %+v before the initiator's Success, want none", got)
			}
			report := mrp.EncodeStatusReport(mrp.SCStatusGeneralSuccess, uint32(mrp.SecureChannelProtocolID), mrp.SCStatusProtocolSessionEstablishmentSuccess, nil)
			if err := rr.p.send(mrp.SCOpcodeStatusReport, 0x0400, report, &counter); err != nil {
				t.Fatal(err)
			}
			if got := rec.snapshot(); len(got) != 1 || got[0].sessionID != 0x2001 || !got[0].completed {
				t.Fatalf("completions %+v, want one for session 0x2001, completed", got)
			}
			// A retransmission of the Success completes nothing again.
			if err := rr.p.send(mrp.SCOpcodeStatusReport, 0x0400, report, &counter); err != nil {
				t.Fatal(err)
			}
			if got := rec.snapshot(); len(got) != 1 {
				t.Fatalf("completions %+v after a repeated Success, want one", got)
			}
		})
		t.Run("resumption ended by the initiator's failure report, "+name, func(t *testing.T) {
			t.Parallel()
			rr, sigma1 := newResumeRig(t, provided)
			rec := &completions{}
			setResumeCompletion(t, rr, provided, rec)
			if err := rr.p.send(mrp.SCOpcodeSigma1, 0x0400, sigma1, nil); err != nil {
				t.Fatal(err)
			}
			_, counter, _ := rr.p.recv(mrp.SCOpcodeSigma2Resume, "Sigma2_Resume")
			report := mrp.EncodeStatusReport(mrp.SCStatusGeneralFailure, uint32(mrp.SecureChannelProtocolID), mrp.SCStatusProtocolInvalidParameter, nil)
			if err := rr.p.send(mrp.SCOpcodeStatusReport, 0x0400, report, &counter); err != nil {
				t.Fatal(err)
			}
			if got := rec.snapshot(); len(got) != 1 || got[0].sessionID != 0x2001 || got[0].completed {
				t.Fatalf("completions %+v, want one for session 0x2001, failed", got)
			}
		})
	}
	t.Run("a Success on another exchange completes nothing", func(t *testing.T) {
		t.Parallel()
		rr, sigma1 := newResumeRig(t, false)
		rec := &completions{}
		setResumeCompletion(t, rr, false, rec)
		if err := rr.p.send(mrp.SCOpcodeSigma1, 0x0400, sigma1, nil); err != nil {
			t.Fatal(err)
		}
		rr.p.recv(mrp.SCOpcodeSigma2Resume, "Sigma2_Resume")
		report := mrp.EncodeStatusReport(mrp.SCStatusGeneralSuccess, uint32(mrp.SecureChannelProtocolID), mrp.SCStatusProtocolSessionEstablishmentSuccess, nil)
		if err := rr.p.send(mrp.SCOpcodeStatusReport, 0x0401, report, nil); err != nil {
			t.Fatal(err)
		}
		if got := rec.snapshot(); len(got) != 0 {
			t.Fatalf("completions %+v, want none", got)
		}
	})
}

// setResumeCompletion installs rec on the adapter that runs rr's resumption:
// the singleton, or the one the provider builds for its exchange.
func setResumeCompletion(t *testing.T, rr *resumeRig, provided bool, rec *completions) {
	t.Helper()
	var a *CaseAdapter
	if provided {
		rr.r.b.mu.RLock()
		provider := rr.r.b.caseProvider
		rr.r.b.mu.RUnlock()
		a, _ = provider(HandshakeKey{InitiatorNodeID: 0xD00D, ExchangeID: 0x0400}).(*CaseAdapter)
	} else {
		a, _ = rr.r.b.caseHandler.(*CaseAdapter)
	}
	if a == nil {
		t.Fatal("no CASE adapter for the resumption")
	}
	a.setOnCompletion(rec.hook)
}

// TestCASEResumeWaitsForSuccessUntilItsDeadline: a resumed session waits for
// the initiator's Success for the exchange's response time from its
// Sigma2_Resume's acknowledgement; at the deadline the handshake fails and
// the session is closed by entry through the host's hook, not left to the
// idle reaper (a host without the hook installs established: no deadline).
// Another message than a StatusReport on the exchange fails it at once; so
// does the Sigma2_Resume's give-up.
// matter.js packages/protocol/src/session/case/CaseServer.ts:204-210,
// MessageExchange.ts:894-917, SecureChannelMessenger.ts:94-98 at b25ed960
// (Fable post-read M1, L1, L4).
func TestCASEResumeWaitsForSuccessUntilItsDeadline(t *testing.T) {
	t.Parallel()
	acked := func(t *testing.T, hooked bool) (*resumeRig, *completions) {
		t.Helper()
		rr, sigma1 := newResumeRig(t, false)
		rec := &completions{}
		if hooked {
			setResumeCompletion(t, rr, false, rec)
		}
		if err := rr.p.send(mrp.SCOpcodeSigma1, 0x0400, sigma1, nil); err != nil {
			t.Fatal(err)
		}
		_, counter, _ := rr.p.recv(mrp.SCOpcodeSigma2Resume, "Sigma2_Resume")
		if err := rr.p.send(mrp.StandaloneAckOpcode, 0x0400, nil, &counter); err != nil {
			t.Fatal(err)
		}
		return rr, rec
	}
	t.Run("closes the session by entry at the deadline when the Success never comes", func(t *testing.T) {
		t.Parallel()
		rr, rec := acked(t, true)
		wait := rr.r.b.caseSigma3Wait(false)
		rr.clk.Advance(wait - time.Millisecond)
		if got := rec.snapshot(); len(got) != 0 {
			t.Fatalf("completions %+v before the deadline", got)
		}
		rr.clk.Advance(time.Millisecond)
		if got := rec.snapshot(); len(got) != 1 || got[0].sessionID != 0x2001 || got[0].completed {
			t.Fatalf("completions %+v at the deadline, want session 0x2001 failed", got)
		}
		if got := rr.closed(); len(got) != 0 {
			t.Fatalf("closed %v by id; the hook closes by entry", got)
		}
		rr.clk.Advance(sessionIdleTimeoutForTest)
		if len(rec.snapshot()) != 1 {
			t.Fatal("the deadline fired more than once")
		}
	})
	t.Run("has no deadline for a host that installs established", func(t *testing.T) {
		t.Parallel()
		rr, _ := acked(t, false)
		rr.clk.Advance(2 * rr.r.b.caseSigma3Wait(false))
		if got := rr.closed(); len(got) != 0 {
			t.Fatalf("closed %v: the session was installed established", got)
		}
	})
	t.Run("a Success before the deadline stops it", func(t *testing.T) {
		t.Parallel()
		rr, rec := acked(t, true)
		report := mrp.EncodeStatusReport(mrp.SCStatusGeneralSuccess, uint32(mrp.SecureChannelProtocolID), mrp.SCStatusProtocolSessionEstablishmentSuccess, nil)
		if err := rr.p.send(mrp.SCOpcodeStatusReport, 0x0400, report, nil); err != nil {
			t.Fatal(err)
		}
		rr.clk.Advance(2 * rr.r.b.caseSigma3Wait(false))
		if got := rec.snapshot(); len(got) != 1 || !got[0].completed {
			t.Fatalf("completions %+v, want one, completed", got)
		}
	})
	t.Run("another message on the exchange fails the resumption", func(t *testing.T) {
		t.Parallel()
		rr, rec := acked(t, true)
		if err := rr.p.send(mrp.SCOpcodeSigma3, 0x0400, []byte{0x15, 0x18}, nil); err != nil {
			t.Fatal(err)
		}
		if got := rec.snapshot(); len(got) != 1 || got[0].sessionID != 0x2001 || got[0].completed {
			t.Fatalf("completions %+v, want session 0x2001 failed", got)
		}
	})
	t.Run("the Sigma2_Resume's give-up closes the session by entry", func(t *testing.T) {
		t.Parallel()
		rr, sigma1 := newResumeRig(t, false)
		rec := &completions{}
		setResumeCompletion(t, rr, false, rec)
		if err := rr.p.send(mrp.SCOpcodeSigma1, 0x0400, sigma1, nil); err != nil {
			t.Fatal(err)
		}
		rr.p.recv(mrp.SCOpcodeSigma2Resume, "Sigma2_Resume")
		rr.giveUp()
		if got := rec.snapshot(); len(got) != 1 || got[0].sessionID != 0x2001 || got[0].completed {
			t.Fatalf("completions %+v, want session 0x2001 failed", got)
		}
	})
}

// sessionIdleTimeoutForTest is the idle reaper's timeout
// (secure/operational SessionIdleTimeout), the bound a resumed session's
// wait had before it had a deadline.
const sessionIdleTimeoutForTest = 5 * time.Minute

// TestCASEResumeGiveUpClosesByEntryOnly: F-b25ed960-38 — for a host that
// installs a resumed session establishing (the completion hook), every close
// of that session is by entry. The Sigma2_Resume's give-up names only an id,
// which by then may belong to another session, so it closes nothing by id:
// neither when the give-up itself ends the wait nor when the wait was ended
// by entry earlier, its Sigma2_Resume still unacknowledged. matter.js closes the session object, never an id
// (packages/protocol/src/session/case/CaseServer.ts:177-190, :192-211 at
// b25ed960).
func TestCASEResumeGiveUpClosesByEntryOnly(t *testing.T) {
	t.Parallel()
	t.Run("the give-up ends the wait", func(t *testing.T) {
		t.Parallel()
		rr, sigma1 := newResumeRig(t, false)
		rec := &completions{}
		setResumeCompletion(t, rr, false, rec)
		if err := rr.p.send(mrp.SCOpcodeSigma1, 0x0400, sigma1, nil); err != nil {
			t.Fatal(err)
		}
		rr.p.recv(mrp.SCOpcodeSigma2Resume, "Sigma2_Resume")
		rr.giveUp()
		if got := rec.snapshot(); len(got) != 1 || got[0].sessionID != 0x2001 || got[0].completed {
			t.Fatalf("completions %+v, want session 0x2001 failed", got)
		}
		if got := rr.closed(); len(got) != 0 {
			t.Fatalf("closed %v by id; the hook closed the session by entry", got)
		}
	})
	t.Run("the wait was ended by entry before the give-up", func(t *testing.T) {
		t.Parallel()
		rr, sigma1 := newResumeRig(t, false)
		rec := &completions{}
		setResumeCompletion(t, rr, false, rec)
		if err := rr.p.send(mrp.SCOpcodeSigma1, 0x0400, sigma1, nil); err != nil {
			t.Fatal(err)
		}
		rr.p.recv(mrp.SCOpcodeSigma2Resume, "Sigma2_Resume")
		// The wait ends by entry while the Sigma2_Resume is still
		// unacknowledged — as its deadline or a failure on another path
		// ends it.
		if id, byEntry := rr.r.b.endAwaitedResume(HandshakeKey{InitiatorNodeID: 0xD00D, ExchangeID: 0x0400}); id != 0x2001 || !byEntry {
			t.Fatalf("ended %#x by entry %v, want 0x2001 by entry", id, byEntry)
		}
		// The id is free again; a later session may hold it. The give-up
		// of the unacknowledged Sigma2_Resume must not close it.
		rr.giveUp()
		if got := rr.closed(); len(got) != 0 {
			t.Fatalf("closed %v by id after the wait ended by entry", got)
		}
		if got := rec.snapshot(); len(got) != 1 {
			t.Fatalf("completions %+v, want the one of the ended wait", got)
		}
	})
	t.Run("a provider's wait whose record is gone ends at its deadline", func(t *testing.T) {
		t.Parallel()
		// With a provider the give-up finds the adapter through the
		// handshake's record; once that is gone it finds none, and the
		// deadline armed at the send is the only close.
		rr, sigma1 := newResumeRig(t, true)
		rec := &completions{}
		setResumeCompletion(t, rr, true, rec)
		if err := rr.p.send(mrp.SCOpcodeSigma1, 0x0400, sigma1, nil); err != nil {
			t.Fatal(err)
		}
		rr.p.recv(mrp.SCOpcodeSigma2Resume, "Sigma2_Resume")
		key := HandshakeKey{InitiatorNodeID: 0xD00D, ExchangeID: 0x0400}
		rr.r.b.mu.Lock()
		delete(rr.r.b.sigma1Replied, key)
		rr.r.b.mu.Unlock()
		rr.giveUp()
		if got := rr.closed(); len(got) != 0 {
			t.Fatalf("closed %v by id at the give-up", got)
		}
		rr.clk.Advance(rr.r.b.caseSigma3Wait(false) + unsecuredEntryLifetime)
		if got := rec.snapshot(); len(got) != 1 || got[0].sessionID != 0x2001 || got[0].completed {
			t.Fatalf("completions %+v after the deadline, want session 0x2001 failed", got)
		}
		if got := rr.closed(); len(got) != 0 {
			t.Fatalf("closed %v by id", got)
		}
	})
}

// TestCASEResumeWaitOutlivesStop: a resumed session still waiting for the
// initiator's Success when the bridge stops is failed by its deadline —
// the host's completion hook runs (and with it the release of the host's
// resumption claim, P6-D): Stop does not touch the wait's timer.
func TestCASEResumeWaitOutlivesStop(t *testing.T) {
	t.Parallel()
	rr, sigma1 := newResumeRig(t, false)
	rec := &completions{}
	setResumeCompletion(t, rr, false, rec)
	if err := rr.p.send(mrp.SCOpcodeSigma1, 0x0400, sigma1, nil); err != nil {
		t.Fatal(err)
	}
	_, counter, _ := rr.p.recv(mrp.SCOpcodeSigma2Resume, "Sigma2_Resume")
	if err := rr.p.send(mrp.StandaloneAckOpcode, 0x0400, nil, &counter); err != nil {
		t.Fatal(err)
	}
	if err := rr.r.b.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := rec.snapshot(); len(got) != 0 {
		t.Fatalf("completions %+v at Stop, want none yet", got)
	}
	rr.clk.Advance(rr.r.b.caseSigma3Wait(false))
	if got := rec.snapshot(); len(got) != 1 || got[0].sessionID != 0x2001 || got[0].completed {
		t.Fatalf("completions %+v after the deadline, want session 0x2001 failed", got)
	}
}
