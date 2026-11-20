// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"slices"
	"sync"
	"testing"

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
