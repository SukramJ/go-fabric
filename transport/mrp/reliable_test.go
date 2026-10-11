// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package mrp

import "testing"

// TestUsesMRP is matter.js Session.ts:355-356 as a truth table.
func TestUsesMRP(t *testing.T) {
	cases := []struct {
		supports, reliable, want bool
	}{
		{true, false, true},   // unicast over UDP
		{true, true, false},   // unicast over TCP
		{false, false, false}, // group session
		{false, true, false},
	}
	for _, c := range cases {
		if got := UsesMRP(c.supports, c.reliable); got != c.want {
			t.Errorf("UsesMRP(%v, %v) = %v, want %v", c.supports, c.reliable, got, c.want)
		}
	}
}
