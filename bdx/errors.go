// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bdx

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Error is a BDX failure carrying the status code a StatusReport reports it
// with. A transfer that ends in an Error sends that StatusReport to the peer
// (BdxSession.ts:186-196) and records how far it came.
//
// Mirrors matter.js packages/protocol/src/bdx/BdxError.ts:11-28.
type Error struct {
	Code StatusCode
	Msg  string
	// BytesTransferred and TotalBytesLength are filled in by the session
	// when the transfer fails (BdxSession.ts:193-194); TotalBytesLength is
	// nil while the size is unknown.
	BytesTransferred uint64
	TotalBytesLength *uint64
}

func newError(code StatusCode, format string, args ...any) *Error {
	return &Error{Code: code, Msg: fmt.Sprintf(format, args...)}
}

func (e *Error) Error() string {
	return fmt.Sprintf("BDX error %s(%d): %s", e.Code, uint16(e.Code), e.Msg)
}

// String names the status code as matter.js does.
func (c StatusCode) String() string {
	switch c {
	case StatusSuccess:
		return "Success"
	case StatusLengthTooLarge:
		return "LengthTooLarge"
	case StatusLengthTooShort:
		return "LengthTooShort"
	case StatusLengthMismatch:
		return "LengthMismatch"
	case StatusLengthRequired:
		return "LengthRequired"
	case StatusBadMessageContent:
		return "BadMessageContent"
	case StatusBadBlockCounter:
		return "BadBlockCounter"
	case StatusUnexpectedMessage:
		return "UnexpectedMessage"
	case StatusResponderBusy:
		return "ResponderBusy"
	case StatusTransferFailedUnknownError:
		return "TransferFailedUnknownError"
	case StatusTransferMethodNotSupported:
		return "TransferMethodNotSupported"
	case StatusFileDesignatorUnknown:
		return "FileDesignatorUnknown"
	case StatusStartOffsetNotSupported:
		return "StartOffsetNotSupported"
	case StatusVersionNotSupported:
		return "VersionNotSupported"
	case StatusUnknown:
		return "Unknown"
	case StatusGeneralError:
		return "GeneralError"
	}
	return fmt.Sprintf("0x%x", uint16(c))
}

// StatusResponseError is a failure StatusReport the peer sent.
//
// Mirrors matter.js BdxError.ts:31-41 (BdxStatusResponseError).
type StatusResponseError struct {
	GeneralStatus  uint16
	ProtocolStatus StatusCode
	Msg            string
}

func (e *StatusResponseError) Error() string {
	return fmt.Sprintf("(general %d / %s (%d)) %s", e.GeneralStatus, e.ProtocolStatus, uint16(e.ProtocolStatus), e.Msg)
}

// AsError returns the *Error in err's chain.
func AsError(err error) (*Error, bool) {
	var e *Error
	ok := errors.As(err, &e)
	return e, ok
}

// Secure Channel framing of a StatusReport: protocol 0, opcode 0x40. A BDX
// exchange sends its StatusReport on the Secure Channel protocol, as every
// matter.js exchange does (MessageExchange.ts:665: `isStandaloneAck ||
// isStatusReport ? SECURE_CHANNEL_PROTOCOL_ID : this.#protocolId`).
const (
	SecureChannelProtocolID uint16 = 0
	OpcodeStatusReport      uint8  = 0x40
)

// General status codes (matter.js packages/types/src/protocol/definitions/
// general.ts:7-12).
const (
	GeneralSuccess uint16 = 0
	GeneralFailure uint16 = 1
)

// StatusReport is a StatusReport with the BDX protocol id.
//
// Mirrors matter.js schema/BdxStatusMessageSchema.ts:14 (a
// ProtocolStatusMessageSchema for BDX_PROTOCOL_ID that allows no
// protocol-specific data) and ProtocolStatusMessage.ts:39-66: general
// status u16, vendor-qualified protocol id u32, protocol status u16, all
// little-endian.
type StatusReport struct {
	GeneralStatus  uint16
	ProtocolID     uint16
	VendorID       uint16
	ProtocolStatus StatusCode
}

// EncodeStatusReport encodes a BDX StatusReport.
func EncodeStatusReport(general uint16, code StatusCode) []byte {
	out := binary.LittleEndian.AppendUint16(nil, general)
	out = binary.LittleEndian.AppendUint32(out, uint32(ProtocolID))
	return binary.LittleEndian.AppendUint16(out, uint16(code))
}

// DecodeStatusReport decodes a StatusReport payload; trailing
// protocol-specific data is ignored.
func DecodeStatusReport(b []byte) (StatusReport, error) {
	r := reader{b: b}
	general := r.u16()
	vp := r.u32()
	code := r.u16()
	if r.err != nil {
		return StatusReport{}, r.err
	}
	return StatusReport{
		GeneralStatus:  general,
		ProtocolID:     uint16(vp & 0xFFFF), //nolint:gosec // masked
		VendorID:       uint16(vp >> 16),    //nolint:gosec // shifted out of 32 bits
		ProtocolStatus: StatusCode(code),
	}, nil
}
