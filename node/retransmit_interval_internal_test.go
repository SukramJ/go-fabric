// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"testing"
	"time"

	"thingfabric.dev/go-fabric/clock/clocktest"
	"thingfabric.dev/go-fabric/secure/sigma"
	"thingfabric.dev/go-fabric/transport/mrp"
)

// TestRetransmitIntervalFollowsThePeer: the session lookup the node
// attaches to its bridge answers a session's retransmission base interval
// from the intervals its peer advertised — the idle one while the peer is
// idle, the active one once it was heard within its active threshold —
// and the spec default for a peer that advertised none. The bridge's
// outbound tracker sizes every retransmission of a secure session from
// it (bridge.SessionRetransmitIntervalResolver); before the node wired it,
// each one ran on the 500 ms idle default. Mirrors matter.js
// MessageChannel.ts:268-275 and MRP.ts:181 at b25ed960.
func TestRetransmitIntervalFollowsThePeer(t *testing.T) {
	t.Parallel()
	cfg := mdnsTestConfig(t)
	cfg.Network.MDNS.Off = true
	clk := clocktest.New(time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC))
	cfg.Clock = clk
	n := newMDNSTestNode(t, cfg)

	advertised, err := n.sec.sessions.OpenFromSigma(1, 2, 3, sigma.SessionKeys{})
	if err != nil {
		t.Fatal(err)
	}
	const idle, active, threshold = 2000 * time.Millisecond, 700 * time.Millisecond, 4000 * time.Millisecond
	advertised.SetPeerMRPIntervals(uint32(idle/time.Millisecond), uint32(active/time.Millisecond), uint32(threshold/time.Millisecond))
	silent, err := n.sec.sessions.OpenFromSigma(1, 2, 4, sigma.SessionKeys{})
	if err != nil {
		t.Fatal(err)
	}

	interval := func(id uint16) time.Duration {
		t.Helper()
		d, ok := n.sec.lookup.RetransmitBaseInterval(id, clk.Now())
		if !ok {
			t.Fatalf("session %d: no retransmission interval", id)
		}
		return d
	}
	// The session opens on a message heard from the peer: active.
	if got := interval(advertised.SessionID); got != active {
		t.Errorf("an active peer's base interval is %v, want its advertised active %v", got, active)
	}
	clk.Advance(threshold + time.Second)
	if got := interval(advertised.SessionID); got != idle {
		t.Errorf("an idle peer's base interval is %v, want its advertised idle %v", got, idle)
	}
	advertised.MarkActiveRx()
	if got := interval(advertised.SessionID); got != active {
		t.Errorf("a peer heard again: %v, want its advertised active %v", got, active)
	}
	if got := interval(silent.SessionID); got != mrp.SessionIdleIntervalDefault {
		t.Errorf("a peer that advertised nothing: %v, want the default %v", got, mrp.SessionIdleIntervalDefault)
	}
	if _, ok := n.sec.lookup.RetransmitBaseInterval(0xfffe, clk.Now()); ok {
		t.Error("an unknown session answered an interval")
	}
}
