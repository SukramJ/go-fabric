// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"encoding/hex"
	"testing"

	"github.com/SukramJ/go-fabric/tlv"
)

// TestReportDataOmitsEmptyReportArrays: a ReportData carries the
// attributeReports and the eventReports array only when it has an entry
// for it — a keep-alive, an event-only report and an empty read response
// carry no attributeReports field at all. Mirrors matter.js
// packages/protocol/src/interaction/InteractionMessenger.ts:490-491 at
// db021da0 (`attributeReports: undefined, eventReports: undefined`), :764-767
// and :782-785 (each array is created with its first entry) and :512-513
// (both deleted after a chunk went out); both fields are optional
// (packages/types/src/protocol/messages/TlvDataReportForSend.ts:20, :23).
// chip's ReadClient reads either array as absent or present
// (src/app/ReadClient.cpp:688-712 at 6170af84, CHIP_END_OF_TLV is no error).
// The expected bytes of the first two rows are matter.js's own
// (im/testdata/im-wire-fixtures.json: report_data_keepalive_no_subid,
// report_data_supress_false_empty_attrs).
func TestReportDataOmitsEmptyReportArrays(t *testing.T) {
	t.Parallel()
	nilWriter := func(*tlv.Encoder, tlv.Tag, AttributeValue) {}
	cases := []struct {
		name string
		rd   ReportData
		want string
	}{
		{"keep-alive", ReportData{SuppressResponse: true}, "15290424ff0c18"},
		{"empty read response", ReportData{}, "15280424ff0c18"},
		{"empty chunk", ReportData{MoreChunkedMessages: true}, "152903280424ff0c18"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			enc := tlv.NewEncoder()
			tc.rd.MarshalTLV(enc, nilWriter)
			got, err := enc.Bytes()
			if err != nil {
				t.Fatalf("MarshalTLV: %v", err)
			}
			if hex.EncodeToString(got) != tc.want {
				t.Fatalf("ReportData = %x, want %s (no attributeReports, no eventReports)", got, tc.want)
			}
		})
	}

	t.Run("an event-only report carries the events alone", func(t *testing.T) {
		t.Parallel()
		enc := tlv.NewEncoder()
		ReportData{EventReports: []EventReport{{IsStatus: true, Status: StatusIB{Status: StatusSuccess}}}}.MarshalTLV(enc, nilWriter)
		got, err := enc.Bytes()
		if err != nil {
			t.Fatalf("MarshalTLV: %v", err)
		}
		dec := tlv.NewDecoder(got)
		if _, err := dec.Next(); err != nil { // the message struct
			t.Fatal(err)
		}
		el, err := dec.Next()
		if err != nil {
			t.Fatal(err)
		}
		if el.Tag.Kind != tlv.TagKindContext || el.Tag.Number != uint32(tagReportEventReports) {
			t.Fatalf("first field = tag %+v, want the eventReports array (%d): %x", el.Tag, tagReportEventReports, got)
		}
	})

	t.Run("a report with an attribute keeps its array", func(t *testing.T) {
		t.Parallel()
		enc := tlv.NewEncoder()
		ReportData{Reports: []AttributeReport{{IsStatus: true, Status: StatusIB{Status: StatusUnsupportedAttribute}}}}.MarshalTLV(enc, nilWriter)
		got, err := enc.Bytes()
		if err != nil {
			t.Fatalf("MarshalTLV: %v", err)
		}
		out, err := UnmarshalReportDataTLV(tlv.NewDecoder(got), nil, nil)
		if err != nil || len(out.Reports) != 1 {
			t.Fatalf("decode = %+v, %v, want one attribute report", out, err)
		}
	})
}

// TestReportDataDecoderAcceptsAbsentAndEmptyArrays: the client decoder
// reads a ReportData without the attributeReports field and one with an
// empty array alike — a peer may send either (matter.js omits, the bytes
// with the empty array are matter.js's TlvDataReportForSend encoding of
// `attributeReports: []`).
func TestReportDataDecoderAcceptsAbsentAndEmptyArrays(t *testing.T) {
	t.Parallel()
	for name, wire := range map[string]string{
		"absent":                 "15290424ff0c18",
		"empty attributeReports": "15360118290424ff0c18",
		"empty eventReports":     "15360218290424ff0c18",
	} {
		b, err := hex.DecodeString(wire)
		if err != nil {
			t.Fatal(err)
		}
		out, err := UnmarshalReportDataTLV(tlv.NewDecoder(b), nil, nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(out.Reports) != 0 || len(out.EventReports) != 0 || !out.SuppressResponse {
			t.Fatalf("%s: decoded %+v, want an empty keep-alive", name, out)
		}
	}
}
