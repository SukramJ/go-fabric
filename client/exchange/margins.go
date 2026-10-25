// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange

import (
	"time"

	"github.com/SukramJ/go-fabric/transport/mrp"
)

// Margins are the additive MRP retransmission margins of a network, by
// traffic class: bulk transfer sustains many round trips over one path,
// so it may need a margin different from normal messaging. Mirrors
// matter.js MRP.Margins (packages/protocol/src/protocol/MRP.ts:12-19 at
// 862ad75f); a network profile precomputes them (client/peer
// NetworkProfile.Margins).
type Margins struct {
	Messaging time.Duration
	BDX       time.Duration
}

// MarginFor is the margin protocol's traffic belongs to: BDX for the Bulk
// Data Exchange protocol, Messaging otherwise; false for nil margins.
// Mirrors matter.js MRP.marginFor (MRP.ts:21-27).
func MarginFor(m *Margins, protocol uint16) (time.Duration, bool) {
	if m == nil {
		return 0, false
	}
	if protocol == bdxProtocolID {
		return m.BDX, true
	}
	return m.Messaging, true
}

// Pad is the sender-side addition to the MRP backoff — matter.js
// MRP.RetryDelayInputs' additionalDelay and fixedBackoff
// (packages/protocol/src/protocol/MRP.ts:122-140 at 862ad75f).
type Pad struct {
	// Additional joins the base interval, so the exponential backoff
	// amplifies it: a network profile's margin.
	Additional time.Duration
	// Fixed is added to the final backoff after margin, exponent and
	// jitter, so it is not amplified: an ICD sender's fast-polling grace.
	Fixed time.Duration
}

// RetransmissionIntervalPadded is [RetransmissionInterval] with the pad:
// matter.js retransmissionIntervalOf with additionalDelay and fixedBackoff
// (MRP.ts:162-190); both apply to the maximum (rand01 = 1) too, so a timer
// sized from it upper-bounds the real padded schedule. A zero pad is the
// bare spec interval.
func RetransmissionIntervalPadded(n int, p Intervals, peerActive bool, rand01 float64, pad Pad) time.Duration {
	base := p.Idle
	if peerActive {
		base = p.Active
	}
	d := mrp.BackoffDuration(base+pad.Additional, n, func() float64 { return rand01 })
	return (d + pad.Fixed).Truncate(time.Millisecond)
}

// maxResponseTimePadded is maxResponseTime with the sender-side pad on
// every transmission: matter.js maxResponseTimeOf's pad (MRP.ts:198-224),
// which only the local direction — our own retransmissions — carries.
func maxResponseTimePadded(p Intervals, peerActive bool, pad Pad) time.Duration {
	var total time.Duration
	for i := range MaxTransmissions {
		if peerActive && total > p.ActiveThreshold {
			peerActive = false
		}
		total += RetransmissionIntervalPadded(i, p, peerActive, 1, pad)
	}
	return total
}

// MaxPeerResponseTimePadded is [MaxPeerResponseTime] with the local
// direction padded — matter.js maxPeerResponseTimeOf's localAdditionalDelay
// and localFixedBackoff (MRP.ts:79-120, the "udp" case): our own
// retransmissions carry our sender-side pad, the peer's direction does not.
func MaxPeerResponseTimePadded(peer *Intervals, peerActive bool, local Intervals, expectedProcessingTime time.Duration, pad Pad) time.Duration {
	var d time.Duration
	if peer != nil {
		d = maxResponseTime(*peer, peerActive)
	}
	return d + maxResponseTimePadded(local, true, pad) + expectedProcessingTime + peerResponseTimeBuffer
}
