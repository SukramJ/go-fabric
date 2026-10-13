// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package sessionparams is the Matter SessionParameters structure the
// secure-channel handshakes exchange: Sigma1's initiatorSessionParams,
// Sigma2's and Sigma2_Resume's responderSessionParams, and the PASE
// PBKDFParamRequest / PBKDFParamResponse fields of the same tag (5).
//
// Mirrors matter.js packages/protocol/src/session/pase/PaseMessages.ts:28-58
// (TlvSessionParameters: nine optional context-tagged fields; the same
// schema is reused by CaseMessages.ts:34, :46, :55) and
// packages/protocol/src/session/SessionParameters.ts (the fallbacks, the
// Matter 1.5 gate on TCP and the rule that MAX_TCP_MESSAGE_SIZE is present
// only when SUPPORTED_TRANSPORTS names TCP).
package sessionparams

import (
	"fmt"

	"github.com/SukramJ/go-fabric/tlv"
)

// Transport bits of the SupportedTransports bitmap (tag 8). Mirrors
// matter.js packages/protocol/src/common/SupportedTransportsBitmap.ts:9-22:
// bit 0 is reserved, tcpClient is BitFlag(1), tcpServer is BitFlag(2).
const (
	// TransportTCPClient: the node implements the TCP client mode and may
	// connect to a peer that is a TCP server.
	TransportTCPClient uint16 = 1 << 1
	// TransportTCPServer: the node implements the TCP server mode and
	// listens for incoming TCP connections.
	TransportTCPServer uint16 = 1 << 2
)

// MinTCPSpecificationVersion is the first specification version whose
// peers may be offered TCP: matter.js SessionParameters.ts:17
// (MIN_TCP_SPEC_VERSION = 0x01050000) clears the transport bits of a peer
// below it (:50-54, "TCP is only honored for Matter 1.5.0+ peers").
const MinTCPSpecificationVersion uint32 = 0x01050000

// DefaultMaxTCPMessageSize is matter.js's fallback for
// MaxTCPMessageSize when a TCP-capable node states none
// (SessionParameters.ts:152, PaseMessages.ts:54 "default: 64000").
const DefaultMaxTCPMessageSize uint32 = 64000

// Parameters is one SessionParameters structure. A zero field is "not
// stated": it is omitted on the wire and a reader falls back to the spec
// default for that field. Field tags and widths follow PaseMessages.ts:28-58.
type Parameters struct {
	SessionIdleInterval      uint32 // [1] TlvUInt32, milliseconds
	SessionActiveInterval    uint32 // [2] TlvUInt32, milliseconds
	SessionActiveThreshold   uint16 // [3] TlvUInt16, milliseconds
	DataModelRevision        uint16 // [4] TlvUInt16
	InteractionModelRevision uint16 // [5] TlvUInt16
	SpecificationVersion     uint32 // [6] TlvUInt32, e.g. 0x01060100
	MaxPathsPerInvoke        uint16 // [7] TlvUInt16
	// SupportedTransports is the SUPPORTED_TRANSPORTS bitmap (TlvUInt16),
	// the transports the node supports in addition to MRP over UDP.
	SupportedTransports uint16 // [8]
	// MaxTCPMessageSize (matter.js `maxTcpMessageSize`) is the largest message the node accepts over TCP
	// in bytes (TlvUInt32). Encoded only when SupportedTransports names
	// TCP (SessionParameters.ts:58-63: "The MAX_TCP_MESSAGE_SIZE field
	// SHALL only be present if the SUPPORTED_TRANSPORTS field indicates
	// that TCP is supported").
	MaxTCPMessageSize uint32 // [9]
}

// IsEmpty reports whether no field is stated. An empty structure is
// omitted on the wire so the peer behaves as if none was supplied.
func (p Parameters) IsEmpty() bool {
	return p == Parameters{}
}

// SupportsTCP reports whether the bitmap names TCP in either role, as
// stated — see [Parameters.EffectiveTransports] for the version gate.
func (p Parameters) SupportsTCP() bool {
	return p.SupportedTransports&(TransportTCPClient|TransportTCPServer) != 0
}

// EffectiveTransports is the SupportedTransports bitmap a reader may act
// on: the stated bits with TCP cleared when the peer's
// SpecificationVersion is below Matter 1.5.0 or unknown (SessionParameters.ts:49-56
// — "clear it for older or unknown spec versions so supportedTransports is
// the single source of truth for transport selection").
func (p Parameters) EffectiveTransports() uint16 {
	if p.SpecificationVersion < MinTCPSpecificationVersion {
		return p.SupportedTransports &^ (TransportTCPClient | TransportTCPServer)
	}
	return p.SupportedTransports
}

// EffectiveMaxTCPMessageSize is the TCP message bound a reader may act
// on: the stated size, matter.js's fallback when a TCP-capable peer
// stated none (SessionParameters.ts:60-63), and 0 when the effective
// transports name no TCP.
func (p Parameters) EffectiveMaxTCPMessageSize() uint32 {
	if p.EffectiveTransports()&(TransportTCPClient|TransportTCPServer) == 0 {
		return 0
	}
	if p.MaxTCPMessageSize == 0 {
		return DefaultMaxTCPMessageSize
	}
	return p.MaxTCPMessageSize
}

// Encode writes the structure as a sub-structure tagged outer: every
// stated field at its spec width (PaseMessages.ts:28-58), tag 9 only when
// the bitmap names TCP. matter.js itself encodes the minimal width and
// its decoder checks only the element type (TlvNumber.ts:118-143 at
// 0ceb202d), so either width is accepted on the wire; the fixed width
// keeps the fixtures stable.
func (p Parameters) Encode(enc *tlv.Encoder, outer tlv.Tag) {
	enc.StartStruct(outer)
	if p.SessionIdleInterval != 0 {
		enc.PutUint32(tlv.ContextTag(1), p.SessionIdleInterval)
	}
	if p.SessionActiveInterval != 0 {
		enc.PutUint32(tlv.ContextTag(2), p.SessionActiveInterval)
	}
	if p.SessionActiveThreshold != 0 {
		enc.PutUint16(tlv.ContextTag(3), p.SessionActiveThreshold)
	}
	if p.DataModelRevision != 0 {
		enc.PutUint16(tlv.ContextTag(4), p.DataModelRevision)
	}
	if p.InteractionModelRevision != 0 {
		enc.PutUint16(tlv.ContextTag(5), p.InteractionModelRevision)
	}
	if p.SpecificationVersion != 0 {
		enc.PutUint32(tlv.ContextTag(6), p.SpecificationVersion)
	}
	if p.MaxPathsPerInvoke != 0 {
		enc.PutUint16(tlv.ContextTag(7), p.MaxPathsPerInvoke)
	}
	if p.SupportedTransports != 0 {
		enc.PutUint16(tlv.ContextTag(8), p.SupportedTransports)
	}
	if p.MaxTCPMessageSize != 0 && p.SupportsTCP() {
		enc.PutUint32(tlv.ContextTag(9), p.MaxTCPMessageSize)
	}
	_ = enc.EndContainer()
}

// DecodeFields reads the fields of a structure whose opening element the
// caller has already consumed, up to and including its end. Unknown tags
// and nested containers are skipped so a future field does not break the
// handshake; a value wider than its field is truncated to the field, as
// the TLV integer rules allow for a value that fits.
func DecodeFields(dec *tlv.Decoder) (Parameters, error) {
	var p Parameters
	for {
		el, err := dec.Next()
		if err != nil {
			return p, fmt.Errorf("sessionparams: %w", err)
		}
		if el.IsEndContainer {
			return p, nil
		}
		if el.IsContainer {
			if err := skipContainer(dec); err != nil {
				return p, err
			}
			continue
		}
		if el.Tag.Kind != tlv.TagKindContext {
			continue
		}
		switch el.Tag.Number {
		case 1:
			p.SessionIdleInterval = uint32(el.Uint & 0xFFFFFFFF)
		case 2:
			p.SessionActiveInterval = uint32(el.Uint & 0xFFFFFFFF)
		case 3:
			p.SessionActiveThreshold = uint16(el.Uint & 0xFFFF)
		case 4:
			p.DataModelRevision = uint16(el.Uint & 0xFFFF)
		case 5:
			p.InteractionModelRevision = uint16(el.Uint & 0xFFFF)
		case 6:
			p.SpecificationVersion = uint32(el.Uint & 0xFFFFFFFF)
		case 7:
			p.MaxPathsPerInvoke = uint16(el.Uint & 0xFFFF)
		case 8:
			p.SupportedTransports = uint16(el.Uint & 0xFFFF)
		case 9:
			p.MaxTCPMessageSize = uint32(el.Uint & 0xFFFFFFFF)
		}
	}
}

// skipContainer consumes the elements of a container whose opening
// element was just read, nested ones included.
func skipContainer(dec *tlv.Decoder) error {
	depth := 1
	for depth > 0 {
		el, err := dec.Next()
		if err != nil {
			return fmt.Errorf("sessionparams: skip: %w", err)
		}
		switch {
		case el.IsEndContainer:
			depth--
		case el.IsContainer:
			depth++
		}
	}
	return nil
}
