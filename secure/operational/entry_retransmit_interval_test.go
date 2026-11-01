// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package operational_test

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
)

// A freshly-opened Entry counts its peer active from creation: matter.js
// sets the active timestamp of every secure session when it is created
// (NodeSession.ts:120 at 862ad75f, "We always set the active timestamp for
// Secure sessions") and chip initialises mLastPeerActivityTime to now
// (SecureSession.h:349 at 6170af84). Without advertised hints the base is
// the spec SESSION_ACTIVE_INTERVAL default (300ms) within the active
// threshold, the idle default (500ms) after it.
func TestEntry_RetransmitBaseInterval_FreshCountsThePeerActive(t *testing.T) {
	t.Parallel()
	m, _ := newTestManager()
	e, err := m.OpenFromSigma(1, 10, 20, testKeys())
	if err != nil {
		t.Fatalf("OpenFromSigma: %v", err)
	}
	if got := e.RetransmitBaseInterval(time.Now()); got != 300*time.Millisecond {
		t.Errorf("RetransmitBaseInterval (fresh entry) = %v, want 300ms (SessionActiveIntervalDefault)", got)
	}
	if got := e.RetransmitBaseInterval(time.Now().Add(5 * time.Second)); got != 500*time.Millisecond {
		t.Errorf("RetransmitBaseInterval (past the 4s threshold) = %v, want 500ms (SessionIdleIntervalDefault)", got)
	}
}

// TestEntry_RetransmitBaseInterval_ActiveAfterMarkActiveRx verifies
// that once the peer's MRP hints are recorded AND the peer has sent a
// message (MarkActiveRx), RetransmitBaseInterval switches to the
// peer's advertised active interval.
func TestEntry_RetransmitBaseInterval_ActiveAfterMarkActiveRx(t *testing.T) {
	t.Parallel()
	m, _ := newTestManager()
	e, err := m.OpenFromSigma(1, 10, 20, testKeys())
	if err != nil {
		t.Fatalf("OpenFromSigma: %v", err)
	}
	e.SetPeerMRPIntervals(5000, 1000, 2000)
	e.MarkActiveRx()
	got := e.RetransmitBaseInterval(time.Now())
	if got != 1000*time.Millisecond {
		t.Errorf("RetransmitBaseInterval (active peer) = %v, want 1000ms (peer active interval)", got)
	}
}

// TestEntry_RetransmitBaseInterval_IdleWithoutRx verifies that without
// inbound activity the peer turns idle once its active threshold has
// passed since the session's creation.
func TestEntry_RetransmitBaseInterval_IdleWithoutRx(t *testing.T) {
	t.Parallel()
	m, _ := newTestManager()
	e, err := m.OpenFromSigma(1, 10, 20, testKeys())
	if err != nil {
		t.Fatalf("OpenFromSigma: %v", err)
	}
	e.SetPeerMRPIntervals(5000, 1000, 2000)
	// No MarkActiveRx call; the 2s threshold has passed since creation.
	got := e.RetransmitBaseInterval(time.Now().Add(3 * time.Second))
	if got != 5000*time.Millisecond {
		t.Errorf("RetransmitBaseInterval (no peer rx yet) = %v, want 5000ms (peer idle interval)", got)
	}
}

// TestEntry_RetransmitBaseInterval_MarkActiveTxDoesNotFlipToActive
// verifies that outbound-only traffic (MarkActiveTx) does NOT count
// as peer activity for the active/idle determination — only inbound
// decrypts (MarkActiveRx) do. Mirrors chip SecureSession's distinct
// GetLastPeerActivityTime (Rx-only) vs the general last-activity
// timestamp (Rx+Tx) used for idle-session eviction.
func TestEntry_RetransmitBaseInterval_MarkActiveTxDoesNotFlipToActive(t *testing.T) {
	t.Parallel()
	m, _ := newTestManager()
	clk := clocktest.New(time.UnixMilli(1_700_000_000_000))
	m.SetClock(clk)
	e, err := m.OpenFromSigma(1, 10, 20, testKeys())
	if err != nil {
		t.Fatalf("OpenFromSigma: %v", err)
	}
	e.SetPeerMRPIntervals(5000, 1000, 2000)
	clk.Set(clk.Now().Add(3 * time.Second)) // past the 2s threshold since creation
	e.MarkActiveTx()
	got := e.RetransmitBaseInterval(clk.Now())
	if got != 5000*time.Millisecond {
		t.Errorf("RetransmitBaseInterval (Tx-only entry) = %v, want 5000ms (idle) — MarkActiveTx must not flip peer-active state", got)
	}
}
