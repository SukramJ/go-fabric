// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

// A port of matter.js packages/protocol/test/peer/PeerConnectTimeoutTest.ts at
// 862ad75f: the timeout Peer.connect resolves from the caller's options.
//
// matter.js tests the isolated expression of Peer.ts:466-475; Go tests the
// same contract through Connect itself. The caller's abort signal is the
// Go context, Forever is a negative ConnectionTimeout.

import (
	"context"
	"errors"
	"testing"
	"time"
)

// connectWith runs Connect on ctx in the background against a peer whose
// only address never answers.
func connectWith(ctx context.Context, t *testing.T, opts ConnectOptions) (*harness, <-chan connectResult) {
	t.Helper()
	h := newHarness(t, nil)
	p, _ := h.set.AddKnownPeer(Descriptor{Address: testAddr, OperationalAddress: udpAddr(addr1)})
	out := make(chan connectResult, 1)
	go func() {
		s, err := p.Connect(ctx, opts)
		out <- connectResult{s, err}
	}()
	h.sigma1(t, addr1)
	return h, out
}

// expectUnreachableAfter checks that Connect returns UnreachableError
// exactly d after it started.
func expectUnreachableAfter(t *testing.T, h *harness, res <-chan connectResult, d time.Duration) {
	t.Helper()
	h.clock.waitTimer(t, d)
	h.clock.Advance(d - time.Millisecond)
	if !h.clock.hasTimer(time.Millisecond) {
		t.Fatalf("the %v connection timeout is no longer armed 1 ms before it is due", d)
	}
	h.clock.Advance(time.Millisecond)
	var unreachable *UnreachableError
	if r := waitResult(t, res); !errors.As(r.err, &unreachable) {
		t.Fatalf("Connect = %v, want UnreachableError", r.err)
	}
}

// TestPeerConnectTimeout is the describe "Peer.connect timeout semantics".
func TestPeerConnectTimeout(t *testing.T) {
	t.Parallel()
	t.Run("falls back to defaultConnectionTimeout when abort is provided without connectionTimeout", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		h, res := connectWith(ctx, t, ConnectOptions{})
		expectUnreachableAfter(t, h, res, DefaultTiming().DefaultConnectionTimeout)
	})

	t.Run("uses caller's connectionTimeout when one is supplied alongside abort", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		h, res := connectWith(ctx, t, ConnectOptions{ConnectionTimeout: 5 * time.Second}) // :48
		expectUnreachableAfter(t, h, res, 5*time.Second)
	})

	t.Run("remains unbounded when caller explicitly passes Forever", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		h, res := connectWith(ctx, t, ConnectOptions{ConnectionTimeout: -1})
		// An hour, far past the 90 s default; the Sigma1 retransmissions
		// are drained.
		for range 60 {
			h.clock.Advance(time.Minute)
			h.udp.drain()
		}
		select {
		case r := <-res:
			t.Fatalf("Connect with Forever returned %v", r.err)
		default:
		}
		// Only the caller's abort ends the wait.
		cancel()
		if r := waitResult(t, res); !errors.Is(r.err, context.Canceled) {
			t.Fatalf("Connect after the abort = %v", r.err)
		}
	})
}
