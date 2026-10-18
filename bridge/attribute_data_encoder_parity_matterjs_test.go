// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

// A port of matter.js packages/protocol/test/protocol/interaction/AttributeDataEncoderTest.ts
// at 862ad75f: chunking a list attribute report (the "chunk arrays for
// DataReports" group); the §8.9 field guarantees are in im.

import (
	"testing"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// TestAttributeDataEncoderChunkArray ports "chunk array"
// (AttributeDataEncoderTest.ts:223-283): Descriptor ServerList [29, 40] at
// DataVersion 12345678. matter.js chunkAttributePayload yields an empty
// REPLACE-ALL plus one ListIndex=null ADD per member, and its messenger
// then moves the first ADD into the REPLACE-ALL
// (InteractionMessenger.ts:657-664). splitListAttributeReport is both
// steps in one, so the expectation is REPLACE-ALL [29] then ADD 40 — every
// chunk at the path without a list index or with ListIndex=null, every
// chunk at the list's DataVersion.
func TestAttributeDataEncoderChunkArray(t *testing.T) {
	t.Parallel()
	t.Run("chunk array", func(t *testing.T) {
		t.Parallel()
		rep := im.AttributeReport{
			Path:        im.ConcreteAttributePath{Endpoint: 0, HasEndpoint: true, Cluster: 0x1d, HasCluster: true, Attribute: 1, HasAttribute: true},
			Value:       im.AttributeValue{Value: []uint32{29, 40}},
			DataVersion: 12345678,
		}
		rd := im.ReportData{Reports: []im.AttributeReport{rep}}
		whole, err := EncodeReportData(rd)
		if err != nil {
			t.Fatalf("EncodeReportData: %v", err)
		}
		// One byte short of the whole list: the head holds one member.
		chunks, ok := splitListAttributeReport(rd, rep, len(whole)-1)
		if !ok || len(chunks) != 2 {
			t.Fatalf("split = %d chunks, ok=%v; want 2", len(chunks), ok)
		}
		member := func(t *testing.T, raw []byte) uint64 {
			t.Helper()
			el, err := tlv.NewDecoder(raw).Next()
			if err != nil {
				t.Fatalf("member: %v", err)
			}
			return el.Uint
		}
		head, add := chunks[0], chunks[1]
		if head.Path.HasListIndex || head.Path.ListAppend || head.DataVersion != 12345678 {
			t.Fatalf("REPLACE-ALL path=%+v dataVersion=%d", head.Path, head.DataVersion)
		}
		list, isList := head.Value.Value.(rawListValue)
		if !isList || len(list) != 1 || member(t, list[0]) != 29 {
			t.Fatalf("REPLACE-ALL value = %#v, want [29]", head.Value.Value)
		}
		if !add.Path.ListAppend || add.DataVersion != 12345678 || add.Path.Cluster != 0x1d || add.Path.Attribute != 1 {
			t.Fatalf("ADD path=%+v dataVersion=%d", add.Path, add.DataVersion)
		}
		item, isItem := add.Value.Value.(rawListMember)
		if !isItem || member(t, item) != 40 {
			t.Fatalf("ADD value = %#v, want 40", add.Value.Value)
		}
	})
}
