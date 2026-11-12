// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction

import (
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/icd"
)

// TestRetries: RetrySchedule.ts:28-60 over the sustain schedule — the base
// doubles, the jitter (floor of 0.25 × base × draw, in ms) is added, the
// sum is clamped to the hour; the zero configuration is 1 s, ×2.
func TestRetries(t *testing.T) {
	t.Parallel()
	r := newRetries(DefaultSustainRetries, func() float64 { return 0 })
	want := []time.Duration{15 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 32 * time.Minute, time.Hour, time.Hour}
	for i, w := range want {
		if got := r.next(); got != w {
			t.Fatalf("interval %d = %v, want %v", i, got, w)
		}
	}
	j := newRetries(DefaultSustainRetries, func() float64 { return 0.9999 })
	if got := j.next(); got != 15*time.Second+3749*time.Millisecond {
		t.Fatalf("jittered first interval %v", got)
	}
	z := newRetries(RetrySchedule{}, func() float64 { return 0.5 })
	if a, b := z.next(), z.next(); a != time.Second || b != 2*time.Second {
		t.Fatalf("defaults %v, %v", a, b)
	}
}

// feedInWindow is a wakefulness provider whose peer is registered in the
// window between an established attempt's hold decision and its wait: nil
// until an attempt has gone out, nil once more for the first read after it
// (the hold decision), the fed wakefulness from then on.
type feedInWindow struct {
	mu        sync.Mutex
	w         *icd.PeerWakefulness
	attempted bool
	consumed  bool
}

func (p *feedInWindow) attempt(int) error {
	p.mu.Lock()
	p.attempted = true
	p.mu.Unlock()
	return nil
}

func (p *feedInWindow) get() *icd.PeerWakefulness {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.attempted {
		return nil
	}
	if !p.consumed {
		p.consumed = true
		return nil
	}
	return p.w
}

// TestSustainedHoldsWakefulnessFedAfterEstablishment: a registration fed
// after the hold decision of an established attempt but before its wait
// recreates the subscription, which then holds the wakefulness — no
// Check-In is reported due beside an active subscription. Before the fix
// the wait read the wakefulness a second time and waited for a mode flip
// of a wakefulness the subscription never held (the race job's hang in
// controller/client TestIcdClientParity/availability).
func TestSustainedHoldsWakefulnessFedAfterEstablishment(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		p := &feedInWindow{}
		r := newICDRig(t, p.attempt)
		p.w = litWakefulness(r.fake)
		p.w.NoteActive()
		f := &feed{}
		r.start(p.get, f.on)
		settle()
		p.w.NoteActive()
		if due, ok := p.w.NextCheckInDue(); ok {
			t.Fatalf("Check-In due %v beside the subscription: the wakefulness fed in the window is not held", due)
		}
		r.wantAttempts(2)
		r.wantActive(true)
	})
}
