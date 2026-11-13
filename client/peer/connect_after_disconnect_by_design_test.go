// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// TestConnectAfterDisconnectRetries pins
// BD-Matter-Peer-ConnectAfterDisconnectRetries (notes/parity/by_design.md):
// a connection process started after Set.Disconnect retries an attempt
// that cannot succeed, at the cadence of an unhandled error.
//
// matter.js's PeerSet.disconnect clears its exchange manager
// (packages/protocol/src/peer/PeerSet.ts:248-256 at db021da0), after which
// the peer context's exchanges accessor throws (:83-86). An attempt reads
// it when it creates its exchange
// (packages/protocol/src/peer/PeerConnection.ts:544). An error that is
// neither a status response nor a TransientPeerCommunicationError gets
// timing.delayAfterUnhandledError and the category "general" (:715-717), a
// warning (:762-764) and another attempt.
//
// TestUnhandledErrorAndDisconnectedSet holds the first pause and the
// timeout; this test holds the repetition: every round fails with
// ErrNotConnectable, is classified "general", pauses
// DelayAfterUnhandledError — not a moment less — sends nothing, and the
// process is still running after it.
func TestConnectAfterDisconnectRetries(t *testing.T) {
	t.Parallel()

	// The classification the cadence follows from.
	timing := DefaultTiming()
	delay, hasDelay, category := classifyError(ErrNotConnectable, timing, func() float64 { return 0.5 }, func() bool { return false })
	if delay != timing.DelayAfterUnhandledError || !hasDelay || category != "general" {
		t.Fatalf("classifyError(ErrNotConnectable) = %v, %v, %q; want %v, true, \"general\"", delay, hasDelay, category, timing.DelayAfterUnhandledError)
	}

	synctest.Test(t, func(t *testing.T) {
		var (
			mu     sync.Mutex
			failed []error
		)
		attempts := func() []error {
			mu.Lock()
			defer mu.Unlock()
			return append([]error(nil), failed...)
		}
		h := newHarness(t, nil)
		p, _ := h.set.AddKnownPeer(Descriptor{Address: testAddr, OperationalAddress: udpAddr(addr1)})
		h.set.Disconnect(context.Background())
		// No timeout: the process ends only when something ends it. The
		// error handler is consulted once per failed attempt and keeps the
		// table's delay.
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		res := make(chan connectResult, 1)
		go func() {
			s, err := p.Connect(ctx, ConnectOptions{ConnectionTimeout: -1, HandleError: func(err error) (time.Duration, bool, error) {
				mu.Lock()
				defer mu.Unlock()
				failed = append(failed, err)
				return 0, false, nil
			}})
			res <- connectResult{s, err}
		}()

		const rounds = 4
		for round := 1; round <= rounds; round++ {
			// The pause after this round's attempt.
			h.clock.waitTimer(t, delay)
			got := attempts()
			if len(got) != round {
				t.Fatalf("round %d: %d failed attempts, want %d", round, len(got), round)
			}
			if err := got[round-1]; !errors.Is(err, ErrNotConnectable) {
				t.Fatalf("round %d: the attempt failed with %v, want ErrNotConnectable", round, err)
			}
			h.udp.none(t)
			h.clock.Advance(delay - time.Millisecond)
			if !h.clock.hasTimer(time.Millisecond) {
				t.Fatalf("round %d: the %v pause ended early", round, delay)
			}
			if n := len(attempts()); n != round {
				t.Fatalf("round %d: attempt %d started inside the pause", round, n)
			}
			h.clock.Advance(time.Millisecond)
		}
		// The next attempt follows the last pause, and the process goes on.
		h.clock.waitTimer(t, delay)
		if n := len(attempts()); n != rounds+1 {
			t.Fatalf("%d failed attempts after %d pauses, want %d", n, rounds, rounds+1)
		}
		h.udp.none(t)
		select {
		case r := <-res:
			t.Fatalf("Connect returned (%v, %v); the process retries until it is ended", r.s, r.err)
		default:
		}
		// Only the caller's context ends the wait.
		cancel()
		if r := waitResult(t, res); !errors.Is(r.err, context.Canceled) {
			t.Fatalf("Connect = %v, want the caller's cancellation", r.err)
		}
	})
}
