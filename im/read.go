// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"

	"github.com/SukramJ/go-fabric/schema"
	"github.com/SukramJ/go-fabric/tlv"
)

// ReadRequestMessage tag numbers (Matter Core Spec §10.6.4).
const (
	tagReadReqAttributeRequests  uint8 = 0
	tagReadReqEventRequests      uint8 = 1
	tagReadReqEventFilters       uint8 = 2
	tagReadReqFabricFiltered     uint8 = 3
	tagReadReqDataVersionFilters uint8 = 4
)

// ReportDataMessage tag numbers (Matter Core Spec §10.6.6).
const (
	tagReportSubscriptionID      uint8 = 0
	tagReportAttributeReports    uint8 = 1
	tagReportEventReports        uint8 = 2
	tagReportMoreChunkedMessages uint8 = 3
	tagReportSuppressResponse    uint8 = 4
)

// AttributeReportIB / AttributeDataIB tag numbers.
const (
	tagAttributeReportStatus uint8 = 0 // AttributeStatusIB
	tagAttributeReportData   uint8 = 1 // AttributeDataIB

	tagAttributeDataDataVersion uint8 = 0
	tagAttributeDataPath        uint8 = 1
	tagAttributeDataValue       uint8 = 2

	tagAttributeStatusPath   uint8 = 0
	tagAttributeStatusStatus uint8 = 1
)

// Errors.
var (
	// ErrInvalidReadRequest is returned for malformed read requests.
	ErrInvalidReadRequest = errors.New("im: invalid ReadRequest")
)

// DataVersionFilter is a single (endpoint, cluster, version) triple
// the controller sends to tell the bridge "I have this version cached;
// skip encoding attributes for this cluster if your DataVersion still
// matches". Matter Core Spec §10.6.5.
type DataVersionFilter struct {
	Endpoint    uint16
	Cluster     uint32
	DataVersion uint32
	// NodeID is the node the filter's ClusterPathIB names (tag 0) when
	// HasNodeID is set; a filter for another node is ignored
	// ([DataVersionFiltersFor]).
	NodeID    uint64
	HasNodeID bool
}

// ReadRequest is the in-memory form of a ReadRequestMessage.
// EventRequests carries the §10.6.4 EventPathIB list when the
// controller requests historical events (e.g. chip-tool's
// `read-event-by-id`). AttributeRequests and EventRequests may both
// be present in the same ReadRequest.
type ReadRequest struct {
	AttributeRequests []ConcreteAttributePath
	EventRequests     []ConcreteEventPath
	// EventFilters carries per-source minimum-event-number hints per
	// Matter §10.6.4 EventFilterIB. The bridge skips events with
	// Number < EventMin in BuildEventReports (EventMin is an inclusive
	// lower bound — see [EventLog.Query]).
	EventFilters       []EventMinimumNumber
	FabricFiltered     bool
	DataVersionFilters []DataVersionFilter
}

// MarshalTLV encodes r at the top level (always anonymous tag) as a
// client sends it. Mirrors matter.js
// packages/types/src/protocol/messages/TlvReadRequest.ts:19-35 at
// db021da0 with the request action/request/Read.ts:40-89 builds: the
// optional lists — AttributeRequests (0), EventRequests (1), EventFilters
// (2) — when the request has them, IsFabricFiltered (3) always,
// DataVersionFilters (4) when it has them, and InteractionModelRevision
// (0xFF) [MatterInteractionModelRevision]
// (packages/model/src/common/Specification.ts:124), in field order. A list
// without members is left out here; Read.ts:75-89 would send one its
// caller passed as an empty array.
func (r ReadRequest) MarshalTLV(enc *tlv.Encoder) {
	enc.StartStruct(tlv.AnonymousTag())
	marshalAttributePaths(enc, tagReadReqAttributeRequests, r.AttributeRequests)
	marshalEventPaths(enc, tagReadReqEventRequests, r.EventRequests)
	marshalEventFilters(enc, tagReadReqEventFilters, r.EventFilters)
	enc.PutBool(tlv.ContextTag(tagReadReqFabricFiltered), r.FabricFiltered)
	marshalDataVersionFilters(enc, tagReadReqDataVersionFilters, r.DataVersionFilters)
	enc.PutUint(tlv.ContextTag(tagInteractionModelRevision), uint64(MatterInteractionModelRevision))
	_ = enc.EndContainer()
}

// UnmarshalReadRequestTLV reads a ReadRequestMessage: its attribute and
// event paths, its event filters, FabricFiltered and its data-version
// filters. A field it does not know is skipped.
func UnmarshalReadRequestTLV(dec *tlv.Decoder) (ReadRequest, error) { //nolint:gocognit,gocyclo // wire/dispatch table over many attribute/opcode cases
	open, err := dec.Next()
	if err != nil {
		return ReadRequest{}, err
	}
	if !open.IsContainer || open.Type != tlv.TypeStructure {
		return ReadRequest{}, fmt.Errorf("%w: expected struct, got 0x%02X", ErrInvalidReadRequest, open.Type)
	}
	var req ReadRequest
	hasFabricFiltered := false
	var later laterErrors
	for {
		el, err := dec.Next()
		if err != nil {
			return ReadRequest{}, fmt.Errorf("%w: %w", ErrInvalidReadRequest, err)
		}
		if el.IsEndContainer {
			if later.first != nil {
				return ReadRequest{}, later.first
			}
			// IsFabricFiltered is mandatory: matter.js
			// packages/types/src/protocol/messages/TlvReadRequest.ts:30
			// (TlvField); chip src/app/ReadHandler.cpp:498. Review
			// 2026-11 W2-wire-04.
			if !hasFabricFiltered {
				return ReadRequest{}, fmt.Errorf("%w: %w", ErrInvalidReadRequest, missingField(tagReadReqFabricFiltered, "IsFabricFiltered"))
			}
			return req, nil
		}
		if err := contextMember(el); err != nil {
			return ReadRequest{}, fmt.Errorf("%w: %w", ErrInvalidReadRequest, err)
		}
		switch uint8(el.Tag.Number & 0xFF) {
		case tagReadReqAttributeRequests:
			if !el.IsContainer || el.Type != tlv.TypeArray {
				return ReadRequest{}, fmt.Errorf("%w: AttributeRequests not array", ErrInvalidReadRequest)
			}
			paths, err := readPathArray(dec)
			if err := later.keep(err); err != nil {
				return ReadRequest{}, err
			}
			req.AttributeRequests = paths
		case tagReadReqEventRequests:
			if !el.IsContainer || el.Type != tlv.TypeArray {
				return ReadRequest{}, fmt.Errorf("%w: EventRequests not array", ErrInvalidReadRequest)
			}
			paths, err := readEventPathArray(dec)
			if err := later.keep(err); err != nil {
				return ReadRequest{}, err
			}
			req.EventRequests = paths
		case tagReadReqEventFilters:
			// EventFilters: Array of EventFilterIB structs per Matter §10.6.9.
			// Each entry carries NodeID (tag 0, optional) and EventMin (tag 1,
			// mandatory) — matter.js
			// packages/types/src/protocol/types/TlvEventFilter.ts:16-17.
			// Mirrors chip src/app/ReadHandler.cpp:598 ProcessEventFilters.
			if !el.IsContainer || el.Type != tlv.TypeArray {
				// Malformed but non-fatal: skip the field. Only a
				// container has elements left to drain — a scalar has
				// already been fully consumed by dec.Next(), and
				// draining "its" container would eat the enclosing
				// ReadRequest struct's EndContainer and make the rest
				// of the message unparseable.
				if el.IsContainer {
					if err := skipContainer(dec); err != nil {
						return ReadRequest{}, err
					}
				}
				continue
			}
			filters, err := readEventFilterArray(dec)
			if err := later.keep(err); err != nil {
				return ReadRequest{}, err
			}
			req.EventFilters = filters
		case tagReadReqFabricFiltered:
			if req.FabricFiltered, err = requestBoolField(el, "FabricFiltered"); err != nil {
				return ReadRequest{}, fmt.Errorf("%w: %w", ErrInvalidReadRequest, err)
			}
			hasFabricFiltered = true
		case tagReadReqDataVersionFilters:
			// DataVersionFilters is an Array of structs per Matter §10.6.4.
			if !el.IsContainer || el.Type != tlv.TypeArray {
				return ReadRequest{}, fmt.Errorf("%w: DataVersionFilters not array", ErrInvalidReadRequest)
			}
			filters, err := readDataVersionFilterArray(dec)
			if err := later.keep(err); err != nil {
				return ReadRequest{}, err
			}
			req.DataVersionFilters = filters
		default:
			// Skip unknown / future fields per Matter forward-compat rules.
			if el.IsContainer {
				if err := skipContainer(dec); err != nil {
					return ReadRequest{}, err
				}
			}
		}
	}
}

// readPathArray walks an Array of AttributePathIB elements until
// EndContainer.
func readPathArray(dec *tlv.Decoder) ([]ConcreteAttributePath, error) {
	var paths []ConcreteAttributePath
	var later laterErrors
	for {
		// Peek the next element via Next; if it's the array's end,
		// return. Otherwise the element MUST be a List opener — the
		// path decoder consumes it.
		el, err := dec.Next()
		if err != nil {
			return nil, err
		}
		if el.IsEndContainer {
			if later.first != nil {
				return nil, later.first
			}
			return paths, nil
		}
		if taggedArrayElement(el) || !el.IsContainer || el.Type != tlv.TypeList {
			return nil, fmt.Errorf("%w: AttributePathIB not list, got 0x%02X", ErrInvalidPath, el.Type)
		}
		// Re-construct the path from the already-opened list. We
		// inline the field-loop body of UnmarshalAttributePathTLV
		// because the opener has been consumed.
		p, err := readAttributePathFields(dec)
		if err := later.keep(err); err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}
}

// readAttributePathFields is the inner half of UnmarshalAttributePathTLV
// — used when the caller has already consumed the list opener.
func readAttributePathFields(dec *tlv.Decoder) (ConcreteAttributePath, error) {
	var p ConcreteAttributePath
	var later laterErrors
	for {
		el, err := dec.Next()
		if err != nil {
			return ConcreteAttributePath{}, fmt.Errorf("%w: %w", ErrInvalidPath, err)
		}
		if el.IsEndContainer {
			if later.first != nil {
				return ConcreteAttributePath{}, later.first
			}
			return p, nil
		}
		if err := readAttributePathField(dec, el, &p); err != nil {
			if err := later.keep(fmt.Errorf("%w: %w", ErrInvalidPath, err)); err != nil {
				return ConcreteAttributePath{}, err
			}
		}
	}
}

// readAttributePathField decodes one field of an AttributePathIB into p.
// Every integer is type- and range-checked, never masked: an endpoint of
// 65536 or -1 would otherwise read as endpoint 0 (review 2026-11
// W2-wire-03). Mirrors matter.js
// packages/types/src/protocol/types/TlvAttributePath.ts:19-26 (bounded
// TlvEndpointNumber / TlvClusterId / TlvAttributeId, nullable TlvUInt16
// listIndex) and chip src/app/MessageDef/AttributePathIB.cpp:220-275. A
// member with a context tag the switch does not know is skipped whole, a
// member of any other tag refuses the path ([contextMember]): the path is
// a TlvTaggedList (TlvAttributePath.ts:19), the same ObjectSchema as
// TlvObject (packages/types/src/tlv/TlvObject.ts:381-382), whose decoder
// refuses a profile and an anonymous tag (:205-214 at db021da0; review
// II, R2-P6-wire-0504, Fable's read of sub round 1, H-5).
func readAttributePathField(dec *tlv.Decoder, el tlv.Element, p *ConcreteAttributePath) error {
	if err := contextMember(el); err != nil {
		return err
	}
	var err error
	switch uint8(el.Tag.Number & 0xFF) {
	case tagAttrPathNode:
		p.Node, err = uint64Field(el)
		p.HasNode = true
	case tagAttrPathEndpoint:
		p.Endpoint, err = uint16Field(el)
		p.HasEndpoint = true
	case tagAttrPathCluster:
		p.Cluster, err = uint32Field(el)
		p.HasCluster = true
	case tagAttrPathAttribute:
		p.Attribute, err = uint32Field(el)
		p.HasAttribute = true
	case tagAttrPathListIndex:
		p.HasListIndex = true
		if el.IsNull {
			p.ListAppend = true
			return nil
		}
		p.ListIndex, err = uint16Field(el)
	case tagAttrPathWildcardPathFlags:
		// TlvBitmap(TlvUInt32, WildcardPathFlagsBitmap), TlvAttributePath.ts:27.
		p.WildcardPathFlags, err = uint32Field(el)
	case tagAttrPathWildcardFilterCfgVer:
		// TlvUInt32, TlvAttributePath.ts:28.
		p.WildcardFilterConfigurationVersion, err = uint32Field(el)
		p.HasWildcardFilterConfigurationVersion = true
	case tagAttrPathEnableTagCompression:
		// An encoder-side optimisation a receiver may ignore (§A.7), but
		// typed: TlvOptionalField(0, TlvBoolean), TlvAttributePath.ts:21.
		_, err = requestBoolField(el, "EnableTagCompression")
	default:
		// Unknown tags: forward compatibility.
		err = skipField(dec, el)
	}
	return err
}

// skipContainer drains every nested element until a balancing
// EndContainer is reached. Used to skip the fields a decoder does not
// read.
func skipContainer(dec *tlv.Decoder) error {
	depth := 1
	for depth > 0 {
		el, err := dec.Next()
		if err != nil {
			return err
		}
		if el.IsContainer {
			depth++
		}
		if el.IsEndContainer {
			depth--
		}
	}
	return nil
}

// ReportData is the encoder side: the device assembles a
// ReportDataMessage from the dispatcher's [ReadResult] slice and ships
// it.
type ReportData struct {
	SubscriptionID  uint32
	HasSubscription bool
	Reports         []AttributeReport
	// EventReports carries the §10.6.6 EventReportIB list. Bridge
	// emits these when a subscribed cluster fires an event (e.g.
	// GenericSwitch InitialPress, DoorLock LockOperation).
	EventReports []EventReport
	// MoreChunkedMessages signals that this ReportDataMessage is one
	// of several chunks for the same logical report. Per Matter
	// §10.6.6 the sender splits oversized ReportData into multiple
	// datagrams, sets the flag on every chunk except the last, and
	// the receiver re-assembles by concatenating attribute / event
	// arrays in arrival order.
	MoreChunkedMessages bool
	SuppressResponse    bool
}

// AttributeReport is one (path, value | status) tuple inside the
// AttributeReports array.
type AttributeReport struct {
	Path   ConcreteAttributePath
	Status StatusIB
	Value  AttributeValue
	// IsStatus distinguishes a "status-only" entry (Status meaningful,
	// Value ignored) from a data entry (Value meaningful, Status zero).
	IsStatus bool
	// DataVersion is the per-cluster version tag chip emits in
	// AttributeDataIB.DataVersion (Matter §10.6.1.4), encoded as it is.
	DataVersion uint32
	// HasDataVersion reports that a received AttributeDataIB carried a
	// DataVersion — it is optional on the wire (TlvAttributeReportData.ts).
	// Set by [UnmarshalReportDataTLV]; the encoder ignores it.
	HasDataVersion bool
	// TagCompression is the received path's EnableTagCompression flag
	// (TlvAttributePath.ts tag 0): the omitted path fields are the
	// previous path's (InputChunk.ts:252-273). Set by
	// [UnmarshalReportDataTLV]; the encoder ignores it.
	TagCompression bool
	// FabricScope is the [ReadResult.FabricScope] the report was built
	// from: the encoder applies it to Value ([MaskFabricScoped]) and
	// answers the path FAILURE when the value cannot be scoped. A sender
	// that splits a list across messages applies it before the split and
	// clears it. Never set on a received report.
	FabricScope *FabricScope
}

// AttributeValueWriter is the function the Dispatcher's caller
// supplies for writing a cluster-native value into TLV. The IM layer
// stays cluster-blind; the writer translates Value into the right
// TLV-encoded shape (uint8, int16, struct, …).
type AttributeValueWriter func(enc *tlv.Encoder, tag tlv.Tag, v AttributeValue)

// MarshalTLV encodes r as a ReportDataMessage. valueWriter writes
// each [AttributeValue] into TLV; the IM layer does not own
// cluster-native type encoding. Event payload writing reuses
// valueWriter — events surface their data with the same cluster-native
// shapes that attributes do.
func (r ReportData) MarshalTLV(enc *tlv.Encoder, valueWriter AttributeValueWriter) {
	enc.StartStruct(tlv.AnonymousTag())
	if r.HasSubscription {
		// SubscriptionId is `TlvUInt32` per Matter §10.6.4.1.
		// Apple Home's IM-decoder rejects the whole ReportDataMessage
		// when this slot arrives as TypeUnsignedInt1 (which the
		// magnitude-driven PutUint emits for sub-IDs ≤ 0xFF). Use the
		// explicit-width helper to lock the wire type.
		enc.PutUint32(tlv.ContextTag(tagReportSubscriptionID), r.SubscriptionID)
	}
	// Each report array is written only when it has an entry: a
	// keep-alive, an event-only report and an empty read response carry no
	// attributeReports field. Mirrors matter.js
	// packages/protocol/src/interaction/InteractionMessenger.ts:490-491 at
	// db021da0 (`attributeReports: undefined, eventReports: undefined`),
	// :764-767 and :782-785 (an array is created with its first entry) and
	// :512-513 (both deleted once a chunk went out) — for every message
	// sendDataReport sends, the read response, the priming report and the
	// subscription's reports alike. A receiver takes an absent array and
	// an empty one alike ([UnmarshalReportDataTLV]; chip
	// src/app/ReadClient.cpp:688-712 at 6170af84).
	if len(r.Reports) > 0 {
		enc.StartArray(tlv.ContextTag(tagReportAttributeReports))
		for _, rep := range r.Reports {
			rep.marshal(enc, valueWriter)
		}
		_ = enc.EndContainer()
	}
	if len(r.EventReports) > 0 {
		enc.StartArray(tlv.ContextTag(tagReportEventReports))
		for _, ev := range r.EventReports {
			ev.marshal(enc, EventDataWriter(valueWriter))
		}
		_ = enc.EndContainer()
	}
	if r.MoreChunkedMessages {
		enc.PutBool(tlv.ContextTag(tagReportMoreChunkedMessages), true)
	}
	// Always emit SuppressResponse, including the explicit `false`. matter.js
	// (packages/types/src/protocol/messages/TlvDataReportForSend.ts:27)
	// declares this as a TlvOptionalField that ServerSubscription always
	// fills with a concrete value: `false` for every ongoing report, the
	// keep-alive included
	// (packages/node/src/node/server/ServerSubscription.ts:914, :928 at
	// db021da0). Apple Home's IM layer treats an absent SuppressResponse
	// differently from an explicit `false` — without the
	// field the controller answers with only a Standalone MRP-ACK and never
	// emits the StatusResponse the spec §10.6.4.1 mandates for
	// non-suppressed reports, which leaves matter.js's waitForSuccess gate
	// open indefinitely (irrelevant here) but also drops the report's
	// "delivery + parse OK" half-handshake the HMOutlet projection appears
	// to gate on. Always-emitting locks the wire shape to matter.js HEAD's
	// behavior.
	enc.PutBool(tlv.ContextTag(tagReportSuppressResponse), r.SuppressResponse)
	// Global IM revision marker (Matter 1.x §10.6, Tag 0xFF). matter.js
	// emits this on every IM message — Apple Home decodes strictly and
	// times subscribe transactions out when it is absent.
	enc.PutUint(tlv.ContextTag(tagInteractionModelRevision), uint64(MatterInteractionModelRevision))
	_ = enc.EndContainer()
}

func (rep AttributeReport) marshal(enc *tlv.Encoder, valueWriter AttributeValueWriter) {
	var scoped []byte
	if rep.FabricScope != nil && !rep.IsStatus {
		var ok bool
		if scoped, ok = scopeAttributeValue(rep.Value, *rep.FabricScope, valueWriter); !ok {
			rep = AttributeReport{Path: rep.Path, IsStatus: true, Status: StatusIB{Status: StatusFailure}}
			rep.Path.ListAppend, rep.Path.HasListIndex, rep.Path.ListIndex = false, false, 0
		}
	}
	enc.StartStruct(tlv.AnonymousTag())
	if rep.IsStatus {
		enc.StartStruct(tlv.ContextTag(tagAttributeReportStatus))
		rep.Path.MarshalTLV(enc, tlv.ContextTag(tagAttributeStatusPath))
		rep.Status.MarshalTLV(enc, tlv.ContextTag(tagAttributeStatusStatus))
		_ = enc.EndContainer()
	} else {
		enc.StartStruct(tlv.ContextTag(tagAttributeReportData))
		// DataVersion is mandatory in AttributeDataIB per Matter
		// §10.6.1.4. chip-tool's ClusterStateCache expects to find
		// it at context tag 0 — without it the controller surfaces
		// CHIP_ERROR_KEY_NOT_FOUND on every read. Every cluster
		// instance has a version (endpoint.TopologyDispatcher, review
		// II decision 46), and it goes out as it is, 0 included.
		dataVersion := rep.DataVersion
		// DataVersion is `TlvUInt32` per Matter §10.6.1.4. Apple Home's
		// MTRDevice IM-decoder rejects the whole AttributeDataIB when
		// DataVersion arrives as TypeUnsignedInt1 — and the v1.x
		// constant 1 always fits in one byte, so the magnitude-driven
		// PutUint silently produced a non-spec wire shape. The result
		// was MTRDevice logging "last report: (null)" while the MRP
		// layer happily ACKed every chunk: Apple's per-element strict
		// type check threw out every report before MTRDevice ever saw
		// it, and HAP-service rebuild then failed with Code=24.
		enc.PutUint32(tlv.ContextTag(tagAttributeDataDataVersion), dataVersion)
		rep.Path.MarshalTLV(enc, tlv.ContextTag(tagAttributeDataPath))
		if scoped != nil {
			_ = enc.PutRawElement(tlv.ContextTag(tagAttributeDataValue), scoped)
		} else {
			valueWriter(enc, tlv.ContextTag(tagAttributeDataValue), rep.Value)
		}
		_ = enc.EndContainer()
	}
	_ = enc.EndContainer()
}

// HandleReadRequest dispatches a parsed ReadRequest through d and
// returns the assembled ReportData. The caller marshals the result.
// Wildcard expansion is the Dispatcher's responsibility; this layer
// only routes paths, one at a time ([AttributeExpansion]).
//
// DataVersionFilter evaluation: for each (endpoint, cluster) pair in
// req.DataVersionFilters, if the cluster's current DataVersion matches
// the cached version the controller sent, the cluster's attributes are
// omitted from the report — the controller already has a fresh copy.
// Matter §10.6.5 / mirrors matter.js InteractionServer.ts
// startReadInteraction DataVersionFilter check.
//
// ACL gate: when d implements [ACLChecker], every RESOLVED result is
// authorized at its concrete (endpoint, cluster) using the resolved
// attribute's read privilege — run per result AFTER path expansion so a
// wildcard-endpoint read is authorized per concrete endpoint, not once
// against endpoint 0. A FULLY-CONCRETE path (endpoint + cluster + attribute
// all named) that is denied returns an AttributeStatusIB carrying
// UnsupportedAccess (0x7e); a WILDCARD-expanded result that is denied is
// SILENTLY OMITTED — a wildcard read discloses only authorized paths (Matter
// §8.4.3.2). PASE sessions bypass the check — commissioning reads must
// succeed before the fabric's ACL entry exists, and keep succeeding after
// AddNOC adopted the session onto the new fabric (matter.js
// packages/protocol/src/interaction/FabricAccessControl.ts:189-191 keys the
// implicit Administer grant on the auth mode, not on the fabric), so the
// bypass is keyed on [IsPASEFromContext] alone: a request without a fabric
// that is not PASE is refused (checkSubjectACL). Mirrors matter.js
// packages/protocol/src/action/server/AttributeReadResponse.ts:238-274
// (addConcrete → error status) and readAttributeForWildcard (bare return).
func HandleReadRequest(ctx context.Context, d Dispatcher, req ReadRequest) ReportData {
	// The gates run per result in [ReadReports]; this is its drained
	// form. A responder that sends the reports chunk by chunk pulls them
	// from [NewReadReports] instead (review II, decision 44).
	var rd ReportData
	reports := NewReadReports(ctx, d, req)
	for rep, ok := reports.Next(); ok; rep, ok = reports.Next() {
		rd.Reports = append(rd.Reports, rep)
	}
	// A plain (non-subscribe) Read's ReportData carries SuppressResponse=true:
	// the controller answers the final chunk with only a Standalone MRP-Ack, not
	// an IM StatusResponse. Mirrors matter.js
	// packages/node/src/node/server/InteractionServer.ts:358-363, :383-389 at db021da0
	// (handleReadRequest returns `dataReport: { suppressResponse: true }`) and
	// chip src/app/ReadHandler.cpp:333 at 6170af84 (`responseExpected = IsType(Subscribe) ||
	// aMoreChunks` → false on a read's final chunk). The chunker propagates this
	// to the terminal chunk only; intermediate chunks keep SuppressResponse=false
	// so the per-chunk StatusResponse handshake still runs. Subscribe priming
	// builds its own report and is unaffected — HandleReadRequest is only called
	// from the plain-read path.
	rd.SuppressResponse = true
	return rd
}

// DataVersionFiltersFor returns the DataVersionFilters that apply to the
// node whose operational node id on the accessing fabric is localNodeID:
// every filter without a NodeID or with NodeID == localNodeID. A filter
// addressed to another node is ignored. Mirrors matter.js
// packages/protocol/src/action/server/AttributeReadResponse.ts:73-77
// (`if (filterNodeId !== undefined && filterNodeId !== nodeId) continue;`).
func DataVersionFiltersFor(filters []DataVersionFilter, localNodeID uint64) []DataVersionFilter {
	out := filters[:0:0]
	for _, f := range filters {
		if !f.HasNodeID || f.NodeID == localNodeID {
			out = append(out, f)
		}
	}
	return out
}

// EventFiltersFor returns the one EventFilterIB that applies to the node
// whose operational node id on the accessing fabric is localNodeID: the
// first filter without a NodeID or with NodeID == localNodeID, as a
// one-element slice, or nil when none applies. The rest are ignored. Pass
// 0 for a session without a fabric. A filter addressed to another node
// (a controller managing several sends one per node) does not lower the
// floor. Mirrors matter.js
// packages/protocol/src/action/server/EventReadResponse.ts:68-77 (the
// first matching filter sets #eventMinVersion, then break; nodeId is
// UNSPECIFIED_NODE_ID without a fabric). Review 2026-11 W2-wire-05.
func EventFiltersFor(filters []EventMinimumNumber, localNodeID uint64) []EventMinimumNumber {
	for _, f := range filters {
		if !f.HasNodeID || f.NodeID == localNodeID {
			return []EventMinimumNumber{f}
		}
	}
	return nil
}

// minEventNumberFromFilters returns the event-number floor of filters: the
// EventMin of the first entry, 0 (no filtering) when filters is empty. The
// caller selects the entry for this node with [EventFiltersFor]; any event
// whose Number is below the floor is already known to the controller
// (EventMin is an inclusive lower bound — see [EventLog.Query]). Mirrors
// matter.js EventReadResponse.ts:68-77 (one filter applies) and
// packages/protocol/src/events/OccurrenceManager.ts:250-254 (that single
// value bounds the query).
func minEventNumberFromFilters(filters []EventMinimumNumber) uint64 {
	if len(filters) == 0 {
		return 0
	}
	return filters[0].EventMin
}

// BuildEventReports evaluates paths against log and returns a slice of
// [EventReport] suitable for embedding into a [ReportData].
//
// This is the event-only counterpart of [HandleReadRequest] for
// attributes. It mirrors the path-expansion + deduplication logic in
// matter.js packages/protocol/src/action/server/EventReadResponse.ts
// (EventReadResponse.process → #readAllowedEvents) and the event
// retrieval in matter.js packages/protocol/src/events/OccurrenceManager.ts:250-285
// at db021da0 (get; review II, R2-P6-doc-0703). Wildcard-expand-then-filter semantics and sort-by-Number
// ascending output order match the matter.js reference implementation.
//
// Each [ConcreteEventPath] is evaluated independently; results are
// de-duplicated by [EventRecord].Number before returning so a wildcard
// path that overlaps with a concrete path does not produce duplicates.
//
// filters carries the EventFilterIB minimum-event-number constraint for
// this node (Matter §10.6.9), selected from the request's with
// [EventFiltersFor]. Pass nil to return all buffered events. The first
// entry's EventMin is the INCLUSIVE lower bound — events with Number <
// minNumber are excluded.
// The returned [EventReport].Timestamp is sourced from
// [EventRecord].EpochMS (POSIX milliseconds — Matter §10.6.6.1
// EpochTimestamp, encoded at tag 3), matching the millisecond timestamp
// the subscribe path emits so the same event reads identically live and
// out-of-band.
//
// Callers that have a [ReadRequest] available should prefer the
// [HandleReadEventRequest] wrapper.
func BuildEventReports(paths []ConcreteEventPath, log *EventLog, filters []EventMinimumNumber) []EventReport {
	if log == nil || len(paths) == 0 {
		return nil
	}
	minNumber := minEventNumberFromFilters(filters)
	seen := make(map[uint64]struct{})
	var out []EventReport
	for _, path := range paths {
		// Resolve wildcard values for the EventLog.Query call.
		var ep uint16 = 0xFFFF
		if path.HasEndpoint {
			ep = path.Endpoint
		}
		var cl uint32 = 0xFFFFFFFF
		if path.HasCluster {
			cl = path.Cluster
		}
		var ev uint32 = 0xFFFFFFFF
		if path.HasEvent {
			ev = path.Event
		}
		records := log.Query(ep, cl, ev, minNumber)
		for _, rec := range records {
			if _, dup := seen[rec.Number]; dup {
				continue
			}
			seen[rec.Number] = struct{}{}
			out = append(out, EventReport{
				Path: ConcreteEventPath{
					Endpoint:    rec.Endpoint,
					HasEndpoint: true,
					Cluster:     rec.Cluster,
					HasCluster:  true,
					Event:       rec.EventID,
					HasEvent:    true,
				},
				Number:    rec.Number,
				Priority:  rec.Priority,
				Timestamp: rec.EpochMS,
				Data:      AttributeValue{Value: rec.Payload},
				IsStatus:  false,
			})
		}
	}
	// Each path is queried on its own, so out is a sequence of
	// independently ascending runs. The wire order must be globally
	// ascending: matter.js registers the allowed paths first and then
	// walks the occurrence store exactly once
	// (packages/protocol/src/action/server/EventReadResponse.ts:334
	// #readAllowedEvents), so a controller that tracks the last
	// EventNumber it saw never observes a descending step.
	slices.SortFunc(out, func(a, b EventReport) int {
		return cmp.Compare(a.Number, b.Number)
	})
	return out
}

// EventPathsForNode returns the event paths that select this node's
// events: a path naming another node is left out, the node of a path
// naming this node (localNodeID, its id on the session's fabric, 0
// without one) is cleared, a path naming no node is kept. The caller
// hands the result to [BuildEventReports] and to a subscription's stored
// paths ([ConcreteEventPath.Matches] does not look at the node), and the
// request's own paths to [DeniedEventPathStatuses], which answers a
// concrete path naming another node. Mirrors matter.js
// packages/protocol/src/action/server/EventReadResponse.ts:138-140 and
// :159-162 at db021da0 (#addWildcard / #addConcrete: a path naming
// another node registers nothing), the node id per DataResponse.ts:57-62;
// the attribute paths' twin is [AttributeExpansion] (review II,
// R2-P4-imread-07).
func EventPathsForNode(paths []ConcreteEventPath, localNodeID uint64) []ConcreteEventPath {
	if !slices.ContainsFunc(paths, func(p ConcreteEventPath) bool { return p.HasNode }) {
		return paths
	}
	out := make([]ConcreteEventPath, 0, len(paths))
	for _, p := range paths {
		if p.HasNode {
			if p.Node != localNodeID {
				continue
			}
			p.HasNode, p.Node = false, 0
		}
		out = append(out, p)
	}
	return out
}

// HandleReadEventRequest evaluates the EventRequests in req against
// log and returns a slice of EventReports suitable for embedding into a
// ReportData.
//
// This is a thin wrapper around [BuildEventReports] that extracts the
// EventRequests and EventFilters from the supplied [ReadRequest]. The
// event-only core logic lives in [BuildEventReports]; this wrapper
// exists so call sites that already hold a [ReadRequest] (e.g. the
// read-opcode handler) do not need to unpack the path slice manually.
//
// Mirrors matter.js packages/protocol/src/action/server/EventReadResponse.ts
// (EventReadResponse.process) for the request-level dispatch and
// matter.js packages/protocol/src/events/OccurrenceManager.ts:250-285 at
// db021da0 (get) for the log query (review II, R2-P6-doc-0703). Passes req.EventFilters to [BuildEventReports]
// for minimum-event-number gating; the caller selects them for this node
// with [EventFiltersFor] first.
func HandleReadEventRequest(req ReadRequest, log *EventLog) []EventReport {
	return BuildEventReports(req.EventRequests, log, req.EventFilters)
}

// EventReadAuthorizer bundles the accessing subject + ACL checker used to gate
// event reads and subscriptions, mirroring the (fabricIndex, subject, CATs)
// tuple the attribute read path threads through [HandleReadRequest]. PASE
// marks the commissioning channel, before AddNOC (FabricIndex 0) and after
// it adopted the session onto the new fabric — the implicit Administer
// grant is keyed on that auth mode
// (matter.js packages/protocol/src/interaction/FabricAccessControl.ts:189-191),
// never on the FabricIndex: a zero FabricIndex without PASE is no grant and
// is refused (checkSubjectACL). A nil Checker denies every event to a
// session that is not PASE, as a [Dispatcher] without an [ACLChecker]
// denies its reads (review II, R2-P6-sec-0560, owner decision 136). PASE
// does not lift the fabric-sensitive drop of
// [AuthorizeEventReports], which compares a record's fabric with
// FabricIndex for every session.
type EventReadAuthorizer struct {
	Checker       ACLChecker
	FabricIndex   uint8
	PASE          bool
	SubjectNodeID uint64
	SubjectCATs   []uint32
	// Paths answers whether a concrete event path exists; nil takes every
	// path to exist.
	Paths EventPathChecker
}

// EventPathChecker is an optional interface a [Dispatcher] implements to
// answer the existence of a concrete event path: UNSUPPORTED_ENDPOINT,
// UNSUPPORTED_CLUSTER, UNSUPPORTED_EVENT, or Success — matter.js
// EventReadResponse.ts #addConcrete's existence checks, in their order.
// A path with its endpoint and cluster named and no event (HasEvent
// false) asks whether the cluster has an event a wildcard event path
// selects: UNSUPPORTED_EVENT when it has none, as matter.js registers a
// wildcard event path per event of the cluster
// (packages/protocol/src/action/server/EventReadResponse.ts:277-284 at
// db021da0).
type EventPathChecker interface {
	EventPathStatus(path ConcreteEventPath) StatusCode
}

// Allows reports whether the accessing subject may read the events of
// (endpoint, cluster): the lowest read privilege among the cluster's events
// in the model ([schema.ClusterEventScope]; View for a cluster it does not
// hold) checked against the ACL. A PASE session allows, as
// AuthorizeEventReports does; without a Checker, or without an accessing
// fabric, a session that is not PASE is denied.
func (a EventReadAuthorizer) Allows(ctx context.Context, endpoint uint16, cluster uint32) bool {
	return a.allowsAt(ctx, endpoint, cluster, schema.ClusterEventScope(cluster).ReadPrivilege)
}

// allowsAt is Allows at privilege.
func (a EventReadAuthorizer) allowsAt(ctx context.Context, endpoint uint16, cluster uint32, privilege uint8) bool {
	// PASE is the only implicit grant: matter.js
	// packages/protocol/src/interaction/FabricAccessControl.ts:189-191
	// keys it on `authMode === Pase && isCommissioning`, never on a
	// missing fabric (review 2026-11 W2-sec-03).
	if a.PASE || IsPASEFromContext(ctx) {
		return true
	}
	if a.Checker == nil {
		return false
	}
	return checkSubjectACL(ctx, a.Checker, a.FabricIndex, a.SubjectNodeID, a.SubjectCATs, endpoint, cluster, privilege).IsSuccess()
}

// AuthorizeEventReports filters events down to those the accessing subject may
// read and drops fabric-sensitive records that belong to another fabric.
// Denied paths/records are SILENTLY OMITTED — a wildcard event read discloses
// only authorized events (Matter §8.4.3.2), never an error status. A PASE
// session (PASE / [IsPASEFromContext], also once AddNOC adopted it onto a
// fabric) passes the ACL gate (a) for every event; a session that is not
// PASE reads none without a Checker or an accessing fabric. The
// fabric-sensitive drop (b) applies to every session.
//
// (a) ACL gate: each event is gated at its read privilege — the event's
//
//	effective read privilege in the model ([eventScope]).
//	Mirrors matter.js packages/protocol/src/action/server/EventReadResponse.ts
//	#addConcrete/#addEventForWildcard (authorize at event.limits.readLevel).
//
// (b) Fabric-sensitive drop: a record of an event whose access is
//
//	fabric-sensitive ("S" in the model, [eventScope]) is disclosed only to the
//
//	fabric that owns it, independent of the read's FabricFiltered flag and of
//	the auth mode: the accessing fabric is auth.FabricIndex — 0 for a PASE
//	session before AddNOC, the adopted fabric after it. Fails closed — a
//	record whose fabric cannot be positively matched to the accessing fabric
//	is never disclosed. Mirrors matter.js
//	packages/protocol/src/action/server/EventReadResponse.ts:79, :331 and
//	:344-352 at db021da0 — #accessingFabricIndex = session.fabric ??
//	NO_FABRIC, and for an event whose limits are fabric-sensitive
//	#readAllowedEvents skips a record whose payload.fabricIndex differs; the
//	file has no auth-mode exception.
func AuthorizeEventReports(ctx context.Context, auth EventReadAuthorizer, events []EventReport) []EventReport {
	aclBypass := auth.PASE || IsPASEFromContext(ctx)
	if !aclBypass && auth.Checker == nil {
		return []EventReport{} // no access check: nothing is granted (decision 136)
	}
	out := make([]EventReport, 0, len(events))
	for _, ev := range events {
		if !aclBypass {
			priv := eventScope(ev.Path.Cluster, ev.Path.Event).ReadPrivilege
			if status := checkSubjectACL(ctx, auth.Checker, auth.FabricIndex, auth.SubjectNodeID, auth.SubjectCATs, ev.Path.Endpoint, ev.Path.Cluster, priv); !status.IsSuccess() {
				continue // subject not permitted to read this event — omit
			}
		}
		if eventScope(ev.Path.Cluster, ev.Path.Event).Sensitive {
			recFabric, ok := eventPayloadFabricIndex(ev.Data.Value)
			if !ok || recFabric != auth.FabricIndex {
				continue // fabric-sensitive record owned by another fabric — omit
			}
		}
		out = append(out, ev)
	}
	return out
}

// eventScope is the model's scope of the (cluster, event) pair: its read
// privilege and whether it is fabric-sensitive, generated per event from
// parity/schema.json ([schema.EventReadScope]). An event the model does
// not hold takes its cluster's scope ([schema.ClusterEventScope]): a
// cluster with a fabric-sensitive event treats an unknown one as
// fabric-sensitive too, and a cluster outside the model is View.
func eventScope(cluster, event uint32) schema.EventScope {
	if s, ok := schema.EventReadScope(cluster, event); ok {
		return s
	}
	return schema.ClusterEventScope(cluster)
}

// eventPayloadFabricIndex extracts the FabricIndex a fabric-sensitive event
// payload carries (e.g. AccessControl.AccessControlEntryChanged sets it per
// §9.10.7.1). Mirrors matter.js EventReadResponse.ts #readAllowedEvents
// `const fabricIndex = isObject(payload) ? payload.fabricIndex : undefined`:
// the payload is duck-typed for a FabricIndex field rather than matched to a
// concrete type (the IM layer stays cluster-blind and cannot import the
// cluster packages). Returns (0,false) when the payload is not a struct
// exposing an unsigned FabricIndex field.
func eventPayloadFabricIndex(payload any) (uint8, bool) {
	if payload == nil {
		return 0, false
	}
	v := reflect.ValueOf(payload)
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return 0, false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return 0, false
	}
	f := v.FieldByName("FabricIndex")
	if !f.IsValid() {
		return 0, false
	}
	switch f.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		// A value no fabric index can take is no fabric: masking it would
		// disclose the record to the fabric its low byte names (review
		// II, R2-P6-sec-0511).
		if f.Uint() > 0xFF {
			return 0, false
		}
		return uint8(f.Uint()), true //nolint:gosec // G115: checked ≤ 0xFF above
	default:
		return 0, false
	}
}

// MaxReadPaths is the hard acceptance ceiling for paths (attribute and
// event paths counted together) in a single Read or Subscribe
// interaction. Mirrors matter.js
// packages/node/src/node/server/InteractionServer.ts:90-94 at db021da0
// (MAX_READ_PATHS / MAX_SUBSCRIBE_PATHS = 10 000, the upper bound of the
// CapabilityMinimaStruct field constraints); a request beyond it is
// answered with PathsExhausted (:375-381, :635-641).
const MaxReadPaths = 10_000

// isGlobalAttributeID reports whether attrID is a universal global attribute
// (0xFFF8-0xFFFD): GeneratedCommandList, AcceptedCommandList, EventList,
// AttributeList, FeatureMap, ClusterRevision. These are legal on a
// wildcard-cluster read path; concrete non-global attributes are not.
func isGlobalAttributeID(attrID uint32) bool {
	return attrID >= 0xFFF8 && attrID <= 0xFFFD
}

// ValidateReadPaths enforces the Matter §8.4.3.2 path rules shared by Read and
// Subscribe: a wildcard cluster (HasCluster == false) combined with a concrete
// non-global attribute, or with a concrete event, is illegal — the whole action
// must be rejected up front with InvalidAction rather than silently expanded.
// Returns StatusInvalidAction on the first offending path, StatusPathsExhausted
// when the request names more than [MaxReadPaths] paths in total, else
// StatusSuccess. Mirrors a Read's order in matter.js
// packages/node/src/node/server/InteractionServer.ts at db021da0:
// validateReadPaths (:138-141, over validateReadAttributesPath :118-128 and
// validateReadEventPath :130-136; #3926) at :373, then the path-count
// ceiling (:375-381). The read dispatcher runs this before building any
// report and answers with the returned status. A Subscribe checks in
// another order — the count first (:635-641), the paths at :699 after the
// KeepSubscriptions teardown and the no-paths check — so its dispatcher
// counts before it calls this.
func ValidateReadPaths(attrs []ConcreteAttributePath, events []ConcreteEventPath) StatusCode {
	for _, p := range attrs {
		if !p.HasCluster && p.HasAttribute && !isGlobalAttributeID(p.Attribute) {
			return StatusInvalidAction
		}
	}
	for _, p := range events {
		if !p.HasCluster && p.HasEvent {
			return StatusInvalidAction
		}
	}
	if len(attrs)+len(events) > MaxReadPaths {
		return StatusPathsExhausted
	}
	return StatusSuccess
}

// DedupAttributePaths returns paths with every repeat of an earlier
// AttributePathIB removed, first occurrence first. A repeated path
// expands to exactly the results the first one did, so reading it again
// only duplicates the report. It drops byte-identical repeats only: paths
// that differ in a field the expansion ignores, and overlapping
// wildcards, are each expanded — what bounds the memory of a response is
// that it is generated chunk by chunk ([AttributeExpansion]), not this.
// Returns the input slice when nothing repeats.
func DedupAttributePaths(paths []ConcreteAttributePath) []ConcreteAttributePath {
	if len(paths) < 2 {
		return paths
	}
	seen := make(map[ConcreteAttributePath]struct{}, len(paths))
	out := make([]ConcreteAttributePath, 0, len(paths))
	for _, p := range paths {
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	if len(out) == len(paths) {
		return paths
	}
	return out
}

// DeniedEventPathStatuses returns an UNSUPPORTED_ACCESS EventStatusIB for
// every concrete event path (endpoint, cluster and event named) whose
// events the subject may not read. A denied concrete path is answered with
// its status, a denied wildcard is skipped silently — matter.js
// EventReadResponse.ts #addConcrete returns #asStatus(path, denied) where
// the wildcard expansion just leaves the cluster out. TC-ACL-2.9 reads
// AccessControlEntryChanged without Administer and expects the status.
//
// A concrete path that does not exist answers its existence status —
// UNSUPPORTED_ENDPOINT, UNSUPPORTED_CLUSTER or UNSUPPORTED_EVENT — to a
// subject that passes the View pass, which comes first; the event's own
// read privilege is checked for an event that exists. Mirrors matter.js
// packages/protocol/src/action/server/EventReadResponse.ts:158-224 at
// db021da0 (#addConcrete, core spec §8.4.3.2); such a path used to be
// answered with nothing.
//
// With this node's id stamped ([WithLocalNodeID]), a concrete path naming
// another node answers UNSUPPORTED_NODE before any other check, as
// matter.js's #addConcrete does (EventReadResponse.ts:159-162 at
// db021da0); [EventPathsForNode] keeps its events out of the report.
func DeniedEventPathStatuses(ctx context.Context, auth EventReadAuthorizer, paths []ConcreteEventPath) []EventReport {
	const privilegeView uint8 = 1
	localNodeID, hasLocalNodeID := LocalNodeIDFromContext(ctx)
	var out []EventReport
	for _, p := range paths {
		if !p.HasEndpoint || !p.HasCluster || !p.HasEvent {
			continue
		}
		status := StatusSuccess
		switch {
		case p.HasNode && hasLocalNodeID && p.Node != localNodeID:
			status = StatusUnsupportedNode
		case !auth.allowsAt(ctx, p.Endpoint, p.Cluster, privilegeView):
			status = StatusUnsupportedAccess
		case auth.Paths != nil && !auth.Paths.EventPathStatus(p).IsSuccess():
			status = auth.Paths.EventPathStatus(p)
		case !auth.Allows(ctx, p.Endpoint, p.Cluster):
			status = StatusUnsupportedAccess
		}
		if !status.IsSuccess() {
			out = append(out, EventReport{Path: p, IsStatus: true, Status: StatusIB{Status: status}})
		}
	}
	return out
}
