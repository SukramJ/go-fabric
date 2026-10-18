// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package ble

// A port of matter.js packages/protocol/test/common/BleListeningClockTest.ts at
// 862ad75f: the accumulator of a BLE client's scan time (ListeningMeter).

import (
	"testing"
	"time"
)

// TestBleListeningClock covers BleListeningClockTest.ts:10-56.
func TestBleListeningClock(t *testing.T) {
	t.Parallel()
	t.Run("counts nothing before the radio scans", func(t *testing.T) {
		t.Parallel()
		clk := newFakeClock()
		m := NewListeningMeter(clk)
		clk.Advance(10 * time.Second)
		if got := m.Total(); got != 0 {
			t.Errorf("total %v, want 0", got)
		}
	})
	t.Run("includes a scan that is still running", func(t *testing.T) {
		t.Parallel()
		clk := newFakeClock()
		m := NewListeningMeter(clk)
		m.Start()
		clk.Advance(3 * time.Second)
		if got := m.Total(); got != 3*time.Second {
			t.Errorf("total %v, want 3s", got)
		}
	})
	t.Run("adds up separate scans and leaves out the time between them", func(t *testing.T) {
		t.Parallel()
		clk := newFakeClock()
		m := NewListeningMeter(clk)
		m.Start()
		clk.Advance(2 * time.Second)
		m.Stop()
		clk.Advance(10 * time.Second)
		m.Start()
		clk.Advance(3 * time.Second)
		m.Stop()
		if got := m.Total(); got != 5*time.Second {
			t.Errorf("total %v, want 5s", got)
		}
	})
	t.Run("keeps the start of a scan when the radio reports it again", func(t *testing.T) {
		t.Parallel()
		clk := newFakeClock()
		m := NewListeningMeter(clk)
		m.Start()
		clk.Advance(2 * time.Second)
		m.Start()
		clk.Advance(2 * time.Second)
		if got := m.Total(); got != 4*time.Second {
			t.Errorf("total %v, want 4s", got)
		}
	})
	t.Run("ignores a stop without a running scan", func(t *testing.T) {
		t.Parallel()
		clk := newFakeClock()
		m := NewListeningMeter(clk)
		m.Start()
		clk.Advance(2 * time.Second)
		m.Stop()
		m.Stop()
		if got := m.Total(); got != 2*time.Second {
			t.Errorf("total %v, want 2s", got)
		}
	})
}
