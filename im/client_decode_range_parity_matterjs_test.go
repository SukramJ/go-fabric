// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"testing"

	"github.com/SukramJ/go-fabric/tlv"
)

// reportWithAttributeData is a ReportData with one AttributeDataIB whose
// DataVersion and path fields the callbacks write.
func reportWithAttributeData(t *testing.T, version, path func(enc *tlv.Encoder)) []byte {
	t.Helper()
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.StartArray(tlv.ContextTag(tagReportAttributeReports))
	enc.StartStruct(tlv.AnonymousTag())
	enc.StartStruct(tlv.ContextTag(tagAttributeReportData))
	if version != nil {
		version(enc)
	}
	enc.StartList(tlv.ContextTag(tagAttributeDataPath))
	path(enc)
	_ = enc.EndContainer()
	enc.PutUint(tlv.ContextTag(2), 1) // Data
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	return encoded(t, enc)
}

// reportWithAttributeStatus is a ReportData with one AttributeStatusIB
// whose StatusIB fields the callback writes.
func reportWithAttributeStatus(t *testing.T, status func(enc *tlv.Encoder)) []byte {
	t.Helper()
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.StartArray(tlv.ContextTag(tagReportAttributeReports))
	enc.StartStruct(tlv.AnonymousTag())
	enc.StartStruct(tlv.ContextTag(tagAttributeReportStatus))
	enc.StartList(tlv.ContextTag(tagAttributeStatusPath))
	wholePath(enc)
	_ = enc.EndContainer()
	enc.StartStruct(tlv.ContextTag(tagAttributeStatusStatus))
	status(enc)
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	return encoded(t, enc)
}

// reportWithEventPriority is a ReportData with one EventDataIB of the given
// priority.
func reportWithEventPriority(t *testing.T, priority uint64) []byte {
	t.Helper()
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.StartArray(tlv.ContextTag(tagReportEventReports))
	enc.StartStruct(tlv.AnonymousTag())
	enc.StartStruct(tlv.ContextTag(tagEventReportData))
	enc.StartList(tlv.ContextTag(tagEventDataPath))
	enc.PutUint(tlv.ContextTag(1), 0)      // Endpoint
	enc.PutUint(tlv.ContextTag(2), 0x0028) // Cluster
	enc.PutUint(tlv.ContextTag(3), 0)      // Event
	_ = enc.EndContainer()
	enc.PutUint(tlv.ContextTag(tagEventDataNumber), 1)
	enc.PutUint(tlv.ContextTag(tagEventDataPriority), priority)
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	return encoded(t, enc)
}

// invokeResponseWithStatus is an InvokeResponse with one CommandStatusIB
// carrying the given CommandRef.
func invokeResponseWithStatus(t *testing.T, commandRef uint64) []byte {
	t.Helper()
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutBool(tlv.ContextTag(tagInvokeRespSuppressResponse), false)
	enc.StartArray(tlv.ContextTag(tagInvokeRespResponses))
	enc.StartStruct(tlv.AnonymousTag())
	enc.StartStruct(tlv.ContextTag(tagInvokeRespCommandStatus))
	enc.StartList(tlv.ContextTag(tagCmdStatusPath))
	enc.PutUint(tlv.ContextTag(0), 0)
	enc.PutUint(tlv.ContextTag(1), 6)
	enc.PutUint(tlv.ContextTag(2), 1)
	_ = enc.EndContainer()
	enc.StartStruct(tlv.ContextTag(tagCmdStatusStatus))
	enc.PutUint(tlv.ContextTag(tagStatusIBStatus), 0)
	_ = enc.EndContainer()
	enc.PutUint(tlv.ContextTag(tagCmdStatusRef), commandRef)
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	return encoded(t, enc)
}

func encoded(t *testing.T, enc *tlv.Encoder) []byte {
	t.Helper()
	b, err := enc.Bytes()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return b
}

func wholePath(enc *tlv.Encoder) {
	enc.PutUint(tlv.ContextTag(tagAttrPathEndpoint), 1)
	enc.PutUint(tlv.ContextTag(tagAttrPathCluster), 6)
	enc.PutUint(tlv.ContextTag(tagAttrPathAttribute), 0)
}

// TestClientDecodersRefuseOutOfRangeIntegers: the decoders a controller
// reads a device's answers with refuse an integer that does not fit its
// field instead of masking it into range — a masked endpoint 65536 is
// endpoint 0, a masked status 0x100 is Success. The path, DataVersion and
// CommandRef widths are matter.js's (packages/types/src/protocol/types/
// TlvAttributePath.ts:22-26, TlvAttributeReportData.ts:15,
// TlvCommandStatus.ts:18 at db021da0, whose TlvNumber refuses a value out
// of bounds, as [uintField] cites). A StatusIB's Status and ClusterStatus
// are refused above 8 bits as chip refuses them
// (src/app/MessageDef/StatusIB.cpp:55-60 with
// src/lib/core/TLVReader.cpp:204-214 at 6170af84,
// CHIP_ERROR_INVALID_INTEGER_VALUE); matter.js reads both as a 32-bit enum
// (packages/types/src/protocol/types/TlvStatus.ts:15-16,
// packages/types/src/tlv/TlvNumber.ts:303 at db021da0), which this module's
// 8-bit StatusCode cannot hold.
func TestClientDecodersRefuseOutOfRangeIntegers(t *testing.T) {
	t.Parallel()
	report := func(b []byte) error {
		_, err := UnmarshalReportDataTLV(tlv.NewDecoder(b), nil, nil)
		return err
	}
	pathWith := func(tag uint8, v uint64) func(enc *tlv.Encoder) {
		return func(enc *tlv.Encoder) {
			wholePath(enc)
			enc.PutUint(tlv.ContextTag(tag), v)
		}
	}
	cases := []struct {
		name   string
		decode func() error
	}{
		{"path endpoint 65536", func() error {
			return report(reportWithAttributeData(t, nil, func(enc *tlv.Encoder) {
				enc.PutUint(tlv.ContextTag(tagAttrPathEndpoint), 65536)
				enc.PutUint(tlv.ContextTag(tagAttrPathCluster), 6)
				enc.PutUint(tlv.ContextTag(tagAttrPathAttribute), 0)
			}))
		}},
		{"path cluster 2^32+6", func() error {
			return report(reportWithAttributeData(t, nil, func(enc *tlv.Encoder) {
				enc.PutUint(tlv.ContextTag(tagAttrPathEndpoint), 1)
				enc.PutUint(tlv.ContextTag(tagAttrPathCluster), 1<<32+6)
				enc.PutUint(tlv.ContextTag(tagAttrPathAttribute), 0)
			}))
		}},
		{"path attribute 2^32", func() error {
			return report(reportWithAttributeData(t, nil, func(enc *tlv.Encoder) {
				enc.PutUint(tlv.ContextTag(tagAttrPathEndpoint), 1)
				enc.PutUint(tlv.ContextTag(tagAttrPathCluster), 6)
				enc.PutUint(tlv.ContextTag(tagAttrPathAttribute), 1<<32)
			}))
		}},
		{"path list index 65536", func() error {
			return report(reportWithAttributeData(t, nil, pathWith(tagAttrPathListIndex, 65536)))
		}},
		{"path endpoint as a signed integer", func() error {
			return report(reportWithAttributeData(t, nil, func(enc *tlv.Encoder) {
				enc.PutInt(tlv.ContextTag(tagAttrPathEndpoint), -1)
				enc.PutUint(tlv.ContextTag(tagAttrPathCluster), 6)
				enc.PutUint(tlv.ContextTag(tagAttrPathAttribute), 0)
			}))
		}},
		{"DataVersion 2^32+1", func() error {
			return report(reportWithAttributeData(t, func(enc *tlv.Encoder) {
				enc.PutUint(tlv.ContextTag(tagAttributeDataDataVersion), 1<<32+1)
			}, wholePath))
		}},
		{"attribute status 0x100", func() error {
			return report(reportWithAttributeStatus(t, func(enc *tlv.Encoder) {
				enc.PutUint(tlv.ContextTag(tagStatusIBStatus), 0x100)
			}))
		}},
		{"attribute cluster status 0x100", func() error {
			return report(reportWithAttributeStatus(t, func(enc *tlv.Encoder) {
				enc.PutUint(tlv.ContextTag(tagStatusIBStatus), 1)
				enc.PutUint(tlv.ContextTag(tagStatusIBClusterStatus), 0x100)
			}))
		}},
		{"event priority 0x101", func() error { return report(reportWithEventPriority(t, 0x101)) }},
		{"CommandRef 65536", func() error {
			_, err := UnmarshalInvokeResponseTLV(tlv.NewDecoder(invokeResponseWithStatus(t, 65536)), nil)
			return err
		}},
		{"StatusIB status 0x100", func() error {
			enc := tlv.NewEncoder()
			enc.StartStruct(tlv.AnonymousTag())
			enc.PutUint(tlv.ContextTag(tagStatusIBStatus), 0x100)
			_ = enc.EndContainer()
			_, err := UnmarshalStatusIBTLV(tlv.NewDecoder(encoded(t, enc)))
			return err
		}},
		{"StatusIB cluster status 0x1FF", func() error {
			enc := tlv.NewEncoder()
			enc.StartStruct(tlv.AnonymousTag())
			enc.PutUint(tlv.ContextTag(tagStatusIBStatus), 1)
			enc.PutUint(tlv.ContextTag(tagStatusIBClusterStatus), 0x1FF)
			_ = enc.EndContainer()
			_, err := UnmarshalStatusIBTLV(tlv.NewDecoder(encoded(t, enc)))
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := tc.decode(); err == nil {
				t.Fatal("decoded without an error: the value was masked into range")
			}
		})
	}
}

// TestClientDecodersKeepTheWholeRange: every value that fits its field
// still decodes, the largest ones and the nullable list index included.
func TestClientDecodersKeepTheWholeRange(t *testing.T) {
	t.Parallel()
	b := reportWithAttributeData(t, func(enc *tlv.Encoder) {
		enc.PutUint(tlv.ContextTag(tagAttributeDataDataVersion), 0xFFFFFFFF)
	}, func(enc *tlv.Encoder) {
		enc.PutUint(tlv.ContextTag(tagAttrPathEndpoint), 0xFFFF)
		enc.PutUint(tlv.ContextTag(tagAttrPathCluster), 0xFFFFFFFF)
		enc.PutUint(tlv.ContextTag(tagAttrPathAttribute), 0xFFFFFFFF)
		enc.PutNull(tlv.ContextTag(tagAttrPathListIndex))
	})
	rd, err := UnmarshalReportDataTLV(tlv.NewDecoder(b), nil, nil)
	if err != nil || len(rd.Reports) != 1 {
		t.Fatalf("decode = %+v, %v", rd, err)
	}
	got := rd.Reports[0]
	if got.Path.Endpoint != 0xFFFF || got.Path.Cluster != 0xFFFFFFFF || got.Path.Attribute != 0xFFFFFFFF ||
		!got.Path.HasListIndex || !got.Path.ListAppend || got.DataVersion != 0xFFFFFFFF || !got.HasDataVersion {
		t.Fatalf("decoded %+v", got)
	}

	b = reportWithAttributeStatus(t, func(enc *tlv.Encoder) {
		enc.PutUint(tlv.ContextTag(tagStatusIBStatus), 0xFF)
		enc.PutUint(tlv.ContextTag(tagStatusIBClusterStatus), 0xFF)
	})
	rd, err = UnmarshalReportDataTLV(tlv.NewDecoder(b), nil, nil)
	if err != nil || len(rd.Reports) != 1 || rd.Reports[0].Status.Status != 0xFF || rd.Reports[0].Status.ClusterStatus != 0xFF {
		t.Fatalf("status decode = %+v, %v", rd, err)
	}

	rd, err = UnmarshalReportDataTLV(tlv.NewDecoder(reportWithEventPriority(t, 2)), nil, nil)
	if err != nil || len(rd.EventReports) != 1 || rd.EventReports[0].Priority != 2 {
		t.Fatalf("event decode = %+v, %v", rd, err)
	}

	ir, err := UnmarshalInvokeResponseTLV(tlv.NewDecoder(invokeResponseWithStatus(t, 0xFFFF)), nil)
	if err != nil || len(ir.Responses) != 1 || ir.Responses[0].CommandRef != 0xFFFF || !ir.Responses[0].HasCommandRef {
		t.Fatalf("invoke decode = %+v, %v", ir, err)
	}
}
