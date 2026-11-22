// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package icd_test

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/icd"
)

// TestPeerWakefulnessDeliversInChangeOrder holds R2-P6-conc-0208: when a
// timer's expiry (available → false) and a NoteActive (→ true) change the
// state concurrently, the observers get the two in the order the changes
// happened, so the value an observer saw last is the state's. The
// observer of the expiry is held inside its callback while NoteActive
// runs on another goroutine; before the fix NoteActive delivered at once
// and the held false landed last.
func TestPeerWakefulnessDeliversInChangeOrder(t *testing.T) {
	t.Parallel()
	w, fake, _ := litPeer()
	w.NoteActive()
	if !w.Available() {
		t.Fatal("not available after activity")
	}
	var (
		mu       sync.Mutex
		last     bool
		seen     []bool
		held     = make(chan struct{})
		release  = make(chan struct{})
		holdOnce sync.Once
	)
	w.OnAvailableChanged(func(v bool) {
		if !v {
			holdOnce.Do(func() {
				close(held)
				<-release
			})
		}
		mu.Lock()
		last = v
		seen = append(seen, v)
		mu.Unlock()
	})
	advanced := make(chan struct{})
	go func() {
		defer close(advanced)
		fake.Advance(checkInDue + time.Millisecond)
	}()
	<-held
	noted := make(chan struct{})
	go func() {
		defer close(noted)
		w.NoteActive()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	select {
	case <-noted:
	case <-ctx.Done():
		t.Fatal("NoteActive blocked behind an observer's callback")
	}
	close(release)
	<-advanced
	mu.Lock()
	defer mu.Unlock()
	if last != w.Available() {
		t.Fatalf("observer saw %v last (all: %v), state is available=%v", last, seen, w.Available())
	}
}

// TestPeerWakefulnessSetOperatingModeIsOneStep holds F-b25ed960-C6: a live
// SIT→LIT flip with activity is one step, as matter.js's IcdClient sets
// requiresAwait and notes the peer active with no await between. A
// goroutine parked on ModeSignal — a sustained subscription's recreate —
// reads Awake when it wakes and must find the peer awake; observers still
// get matter.js's order (awake false, mode, awake true).
func TestPeerWakefulnessSetOperatingModeIsOneStep(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		w := icd.NewPeerWakefulness(clocktest.New(modeEpoch))
		w.SetTimings(wakeTimings)
		var (
			mu  sync.Mutex
			evs []string
		)
		w.OnAwakeChanged(func(v bool) { mu.Lock(); evs = append(evs, fmt.Sprintf("awake %v", v)); mu.Unlock() })
		w.OnOperatingModeChanged(func(v bool) { mu.Lock(); evs = append(evs, fmt.Sprintf("mode %v", v)); mu.Unlock() })
		edge := w.ModeSignal()
		seen := make(chan bool, 1)
		go func() {
			<-edge
			seen <- w.Awake()
		}()
		synctest.Wait()
		w.SetOperatingMode(true, true)
		if !<-seen {
			t.Fatal("the recreate woken by the flip found the peer asleep")
		}
		mu.Lock()
		defer mu.Unlock()
		if want := []string{"awake false", "mode true", "awake true"}; !slices.Equal(evs, want) {
			t.Fatalf("events %v, want %v", evs, want)
		}
	})
}
