// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange

// The MRP margin cases of matter.js at 862ad75f:
// packages/protocol/test/protocol/MRPTest.ts (marginFor, the padded
// retransmission interval, its maximum, the padded return leg) and
// packages/protocol/test/protocol/MessageExchangeTest.ts ("MRP backoff
// margin" :685-841, the padded BDX cap :404-496, the closing notification
// :562-575). matter.js captures the additionalDelay the exchange hands the
// channel; Go reads the exchange's own sendAdditionalDelay, the same value.

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/secure/sessionparams"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

const imProtocolID uint16 = 0x0001

func TestMRPMarginFor(t *testing.T) {
	t.Parallel()
	m := &Margins{Messaging: 1500 * time.Millisecond, BDX: 5 * time.Second}
	t.Run("selects the bdx margin for bulk transfer", func(t *testing.T) {
		if d, ok := MarginFor(m, bdxProtocolID); !ok || d != 5*time.Second {
			t.Errorf("%v %v", d, ok)
		}
	})
	t.Run("selects the messaging margin for other protocols", func(t *testing.T) {
		for _, p := range []uint16{imProtocolID, mrp.SecureChannelProtocolID} {
			if d, ok := MarginFor(m, p); !ok || d != 1500*time.Millisecond {
				t.Errorf("protocol %d: %v %v", p, d, ok)
			}
		}
	})
	t.Run("has no margin to offer when the peer's medium is unknown", func(t *testing.T) {
		if _, ok := MarginFor(nil, bdxProtocolID); ok {
			t.Error("a margin for nil margins")
		}
	})
}

// mrpRange is runtimeRangeFor (MRPTest.ts:41-52): the jitter window of the
// padded backoff.
func mrpRange(base time.Duration, n int, pad Pad) (lo, hi time.Duration) {
	amp := float64((base + pad.Additional).Milliseconds()) * 1.1 * math.Pow(1.6, math.Max(0, float64(n-1)))
	return time.Duration(math.Floor(amp))*time.Millisecond + pad.Fixed, time.Duration(math.Floor(amp*1.25))*time.Millisecond + pad.Fixed
}

func TestMRPRetransmissionIntervalPadded(t *testing.T) {
	t.Parallel()
	add := Pad{Additional: 1500 * time.Millisecond}
	for _, c := range []struct {
		name   string
		n      int
		active bool
		base   time.Duration
	}{
		{"uses the active interval for the first transmission when the peer is active", 0, true, mrpParams.Active},
		{"uses the idle interval for the first transmission when the peer is inactive", 0, false, mrpParams.Idle},
		{"uses the active interval for retransmissions while the peer is active", 1, true, mrpParams.Active},
		{"uses the idle interval for retransmissions when the peer is no longer active", 1, false, mrpParams.Idle},
	} {
		t.Run(c.name, func(t *testing.T) {
			lo, hi := mrpRange(c.base, c.n, add)
			for _, r := range []float64{0, 0.5, 0.999} {
				if d := RetransmissionIntervalPadded(c.n, mrpParams, c.active, r, add); d < lo || d > hi {
					t.Errorf("rand %v: %v not in [%v, %v]", r, d, lo, hi)
				}
			}
		})
	}
	t.Run("adds fixedBackoff as a flat pad after the backoff, not amplified by the exponent", func(t *testing.T) {
		// MRPTest.ts:113-126: transmission 2 of an active peer.
		fixed := Pad{Fixed: 200 * time.Millisecond}
		with := RetransmissionIntervalPadded(2, mrpParams, true, 0.5, fixed)
		without := RetransmissionIntervalPadded(2, mrpParams, true, 0.5, Pad{})
		if with-without > 200*time.Millisecond || with-without < 199*time.Millisecond {
			t.Errorf("fixed pad added %v, want 200ms", with-without)
		}
	})
	t.Run("applies additionalDelay (amplified) and fixedBackoff (flat) independently", func(t *testing.T) {
		both := Pad{Additional: 1500 * time.Millisecond, Fixed: 200 * time.Millisecond}
		lo, hi := mrpRange(mrpParams.Active, 1, both)
		if d := RetransmissionIntervalPadded(1, mrpParams, true, 0.5, both); d < lo || d > hi {
			t.Errorf("%v not in [%v, %v]", d, lo, hi)
		}
	})
	t.Run("includes additionalDelay (amplified) and fixedBackoff (flat) so the maximum upper-bounds real sends", func(t *testing.T) {
		// MRPTest.ts:196-209.
		pad := Pad{Additional: 1500 * time.Millisecond, Fixed: 200 * time.Millisecond}
		want := time.Duration(math.Floor(float64((mrpParams.Idle+pad.Additional).Milliseconds())*1.1*1.25))*time.Millisecond + pad.Fixed
		if d := RetransmissionIntervalPadded(1, mrpParams, false, 1, pad); d != want {
			t.Errorf("%v, want %v", d, want)
		}
		if RetransmissionIntervalPadded(1, mrpParams, false, 1, pad) <= RetransmissionInterval(1, mrpParams, false, 1) {
			t.Error("the padded maximum is not above the bare one")
		}
	})
	t.Run("adds our sender-side fixedBackoff to the local return leg only", func(t *testing.T) {
		// MRPTest.ts:288-303.
		base := MaxPeerResponseTimePadded(nil, true, mrpDefaults, DefaultExpectedProcessingTime, Pad{})
		with := MaxPeerResponseTimePadded(nil, true, mrpDefaults, DefaultExpectedProcessingTime, Pad{Fixed: 200 * time.Millisecond})
		if with-base != MaxTransmissions*200*time.Millisecond {
			t.Errorf("pad added %v, want %v", with-base, MaxTransmissions*200*time.Millisecond)
		}
		if base != MaxPeerResponseTime(nil, true, mrpDefaults, DefaultExpectedProcessingTime) {
			t.Error("a zero pad changed the response time")
		}
	})
}

// marginExchange is captureAdditionalDelay's exchange (MessageExchangeTest.ts:699-748):
// local margin, optional session peer margins, optional explicit margin,
// initiated or peer-initiated, on protocol.
func marginExchange(t *testing.T, local time.Duration, session *Margins, explicit *time.Duration, peerInitiated bool, protocol uint16) *Exchange {
	t.Helper()
	e := mxNew(t, false, nil)
	e.m.localAdditional = local
	if session != nil {
		m := *session
		e.s.SetPeerMargins(func() *Margins { return &m })
	}
	opts := Options{PeerAdditionalMrpDelay: explicit}
	if peerInitiated {
		return newExchange(e.m, e.s, 7, protocol, false, opts)
	}
	x, err := e.m.Initiate(e.s, protocol, opts)
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func TestMessageExchangeMRPBackoffMargin(t *testing.T) {
	t.Parallel()
	sec := func(f float64) time.Duration { return time.Duration(f * float64(time.Second)) }
	ptr := func(d time.Duration) *time.Duration { return &d }
	even := &Margins{Messaging: sec(1.5), BDX: sec(1.5)}
	split := &Margins{Messaging: sec(1.5), BDX: sec(5)}
	for _, c := range []struct {
		name          string
		local         time.Duration
		session       *Margins
		explicit      *time.Duration
		peerInitiated bool
		protocol      uint16
		want          time.Duration
	}{
		// The throttle profile is not the exchange's: Go's exchange never
		// sees it (the provider takes its slot), so "ignores the throttle
		// profile margin" holds by construction.
		{"uses peerAdditionalMrpDelay and ignores the throttle profile margin", 0, nil, ptr(sec(1.5)), false, mrp.SecureChannelProtocolID, sec(1.5)},
		{"falls back to localAdditionalMrpDelay as a floor when no peer margin is given", sec(1.5), nil, nil, false, mrp.SecureChannelProtocolID, sec(1.5)},
		{"applies no margin when neither local nor peer margin is set", 0, nil, nil, false, mrp.SecureChannelProtocolID, 0},
		{"applies the session's peer margin to peer-initiated exchanges", 0, even, nil, true, mrp.SecureChannelProtocolID, sec(1.5)},
		{"applies the session's peer margin to initiated exchanges without an explicit margin", 0, even, nil, false, mrp.SecureChannelProtocolID, sec(1.5)},
		{"prefers the explicit peer margin over the session's", 0, even, ptr(0), false, mrp.SecureChannelProtocolID, 0},
		{"selects the BDX margin for BDX exchanges, in either direction (initiated)", 0, split, nil, false, bdxProtocolID, sec(5)},
		{"selects the BDX margin for BDX exchanges, in either direction (peer-initiated)", 0, split, nil, true, bdxProtocolID, sec(5)},
		{"selects the messaging margin for other protocols", 0, split, nil, false, mrp.SecureChannelProtocolID, sec(1.5)},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			x := marginExchange(t, c.local, c.session, c.explicit, c.peerInitiated, c.protocol)
			if got := x.sendAdditionalDelay(); got != c.want {
				t.Errorf("additional delay %v, want %v", got, c.want)
			}
		})
	}
	t.Run("passes localFixedMrpBackoff through as the fixed backoff pad, separate from additionalDelay", func(t *testing.T) {
		t.Parallel()
		x := marginExchange(t, 0, nil, nil, false, mrp.SecureChannelProtocolID)
		x.m.localFixed = 200 * time.Millisecond
		if pad := x.sendPad(); pad.Fixed != 200*time.Millisecond || pad.Additional != 0 {
			t.Errorf("pad %+v", pad)
		}
	})
	t.Run("pads the scheduled backoff", func(t *testing.T) {
		t.Parallel()
		x := marginExchange(t, 0, nil, ptr(sec(1.5)), false, mrp.SecureChannelProtocolID)
		iv, active := x.session.timing()
		x.mu.Lock()
		got := x.backoffLocked(0)
		x.mu.Unlock()
		if want := RetransmissionIntervalPadded(0, iv, active, 0.5, Pad{Additional: sec(1.5)}); got != want {
			t.Errorf("backoff %v, want %v", got, want)
		}
	})
}

// paddedRetransmissionsWithin is MessageExchangeTest.ts:414-466 with the
// 20 s local margin of PADDED (:470) on a FAST_PEER (:471).
func paddedRetransmissionsWithin(t *testing.T, protocol uint16, window time.Duration) int {
	t.Helper()
	fast := &sessionparams.Parameters{SessionActiveInterval: 300, SessionIdleInterval: 500, SessionActiveThreshold: 4000}
	e := mxNew(t, false, fast)
	e.m.localAdditional = 20 * time.Second
	x := e.exchange(t, protocol)
	done := sendAsync(x, 1, nil, SendOptions{})
	e.conn.waitSent(t, 1)
	for elapsed := time.Duration(0); elapsed < window; elapsed += 500 * time.Millisecond {
		e.clock.Advance(500 * time.Millisecond)
	}
	sends, _ := e.counts(t)
	x.onMessageReceived(acking(inbound(x, 11, protocol, 1), e.firstCounter(t)), false)
	_ = waitErr(t, done)
	return sends - 1
}

func TestMessageExchangePaddedBDXCap(t *testing.T) {
	t.Parallel()
	t.Run("retransmits a BDX message within the peer's response budget", func(t *testing.T) {
		t.Parallel()
		if r := paddedRetransmissionsWithin(t, bdxProtocolID, 8*time.Second); r != 1 {
			t.Errorf("retransmissions = %d, want 1", r)
		}
	})
	t.Run("leaves a non-BDX schedule uncapped", func(t *testing.T) {
		t.Parallel()
		if r := paddedRetransmissionsWithin(t, mrp.SecureChannelProtocolID, 8*time.Second); r != 0 {
			t.Errorf("retransmissions = %d, want 0", r)
		}
	})
}

// TestMessageExchangeClosingNotification is MessageExchangeTest.ts:562-575:
// the closing hook (Options.OnClosing) runs once however often the
// exchange is closed.
func TestMessageExchangeClosingNotification(t *testing.T) {
	t.Parallel()
	t.Run("emits closing once when close is invoked again after the exchange completed its close", func(t *testing.T) {
		t.Parallel()
		e := mxNew(t, false, nil)
		n := 0
		x, err := e.m.Initiate(e.s, mrp.SecureChannelProtocolID, Options{OnClosing: func() { n++ }})
		if err != nil {
			t.Fatal(err)
		}
		if err := x.Send(context.Background(), 0, nil, SendOptions{SuppressAck: true, DisableMRP: true}); err != nil {
			t.Fatal(err)
		}
		x.Close()
		<-x.Done()
		x.Close()
		if n != 1 {
			t.Errorf("closing ran %d times, want 1", n)
		}
	})
}
