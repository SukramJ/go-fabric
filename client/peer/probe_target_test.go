// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// probeLog keeps the attributes of every matter.controller.peer.probe
// record a logger handled, the ones bound with With included.
type probeLog struct {
	mu     *sync.Mutex
	bound  []slog.Attr
	probes *[]map[string]string
}

func newProbeLog() *probeLog {
	return &probeLog{mu: &sync.Mutex{}, probes: &[]map[string]string{}}
}

func (l *probeLog) Enabled(context.Context, slog.Level) bool { return true }

func (l *probeLog) Handle(_ context.Context, r slog.Record) error {
	if r.Message != "matter.controller.peer.probe" {
		return nil
	}
	m := map[string]string{}
	for _, a := range l.bound {
		m[a.Key] = a.Value.String()
	}
	r.Attrs(func(a slog.Attr) bool {
		m[a.Key] = a.Value.String()
		return true
	})
	l.mu.Lock()
	defer l.mu.Unlock()
	*l.probes = append(*l.probes, m)
	return nil
}

func (l *probeLog) WithAttrs(as []slog.Attr) slog.Handler {
	c := *l
	c.bound = append(append([]slog.Attr(nil), l.bound...), as...)
	return &c
}

func (l *probeLog) WithGroup(string) slog.Handler { return l }

func (l *probeLog) records() []map[string]string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]map[string]string(nil), *l.probes...)
}

// TestProbeLogNamesTheTarget: every probe of a run names the address it
// probed in target — the current address and an alternate — while address
// stays the session's current one (issue 488).
func TestProbeLogNamesTheTarget(t *testing.T) {
	t.Parallel()
	pl := newProbeLog()
	h, p, n := monitoredWith(t, func(c *Context) {
		c.Logger = slog.New(pl)
		c.Timing = &Timing{
			AddressChangeStabilizationDelay: 99 * time.Hour,
			AddressChangeProbeCooldown:      ProbeCooldown{Min: time.Second, Max: time.Minute},
		}
	})
	h.clock.Advance(time.Hour)
	h.announceAddrs(t, "fd00::1", "fd00::2")
	eventually(t, "the discovered addresses", func() bool { return len(p.Service().Addresses()) == 2 })
	h.udp.drain()
	rs, _ := failRuns(t, h, verifyAsync(context.Background(), p, ReasonSessionSuspect))
	if rs[0].ok || !n.s.IsPeerLost() {
		t.Fatal("nothing answered: the session is lost")
	}
	seen := map[string]bool{}
	for _, r := range pl.records() {
		if r["address"] != addr1 {
			t.Fatalf("probe record %v: address is the current address %s", r, addr1)
		}
		seen[r["target"]] = true
	}
	if !seen[addr1] || !seen[addr2] || len(seen) != 2 {
		t.Fatalf("probe targets %v, want %s and %s", seen, addr1, addr2)
	}
}
