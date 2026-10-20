// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"errors"
	"fmt"

	"github.com/SukramJ/go-fabric/tlv"
)

// The client side of the Interaction Model messages: the encoders of the
// requests a controller sends and the decoders of the answers it receives
// (ADR 0020, stage 3). The shapes are matter.js's TLV message definitions,
// packages/types/src/protocol/messages/Tlv*.ts and the information blocks
// of packages/types/src/protocol/types/ at 0ceb202d; the tags are the
// server side's.

// Errors of the client decoders.
var (
	// ErrInvalidWriteResponse is returned for a malformed WriteResponse.
	ErrInvalidWriteResponse = errors.New("im: invalid WriteResponse")
	// ErrInvalidSubscribeResponse is returned for a malformed
	// SubscribeResponse.
	ErrInvalidSubscribeResponse = errors.New("im: invalid SubscribeResponse")
	// ErrInvalidReportData is returned for a malformed ReportData.
	ErrInvalidReportData = errors.New("im: invalid ReportData")
)

// marshalAttributePaths writes the AttributePathIB array at tag, nothing
// for an empty list (TlvOptionalField(TlvArray(TlvAttributePath))).
func marshalAttributePaths(enc *tlv.Encoder, tag uint8, paths []ConcreteAttributePath) {
	if len(paths) == 0 {
		return
	}
	enc.StartArray(tlv.ContextTag(tag))
	for _, p := range paths {
		p.MarshalTLV(enc, tlv.AnonymousTag())
	}
	_ = enc.EndContainer()
}

// marshalEventPaths writes the EventPathIB array at tag, nothing for an
// empty list.
func marshalEventPaths(enc *tlv.Encoder, tag uint8, paths []ConcreteEventPath) {
	if len(paths) == 0 {
		return
	}
	enc.StartArray(tlv.ContextTag(tag))
	for _, p := range paths {
		p.MarshalTLV(enc, tlv.AnonymousTag())
	}
	_ = enc.EndContainer()
}

// marshalEventFilters writes the EventFilterIB array at tag, nothing for
// an empty list: each a structure of NodeId (0, optional) and EventMin (1)
// — matter.js packages/types/src/protocol/types/TlvEventFilter.ts.
func marshalEventFilters(enc *tlv.Encoder, tag uint8, filters []EventMinimumNumber) {
	if len(filters) == 0 {
		return
	}
	enc.StartArray(tlv.ContextTag(tag))
	for _, f := range filters {
		enc.StartStruct(tlv.AnonymousTag())
		if f.HasNodeID {
			enc.PutUint(tlv.ContextTag(0), f.NodeID)
		}
		enc.PutUint(tlv.ContextTag(1), f.EventMin)
		_ = enc.EndContainer()
	}
	_ = enc.EndContainer()
}

// marshalDataVersionFilters writes the DataVersionFilterIB array at tag,
// nothing for an empty list: each a structure of Path (0, a ClusterPathIB
// list of EndpointId (1) and ClusterId (2)) and DataVersion (1) — matter.js
// TlvDataVersionFilter.ts and TlvClusterPath.ts.
func marshalDataVersionFilters(enc *tlv.Encoder, tag uint8, filters []DataVersionFilter) {
	if len(filters) == 0 {
		return
	}
	enc.StartArray(tlv.ContextTag(tag))
	for _, f := range filters {
		marshalDataVersionFilter(enc, f)
	}
	_ = enc.EndContainer()
}

func marshalDataVersionFilter(enc *tlv.Encoder, f DataVersionFilter) {
	enc.StartStruct(tlv.AnonymousTag())
	enc.StartList(tlv.ContextTag(0))
	enc.PutUint(tlv.ContextTag(1), uint64(f.Endpoint))
	enc.PutUint(tlv.ContextTag(2), uint64(f.Cluster))
	_ = enc.EndContainer()
	enc.PutUint(tlv.ContextTag(1), uint64(f.DataVersion))
	_ = enc.EndContainer()
}

// DataVersionFilterSize is the encoded size of f as one member of the
// DataVersionFilters array — what matter.js's #shortenDataVersionFilters
// measures with TlvDataVersionFilter.encode
// (interaction/InteractionMessenger.ts:1036-1037).
func DataVersionFilterSize(f DataVersionFilter) int {
	enc := tlv.NewEncoder()
	marshalDataVersionFilter(enc, f)
	b, _ := enc.Bytes()
	return len(b)
}

// MarshalTLV encodes w as a client sends it. Mirrors matter.js
// packages/types/src/protocol/messages/TlvWriteRequest.ts:16-27 at
// 0ceb202d with the request action/request/Write.ts:56-184 builds:
// SuppressResponse (0) only when set, TimedRequest (1), WriteRequests (2)
// of AttributeDataIB — DataVersion (0) when set, Path (1), Data (2)
// through valueWriter (TlvAttributeData.ts) — MoreChunkedMessages (3),
// which a client sends false (Write.ts:77), and InteractionModelRevision
// (0xFF).
func (w WriteRequest) MarshalTLV(enc *tlv.Encoder, valueWriter AttributeValueWriter) {
	enc.StartStruct(tlv.AnonymousTag())
	if w.SuppressResponse {
		enc.PutBool(tlv.ContextTag(tagWriteReqSuppressResponse), true)
	}
	enc.PutBool(tlv.ContextTag(tagWriteReqTimedRequest), w.TimedRequest)
	enc.StartArray(tlv.ContextTag(tagWriteReqWriteRequests))
	for _, a := range w.Writes {
		enc.StartStruct(tlv.AnonymousTag())
		if a.HasDataVersion {
			enc.PutUint(tlv.ContextTag(tagAttributeDataDataVersion), uint64(a.DataVersion))
		}
		a.Path.MarshalTLV(enc, tlv.ContextTag(tagAttributeDataPath))
		valueWriter(enc, tlv.ContextTag(tagAttributeDataValue), a.Value)
		_ = enc.EndContainer()
	}
	_ = enc.EndContainer()
	enc.PutBool(tlv.ContextTag(tagWriteReqMoreChunked), w.MoreChunkedMessages)
	enc.PutUint(tlv.ContextTag(tagInteractionModelRevision), uint64(MatterInteractionModelRevision))
	_ = enc.EndContainer()
}

// MarshalTLV encodes r as a client sends it — matter.js
// packages/types/src/protocol/messages/TlvTimedRequest.ts:13-17 (Timeout
// (0) in milliseconds, InteractionModelRevision (0xFF)), as
// InteractionClientMessenger.sendTimedRequest fills it
// (interaction/InteractionMessenger.ts:1124-1136).
func (r TimedRequest) MarshalTLV(enc *tlv.Encoder) {
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutUint(tlv.ContextTag(tagTimedReqTimeout), uint64(r.TimeoutMs))
	enc.PutUint(tlv.ContextTag(tagTimedReqInteractionModelRevision), uint64(MatterInteractionModelRevision))
	_ = enc.EndContainer()
}

// openStruct reads the anonymous structure that opens a message.
func openStruct(dec *tlv.Decoder, class error) error {
	open, err := dec.Next()
	if err != nil {
		return fmt.Errorf("%w: %w", class, err)
	}
	if !open.IsContainer || open.Type != tlv.TypeStructure {
		return fmt.Errorf("%w: expected a structure, got 0x%02X", class, open.Type)
	}
	return nil
}

// isUnsigned reports whether el is an unsigned integer of any width.
func isUnsigned(el tlv.Element) bool {
	return el.Type >= tlv.TypeUnsignedInt1 && el.Type <= tlv.TypeUnsignedInt8
}

// skipElement drains el when it opened a container.
func skipElement(dec *tlv.Decoder, el tlv.Element) error {
	if el.IsContainer {
		return skipContainer(dec)
	}
	return nil
}

// UnmarshalWriteResponseTLV decodes a WriteResponseMessage — matter.js
// TlvWriteResponse.ts:15-19: WriteResponses (0), an array of
// AttributeStatusIB (TlvAttributeStatus.ts: Path (0), Status (1)).
func UnmarshalWriteResponseTLV(dec *tlv.Decoder) (WriteResponse, error) {
	if err := openStruct(dec, ErrInvalidWriteResponse); err != nil {
		return WriteResponse{}, err
	}
	var wr WriteResponse
	for {
		el, err := dec.Next()
		if err != nil {
			return WriteResponse{}, fmt.Errorf("%w: %w", ErrInvalidWriteResponse, err)
		}
		if el.IsEndContainer {
			return wr, nil
		}
		if el.Tag.Kind != tlv.TagKindContext || el.Tag.Number != uint32(tagWriteRespResponses) {
			if err := skipElement(dec, el); err != nil {
				return WriteResponse{}, fmt.Errorf("%w: %w", ErrInvalidWriteResponse, err)
			}
			continue
		}
		if !el.IsContainer || el.Type != tlv.TypeArray {
			return WriteResponse{}, fmt.Errorf("%w: WriteResponses is not an array", ErrInvalidWriteResponse)
		}
		for {
			ib, err := dec.Next()
			if err != nil {
				return WriteResponse{}, fmt.Errorf("%w: %w", ErrInvalidWriteResponse, err)
			}
			if ib.IsEndContainer {
				break
			}
			if !ib.IsContainer || ib.Type != tlv.TypeStructure {
				return WriteResponse{}, fmt.Errorf("%w: AttributeStatusIB is not a structure", ErrInvalidWriteResponse)
			}
			st, _, err := readAttributeStatusFields(dec)
			if err != nil {
				return WriteResponse{}, fmt.Errorf("%w: %w", ErrInvalidWriteResponse, err)
			}
			wr.Responses = append(wr.Responses, st)
		}
	}
}

// readAttributeStatusFields reads an AttributeStatusIB after its opener,
// with its path's EnableTagCompression flag.
func readAttributeStatusFields(dec *tlv.Decoder) (st AttributeStatus, compressed bool, err error) {
	for {
		el, err := dec.Next()
		if err != nil {
			return AttributeStatus{}, false, err
		}
		if el.IsEndContainer {
			return st, compressed, nil
		}
		switch {
		case el.Tag.Kind == tlv.TagKindContext && el.Tag.Number == uint32(tagAttributeStatusPath) && el.IsContainer && el.Type == tlv.TypeList:
			if st.Path, compressed, err = readReportPathFields(dec); err != nil {
				return AttributeStatus{}, false, err
			}
		case el.Tag.Kind == tlv.TagKindContext && el.Tag.Number == uint32(tagAttributeStatusStatus) && el.IsContainer && el.Type == tlv.TypeStructure:
			if err := readStatusIBFields(dec, &st.Status); err != nil {
				return AttributeStatus{}, false, err
			}
		default:
			if err := skipElement(dec, el); err != nil {
				return AttributeStatus{}, false, err
			}
		}
	}
}

// readReportPathFields reads an AttributePathIB after its opener —
// TlvAttributePath.ts: EnableTagCompression (0), Node (1), Endpoint (2),
// Cluster (3), Attribute (4), ListIndex (5, nullable) — returning the
// EnableTagCompression flag beside the path.
func readReportPathFields(dec *tlv.Decoder) (p ConcreteAttributePath, compressed bool, err error) {
	for {
		el, err := dec.Next()
		if err != nil {
			return ConcreteAttributePath{}, false, fmt.Errorf("%w: %w", ErrInvalidPath, err)
		}
		if el.IsEndContainer {
			return p, compressed, nil
		}
		if el.Tag.Kind != tlv.TagKindContext {
			if err := skipElement(dec, el); err != nil {
				return ConcreteAttributePath{}, false, err
			}
			continue
		}
		switch uint8(el.Tag.Number & 0xFF) {
		case tagAttrPathEnableTagCompression:
			compressed = el.Bool
		case tagAttrPathNode:
			p.Node, p.HasNode = el.Uint, true
		case tagAttrPathEndpoint:
			p.Endpoint, p.HasEndpoint = uint16(el.Uint&0xFFFF), true
		case tagAttrPathCluster:
			p.Cluster, p.HasCluster = uint32(el.Uint&0xFFFFFFFF), true
		case tagAttrPathAttribute:
			p.Attribute, p.HasAttribute = uint32(el.Uint&0xFFFFFFFF), true
		case tagAttrPathListIndex:
			p.ListIndex, p.HasListIndex, p.ListAppend = uint16(el.Uint&0xFFFF), true, el.IsNull
		default:
			if err := skipElement(dec, el); err != nil {
				return ConcreteAttributePath{}, false, err
			}
		}
	}
}

// UnmarshalSubscribeResponseTLV decodes a SubscribeResponseMessage —
// matter.js TlvSubscribeResponse.ts:13-21: SubscriptionId (0) and
// MaxInterval (2), both mandatory.
func UnmarshalSubscribeResponseTLV(dec *tlv.Decoder) (SubscribeResponse, error) {
	if err := openStruct(dec, ErrInvalidSubscribeResponse); err != nil {
		return SubscribeResponse{}, err
	}
	var sr SubscribeResponse
	var haveID, haveMax bool
	for {
		el, err := dec.Next()
		if err != nil {
			return SubscribeResponse{}, fmt.Errorf("%w: %w", ErrInvalidSubscribeResponse, err)
		}
		if el.IsEndContainer {
			break
		}
		switch {
		case el.Tag.Kind == tlv.TagKindContext && el.Tag.Number == uint32(tagSubRespSubscriptionID) && isUnsigned(el):
			if el.Uint > 0xFFFFFFFF {
				return SubscribeResponse{}, fmt.Errorf("%w: SubscriptionId exceeds uint32", ErrInvalidSubscribeResponse)
			}
			sr.SubscriptionID, haveID = uint32(el.Uint), true
		case el.Tag.Kind == tlv.TagKindContext && el.Tag.Number == uint32(tagSubRespMaxInterval) && isUnsigned(el):
			if el.Uint > 0xFFFF {
				return SubscribeResponse{}, fmt.Errorf("%w: MaxInterval exceeds uint16", ErrInvalidSubscribeResponse)
			}
			sr.MaxInterval, haveMax = uint16(el.Uint), true
		default:
			if err := skipElement(dec, el); err != nil {
				return SubscribeResponse{}, fmt.Errorf("%w: %w", ErrInvalidSubscribeResponse, err)
			}
		}
	}
	if !haveID || !haveMax {
		return SubscribeResponse{}, fmt.Errorf("%w: SubscriptionId and MaxInterval are mandatory", ErrInvalidSubscribeResponse)
	}
	return sr, nil
}

// EventDataReader extracts an event's Data from the TLV stream, as
// [AttributeValueReader] does an attribute value: dec is positioned after
// el, and a container's members are the reader's to drain.
type EventDataReader func(path ConcreteEventPath, el tlv.Element, dec *tlv.Decoder) (AttributeValue, error)

// UnmarshalReportDataTLV decodes a ReportDataMessage as a client receives
// it — matter.js packages/types/src/protocol/messages/TlvDataReport.ts:17-31
// at 0ceb202d: SubscriptionId (0), AttributeReports (1), EventReports (2),
// MoreChunkedMessages (3), SuppressResponse (4). An AttributeReportIB is
// a status (AttributeStatusIB) or data (AttributeDataIB: DataVersion (0,
// optional), Path (1), Data (2), the value read through valueReader); an
// EventReportIB a status (EventStatusIB) or data (EventDataIB: Path (0),
// EventNumber (1), Priority (2), the four optional timestamps (3-6), Data
// (7, optional) through eventReader) — TlvAttributeReport.ts,
// TlvAttributeReportData.ts, TlvEventReport.ts, TlvEventData.ts. A nil
// reader skips the values. The path's EnableTagCompression flag is kept on
// the report for the client to resolve (InputChunk.ts:252-273).
func UnmarshalReportDataTLV(dec *tlv.Decoder, valueReader AttributeValueReader, eventReader EventDataReader) (ReportData, error) {
	if err := openStruct(dec, ErrInvalidReportData); err != nil {
		return ReportData{}, err
	}
	var rd ReportData
	for {
		el, err := dec.Next()
		if err != nil {
			return ReportData{}, fmt.Errorf("%w: %w", ErrInvalidReportData, err)
		}
		if el.IsEndContainer {
			return rd, nil
		}
		if el.Tag.Kind != tlv.TagKindContext {
			if err := skipElement(dec, el); err != nil {
				return ReportData{}, fmt.Errorf("%w: %w", ErrInvalidReportData, err)
			}
			continue
		}
		switch uint8(el.Tag.Number & 0xFF) {
		case tagReportSubscriptionID:
			if el.Uint > 0xFFFFFFFF {
				return ReportData{}, fmt.Errorf("%w: SubscriptionId exceeds uint32", ErrInvalidReportData)
			}
			rd.SubscriptionID, rd.HasSubscription = uint32(el.Uint), true
		case tagReportAttributeReports:
			if !el.IsContainer || el.Type != tlv.TypeArray {
				return ReportData{}, fmt.Errorf("%w: AttributeReports is not an array", ErrInvalidReportData)
			}
			if rd.Reports, err = readAttributeReports(dec, valueReader); err != nil {
				return ReportData{}, fmt.Errorf("%w: %w", ErrInvalidReportData, err)
			}
		case tagReportEventReports:
			if !el.IsContainer || el.Type != tlv.TypeArray {
				return ReportData{}, fmt.Errorf("%w: EventReports is not an array", ErrInvalidReportData)
			}
			if rd.EventReports, err = readEventReports(dec, eventReader); err != nil {
				return ReportData{}, fmt.Errorf("%w: %w", ErrInvalidReportData, err)
			}
		case tagReportMoreChunkedMessages:
			rd.MoreChunkedMessages = el.Bool
		case tagReportSuppressResponse:
			rd.SuppressResponse = el.Bool
		default:
			if err := skipElement(dec, el); err != nil {
				return ReportData{}, fmt.Errorf("%w: %w", ErrInvalidReportData, err)
			}
		}
	}
}

// readAttributeReports reads the AttributeReportIB array after its opener.
func readAttributeReports(dec *tlv.Decoder, valueReader AttributeValueReader) ([]AttributeReport, error) {
	var out []AttributeReport
	for {
		ib, err := dec.Next()
		if err != nil {
			return nil, err
		}
		if ib.IsEndContainer {
			return out, nil
		}
		if !ib.IsContainer || ib.Type != tlv.TypeStructure {
			return nil, errors.New("im: AttributeReportIB is not a structure")
		}
		rep, err := readAttributeReport(dec, valueReader)
		if err != nil {
			return nil, err
		}
		out = append(out, rep)
	}
}

// readAttributeReport reads one AttributeReportIB after its opener.
func readAttributeReport(dec *tlv.Decoder, valueReader AttributeValueReader) (AttributeReport, error) {
	var rep AttributeReport
	for {
		el, err := dec.Next()
		if err != nil {
			return AttributeReport{}, err
		}
		if el.IsEndContainer {
			return rep, nil
		}
		switch {
		case el.Tag.Kind == tlv.TagKindContext && el.Tag.Number == uint32(tagAttributeReportStatus) && el.IsContainer && el.Type == tlv.TypeStructure:
			st, compressed, err := readAttributeStatusFields(dec)
			if err != nil {
				return AttributeReport{}, err
			}
			rep.IsStatus, rep.Path, rep.Status, rep.TagCompression = true, st.Path, st.Status, compressed
		case el.Tag.Kind == tlv.TagKindContext && el.Tag.Number == uint32(tagAttributeReportData) && el.IsContainer && el.Type == tlv.TypeStructure:
			if err := readAttributeReportData(dec, &rep, valueReader); err != nil {
				return AttributeReport{}, err
			}
		default:
			if err := skipElement(dec, el); err != nil {
				return AttributeReport{}, err
			}
		}
	}
}

// readAttributeReportData reads an AttributeDataIB after its opener into
// rep. The path precedes the data on the wire (TlvAttributeReportData.ts
// field order), so the reader sees the path.
func readAttributeReportData(dec *tlv.Decoder, rep *AttributeReport, valueReader AttributeValueReader) error {
	for {
		el, err := dec.Next()
		if err != nil {
			return err
		}
		if el.IsEndContainer {
			return nil
		}
		if el.Tag.Kind != tlv.TagKindContext {
			if err := skipElement(dec, el); err != nil {
				return err
			}
			continue
		}
		switch uint8(el.Tag.Number & 0xFF) {
		case tagAttributeDataDataVersion:
			rep.DataVersion, rep.HasDataVersion = uint32(el.Uint&0xFFFFFFFF), true
		case tagAttributeDataPath:
			if !el.IsContainer || el.Type != tlv.TypeList {
				return errors.New("im: AttributeDataIB path is not a list")
			}
			if rep.Path, rep.TagCompression, err = readReportPathFields(dec); err != nil {
				return err
			}
		case tagAttributeDataValue:
			if valueReader == nil {
				if err := skipElement(dec, el); err != nil {
					return err
				}
				continue
			}
			if rep.Value, err = valueReader(rep.Path, el, dec); err != nil {
				return err
			}
		default:
			if err := skipElement(dec, el); err != nil {
				return err
			}
		}
	}
}

// readEventReports reads the EventReportIB array after its opener.
func readEventReports(dec *tlv.Decoder, eventReader EventDataReader) ([]EventReport, error) {
	var out []EventReport
	for {
		ib, err := dec.Next()
		if err != nil {
			return nil, err
		}
		if ib.IsEndContainer {
			return out, nil
		}
		if !ib.IsContainer || ib.Type != tlv.TypeStructure {
			return nil, errors.New("im: EventReportIB is not a structure")
		}
		var rep EventReport
		for {
			el, err := dec.Next()
			if err != nil {
				return nil, err
			}
			if el.IsEndContainer {
				break
			}
			switch {
			case el.Tag.Kind == tlv.TagKindContext && el.Tag.Number == uint32(tagEventReportStatus) && el.IsContainer && el.Type == tlv.TypeStructure:
				if err := readEventStatusFields(dec, &rep); err != nil {
					return nil, err
				}
				rep.IsStatus = true
			case el.Tag.Kind == tlv.TagKindContext && el.Tag.Number == uint32(tagEventReportData) && el.IsContainer && el.Type == tlv.TypeStructure:
				if err := readEventDataFields(dec, &rep, eventReader); err != nil {
					return nil, err
				}
			default:
				if err := skipElement(dec, el); err != nil {
					return nil, err
				}
			}
		}
		out = append(out, rep)
	}
}

// readEventStatusFields reads an EventStatusIB after its opener
// (TlvEventStatus.ts: Path (0), Status (1)).
func readEventStatusFields(dec *tlv.Decoder, rep *EventReport) error {
	for {
		el, err := dec.Next()
		if err != nil {
			return err
		}
		if el.IsEndContainer {
			return nil
		}
		switch {
		case el.Tag.Kind == tlv.TagKindContext && el.Tag.Number == uint32(tagEventStatusPath) && el.IsContainer && el.Type == tlv.TypeList:
			if rep.Path, err = readEventPathFields(dec); err != nil {
				return err
			}
		case el.Tag.Kind == tlv.TagKindContext && el.Tag.Number == uint32(tagEventStatusStatus) && el.IsContainer && el.Type == tlv.TypeStructure:
			if err := readStatusIBFields(dec, &rep.Status); err != nil {
				return err
			}
		default:
			if err := skipElement(dec, el); err != nil {
				return err
			}
		}
	}
}

// readEventDataFields reads an EventDataIB after its opener
// (TlvEventData.ts: Path (0), EventNumber (1), Priority (2),
// EpochTimestamp (3), SystemTimestamp (4), DeltaEpochTimestamp (5),
// DeltaSystemTimestamp (6), Data (7)).
func readEventDataFields(dec *tlv.Decoder, rep *EventReport, eventReader EventDataReader) error {
	rep.Received = &ReceivedEventData{}
	for {
		el, err := dec.Next()
		if err != nil {
			return err
		}
		if el.IsEndContainer {
			return nil
		}
		if el.Tag.Kind != tlv.TagKindContext {
			if err := skipElement(dec, el); err != nil {
				return err
			}
			continue
		}
		switch uint8(el.Tag.Number & 0xFF) {
		case tagEventDataPath:
			if !el.IsContainer || el.Type != tlv.TypeList {
				return errors.New("im: EventDataIB path is not a list")
			}
			if rep.Path, err = readEventPathFields(dec); err != nil {
				return err
			}
		case tagEventDataNumber:
			rep.Number = el.Uint
		case tagEventDataPriority:
			rep.Priority = EventPriority(el.Uint & 0xFF)
		case tagEventDataEpochTimestamp:
			rep.Timestamp, rep.Received.HasEpochTimestamp = el.Uint, true
		case tagEventDataSystemTimestamp:
			rep.Received.SystemTimestamp, rep.Received.HasSystemTimestamp = el.Uint, true
		case tagEventDataDeltaEpochTimestamp:
			rep.Received.DeltaEpochTimestamp, rep.Received.HasDeltaEpochTimestamp = el.Uint, true
		case tagEventDataDeltaSystemTimestamp:
			rep.Received.DeltaSystemTimestamp, rep.Received.HasDeltaSystemTimestamp = el.Uint, true
		case tagEventDataData:
			rep.Received.HasData = true
			if eventReader == nil {
				if err := skipElement(dec, el); err != nil {
					return err
				}
				continue
			}
			if rep.Data, err = eventReader(rep.Path, el, dec); err != nil {
				return err
			}
		default:
			if err := skipElement(dec, el); err != nil {
				return err
			}
		}
	}
}
