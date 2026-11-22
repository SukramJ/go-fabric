// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mrp_test

// A port of matter.js packages/protocol/test/protocol/MessageExchangeTest.ts
// :207-355 at b25ed960, describe "liveness kick on duplicate", against the
// predicate both exchange implementations share. matter.js drives a
// MessageExchange to the duplicate; here the state it reaches is stated:
// SLOW_PEER (both intervals 7 s, active threshold 200 ms) arms the BDX cap
// of 7.4 s, the margins are 0, the pending message acknowledges counter 10
// and went out gap ago. That a kick replaces the standalone acknowledgement
// is the callers' (client/exchange:TestMessageExchangeLivenessKickOnDuplicate,
// bridge:TestBDXLivenessKickOnDuplicate).

import (
	"testing"
	"time"

	"thingfabric.dev/go-fabric/transport/mrp"
)

// slowPeerKick is sendThenDuplicate's state (MessageExchangeTest.ts:225-301)
// at the duplicate, gap after the send.
func slowPeerKick(gap time.Duration) mrp.LivenessKick {
	return mrp.LivenessKick{
		BDX:               true,
		Armed:             true,
		PendingAcks:       true,
		PendingAckCounter: 10,
		DuplicateCounter:  10,
		Transmitted:       true,
		Elapsed:           gap,
		Interval:          7400 * time.Millisecond,
		ActiveInterval:    7 * time.Second,
		ActiveThreshold:   200 * time.Millisecond,
	}
}

func TestShouldKickForLivenessParityMatterJS(t *testing.T) {
	t.Parallel()
	t.Run("retransmits a BDX message when the peer re-asks for what it answers", func(t *testing.T) {
		t.Parallel()
		if !mrp.ShouldKickForLiveness(slowPeerKick(7100 * time.Millisecond)) {
			t.Error("no kick")
		}
	})
	t.Run("lets the retransmission carry the ack instead of sending a standalone one", func(t *testing.T) {
		t.Parallel()
		// The verdict is what replaces the standalone ack at the call site
		// (MessageExchange.ts:514-523): a kick means none is sent.
		if !mrp.ShouldKickForLiveness(slowPeerKick(7100 * time.Millisecond)) {
			t.Error("no kick: the standalone ack would go out")
		}
	})
	t.Run("does not retransmit before the schedule's own cadence has elapsed", func(t *testing.T) {
		t.Parallel()
		if mrp.ShouldKickForLiveness(slowPeerKick(6500 * time.Millisecond)) {
			t.Error("kicked")
		}
	})
	t.Run("does not retransmit when the peer can stay awake for the remaining wait", func(t *testing.T) {
		t.Parallel()
		k := slowPeerKick(7100 * time.Millisecond)
		k.ActiveThreshold = 500 * time.Millisecond
		if mrp.ShouldKickForLiveness(k) {
			t.Error("kicked")
		}
	})
	t.Run("acks rather than retransmitting once the retransmission budget is down to its last attempt", func(t *testing.T) {
		t.Parallel()
		k := slowPeerKick(7100 * time.Millisecond)
		k.NextBudgetSpent = true
		if mrp.ShouldKickForLiveness(k) {
			t.Error("kicked")
		}
	})
	t.Run("does not retransmit for a non-BDX protocol", func(t *testing.T) {
		t.Parallel()
		k := slowPeerKick(7100 * time.Millisecond)
		k.BDX = false
		if mrp.ShouldKickForLiveness(k) {
			t.Error("kicked")
		}
	})
	t.Run("does not retransmit for a duplicate of an unrelated message", func(t *testing.T) {
		t.Parallel()
		k := slowPeerKick(7100 * time.Millisecond)
		k.DuplicateCounter = 99
		if mrp.ShouldKickForLiveness(k) {
			t.Error("kicked")
		}
	})
}

// TestShouldKickForLivenessGates covers the checks the matter.js cases do
// not vary: a closed exchange, no running timer, a pending message that
// acknowledges nothing, an unknown transmission instant, the additional
// delay in the cadence. MessageExchange.ts:594-603, :605-607, :618-622 at
// b25ed960.
func TestShouldKickForLivenessGates(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		edit func(*mrp.LivenessKick)
		want bool
	}{
		{"closed", func(k *mrp.LivenessKick) { k.Closed = true }, false},
		{"timer not running", func(k *mrp.LivenessKick) { k.Armed = false }, false},
		{"pending acknowledges nothing", func(k *mrp.LivenessKick) { k.PendingAcks = false }, false},
		{"no transmission instant", func(k *mrp.LivenessKick) { k.Transmitted = false }, false},
		{"additional delay not yet elapsed", func(k *mrp.LivenessKick) { k.AdditionalDelay = 200 * time.Millisecond }, false},
		{"additional delay elapsed", func(k *mrp.LivenessKick) { k.AdditionalDelay = 100 * time.Millisecond }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			k := slowPeerKick(7100 * time.Millisecond)
			tc.edit(&k)
			if got := mrp.ShouldKickForLiveness(k); got != tc.want {
				t.Errorf("kick = %v, want %v", got, tc.want)
			}
		})
	}
}
