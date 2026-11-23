// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"strings"
	"testing"
)

// TestUnparsableHeadersWarnOncePerInterval: datagrams of an
// unauthenticated sender whose message header does not parse write one
// Warn line per interval, not one per datagram (review II,
// R2-C2-bound-0303).
func TestUnparsableHeadersWarnOncePerInterval(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	b := &Bridge{logger: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))}
	src := &net.UDPAddr{IP: net.IPv6loopback, Port: 5540}
	for range 50 {
		if err := b.dispatch(context.Background(), []byte{0x00, 0x00}, src); err == nil {
			t.Fatal("a two-byte datagram parsed")
		}
	}
	if n := strings.Count(buf.String(), "matter.rx.header"); n != 1 {
		t.Fatalf("%d Warn lines for 50 unparsable datagrams, want 1:\n%s", n, buf.String())
	}
}

// TestRxWarnLimiterCountsWhatItHolds: inside an interval one line per
// event is let through, each event on its own; the next interval's line
// carries the count of the lines held back.
func TestRxWarnLimiterCountsWhatItHolds(t *testing.T) {
	t.Parallel()
	var l rxWarnLimiter
	if w, _ := l.allow("a", 0); !w {
		t.Fatal("the first line was held")
	}
	if w, _ := l.allow("b", 0); !w {
		t.Fatal("another event's first line was held")
	}
	for range 5 {
		if w, _ := l.allow("a", rxWarnInterval-1); w {
			t.Fatal("a line inside the interval passed")
		}
	}
	if w, n := l.allow("a", rxWarnInterval); !w || n != 5 {
		t.Fatalf("the next interval's line: warn=%v suppressed=%d, want true, 5", w, n)
	}
}
