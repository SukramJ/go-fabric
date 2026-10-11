// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package mrp

// UsesMRP reports whether the Message Reliability Protocol runs for a
// session's exchanges. It does when the session type supports MRP and
// its channel is not reliable by itself. Mirrors matter.js
// packages/protocol/src/session/Session.ts:355-356
// (`usesMrp = supportsMRP && !channel?.isReliable`); supportsMRP is true
// for secure and unsecured unicast sessions (NodeSession.ts:59,
// UnsecuredSession.ts:23) and false for group sessions
// (GroupSession.ts:74).
//
// When it is false — a session over TCP — matter.js's MessageExchange
// drops every MRP mechanism:
//
//   - an inbound message's R flag is ignored, so no acknowledgement is
//     owed (MessageExchange.ts:448-452);
//   - an outbound message does not set the R flag
//     (MessageExchange.ts:660);
//   - no acknowledgement is piggybacked on an outbound message
//     (MessageExchange.ts:680-686);
//   - no standalone acknowledgement is sent (MessageExchange.ts:666-668,
//     :867-869);
//   - no retransmission timer is armed (MessageExchange.ts:752).
//
// Message counters and the secure-channel processing are unchanged.
func UsesMRP(supportsMRP, reliableChannel bool) bool {
	return supportsMRP && !reliableChannel
}
