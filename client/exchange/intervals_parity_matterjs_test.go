// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange_test

// A port of matter.js packages/protocol/test/session/SessionIntervalsTest.ts
// at 862ad75f: the interval defaults and overrides (exchange.IntervalsOf, the
// SessionIntervals factory) and their clamping for advertisement (the
// mdns record builders, which apply SessionIntervals.forAdvertisement).

import (
	"strconv"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
)

const sessionIntervalsTwoHours = uint32(2 * time.Hour / time.Millisecond)

func TestSessionIntervals(t *testing.T) {
	t.Parallel()
	t.Run("applies defaults when nothing is provided", func(t *testing.T) {
		t.Parallel()
		want := exchange.Intervals{Idle: 500 * time.Millisecond, Active: 300 * time.Millisecond, ActiveThreshold: 4 * time.Second}
		if got := exchange.IntervalsOf(sessionparams.Parameters{}); got != want {
			t.Fatalf("IntervalsOf({}) = %+v, want %+v", got, want)
		}
	})
	t.Run("overrides only the provided intervals", func(t *testing.T) {
		t.Parallel()
		want := exchange.Intervals{Idle: 100 * time.Millisecond, Active: 300 * time.Millisecond, ActiveThreshold: 4 * time.Second}
		if got := exchange.IntervalsOf(sessionparams.Parameters{SessionIdleInterval: 100}); got != want {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	})
	t.Run("accepts an idle interval over one hour", func(t *testing.T) {
		t.Parallel()
		if got := exchange.IntervalsOf(sessionparams.Parameters{SessionIdleInterval: sessionIntervalsTwoHours}).Idle; got != 2*time.Hour {
			t.Fatalf("Idle = %v, want 2h", got)
		}
	})
	t.Run("accepts an active interval over one hour", func(t *testing.T) {
		t.Parallel()
		if got := exchange.IntervalsOf(sessionparams.Parameters{SessionActiveInterval: sessionIntervalsTwoHours}).Active; got != 2*time.Hour {
			t.Fatalf("Active = %v, want 2h", got)
		}
	})
}

// sessionIntervalsAdvertised is SessionIntervals.forAdvertisement through the
// operational record the device advertises: the configured SII, SAI and SAT
// (0 for "not provided") and the TXT values they become.
func sessionIntervalsAdvertised(t *testing.T, sii, sai, sat uint32) (gotSII, gotSAI, gotSAT uint64) {
	t.Helper()
	svc := mdns.BuildOperationalService(mdns.OperationalServiceConfig{
		SessionIdleInterval: sii, SessionActiveInterval: sai, SessionActiveThreshold: sat,
	})
	values := map[string]uint64{}
	for _, r := range svc.TXT {
		if r.Key == "SII" || r.Key == "SAI" || r.Key == "SAT" {
			v, err := strconv.ParseUint(r.Value, 10, 64)
			if err != nil {
				t.Fatalf("%s=%q: %v", r.Key, r.Value, err)
			}
			values[r.Key] = v
		}
	}
	return values["SII"], values["SAI"], values["SAT"]
}

func TestSessionIntervalsForAdvertisement(t *testing.T) {
	t.Parallel()
	const hour = 3_600_000
	t.Run("passes through values within the spec maxima", func(t *testing.T) {
		t.Parallel()
		if sii, sai, sat := sessionIntervalsAdvertised(t, hour, 0, 0); sii != hour || sai != 300 || sat != 4000 {
			t.Fatalf("SII/SAI/SAT = %d/%d/%d, want %d/300/4000", sii, sai, sat, hour)
		}
	})
	t.Run("clamps an idle interval over one hour to one hour", func(t *testing.T) {
		t.Parallel()
		if sii, _, _ := sessionIntervalsAdvertised(t, sessionIntervalsTwoHours, 0, 0); sii != hour {
			t.Fatalf("SII = %d, want %d", sii, hour)
		}
	})
	t.Run("clamps an active interval over one hour to one hour", func(t *testing.T) {
		t.Parallel()
		if _, sai, _ := sessionIntervalsAdvertised(t, 0, sessionIntervalsTwoHours, 0); sai != hour {
			t.Fatalf("SAI = %d, want %d", sai, hour)
		}
	})
	t.Run("clamps an active threshold over 65535 milliseconds", func(t *testing.T) {
		t.Parallel()
		if _, _, sat := sessionIntervalsAdvertised(t, 0, 0, 65536); sat != 65535 {
			t.Fatalf("SAT = %d, want 65535", sat)
		}
	})
	t.Run("accepts an active threshold of exactly 65535 milliseconds", func(t *testing.T) {
		t.Parallel()
		if _, _, sat := sessionIntervalsAdvertised(t, 0, 0, 65535); sat != 65535 {
			t.Fatalf("SAT = %d, want 65535", sat)
		}
	})
}
