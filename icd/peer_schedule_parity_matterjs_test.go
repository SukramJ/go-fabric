// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/protocol/test/icd/IcdPeerScheduleTest.ts at
// 862ad75f: a LIT peer's active mode after a Check-In, other activity and a
// StayActive promise, and the deadline of its next Check-In.
package icd_test

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/icd"
)

var scheduleT0 = time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC)

func at(d time.Duration) time.Time { return scheduleT0.Add(d) }

// schedule is IcdPeerScheduleTest.ts:16-25: threshold 4 s, duration 0,
// idle 30 s, with the case's overrides.
func schedule(edit func(*icd.Timings)) *icd.PeerSchedule {
	s := icd.NewPeerSchedule()
	t := icd.Timings{ActiveModeThreshold: 4 * time.Second, IdleModeDuration: 30 * time.Second}
	if edit != nil {
		edit(&t)
	}
	s.SetTimings(t)
	return s
}

func wantActiveUntil(t *testing.T, s *icd.PeerSchedule, want time.Time) {
	t.Helper()
	if got, ok := s.ActiveUntil(); !ok || !got.Equal(want) {
		t.Fatalf("activeUntil %v (%v), want %v", got, ok, want)
	}
}

func wantDue(t *testing.T, s *icd.PeerSchedule, want time.Time) {
	t.Helper()
	if got, ok := s.NextCheckInDue(); !ok || !got.Equal(want) {
		t.Fatalf("nextCheckInDue %v (%v), want %v", got, ok, want)
	}
}

func TestIcdPeerSchedule(t *testing.T) {
	t.Parallel()
	duration20 := func(t *icd.Timings) { t.ActiveModeDuration = 20 * time.Second }
	t.Run("has no deadlines before any activity", func(t *testing.T) {
		t.Parallel()
		s := schedule(nil)
		if _, ok := s.ActiveUntil(); ok {
			t.Fatal("activeUntil set")
		}
		if _, ok := s.NextCheckInDue(); ok {
			t.Fatal("nextCheckInDue set")
		}
	})
	t.Run("Active Mode", func(t *testing.T) {
		t.Parallel()
		t.Run("lasts activeModeThreshold after activity", func(t *testing.T) {
			t.Parallel()
			s := schedule(nil)
			s.NoteActive(scheduleT0)
			wantActiveUntil(t, s, at(4*time.Second))
		})
		t.Run("lasts activeModeDuration after a Check-In when that is longer", func(t *testing.T) {
			t.Parallel()
			s := schedule(duration20)
			s.NoteCheckIn(scheduleT0, nil)
			wantActiveUntil(t, s, at(20*time.Second))
		})
		t.Run("uses the threshold a Check-In carries", func(t *testing.T) {
			t.Parallel()
			s := schedule(nil)
			nine := 9 * time.Second
			s.NoteCheckIn(scheduleT0, &nine)
			wantActiveUntil(t, s, at(9*time.Second))
		})
		t.Run("does not apply activeModeDuration to activity other than a Check-In", func(t *testing.T) {
			t.Parallel()
			s := schedule(duration20)
			s.NoteActive(scheduleT0)
			wantActiveUntil(t, s, at(4*time.Second))
		})
		t.Run("is not shortened by later activity", func(t *testing.T) {
			t.Parallel()
			s := schedule(duration20)
			s.NoteCheckIn(scheduleT0, nil)
			s.NoteActive(at(10 * time.Second))
			wantActiveUntil(t, s, at(20*time.Second))
		})
		t.Run("lasts until a StayActive promise ends", func(t *testing.T) {
			t.Parallel()
			s := schedule(nil)
			s.NoteActive(scheduleT0)
			s.NoteStayActive(scheduleT0, 60*time.Second)
			wantActiveUntil(t, s, at(60*time.Second))
		})
	})
	t.Run("next Check-In", func(t *testing.T) {
		t.Parallel()
		t.Run("is due idleModeDuration plus margin after Active Mode", func(t *testing.T) {
			t.Parallel()
			s := schedule(duration20)
			s.NoteCheckIn(scheduleT0, nil)
			wantDue(t, s, at(20*time.Second+30*time.Second+icd.CheckInMargin))
		})
		t.Run("moves with later activity", func(t *testing.T) {
			t.Parallel()
			s := schedule(nil)
			s.NoteCheckIn(scheduleT0, nil)
			s.NoteActive(at(100 * time.Second))
			wantDue(t, s, at(104*time.Second+30*time.Second+icd.CheckInMargin))
		})
		t.Run("follows new timings", func(t *testing.T) {
			t.Parallel()
			s := schedule(nil)
			s.NoteActive(scheduleT0)
			tm := s.Timings()
			tm.IdleModeDuration = 60 * time.Second
			s.SetTimings(tm)
			wantDue(t, s, at(4*time.Second+60*time.Second+icd.CheckInMargin))
		})
	})
	t.Run("has an idle window of idleModeDuration plus margin", func(t *testing.T) {
		t.Parallel()
		if got := schedule(nil).IdleWindow(); got != 30*time.Second+icd.CheckInMargin {
			t.Fatal(got)
		}
	})
}
