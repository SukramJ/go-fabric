// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"errors"
	"fmt"

	"github.com/SukramJ/go-fabric/tlv"
)

// SubscribeRequestMessage tag numbers (Matter Core Spec §10.6.9).
const (
	tagSubReqKeepSubscriptions  uint8 = 0
	tagSubReqMinIntervalFloor   uint8 = 1
	tagSubReqMaxIntervalCeiling uint8 = 2
	tagSubReqAttributeRequests  uint8 = 3
	tagSubReqEventRequests      uint8 = 4
	tagSubReqEventFilters       uint8 = 5
	tagSubReqFabricFiltered     uint8 = 7
	tagSubReqDataVersionFilters uint8 = 8
)

// SubscribeResponseMessage tag numbers.
const (
	tagSubRespSubscriptionID uint8 = 0
	tagSubRespMaxInterval    uint8 = 2
	// tagInteractionModelRevision is the global Matter 1.x IM-revision
	// marker every IM message carries on tag 0xFF. matter.js's
	// TlvSubscribeResponse / TlvDataReportForSend / TlvStatusResponse
	// encode it on every reply. Apple Home strict-decodes the field
	// and silently re-sends the request when it is missing — the
	// SubscribeResponse appears to "never arrive", Apple's MTRDevice
	// times the transaction out after ~10 s and resubscribes in a
	// loop.
	tagInteractionModelRevision uint8 = 0xFF

	// MatterInteractionModelRevision is the IM revision the bridge
	// advertises. Mirrors matter.js HEAD packages/model/src/common/
	// Specification.ts INTERACTION_MODEL_REVISION (12 at the Matter
	// 1.6.1 pin; capped below 13 because revision 13's only delta,
	// WildcardFilterConfigurationVersion, is provisional). The wire
	// fixtures in im/testdata read the same constant from matter.js.
	MatterInteractionModelRevision uint8 = 12
)

// Errors.
var (
	// ErrInvalidSubscribeRequest is returned for malformed subscribes.
	ErrInvalidSubscribeRequest = errors.New("im: invalid SubscribeRequest")
)

// EventMinimumNumber is one entry in the EventFilters list that a
// commissioner sends to tell the bridge "the lowest event number I still
// want is EventMin; everything below it I already have."
//
// Mirrors Matter Core Spec §10.6.9 EventFilterIB and matter.js
// packages/types/src/protocol/types/TlvEventFilter.ts. chip
// src/app/ReadHandler.cpp:598 ProcessEventFilters stores EventMin
// from EventFilterIB.EventMin and uses it to gate the event query.
type EventMinimumNumber struct {
	// NodeID is the optional node filter from EventFilterIB tag 0: the
	// filter applies only to the node with this operational node id
	// ([EventFiltersFor]).
	NodeID    uint64
	HasNodeID bool
	// EventMin is the minimum event number (INCLUSIVE lower bound): the
	// bridge emits events with Number >= EventMin — see [EventLog.Query].
	// Corresponds to EventFilterIB tag 1.
	EventMin uint64
}

// SubscribeRequest is the in-memory form of a SubscribeRequestMessage.
//
// This type is the message shape only. The state machine around it —
// cadence enforcement and the per-fabric subscription cap — lives in
// im/subscription.Manager, which the daemon attaches to the bridge, so
// both are in force in production. Replay buffering is the part that
// remains unbuilt. The message-layer surface is kept stable against
// cluster-side callers that need to express "subscribe to OnOff state".
type SubscribeRequest struct {
	KeepSubscriptions  bool
	MinIntervalFloor   uint16
	MaxIntervalCeiling uint16
	AttributeRequests  []ConcreteAttributePath
	// EventRequests carries the subscription's interest in cluster
	// events (Matter §10.6.6). Wildcards work the same way as
	// AttributeRequests: leaving HasEndpoint / HasCluster / HasEvent
	// false matches every value.
	EventRequests []ConcreteEventPath
	// EventFilters carries per-source minimum-event-number hints the
	// controller sends to avoid re-receiving events it has already seen
	// (Matter §10.6.9 EventFilterIB). The bridge evaluates the filters
	// in BuildEventReports and skips events whose Number ≤ the
	// supplied EventMin.
	EventFilters   []EventMinimumNumber
	FabricFiltered bool
	// DataVersionFilters carries per-cluster version hints the
	// controller sends to avoid re-receiving attribute data it already
	// has cached (Matter §10.6.5). The bridge evaluates the filters
	// during the initial ReportData assembly and skips clusters whose
	// current DataVersion matches the cached one.
	DataVersionFilters []DataVersionFilter
}

// MarshalTLV encodes r as a client sends it. Mirrors matter.js
// packages/types/src/protocol/messages/TlvSubscribeRequest.ts:19-44 at
// 0ceb202d: KeepSubscriptions (0), MinIntervalFloor (1) and
// MaxIntervalCeiling (2) in seconds, the optional lists —
// AttributeRequests (3), EventRequests (4), EventFilters (5) — only with
// members, IsFabricFiltered (7), DataVersionFilters (8) only with members,
// InteractionModelRevision (0xFF), in field order.
func (r SubscribeRequest) MarshalTLV(enc *tlv.Encoder) {
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutBool(tlv.ContextTag(tagSubReqKeepSubscriptions), r.KeepSubscriptions)
	enc.PutUint(tlv.ContextTag(tagSubReqMinIntervalFloor), uint64(r.MinIntervalFloor))
	enc.PutUint(tlv.ContextTag(tagSubReqMaxIntervalCeiling), uint64(r.MaxIntervalCeiling))
	marshalAttributePaths(enc, tagSubReqAttributeRequests, r.AttributeRequests)
	marshalEventPaths(enc, tagSubReqEventRequests, r.EventRequests)
	marshalEventFilters(enc, tagSubReqEventFilters, r.EventFilters)
	enc.PutBool(tlv.ContextTag(tagSubReqFabricFiltered), r.FabricFiltered)
	marshalDataVersionFilters(enc, tagSubReqDataVersionFilters, r.DataVersionFilters)
	enc.PutUint(tlv.ContextTag(tagInteractionModelRevision), uint64(MatterInteractionModelRevision))
	_ = enc.EndContainer()
}

// UnmarshalSubscribeRequestTLV decodes a SubscribeRequestMessage.
func UnmarshalSubscribeRequestTLV(dec *tlv.Decoder) (SubscribeRequest, error) { //nolint:gocognit,gocyclo,funlen // wire/dispatch table over many attribute/opcode cases
	open, err := dec.Next()
	if err != nil {
		return SubscribeRequest{}, err
	}
	if !open.IsContainer || open.Type != tlv.TypeStructure {
		return SubscribeRequest{}, fmt.Errorf("%w: expected struct, got 0x%02X", ErrInvalidSubscribeRequest, open.Type)
	}
	var req SubscribeRequest
	var hasKeep, hasMin, hasMax, hasFabricFiltered bool
	for {
		el, err := dec.Next()
		if err != nil {
			return SubscribeRequest{}, fmt.Errorf("%w: %w", ErrInvalidSubscribeRequest, err)
		}
		if el.IsEndContainer {
			// KeepSubscriptions, both intervals and IsFabricFiltered are
			// mandatory: matter.js
			// packages/types/src/protocol/messages/TlvSubscribeRequest.ts
			// :21-27, :39 (TlvField); chip src/app/ReadHandler.cpp:761-762,
			// :822. Review 2026-11 W2-wire-04.
			switch {
			case !hasKeep:
				err = missingField(tagSubReqKeepSubscriptions, "KeepSubscriptions")
			case !hasMin:
				err = missingField(tagSubReqMinIntervalFloor, "MinIntervalFloor")
			case !hasMax:
				err = missingField(tagSubReqMaxIntervalCeiling, "MaxIntervalCeiling")
			case !hasFabricFiltered:
				err = missingField(tagSubReqFabricFiltered, "IsFabricFiltered")
			}
			if err != nil {
				return SubscribeRequest{}, fmt.Errorf("%w: %w", ErrInvalidSubscribeRequest, err)
			}
			return req, nil
		}
		if el.Tag.Kind != tlv.TagKindContext {
			continue
		}
		switch uint8(el.Tag.Number & 0xFF) {
		case tagSubReqKeepSubscriptions:
			if req.KeepSubscriptions, err = boolField(el); err != nil {
				return SubscribeRequest{}, fmt.Errorf("%w: %w", ErrInvalidSubscribeRequest, err)
			}
			hasKeep = true
		case tagSubReqMinIntervalFloor:
			// TlvUInt16, never masked (matter.js packages/types/src/protocol/messages/TlvSubscribeRequest.ts:24-27;
			// review 2026-11 W2-wire-03).
			if req.MinIntervalFloor, err = uint16Field(el); err != nil {
				return SubscribeRequest{}, fmt.Errorf("%w: %w", ErrInvalidSubscribeRequest, err)
			}
			hasMin = true
		case tagSubReqMaxIntervalCeiling:
			if req.MaxIntervalCeiling, err = uint16Field(el); err != nil {
				return SubscribeRequest{}, fmt.Errorf("%w: %w", ErrInvalidSubscribeRequest, err)
			}
			hasMax = true
		case tagSubReqAttributeRequests:
			if !el.IsContainer || el.Type != tlv.TypeArray {
				return SubscribeRequest{}, fmt.Errorf("%w: AttributeRequests not array", ErrInvalidSubscribeRequest)
			}
			paths, err := readPathArray(dec)
			if err != nil {
				return SubscribeRequest{}, err
			}
			req.AttributeRequests = paths
		case tagSubReqEventRequests:
			if !el.IsContainer || el.Type != tlv.TypeArray {
				return SubscribeRequest{}, fmt.Errorf("%w: EventRequests not array", ErrInvalidSubscribeRequest)
			}
			eventPaths, err := readEventPathArray(dec)
			if err != nil {
				return SubscribeRequest{}, err
			}
			req.EventRequests = eventPaths
		case tagSubReqEventFilters:
			// EventFilters: Array of EventFilterIB structs per Matter §10.6.9.
			// Each entry carries NodeID (tag 0, optional) and EventMin (tag 1,
			// mandatory) — matter.js
			// packages/types/src/protocol/types/TlvEventFilter.ts:16-17.
			// Mirrors chip src/app/ReadHandler.cpp:598 ProcessEventFilters.
			if !el.IsContainer || el.Type != tlv.TypeArray {
				// Malformed but non-fatal: skip the field. Draining is
				// only correct for a container — a scalar is already
				// consumed, and skipContainer would swallow the
				// enclosing SubscribeRequest struct's EndContainer.
				if el.IsContainer {
					if err := skipContainer(dec); err != nil {
						return SubscribeRequest{}, err
					}
				}
				continue
			}
			filters, err := readEventFilterArray(dec)
			if err != nil {
				return SubscribeRequest{}, err
			}
			req.EventFilters = filters
		case tagSubReqFabricFiltered:
			if req.FabricFiltered, err = boolField(el); err != nil {
				return SubscribeRequest{}, fmt.Errorf("%w: %w", ErrInvalidSubscribeRequest, err)
			}
			hasFabricFiltered = true
		case tagSubReqDataVersionFilters:
			if !el.IsContainer || el.Type != tlv.TypeArray {
				return SubscribeRequest{}, fmt.Errorf("%w: DataVersionFilters not array", ErrInvalidSubscribeRequest)
			}
			filters, err := readDataVersionFilterArray(dec)
			if err != nil {
				return SubscribeRequest{}, err
			}
			req.DataVersionFilters = filters
		default:
			if el.IsContainer {
				if err := skipContainer(dec); err != nil {
					return SubscribeRequest{}, err
				}
			}
		}
	}
}

// SubscribeResponse is the in-memory form of a
// SubscribeResponseMessage.
type SubscribeResponse struct {
	SubscriptionID uint32
	MaxInterval    uint16
}

// MarshalTLV encodes sr.
//
// SubscriptionId keeps the fixed 4-byte form Apple's MTRDevice needs
// (by_design L5-Apple-Width). MaxInterval is encoded at the smallest
// fitting width, as matter.js does (TlvSubscribeResponse.ts:18 at
// 0ceb202d); chip reads it with TLVReader::Get(uint16_t&), which accepts
// any unsigned width that fits (src/app/MessageDef/SubscribeResponseMessage.cpp:87-90,
// src/lib/core/TLVReader.cpp:216-226 at 6170af84), so an earlier note
// here that a narrowed MaxInterval collapses chip-tool's liveness timer
// does not match chip's reader.
func (sr SubscribeResponse) MarshalTLV(enc *tlv.Encoder) {
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutUint32(tlv.ContextTag(tagSubRespSubscriptionID), sr.SubscriptionID)
	// MaxInterval at the smallest fitting width: matter.js
	// packages/types/src/protocol/messages/TlvSubscribeResponse.ts:18 at
	// 0ceb202d (TlvUInt16, length from the value). Only SubscriptionId
	// keeps the fixed 4-byte form (by_design L5-Apple-Width).
	enc.PutUint(tlv.ContextTag(tagSubRespMaxInterval), uint64(sr.MaxInterval))
	enc.PutUint(tlv.ContextTag(tagInteractionModelRevision), uint64(MatterInteractionModelRevision))
	_ = enc.EndContainer()
}
