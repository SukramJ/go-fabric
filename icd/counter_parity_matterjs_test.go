// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/protocol/test/icd/IcdCounterTest.ts at
// 862ad75f: the boot bump, the change callback, the wrap at 2^32 and the
// refused advance. matter.js's ImplementationError is ErrCounterAdvance; the
// fractional and NaN advances of the refusal case are not representable in
// the Go signature (an int64), so the case holds the two ends of the range.
package icd_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/icd"
)

// bootBump derives the bump from a zero seed, as IcdCounterTest.ts:10-13 does.
func bootBump() uint32 { return icd.NewCounter(0).Value() }

func TestIcdCounter(t *testing.T) {
	t.Parallel()
	t.Run("advances the seed by a constant boot bump", func(t *testing.T) {
		t.Parallel()
		if got := icd.NewCounter(500).Value(); got != 500+bootBump() {
			t.Fatalf("value %d", got)
		}
	})
	t.Run("applies a positive boot bump", func(t *testing.T) {
		t.Parallel()
		if got := icd.NewCounter(10).Value(); got <= 10 {
			t.Fatalf("value %d", got)
		}
	})
	t.Run("emits the new value on increment", func(t *testing.T) {
		t.Parallel()
		c := icd.NewCounter(0)
		base := c.Value()
		var seen []uint32
		c.OnChange(func(v uint32) { seen = append(seen, v) })
		if got := c.Increment(); got != base+1 {
			t.Fatalf("first %d", got)
		}
		if got := c.Increment(); got != base+2 {
			t.Fatalf("second %d", got)
		}
		if !slices.Equal(seen, []uint32{base + 1, base + 2}) {
			t.Fatalf("seen %v", seen)
		}
	})
	t.Run("wraps at 2^32 (uint32 semantics)", func(t *testing.T) {
		t.Parallel()
		c := icd.NewCounter(0xffffffff - bootBump())
		if c.Value() != 0xffffffff {
			t.Fatalf("value %#x", c.Value())
		}
		if got := c.Increment(); got != 0 {
			t.Fatalf("increment %d", got)
		}
	})
	t.Run("advances by an arbitrary amount with wrap-around and emits the new value", func(t *testing.T) {
		t.Parallel()
		c := icd.NewCounter(0)
		base := c.Value()
		var seen []uint32
		c.OnChange(func(v uint32) { seen = append(seen, v) })
		first, err := c.Advance(0x7fffffff)
		if err != nil || first != base+0x7fffffff {
			t.Fatalf("first %d %v", first, err)
		}
		second, err := c.Advance(0xffffffff)
		if err != nil || second != base+0x7fffffff-1 {
			t.Fatalf("second %d %v", second, err)
		}
		if !slices.Equal(seen, []uint32{base + 0x7fffffff, base + 0x7fffffff - 1}) {
			t.Fatalf("seen %v", seen)
		}
	})
	t.Run("refuses an advance that is not an integer from 0 to 0xffffffff", func(t *testing.T) {
		t.Parallel()
		c := icd.NewCounter(0)
		before := c.Value()
		for _, by := range []int64{-1, 0x100000000} {
			if _, err := c.Advance(by); !errors.Is(err, icd.ErrCounterAdvance) {
				t.Errorf("Advance(%d) = %v", by, err)
			}
		}
		if c.Value() != before {
			t.Fatalf("value moved to %d", c.Value())
		}
	})
}

// TestRandomInitialValue holds IcdCounter.ts:46-48: 1 to 2^28.
func TestRandomInitialValue(t *testing.T) {
	t.Parallel()
	for range 64 {
		if v := icd.RandomInitialValue(); v < 1 || v > 1<<28 {
			t.Fatalf("value %#x", v)
		}
	}
}
