// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"reflect"
	"testing"

	"github.com/SukramJ/go-fabric/tlv"
)

// unknownTag is a context tag no structure of this package defines.
const unknownTag = 0x1D

// TestStructureDecodersSkipAnUnknownMemberWhole is the standing guard of
// R2-P6-wire-0504 (review II): every structure decoder skips a member
// whose context tag it does not know as one whole element — a container
// there that holds a look-alike of a real field changes nothing, as
// matter.js TlvObject skips an unknown context tag whole
// (packages/types/src/tlv/TlvObject.ts:215-219 at db021da0). Before, the
// container's members were read as the enclosing structure's: an invoke
// ran another command, a read flipped FabricFiltered, a TimedRequest
// lost its Timeout.
func TestStructureDecodersSkipAnUnknownMemberWhole(t *testing.T) {
	t.Parallel()
	// decoy opens an unknown structure that holds what put writes.
	decoy := func(enc *tlv.Encoder, put func(*tlv.Encoder)) {
		enc.StartStruct(tlv.ContextTag(unknownTag))
		put(enc)
		_ = enc.EndContainer()
	}
	cmdPath := func(enc *tlv.Encoder, tag uint8, ep uint16, cluster, cmd uint32) {
		enc.StartList(tlv.ContextTag(tag))
		enc.PutUint(tlv.ContextTag(tagCmdPathEndpoint), uint64(ep))
		enc.PutUint(tlv.ContextTag(tagCmdPathCluster), uint64(cluster))
		enc.PutUint(tlv.ContextTag(tagCmdPathCommand), uint64(cmd))
		_ = enc.EndContainer()
	}
	attrPath := func(enc *tlv.Encoder, tag tlv.Tag, ep uint16, cluster, attr uint32) {
		enc.StartList(tag)
		enc.PutUint(tlv.ContextTag(tagAttrPathEndpoint), uint64(ep))
		enc.PutUint(tlv.ContextTag(tagAttrPathCluster), uint64(cluster))
		enc.PutUint(tlv.ContextTag(tagAttrPathAttribute), uint64(attr))
		_ = enc.EndContainer()
	}
	cases := []struct {
		name   string
		encode func(enc *tlv.Encoder, withDecoy bool)
		decode func(*tlv.Decoder) (any, error)
	}{
		{"InvokeRequest / CommandDataIB", func(enc *tlv.Encoder, d bool) {
			enc.StartStruct(tlv.AnonymousTag())
			enc.PutBool(tlv.ContextTag(tagInvokeReqSuppressResponse), false)
			enc.PutBool(tlv.ContextTag(tagInvokeReqTimedRequest), false)
			enc.StartArray(tlv.ContextTag(tagInvokeReqInvokeRequests))
			enc.StartStruct(tlv.AnonymousTag())
			if d {
				decoy(enc, func(e *tlv.Encoder) { cmdPath(e, tagCmdDataPath, 0, 0x3E, 0x0A) })
			}
			cmdPath(enc, tagCmdDataPath, 1, 0x6, 0)
			enc.StartStruct(tlv.ContextTag(tagCmdDataFields))
			_ = enc.EndContainer()
			_ = enc.EndContainer()
			_ = enc.EndContainer()
			_ = enc.EndContainer()
		}, func(d *tlv.Decoder) (any, error) { return UnmarshalInvokeRequestTLV(d, nil) }},
		{"ReadRequest", func(enc *tlv.Encoder, d bool) {
			enc.StartStruct(tlv.AnonymousTag())
			enc.StartArray(tlv.ContextTag(tagReadReqAttributeRequests))
			attrPath(enc, tlv.AnonymousTag(), 1, 6, 0)
			_ = enc.EndContainer()
			if d {
				decoy(enc, func(e *tlv.Encoder) { e.PutBool(tlv.ContextTag(tagReadReqFabricFiltered), true) })
			}
			enc.PutBool(tlv.ContextTag(tagReadReqFabricFiltered), false)
			_ = enc.EndContainer()
		}, func(d *tlv.Decoder) (any, error) { return UnmarshalReadRequestTLV(d) }},
		{"SubscribeRequest", func(enc *tlv.Encoder, d bool) {
			enc.StartStruct(tlv.AnonymousTag())
			if d {
				decoy(enc, func(e *tlv.Encoder) { e.PutBool(tlv.ContextTag(tagSubReqKeepSubscriptions), true) })
			}
			enc.PutBool(tlv.ContextTag(tagSubReqKeepSubscriptions), false)
			enc.PutUint(tlv.ContextTag(tagSubReqMinIntervalFloor), 0)
			enc.PutUint(tlv.ContextTag(tagSubReqMaxIntervalCeiling), 60)
			enc.StartArray(tlv.ContextTag(tagSubReqAttributeRequests))
			attrPath(enc, tlv.AnonymousTag(), 1, 6, 0)
			_ = enc.EndContainer()
			enc.PutBool(tlv.ContextTag(tagSubReqFabricFiltered), false)
			_ = enc.EndContainer()
		}, func(d *tlv.Decoder) (any, error) { return UnmarshalSubscribeRequestTLV(d) }},
		{"WriteRequest / AttributeDataIB", func(enc *tlv.Encoder, d bool) {
			enc.StartStruct(tlv.AnonymousTag())
			enc.PutBool(tlv.ContextTag(tagWriteReqTimedRequest), false)
			enc.StartArray(tlv.ContextTag(tagWriteReqWriteRequests))
			enc.StartStruct(tlv.AnonymousTag())
			if d {
				decoy(enc, func(e *tlv.Encoder) { attrPath(e, tlv.ContextTag(tagAttributeDataPath), 0, 0x1F, 0) })
			}
			attrPath(enc, tlv.ContextTag(tagAttributeDataPath), 1, 6, 0x4003)
			enc.PutUint(tlv.ContextTag(tagAttributeDataValue), 1)
			_ = enc.EndContainer()
			_ = enc.EndContainer()
			_ = enc.EndContainer()
		}, func(d *tlv.Decoder) (any, error) { return UnmarshalWriteRequestTLV(d, nil) }},
		{"TimedRequest", func(enc *tlv.Encoder, d bool) {
			enc.StartStruct(tlv.AnonymousTag())
			if d {
				decoy(enc, func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(tagTimedReqTimeout), 9) })
			}
			enc.PutUint(tlv.ContextTag(tagTimedReqTimeout), 500)
			_ = enc.EndContainer()
		}, func(d *tlv.Decoder) (any, error) { return UnmarshalTimedRequestTLV(d) }},
		{"StatusResponse", func(enc *tlv.Encoder, d bool) {
			enc.StartStruct(tlv.AnonymousTag())
			if d {
				decoy(enc, func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(tagStatusResponseStatus), 1) })
			}
			enc.PutUint(tlv.ContextTag(tagStatusResponseStatus), 0)
			_ = enc.EndContainer()
		}, func(d *tlv.Decoder) (any, error) { return UnmarshalStatusResponseTLV(d) }},
		{"StatusIB", func(enc *tlv.Encoder, d bool) {
			enc.StartStruct(tlv.AnonymousTag())
			if d {
				decoy(enc, func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(tagStatusIBStatus), 1) })
			}
			enc.PutUint(tlv.ContextTag(tagStatusIBStatus), 0)
			_ = enc.EndContainer()
		}, func(d *tlv.Decoder) (any, error) { return UnmarshalStatusIBTLV(d) }},
	}
	for _, tc := range cases {
		wire := func(withDecoy bool) []byte {
			enc := tlv.NewEncoder()
			tc.encode(enc, withDecoy)
			b, err := enc.Bytes()
			if err != nil {
				t.Fatal(err)
			}
			return b
		}
		plain, err := tc.decode(tlv.NewDecoder(wire(false)))
		if err != nil {
			t.Fatalf("%s: the plain message is refused: %v", tc.name, err)
		}
		got, err := tc.decode(tlv.NewDecoder(wire(true)))
		if err != nil {
			t.Errorf("%s: an unknown member refused the message: %v", tc.name, err)
			continue
		}
		if !reflect.DeepEqual(got, plain) {
			t.Errorf("%s: an unknown member changed the decode:\n got  %+v\n want %+v", tc.name, got, plain)
		}
	}
}
