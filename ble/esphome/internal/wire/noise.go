// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package wire

// The Noise transport of the ESPHome native API, both ends, in this one
// file. The client side follows aioesphomeapi
// (aioesphomeapi/_frame_helper/noise.py and noise_encryption.py at
// 3b2a4a2d, MIT) step by step; the server side is the same wire seen from
// the device, for the fake proxy of esphometest.
//
// The handshake is Noise_NNpsk0_25519_ChaChaPoly_SHA256
// (noise_encryption.py:44) with the client as initiator (noise.py:299), the
// API key as the pre-shared key (noise.py:300, decoded by
// noise_encryption.py:51-65) and the prologue "NoiseAPIInit\x00\x00"
// (noise.py:301). github.com/flynn/noise runs the pattern; its CipherState
// is the transport cipher: ChaCha20-Poly1305, empty associated data, the
// nonce four zero bytes and the 64-bit little-endian message counter, one
// counter per direction, incremented per frame — what noise_encryption.py's
// EncryptCipher and DecryptCipher do (:18, :68-103).

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/flynn/noise"
)

// The Noise errors, after aioesphomeapi's.
var (
	// ErrInvalidKey is a handshake the device refused for the key:
	// "Handshake MAC failure" or a response that fails authentication
	// (noise.py:305-323, :336-352).
	ErrInvalidKey = errors.New("esphome: invalid encryption key")
	// ErrHandshake is any other failed Noise handshake.
	ErrHandshake = errors.New("esphome: Noise handshake failed")
	// ErrMalformedKey is an API key that is not base64 of 32 bytes
	// (noise_encryption.py:51-65).
	ErrMalformedKey = errors.New("esphome: malformed API key, expected base64 of 32 bytes")
)

// noiseHello is the client hello: an empty frame (noise.py:50).
var noiseHello = []byte{0x01, 0x00, 0x00}

// prologueInit is the prologue's fixed part; the device appends the client
// hello's length (two bytes, big-endian) and contents, so an empty hello
// gives noise.py:301's "NoiseAPIInit\x00\x00".
const prologueInit = "NoiseAPIInit"

// macFailure is the device's reject text for a wrong key (noise.py:310).
const macFailure = "Handshake MAC failure"

// noiseOverhead is the inner header (type, length) plus the AEAD tag.
const noiseOverhead = 4 + 16

func cipherSuite() noise.CipherSuite {
	return noise.NewCipherSuite(noise.DH25519, noise.CipherChaChaPoly, noise.HashSHA256)
}

// DecodeKey decodes an API key: standard base64 of exactly 32 bytes
// (noise_encryption.py:51-65; whitespace is ignored as binascii.a2b_base64
// ignores it).
func DecodeKey(key string) ([]byte, error) {
	k, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(key), ""))
	if err != nil || len(k) != 32 {
		return nil, ErrMalformedKey
	}
	return k, nil
}

// ServerHello is what the device's hello frame names: its name and MAC,
// each when present (noise.py:207-275).
type ServerHello struct {
	Name string
	MAC  string
}

// Noise is an established Noise transport; it implements Framer.
type Noise struct {
	r *bufio.Reader
	// mu serialises writes: the encryption counter must advance in the
	// order the frames leave.
	mu  sync.Mutex
	w   io.Writer
	enc *noise.CipherState
	dec *noise.CipherState
}

// readFrame reads one Noise frame: 0x01, a 16-bit big-endian length, the
// frame (noise.py:151-195). A plaintext preamble is ErrPlaintextDevice.
func readFrame(r *bufio.Reader) ([]byte, error) {
	var hdr [3]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	switch hdr[0] {
	case 0x01:
	case 0x00:
		return nil, ErrPlaintextDevice
	default:
		return nil, fmt.Errorf("%w: marker byte invalid: %d", ErrProtocol, hdr[0])
	}
	frame := make([]byte, binary.BigEndian.Uint16(hdr[1:]))
	if _, err := io.ReadFull(r, frame); err != nil {
		return nil, err
	}
	return frame, nil
}

// appendFrame is the Noise frame of data.
func appendFrame(data []byte) []byte {
	b := []byte{0x01}
	b = binary.BigEndian.AppendUint16(b, uint16(len(data))) //nolint:gosec // callers bound data to MaxFrameSize
	return append(b, data...)
}

// ClientHandshake runs the client's side of the handshake over rw with the
// decoded key psk: the hello and the first handshake message in one write
// (noise.py:197-205: NOISE_HELLO, then a frame of the indicator byte 0x00
// and the Noise message), the device's hello (noise.py:207-275), its
// handshake message (byte 0x00 and the Noise message, or a non-zero byte
// and the reason; noise.py:325-362). rng is the source of the ephemeral
// key; nil is crypto/rand.
func ClientHandshake(rw io.ReadWriter, psk []byte, rng io.Reader) (*Noise, ServerHello, error) {
	var hello ServerHello
	hs, err := noise.NewHandshakeState(noise.Config{
		CipherSuite: cipherSuite(), Random: rng, Pattern: noise.HandshakeNN, Initiator: true,
		Prologue: []byte(prologueInit + "\x00\x00"), PresharedKey: psk, PresharedKeyPlacement: 0,
	})
	if err != nil {
		return nil, hello, fmt.Errorf("%w: %w", ErrHandshake, err)
	}
	msg1, _, _, err := hs.WriteMessage(nil, nil)
	if err != nil {
		return nil, hello, fmt.Errorf("%w: %w", ErrHandshake, err)
	}
	out := append(append([]byte(nil), noiseHello...), appendFrame(append([]byte{0x00}, msg1...))...)
	if _, err := rw.Write(out); err != nil {
		return nil, hello, err
	}
	r := bufio.NewReader(rw)
	sh, err := readFrame(r)
	if err != nil {
		return nil, hello, err
	}
	if len(sh) == 0 {
		return nil, hello, fmt.Errorf("%w: ServerHello is empty", ErrHandshake)
	}
	if sh[0] != 0x01 {
		return nil, hello, fmt.Errorf("%w: unknown protocol selected by the device: %d", ErrHandshake, sh[0])
	}
	if i := bytes.IndexByte(sh[1:], 0); i >= 0 {
		hello.Name = string(sh[1 : 1+i])
		rest := sh[2+i:]
		if j := bytes.IndexByte(rest, 0); j >= 0 {
			hello.MAC = string(rest[:j])
		}
	}
	msg2, err := readFrame(r)
	if err != nil {
		return nil, hello, err
	}
	if len(msg2) == 0 {
		return nil, hello, fmt.Errorf("%w: handshake frame is empty", ErrHandshake)
	}
	if msg2[0] != 0x00 {
		if string(msg2[1:]) == macFailure {
			return nil, hello, ErrInvalidKey
		}
		return nil, hello, fmt.Errorf("%w: %q", ErrHandshake, msg2[1:])
	}
	_, enc, dec, err := hs.ReadMessage(nil, msg2[1:])
	if err != nil {
		// A response that fails authentication is a wrong key
		// (noise.py:336-352).
		return nil, hello, fmt.Errorf("%w: %w", ErrInvalidKey, err)
	}
	if enc == nil || dec == nil {
		return nil, hello, fmt.Errorf("%w: handshake not complete", ErrHandshake)
	}
	return &Noise{r: r, w: rw, enc: enc, dec: dec}, hello, nil
}

// ServerHandshake runs the device's side over rw for the fake proxy: it
// reads the client hello, whose length and contents complete the prologue,
// sends the device's hello (0x01, the name, 0, the MAC, 0), reads the first
// handshake message and answers with the second, or with the reject frame
// (a non-zero byte and the reason) when the key does not match.
func ServerHandshake(rw io.ReadWriter, psk []byte, hello ServerHello, rng io.Reader) (*Noise, error) {
	r := bufio.NewReader(rw)
	ch, err := readFrame(r)
	if err != nil {
		if errors.Is(err, ErrPlaintextDevice) {
			// A plaintext client: answer with the Noise preamble, which
			// it reads as "requires encryption".
			_, _ = rw.Write(appendFrame(append([]byte{0x01}, "Bad indicator byte"...)))
		}
		return nil, err
	}
	prologue := binary.BigEndian.AppendUint16([]byte(prologueInit), uint16(len(ch))) //nolint:gosec // a frame is at most 65535 bytes
	prologue = append(prologue, ch...)
	sh := append([]byte{0x01}, hello.Name...)
	sh = append(append(sh, 0), hello.MAC...)
	sh = append(sh, 0)
	if _, err := rw.Write(appendFrame(sh)); err != nil {
		return nil, err
	}
	hs, err := noise.NewHandshakeState(noise.Config{
		CipherSuite: cipherSuite(), Random: rng, Pattern: noise.HandshakeNN, Initiator: false,
		Prologue: prologue, PresharedKey: psk, PresharedKeyPlacement: 0,
	})
	if err != nil {
		return nil, err
	}
	reject := func(reason string) error {
		_, _ = rw.Write(appendFrame(append([]byte{0x01}, reason...)))
		return fmt.Errorf("%w: %s", ErrHandshake, reason)
	}
	msg1, err := readFrame(r)
	if err != nil {
		return nil, err
	}
	switch {
	case len(msg1) == 0:
		return nil, reject("Empty handshake message")
	case msg1[0] != 0x00:
		return nil, reject("Bad handshake error byte")
	}
	if _, _, _, err := hs.ReadMessage(nil, msg1[1:]); err != nil {
		_ = reject(macFailure)
		return nil, ErrInvalidKey
	}
	msg2, dec, enc, err := hs.WriteMessage(nil, nil)
	if err != nil {
		return nil, err
	}
	if _, err := rw.Write(appendFrame(append([]byte{0x00}, msg2...))); err != nil {
		return nil, err
	}
	return &Noise{r: r, w: rw, enc: enc, dec: dec}, nil
}

// WriteMessage implements Framer: the type and the length, big-endian, and
// the payload, encrypted, in one frame (noise.py:62-84
// make_noise_packets).
func (n *Noise) WriteMessage(m Message) error {
	payload := m.Marshal()
	if len(payload)+noiseOverhead > MaxFrameSize {
		return fmt.Errorf("%w: message of %d bytes exceeds a frame", ErrProtocol, len(payload))
	}
	inner := binary.BigEndian.AppendUint16(nil, m.Type())
	inner = binary.BigEndian.AppendUint16(inner, uint16(len(payload))) //nolint:gosec // bounded above
	inner = append(inner, payload...)
	n.mu.Lock()
	defer n.mu.Unlock()
	ct, err := n.enc.Encrypt(nil, nil, inner)
	if err != nil {
		return err
	}
	_, err = n.w.Write(appendFrame(ct))
	return err
}

// ReadMessage implements Framer: the frame decrypted, the type read and the
// inner length ignored, as the client ignores it (noise.py:375-414).
func (n *Noise) ReadMessage() (typ uint16, payload []byte, err error) {
	frame, err := readFrame(n.r)
	if err != nil {
		return 0, nil, err
	}
	var msg []byte
	msg, err = n.dec.Decrypt(nil, nil, frame)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: decryption failed: %w", ErrProtocol, err)
	}
	if len(msg) < 4 {
		return 0, nil, fmt.Errorf("%w: decrypted message too short: %d bytes", ErrProtocol, len(msg))
	}
	return binary.BigEndian.Uint16(msg), msg[4:], nil
}
