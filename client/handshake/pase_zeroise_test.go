// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package handshake_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/secure/spake2"
)

// TestPASEClosesItsProver: whichever way a PASE ends, the prover it ran on
// is closed — its own copy of Ke after a success, w0, w1 and x when the
// handshake ended before Pake2 (review II, R2-P0-rv-B03). What Close
// overwrites is pinned in secure/spake2 (TestCloseOverwritesKeAfterSuccess,
// TestCloseOverwritesAnAbandonedHandshake); here a closed prover is told
// by what it answers: no shared secret, and ErrSessionState to a step its
// state would otherwise take. Not parallel: it replaces the prover seam.
//
// Each case opens PASE to a device of its own. A device ignores a new PASE
// exchange while it holds an established PASE session that is not closing
// (bridge/securechannel.go, paseSessionExists), and the CloseSession that
// ends the first case's session asks for no acknowledgement
// (client/exchange Manager.CloseSession), so on a shared device the next
// case's PBKDFParamRequest can be handled first and go unanswered.
func TestPASEClosesItsProver(t *testing.T) {
	held := handshake.HoldProvers(t)
	closed := func(t *testing.T, p *spake2.Prover) {
		t.Helper()
		if ke := p.SharedSecret(); ke != nil {
			t.Errorf("the prover still hands out Ke: % x", ke)
		}
		// GeneratePake1 before Pake1, ProcessPake2 after it: one of the
		// two is the step an open prover would take next.
		_, err1 := p.GeneratePake1()
		_, err2 := p.ProcessPake2(make([]byte, spake2.PointSize), make([]byte, spake2.ConfirmTagSize))
		if !errors.Is(err1, spake2.ErrSessionState) || !errors.Is(err2, spake2.ErrSessionState) {
			t.Errorf("the prover was left open: GeneratePake1 %v, ProcessPake2 %v, want ErrSessionState twice", err1, err2)
		}
	}
	only := func(t *testing.T) *spake2.Prover {
		t.Helper()
		if len(*held) != 1 {
			t.Fatalf("%d provers created, want 1", len(*held))
		}
		p := (*held)[0]
		*held = nil
		return p
	}

	t.Run("after a success", func(t *testing.T) {
		_, addr := startNode(t)
		m, _ := newManager(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		s, err := handshake.PASE(ctx, m, addr, testPasscode, handshake.PASEOptions{SuppressPeerLoss: true})
		if err != nil {
			t.Fatalf("PASE: %v", err)
		}
		closed(t, only(t))
		// The session has its keys all the same.
		readNodeLabel(t, m, s)
		m.CloseSession(ctx, s)
	})

	t.Run("cancelled before Pake1 is sent", func(t *testing.T) {
		_, addr := startNode(t)
		m, _ := newManager(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if _, err := handshake.PASE(ctx, m, addr, testPasscode, handshake.PASEOptions{OnPBKDFParamResponse: cancel}); !errors.Is(err, context.Canceled) {
			t.Fatalf("PASE = %v, want context.Canceled", err)
		}
		closed(t, only(t))
	})

	t.Run("after a wrong passcode", func(t *testing.T) {
		_, addr := startNode(t)
		m, _ := newManager(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := handshake.PASE(ctx, m, addr, testPasscode+1, handshake.PASEOptions{}); !errors.Is(err, handshake.ErrPasscodeMismatch) {
			t.Fatalf("PASE = %v, want ErrPasscodeMismatch", err)
		}
		closed(t, only(t))
	})
}
