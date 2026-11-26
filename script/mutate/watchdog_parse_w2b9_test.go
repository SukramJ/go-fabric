// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"testing"
)

// TestParseProcStatW2B9: pgrp and rss are counted from the last ')', so a
// comm with spaces and parentheses does not shift them; a short or
// malformed line is not ok.
func TestParseProcStatW2B9(t *testing.T) {
	line := "4242 (go (test) x) S 4200 4241 4200 0 -1 4194560 100 0 0 0 5 3 0 0 20 0 9 0 777 123456789 2560 18446744073709551615 1 1 0 0 0 0 0 0 0\n"
	g, r, ok := parseProcStat([]byte(line))
	if !ok || g != 4241 || r != 2560 {
		t.Errorf("parseProcStat = %d, %d, %v; want 4241, 2560, true", g, r, ok)
	}
	for _, bad := range []string{"", "1 (x) S 1 2", "1 (x) S 1 x 1 0 -1 0 0 0 0 0 0 0 0 0 20 0 1 0 1 1 7", "1 (x) S 1 2 1 0 -1 0 0 0 0 0 0 0 0 0 20 0 1 0 1 1 y"} {
		if _, _, ok := parseProcStat([]byte(bad)); ok {
			t.Errorf("parseProcStat(%q) ok; want not ok", bad)
		}
	}
}

// TestSumPsGroupRSSW2B9: only the lines of the group count, in KiB; a
// line that does not parse is skipped.
func TestSumPsGroupRSSW2B9(t *testing.T) {
	out := "    1  23168\n  700   1024\n  700      3\n 7000  99999\n  700\n  abc  5\n  700  1x\n"
	if got, want := sumPsGroupRSS([]byte(out), 700), int64(1027<<10); got != want {
		t.Errorf("sumPsGroupRSS = %d, want %d", got, want)
	}
	if got := sumPsGroupRSS([]byte(out), 9); got != 0 {
		t.Errorf("sumPsGroupRSS of an absent group = %d, want 0", got)
	}
}

// TestByteSizeW2B9: the -maxrss value, plain or with a suffix, and its
// rendering; a negative, unknown or overflowing size is refused.
func TestByteSizeW2B9(t *testing.T) {
	for in, want := range map[string]int64{"0": 0, "1000": 1000, "512KiB": 512 << 10, "200MiB": 200 << 20, "3GiB": 3 << 30} {
		var b byteSize
		if err := b.Set(in); err != nil || int64(b) != want {
			t.Errorf("Set(%q) = %d, %v; want %d", in, b, err, want)
		}
	}
	for _, in := range []string{"-1", "3GB", "GiB", "x", "8589934592GiB"} {
		var b byteSize
		if err := b.Set(in); err == nil {
			t.Errorf("Set(%q) = %d; want an error", in, b)
		}
	}
	for v, want := range map[byteSize]string{0: "0", 1000: "1000", 3 << 30: "3GiB", 1536 << 20: "1536MiB", 2048: "2KiB"} {
		if got := v.String(); got != want {
			t.Errorf("String(%d) = %q, want %q", int64(v), got, want)
		}
	}
	var stderr bytes.Buffer
	if c, ok := parseFlags(nil, &stderr); !ok || c.maxRSS != 3<<30 {
		t.Errorf("default -maxrss = %d, %v; want 3GiB", c.maxRSS, ok)
	}
	if _, ok := parseFlags([]string{"-maxrss", "lots"}, &stderr); ok {
		t.Error("-maxrss lots accepted")
	}
}
