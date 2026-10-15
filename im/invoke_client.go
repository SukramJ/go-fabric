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
// command's fields through fieldsWriter, stamped with this module's
// InteractionModelRevision.
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
	enc.PutUint(tlv.ContextTag(tagInteractionModelRevision), uint64(MatterInteractionModelRevision))
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
			resp.SuppressResponse = el.Bool
		case tagInvokeRespMoreChunked:
			resp.MoreChunkedMessages = el.Bool
		case tagInvokeRespResponses:
			if !el.IsContainer || el.Type != tlv.TypeArray {
				return InvokeResponse{}, fmt.Errorf("%w: InvokeResponses not array", ErrInvalidInvokeRequest)
			}
			for {
				ib, err := dec.Next()
				if err != nil {
					return InvokeResponse{}, err
				}
				if ib.IsEndContainer {
					break
				}
				if !ib.IsContainer || ib.Type != tlv.TypeStructure {
					return InvokeResponse{}, fmt.Errorf("%w: InvokeResponseIB not struct", ErrInvalidInvokeRequest)
				}
				ent, err := readInvokeResponseIB(dec, fieldsReader)
				if err != nil {
					return InvokeResponse{}, err
				}
				resp.Responses = append(resp.Responses, ent)
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

// readInvokeResponseIB reads one InvokeResponseIB after its opener: a
// CommandDataIB (tag 0) or a CommandStatusIB (tag 1).
func readInvokeResponseIB(dec *tlv.Decoder, fieldsReader CommandFieldsReader) (InvokeResponseEntry, error) {
	var ent InvokeResponseEntry
	for {
		el, err := dec.Next()
		if err != nil {
			return InvokeResponseEntry{}, err
		}
		if el.IsEndContainer {
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
		case tagInvokeRespCommandStatus:
			if err := readCommandStatusIB(dec, &ent); err != nil {
				return InvokeResponseEntry{}, err
			}
			ent.IsStatus = true
		default:
			if err := skipContainer(dec); err != nil {
				return InvokeResponseEntry{}, err
			}
		}
	}
}

// readCommandStatusIB reads a CommandStatusIB after its opener.
func readCommandStatusIB(dec *tlv.Decoder, ent *InvokeResponseEntry) error {
	for {
		el, err := dec.Next()
		if err != nil {
			return err
		}
		if el.IsEndContainer {
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
			ent.Path = p
		case tagCmdStatusStatus:
			if !el.IsContainer || el.Type != tlv.TypeStructure {
				return fmt.Errorf("%w: StatusIB not struct", ErrInvalidStatusIB)
			}
			if err := readStatusIBFields(dec, &ent.Status); err != nil {
				return err
			}
		case tagCmdStatusRef:
			ent.CommandRef = uint16(el.Uint & 0xFFFF)
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

// readStatusIBFields reads a StatusIB's fields after its opener.
func readStatusIBFields(dec *tlv.Decoder, st *StatusIB) error {
	for {
		f, err := dec.Next()
		if err != nil {
			return err
		}
		if f.IsEndContainer {
			return nil
		}
		if f.Tag.Kind != tlv.TagKindContext {
			continue
		}
		switch uint8(f.Tag.Number & 0xFF) {
		case tagStatusIBStatus:
			st.Status = StatusCode(f.Uint & 0xFF)
		case tagStatusIBClusterStatus:
			st.ClusterStatus = uint8(f.Uint & 0xFF)
			st.HasClusterStatus = true
		}
	}
}
