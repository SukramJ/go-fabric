// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mrp

import (
	"errors"
	"math"
	"time"
)

// MRP backoff parameters (Matter Core Spec §4.12.6 Table 9).
const (
	// MRPBackoffBase is the initial retransmission interval.
	MRPBackoffBase = 300 * time.Millisecond
	// MRPBackoffJitterFactor is the upper bound on the random jitter
	// fraction added to each retransmit window.
	MRPBackoffJitterFactor = 0.25
	// MRPBackoffMargin scales the computed backoff up to compensate
	// for clock skew and processing delay on the peer.
	MRPBackoffMargin = 1.1
	// MRPBackoffThreshold is the multiplicative growth factor between
	// successive retransmissions.
	MRPBackoffThreshold = 1.6
	// MaxRetransmissions is the default cap on retries before the
	// caller surfaces a delivery failure.
	MaxRetransmissions = 4

	// SessionIdleIntervalDefault is the SESSION_IDLE_INTERVAL fallback
	// used when a peer never advertised its MRP parameters. Mirrors
	// matter.js SessionIntervals.ts:45-49 defaults.
	SessionIdleIntervalDefault = 500 * time.Millisecond
	// SessionActiveIntervalDefault is the SESSION_ACTIVE_INTERVAL
	// fallback.
	SessionActiveIntervalDefault = 300 * time.Millisecond
	// SessionActiveThresholdDefault is the SESSION_ACTIVE_THRESHOLD
	// fallback: a peer that sent a message within this window counts
	// as active and gets the shorter active interval.
	SessionActiveThresholdDefault = 4 * time.Second
)

// BackoffDuration computes the wait before (re)transmission number
// `transmission` (0 = the initial send) from the peer-appropriate
// base interval per Matter §4.12.2.1:
//
//	interval = base × MARGIN × BASE^max(0, n-1) × (1 + uniform[0, JITTER))
//
// rand01 supplies the jitter draw in [0, 1); pass a deterministic
// func in tests. Mirrors matter.js MRP.ts retransmissionIntervalOf
// (:125-146) and chip ReliableMessageMgr.cpp GetBackoff — the
// exponent threshold means the first retransmit still waits the
// un-grown base interval.
func BackoffDuration(base time.Duration, transmission int, rand01 func() float64) time.Duration {
	exp := max(transmission-1, 0)
	factor := math.Pow(MRPBackoffThreshold, float64(exp))
	jitter := 1 + rand01()*MRPBackoffJitterFactor
	// matter.js computes the backoff in JavaScript numbers, which grow
	// without wrapping (MRP.ts:183-190); a Go Duration saturates instead
	// of overflowing to a negative wait from transmission 52 on, which made
	// an unbounded attempt retransmit in a tight loop.
	d := float64(base) * MRPBackoffMargin * factor * jitter
	if d >= math.MaxInt64 {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(d)
}

// ErrMaxRetransmissionsReached reports a reliable message whose retry
// count has exceeded [MaxRetransmissions]. The bridge's outbound tracker
// surfaces it to the peer-facing paths.
var ErrMaxRetransmissionsReached = errors.New("mrp: max retransmissions reached")
