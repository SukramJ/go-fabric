// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package tcp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

// TestFramingConstantsMirrorMatterJS pins the constants to the values
// read in matter.js: TcpChannel.ts:28 (FRAMING_HEADER_SIZE = 4), :32
// (MAX_RECEIVE_BUFFER_FACTOR = 2), packages/general/src/net/tcp/TcpConnection.ts:11
// (DEFAULT_MAX_TCP_MESSAGE_SIZE = 64_000), TcpTransport.ts:28 and :31.
func TestFramingConstantsMirrorMatterJS(t *testing.T) {
	if FramingHeaderSize != 4 || maxReceiveBufferFactor != 2 || DefaultMaxPayloadSize != 64_000 {
		t.Fatalf("framing constants drifted: header=%d factor=%d max=%d", FramingHeaderSize, maxReceiveBufferFactor, DefaultMaxPayloadSize)
	}
	if MaxConnectionsPerPeerIP != 3 || NewConnectionIdleTimeout.Seconds() != 10 {
		t.Fatalf("transport constants drifted: cap=%d idle=%v", MaxConnectionsPerPeerIP, NewConnectionIdleTimeout)
	}
}

func frameOf(msg []byte) []byte {
	f := make([]byte, 4+len(msg))
	binary.LittleEndian.PutUint32(f, uint32(len(msg))) //nolint:gosec // test sizes are small
	copy(f[4:], msg)
	return f
}

func TestEncodeFrameLittleEndianLength(t *testing.T) {
	got, err := encodeFrame([]byte{0xAA, 0xBB, 0xCC}, DefaultMaxPayloadSize)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x03, 0x00, 0x00, 0x00, 0xAA, 0xBB, 0xCC}
	if !bytes.Equal(got, want) {
		t.Fatalf("frame = % X, want % X", got, want)
	}
}

func TestEncodeFrameRejectsAboveMaxMessageSize(t *testing.T) {
	// maxMessageSize = maxPayloadSize - 4 (TcpChannel.ts:114-117).
	if _, err := encodeFrame(make([]byte, 12), 16); err != nil {
		t.Fatalf("12 bytes at max payload 16 must fit: %v", err)
	}
	if _, err := encodeFrame(make([]byte, 13), 16); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("13 bytes at max payload 16: err = %v, want ErrMessageTooLarge", err)
	}
}

func TestAssemblerRoundTripAcrossChunks(t *testing.T) {
	a := newAssembler(DefaultMaxPayloadSize)
	stream := append(frameOf([]byte("first")), frameOf([]byte("second message"))...)
	var got [][]byte
	// Feed one byte at a time: every split point of header and body.
	for i := range stream {
		msgs, err := a.push(stream[i : i+1])
		if err != nil {
			t.Fatalf("push byte %d: %v", i, err)
		}
		got = append(got, msgs...)
	}
	if len(got) != 2 || string(got[0]) != "first" || string(got[1]) != "second message" {
		t.Fatalf("got %q", got)
	}
	if a.buf != nil {
		t.Fatalf("assembler kept %d bytes after complete frames", len(a.buf))
	}
}

func TestAssemblerSeveralFramesInOneChunk(t *testing.T) {
	a := newAssembler(DefaultMaxPayloadSize)
	chunk := append(append(frameOf([]byte{1}), frameOf([]byte{2, 2})...), 0x05, 0x00) // third frame's header begun
	msgs, err := a.push(chunk)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || len(a.buf) != 2 {
		t.Fatalf("msgs=%d buffered=%d", len(msgs), len(a.buf))
	}
}

func TestAssemblerZeroLengthFrameCloses(t *testing.T) {
	a := newAssembler(DefaultMaxPayloadSize)
	msgs, err := a.push(append(frameOf([]byte{7}), 0, 0, 0, 0))
	if !errors.Is(err, ErrZeroLengthFrame) {
		t.Fatalf("err = %v, want ErrZeroLengthFrame", err)
	}
	// The frame before the bad header is still delivered.
	if len(msgs) != 1 {
		t.Fatalf("msgs = %d, want 1", len(msgs))
	}
}

func TestAssemblerOversizedAnnouncedLengthCloses(t *testing.T) {
	a := newAssembler(16)
	if _, err := a.push(frameOf(make([]byte, 12))); err != nil {
		t.Fatalf("12-byte message at max payload 16: %v", err)
	}
	hdr := make([]byte, 4)
	binary.LittleEndian.PutUint32(hdr, 13)
	if _, err := a.push(hdr); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("err = %v, want ErrFrameTooLarge", err)
	}
	// A malformed length far beyond the limit (0xFFFFFFFF).
	b := newAssembler(DefaultMaxPayloadSize)
	if _, err := b.push([]byte{0xFF, 0xFF, 0xFF, 0xFF}); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("err = %v, want ErrFrameTooLarge", err)
	}
}

func TestAssemblerReceiveBufferBound(t *testing.T) {
	// matter.js checks the cap before extracting (TcpChannel.ts:211-218):
	// a chunk that pushes the buffer past 2 × maxPayloadSize closes the
	// connection even when it holds complete frames.
	a := newAssembler(16)
	if _, err := a.push(make([]byte, 33)); !errors.Is(err, ErrReceiveBufferExceeded) {
		t.Fatalf("33 bytes at cap 32: err = %v, want ErrReceiveBufferExceeded", err)
	}
	b := newAssembler(16)
	chunk := bytes.Repeat(frameOf(make([]byte, 4)), 4) // 32 bytes: at the cap, not past it
	msgs, err := b.push(chunk)
	if err != nil || len(msgs) != 4 {
		t.Fatalf("32 bytes at cap 32: msgs=%d err=%v", len(msgs), err)
	}
}
