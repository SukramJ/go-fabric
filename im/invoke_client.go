// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"fmt"

	"github.com/SukramJ/go-fabric/tlv"
)

// The client side of Invoke: the one interaction this node initiates, for
// the OTA Software Update Requestor's commands to its provider (ADR 0019).
// The tags are the server side's; the shape is matter.js's TlvInvokeRequest
// / TlvInvokeResponse (packages/types/src/protocol/messages/).

// MarshalInvokeRequestTLV encodes req as an InvokeRequestMessage, each
// command's fields through fieldsWriter, in matter.js TlvInvokeRequest's
// field order (packages/types/src/protocol/messages/TlvInvokeRequest.ts:17-31
// at 862ad75f): the DelayReportData (tag 3) when req carries one, both its
// fields written (TlvDelayReportData.ts, TlvField with a decode fallback
// only), then req's InteractionModelRevision — this module's
// [MatterInteractionModelRevision] when 0.
func MarshalInvokeRequestTLV(enc *tlv.Encoder, req InvokeRequest, fieldsWriter CommandFieldsWriter) {
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutBool(tlv.ContextTag(tagInvokeReqSuppressResponse), req.SuppressResponse)
	enc.PutBool(tlv.ContextTag(tagInvokeReqTimedRequest), req.TimedRequest)
	enc.StartArray(tlv.ContextTag(tagInvokeReqInvokeRequests))
	for _, inv := range req.Invokes {
		enc.StartStruct(tlv.AnonymousTag())
		inv.Path.MarshalTLV(enc, tlv.ContextTag(tagCmdDataPath))
		fieldsWriter(enc, tlv.ContextTag(tagCmdDataFields), inv.Fields)
		if inv.HasCommandRef {
			enc.PutUint(tlv.ContextTag(tagCmdDataRef), uint64(inv.CommandRef))
		}
		_ = enc.EndContainer()
	}
	_ = enc.EndContainer()
	if d := req.DelayReportData; d != nil {
		enc.StartStruct(tlv.ContextTag(tagInvokeReqDelayReportData))
		enc.PutUint(tlv.ContextTag(0), uint64(d.DelayMinMs))
		enc.PutUint(tlv.ContextTag(1), uint64(d.DelayJitterWindowMs))
		_ = enc.EndContainer()
	}
	revision := req.InteractionModelRevision
	if revision == 0 {
		revision = MatterInteractionModelRevision
	}
	enc.PutUint(tlv.ContextTag(tagInteractionModelRevision), uint64(revision))
	_ = enc.EndContainer()
}

// UnmarshalInvokeResponseTLV decodes an InvokeResponseMessage, each
// command response's fields through fieldsReader (nil skips them).
func UnmarshalInvokeResponseTLV(dec *tlv.Decoder, fieldsReader CommandFieldsReader) (InvokeResponse, error) {
	open, err := dec.Next()
	if err != nil {
		return InvokeResponse{}, err
	}
	if !open.IsContainer || open.Type != tlv.TypeStructure {
		return InvokeResponse{}, fmt.Errorf("%w: InvokeResponse is not a struct", ErrInvalidInvokeRequest)
	}
	var resp InvokeResponse
	for {
		el, err := dec.Next()
		if err != nil {
			return InvokeResponse{}, err
		}
		if el.IsEndContainer {
			return resp, nil
		}
		if el.Tag.Kind != tlv.TagKindContext {
			if el.IsContainer {
				if err := skipContainer(dec); err != nil {
					return InvokeResponse{}, err
				}
			}
			continue
		}
		switch uint8(el.Tag.Number & 0xFF) {
		case tagInvokeRespSuppressResponse:
			if resp.SuppressResponse, err = boolField(el); err != nil {
				return InvokeResponse{}, fmt.Errorf("%w: %w", ErrInvalidInvokeRequest, err)
			}
		case tagInvokeRespMoreChunked:
			if resp.MoreChunkedMessages, err = boolField(el); err != nil {
				return InvokeResponse{}, fmt.Errorf("%w: %w", ErrInvalidInvokeRequest, err)
			}
		case tagInvokeRespResponses:
			if !el.IsContainer || el.Type != tlv.TypeArray {
				return InvokeResponse{}, fmt.Errorf("%w: InvokeResponses not array", ErrInvalidInvokeRequest)
			}
			if resp.Responses, err = readInvokeResponseIBs(dec, fieldsReader); err != nil {
				return InvokeResponse{}, err
			}
		default:
			if el.IsContainer {
				if err := skipContainer(dec); err != nil {
					return InvokeResponse{}, err
				}
			}
		}
	}
}

// readInvokeResponseIBs reads the InvokeResponses array after its opener.
func readInvokeResponseIBs(dec *tlv.Decoder, fieldsReader CommandFieldsReader) ([]InvokeResponseEntry, error) {
	var out []InvokeResponseEntry
	for {
		ib, err := dec.Next()
		if err != nil {
			return nil, err
		}
		if ib.IsEndContainer {
			return out, nil
		}
		if !ib.IsContainer || ib.Type != tlv.TypeStructure {
			return nil, fmt.Errorf("%w: InvokeResponseIB not struct", ErrInvalidInvokeRequest)
		}
		ent, err := readInvokeResponseIB(dec, fieldsReader)
		if err != nil {
			return nil, err
		}
		out = append(out, ent)
	}
}

// readInvokeResponseIB reads one InvokeResponseIB after its opener: a
// CommandDataIB (tag 0) or a CommandStatusIB (tag 1).
//
// Exactly one of the two is present, as in every InvokeResponseIB
// matter.js decodes (TlvInvokeResponseData.ts); a block with neither or
// both is refused, not read as a success (review II, R2-P1-req2-09).
func readInvokeResponseIB(dec *tlv.Decoder, fieldsReader CommandFieldsReader) (InvokeResponseEntry, error) {
	var ent InvokeResponseEntry
	members := 0
	for {
		el, err := dec.Next()
		if err != nil {
			return InvokeResponseEntry{}, err
		}
		if el.IsEndContainer {
			if members != 1 {
				return InvokeResponseEntry{}, fmt.Errorf("%w: InvokeResponseIB carries %d of CommandDataIB and CommandStatusIB, want one", ErrInvalidInvokeRequest, members)
			}
			return ent, nil
		}
		if el.Tag.Kind != tlv.TagKindContext || !el.IsContainer {
			if el.IsContainer {
				if err := skipContainer(dec); err != nil {
					return InvokeResponseEntry{}, err
				}
			}
			continue
		}
		switch uint8(el.Tag.Number & 0xFF) {
		case tagInvokeRespCommandData:
			inv, err := readCommandInvocation(dec, fieldsReader)
			if err != nil {
				return InvokeResponseEntry{}, err
			}
			ent.Path, ent.Response, ent.HasResponse = inv.Path, inv.Fields, inv.Fields != nil
			ent.CommandRef, ent.HasCommandRef = inv.CommandRef, inv.HasCommandRef
			members++
		case tagInvokeRespCommandStatus:
			if err := readCommandStatusIB(dec, &ent); err != nil {
				return InvokeResponseEntry{}, err
			}
			ent.IsStatus = true
			members++
		default:
			if err := skipContainer(dec); err != nil {
				return InvokeResponseEntry{}, err
			}
		}
	}
}

// readCommandStatusIB reads a CommandStatusIB after its opener. Its path
// and its status are mandatory (matter.js TlvCommandStatus.ts:16-17 at
// db021da0, both TlvField).
func readCommandStatusIB(dec *tlv.Decoder, ent *InvokeResponseEntry) error {
	sawPath, sawStatus := false, false
	for {
		el, err := dec.Next()
		if err != nil {
			return err
		}
		if el.IsEndContainer {
			if !sawPath || !sawStatus {
				return fmt.Errorf("%w: CommandStatusIB without its path or status", ErrInvalidInvokeRequest)
			}
			return nil
		}
		if el.Tag.Kind != tlv.TagKindContext {
			if el.IsContainer {
				if err := skipContainer(dec); err != nil {
					return err
				}
			}
			continue
		}
		switch uint8(el.Tag.Number & 0xFF) {
		case tagCmdStatusPath:
			if !el.IsContainer || el.Type != tlv.TypeList {
				return fmt.Errorf("%w: CommandPathIB not list", ErrInvalidInvokeRequest)
			}
			p, err := readCommandPathFields(dec)
			if err != nil {
				return err
			}
			ent.Path, sawPath = p, true
		case tagCmdStatusStatus:
			if !el.IsContainer || el.Type != tlv.TypeStructure {
				return fmt.Errorf("%w: StatusIB not struct", ErrInvalidStatusIB)
			}
			if err := readStatusIBFields(dec, &ent.Status); err != nil {
				return err
			}
			sawStatus = true
		case tagCmdStatusRef:
			// TlvUInt16 (TlvCommandStatus.ts:18): refused when wider, never
			// masked onto another command's reference ([uintField]).
			ref, err := uint16Field(el)
			if err != nil {
				return err
			}
			ent.CommandRef = ref
			ent.HasCommandRef = true
		default:
			if el.IsContainer {
				if err := skipContainer(dec); err != nil {
					return err
				}
			}
		}
	}
}

// readStatusIBFields reads a StatusIB's fields after its opener. Status
// is mandatory (matter.js TlvStatus.ts:15 at db021da0), as
// [UnmarshalStatusIBTLV] holds it: a block without it is refused, not
// read as Success; a member it does not know is skipped whole (review
// II, R2-P1-req2-09).
func readStatusIBFields(dec *tlv.Decoder, st *StatusIB) error {
	sawStatus := false
	for {
		f, err := dec.Next()
		if err != nil {
			return err
		}
		if f.IsEndContainer {
			if !sawStatus {
				return fmt.Errorf("%w: missing Status", ErrInvalidStatusIB)
			}
			return nil
		}
		if f.Tag.Kind != tlv.TagKindContext {
			if err := skipElement(dec, f); err != nil {
				return err
			}
			continue
		}
		switch uint8(f.Tag.Number & 0xFF) {
		case tagStatusIBStatus:
			status, err := statusIBField(f)
			if err != nil {
				return err
			}
			st.Status = StatusCode(status)
			sawStatus = true
		case tagStatusIBClusterStatus:
			if st.ClusterStatus, err = statusIBField(f); err != nil {
				return err
			}
			st.HasClusterStatus = true
		default:
			if err := skipElement(dec, f); err != nil {
				return err
			}
		}
	}
}
