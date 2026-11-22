// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"errors"
	"fmt"

	"thingfabric.dev/go-fabric/tlv"
)

// TimedRequestMessage tag numbers per Matter Core Spec §10.6.10.
const (
	tagTimedReqTimeout                  uint8 = 0
	tagTimedReqInteractionModelRevision uint8 = 0xFF
)

// StatusResponseMessage tag numbers per Matter Core Spec §10.6.9.
const (
	tagStatusResponseStatus                   uint8 = 0
	tagStatusResponseInteractionModelRevision uint8 = 0xFF
)

// InteractionModelRevision is kept for back-compat with timed.go's
// existing call sites. Use [MatterInteractionModelRevision] (defined
// in subscribe.go) for new code.
//
// It is the IM protocol revision the bridge advertises in TimedRequest
// replies and StatusResponses (Matter §8.1.4) and always equals
// [MatterInteractionModelRevision]: 12, matter.js HEAD
// packages/model/src/common/Specification.ts INTERACTION_MODEL_REVISION
// at the Matter 1.6.1 pin. matter.js caps the value at 12 because
// revision 13's only delta is the provisional
// WildcardFilterConfigurationVersion (§8.2.1.7.1). The value 11 once made
// Apple Home treat subscribe transactions as stale-protocol; 12 is what
// matter.js ships to every ecosystem.
const InteractionModelRevision uint8 = MatterInteractionModelRevision

// Errors.
var (
	// ErrInvalidTimedRequest is returned for malformed TimedRequests.
	ErrInvalidTimedRequest = errors.New("im: invalid TimedRequest")
	// ErrInvalidStatusResponse is returned for malformed StatusResponses.
	ErrInvalidStatusResponse = errors.New("im: invalid StatusResponse")
)

// TimedRequest is the in-memory form of a TimedRequestMessage. The
// commissioner sends one before a Write/Invoke that the spec
// requires to be timed (e.g. door-lock unlocks). The bridge replies
// with a [StatusResponse]{Success} and stamps a per-exchange
// deadline; the matching follow-up Write/Invoke is then gated by
// `Bridge.checkTimedGate` per Matter §8.7 — a follow-up after the
// window expired is rejected with TIMEOUT (0x94), one whose Timed flag
// disagrees with the window (set without one, clear within one) with
// TIMED_REQUEST_MISMATCH (0xC9), as matter.js answers
// (packages/node/src/node/server/InteractionServer.ts:426-436 write,
// :994-1004 invoke at db021da0).
type TimedRequest struct {
	// TimeoutMs is the maximum number of milliseconds the bridge
	// must wait between this TimedRequest and the follow-up
	// Write / Invoke that it gates. Per Matter §10.7.1 the value is
	// uint16 (max 65 535 ms ≈ 65 s).
	TimeoutMs uint16
}

// UnmarshalTimedRequestTLV parses a TimedRequestMessage TLV payload.
func UnmarshalTimedRequestTLV(dec *tlv.Decoder) (TimedRequest, error) {
	var req TimedRequest
	hasTimeout := false
	open, err := dec.Next()
	if err != nil {
		return req, fmt.Errorf("%w: top: %w", ErrInvalidTimedRequest, err)
	}
	if open.Type != tlv.TypeStructure {
		return req, fmt.Errorf("%w: top must be Structure", ErrInvalidTimedRequest)
	}
	var later laterErrors
	for {
		el, err := dec.Next()
		if err != nil {
			return req, fmt.Errorf("%w: %w", ErrInvalidTimedRequest, err)
		}
		if el.IsEndContainer {
			if later.first != nil {
				return TimedRequest{}, later.first
			}
			break
		}
		if err := contextMember(el); err != nil {
			return TimedRequest{}, fmt.Errorf("%w: %w", ErrInvalidTimedRequest, err)
		}
		switch uint8(el.Tag.Number & 0xFF) {
		case tagTimedReqTimeout:
			hasTimeout = true
			// TlvUInt16, never masked: 70000 ms as a uint32 is refused,
			// not a 4464 ms window (matter.js packages/types/src/protocol/messages/TlvTimedRequest.ts:15; review
			// 2026-11 W2-wire-03).
			if req.TimeoutMs, err = uint16Field(el); err != nil {
				if err := later.keep(fmt.Errorf("%w: %w", ErrInvalidTimedRequest, err)); err != nil {
					return TimedRequest{}, err
				}
			}
		default:
			// InteractionModelRevision is decoded but not retained — the
			// field is informational; we always reply with our own
			// revision. Unknown fields are skipped whole.
			if err := skipField(dec, el); err != nil {
				return TimedRequest{}, fmt.Errorf("%w: %w", ErrInvalidTimedRequest, err)
			}
		}
	}
	// Timeout is mandatory: matter.js
	// packages/types/src/protocol/messages/TlvTimedRequest.ts:15 (TlvField).
	// Review 2026-11 W2-wire-04.
	if !hasTimeout {
		return TimedRequest{}, fmt.Errorf("%w: %w", ErrInvalidTimedRequest, missingField(tagTimedReqTimeout, "Timeout"))
	}
	return req, nil
}

// StatusResponse is the in-memory form of a StatusResponseMessage.
// The bridge emits it as the reply to a TimedRequest (always
// Success) and as the top-level rejection of a malformed action; it
// receives one from the controller after every ReportData chunk and
// every ongoing subscription report (Matter §8.6.2), where the carried
// status decides whether the interaction may continue.
type StatusResponse struct {
	Status StatusCode
}

// UnmarshalStatusResponseTLV parses a StatusResponseMessage TLV
// payload. Mirrors matter.js TlvStatusResponse decoding in
// packages/protocol/src/interaction/InteractionMessenger.ts:259-272 at
// db021da0 (throwIfErrorStatusMessage), which reads the status of
// every inbound StatusResponse before deciding whether the exchange
// goes on.
func UnmarshalStatusResponseTLV(dec *tlv.Decoder) (StatusResponse, error) {
	var sr StatusResponse
	open, err := dec.Next()
	if err != nil {
		return sr, fmt.Errorf("%w: top: %w", ErrInvalidStatusResponse, err)
	}
	if !open.IsContainer || open.Type != tlv.TypeStructure {
		return sr, fmt.Errorf("%w: top must be Structure", ErrInvalidStatusResponse)
	}
	seenStatus := false
	var later laterErrors
	for {
		el, err := dec.Next()
		if err != nil {
			return sr, fmt.Errorf("%w: %w", ErrInvalidStatusResponse, err)
		}
		if el.IsEndContainer {
			if later.first != nil {
				return StatusResponse{}, later.first
			}
			break
		}
		if err := contextMember(el); err != nil {
			return sr, fmt.Errorf("%w: %w", ErrInvalidStatusResponse, err)
		}
		switch uint8(el.Tag.Number & 0xFF) {
		case tagStatusResponseStatus:
			// An unsigned integer, as the StatusIB's status
			// ([statusIBField]): matter.js decodes the field as a
			// TlvEnum, a TlvUInt32, which throws on another TLV type
			// (packages/types/src/tlv/TlvNumber.ts:289, :124-131 at db021da0);
			// a signed integer or a boolean used to read as Success
			// (review II, R2-P6-wire-0503).
			if el.IsContainer {
				return sr, fmt.Errorf("%w: status is a container", ErrInvalidStatusResponse)
			}
			v, err := uint8Field(el)
			if err != nil {
				if err := later.keep(fmt.Errorf("%w: status: %w", ErrInvalidStatusResponse, err)); err != nil {
					return sr, err
				}
			}
			sr.Status = StatusCode(v)
			seenStatus = true
		default:
			// InteractionModelRevision and unknown fields: informational.
			if el.IsContainer {
				if err := skipContainer(dec); err != nil {
					return sr, fmt.Errorf("%w: %w", ErrInvalidStatusResponse, err)
				}
			}
		}
	}
	if !seenStatus {
		return sr, fmt.Errorf("%w: missing Status", ErrInvalidStatusResponse)
	}
	return sr, nil
}

// MarshalTLV encodes sr at the top level (anonymous tag).
func (sr StatusResponse) MarshalTLV(enc *tlv.Encoder) {
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutUint(tlv.ContextTag(tagStatusResponseStatus), uint64(sr.Status))
	enc.PutUint(tlv.ContextTag(tagStatusResponseInteractionModelRevision), uint64(InteractionModelRevision))
	_ = enc.EndContainer()
}
