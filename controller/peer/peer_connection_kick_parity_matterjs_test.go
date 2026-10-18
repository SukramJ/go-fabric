// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

// A port of matter.js packages/protocol/test/peer/PeerConnectionKickTest.ts at
// 862ad75f: the kick timing parameters, the kick throttle, the per-origin
// restart cooldown, the retransmission gate and the restart of one try.
//
// matter.js models the closures in isolation; Go tests the closures of
// controller/peer: the throttle is connection.kick (matter.js's
// QuietObservable), the cooldown restartAllowedLocked, the gate kickGate,
// and the localAbort pattern is the per-try context of attemptOnce,
// driven through a running connection process.

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/exchange"
)

// TestPeerConnectionKickTiming is the describe "PeerTimingParameters"
// (the other defaults are TestParityTimingDefaults).
func TestPeerConnectionKickTiming(t *testing.T) {
	t.Parallel()
	d := DefaultTiming()
	t.Run("has correct default for kickMinRetransmissions", func(t *testing.T) {
		t.Parallel()
		if d.KickMinRetransmissions != 2 {
			t.Fatalf("kickMinRetransmissions = %d, want 2", d.KickMinRetransmissions)
		}
	})
	t.Run("allows overriding kick restart cooldowns", func(t *testing.T) {
		t.Parallel()
		custom := Merge(d, Timing{KickRestartCooldown: KickRestartCooldown{AddressChange: 5 * time.Minute, Connect: 30 * time.Second}})
		if custom.KickRestartCooldown.AddressChange != 5*time.Minute || custom.KickRestartCooldown.Connect != 30*time.Second {
			t.Fatalf("kickRestartCooldown = %+v", custom.KickRestartCooldown)
		}
	})
	t.Run("keeps the untouched members of a partially overridden group", func(t *testing.T) {
		t.Parallel()
		custom := Merge(d, Timing{
			KickRestartCooldown:        KickRestartCooldown{Connect: 30 * time.Second},
			AddressChangeProbeCooldown: ProbeCooldown{Min: 45 * time.Second},
		})
		if custom.KickRestartCooldown.Connect != 30*time.Second || custom.KickRestartCooldown.AddressChange != d.KickRestartCooldown.AddressChange {
			t.Fatalf("kickRestartCooldown = %+v", custom.KickRestartCooldown)
		}
		if custom.AddressChangeProbeCooldown.Min != 45*time.Second || custom.AddressChangeProbeCooldown.Max != d.AddressChangeProbeCooldown.Max {
			t.Fatalf("addressChangeProbeCooldown = %+v", custom.AddressChangeProbeCooldown)
		}
	})
	t.Run("keeps the untouched members of a group when merging onto a custom base", func(t *testing.T) {
		t.Parallel()
		base := Merge(d, Timing{KickRestartCooldown: KickRestartCooldown{AddressChange: 5 * time.Minute, Connect: 5 * time.Minute}})
		merged := Merge(base, Timing{KickRestartCooldown: KickRestartCooldown{Connect: 30 * time.Second}})
		if merged.KickRestartCooldown.Connect != 30*time.Second || merged.KickRestartCooldown.AddressChange != 5*time.Minute {
			t.Fatalf("kickRestartCooldown = %+v", merged.KickRestartCooldown)
		}
	})
}

// kickRecorder is a connection of a fresh peer with one kick handler that
// records the origins the throttle lets through.
func kickRecorder(t *testing.T) (*harness, *connection, *[]string) {
	t.Helper()
	h := newHarness(t, nil)
	p, _ := h.set.For(testAddr)
	received := &[]string{}
	c := &connection{p: p, timing: h.set.Timing(), kickHandlers: map[*attempt]func(kickOrigin){}}
	c.kickHandlers[&attempt{}] = func(o kickOrigin) { *received = append(*received, o.String()) }
	return h, c, received
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestPeerConnectionKickOrigin is the describe "KickOrigin through
// QuietObservable": the throttle of Peer.ts:641-648 (3 s,
// skipSuppressedEmits).
func TestPeerConnectionKickOrigin(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		origin kickOrigin
		want   string
	}{
		{"passes 'discover' origin to observer", kickDiscover, "discover"},
		{"passes 'connect' origin to observer", kickConnect, "connect"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, c, received := kickRecorder(t)
			c.kick(tc.origin)
			if !equalStrings(*received, []string{tc.want}) {
				t.Fatalf("received %v", *received)
			}
		})
	}
	t.Run("suppresses rapid-fire kicks within minimumEmitInterval", func(t *testing.T) {
		t.Parallel()
		_, c, received := kickRecorder(t)
		c.kick(kickDiscover) // accepted
		c.kick(kickDiscover) // suppressed
		c.kick(kickConnect)  // suppressed
		if !equalStrings(*received, []string{"discover"}) {
			t.Fatalf("received %v", *received)
		}
	})
	t.Run("allows kick after minimumEmitInterval has elapsed", func(t *testing.T) {
		t.Parallel()
		h, c, received := kickRecorder(t)
		c.kick(kickDiscover)
		h.clock.Advance(3*time.Second + time.Millisecond) // :152
		c.kick(kickConnect)
		if !equalStrings(*received, []string{"discover", "connect"}) {
			t.Fatalf("received %v", *received)
		}
	})
}

// restartRecorder is a connection of a fresh peer on a set with the given
// timing override; handle runs its restart cooldown and records the
// restarts it allows.
func restartRecorder(t *testing.T, over *Timing) (h *harness, handle func(kickOrigin) bool, restarts *[]string) {
	t.Helper()
	h = newHarness(t, func(c *Context) { c.Timing = over })
	p, _ := h.set.For(testAddr)
	c := &connection{p: p, setTiming: h.set.Timing()}
	restarts = &[]string{}
	handle = func(o kickOrigin) bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		ok := c.restartAllowedLocked(o)
		if ok {
			*restarts = append(*restarts, o.String())
		}
		return ok
	}
	return h, handle, restarts
}

// TestPeerConnectionKickRateLimiting is the describe "kick rate-limiting".
func TestPeerConnectionKickRateLimiting(t *testing.T) {
	t.Parallel()
	type step struct {
		advance time.Duration
		origin  kickOrigin
		want    bool
	}
	cases := []struct {
		name     string
		over     *Timing
		steps    []step
		restarts []string
	}{
		{"accepts the first kick unconditionally", nil, []step{{0, kickDiscover, true}}, []string{"discover"}},
		{
			"suppresses a second 'discover' kick within 30 minutes", nil,
			[]step{{0, kickDiscover, true}, {29 * time.Minute, kickDiscover, false}},
			[]string{"discover"},
		},
		{
			"accepts a second 'discover' kick after 30 minutes", nil,
			[]step{{0, kickDiscover, true}, {30 * time.Minute, kickDiscover, true}},
			[]string{"discover", "discover"},
		},
		{
			"suppresses a second 'connect' kick within 10 minutes", nil,
			[]step{{0, kickConnect, true}, {9 * time.Minute, kickConnect, false}},
			[]string{"connect"},
		},
		{
			"accepts a second 'connect' kick after 10 minutes", nil,
			[]step{{0, kickConnect, true}, {10 * time.Minute, kickConnect, true}},
			[]string{"connect", "connect"},
		},
		{"uses independent thresholds for different origins", nil, []step{ // :236-254
			{0, kickDiscover, true},
			{10 * time.Minute, kickConnect, true},
			{20 * time.Minute, kickDiscover, false},
			{10 * time.Minute, kickDiscover, true},
		}, []string{"discover", "connect", "discover"}},
		{
			"shares lastRestartAt across concurrent handlers", nil,
			[]step{{0, kickDiscover, true}, {0, kickDiscover, false}},
			[]string{"discover"},
		},
		{
			"allows custom thresholds via PeerTimingParameters override",
			&Timing{KickRestartCooldown: KickRestartCooldown{AddressChange: 60 * time.Second, Connect: 10 * time.Second}}, // :274-279
			[]step{{0, kickDiscover, true}, {30 * time.Second, kickDiscover, false}, {30 * time.Second, kickDiscover, true}},
			[]string{"discover", "discover"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h, handle, restarts := restartRecorder(t, tc.over)
			for i, s := range tc.steps {
				h.clock.Advance(s.advance)
				if got := handle(s.origin); got != s.want {
					t.Fatalf("step %d (%s): restart %v, want %v", i, s.origin, got, s.want)
				}
			}
			if !equalStrings(*restarts, tc.restarts) {
				t.Fatalf("restarts %v, want %v", *restarts, tc.restarts)
			}
		})
	}
}

// TestPeerConnectionKickGate is the describe "kick gate (count + saving)".
func TestPeerConnectionKickGate(t *testing.T) {
	t.Parallel()
	timing := DefaultTiming()
	for _, tc := range []struct {
		name   string
		count  int
		saving time.Duration
		want   bool
	}{
		{"suppresses while the exchange has not retransmitted enough", 1, 2 * time.Minute, false},
		{"suppresses when a restart would save too little time (e.g. idle/sleepy peer)", 5, 10 * time.Second, false},
		{"restarts once both retransmission count and restart saving clear their thresholds", 2, 90 * time.Second, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got, _ := kickGate(timing, tc.count, tc.saving); got != tc.want {
				t.Fatalf("gate(%d, %v) = %v, want %v", tc.count, tc.saving, got, tc.want)
			}
		})
	}
}

// kickableTry starts a process at addr1 and retransmits its Sigma1 until
// the kick gate opens; it returns the process, the attempt and the try's
// exchange.
func kickableTry(t *testing.T, h *harness, p *Peer) (*connection, *attempt, *exchange.Exchange) {
	t.Helper()
	h.sigma1(t, addr1)
	c := process(p)
	var att *attempt
	var x *exchange.Exchange
	eventually(t, "the try's exchange", func() bool {
		att = attemptAt(c, addr1)
		if att == nil {
			return false
		}
		x = att.exchange.Load()
		return x != nil && x.RetransmissionCount() == 0
	})
	for ok, _ := kickGate(DefaultTiming(), x.RetransmissionCount(), x.RetransmissionRestartSaving()); !ok; ok, _ = kickGate(DefaultTiming(), x.RetransmissionCount(), x.RetransmissionRestartSaving()) {
		h.clock.Advance(5 * time.Second)
	}
	return c, att, x
}

// ended waits for the exchange to end.
func ended(t *testing.T, x *exchange.Exchange, what string) {
	t.Helper()
	select {
	case <-x.Done():
	case <-time.After(10 * time.Second):
		t.Fatalf("%s: the try's exchange did not end", what)
	}
}

// TestPeerConnectionKickLocalAbort is the describe "localAbort restart
// pattern": the per-try context of attemptOnce under the attempt's
// context.
func TestPeerConnectionKickLocalAbort(t *testing.T) {
	t.Parallel()
	start := func(t *testing.T) (*harness, *Peer) {
		t.Helper()
		h := newHarness(t, nil)
		p, _ := h.set.AddKnownPeer(Descriptor{Address: testAddr, OperationalAddress: udpAddr(addr1)})
		connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
		return h, p
	}
	t.Run("firing localAbort causes AbortedError via Abort.attempt", func(t *testing.T) {
		t.Parallel()
		h, p := start(t)
		_, att, x := kickableTry(t, h, p)
		p.Kick()
		ended(t, x, "a restarting kick")
		// Only the try ended, not the attempt at the address.
		if att.ctx.Err() != nil {
			t.Fatal("the restart aborted the attempt")
		}
	})
	t.Run("addressAbort propagates to localAbort", func(t *testing.T) {
		t.Parallel()
		h, p := start(t)
		_, att, x := kickableTry(t, h, p)
		att.cancel()
		ended(t, x, "the attempt's abort")
	})
	t.Run("localAbort on already-aborted signal is harmless", func(t *testing.T) {
		t.Parallel()
		h, p := start(t)
		_, att, x := kickableTry(t, h, p)
		att.cancel()
		p.Kick() // may reach the aborted try's restart; must not panic
		ended(t, x, "the attempt's abort")
	})
	t.Run("connect loop restarts after localAbort fires", func(t *testing.T) {
		t.Parallel()
		h, p := start(t)
		c, att, x := kickableTry(t, h, p)
		ids := map[uint16]bool{x.ID(): true}
		for range 2 { // :366, three tries, the first two restarted
			p.Kick()
			ended(t, x, "a restarting kick")
			h.clock.Advance(DefaultTiming().KickRestartCooldown.Connect) // the next restart's cooldown
			h.sigma1(t, addr1)
			eventually(t, "the next try", func() bool {
				x = att.exchange.Load()
				return x != nil && !ids[x.ID()]
			})
			ids[x.ID()] = true
			for ok, _ := kickGate(DefaultTiming(), x.RetransmissionCount(), x.RetransmissionRestartSaving()); !ok; ok, _ = kickGate(DefaultTiming(), x.RetransmissionCount(), x.RetransmissionRestartSaving()) {
				h.clock.Advance(5 * time.Second)
			}
		}
		if len(ids) != 3 || att.ctx.Err() != nil || attemptAt(c, addr1) != att {
			t.Fatalf("%d tries on the attempt (aborted %v), want 3 on one live attempt", len(ids), att.ctx.Err() != nil)
		}
	})
}

// TestPeerConnectionKickObserver is the describe "Observable<[KickOrigin]>
// type contract": a try's kick handler is registered for the try only.
func TestPeerConnectionKickObserver(t *testing.T) {
	t.Parallel()
	t.Run("use() returns disposable that unregisters observer", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, nil)
		p, _ := h.set.AddKnownPeer(Descriptor{Address: testAddr, OperationalAddress: udpAddr(addr1)})
		connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
		c, att, x := kickableTry(t, h, p)
		registered := func() bool {
			c.mu.Lock()
			defer c.mu.Unlock()
			_, ok := c.kickHandlers[att]
			return ok
		}
		if !registered() {
			t.Fatal("the running try has no kick handler")
		}
		att.cancel()
		ended(t, x, "the attempt's abort")
		eventually(t, "the try's handler to be unregistered", func() bool { return !registered() })
	})
}
