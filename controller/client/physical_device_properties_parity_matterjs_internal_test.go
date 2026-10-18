// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/protocol/test/peer/PhysicalDevicePropertiesTest.ts
// at 862ad75f: the subscription interval bounds the controller derives from
// a peer's physical properties (PhysicalDeviceProperties.subscriptionIntervalBoundsFor;
// the client's intervalBounds over a Properties value). The Go subject of
// this protocol test lives in controller/client, so it is ported here. A
// matter.js call without properties is the zero Properties; the jitter is
// math/rand's, as matter.js's crypto randomness.
package client

import (
	"log/slog"
	"math"
	"math/rand/v2"
	"testing"
	"time"
)

// baseProperties is PhysicalDevicePropertiesTest.ts:15-25 BASE_PROPERTIES:
// a mains-powered Wi-Fi device.
func baseProperties() Properties {
	return Properties{SupportsWifi: true, MainsPowered: true}
}

const spec13 = 0x0103_0000

// threadPre13 is PhysicalDevicePropertiesTest.ts:37-42 THREAD_PRE_1_3.
func threadPre13() Properties {
	p := baseProperties()
	p.SupportsWifi, p.SupportsThread = false, true
	active := true
	p.ThreadActive = &active
	return p
}

// bounds is subscriptionIntervalBoundsFor({ properties, request }).
func bounds(p Properties, reqFloor, reqCeiling *time.Duration) (time.Duration, time.Duration) {
	return intervalBounds(p, reqFloor, reqCeiling, rand.Float64, slog.New(slog.DiscardHandler))
}

// expectJittered is PhysicalDevicePropertiesTest.ts:128-133: up to
// max(10 %, 10 s) one-sided jitter, floored to whole seconds.
func expectJittered(t *testing.T, actual time.Duration, baseSeconds float64) {
	t.Helper()
	window := math.Max(baseSeconds*0.1, 10)
	if actual < time.Duration(baseSeconds)*time.Second || actual > time.Duration(math.Floor(baseSeconds+window))*time.Second {
		t.Fatalf("ceiling %v, want %v s within its jitter", actual, baseSeconds)
	}
}

func TestPhysicalDevicePropertiesParity(t *testing.T) {
	t.Parallel()
	t.Run("defaults to false in BASE_PROPERTIES", func(t *testing.T) {
		t.Parallel()
		if baseProperties().LongIdleTimeOperating {
			t.Fatal("LongIdleTimeOperating set")
		}
	})
}

func TestPhysicalDevicePropertiesMinIntervalFloorParity(t *testing.T) {
	t.Parallel()
	floorOf := func(p Properties, req *time.Duration) time.Duration {
		f, _ := bounds(p, req, nil)
		return f
	}
	icd := func() Properties { p := baseProperties(); p.IntermittentlyConnected = true; return p }
	for _, c := range []struct {
		name string
		p    Properties
		req  *time.Duration
		want time.Duration
	}{
		{"defaults to Instant (0) when called with no arguments", Properties{}, nil, 0},
		{"defaults to Instant (0) for a non-Thread device with no floor requested", baseProperties(), nil, 0},
		{"uses 1 second for a pre-1.3 Thread device with no floor requested", threadPre13(), nil, time.Second},
		{"uses 1 second for a Thread device that only supports Thread but is not yet active", func() Properties {
			p := baseProperties()
			p.SupportsWifi, p.SupportsThread = false, true
			return p
		}(), nil, time.Second},
		{"uses Instant (0) for a Thread device on Matter 1.3 or later", func() Properties {
			p := threadPre13()
			v := uint32(spec13)
			p.SpecificationVersion = &v
			return p
		}(), nil, 0},
		{"uses Instant (0) for a pre-1.3 Thread device that has a Generic Switch endpoint", func() Properties {
			p := threadPre13()
			p.DeviceTypes = []uint32{genericSwitchDeviceType}
			return p
		}(), nil, 0},
		{"respects a custom floor requested for a non-ICD device", baseProperties(), dur(30 * time.Second), 30 * time.Second},
		{"is always Instant (0) for an ICD device even when no floor is requested", icd(), nil, 0},
		{"overwrites a non-zero requested floor to Instant (0) for an ICD device", icd(), dur(30 * time.Second), 0},
		{"keeps Instant (0) for an ICD device when the requested floor is already Instant", icd(), dur(0), 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := floorOf(c.p, c.req); got != c.want {
				t.Fatalf("floor %v, want %v", got, c.want)
			}
		})
	}
}

func TestPhysicalDevicePropertiesMaxIntervalCeilingParity(t *testing.T) {
	t.Parallel()
	ceilingOf := func(p Properties, reqFloor, reqCeiling *time.Duration) time.Duration {
		_, c := bounds(p, reqFloor, reqCeiling)
		return c
	}
	with := func(f func(p *Properties)) Properties { p := baseProperties(); f(&p); return p }
	inactive, active := false, true
	for _, c := range []struct {
		name string
		p    Properties
		base float64
	}{
		{"defaults to 1 minute with no properties", Properties{}, 60},
		{"uses 1 minute for a WiFi device", with(func(p *Properties) { p.SupportsWifi = true }), 60},
		{"uses 1 minute for a Thread device that is not sleepy", with(func(p *Properties) { p.SupportsThread = true }), 60},
		{"uses 3 minutes for a Thread sleepy end device", with(func(p *Properties) { p.SupportsThread, p.ThreadSleepyEndDevice = true, true }), 180},
		{"uses 10 minutes for a battery-powered device", with(func(p *Properties) { p.BatteryPowered, p.MainsPowered = true, false }), 600},
		{"uses non-battery ceiling when device is both battery and mains powered", with(func(p *Properties) { p.BatteryPowered = true }), 60},
		{"applies jitter regardless of network type", with(func(p *Properties) { p.SupportsThread, p.ThreadActive = true, &active }), 60},
		{"applies jitter when Thread is not active", with(func(p *Properties) { p.SupportsThread, p.ThreadActive = true, &inactive }), 60},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			expectJittered(t, ceilingOf(c.p, nil, nil), c.base)
		})
	}

	t.Run("passes an explicitly requested ceiling through unchanged, with no jitter", func(t *testing.T) {
		t.Parallel()
		for range 200 {
			if got := ceilingOf(Properties{}, nil, dur(80*time.Second)); got != 80*time.Second {
				t.Fatalf("ceiling %v, want 80 s", got)
			}
		}
	})

	t.Run("still derives jitter when the caller omits a ceiling", func(t *testing.T) {
		t.Parallel()
		observed := map[time.Duration]bool{}
		for range 200 {
			c := ceilingOf(Properties{}, nil, nil)
			expectJittered(t, c, 60)
			observed[c] = true
		}
		if len(observed) <= 1 {
			t.Fatalf("one ceiling %v in 200 draws: no jitter", observed)
		}
	})

	t.Run("clamps an explicitly requested ceiling below the floor up to the floor", func(t *testing.T) {
		t.Parallel()
		if got := ceilingOf(Properties{}, dur(30*time.Second), dur(10*time.Second)); got != 30*time.Second {
			t.Fatalf("ceiling %v, want 30 s", got)
		}
	})

	t.Run("applies jitter for an ICD device while keeping the Instant floor", func(t *testing.T) {
		t.Parallel()
		f, c := bounds(with(func(p *Properties) { p.IntermittentlyConnected = true }), nil, nil)
		if f != 0 {
			t.Fatalf("floor %v, want 0", f)
		}
		expectJittered(t, c, 60)
	})
}

func TestPhysicalDevicePropertiesICDCeilingParity(t *testing.T) {
	t.Parallel()
	icd := func(idle *time.Duration, f func(p *Properties)) Properties {
		p := baseProperties()
		p.IntermittentlyConnected, p.IdleModeDuration = true, idle
		if f != nil {
			f(&p)
		}
		return p
	}
	for _, c := range []struct {
		name string
		p    Properties
		req  *time.Duration
		want time.Duration
	}{
		{"uses idleModeDuration exactly as the ceiling for an ICD peer, with no jitter", icd(dur(60*time.Second), nil), nil, 60 * time.Second},
		{"scales the ceiling to a long idleModeDuration, with no jitter", icd(dur(60*time.Minute), nil), nil, 3600 * time.Second},
		{"prefers idleModeDuration over the battery ceiling for a battery-powered ICD, with no jitter", icd(dur(60*time.Second), func(p *Properties) {
			p.BatteryPowered, p.MainsPowered = true, false
		}), nil, 60 * time.Second},
		{"clamps a huge idleModeDuration to the uint16 wire max, with no jitter", icd(dur(100_000*time.Second), nil), nil, math.MaxUint16 * time.Second},
		{"passes an explicitly requested ceiling through unchanged for an ICD peer, with no jitter", icd(dur(60*time.Second), nil), dur(45 * time.Second), 45 * time.Second},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if _, got := bounds(c.p, nil, c.req); got != c.want {
				t.Fatalf("ceiling %v, want %v", got, c.want)
			}
		})
	}
	t.Run("falls back to the generic ICD ceiling when idleModeDuration is unknown", func(t *testing.T) {
		t.Parallel()
		_, c := bounds(icd(nil, nil), nil, nil)
		expectJittered(t, c, 60)
	})
}
