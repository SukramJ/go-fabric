// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package handshake

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// Errors of the handshakes.
var (
	// ErrUnexpectedData is a handshake message that is not what the step
	// expects — the wrong message type, a payload that does not decode, a
	// missing field — matter.js UnexpectedDataError / MatterFlowError
	// (SecureChannelMessenger.ts:95-98, :130-137; CaseMessenger.ts:55-58).
	ErrUnexpectedData = errors.New("handshake: unexpected data")

	// ErrPasscodeMismatch is the responder's PASE key confirmation (Pake2
	// cB) not verifying: the passcode is wrong for the device at this
	// address — matter.js PasscodeMismatchError (PaseClient.ts:35-41,
	// thrown at :186-190).
	ErrPasscodeMismatch = errors.New("handshake: PASE key confirmation does not match: the passcode is wrong for this device")
)

// Processing times of the Secure Channel messenger. Mirrors matter.js
// packages/protocol/src/securechannel/SecureChannelMessenger.ts:55
// (EXPECTED_CRYPTO_PROCESSING_TIME = 35 s, the messenger's default) and
// :58 (DEFAULT_NORMAL_PROCESSING_TIME = 2 s, used for the PBKDF
// parameter exchange, PaseMessenger.ts:84-96) at 0ceb202d.
const (
	ExpectedCryptoProcessingTime = 35 * time.Second
	DefaultNormalProcessingTime  = 2 * time.Second
)

// statusInvalidParam is the Secure Channel protocol code INVALID_PARAMETER.
const statusInvalidParam = mrp.SCStatusProtocolInvalidParameter

// nextMessage waits for the next message on x and checks it: a
// StatusReport that is not Success is a [*exchange.StatusReportError],
// any other message than want an [ErrUnexpectedData]. Mirrors matter.js
// SecureChannelMessenger.ts:83-100 (nextMessage, throwIfErrorStatusReport).
func nextMessage(ctx context.Context, x *exchange.Exchange, want uint8, ept time.Duration) (*exchange.Message, error) {
	msg, err := x.Next(ctx, exchange.ReceiveOptions{ExpectedProcessingTime: ept})
	if err != nil {
		return nil, err
	}
	if err := checkStatus(msg); err != nil {
		return nil, err
	}
	if msg.Opcode != want {
		return nil, fmt.Errorf("%w: message type 0x%02X, expected 0x%02X", ErrUnexpectedData, msg.Opcode, want)
	}
	return msg, nil
}

// checkStatus fails on a StatusReport that is not Success.
func checkStatus(msg *exchange.Message) error {
	if msg.ProtocolID != mrp.SecureChannelProtocolID || msg.Opcode != mrp.SCOpcodeStatusReport {
		return nil
	}
	return exchange.CheckStatusReport(msg.Payload)
}

// sendStatus sends a reliable Secure Channel StatusReport — matter.js
// SecureChannelMessenger.ts:170-176, :209-232 (sendError, sendSuccess).
func sendStatus(ctx context.Context, x *exchange.Exchange, general, code uint16) error {
	report := exchange.StatusReport{GeneralCode: general, ProtocolID: uint32(mrp.SecureChannelProtocolID), ProtocolCode: code}
	return x.Send(ctx, mrp.SCOpcodeStatusReport, report.Encode(), exchange.SendOptions{})
}

// reportsFailureToPeer reports whether a handshake that failed with err
// tells the peer with a StatusReport InvalidParam: not when our last
// message is unacknowledged (the send would be a flow error) and not for
// a network, transient or retransmission-limit error — the peer cannot
// be reached, or it reported the failure itself. Mirrors matter.js
// PaseClient.ts:94-113 and CaseClient.ts:83-99.
func reportsFailureToPeer(x *exchange.Exchange, err error) bool {
	return !x.HasUnackedMessage() &&
		!errors.Is(err, exchange.ErrNetwork) &&
		!errors.Is(err, exchange.ErrTransient) &&
		!errors.Is(err, exchange.ErrRetransmissionLimit)
}
