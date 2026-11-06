// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package tcp

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// FramingHeaderSize is the size of the length prefix in front of every
// message. Mirrors matter.js
// packages/protocol/src/transport/tcp/TcpChannel.ts:28
// (FRAMING_HEADER_SIZE = 4).
const FramingHeaderSize = 4

// DefaultMaxPayloadSize is the default MAX_TCP_MESSAGE_SIZE: the largest
// Matter message carried over TCP, the 4-byte length prefix not counted.
// Mirrors matter.js packages/general/src/net/tcp/TcpConnection.ts:10-15 at
// db021da0 (DEFAULT_MAX_TCP_MESSAGE_SIZE = 64_000), which TcpChannel.ts:69
// takes as its default maxPayloadSize — "the maximum message size
// excluding the 4-byte length header, as MAX_TCP_MESSAGE_SIZE defines it"
// (:37-41). Until the db021da0 lift (matter.js #4793) the prefix counted
// against it, and a message of 63997 to 64000 bytes was refused.
const DefaultMaxPayloadSize = 64_000

// Errors.
var (
	// ErrMessageTooLarge is returned by [Conn.Send] for a message above
	// the connection's [Conn.MaxMessageSize] — matter.js
	// MessageTooLargeError (TcpChannel.ts:116-120 at db021da0).
	ErrMessageTooLarge = errors.New("tcp: message exceeds the TCP limit")
	// ErrConnClosed is returned by [Conn.Send] after the connection
	// closed (TcpChannel.ts:120-122).
	ErrConnClosed = errors.New("tcp: connection closed")
	// ErrZeroLengthFrame closes a connection that announced an empty
	// message: one can never hold the mandatory message header
	// (TcpChannel.ts:257-264).
	ErrZeroLengthFrame = errors.New("tcp: zero-length frame")
	// ErrFrameTooLarge closes a connection that announced a message
	// above the limit (TcpChannel.ts:251-256 at db021da0).
	ErrFrameTooLarge = errors.New("tcp: announced message exceeds the TCP limit")
)

// encodeFrame prefixes msg with its 4-byte little-endian length; msg may
// carry maxPayloadSize bytes. Mirrors TcpChannel.ts:110-127 send at
// db021da0.
func encodeFrame(msg []byte, maxPayloadSize int) ([]byte, error) {
	if len(msg) > maxPayloadSize {
		return nil, fmt.Errorf("%w: %d > %d", ErrMessageTooLarge, len(msg), maxPayloadSize)
	}
	frame := make([]byte, FramingHeaderSize+len(msg))
	binary.LittleEndian.PutUint32(frame, uint32(len(msg))) //nolint:gosec // bounded by maxPayloadSize above
	copy(frame[FramingHeaderSize:], msg)
	return frame, nil
}

// assembler reassembles the messages of one connection from the byte
// chunks the socket delivers. The Go translation of TcpChannel.ts's
// #handleData (:202-220) and #extractMessages (:248-298).
type assembler struct {
	maxPayloadSize int
	buf            []byte
}

func newAssembler(maxPayloadSize int) *assembler {
	return &assembler{maxPayloadSize: maxPayloadSize}
}

// push appends chunk and returns every message it completed, each in
// its own slice. A non-nil error means the connection must be closed;
// the messages completed before the offending frame are still returned,
// as matter.js delivers them before it reaches the bad header.
func (a *assembler) push(chunk []byte) ([][]byte, error) {
	// No cap on the buffered bytes: a header announcing more than the
	// limit closes the connection, which bounds an incomplete message, and
	// several complete frames in one read are all taken (TcpChannel.ts:204
	// at db021da0; the cap of twice the limit closed such a connection
	// until the db021da0 lift).
	a.buf = append(a.buf, chunk...)
	var out [][]byte
	for len(a.buf) >= FramingHeaderSize {
		n := int(binary.LittleEndian.Uint32(a.buf))
		if n == 0 {
			return out, ErrZeroLengthFrame
		}
		if n > a.maxPayloadSize {
			return out, fmt.Errorf("%w: %d > %d", ErrFrameTooLarge, n, a.maxPayloadSize)
		}
		total := FramingHeaderSize + n
		if len(a.buf) < total {
			break
		}
		msg := make([]byte, n)
		copy(msg, a.buf[FramingHeaderSize:total])
		out = append(out, msg)
		a.buf = a.buf[total:]
	}
	if len(a.buf) == 0 {
		a.buf = nil
	}
	return out, nil
}
