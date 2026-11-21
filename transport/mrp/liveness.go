// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mrp

import (
	"math"
	"time"
)

// LivenessKick is the state of an exchange that received a duplicate of a
// message requiring an acknowledgement, as [ShouldKickForLiveness] reads
// it: whether the duplicate proves the peer awake and the exchange's reply
// lost, so the pending message is retransmitted now rather than on its
// schedule. Each caller fills it from its own exchange; the budget arrives
// as a verdict because its inputs differ per caller.
type LivenessKick struct {
	// BDX reports that the exchange's protocol — not the duplicate's — is
	// BDX.
	BDX bool
	// Closed is matter.js's considerClosed: the exchange is closed, or it
	// is an initiator exchange that is closing.
	Closed bool
	// Armed reports a pending message whose retransmission timer runs:
	// no transmission of it is in flight.
	Armed bool
	// PendingAcks reports that the pending message carries an
	// acknowledgement, of the message counter PendingAckCounter.
	PendingAcks       bool
	PendingAckCounter uint32
	// DuplicateCounter is the duplicate's message counter.
	DuplicateCounter uint32
	// NextBudgetSpent is the exchange's verdict that the transmission
	// after those already sent may not go out — matter.js
	// #isTransmissionBudgetSpent(retransmissions + 1).
	NextBudgetSpent bool
	// Transmitted reports that the instant of the pending message's last
	// transmission is known; Elapsed is the time since it. Without it the
	// elapsed time counts as unbounded.
	Transmitted bool
	Elapsed     time.Duration
	// Interval is the armed retransmission timer's interval.
	Interval time.Duration
	// ActiveInterval and ActiveThreshold are the peer's session
	// parameters as the session holds them.
	ActiveInterval  time.Duration
	ActiveThreshold time.Duration
	// AdditionalDelay is the exchange's additional MRP delay of its sends
	// (matter.js #sendAdditionalDelay).
	AdditionalDelay time.Duration
}

// ShouldKickForLiveness reports whether the pending message of k's
// exchange is retransmitted now, in place of the standalone
// acknowledgement the duplicate would get. Only on a BDX exchange that is
// not closed, for a duplicate of exactly the message the pending one
// acknowledges, while its timer runs; never when the next transmission is
// past the budget; never faster than the peer's active interval plus the
// additional delay since the last transmission; and only when the
// scheduled attempt lies further away than the peer's active threshold —
// a peer that stays awake that long loses nothing to the schedule. The
// caller stops the timer and retransmits when it reports true.
// Mirrors matter.js packages/protocol/src/protocol/MessageExchange.ts:593-629 at b25ed960
// (#kickRetransmissionForLiveness, its checks before the retransmission),
// called at packages/protocol/src/protocol/MessageExchange.ts:514-523 at b25ed960
// (a duplicate requiring an ack: the standalone ack only when nothing was kicked).
func ShouldKickForLiveness(k LivenessKick) bool {
	if !k.BDX || k.Closed {
		return false
	}
	if !k.Armed || !k.PendingAcks || k.DuplicateCounter != k.PendingAckCounter {
		return false
	}
	if k.NextBudgetSpent {
		return false
	}
	elapsed := time.Duration(math.MaxInt64)
	if k.Transmitted {
		elapsed = k.Elapsed
	}
	if elapsed < k.ActiveInterval+k.AdditionalDelay {
		return false
	}
	remaining := max(0, k.Interval-elapsed)
	return remaining > k.ActiveThreshold
}
