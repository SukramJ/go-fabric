// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package btp

import (
	"encoding/hex"
	"sort"
	"sync"
	"testing"
	"time"
)

// fakeClock is a manual Clock: timers fire, in order, inside Advance —
// matter.js's MockTime for these tests.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Duration
	timers []*fakeTimer
}

type fakeTimer struct {
	c       *fakeClock
	at      time.Duration
	f       func()
	stopped bool
	fired   bool
}

func (c *fakeClock) AfterFunc(d time.Duration, f func()) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{c: c, at: c.now + d, f: f}
	c.timers = append(c.timers, t)
	return t
}

func (t *fakeTimer) Stop() bool {
	t.c.mu.Lock()
	defer t.c.mu.Unlock()
	was := !t.stopped && !t.fired
	t.stopped = true
	return was
}

// Advance moves the clock by d, firing every due timer in order.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	target := c.now + d
	for {
		sort.SliceStable(c.timers, func(i, j int) bool { return c.timers[i].at < c.timers[j].at })
		var next *fakeTimer
		for _, t := range c.timers {
			if !t.stopped && !t.fired && t.at <= target {
				next = t
				break
			}
		}
		if next == nil {
			break
		}
		next.fired = true
		c.now = next.at
		c.mu.Unlock()
		next.f()
		c.mu.Lock()
	}
	c.now = target
	c.mu.Unlock()
}

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func u8(v uint8) *uint8    { return &v }
func u16(v uint16) *uint16 { return &v }

func mustEncode(t *testing.T, p Packet) []byte {
	t.Helper()
	b, err := EncodePacket(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustDecode(t *testing.T, b []byte) Packet {
	t.Helper()
	p, err := DecodePacket(b)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// recorder collects writes, safe for the goroutines the hold tests use.
type recorder struct {
	mu     sync.Mutex
	writes [][]byte
}

func (r *recorder) add(b []byte) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writes = append(r.writes, append([]byte(nil), b...))
	return len(r.writes)
}

func (r *recorder) len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.writes)
}

func (r *recorder) at(i int) []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.writes[i]
}

// waitFor polls cond until it holds, for the cases whose write blocks on
// another goroutine.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		time.Sleep(time.Millisecond)
	}
}

func noDisconnect(t *testing.T) func() error {
	t.Helper()
	return func() error { t.Helper(); t.Error("Disconnect should not be called"); return nil }
}

func noMessage(t *testing.T) func([]byte) error {
	t.Helper()
	return func([]byte) error { t.Helper(); t.Error("Message should not be called"); return nil }
}
