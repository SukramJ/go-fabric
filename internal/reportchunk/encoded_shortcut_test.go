// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package reportchunk_test

import (
	"bytes"
	"testing"
	"unsafe"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/internal/reportchunk"
	"thingfabric.dev/go-fabric/internal/valueenc"
	"thingfabric.dev/go-fabric/tlv"
)

// sinkValue keeps a measured result alive.
var sinkValue im.AttributeValue

// scopedEntries is a fabric-scoped list: entry i holds data i in field 1
// and the fabric index fabrics[i] in field 254.
type scopedEntries []uint8

// scopedWrite writes [scopedEntries]; anything else as write does.
func scopedWrite(enc *tlv.Encoder, tag tlv.Tag, v im.AttributeValue) {
	l, ok := v.Value.(scopedEntries)
	if !ok || v.IsNull {
		write(enc, tag, v)
		return
	}
	enc.StartArray(tag)
	for i, f := range l {
		enc.StartStruct(tlv.AnonymousTag())
		enc.PutUint(tlv.ContextTag(1), uint64(i))
		enc.PutUint(tlv.ContextTag(254), uint64(f))
		_ = enc.EndContainer()
	}
	_ = enc.EndContainer()
}

// resolvedValue is v as the resolver carries it ([reportchunk.Encoded]).
func resolvedValue(t testing.TB, w im.AttributeValueWriter, v im.AttributeValue) im.AttributeValue {
	t.Helper()
	el, err := valueenc.Encode(func(enc *tlv.Encoder, tag tlv.Tag) { w(enc, tag, v) })
	if err != nil {
		t.Fatal(err)
	}
	return im.AttributeValue{Value: reportchunk.Encoded(el)}
}

// TestApplyFabricScopeEncoded (#635): a resolved value is scoped from its
// bytes as they stand, not encoded again — the result is the one the
// value's writer gives, and it costs fewer allocations than encoding the
// value afresh does.
func TestApplyFabricScopeEncoded(t *testing.T) {
	raw := attr(7, scopedEntries{1, 2, 1, 3, 1})
	raw.FabricScope = &im.FabricScope{Fabric: 1, Filtered: true}
	enc := raw
	enc.Value = resolvedValue(t, scopedWrite, raw.Value)

	want := reportchunk.ApplyFabricScope(raw, scopedWrite)
	got := reportchunk.ApplyFabricScope(enc, scopedWrite)
	wantList, ok1 := want.Value.Value.(reportchunk.List)
	gotList, ok2 := got.Value.Value.(reportchunk.List)
	if !ok1 || !ok2 || len(wantList) != 3 || len(gotList) != len(wantList) || got.FabricScope != nil {
		t.Fatalf("scoped: got %+v, want %+v (three entries of fabric 1)", got, want)
	}
	for i := range wantList {
		if !bytes.Equal(gotList[i], wantList[i]) {
			t.Fatalf("member %d = % x, want % x", i, gotList[i], wantList[i])
		}
	}

	// What scoping the bytes as they stand costs: the mask and the split.
	el := []byte(enc.Value.Value.(reportchunk.Encoded))
	direct := testing.AllocsPerRun(50, func() {
		m, _ := im.MaskFabricScoped(el, *raw.FabricScope)
		members, _ := tlv.SplitArrayMembers(m)
		sinkValue = im.AttributeValue{Value: reportchunk.List(members)}
	})
	resolved := testing.AllocsPerRun(50, func() { _ = reportchunk.ApplyFabricScope(enc, scopedWrite) })
	if resolved > direct {
		t.Fatalf("a resolved value costs %v allocations, scoping its bytes %v: it was encoded again", resolved, direct)
	}
}

// TestSplitListEncoded (#635): a resolved list splits into the reports
// the value's writer gives, byte for byte, and its members are sub-slices
// of the resolved bytes — not of a copy made by encoding them again.
func TestSplitListEncoded(t *testing.T) {
	t.Parallel()
	long := make([]uint32, 300)
	for i := range long {
		long[i] = uint32(i * 7)
	}
	raw := attr(8, long)
	enc := raw
	enc.Value = resolvedValue(t, write, raw.Value)
	rd := im.ReportData{HasSubscription: true, SubscriptionID: 1}

	want, ok1 := reportchunk.SplitList(rd, raw, 400, write)
	got, ok2 := reportchunk.SplitList(rd, enc, 400, write)
	if !ok1 || !ok2 || len(got) != len(want) || len(got) < 2 {
		t.Fatalf("split: %d reports (ok %v), want %d (ok %v), several", len(got), ok2, len(want), ok1)
	}
	gotBytes := encodedSizeBytes(t, im.ReportData{Reports: got})
	wantBytes := encodedSizeBytes(t, im.ReportData{Reports: want})
	if !bytes.Equal(gotBytes, wantBytes) {
		t.Fatalf("split reports differ:\n% x\nwant\n% x", gotBytes, wantBytes)
	}

	resolved := enc.Value.Value.(reportchunk.Encoded)
	lo, hi := uintptr(unsafe.Pointer(unsafe.SliceData(resolved))), uintptr(unsafe.Pointer(unsafe.SliceData(resolved)))+uintptr(len(resolved))
	head, ok := got[0].Value.Value.(reportchunk.List)
	if !ok || len(head) == 0 {
		t.Fatalf("head report %+v, want a list", got[0])
	}
	if p := uintptr(unsafe.Pointer(unsafe.SliceData(head[0]))); p < lo || p >= hi {
		t.Fatal("the head's first member is not a sub-slice of the resolved bytes: the value was encoded again")
	}
}
