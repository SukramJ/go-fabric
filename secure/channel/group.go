// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package channel

import (
	"crypto/aes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/SukramJ/go-fabric/secure/aesccm"
	"github.com/SukramJ/go-fabric/transport/message"
)

// The send half of a group session: the AEAD seal of a group message and
// the privacy obfuscation of its header — the counterparts of the
// candidate decrypt and deobfuscation groups.Manager.Decode runs. Mirrors
// matter.js packages/protocol/src/session/GroupSession.ts:251-287 (encode)
// and packages/protocol/src/codec/MessagePrivacy.ts:38-56 at 0ceb202d.

// ErrNotGroupHeader is returned by [EncryptGroup] for a header that is not
// a group-session header carrying a destination group id. Mirrors the
// UnexpectedDataError of GroupSession.ts:254-257 ("Group ID is required
// for GroupSession encode").
var ErrNotGroupHeader = errors.New("channel: group encode needs a group-session header with a destination group id")

// privacyHeaderOffset is where the obfuscated header region starts: the
// Message Flags byte, the Session ID and the Security Flags stay in the
// clear (GroupSession.ts:278, :285 — `headerBytes.slice(4)` /
// `headerBytes.slice(0, 4)`).
const privacyHeaderOffset = 4

// EncryptGroup seals payload — the protocol header and the application
// payload — under an operational group key and returns the packet header
// bytes it authenticated and the ciphertext with its 16-byte MIC. Mirrors
// matter.js GroupSession.encode (GroupSession.ts:251-267) line by line:
//
//   - :254-257 the header must name a destination group ([ErrNotGroupHeader]);
//   - :259 headerBytes = MessageCodec.encodePacketHeader(packetHeader) —
//     [message.Header.MarshalPacket], with its group constraint;
//   - :260 securityFlags = headerBytes[3];
//   - :261 nonce = Session.generateNonce(securityFlags, messageId,
//     fabric.nodeId) — the 13-byte nonce of Matter §4.7.1.1 with
//     sourceNodeID, the sender's node id on the fabric;
//   - :262-267 ciphertext = crypto.encrypt(operationalGroupKey, payload,
//     nonce, headerBytes) — AES-CCM, the unobfuscated header bytes as AAD.
//
// The header is the one the caller built (sessionId already the group
// session id, GroupSession.ts:252); EncryptGroup does not change it.
func EncryptGroup(operationalKey []byte, sourceNodeID uint64, header message.Header, payload []byte) (headerBytes, ciphertext []byte, err error) {
	if header.SessionType != message.SessionGroup || header.DestSize != message.DestGroup {
		return nil, nil, ErrNotGroupHeader
	}
	headerBytes, err = header.MarshalPacket()
	if err != nil {
		return nil, nil, fmt.Errorf("channel: group encode: %w", err)
	}
	securityFlags := headerBytes[3]
	nonce := buildNonce(securityFlags, header.MessageCounter, sourceNodeID)
	ccm, err := aesccm.New(operationalKey)
	if err != nil {
		return nil, nil, fmt.Errorf("channel: group key: %w", err)
	}
	ciphertext, err = ccm.Seal(nil, nonce, payload, headerBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("channel: group seal: %w", err)
	}
	return headerBytes, ciphertext, nil
}

// ObfuscateGroupHeader applies the privacy enhancement to a sealed group
// message's header and returns the header as it goes on the wire. Mirrors
// matter.js GroupSession.encode (GroupSession.ts:276-286) line by line:
//
//   - :276 mic = the last CRYPTO_AEAD_MIC_LENGTH_BYTES (16) of the
//     ciphertext;
//   - :277 privacyNonce = MessagePrivacy.buildNonce(header.sessionId, mic)
//     — [PrivacyNonce];
//   - :278 region = headerBytes.slice(4);
//   - :279-284 obfuscated = MessagePrivacy.obfuscate(privacyKey, region,
//     privacyNonce) — [Obfuscate];
//   - :285 the wire header is headerBytes[0:4] followed by the obfuscated
//     region.
//
// The input slices are not modified.
func ObfuscateGroupHeader(privacyKey []byte, sessionID uint16, headerBytes, ciphertext []byte) ([]byte, error) {
	if len(headerBytes) < privacyHeaderOffset {
		return nil, fmt.Errorf("channel: group header is %d bytes", len(headerBytes))
	}
	if len(ciphertext) < aesccm.TagSize {
		return nil, fmt.Errorf("%w: got %d", ErrPrivacyMICShort, len(ciphertext))
	}
	mic := ciphertext[len(ciphertext)-aesccm.TagSize:]
	privacyNonce, err := PrivacyNonce(sessionID, mic)
	if err != nil {
		return nil, err
	}
	obfuscated, err := Obfuscate(privacyKey, headerBytes[privacyHeaderOffset:], privacyNonce)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(headerBytes))
	out = append(out, headerBytes[:privacyHeaderOffset]...)
	return append(out, obfuscated...), nil
}

// PrivacyNonce builds the 13-byte privacy nonce: the session id
// big-endian, then the last 11 bytes of the 16-byte MIC. Mirrors matter.js
// MessagePrivacy.buildNonce (MessagePrivacy.ts:38-47), which refuses a MIC
// of any other length (:40-42).
func PrivacyNonce(sessionID uint16, mic []byte) ([]byte, error) {
	if len(mic) != privacyMICLength {
		return nil, fmt.Errorf("%w: privacy nonce requires a %d-byte MIC, got %d", ErrPrivacyMICShort, privacyMICLength, len(mic))
	}
	nonce := make([]byte, 0, PrivacyNonceSize)
	nonce = binary.BigEndian.AppendUint16(nonce, sessionID)                   // :44
	return append(nonce, mic[privacyMICLength-privacyNonceMICBytes:]...), nil // :45 NONCE_MIC_OFFSET = 16 - 11
}

// Obfuscate XORs data with the AES-CTR keystream of privacyKey and nonce
// — AES-CCM with empty AAD, keeping the ciphertext without its tag — and
// returns the result; it both obfuscates and deobfuscates. Mirrors
// matter.js MessagePrivacy.obfuscate (MessagePrivacy.ts:53-56):
// crypto.encrypt(privacyKey, data, nonce) sliced to data's length, whose
// keystream are the CCM counter blocks 0x01 ‖ nonce ‖ i for i = 1, 2, …
// (L = 2; counter 0 is the tag's).
func Obfuscate(privacyKey, data, nonce []byte) ([]byte, error) {
	if len(privacyKey) != PrivacyKeySize {
		return nil, fmt.Errorf("%w: privacy key got %d", ErrPrivacyKeySource, len(privacyKey))
	}
	if len(nonce) != PrivacyNonceSize {
		return nil, fmt.Errorf("channel: privacy nonce must be %d bytes, got %d", PrivacyNonceSize, len(nonce))
	}
	cipher, err := aes.NewCipher(privacyKey)
	if err != nil {
		return nil, fmt.Errorf("channel: privacy cipher: %w", err)
	}
	block := make([]byte, PrivacyKeySize)
	block[0] = privacyCTRFlags
	copy(block[1:1+PrivacyNonceSize], nonce)
	out := make([]byte, len(data))
	stream := make([]byte, PrivacyKeySize)
	for off := 0; off < len(data); off += PrivacyKeySize {
		//nolint:gosec // the header region is at most 20 bytes: two blocks
		binary.BigEndian.PutUint16(block[PrivacyKeySize-2:], uint16(off/PrivacyKeySize+1))
		cipher.Encrypt(stream, block)
		for i := off; i < len(data) && i < off+PrivacyKeySize; i++ {
			out[i] = data[i] ^ stream[i-off]
		}
	}
	return out, nil
}
