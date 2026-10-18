// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction

// A port of matter.js packages/protocol/test/protocol/interaction/AttributeDataDecoderTest.ts
// at 862ad75f: decoding attribute reports into values — chunked lists with
// indexed changes, chip-tool captures, duplicates, tag compression,
// unknown clusters.
//
// matter.js decodes through normalizeAndDecodeReadAttributeReport and its
// schema; the Go counterpart is the schema-blind assembler (InputChunk with
// decodeUnknownAttributeValue), so attribute names are not asserted and a
// value is compared as its TLV node, rendered with context tag numbers for
// field names (featureMap's empty bitmap {} is the integer 0 on the wire).

import (
	"encoding/hex"
	"log/slog"
	"strconv"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/tlv"
)

// adPath is one AttributePathIB as a test writes it.
type adPath struct {
	compressed          bool
	hasEP, hasCL, hasAT bool
	ep                  uint16
	cl, at              uint32
	index               int // -1: no ListIndex, -2: ListIndex null
}

func adFull(ep uint16, cl, at uint32) adPath {
	return adPath{hasEP: true, hasCL: true, hasAT: true, ep: ep, cl: cl, at: at, index: -1}
}

func (p adPath) idx(i int) adPath { p.index = i; return p }

// adEntry is one AttributeReportIB carrying AttributeData.
type adEntry struct {
	path    adPath
	version uint32
	data    func(enc *tlv.Encoder, tag tlv.Tag)
}

// adACL writes TlvAclTestSchema (AttributeDataDecoderTest.ts:33-38); -1 is null.
func adACL(privilege, authMode uint64, subjects, targets int) func(*tlv.Encoder, tlv.Tag) {
	return func(enc *tlv.Encoder, tag tlv.Tag) {
		enc.StartStruct(tag)
		enc.PutUint(tlv.ContextTag(1), privilege)
		enc.PutUint(tlv.ContextTag(2), authMode)
		for i, v := range []int{subjects, targets} {
			if v < 0 {
				enc.PutNull(tlv.ContextTag(uint8(3 + i)))
			} else {
				enc.PutUint(tlv.ContextTag(uint8(3+i)), uint64(v))
			}
		}
		_ = enc.EndContainer()
	}
}

func adUints(vs ...uint64) func(*tlv.Encoder, tlv.Tag) {
	return func(enc *tlv.Encoder, tag tlv.Tag) {
		enc.StartArray(tag)
		for _, v := range vs {
			enc.PutUint(tlv.AnonymousTag(), v)
		}
		_ = enc.EndContainer()
	}
}

func adUint(v uint64) func(*tlv.Encoder, tlv.Tag) {
	return func(enc *tlv.Encoder, tag tlv.Tag) { enc.PutUint(tag, v) }
}

func adBool(v bool) func(*tlv.Encoder, tlv.Tag) {
	return func(enc *tlv.Encoder, tag tlv.Tag) { enc.PutBool(tag, v) }
}

func adNull(enc *tlv.Encoder, tag tlv.Tag) { enc.PutNull(tag) }

// adReport encodes a ReportDataMessage with the entries.
func adReport(t *testing.T, entries ...adEntry) []byte {
	t.Helper()
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.StartArray(tlv.ContextTag(1))
	for _, e := range entries {
		enc.StartStruct(tlv.AnonymousTag())
		enc.StartStruct(tlv.ContextTag(1))
		enc.PutUint32(tlv.ContextTag(0), e.version)
		enc.StartList(tlv.ContextTag(1))
		p := e.path
		if p.compressed {
			enc.PutBool(tlv.ContextTag(0), true)
		}
		if p.hasEP {
			enc.PutUint(tlv.ContextTag(2), uint64(p.ep))
		}
		if p.hasCL {
			enc.PutUint(tlv.ContextTag(3), uint64(p.cl))
		}
		if p.hasAT {
			enc.PutUint(tlv.ContextTag(4), uint64(p.at))
		}
		switch {
		case p.index == -2:
			enc.PutNull(tlv.ContextTag(5))
		case p.index >= 0:
			enc.PutUint(tlv.ContextTag(5), uint64(p.index))
		}
		_ = enc.EndContainer()
		e.data(enc, tlv.ContextTag(2))
		_ = enc.EndContainer()
		_ = enc.EndContainer()
	}
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	b, err := enc.Bytes()
	if err != nil {
		t.Fatalf("encode report: %v", err)
	}
	return b
}

// adAssemble decodes each payload as the last chunk of a report and
// returns the report.
func adAssemble(t *testing.T, payloads ...[]byte) *Report {
	t.Helper()
	a := newAssembler(slog.New(slog.DiscardHandler))
	for _, p := range payloads {
		rd, err := decodeReport(p)
		if err != nil {
			t.Fatalf("decodeReport: %v", err)
		}
		// matter.js hands the decoder the attributeReports alone, not the
		// capture's MoreChunkedMessages (AttributeDataDecoderTest.ts:490).
		rd.MoreChunkedMessages = false
		a.add(rd)
	}
	return a.report
}

func adHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	return b
}

// adRender renders a node as JSON-like text, struct fields by tag number.
func adRender(n spec.Node) string {
	el := n.El
	join := func(open, closing string, field bool) string {
		parts := make([]string, 0, len(n.Children))
		for _, c := range n.Children {
			s := adRender(c)
			if field {
				s = strconv.FormatUint(uint64(c.El.Tag.Number), 10) + ":" + s
			}
			parts = append(parts, s)
		}
		return open + strings.Join(parts, ",") + closing
	}
	switch {
	case el.Type == tlv.TypeNull || el.IsNull:
		return "null"
	case el.Type == tlv.TypeStructure:
		return join("{", "}", true)
	case el.Type == tlv.TypeArray || el.Type == tlv.TypeList:
		return join("[", "]", false)
	case el.Type == tlv.TypeBoolTrue:
		return "true"
	case el.Type == tlv.TypeBoolFalse:
		return "false"
	case el.Type >= tlv.TypeUTF8Str1 && el.Type <= tlv.TypeUTF8Str8:
		return strconv.Quote(el.String)
	case el.Type >= tlv.TypeUnsignedInt1 && el.Type <= tlv.TypeUnsignedInt8:
		return strconv.FormatUint(el.Uint, 10)
	case el.Type <= tlv.TypeSignedInt8:
		return strconv.FormatInt(el.Int, 10)
	}
	return "?"
}

// adWant is one expected attribute of a report.
type adWant struct {
	ep    uint16
	cl    uint32
	at    uint32
	value string
}

func adExpect(t *testing.T, r *Report, want ...adWant) {
	t.Helper()
	if len(r.Attributes) != len(want) {
		got := make([]string, 0, len(r.Attributes))
		for _, a := range r.Attributes {
			got = append(got, adRender(a.Value))
		}
		t.Fatalf("attributes = %d %v, want %d", len(r.Attributes), got, len(want))
	}
	for i, w := range want {
		a := r.Attributes[i]
		if a.Path.Endpoint != w.ep || a.Path.Cluster != w.cl || a.Path.Attribute != w.at || a.Path.HasNode {
			t.Fatalf("[%d] path = %+v, want %d/%#x/%#x", i, a.Path, w.ep, w.cl, w.at)
		}
		if got := adRender(a.Value); got != w.value {
			t.Fatalf("[%d] %d/%#x/%#x value = %s, want %s", i, w.ep, w.cl, w.at, got, w.value)
		}
	}
}

const (
	adACL1 = "{1:1,2:2,3:null,4:null}"
	adACL2 = "{1:2,2:2,3:null,4:null}"
	adACL3 = "{1:3,2:2,3:null,4:null}"
	adACL4 = "{1:4,2:2,3:null,4:null}"
	// AttributeDataDecoderTest.ts:459,792: softwareVersion 1.
	adOneSW = "153601153501260055156878370124020024032824040918240201181818290424ff0118"
)

// TestAttributeDataDecoderChunkedArrays ports "decode chunked array using
// raw data from chip-tool" (AttributeDataDecoderTest.ts:43-408).
func TestAttributeDataDecoderChunkedArrays(t *testing.T) {
	t.Parallel()
	acl := adFull(0, 0x1f, 0)
	empty := adEntry{path: acl, data: adUints()}

	t.Run("decode chunked array with two elements", func(t *testing.T) {
		t.Parallel()
		r := adAssemble(t, adReport(t, empty,
			adEntry{path: acl.idx(-2), data: adACL(1, 2, -1, -1)},
			adEntry{path: acl.idx(-2), data: adACL(2, 2, -1, -1)}))
		adExpect(t, r, adWant{0, 0x1f, 0, "[" + adACL1 + "," + adACL2 + "]"})
	})

	t.Run("decode chunked array with indexed adding", func(t *testing.T) {
		t.Parallel()
		r := adAssemble(t, adReport(t, empty,
			adEntry{path: acl.idx(0), data: adACL(1, 2, -1, -1)},
			adEntry{path: acl.idx(1), data: adACL(2, 2, -1, -1)}))
		adExpect(t, r, adWant{0, 0x1f, 0, "[" + adACL1 + "," + adACL2 + "]"})
	})

	t.Run("decode chunked array with indexed deletion", func(t *testing.T) {
		t.Parallel()
		r := adAssemble(t, adReport(t, empty,
			adEntry{path: acl.idx(0), data: adACL(1, 2, -1, -1)},
			adEntry{path: acl.idx(1), data: adACL(2, 2, -1, -1)},
			adEntry{path: acl.idx(2), data: adACL(3, 2, -1, -1)},
			adEntry{path: acl.idx(1), data: adNull}))
		adExpect(t, r, adWant{0, 0x1f, 0, "[" + adACL1 + "," + adACL3 + "]"})
	})

	t.Run("decode chunked array with indexed modify", func(t *testing.T) {
		t.Parallel()
		r := adAssemble(t, adReport(t, empty,
			adEntry{path: acl.idx(0), data: adACL(1, 2, -1, -1)},
			adEntry{path: acl.idx(1), data: adACL(2, 2, -1, -1)},
			adEntry{path: acl.idx(2), data: adACL(3, 2, -1, -1)},
			adEntry{path: acl.idx(1), data: adACL(4, 2, -1, -1)}))
		adExpect(t, r, adWant{0, 0x1f, 0, "[" + adACL1 + "," + adACL4 + "," + adACL3 + "]"})
	})
}

// TestAttributeDataDecoderChipToolCaptures ports "decode raw data taken
// from chip.tool" (AttributeDataDecoderTest.ts:410-787); the hex vectors
// are the test's.
func TestAttributeDataDecoderChipToolCaptures(t *testing.T) {
	t.Parallel()

	t.Run("decode chunked array", func(t *testing.T) {
		t.Parallel()
		// AttributeDataDecoderTest.ts:414: Descriptor ServerList, one REPLACE
		// with [4] and twenty ListIndex=null ADDs.
		hexData := "1536011535012600605bd045370124020024031d2404011836021818181535012600605bd045370124020024031d24040134051824020418181535012600605bd045370124020024031d24040134051824021d18181535012600605bd045370124020024031d24040134051824021f18181535012600605bd045370124020024031d24040134051824022818181535012600605bd045370124020024031d24040134051824022a18181535012600605bd045370124020024031d24040134051824022b18181535012600605bd045370124020024031d24040134051824022c18181535012600605bd045370124020024031d24040134051824023018181535012600605bd045370124020024031d24040134051824023118181535012600605bd045370124020024031d24040134051824023218181535012600605bd045370124020024031d24040134051824023318181535012600605bd045370124020024031d24040134051824023418181535012600605bd045370124020024031d24040134051824023518181535012600605bd045370124020024031d24040134051824023618181535012600605bd045370124020024031d24040134051824023718181535012600605bd045370124020024031d24040134051824023b18181535012600605bd045370124020024031d24040134051824023c18181535012600605bd045370124020024031d24040134051824023e18181535012600605bd045370124020024031d24040134051824023f18181535012600605bd045370124020024031d24040134051824024018181535012600605bd045370124020024031d240401340518240241181818290424ff0118"
		r := adAssemble(t, adHex(t, hexData))
		adExpect(t, r, adWant{0, 29, 1, "[4,29,31,40,42,43,44,48,49,50,51,52,53,54,55,59,60,62,63,64,65]"})
	})

	t.Run("decode number attribute", func(t *testing.T) {
		t.Parallel()
		adExpect(t, adAssemble(t, adHex(t, adOneSW)), adWant{0, 40, 9, "1"})
	})

	t.Run("decode whole cluster response", func(t *testing.T) {
		t.Parallel()
		// AttributeDataDecoderTest.ts:482: Basic Information, every attribute.
		hexData := "15360115350126005515687837012402002403282404001824020118181535012600551568783701240200240328240401182c020b544553545f56454e444f5218181535012600551568783701240200240328240402182502f1ff18181535012600551568783701240200240328240403182c020c544553545f50524f4455435418181535012600551568783701240200240328240404182502018018181535012600551568783701240200240328240405182c020018181535012600551568783701240200240328240406182c02025553181815350126005515687837012402002403282404071824020018181535012600551568783701240200240328240408182c020c544553545f56455253494f4e18181535012600551568783701240200240328240409182402011818153501260055156878370124020024032824040a182c0203312e301818153501260055156878370124020024032824040b182c020832303230303130311818153501260055156878370124020024032824040c182c02001818153501260055156878370124020024032824040d182c02001818153501260055156878370124020024032824040e182c02001818153501260055156878370124020024032824040f182c0207544553545f534e181815350126005515687837012402002403282404101828021818153501260055156878370124020024032824041118290218181535012600551568783701240200240328240412182c021032354536333242424137354642453943181815350126005515687837012402002403282404131835022400032501ffff18181815350126005515687837012402002403282504fcff18240200181815350126005515687837012402002403282504fdff18240201181815350126005515687837012402002403282504f8ff18360218181815350126005515687837012402002403282504f9ff18360218181815350126005515687837012402002403282504faff18360218181815350126005515687837012402002403282504faff340518240200181815350126005515687837012402002403282504faff340518240201181815350126005515687837012402002403282504faff340518240202181815350126005515687837012402002403282504fbff18360218181815350126005515687837012402002403282504fbff340518240200181815350126005515687837012402002403282504fbff340518240201181815350126005515687837012402002403282504fbff340518240202181815350126005515687837012402002403282504fbff340518240203181815350126005515687837012402002403282504fbff340518240204181818290324ff0118"
		r := adAssemble(t, adHex(t, hexData))
		want := []adWant{
			{0, 40, 0, "1"}, {0, 40, 1, `"TEST_VENDOR"`}, {0, 40, 2, "65521"},
			{0, 40, 3, `"TEST_PRODUCT"`}, {0, 40, 4, "32769"}, {0, 40, 5, `""`},
			{0, 40, 6, `"US"`}, {0, 40, 7, "0"}, {0, 40, 8, `"TEST_VERSION"`},
			{0, 40, 9, "1"}, {0, 40, 10, `"1.0"`}, {0, 40, 11, `"20200101"`},
			{0, 40, 12, `""`}, {0, 40, 13, `""`}, {0, 40, 14, `""`},
			{0, 40, 15, `"TEST_SN"`}, {0, 40, 16, "false"}, {0, 40, 17, "true"},
			{0, 40, 18, `"25E632BBA75FBE9C"`},
			{0, 40, 19, "{0:3,1:65535}"}, // capabilityMinima
			{0, 40, 65532, "0"},          // featureMap {}
			{0, 40, 65533, "1"}, {0, 40, 65528, "[]"}, {0, 40, 65529, "[]"},
			{0, 40, 65530, "[0,1,2]"}, {0, 40, 65531, "[0,1,2,3,4]"},
		}
		adExpect(t, r, want...)
		for _, a := range r.Attributes {
			if a.DataVersion != 2020087125 {
				t.Fatalf("%d/%d/%d version = %d, want 2020087125", a.Path.Endpoint, a.Path.Cluster, a.Path.Attribute, a.DataVersion)
			}
		}
		if len(r.Statuses) != 0 {
			t.Fatalf("statuses = %v, want none", r.Statuses)
		}
	})
}

// TestAttributeDataDecoderUnknownValues ports
// "decodeUnknownAttributeValueWithSchema" (AttributeDataDecoderTest.ts:789-914).
func TestAttributeDataDecoderUnknownValues(t *testing.T) {
	t.Parallel()
	acl := adFull(0, 0x1f, 0)

	t.Run("decode number attribute as unknown", func(t *testing.T) {
		t.Parallel()
		adExpect(t, adAssemble(t, adHex(t, adOneSW)), adWant{0, 40, 9, "1"})
	})

	t.Run("decode object attribute as unknown", func(t *testing.T) {
		t.Parallel()
		r := adAssemble(t, adReport(t, adEntry{path: acl, data: adACL(1, 2, -1, -1)}))
		adExpect(t, r, adWant{0, 0x1f, 0, adACL1})
	})

	t.Run("decode object array attribute as unknown", func(t *testing.T) {
		t.Parallel()
		list := func(enc *tlv.Encoder, tag tlv.Tag) {
			enc.StartArray(tag)
			adACL(1, 2, -1, -1)(enc, tlv.AnonymousTag())
			adACL(3, 4, 5, 6)(enc, tlv.AnonymousTag())
			_ = enc.EndContainer()
		}
		r := adAssemble(t, adReport(t, adEntry{path: acl, data: list}))
		adExpect(t, r, adWant{0, 0x1f, 0, "[" + adACL1 + ",{1:3,2:4,3:5,4:6}]"})
	})

	t.Run("decode object chunked array attribute as unknown", func(t *testing.T) {
		t.Parallel()
		r := adAssemble(t, adReport(t,
			adEntry{path: acl, data: adUints()},
			adEntry{path: acl.idx(0), data: adACL(1, 2, -1, -1)},
			adEntry{path: acl.idx(1), data: adACL(2, 2, -1, 6)}))
		adExpect(t, r, adWant{0, 0x1f, 0, "[" + adACL1 + ",{1:2,2:2,3:null,4:6}]"})
	})
}

// TestAttributeDataDecoderDuplicates ports "decode duplicate non list
// values" (AttributeDataDecoderTest.ts:916-1010); the second case stays
// in the register's missing list.
func TestAttributeDataDecoderDuplicates(t *testing.T) {
	t.Parallel()
	t.Run("decode chunked array with two elements", func(t *testing.T) {
		t.Parallel()
		onOff := adFull(0, 6, 0)
		r := adAssemble(t, adReport(t,
			adEntry{path: onOff, version: 12345, data: adBool(true)},
			adEntry{path: onOff, version: 23456, data: adBool(false)}))
		adExpect(t, r, adWant{0, 6, 0, "false"})
	})
}

// TestAttributeDataDecoderNormalize ports "normalizeAttributeData"
// (AttributeDataDecoderTest.ts:1012-1171): the grouping by path and the
// tag compression resolution, observed as the attributes they assemble.
func TestAttributeDataDecoderNormalize(t *testing.T) {
	t.Parallel()
	acl := adFull(0, 0x1f, 0)
	twoACLs := "[" + adACL1 + "," + adACL2 + "]"

	t.Run("normalize data with all paths given for one endpoint", func(t *testing.T) {
		t.Parallel()
		r := adAssemble(t, adReport(t,
			adEntry{path: acl, data: adUints()},
			adEntry{path: acl.idx(0), data: adACL(1, 2, -1, -1)},
			adEntry{path: acl.idx(1), data: adACL(2, 2, -1, -1)}))
		adExpect(t, r, adWant{0, 0x1f, 0, twoACLs})
	})

	t.Run("normalize data with all paths given for two endpoints", func(t *testing.T) {
		t.Parallel()
		r := adAssemble(t, adReport(t,
			adEntry{path: acl, data: adUints()},
			adEntry{path: acl.idx(0), data: adACL(1, 2, -1, -1)},
			adEntry{path: acl.idx(1), data: adACL(2, 2, -1, -1)},
			adEntry{path: adFull(0, 0x1f, 1), data: adUints()}))
		adExpect(t, r, adWant{0, 0x1f, 0, twoACLs}, adWant{0, 0x1f, 1, "[]"})
	})

	t.Run("normalize data with all paths given for two endpoints with enabledtagCompression", func(t *testing.T) {
		t.Parallel()
		// AttributeDataDecoderTest.ts:1116,1126,1138: compressed paths
		// carrying only a list index, or only the attribute id.
		r := adAssemble(t, adReport(t,
			adEntry{path: acl, data: adUints()},
			adEntry{path: adPath{compressed: true, index: 0}, data: adACL(1, 2, -1, -1)},
			adEntry{path: adPath{compressed: true, index: 1}, data: adACL(2, 2, -1, -1)},
			adEntry{path: adPath{compressed: true, hasAT: true, at: 1, index: -1}, data: adUints()}))
		adExpect(t, r, adWant{0, 0x1f, 0, twoACLs}, adWant{0, 0x1f, 1, "[]"})
	})
}

// TestAttributeDataDecoderUnknownCluster ports "unknown cluster with
// global attributes" (AttributeDataDecoderTest.ts:1173-1233). The Go
// assembler decodes every cluster schema-blind, so the global attributes
// of an unknown cluster come out as their TLV values.
func TestAttributeDataDecoderUnknownCluster(t *testing.T) {
	t.Parallel()
	t.Run("decodes global attributes with proper types for unknown clusters", func(t *testing.T) {
		t.Parallel()
		const cl = 0x0001_fc00
		r := adAssemble(t, adReport(t,
			adEntry{path: adFull(1, cl, 0xfffd), version: 1, data: adUint(3)},
			adEntry{path: adFull(1, cl, 0xfffc), version: 1, data: adUint(0)},
			adEntry{path: adFull(1, cl, 0xfffb), version: 1, data: adUints(0, 1, 2)},
			adEntry{path: adFull(1, cl, 0xfff9), version: 1, data: adUints()},
			adEntry{path: adFull(1, cl, 0xfff8), version: 1, data: adUints(1)},
			adEntry{path: adFull(1, cl, 0x0), version: 1, data: adUint(42)}))
		adExpect(t, r,
			adWant{1, cl, 0xfffd, "3"}, adWant{1, cl, 0xfffc, "0"}, adWant{1, cl, 0xfffb, "[0,1,2]"},
			adWant{1, cl, 0xfff9, "[]"}, adWant{1, cl, 0xfff8, "[1]"}, adWant{1, cl, 0x0, "42"})
	})
}
