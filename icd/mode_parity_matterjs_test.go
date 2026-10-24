// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/icd-management/IcdModeTest.ts
// at 862ad75f: the device's idle/active machine on a fake clock
// (MockTime.advance is Fake.Advance). The two "wall-clock steps" cases need
// MockTime.stepWallClock, which moves the wall clock apart from the
// monotonic one; clocktest.Fake has one reading (its Set moves both), and on
// the system clock a time.Time carries the monotonic reading a wall step
// does not move, so the machine's deadlines cannot see one — skipped with
// that reason. "with a clock that advances on every read" wraps the fake in
// a clock whose every Now is a microsecond later, as the case redefines
// Time.nowUs.
package icd_test

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/icd"
)

var modeEpoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

type modeEvents struct {
	mu     sync.Mutex
	events []string
}

func (e *modeEvents) add(s string) {
	e.mu.Lock()
	e.events = append(e.events, s)
	e.mu.Unlock()
}

func (e *modeEvents) want(t *testing.T, want ...string) {
	t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	if !slices.Equal(e.events, want) {
		t.Fatalf("events %v, want %v", e.events, want)
	}
}

// makeState is IcdModeTest.ts:10-20: duration 2 s, threshold 1 s.
func makeState(c clock.Clock) (*icd.ModeState, *modeEvents) {
	ev := &modeEvents{}
	s := icd.NewModeState(icd.ModeOptions{
		ActiveModeDuration:  2 * time.Second,
		ActiveModeThreshold: time.Second,
		OnActiveEntered:     func() { ev.add("active") },
		OnIdleEntered:       func() { ev.add("idle") },
		OnMayEnterIdle:      func() { ev.add("may-idle") },
		Clock:               c,
	})
	return s, ev
}

// tickingClock advances its fake by a microsecond before every reading.
type tickingClock struct{ *clocktest.Fake }

func (c tickingClock) Now() time.Time {
	c.Set(c.Fake.Now().Add(time.Microsecond))
	return c.Fake.Now()
}

func TestIcdModeState(t *testing.T) {
	t.Parallel()
	t.Run("enters active mode on start", func(t *testing.T) {
		t.Parallel()
		s, ev := makeState(clocktest.New(modeEpoch))
		s.Start()
		if s.Mode() != icd.ModeActive {
			t.Fatal(s.Mode())
		}
		ev.want(t, "active")
	})
	t.Run("signals may-enter-idle after the active window but stays active (no auto-transition)", func(t *testing.T) {
		t.Parallel()
		fake := clocktest.New(modeEpoch)
		s, ev := makeState(fake)
		s.Start()
		fake.Advance(2000 * time.Millisecond)
		ev.want(t, "active", "may-idle")
		if s.Mode() != icd.ModeActive {
			t.Fatal(s.Mode())
		}
		fake.Advance(60000 * time.Millisecond)
		ev.want(t, "active", "may-idle")
	})
	t.Run("enters idle only when told, unconditionally", func(t *testing.T) {
		t.Parallel()
		s, ev := makeState(clocktest.New(modeEpoch))
		s.Start()
		s.EnterIdle()
		if s.Mode() != icd.ModeIdle {
			t.Fatal(s.Mode())
		}
		ev.want(t, "active", "idle")
	})
	t.Run("comes back to active on requestActive while idle", func(t *testing.T) {
		t.Parallel()
		s, ev := makeState(clocktest.New(modeEpoch))
		s.Start()
		s.EnterIdle()
		promised := s.RequestActive(5 * time.Second)
		if s.Mode() != icd.ModeActive || promised != 5*time.Second {
			t.Fatalf("%v %v", s.Mode(), promised)
		}
		ev.want(t, "active", "idle", "active")
	})
	t.Run("comes back to active on activity while idle", func(t *testing.T) {
		t.Parallel()
		s, ev := makeState(clocktest.New(modeEpoch))
		s.Start()
		s.EnterIdle()
		s.NoteActivity()
		if s.Mode() != icd.ModeActive {
			t.Fatal(s.Mode())
		}
		ev.want(t, "active", "idle", "active")
	})
	t.Run("activity extends the active window, deferring may-enter-idle, never shortening", func(t *testing.T) {
		t.Parallel()
		fake := clocktest.New(modeEpoch)
		s, ev := makeState(fake)
		s.Start()
		fake.Advance(1500 * time.Millisecond)
		s.NoteActivity() // window -> 1.5 s + 1 s threshold = 2.5 s
		fake.Advance(900 * time.Millisecond)
		ev.want(t, "active")
		fake.Advance(200 * time.Millisecond)
		ev.want(t, "active", "may-idle")
		if s.Mode() != icd.ModeActive {
			t.Fatal(s.Mode())
		}
	})
	t.Run("requestActive never shortens an existing longer window", func(t *testing.T) {
		t.Parallel()
		s, _ := makeState(clocktest.New(modeEpoch))
		s.Start()
		long := s.RequestActive(8 * time.Second)
		if short := s.RequestActive(time.Second); short != long {
			t.Fatalf("short %v, long %v", short, long)
		}
	})
	t.Run("stop halts the machine; enterIdle/noteActivity become no-ops", func(t *testing.T) {
		t.Parallel()
		fake := clocktest.New(modeEpoch)
		s, ev := makeState(fake)
		s.Start()
		s.Stop()
		if s.Mode() != icd.ModeIdle {
			t.Fatal(s.Mode())
		}
		s.NoteActivity()
		s.EnterIdle()
		fake.Advance(60000 * time.Millisecond)
		ev.want(t, "active")
	})
	t.Run("enterIdle is a no-op when already idle", func(t *testing.T) {
		t.Parallel()
		s, ev := makeState(clocktest.New(modeEpoch))
		s.Start()
		s.EnterIdle()
		s.EnterIdle()
		ev.want(t, "active", "idle")
	})
	t.Run("wall-clock steps", func(t *testing.T) {
		t.Parallel()
		for _, name := range []string{
			"keeps the active deadline and promise across a forward step",
			"keeps the active deadline and promise across a backward step",
		} {
			t.Run(name, func(t *testing.T) {
				t.Skip("MockTime.stepWallClock: clocktest.Fake has one reading, and the system clock's time.Time carries the monotonic reading a wall-clock step does not move")
			})
		}
	})
	t.Run("with a clock that advances on every read", func(t *testing.T) {
		t.Parallel()
		t.Run("promises the full request when it wakes the device", func(t *testing.T) {
			t.Parallel()
			s, _ := makeState(tickingClock{clocktest.New(modeEpoch)})
			s.Start()
			s.EnterIdle()
			if got := s.RequestActive(5 * time.Second); got != 5*time.Second {
				t.Fatal(got)
			}
		})
		t.Run("promises the full request when it extends the active window", func(t *testing.T) {
			t.Parallel()
			s, _ := makeState(tickingClock{clocktest.New(modeEpoch)})
			s.Start()
			if got := s.RequestActive(8 * time.Second); got != 8*time.Second {
				t.Fatal(got)
			}
		})
	})
	t.Run("promises whole milliseconds", func(t *testing.T) {
		t.Parallel()
		fake := clocktest.New(modeEpoch)
		s, _ := makeState(fake)
		s.Start()
		fake.Advance(250 * time.Microsecond)
		if got := s.RequestActive(time.Second); got != 1999*time.Millisecond {
			t.Fatal(got)
		}
	})
}
