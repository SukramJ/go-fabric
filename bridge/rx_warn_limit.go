// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"thingfabric.dev/go-fabric/clock"
)

// rxWarnInterval is how often a per-datagram warning of an
// unauthenticated sender reaches the Warn level: once per interval per
// event, the lines in between at Debug, counted on the next Warn.
const rxWarnInterval = time.Minute

// rxWarnLimiter bounds the Warn lines the receive path writes once per
// datagram before any authentication — a header that does not parse, a
// PASE message during the lockout — so a sender on the link cannot turn
// its datagram rate into the host's log rate, as [sessionMissBurst] does
// for the session-id misses (review II, R2-C2-bound-0303). Its keys are
// the events' constant names, so the table holds a handful of entries.
type rxWarnLimiter struct {
	mu    sync.Mutex
	slots map[string]*rxWarnSlot
}

type rxWarnSlot struct {
	at         time.Duration // monotonic reading of the last Warn
	suppressed int           // lines since it, written at Debug
}

// allow reports whether event's line at the monotonic reading at is
// written at Warn, and how many lines went to Debug since the last one.
func (l *rxWarnLimiter) allow(event string, at time.Duration) (warn bool, suppressed int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.slots == nil {
		l.slots = make(map[string]*rxWarnSlot)
	}
	s, ok := l.slots[event]
	if !ok {
		l.slots[event] = &rxWarnSlot{at: at}
		return true, 0
	}
	if at-s.at < rxWarnInterval {
		s.suppressed++
		return false, 0
	}
	suppressed = s.suppressed
	s.at, s.suppressed = at, 0
	return true, suppressed
}

// warnPerDatagram writes event at Warn once per [rxWarnInterval], with
// the count of the lines in between, and at Debug otherwise.
func (b *Bridge) warnPerDatagram(event string, attrs ...slog.Attr) {
	warn, suppressed := b.rxWarns.allow(event, clock.MonotonicOf(b.clk()))
	if !warn {
		b.logger.LogAttrs(context.Background(), slog.LevelDebug, event, attrs...)
		return
	}
	if suppressed > 0 {
		attrs = append(attrs, slog.Int("suppressed_since_last", suppressed))
	}
	b.logger.LogAttrs(context.Background(), slog.LevelWarn, event, attrs...)
}
