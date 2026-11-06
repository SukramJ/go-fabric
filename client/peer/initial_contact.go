// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"errors"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
)

// transmissionsPerRefresh is the transmissions of each fresh Sigma1 once
// the schedule refreshes: "Leaves a late Sigma2 time to arrive and
// tolerates a single lost transmission before the next refresh." Mirrors
// matter.js packages/protocol/src/peer/InitialContactSchedule.ts:14-18 at
// db021da0 (TRANSMISSIONS_PER_REFRESH = 3).
const transmissionsPerRefresh = 3

// contactOutcome is how the schedule continues after [contactAttempt.fail]
// — matter.js InitialContactSchedule.Outcome (:189-197): "switched" and
// "refreshed" start the next, refreshing attempt at once (the first of a
// run is "switched"); "restarted" starts the next attempt at once after a
// kick had restarted the schedule; "" is the usual error handling.
type contactOutcome string

const (
	contactSwitched  contactOutcome = "switched"
	contactRefreshed contactOutcome = "refreshed"
	contactRestarted contactOutcome = "restarted"
)

// Why a kick may not restart an attempt — matter.js
// InitialContactSchedule.KickBlocker (:199-209).
const (
	kickBlockedEnded           = "ended"
	kickBlockedAnswered        = "answered"
	kickBlockedRetransmissions = "retransmissions"
	kickBlockedSaving          = "saving"
)

// initialContactSchedule is the Sigma1 transmission schedule of one
// address a connection tries to reach, spanning the CASE exchanges of the
// address, one [contactAttempt] each. In the identical phase an attempt
// sends one Sigma1 with the MRP backoff until both the MRP budget and
// MaxDelayBetweenInitialContactRetries since its first transmission have
// passed; in the refreshing phase each attempt sends a fresh Sigma1 —
// a new exchange, ephemeral key, node id and counter — three times,
// continuing the backoff. An unacknowledged Sigma1 that ended its
// attempt makes the next one refresh; every other end of an attempt — the
// peer answered, a kick restart, another error, an abort — returns the
// schedule to the identical phase. Mirrors matter.js
// InitialContactSchedule (InitialContactSchedule.ts:20-177 at db021da0,
// matter.js #4794): "a fresh Sigma1 is cheap and CHIP never sends one
// more than MRP_MAX_TRANSMISSIONS times".
type initialContactSchedule struct {
	timing         Timing
	onUnresponsive func()

	mu                   sync.Mutex
	refreshing           bool
	earlierTransmissions int
	current              *contactAttempt
	unresponsiveSignaled bool
}

func newInitialContactSchedule(timing Timing, onUnresponsive func()) *initialContactSchedule {
	return &initialContactSchedule{timing: timing, onUnresponsive: onUnresponsive}
}

// errAttemptOpen is begin while the previous attempt is still open — an
// internal error, as matter.js's InternalError (:66-68).
var errAttemptOpen = errors.New("peer: initial contact attempt started while the previous one is still open")

// begin starts the attempt for the next exchange; isDelayedFallback starts
// it at the maximum retry delay. Mirrors InitialContactSchedule.begin
// (:56-162).
func (s *initialContactSchedule) begin(isDelayedFallback bool) (*contactAttempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current != nil {
		return nil, errAttemptOpen
	}
	a := &contactAttempt{s: s, isRefresh: s.refreshing, earlier: s.earlierTransmissions}
	a.maxRetransmissionTime = s.timing.MaxDelayBetweenInitialContactRetries
	if isDelayedFallback {
		a.initialRetransmissionTime = s.timing.MaxDelayBetweenInitialContactRetries
	}
	if a.isRefresh {
		a.maxTransmissions = transmissionsPerRefresh
		a.backoffOffset = a.earlier
	} else {
		a.maxTransmissions = -1 // unlimited
		a.retransmissionTimeLimit = s.timing.MaxDelayBetweenInitialContactRetries
	}
	s.current = a
	return a, nil
}

// resetLocked returns the schedule to the identical phase (:164-169).
func (s *initialContactSchedule) resetLocked() {
	s.refreshing = false
	s.earlierTransmissions = 0
	s.current = nil
	s.unresponsiveSignaled = false
}

// retransmittedLocked signals the peer unresponsive once per run when
// Sigma1 has been retransmitted past MRP's transmission budget, counted
// across attempts (:171-176). It reports whether to signal.
func (s *initialContactSchedule) retransmittedLocked(retransmissions int) bool {
	if !s.unresponsiveSignaled && retransmissions > exchange.MaxTransmissions {
		s.unresponsiveSignaled = true
		return true
	}
	return false
}

// contactAttempt is one CASE exchange of an [initialContactSchedule]: it
// observes the exchange through onSend and onReceive and ends with
// whichever comes first of fail, restart or dispose. Mirrors matter.js
// InitialContactSchedule.Attempt (:211-245).
type contactAttempt struct {
	s         *initialContactSchedule
	isRefresh bool
	earlier   int

	// The Sigma1 send options (InitialContactSchedule.SendOptions,
	// :180-187): MaxInitialRetransmissions (-1 unlimited),
	// MaxInitialRetransmissionTime, InitialRetransmissionTime,
	// InitialRetransmissionTimeLimit, InitialBackoffOffset.
	maxTransmissions          int
	maxRetransmissionTime     time.Duration
	initialRetransmissionTime time.Duration
	retransmissionTimeLimit   time.Duration
	backoffOffset             int

	retransmissions int
	answered        bool
	ended           bool
	restarted       bool
}

// finishLocked ends the attempt and reports whether it was the current one
// (:90-94).
func (a *contactAttempt) finishLocked() bool {
	wasCurrent := !a.ended && a.s.current == a
	a.ended = true
	return wasCurrent
}

// onSend observes a (re)transmission of Sigma1 (:100-106).
func (a *contactAttempt) onSend(retransmission int) {
	a.s.mu.Lock()
	if a.ended || a.answered {
		a.s.mu.Unlock()
		return
	}
	a.retransmissions = retransmission
	signal := a.s.retransmittedLocked(a.earlier + a.retransmissions)
	a.s.mu.Unlock()
	if signal && a.s.onUnresponsive != nil {
		a.s.onUnresponsive()
	}
}

// onReceive observes a message of the peer on the exchange (:108-110).
func (a *contactAttempt) onReceive() {
	a.s.mu.Lock()
	a.answered = true
	a.s.mu.Unlock()
}

// kickBlocker is why a kick may not restart the attempt, "" when it may;
// it has no side effects. restartIsDelayedFallback measures the saving
// against the start a restarted attempt would use (:112-126).
func (a *contactAttempt) kickBlocker(saving time.Duration, restartIsDelayedFallback bool) string {
	a.s.mu.Lock()
	defer a.s.mu.Unlock()
	switch {
	case a.ended:
		return kickBlockedEnded
	case a.answered:
		return kickBlockedAnswered
	case a.earlier+a.retransmissions < a.s.timing.KickMinRetransmissions:
		return kickBlockedRetransmissions
	}
	var restartDelay time.Duration
	if restartIsDelayedFallback {
		restartDelay = a.s.timing.MaxDelayBetweenInitialContactRetries
	}
	if saving-restartDelay < a.s.timing.KickMinRestartSaving {
		return kickBlockedSaving
	}
	return ""
}

// restart ends the attempt because a kick restarts the exchange, and
// returns the schedule to the identical phase (:128-133).
func (a *contactAttempt) restart() {
	a.s.mu.Lock()
	defer a.s.mu.Unlock()
	if a.finishLocked() {
		a.restarted = true
		a.s.resetLocked()
	}
}

// fail ends the attempt because its exchange failed with err (:135-151):
// a Sigma1 the peer never acknowledged — and never answered — moves the
// schedule to the refreshing phase; anything else returns it to the
// identical phase and to the usual error handling. matter.js's
// PeerAckMissingError is the Go [exchange.PeerUnresponsiveError] the
// exchange raises for an unacknowledged message.
func (a *contactAttempt) fail(err error) contactOutcome {
	var ackMissing *exchange.PeerUnresponsiveError
	a.s.mu.Lock()
	defer a.s.mu.Unlock()
	unacknowledged := !a.answered && errors.As(err, &ackMissing)
	if a.restarted {
		if unacknowledged {
			return contactRestarted
		}
		return ""
	}
	if !a.finishLocked() {
		return ""
	}
	if !unacknowledged {
		a.s.resetLocked()
		return ""
	}
	a.s.earlierTransmissions = a.earlier + a.retransmissions + 1
	a.s.current = nil
	a.s.refreshing = true
	if a.isRefresh {
		return contactRefreshed
	}
	return contactSwitched
}

// dispose ends an attempt that is still the current one and returns the
// schedule to the identical phase (:153-157).
func (a *contactAttempt) dispose() {
	a.s.mu.Lock()
	defer a.s.mu.Unlock()
	if a.finishLocked() {
		a.s.resetLocked()
	}
}
