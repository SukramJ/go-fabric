// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package wire

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"sync"
)

// The framing errors. The texts follow aioesphomeapi's, so a log reads the
// same as Home Assistant's.
var (
	// ErrRequiresEncryption is a plaintext client answered with the Noise
	// preamble: the device has an API key (_frame_helper/plain_text.py:153-158).
	ErrRequiresEncryption = errors.New("esphome: the device requires encryption (an API key)")
	// ErrPlaintextDevice is a Noise client answered with the plaintext
	// preamble: the device has no API key (_frame_helper/noise.py:160-167).
	ErrPlaintextDevice = errors.New("esphome: the device uses the plaintext protocol (no API key)")
	// ErrProtocol is a frame that breaks the framing.
	ErrProtocol = errors.New("esphome: API protocol error")
)

// MaxFrameSize bounds a frame's length: the firmware's 16-bit length, the
// plaintext path's cap (_frame_helper/plain_text.py:27-33).
const MaxFrameSize = 65535

// maxVaruintBytes caps a plaintext varuint at four bytes
// (_frame_helper/plain_text.py:15-25).
const maxVaruintBytes = 4

// Framer reads and writes API messages over one stream.
type Framer interface {
	// WriteMessage writes one message; safe for concurrent use, the
	// messages leave in the order of the calls.
	WriteMessage(m Message) error
	// ReadMessage reads the next message; one reader at a time.
	ReadMessage() (typ uint16, payload []byte, err error)
}

// Plaintext is the plaintext framing: 0x00, the payload length and the
// message type as varuints, then the payload
// (_frame_helper/packets.py:11-46 make_plain_text_packets,
// _frame_helper/plain_text.py:96-150).
type Plaintext struct {
	r *bufio.Reader
	// mu serialises writes.
	mu sync.Mutex
	w  io.Writer
}

// NewPlaintext frames over rw.
func NewPlaintext(rw io.ReadWriter) *Plaintext {
	return &Plaintext{r: bufio.NewReader(rw), w: rw}
}

// AppendPlaintext appends m's plaintext frame to b.
func AppendPlaintext(b []byte, m Message) []byte {
	payload := m.Marshal()
	b = append(b, 0x00)
	b = AppendVarint(b, uint64(len(payload)))
	b = AppendVarint(b, uint64(m.Type()))
	return append(b, payload...)
}

// WriteMessage implements Framer.
func (p *Plaintext) WriteMessage(m Message) error {
	frame := AppendPlaintext(nil, m)
	p.mu.Lock()
	defer p.mu.Unlock()
	_, err := p.w.Write(frame)
	return err
}

// ReadMessage implements Framer.
func (p *Plaintext) ReadMessage() (typ uint16, payload []byte, err error) {
	pre, err := p.r.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	var length, t uint32
	switch pre {
	case 0x00:
	case 0x01:
		return 0, nil, ErrRequiresEncryption
	default:
		return 0, nil, fmt.Errorf("%w: invalid preamble 0x%02x", ErrProtocol, pre)
	}
	length, err = readVaruint(p.r)
	if err != nil {
		return 0, nil, err
	}
	if length > MaxFrameSize {
		return 0, nil, fmt.Errorf("%w: frame length %d exceeds %d", ErrProtocol, length, MaxFrameSize)
	}
	t, err = readVaruint(p.r)
	if err != nil {
		return 0, nil, err
	}
	if t > 0xFFFF {
		return 0, nil, fmt.Errorf("%w: message type %d", ErrProtocol, t)
	}
	payload = make([]byte, length)
	if _, err := io.ReadFull(p.r, payload); err != nil {
		return 0, nil, err
	}
	return uint16(t), payload, nil
}

// readVaruint reads a varuint of at most maxVaruintBytes bytes.
func readVaruint(r io.ByteReader) (uint32, error) {
	var v uint32
	for i := range maxVaruintBytes {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		v |= uint32(b&0x7f) << (7 * i)
		if b&0x80 == 0 {
			return v, nil
		}
	}
	return 0, fmt.Errorf("%w: varuint exceeds %d bytes", ErrProtocol, maxVaruintBytes)
}
