// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package transition_test

import (
	"testing"

	"thingfabric.dev/go-fabric/cluster/transition"
)

// TestTargetFromReadsTheValueTheEngineReads holds decision 164
// (R2-P6-conc-1407, step half): TargetFrom, the twin of RateFrom, derives
// the target from the value Read returns inside Start, so a step command
// reads and starts in one step; with the value unknown the transition
// keeps Target.
func TestTargetFromReadsTheValueTheEngineReads(t *testing.T) {
	value, known := 150.0, true
	var seen []float64
	e := transition.New(transition.Config{
		Properties: map[string]transition.Property{"p": {Min: 0, Max: 1000}},
		Read:       func(string) (float64, bool) { return value, known },
		Apply: func(changes []transition.Change) error {
			value = changes[0].Value
			return nil
		},
	})
	step := func(current float64) float64 { seen = append(seen, current); return current + 10 }
	if err := e.Start(transition.Transition{Name: "p", Target: 40, TargetFrom: step}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0] != 150 || value != 160 {
		t.Errorf("TargetFrom saw %v and the value is %v, want [150] and 160", seen, value)
	}
	known = false
	if err := e.Start(transition.Transition{Name: "p", Target: 40, TargetFrom: step}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || value != 40 {
		t.Errorf("an unknown value: TargetFrom saw %v and the value is %v, want no call and Target 40", seen, value)
	}
}
