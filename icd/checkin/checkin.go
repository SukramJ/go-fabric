// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package checkin is the Check-In Protocol message codec and its counter
// validation (Matter Core §4.22.3), the payload an intermittently connected
// device sends unsecured as Secure Channel opcode 0x50 and a registered
// client decrypts with the key it registered.
//
// It mirrors matter.js packages/protocol/src/icd/CheckInMessage.ts at
// 862ad75f, the class CheckInMessage (:30-143) and its namespace types
// (:145-180): the static methods become package functions, the crypto
// port is crypto/hmac + crypto/sha256 for the nonce and the module's
// AES-CCM-128 (secure/aesccm) for the AEAD, and matter.js's
// UnexpectedDataError is ErrMalformed.
//
// Wire layout of a payload (CheckInMessage.ts:25):
//
//	nonce(13) ‖ AES-128-CCM(counter LE32 ‖ applicationData) ‖ MIC(16)
//
// with nonce = the first 13 bytes of HMAC-SHA256(key, counter LE32).
package checkin

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/SukramJ/go-fabric/secure/aesccm"
)

// The payload's fixed lengths (CheckInMessage.ts:10-13).
const (
	// NonceLength is the nonce prefix of a payload, NONCE_LENGTH.
	NonceLength = 13
	// MICLength is the AES-CCM tag that ends a payload, MIC_LENGTH.
	MICLength = 16
	// CounterLength is the little-endian counter that opens the
	// plaintext, COUNTER_LENGTH.
	CounterLength = 4
	// MinPayloadLength is the shortest payload Decode accepts: a nonce,
	// a counter and a tag with no application data, MIN_PAYLOAD_LENGTH.
	MinPayloadLength = NonceLength + CounterLength + MICLength
)

// KeyRefreshOffset is the counter offset from a key's starting counter at
// which the client must refresh the key (Core §4.22.3.4.1,
// KEY_REFRESH_OFFSET, CheckInMessage.ts:20).
const KeyRefreshOffset uint32 = 0x80000000

// ErrMalformed reports a payload that is too short, does not authenticate
// under the key, or carries a nonce that is not the counter's — every
// failure matter.js raises as UnexpectedDataError (CheckInMessage.ts:61-63,
// :69-73, :79-82, :108-112). A receiver trying several keys treats it as
// "not this key" and nothing else.
var ErrMalformed = errors.New("checkin: malformed Check-In payload")

// Nonce derives the 13-byte nonce of counter under key: the first 13 bytes
// of HMAC-SHA256(key, counter LE32) (Core §4.22.3.1).
//
// Mirrors matter.js CheckInMessage.ts:36-40 (generateNonce).
func Nonce(key []byte, counter uint32) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(counterBytes(counter))
	return mac.Sum(nil)[:NonceLength]
}

// Encode builds a Check-In payload: the nonce of counter followed by
// AES-128-CCM(counter LE32 ‖ applicationData) under key, the tag last. The
// key must be 16 bytes (aesccm.ErrKeySize otherwise).
//
// Mirrors matter.js CheckInMessage.ts:45-50 (encode).
func Encode(key []byte, counter uint32, applicationData []byte) ([]byte, error) {
	ccm, err := aesccm.New(key)
	if err != nil {
		return nil, fmt.Errorf("checkin: %w", err)
	}
	nonce := Nonce(key, counter)
	plaintext := append(counterBytes(counter), applicationData...)
	out := make([]byte, 0, NonceLength+len(plaintext)+MICLength)
	out = append(out, nonce...)
	out, err = ccm.Seal(out, nonce, plaintext, nil)
	if err != nil {
		return nil, fmt.Errorf("checkin: %w", err)
	}
	return out, nil
}

// Decode authenticates a Check-In payload under key and returns its counter
// and application data. A payload shorter than MinPayloadLength, one that
// fails to decrypt, and one whose nonce is not the decrypted counter's
// return ErrMalformed; a key that is not 16 bytes returns the aesccm
// error.
//
// Mirrors matter.js CheckInMessage.ts:59-85 (decode).
func Decode(key, payload []byte) (counter uint32, applicationData []byte, err error) {
	if len(payload) < MinPayloadLength {
		return 0, nil, fmt.Errorf("%w: %d < %d bytes", ErrMalformed, len(payload), MinPayloadLength)
	}
	ccm, err := aesccm.New(key)
	if err != nil {
		return 0, nil, fmt.Errorf("checkin: %w", err)
	}
	nonce := payload[:NonceLength]
	plaintext, err := ccm.Open(nil, nonce, payload[NonceLength:], nil)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: decryption failed: %w", ErrMalformed, err)
	}
	// MinPayloadLength leaves at least CounterLength bytes of plaintext.
	counter = binary.LittleEndian.Uint32(plaintext[:CounterLength])
	if !hmac.Equal(nonce, Nonce(key, counter)) {
		return 0, nil, fmt.Errorf("%w: nonce does not match counter", ErrMalformed)
	}
	return counter, plaintext[CounterLength:], nil
}

// EncodeICD builds the ICD Check-In payload, whose application data is the
// device's ActiveModeThreshold in milliseconds as uint16 LE (Core
// §9.15.1.3.3).
//
// Mirrors matter.js CheckInMessage.ts:92-96 (encodeIcd).
func EncodeICD(key []byte, counter uint32, activeModeThreshold uint16) ([]byte, error) {
	return Encode(key, counter, binary.LittleEndian.AppendUint16(nil, activeModeThreshold))
}

// DecodeICD decodes an ICD Check-In payload into its counter and the
// ActiveModeThreshold it carries; application data shorter than two bytes
// returns ErrMalformed.
//
// Mirrors matter.js CheckInMessage.ts:105-115 (decodeIcd).
func DecodeICD(key, payload []byte) (counter uint32, activeModeThreshold uint16, err error) {
	counter, appData, err := Decode(key, payload)
	if err != nil {
		return 0, 0, err
	}
	if len(appData) < 2 {
		return 0, 0, fmt.Errorf("%w: application data too short for activeModeThreshold: %d < 2", ErrMalformed, len(appData))
	}
	return counter, binary.LittleEndian.Uint16(appData), nil
}

// Validation is the verdict of ValidateCounter (CheckInMessage.ts:172-179,
// CounterValidation).
type Validation struct {
	// Valid reports a fresh counter: neither a replay nor older than the
	// last one accepted.
	Valid bool
	// Offset is the unsigned distance from the key's starting counter to
	// the counter, wrapping at 2^32.
	Offset uint32
	// RefreshNeeded is true when Offset is at least 2^31: the client must
	// refresh the key.
	RefreshNeeded bool
}

// ValidateCounter holds a received counter against a key's rolling state —
// its starting counter and the offset of the last counter accepted under it
// (CounterState, CheckInMessage.ts:164-169). The offset is uint32
// arithmetic, so a counter that wrapped past 2^32 since counterStart still
// measures forward.
//
// Mirrors matter.js CheckInMessage.ts:123-127 (validateCounter).
func ValidateCounter(counter, counterStart, lastOffset uint32) Validation {
	offset := counter - counterStart
	return Validation{Valid: offset > lastOffset, Offset: offset, RefreshNeeded: RefreshNeededAt(offset)}
}

// RefreshNeededAt reports whether an offset from a key's starting counter
// requires a key refresh: at 2^31 and above.
//
// Mirrors matter.js CheckInMessage.ts:134-136 (refreshNeededAt).
func RefreshNeededAt(offset uint32) bool {
	return offset >= KeyRefreshOffset
}

// counterBytes is the counter as uint32 LE. Mirrors matter.js
// CheckInMessage.ts:138-142 (#counterToBytes).
func counterBytes(counter uint32) []byte {
	return binary.LittleEndian.AppendUint32(make([]byte, 0, CounterLength), counter)
}
