// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package icd

import "time"

// CheckInMargin is the slack on a peer's Check-In deadline. A Check-In is
// an unreliable, unacknowledged sessionless message, so it covers only the
// device's scheduling jitter. Mirrors matter.js IcdPeerSchedule.ts:28
// (CHECK_IN_MARGIN).
const CheckInMargin = 10 * time.Second

// Timings are a peer's ICD timings, from its ICD Management cluster
// (IcdPeerSchedule.ts:92-96).
type Timings struct {
	ActiveModeThreshold time.Duration
	ActiveModeDuration  time.Duration
	IdleModeDuration    time.Duration
}

// DefaultTimings are the timings of a peer whose ICD Management state is
// not known (IcdPeerSchedule.ts:30-34, DEFAULT_TIMINGS).
var DefaultTimings = Timings{ActiveModeThreshold: 5 * time.Second, IdleModeDuration: 30 * time.Second}

// PeerSchedule is when a LIT ICD peer is in active mode and when its next
// Check-In is due, from its timings and the activity the controller saw:
// active for ActiveModeDuration after a Check-In, ActiveModeThreshold after
// other activity, and until a StayActive promise ends, whichever is last;
// then idle for at most IdleModeDuration before it checks in again. The
// peer is assumed not to back off, as the CHIP SDK's default strategy does
// not. Callers pass the time; the schedule holds no timer. Not safe for
// concurrent use; [PeerWakefulness] guards it.
//
// Mirrors matter.js packages/protocol/src/icd/IcdPeerSchedule.ts:23-88
// (Core §9.15.1.3.2.3, §9.16.6.2-3).
type PeerSchedule struct {
	timings     Timings
	activeUntil time.Time // zero before any activity
	set         bool
}

// NewPeerSchedule returns a schedule with [DefaultTimings].
func NewPeerSchedule() *PeerSchedule { return &PeerSchedule{timings: DefaultTimings} }

// Timings are the schedule's timings (IcdPeerSchedule.ts:39-41).
func (s *PeerSchedule) Timings() Timings { return s.timings }

// SetTimings replaces them (IcdPeerSchedule.ts:43-45).
func (s *PeerSchedule) SetTimings(t Timings) { s.timings = t }

// ActiveUntil is the end of the peer's estimated active mode; ok is false
// before any activity (IcdPeerSchedule.ts:48-50).
func (s *PeerSchedule) ActiveUntil() (t time.Time, ok bool) { return s.activeUntil, s.set }

// NextCheckInDue is the deadline of the peer's next Check-In; ok is false
// before any activity (IcdPeerSchedule.ts:53-58).
func (s *PeerSchedule) NextCheckInDue() (t time.Time, ok bool) {
	if !s.set {
		return time.Time{}, false
	}
	return s.activeUntil.Add(s.IdleWindow()), true
}

// IdleWindow is the longest the peer may idle before its next Check-In,
// with the margin (IcdPeerSchedule.ts:61-63).
func (s *PeerSchedule) IdleWindow() time.Duration { return s.timings.IdleModeDuration + CheckInMargin }

// NoteCheckIn records a Check-In, the peer entering active mode; threshold,
// when not nil, is the ActiveModeThreshold the Check-In carried, which
// overrides the configured one. Active mode lasts the longer of
// ActiveModeDuration and the threshold — not their sum, as the CHIP SDK's
// ICDManager does. Mirrors IcdPeerSchedule.ts:66-70.
func (s *PeerSchedule) NoteCheckIn(now time.Time, threshold *time.Duration) {
	t := s.timings.ActiveModeThreshold
	if threshold != nil {
		t = *threshold
	}
	s.extendActive(now, max(s.timings.ActiveModeDuration, t))
}

// NoteActive records activity other than a Check-In (IcdPeerSchedule.ts:73-75).
func (s *PeerSchedule) NoteActive(now time.Time) { s.extendActive(now, s.timings.ActiveModeThreshold) }

// NoteStayActive records a StayActive promise (IcdPeerSchedule.ts:78-80).
func (s *PeerSchedule) NoteStayActive(now time.Time, promised time.Duration) {
	s.extendActive(now, promised)
}

// extendActive never shortens active mode (IcdPeerSchedule.ts:82-87).
func (s *PeerSchedule) extendActive(now time.Time, d time.Duration) {
	until := now.Add(d)
	if !s.set || until.After(s.activeUntil) {
		s.activeUntil, s.set = until, true
	}
}
