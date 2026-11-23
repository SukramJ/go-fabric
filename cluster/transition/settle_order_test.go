// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package transition_test

import (
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"thingfabric.dev/go-fabric/cluster/transition"
)

// TestReplaceSettlesAfterTheImmediateValue (R2-C2-fsm-1401): an immediate
// command that replaces a running transition of the same property settles
// the replaced name only after its own value is applied, so a Settled
// hook that flushes the attribute (the servers' Quieter.EmitNow) puts the
// command's value on the wire, not the stopped transition's last step.
func TestReplaceSettlesAfterTheImmediateValue(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			mu      sync.Mutex
			value   = 1.0
			atSettl []float64
		)
		e := transition.New(transition.Config{
			Manage:     true,
			Properties: level,
			Read: func(string) (float64, bool) {
				mu.Lock()
				defer mu.Unlock()
				return value, true
			},
			Apply: func(cs []transition.Change) error {
				mu.Lock()
				defer mu.Unlock()
				for _, c := range cs {
					value = c.Value
				}
				return nil
			},
			Settled: func(string) {
				mu.Lock()
				defer mu.Unlock()
				atSettl = append(atSettl, value)
			},
		})
		if err := e.Start(transition.Transition{Name: "level", Rate: 253.0 / 15, Target: 254}); err != nil {
			t.Fatal(err)
		}
		sleep(time.Second)
		if err := e.Start(transition.Transition{Name: "level", Target: 100}); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		defer mu.Unlock()
		if len(atSettl) != 1 || atSettl[0] != 100 {
			t.Errorf("value at Settled %v, want [100] (the command's value, applied first)", atSettl)
		}
	})
}
