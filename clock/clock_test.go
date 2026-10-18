// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package clock_test

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"
)

// fixed is a clock.Clock whose Now is t and whose timers are the system's.
type fixed struct {
	clock.System
	t time.Time
}

func (f fixed) Now() time.Time { return f.t }

func TestSystemIsTheStandardLibrary(t *testing.T) {
	t.Parallel()
	var c clock.Clock = clock.System{}
	if before := time.Now(); c.Now().Before(before) {
		t.Fatal("System.Now is behind time.Now")
	}
	done := make(chan struct{})
	tm := c.AfterFunc(time.Millisecond, func() { close(done) })
	<-done
	if tm.C() != nil {
		t.Fatal("an AfterFunc timer has a channel")
	}
	if tm.Stop() {
		t.Fatal("Stop after firing reported true")
	}
	nt := c.NewTimer(time.Hour)
	if !nt.Reset(time.Millisecond) {
		t.Fatal("Reset of an armed timer reported false")
	}
	<-nt.C()
	tk := c.NewTicker(time.Millisecond)
	<-tk.C()
	tk.Reset(time.Millisecond)
	<-tk.C()
	tk.Stop()
}

func TestHelpers(t *testing.T) {
	t.Parallel()
	if clock.Or(nil) != (clock.System{}) {
		t.Fatal("Or(nil) is not System")
	}
	base := time.Date(2025, 1, 1, 12, 34, 56, 0, time.UTC)
	f := fixed{t: base}
	if clock.Or(f) != clock.Clock(f) {
		t.Fatal("Or(c) is not c")
	}
	if clock.Since(f, base.Add(-time.Second)) != time.Second {
		t.Fatal("Since does not read the clock")
	}
	if clock.Until(f, base.Add(time.Second)) != time.Second {
		t.Fatal("Until does not read the clock")
	}
	if !clock.Sleep(f, time.Millisecond, nil) {
		t.Fatal("Sleep did not report the full wait")
	}
	done := make(chan struct{})
	close(done)
	if clock.Sleep(f, time.Hour, done) {
		t.Fatal("Sleep reported the full wait though done was closed")
	}
	<-clock.After(f, time.Millisecond)
}
