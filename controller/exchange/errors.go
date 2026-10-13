// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package exchange

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/SukramJ/go-fabric/transport/mrp"
)

// Errors of the exchange layer. The classes follow matter.js
// packages/protocol/src/peer/PeerCommunicationError.ts and
// packages/protocol/src/protocol/errors.ts at 0ceb202d; a caller tests
// them with errors.Is.
var (
	// ErrTransient is the class of every error after which the peer may
	// still be present but cannot be reached right now — matter.js
	// TransientPeerCommunicationError (PeerCommunicationError.ts:19).
	// [PeerUnresponsiveError], [PeerMessageMissingError] and
	// [StatusReportError] are transient.
	ErrTransient = errors.New("exchange: transient peer communication error")

	// ErrNetwork wraps a failure of the socket to send — matter.js
	// NetworkError.
	ErrNetwork = errors.New("exchange: network error")

	// ErrRetransmissionLimit is matter.js RetransmissionLimitReachedError
	// (errors.ts:17, deprecated there and thrown by no exchange at
	// 0ceb202d). It is kept so the handshakes' "do not report an error to
	// a peer we cannot reach" rule names the same three classes matter.js
	// names (PaseClient.ts:101, CaseClient.ts:89).
	ErrRetransmissionLimit = errors.New("exchange: retransmission limit reached")

	// ErrFlow is a call the exchange's state forbids — a second send
	// while the previous message is unacknowledged (MessageExchange.ts:669-673),
	// a message that fails to acknowledge our pending one
	// (MessageExchange.ts:493-504) — matter.js MatterFlowError.
	ErrFlow = errors.New("exchange: flow error")

	// ErrExchangeClosed is returned by a call on a closed exchange
	// (MessageExchange.ts:606-608, ClosedError).
	ErrExchangeClosed = errors.New("exchange: exchange closed")

	// ErrSessionClosed is returned by a call on a closed session
	// (matter.js SessionClosedError, errors.ts:35).
	ErrSessionClosed = errors.New("exchange: session closed")

	// ErrUnexpectedMessage is a reply the exchange did not expect — an
	// ack-only send answered with data (MessageExchange.ts:815-817).
	ErrUnexpectedMessage = errors.New("exchange: unexpected message")

	// ErrNoSessionID is returned when every session id is taken and no
	// session can be closed to free one (SessionManager.ts:515-523).
	ErrNoSessionID = errors.New("exchange: no session id available")
)

// PeerUnresponsiveError is a reliable message the peer never acknowledged
// within MaxTransmissions transmissions and the final wait — matter.js
// PeerUnresponsiveError (PeerCommunicationError.ts:40-46), raised by
// MessageExchange.ts:763-765.
type PeerUnresponsiveError struct {
	// Waited is how long the exchange waited for the acknowledgement.
	Waited time.Duration
}

func (e *PeerUnresponsiveError) Error() string {
	return fmt.Sprintf("exchange: peer is no longer responding (timed out after %s)", e.Waited)
}

// Is makes the error match [ErrTransient].
func (e *PeerUnresponsiveError) Is(target error) bool { return target == ErrTransient }

// PeerMessageMissingError is an expected message that did not arrive on
// the exchange before the timeout — matter.js PeerMessageMissingError
// (PeerCommunicationError.ts:57), raised by MessageExchange.ts:854-856.
type PeerMessageMissingError struct {
	// Timeout is the wait that expired.
	Timeout time.Duration
}

func (e *PeerMessageMissingError) Error() string {
	return fmt.Sprintf("exchange: expected message missing after %s", e.Timeout)
}

// Is makes the error match [ErrTransient] and a [*PeerUnresponsiveError]
// target, as the matter.js class extends PeerUnresponsiveError.
func (e *PeerMessageMissingError) Is(target error) bool {
	if target == ErrTransient {
		return true
	}
	_, ok := target.(*PeerUnresponsiveError)
	return ok
}

// PeerInitiatedCloseError is the cause a session ends with when the peer
// closed it with a CloseSession StatusReport — matter.js
// PeerInitiatedCloseError (PeerCommunicationError.ts:71-75), raised by
// NodeSession.handlePeerClose (NodeSession.ts:305-309). It is transient.
type PeerInitiatedCloseError struct{}

func (*PeerInitiatedCloseError) Error() string { return "exchange: session ended by peer" }

// Is makes the error match [ErrTransient].
func (*PeerInitiatedCloseError) Is(target error) bool { return target == ErrTransient }

// StatusReport is a Secure Channel StatusReport body: general code,
// protocol id, protocol code and optional protocol data, little-endian —
// the layout transport/mrp.EncodeStatusReport writes.
type StatusReport struct {
	GeneralCode  uint16
	ProtocolID   uint32
	ProtocolCode uint16
	ProtocolData []byte
}

// DecodeStatusReport splits a StatusReport body. Mirrors matter.js
// packages/protocol/src/protocol/ProtocolStatusMessage.ts:50-66
// (decodeInternal) at 0ceb202d, which SecureChannelStatusMessageSchema.ts:14
// instantiates for the Secure Channel; ProtocolID carries the vendor id
// in its upper 16 bits as on the wire. It is the decoder
// bridge/bridgetest/transport.go:217 carries for its own use. ok is false
// for a body shorter than 8 bytes.
func DecodeStatusReport(payload []byte) (StatusReport, bool) {
	if len(payload) < 8 {
		return StatusReport{}, false
	}
	r := StatusReport{
		GeneralCode:  binary.LittleEndian.Uint16(payload[0:2]),
		ProtocolID:   binary.LittleEndian.Uint32(payload[2:6]),
		ProtocolCode: binary.LittleEndian.Uint16(payload[6:8]),
	}
	if len(payload) > 8 {
		r.ProtocolData = append([]byte(nil), payload[8:]...)
	}
	return r, true
}

// Encode is the wire body of r.
func (r StatusReport) Encode() []byte {
	return mrp.EncodeStatusReport(r.GeneralCode, r.ProtocolID, r.ProtocolCode, r.ProtocolData)
}

// StatusReportError is a StatusReport the peer answered with that is not
// Success — matter.js ChannelStatusResponseError
// (SecureChannelMessenger.ts:32-52, thrown at :234-259). It is transient.
type StatusReportError struct {
	Report StatusReport
}

func (e *StatusReportError) Error() string {
	return fmt.Sprintf("exchange: peer status report general 0x%04X protocol 0x%08X code 0x%04X",
		e.Report.GeneralCode, e.Report.ProtocolID, e.Report.ProtocolCode)
}

// Is makes the error match [ErrTransient].
func (e *StatusReportError) Is(target error) bool { return target == ErrTransient }

// CheckStatusReport returns a [*StatusReportError] unless payload is a
// StatusReport with general code Success and protocol code Success
// (SESSION_ESTABLISHMENT_SUCCESS) — SecureChannelMessenger.ts:234-259
// throwIfErrorStatusReport. A body too short to decode is an
// [ErrUnexpectedMessage].
func CheckStatusReport(payload []byte) error {
	r, ok := DecodeStatusReport(payload)
	if !ok {
		return fmt.Errorf("%w: StatusReport body of %d bytes", ErrUnexpectedMessage, len(payload))
	}
	if r.GeneralCode != mrp.SCStatusGeneralSuccess || r.ProtocolCode != mrp.SCStatusProtocolSessionEstablishmentSuccess {
		return &StatusReportError{Report: r}
	}
	return nil
}
