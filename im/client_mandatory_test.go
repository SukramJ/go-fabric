// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"testing"

	"github.com/SukramJ/go-fabric/tlv"
)

// TestClientDecodersRefuseMissingMandatoryMembers (review II,
// R2-P1-req2-09): the decoders of what a device answers refuse an
// information block without its mandatory members and a scalar field of
// the wrong type, instead of reading the zero value — a StatusIB without
// its Status is not Success. matter.js: TlvStatus.ts:15 (status is a
// TlvField), TlvCommandStatus.ts:16-17 and TlvAttributeStatus.ts:16-17
// (both members TlvField), TlvObject.ts:131 / :261
// (ValidationMandatoryFieldMissingError), at db021da0.
func TestClientDecodersRefuseMissingMandatoryMembers(t *testing.T) {
	t.Parallel()
	ctx := tlv.ContextTag
	anon := tlv.AnonymousTag()
	cmdPath := ConcreteCommandPath{Endpoint: 1, Cluster: 6, Command: 1}
	attrPath := ConcreteAttributePath{Endpoint: 1, Cluster: 6, Attribute: 0, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	encode := func(t *testing.T, build func(e *tlv.Encoder)) *tlv.Decoder {
		t.Helper()
		e := tlv.NewEncoder()
		build(e)
		b, err := e.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		return tlv.NewDecoder(b)
	}
	invoke := func(ib func(e *tlv.Encoder)) func(e *tlv.Encoder) {
		return func(e *tlv.Encoder) {
			e.StartStruct(anon)
			e.PutBool(ctx(tagInvokeRespSuppressResponse), false)
			e.StartArray(ctx(tagInvokeRespResponses))
			e.StartStruct(anon) // InvokeResponseIB
			ib(e)
			_ = e.EndContainer()
			_ = e.EndContainer()
			_ = e.EndContainer()
		}
	}
	report := func(fields func(e *tlv.Encoder)) func(e *tlv.Encoder) {
		return func(e *tlv.Encoder) {
			e.StartStruct(anon)
			fields(e)
			_ = e.EndContainer()
		}
	}
	attrReports := func(ib func(e *tlv.Encoder)) func(e *tlv.Encoder) {
		return report(func(e *tlv.Encoder) {
			e.StartArray(ctx(tagReportAttributeReports))
			e.StartStruct(anon) // AttributeReportIB
			ib(e)
			_ = e.EndContainer()
			_ = e.EndContainer()
		})
	}
	write := func(st func(e *tlv.Encoder)) func(e *tlv.Encoder) {
		return func(e *tlv.Encoder) {
			e.StartStruct(anon)
			e.StartArray(ctx(tagWriteRespResponses))
			e.StartStruct(anon) // AttributeStatusIB
			st(e)
			_ = e.EndContainer()
			_ = e.EndContainer()
			_ = e.EndContainer()
		}
	}

	for _, tc := range []struct {
		name   string
		build  func(e *tlv.Encoder)
		decode func(dec *tlv.Decoder) error
	}{
		{"invoke: a CommandStatusIB whose StatusIB has no Status", invoke(func(e *tlv.Encoder) {
			e.StartStruct(ctx(tagInvokeRespCommandStatus))
			cmdPath.MarshalTLV(e, ctx(tagCmdStatusPath))
			e.StartStruct(ctx(tagCmdStatusStatus))
			_ = e.EndContainer()
			_ = e.EndContainer()
		}), decodeInvoke},
		{"invoke: a CommandStatusIB without its path", invoke(func(e *tlv.Encoder) {
			e.StartStruct(ctx(tagInvokeRespCommandStatus))
			StatusIB{Status: StatusSuccess}.MarshalTLV(e, ctx(tagCmdStatusStatus))
			_ = e.EndContainer()
		}), decodeInvoke},
		{"invoke: an empty InvokeResponseIB", invoke(func(*tlv.Encoder) {}), decodeInvoke},
		{"write: an AttributeStatusIB whose StatusIB has no Status", write(func(e *tlv.Encoder) {
			attrPath.MarshalTLV(e, ctx(tagAttributeStatusPath))
			e.StartStruct(ctx(tagAttributeStatusStatus))
			_ = e.EndContainer()
		}), decodeWrite},
		{"write: an AttributeStatusIB without its path", write(func(e *tlv.Encoder) {
			StatusIB{Status: StatusSuccess}.MarshalTLV(e, ctx(tagAttributeStatusStatus))
		}), decodeWrite},
		{"report: an empty AttributeReportIB", attrReports(func(*tlv.Encoder) {}), decodeReport},
		{"report: a signed SubscriptionId", report(func(e *tlv.Encoder) {
			e.PutInt(ctx(tagReportSubscriptionID), 5)
		}), decodeReport},
		{"report: a structure at MoreChunkedMessages", report(func(e *tlv.Encoder) {
			e.StartStruct(ctx(tagReportMoreChunkedMessages))
			e.PutUint(ctx(tagReportSubscriptionID), 7)
			_ = e.EndContainer()
		}), decodeReport},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := tc.decode(encode(t, tc.build)); err == nil {
				t.Fatal("decoded without an error")
			}
		})
	}
}

func decodeInvoke(dec *tlv.Decoder) error {
	_, err := UnmarshalInvokeResponseTLV(dec, nil)
	return err
}

func decodeWrite(dec *tlv.Decoder) error {
	_, err := UnmarshalWriteResponseTLV(dec)
	return err
}

func decodeReport(dec *tlv.Decoder) error {
	_, err := UnmarshalReportDataTLV(dec, nil, nil)
	return err
}
