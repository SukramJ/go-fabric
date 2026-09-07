// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package im

import (
	"errors"
	"fmt"

	"github.com/SukramJ/go-fabric/tlv"
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
// matter.js v0.16.10 emits 13 (Matter 1.5) on every IM message; the
// previous value of 11 made Apple Home tag our subscribe transactions
// as stale-protocol and time them out after ~10 s.
//
// Original doc:
// InteractionModelRevision is the IM protocol revision the bridge
// advertises in TimedRequest replies and StatusResponses (Matter
// §8.1.4 — bumped to 11 in 1.5.1; older controllers accept lower
// values gracefully).
const InteractionModelRevision uint8 = 13

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
// `Bridge.checkTimedGate` per Matter §8.7 — late or missing
// follow-ups get rejected with TIMEOUT (0x94) or
// NEEDS_TIMED_INTERACTION (0xC6).
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
	open, err := dec.Next()
	if err != nil {
		return req, fmt.Errorf("%w: top: %w", ErrInvalidTimedRequest, err)
	}
	if open.Type != tlv.TypeStructure {
		return req, fmt.Errorf("%w: top must be Structure", ErrInvalidTimedRequest)
	}
	for {
		el, err := dec.Next()
		if err != nil {
			return req, fmt.Errorf("%w: %w", ErrInvalidTimedRequest, err)
		}
		if el.IsEndContainer {
			break
		}
		if el.Tag.Kind != tlv.TagKindContext {
			continue
		}
		switch uint8(el.Tag.Number & 0xFF) {
		case tagTimedReqTimeout:
			req.TimeoutMs = uint16(el.Uint & 0xFFFF)
		case tagTimedReqInteractionModelRevision:
			// Decoded but not retained — the IM-revision field is
			// informational; we always reply with our own revision.
		}
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
// packages/protocol/src/interaction/InteractionMessenger.ts:183-196
// (throwIfErrorStatusMessage), which reads the status of every inbound
// StatusResponse before deciding whether the exchange goes on.
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
	for {
		el, err := dec.Next()
		if err != nil {
			return sr, fmt.Errorf("%w: %w", ErrInvalidStatusResponse, err)
		}
		if el.IsEndContainer {
			break
		}
		if el.Tag.Kind != tlv.TagKindContext {
			if el.IsContainer {
				if err := skipContainer(dec); err != nil {
					return sr, fmt.Errorf("%w: %w", ErrInvalidStatusResponse, err)
				}
			}
			continue
		}
		switch uint8(el.Tag.Number & 0xFF) {
		case tagStatusResponseStatus:
			if el.Uint > 0xFF {
				return sr, fmt.Errorf("%w: status %d exceeds uint8", ErrInvalidStatusResponse, el.Uint)
			}
			sr.Status = StatusCode(el.Uint)
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
