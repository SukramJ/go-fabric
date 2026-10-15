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

// DefaultMaxPayloadSize is the default maximum frame size (length
// prefix plus message). Mirrors matter.js
// packages/general/src/net/tcp/TcpConnection.ts:11
// (DEFAULT_MAX_TCP_MESSAGE_SIZE = 64_000), which TcpChannel.ts:72 and
// TcpTransport.ts:62 take as their default maxPayloadSize.
const DefaultMaxPayloadSize = 64_000

// maxReceiveBufferFactor bounds the bytes buffered without completing a
// message at this multiple of the maximum payload size. Mirrors
// TcpChannel.ts:32 (MAX_RECEIVE_BUFFER_FACTOR = 2).
const maxReceiveBufferFactor = 2

// Errors.
var (
	// ErrMessageTooLarge is returned by [Conn.Send] for a message above
	// the connection's [Conn.MaxMessageSize] (TcpChannel.ts:125-126).
	ErrMessageTooLarge = errors.New("tcp: message exceeds the TCP limit")
	// ErrConnClosed is returned by [Conn.Send] after the connection
	// closed (TcpChannel.ts:120-122).
	ErrConnClosed = errors.New("tcp: connection closed")
	// ErrZeroLengthFrame closes a connection that announced an empty
	// message: one can never hold the mandatory message header
	// (TcpChannel.ts:257-264).
	ErrZeroLengthFrame = errors.New("tcp: zero-length frame")
	// ErrFrameTooLarge closes a connection that announced a message
	// above the limit (TcpChannel.ts:266-271).
	ErrFrameTooLarge = errors.New("tcp: announced message exceeds the TCP limit")
	// ErrReceiveBufferExceeded closes a connection whose buffered bytes
	// passed twice the maximum payload size without completing a
	// message (TcpChannel.ts:211-218).
	ErrReceiveBufferExceeded = errors.New("tcp: receive buffer exceeded")
)

// encodeFrame prefixes msg with its 4-byte little-endian length.
// Mirrors TcpChannel.ts:119-133 send.
func encodeFrame(msg []byte, maxPayloadSize int) ([]byte, error) {
	if limit := maxPayloadSize - FramingHeaderSize; len(msg) > limit {
		return nil, fmt.Errorf("%w: %d > %d", ErrMessageTooLarge, len(msg), limit)
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

// maxMessageSize mirrors TcpChannel.ts:114-117.
func (a *assembler) maxMessageSize() int { return a.maxPayloadSize - FramingHeaderSize }

// push appends chunk and returns every message it completed, each in
// its own slice. A non-nil error means the connection must be closed;
// the messages completed before the offending frame are still returned,
// as matter.js delivers them before it reaches the bad header.
func (a *assembler) push(chunk []byte) ([][]byte, error) {
	a.buf = append(a.buf, chunk...)
	// Receive buffer cap, checked before extraction (TcpChannel.ts:211-218).
	if len(a.buf) > a.maxPayloadSize*maxReceiveBufferFactor {
		return nil, fmt.Errorf("%w: %d > %d bytes", ErrReceiveBufferExceeded, len(a.buf), a.maxPayloadSize*maxReceiveBufferFactor)
	}
	var out [][]byte
	for len(a.buf) >= FramingHeaderSize {
		n := int(binary.LittleEndian.Uint32(a.buf))
		if n == 0 {
			return out, ErrZeroLengthFrame
		}
		if n > a.maxMessageSize() {
			return out, fmt.Errorf("%w: %d > %d", ErrFrameTooLarge, n, a.maxMessageSize())
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
