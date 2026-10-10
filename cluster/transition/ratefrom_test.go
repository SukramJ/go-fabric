// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package transition_test

import (
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/cluster/transition"
)

// TestRateFromReadsTheValueTheEngineReads holds the fix for the race a
// caller had when it read the current value itself and then called Start:
// a step of the running transition could land between the two reads, so
// the rate came from the old value and RemainingTime from the new one.
// RateFrom is evaluated inside Start, under the engine's lock, with the
// value Read returns at that moment, as matter.js computes the rate and
// starts in one synchronous handler.
func TestRateFromReadsTheValueTheEngineReads(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var mu sync.Mutex
		value := 100.0
		var seen []float64
		e := transition.New(transition.Config{
			Manage:     true,
			Properties: map[string]transition.Property{"p": {Min: 0, Max: 1000}},
			Read: func(string) (float64, bool) {
				mu.Lock()
				defer mu.Unlock()
				return value, true
			},
			Apply: func(changes []transition.Change) error {
				mu.Lock()
				defer mu.Unlock()
				value = changes[0].Value
				return nil
			},
		})
		// 100 → 200 at 10 per second; after 5 s the value is near 150.
		if err := e.Start(transition.Transition{Name: "p", Rate: 10, Target: 200}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Second)
		synctest.Wait()
		// A second command towards 300 over 10 s: the rate must come from
		// the engine's own read (150), never from a value the caller read.
		if err := e.Start(transition.Transition{
			Name: "p", Target: 300,
			RateFrom: func(current float64) float64 {
				seen = append(seen, current)
				return (300 - current) / 10
			},
		}); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		now := value
		mu.Unlock()
		if len(seen) != 1 || seen[0] != now {
			t.Fatalf("RateFrom saw %v, want [%v], the value the engine holds", seen, now)
		}
		if now < 140 || now > 160 {
			t.Fatalf("value after 5 s = %v, want near 150", now)
		}
		// A rate of (300 - current) / 10 per second reaches 300 after 10 s
		// whatever current was, if and only if both came from the same read.
		if rt := e.RemainingTime(); rt != 100 {
			t.Fatalf("RemainingTime = %d tenths, want 100", rt)
		}
		e.CancelAll()
	})
}
