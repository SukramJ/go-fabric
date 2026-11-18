// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/SukramJ/go-fabric/tlv"
)

// matchDataVersionFilter searches filters for a matching (endpoint, cluster)
// pair and returns (cachedVersion, true) on hit.
// Returns (0, false) when no filter covers the (endpoint, cluster).
func matchDataVersionFilter(filters []DataVersionFilter, endpoint uint16, cluster uint32) (uint32, bool) {
	return MatchDataVersionFilter(filters, endpoint, cluster)
}

// dataVersionFiltersForContext is [DataVersionFiltersFor] for the local
// node id ctx carries ([WithLocalNodeID]); without one a filter's node is
// not looked at.
func dataVersionFiltersForContext(ctx context.Context, filters []DataVersionFilter) []DataVersionFilter {
	if localNodeID, ok := LocalNodeIDFromContext(ctx); ok {
		return DataVersionFiltersFor(filters, localNodeID)
	}
	return filters
}

// MatchDataVersionFilter is the exported form of [matchDataVersionFilter]
// used by the bridge's Subscribe path (bridge/subscribe.go) to apply
// the same filter evaluation logic as HandleReadRequest.
func MatchDataVersionFilter(filters []DataVersionFilter, endpoint uint16, cluster uint32) (uint32, bool) {
	for _, f := range filters {
		if f.Endpoint == endpoint && f.Cluster == cluster {
			return f.DataVersion, true
		}
	}
	return 0, false
}

// errFilterElement is an element of the EventFilters or DataVersionFilters
// array that is not an anonymous structure. Both references refuse the
// request for it: matter.js decodes either array as a TlvArray, which
// throws on a tagged element (packages/types/src/tlv/TlvArray.ts:77-78 at
// db021da0) and on one that is not a structure
// (packages/types/src/tlv/TlvObject.ts:205-207); connectedhomeip's
// ReadHandler returns CHIP_ERROR_INVALID_TLV_TAG for a tagged one
// (src/app/ReadHandler.cpp:543-545, :597-599 at 6170af84) and fails a
// non-structure in the filter's parser (:549, :601;
// src/app/MessageDef/StructParser.cpp:21-24). Review II, F-db021da0-48.
var errFilterElement = errors.New("im: malformed filter element")

// readDataVersionFilterArray parses an Array of DataVersionFilter structs.
// The array opener has already been consumed by the caller.
// Called from [UnmarshalReadRequestTLV] and [UnmarshalSubscribeRequestTLV].
// Each element is a struct per Matter Core Spec §10.6.4 DataVersionFilter:
//
//	tag 0 = Path (List: tag 1=EndpointID, tag 2=ClusterID)
//	tag 1 = DataVersion (uint32)
func readDataVersionFilterArray(dec *tlv.Decoder) ([]DataVersionFilter, error) {
	var filters []DataVersionFilter
	var later laterErrors
	for {
		el, err := dec.Next()
		if err != nil {
			return nil, err
		}
		if el.IsEndContainer {
			if later.first != nil {
				return nil, later.first
			}
			return filters, nil
		}
		if taggedArrayElement(el) || !el.IsContainer || el.Type != tlv.TypeStructure {
			// Not a DataVersionFilterIB: the request is malformed, as
			// matter.js and connectedhomeip refuse it ([errFilterElement]).
			return nil, fmt.Errorf("%w: DataVersionFilters element (type 0x%02X, tag kind %d)", errFilterElement, el.Type, el.Tag.Kind)
		}
		f, err := readDataVersionFilterFields(dec)
		if err := later.keep(err); err != nil {
			return nil, err
		}
		filters = append(filters, f)
	}
}

// readDataVersionFilterFields parses the inner fields of a DataVersionFilter
// struct after the struct opener has been consumed.
// Matter §10.6.4 DataVersionFilterIB:
//
//	tag 0 = Path (ClusterPathIB — a List with tag 1=EndpointID, tag 2=ClusterID)
//	tag 1 = DataVersion (uint32)
func readDataVersionFilterFields(dec *tlv.Decoder) (DataVersionFilter, error) {
	var f DataVersionFilter
	var hasPath, hasVersion bool
	var later laterErrors
	for {
		el, err := dec.Next()
		if err != nil {
			return DataVersionFilter{}, err
		}
		if el.IsEndContainer {
			if later.first != nil {
				return DataVersionFilter{}, later.first
			}
			// Path and DataVersion are mandatory, and so are the path's
			// endpoint and cluster: matter.js
			// packages/types/src/protocol/types/TlvDataVersionFilter.ts:16-17,
			// TlvClusterPath.ts:16-17 (TlvField). Review 2026-11 W2-wire-04.
			switch {
			case !hasPath:
				return DataVersionFilter{}, missingField(0, "DataVersionFilterIB.Path")
			case !hasVersion:
				return DataVersionFilter{}, missingField(1, "DataVersionFilterIB.DataVersion")
			}
			return f, nil
		}
		if err := contextMember(el); err != nil {
			return DataVersionFilter{}, err
		}
		switch uint8(el.Tag.Number & 0xFF) {
		case 0: // Path (ClusterPathIB — a List)
			if !el.IsContainer || el.Type != tlv.TypeList {
				return DataVersionFilter{}, &FieldError{Status: StatusInvalidAction, Tag: 0, Reason: "ClusterPathIB not a list"}
			}
			f.NodeID, f.HasNodeID, f.Endpoint, f.Cluster, err = readClusterPathFields(dec)
			if err := later.keep(err); err != nil {
				return DataVersionFilter{}, err
			}
			hasPath = true
		case 1: // DataVersion (uint32)
			v, err := uint32Field(el)
			if err := later.keep(err); err != nil {
				return DataVersionFilter{}, err
			}
			f.DataVersion = v
			hasVersion = true
		default:
			if err := skipField(dec, el); err != nil {
				return DataVersionFilter{}, err
			}
		}
	}
}

// readClusterPathFields parses a ClusterPathIB after its list opener has
// been consumed: endpoint (tag 1) and cluster (tag 2) are mandatory and
// type- and range-checked (matter.js
// packages/types/src/protocol/types/TlvClusterPath.ts:16-17; review
// 2026-11 W2-wire-03, W2-wire-04); the optional node (tag 0) is returned
// with hasNode. Other fields are skipped.
func readClusterPathFields(dec *tlv.Decoder) (node uint64, hasNode bool, endpoint uint16, cluster uint32, err error) {
	var hasEndpoint, hasCluster bool
	var later laterErrors
	for {
		inner, nextErr := dec.Next()
		if nextErr != nil {
			return 0, false, 0, 0, nextErr
		}
		if inner.IsEndContainer {
			if later.first != nil {
				return 0, false, 0, 0, later.first
			}
			if !hasEndpoint {
				return 0, false, 0, 0, missingField(1, "ClusterPathIB.Endpoint")
			}
			if !hasCluster {
				return 0, false, 0, 0, missingField(2, "ClusterPathIB.Cluster")
			}
			return node, hasNode, endpoint, cluster, nil
		}
		// A non-context member refuses the path, as for every path list
		// ([readAttributePathField]; TlvClusterPath.ts:13 is a TlvTaggedList).
		if ctxErr := contextMember(inner); ctxErr != nil {
			return 0, false, 0, 0, ctxErr
		}
		switch uint8(inner.Tag.Number & 0xFF) {
		case 0: // NodeID
			node, err = uintField(inner, math.MaxUint64)
			hasNode = true
		case 1: // EndpointID
			endpoint, err = uint16Field(inner)
			hasEndpoint = true
		case 2: // ClusterID
			cluster, err = uint32Field(inner)
			hasCluster = true
		default:
			err = skipField(dec, inner)
		}
		if err := later.keep(err); err != nil {
			return 0, false, 0, 0, err
		}
	}
}

// readEventFilterArray parses an Array of EventFilterIB structs per
// Matter Core Spec §10.6.9. The array opener has already been consumed
// by the caller. Called from [UnmarshalSubscribeRequestTLV] and
// [UnmarshalReadRequestTLV].
//
// Mirrors chip src/app/ReadHandler.cpp ProcessEventFilters.
func readEventFilterArray(dec *tlv.Decoder) ([]EventMinimumNumber, error) {
	var filters []EventMinimumNumber
	var later laterErrors
	for {
		el, err := dec.Next()
		if err != nil {
			return nil, err
		}
		if el.IsEndContainer {
			if later.first != nil {
				return nil, later.first
			}
			return filters, nil
		}
		if taggedArrayElement(el) || !el.IsContainer || el.Type != tlv.TypeStructure {
			// Not an EventFilterIB: the request is malformed, as
			// matter.js and connectedhomeip refuse it ([errFilterElement]).
			return nil, fmt.Errorf("%w: EventFilters element (type 0x%02X, tag kind %d)", errFilterElement, el.Type, el.Tag.Kind)
		}
		f, err := readEventFilterFields(dec)
		if err := later.keep(err); err != nil {
			return nil, err
		}
		filters = append(filters, f)
	}
}

// readEventFilterFields parses the inner fields of an EventFilterIB struct
// after the struct opener has been consumed.
//
// Matter §10.6.9 EventFilterIB:
//
//	tag 0 = NodeID (uint64, optional)
//	tag 1 = EventMin (uint64, mandatory)
//
// Mirrors matter.js packages/types/src/protocol/types/TlvEventFilter.ts:16-17
// (`nodeId: TlvOptionalField(0, TlvNodeId)`, `eventMin: TlvField(1,
// TlvUInt64)`) and chip src/app/ReadHandler.cpp ProcessEventFilters.
func readEventFilterFields(dec *tlv.Decoder) (EventMinimumNumber, error) {
	const (
		tagEventFilterNodeID   uint8 = 0
		tagEventFilterEventMin uint8 = 1
	)

	var f EventMinimumNumber
	hasEventMin := false
	var later laterErrors
	for {
		el, err := dec.Next()
		if err != nil {
			return EventMinimumNumber{}, err
		}
		if el.IsEndContainer {
			if later.first != nil {
				return EventMinimumNumber{}, later.first
			}
			// EventMin is mandatory: matter.js
			// packages/types/src/protocol/types/TlvEventFilter.ts:17
			// (TlvField); chip src/app/ReadHandler.cpp:603. Review
			// 2026-11 W2-wire-04.
			if !hasEventMin {
				return EventMinimumNumber{}, missingField(tagEventFilterEventMin, "EventFilterIB.EventMin")
			}
			return f, nil
		}
		if err := contextMember(el); err != nil {
			return EventMinimumNumber{}, err
		}
		switch uint8(el.Tag.Number & 0xFF) {
		case tagEventFilterNodeID:
			f.NodeID, err = uint64Field(el)
			if err := later.keep(err); err != nil {
				return EventMinimumNumber{}, err
			}
			f.HasNodeID = true
		case tagEventFilterEventMin:
			f.EventMin, err = uint64Field(el)
			if err := later.keep(err); err != nil {
				return EventMinimumNumber{}, err
			}
			hasEventMin = true
		default:
			if el.IsContainer {
				if skipErr := skipContainer(dec); skipErr != nil {
					return EventMinimumNumber{}, skipErr
				}
			}
		}
	}
}
