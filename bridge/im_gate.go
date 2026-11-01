// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/transport/message"
)

// imGateAction is the pre-dispatch routing decision for an inbound IM
// message, computed from opcode + session type alone — no Bridge state,
// so it is table-testable in isolation.
type imGateAction int

const (
	imGateProceed            imGateAction = iota // decode + dispatch
	imGateAbsorbStatusResp                       // StatusResponse: MRP ack, no dispatch
	imGateRejectUnsupported                      // non-request opcode
	imGateRejectGroupSession                     // Read/Subscribe/Timed over a group session (§8.5.7)
	imGateDropUnsecured                          // any IM message on the unsecured session 0
)

// classifyIMOpcode returns the pre-dispatch routing decision for an
// inbound IM message based on the opcode and session type alone.
//
// Group-session reject guard. Matter §8.5.7: only Write- and
// Invoke-Requests are valid over Secure Group sessions; Read,
// Subscribe, and Timed requests are unicast-only and must be
// rejected with StatusResponse(InvalidAction).
//
// Mirrors matter.js packages/protocol/src/interaction/
// InteractionMessenger.ts:240,269,287 — ReadRequest /
// SubscribeRequest / TimedRequest each throw a
// StatusResponseError(InvalidAction, "<op> is not supported in
// group sessions"). Without this guard, a malicious group sender
// could enumerate the bridge's attribute tree via a single
// multicast Read.
//
// Secure-session guard. The Interaction Model runs on secure sessions
// only: an IM message on the unsecured session (unicast session type,
// session id 0) is dropped before decode — no exchange, no reply beyond
// the MRP standalone ack. Mirrors matter.js
// packages/node/src/node/server/InteractionServer.ts:161
// (`requiresSecureSession = true`) and
// packages/protocol/src/protocol/ExchangeManager.ts:391-412, which logs
// "Ignore … not matching the security requirements" and opens no
// exchange for a handler whose requiresSecureSession differs from
// session.isSecure.
func classifyIMOpcode(opcode uint8, sessionType message.SessionType, sessionID uint16) imGateAction {
	if sessionType == message.SessionUnsecured && sessionID == 0 {
		return imGateDropUnsecured
	}
	if opcode == im.OpcodeStatusResponse {
		return imGateAbsorbStatusResp
	}
	if !im.IsRequestOpcode(opcode) {
		return imGateRejectUnsupported
	}
	if sessionType == message.SessionGroup {
		switch opcode {
		case im.OpcodeReadRequest, im.OpcodeSubscribeRequest, im.OpcodeTimedRequest:
			return imGateRejectGroupSession
		}
	}
	return imGateProceed
}
