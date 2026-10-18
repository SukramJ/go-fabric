// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package message

import "errors"

// Errors of [Header.MarshalPacket].
var (
	// ErrGroupHeader is returned for a group-session header that lacks
	// the destination group id or the source node id, or names a
	// destination node id. Mirrors the InternalError of matter.js
	// packages/protocol/src/codec/MessageCodec.ts:481-488
	// (encodePacketHeader).
	ErrGroupHeader = errors.New("message: group session must have a destination group id and a source node id and no destination node id")

	// ErrEncodeExtensions is returned for a header that carries Message
	// Extensions: matter.js encodes none (MessageCodec.ts:440-443,
	// encodePacket: "Message extensions not supported when encoding a
	// packet").
	ErrEncodeExtensions = errors.New("message: message extensions not supported when encoding a packet")
)

// MarshalPacket encodes the header of an outbound packet, refusing what
// matter.js refuses to encode. Mirrors matter.js
// packages/protocol/src/codec/MessageCodec.ts at 0ceb202d:
//
//   - :441-443 encodePacket — a header with Message Extensions is not
//     encoded ([ErrEncodeExtensions]);
//   - :481-488 encodePacketHeader — a group-session header must carry the
//     destination group id and the source node id and no destination node
//     id ([ErrGroupHeader]);
//   - :489-504 — the bytes are [Header.Marshal]'s: Message Flags, Session
//     ID, Security Flags, Message Counter, Source Node ID, the destination.
//
// [Header.Marshal] stays the unchecked encoder the AAD and the tests use.
func (h Header) MarshalPacket() ([]byte, error) {
	if h.HasExtension || len(h.MessageExtension) > 0 {
		return nil, ErrEncodeExtensions
	}
	if h.SessionType == SessionGroup && (h.DestSize != DestGroup || !h.HasSourceNodeID) {
		return nil, ErrGroupHeader
	}
	return h.Marshal(), nil
}

// ErrEncodeSecuredExtensions is returned for a protocol header that carries
// Secured Extensions: matter.js encodes none (MessageCodec.ts:430-432,
// encodePayload: "Security extensions not supported when encoding a
// payload").
var ErrEncodeSecuredExtensions = errors.New("message: security extensions not supported when encoding a payload")

// MarshalPayload encodes the protocol header of an outbound message,
// refusing what matter.js refuses to encode — the payload-side twin of
// [Header.MarshalPacket]. Mirrors matter.js
// packages/protocol/src/codec/MessageCodec.ts:429-437 (encodePayload): a
// header with Secured Extensions is not encoded
// ([ErrEncodeSecuredExtensions]); otherwise the bytes are
// [ProtocolHeader.Marshal]'s. [ProtocolHeader.Marshal] stays the unchecked
// encoder the decode round-trip tests use.
func (h ProtocolHeader) MarshalPayload() ([]byte, error) {
	if h.HasSecuredExt || len(h.SecuredExtension) > 0 {
		return nil, ErrEncodeSecuredExtensions
	}
	return h.Marshal(), nil
}
