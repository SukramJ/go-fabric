// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange

import (
	"time"

	"github.com/SukramJ/go-fabric/transport/mrp"
)

// MRP constants of the exchange layer.
const (
	// MaxTransmissions is the number of transmissions of one reliable
	// message, the first included. Mirrors matter.js
	// packages/protocol/src/protocol/MRP.ts:33 (MAX_TRANSMISSIONS = 5) at
	// 0ceb202d.
	MaxTransmissions = 5

	// StandaloneAckTimeout is how long a received reliable message waits
	// for an outbound message to piggyback its acknowledgement before a
	// standalone ack goes out. Mirrors matter.js MRP.ts:48
	// (STANDALONE_ACK_TIMEOUT = 200 ms), the delay of mrp.AckTracker.
	StandaloneAckTimeout = mrp.DefaultStandaloneAckDelay

	// DefaultExpectedProcessingTime is the peer's processing time an
	// exchange assumes when the caller states none. Mirrors matter.js
	// MRP.ts:55 (DEFAULT_EXPECTED_PROCESSING_TIME = 2 s).
	DefaultExpectedProcessingTime = 2 * time.Second

	// MaxPayloadSize is the largest message payload an exchange sends over
	// UDP: the 1232-byte UDP payload of an IPv6 minimum MTU less the Matter
	// message overhead (26 + 12 + 16). Mirrors matter.js
	// packages/protocol/src/protocol/MessageExchange.ts:156
	// (MATTER_MESSAGE_OVERHEAD), :435-439 (maxPayloadSize) and
	// packages/general/src/net/udp/UdpSocket.ts:12 (MAX_UDP_MESSAGE_SIZE).
	MaxPayloadSize = 1232 - (26 + 12 + 16)

	// peerResponseTimeBuffer is the margin added to the peer's worst-case
	// response time. Mirrors matter.js MRP.ts:61 (PEER_RESPONSE_TIME_BUFFER
	// = 5 s).
	peerResponseTimeBuffer = 5 * time.Second
)

// Intervals are the MRP timing parameters of one side of a session.
type Intervals struct {
	// Idle is SESSION_IDLE_INTERVAL: the retry base while the node is idle.
	Idle time.Duration
	// Active is SESSION_ACTIVE_INTERVAL: the retry base while it is active.
	Active time.Duration
	// ActiveThreshold is SESSION_ACTIVE_THRESHOLD: how long after its last
	// message the node counts as active.
	ActiveThreshold time.Duration
}

// RetransmissionInterval is the wait after transmission number n (0 is
// the first transmission) of a reliable message to a peer with intervals
// p: the peer's active interval while it is active, its idle interval
// otherwise, grown by the §4.12.2.1 backoff and jittered by rand01 (a
// value in [0, 1); 1 gives the maximum), floored to whole milliseconds.
//
// Mirrors matter.js packages/protocol/src/protocol/MRP.ts:162-190
// (retransmissionIntervalOf) at 0ceb202d with additionalDelay and
// fixedBackoff zero — the controller has no network profile — through
// transport/mrp.BackoffDuration, the same formula.
func RetransmissionInterval(n int, p Intervals, peerActive bool, rand01 float64) time.Duration {
	base := p.Idle
	if peerActive {
		base = p.Active
	}
	d := mrp.BackoffDuration(base, n, func() float64 { return rand01 })
	return d.Truncate(time.Millisecond)
}

// maxResponseTime is the longest one direction of an exchange can take:
// the maximum backoff of all MaxTransmissions transmissions, where a peer
// assumed active turns idle once the accumulated wait passes its active
// threshold. Mirrors matter.js MRP.ts:198-224 (maxResponseTimeOf).
func maxResponseTime(p Intervals, peerActive bool) time.Duration {
	var total time.Duration
	for i := range MaxTransmissions {
		if peerActive && total > p.ActiveThreshold {
			peerActive = false
		}
		total += RetransmissionInterval(i, p, peerActive, 1)
	}
	return total
}

// ReliableMaxPeerResponseTime is how long an exchange waits for the
// peer's next message on a reliable channel without MRP: 30 s, or the
// expected processing time when longer, plus the 5 s buffer. Mirrors
// matter.js MRP.ts:111-113 (the "ble" case; the "tcp" case at :90-92 is
// the same).
func ReliableMaxPeerResponseTime(expectedProcessingTime time.Duration) time.Duration {
	return max(reliablePeerResponseTime, expectedProcessingTime) + peerResponseTimeBuffer
}

// reliablePeerResponseTime is the 30 s base of a reliable channel (MRP.ts:92, :113).
const reliablePeerResponseTime = 30 * time.Second

// MaxPeerResponseTime is how long an exchange waits for the peer's next
// message over UDP with MRP: our own worst-case retransmission time
// (local intervals, assumed active), the expected processing time and a
// 5 s buffer, plus — when peer is not nil — the peer's worst-case
// retransmission time back to us. Mirrors matter.js MRP.ts:79-120
// (maxPeerResponseTimeOf, the "udp" case).
//
// Exchange.Next passes no peer intervals: matter.js MessageExchange.ts:840-847
// calls calculateMaximumPeerResponseTime with includeMaximumSendingTime
// undefined, and MessageChannel.ts:224-226 then omits the peer direction.
func MaxPeerResponseTime(peer *Intervals, peerActive bool, local Intervals, expectedProcessingTime time.Duration) time.Duration {
	var d time.Duration
	if peer != nil {
		d = maxResponseTime(*peer, peerActive)
	}
	return d + maxResponseTime(local, true) + expectedProcessingTime + peerResponseTimeBuffer
}
