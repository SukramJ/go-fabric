// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/protocol/test/icd/IcdCheckInBackOffTest.ts at
// 862ad75f: the doubling gap and its cap, the cap of one cycle, and the three
// resets. matter.js's string keys are ClientKeys.
package icd_test

import (
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/icd"
)

var (
	keyA = icd.ClientKey{FabricIndex: 1, CheckInNodeID: 0xa}
	keyB = icd.ClientKey{FabricIndex: 1, CheckInNodeID: 0xb}
)

// sendCycles is IcdCheckInBackOffTest.ts:10-20.
func sendCycles(b icd.CheckInBackOff, k icd.ClientKey, cycles int) []int {
	var out []int
	for c := range cycles {
		if b.ShouldSend(k) {
			out = append(out, c)
			b.RecordSent(k)
		}
	}
	return out
}

func TestDoublingCheckInBackOff(t *testing.T) {
	t.Parallel()
	t.Run("doubles the gap, capped at maximumCheckInBackoff", func(t *testing.T) {
		t.Parallel()
		b := icd.NewDoublingBackOff(60*time.Second, 960*time.Second) // maxCycles = 16
		if got := sendCycles(b, keyA, 64); !slices.Equal(got, []int{0, 1, 3, 7, 15, 31, 47, 63}) {
			t.Fatal(got)
		}
	})
	t.Run("never backs off when maximumCheckInBackoff equals idleModeDuration", func(t *testing.T) {
		t.Parallel()
		b := icd.NewDoublingBackOff(60*time.Second, 60*time.Second) // maxCycles = 1
		if got := sendCycles(b, keyA, 5); !slices.Equal(got, []int{0, 1, 2, 3, 4}) {
			t.Fatal(got)
		}
	})
	t.Run("recordAnswered resets to immediate", func(t *testing.T) {
		t.Parallel()
		b := icd.NewDoublingBackOff(60*time.Second, 960*time.Second)
		b.ShouldSend(keyA)
		b.RecordSent(keyA)
		b.ShouldSend(keyA)
		b.RecordSent(keyA) // waiting 2 cycles
		b.RecordAnswered(keyA)
		if !b.ShouldSend(keyA) {
			t.Fatal("not immediate")
		}
	})
	t.Run("resetAll resets every client", func(t *testing.T) {
		t.Parallel()
		b := icd.NewDoublingBackOff(60*time.Second, 960*time.Second)
		for _, k := range []icd.ClientKey{keyA, keyB} {
			b.ShouldSend(k)
			b.RecordSent(k)
			b.ShouldSend(k)
			b.RecordSent(k)
		}
		b.ResetAll()
		if !b.ShouldSend(keyA) || !b.ShouldSend(keyB) {
			t.Fatal("not reset")
		}
	})
	t.Run("forget drops a client's state", func(t *testing.T) {
		t.Parallel()
		b := icd.NewDoublingBackOff(60*time.Second, 960*time.Second)
		b.ShouldSend(keyA)
		b.RecordSent(keyA)
		b.Forget(keyA)
		if !b.ShouldSend(keyA) {
			t.Fatal("state kept")
		}
	})
}
