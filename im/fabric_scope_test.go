// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/SukramJ/go-fabric/parity"
	"github.com/SukramJ/go-fabric/tlv"
)

// scopeEntry is a fabric-scoped list entry: fields 1 (a nested list) and 2,
// and FabricIndex when index is set.
type scopeEntry struct{ index *uint64 }

func indexOf(i uint64) *uint64 { return &i }

func encodeEntries(t *testing.T, listType tlv.ElementType, entries ...scopeEntry) []byte {
	t.Helper()
	enc := tlv.NewEncoder()
	if listType == tlv.TypeList {
		enc.StartList(tlv.AnonymousTag())
	} else {
		enc.StartArray(tlv.AnonymousTag())
	}
	for _, e := range entries {
		enc.StartStruct(tlv.AnonymousTag())
		enc.StartArray(tlv.ContextTag(1))
		enc.PutUint(tlv.AnonymousTag(), 7)
		_ = enc.EndContainer()
		enc.PutUint(tlv.ContextTag(2), 9)
		if e.index != nil {
			enc.PutUint(tlv.ContextTag(0xFE), *e.index)
		}
		_ = enc.EndContainer()
	}
	_ = enc.EndContainer()
	b, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// entryWithout is an entry of fabric without the fields of withheld (1, 2).
func entryWithout(fabric uint64, withheld ...uint32) []byte {
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	if !containsTag(withheld, 1) {
		enc.StartArray(tlv.ContextTag(1))
		enc.PutUint(tlv.AnonymousTag(), 7)
		_ = enc.EndContainer()
	}
	if !containsTag(withheld, 2) {
		enc.PutUint(tlv.ContextTag(2), 9)
	}
	enc.PutUint(tlv.ContextTag(0xFE), fabric)
	_ = enc.EndContainer()
	b, _ := enc.Bytes()
	return b
}

func containsTag(tags []uint32, tag uint32) bool {
	for _, t := range tags {
		if t == tag {
			return true
		}
	}
	return false
}

func listOf(listType tlv.ElementType, entries ...[]byte) []byte {
	out := []byte{byte(listType)}
	for _, e := range entries {
		out = append(out, e...)
	}
	return append(out, byte(tlv.TypeEndContainer))
}

// TestMaskFabricScopedFiltersAndMasksByTheEntrysOwner: the mask keeps the
// reader's own entry whole; drops another fabric's on a filtered request
// or for a fabric-sensitive attribute; otherwise keeps it without the
// listed fields, a nested field included. A session without a fabric owns
// no entry. The list's container type is kept.
func TestMaskFabricScopedFiltersAndMasksByTheEntrysOwner(t *testing.T) {
	t.Parallel()
	fields := []uint32{1}
	for _, listType := range []tlv.ElementType{tlv.TypeArray, tlv.TypeList} {
		in := encodeEntries(t, listType, scopeEntry{indexOf(1)}, scopeEntry{indexOf(2)})
		for _, c := range []struct {
			name  string
			scope FabricScope
			want  []byte
		}{
			{"unfiltered by 2", FabricScope{Fabric: 2, Fields: fields}, listOf(listType, entryWithout(1, 1), entryWithout(2))},
			{"filtered by 2", FabricScope{Fabric: 2, Filtered: true, Fields: fields}, listOf(listType, entryWithout(2))},
			{"sensitive, unfiltered by 2", FabricScope{Fabric: 2, Sensitive: true}, listOf(listType, entryWithout(2))},
			{"unfiltered without a fabric", FabricScope{Fields: fields}, listOf(listType, entryWithout(1, 1), entryWithout(2, 1))},
			{"filtered without a fabric", FabricScope{Filtered: true, Fields: fields}, listOf(listType)},
			{"sensitive without a fabric", FabricScope{Sensitive: true}, listOf(listType)},
			{"no fields to withhold", FabricScope{Fabric: 3}, in},
		} {
			got, ok := MaskFabricScoped(in, c.scope)
			if !ok || !bytes.Equal(got, c.want) {
				t.Errorf("list type 0x%02X, %s: %v % X, want % X", listType, c.name, ok, got, c.want)
			}
		}
	}
}

// TestMaskFabricScopedRefusesWhatItCannotScope: an entry without a
// FabricIndex, or with one outside 1 to 254, refuses the whole value — whoever reads,
// filtered or not; so do a value that is no list, an entry that is no
// struct, a FabricIndex that is no unsigned integer and trailing bytes. A
// null value passes unchanged.
func TestMaskFabricScopedRefusesWhatItCannotScope(t *testing.T) {
	t.Parallel()
	for _, scope := range []FabricScope{{Fabric: 1}, {Fabric: 1, Filtered: true}, {Fabric: 1, Sensitive: true}, {}} {
		for name, in := range map[string][]byte{
			"no index":     encodeEntries(t, tlv.TypeArray, scopeEntry{indexOf(1)}, scopeEntry{}),
			"index 0":      encodeEntries(t, tlv.TypeArray, scopeEntry{indexOf(1)}, scopeEntry{indexOf(0)}),
			"index 255":    encodeEntries(t, tlv.TypeArray, scopeEntry{indexOf(1)}, scopeEntry{indexOf(255)}),
			"index 0x100":  encodeEntries(t, tlv.TypeArray, scopeEntry{indexOf(1)}, scopeEntry{indexOf(0x100)}),
			"no list":      {byte(tlv.TypeUnsignedInt1), 5},
			"no struct":    {byte(tlv.TypeArray), byte(tlv.TypeUnsignedInt1), 5, byte(tlv.TypeEndContainer)},
			"signed index": {byte(tlv.TypeArray), byte(tlv.TypeStructure), 0x20, 0xFE, 1, byte(tlv.TypeEndContainer), byte(tlv.TypeEndContainer)},
			"trailing":     append(encodeEntries(t, tlv.TypeArray, scopeEntry{indexOf(1)}), 0x04, 0x01),
			"truncated":    encodeEntries(t, tlv.TypeArray, scopeEntry{indexOf(1)})[:4],
		} {
			if got, ok := MaskFabricScoped(in, scope); ok {
				t.Errorf("%s, scope %+v: scoped to % X, want a refusal", name, scope, got)
			}
		}
	}
	// A reader whose fabric could not be resolved (255) owns no entry:
	// an entry of fabric 1 is masked for it, not served whole.
	in := encodeEntries(t, tlv.TypeArray, scopeEntry{indexOf(1)})
	if got, ok := MaskFabricScoped(in, FabricScope{Fabric: 255, Fields: []uint32{1}}); !ok || !bytes.Equal(got, listOf(tlv.TypeArray, entryWithout(1, 1))) {
		t.Errorf("unresolved reader: %v % X", ok, got)
	}
	null := []byte{byte(tlv.TypeNull)}
	if got, ok := MaskFabricScoped(null, FabricScope{Fabric: 1}); !ok || !bytes.Equal(got, null) {
		t.Errorf("null: %v % X", ok, got)
	}
}

// TestAttributeReportEncoderAppliesItsScope: an attribute report that
// carries a scope is encoded scoped, and as a FAILURE status of its path
// when the value cannot be scoped.
func TestAttributeReportEncoderAppliesItsScope(t *testing.T) {
	t.Parallel()
	path := ConcreteAttributePath{Endpoint: 1, Cluster: 0x0097, Attribute: 0, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	writeRaw := func(enc *tlv.Encoder, tag tlv.Tag, v AttributeValue) {
		_ = enc.PutRawElement(tag, v.Value.([]byte))
	}
	encode := func(rep AttributeReport) ReportData {
		enc := tlv.NewEncoder()
		ReportData{Reports: []AttributeReport{rep}}.MarshalTLV(enc, writeRaw)
		b, err := enc.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		readRaw := func(_ ConcreteAttributePath, el tlv.Element, dec *tlv.Decoder) (AttributeValue, error) {
			// Re-encode what arrived to compare it.
			sub := tlv.NewEncoder()
			if err := copyElement(sub, el, dec); err != nil {
				return AttributeValue{}, err
			}
			out, err := sub.Bytes()
			return AttributeValue{Value: out}, err
		}
		rd, err := UnmarshalReportDataTLV(tlv.NewDecoder(b), readRaw, nil)
		if err != nil {
			t.Fatal(err)
		}
		return rd
	}
	two := encodeEntries(t, tlv.TypeArray, scopeEntry{indexOf(1)}, scopeEntry{indexOf(2)})
	rd := encode(AttributeReport{Path: path, Value: AttributeValue{Value: two}, FabricScope: &FabricScope{Fabric: 2, Filtered: true}})
	if len(rd.Reports) != 1 || rd.Reports[0].IsStatus || !bytes.Equal(rd.Reports[0].Value.Value.([]byte), listOf(tlv.TypeArray, entryWithout(2))) {
		t.Errorf("scoped report %+v", rd.Reports)
	}
	bad := encodeEntries(t, tlv.TypeArray, scopeEntry{})
	rd = encode(AttributeReport{Path: path, Value: AttributeValue{Value: bad}, FabricScope: &FabricScope{Fabric: 2}})
	if len(rd.Reports) != 1 || !rd.Reports[0].IsStatus || rd.Reports[0].Status.Status != StatusFailure || rd.Reports[0].Path != path {
		t.Errorf("unscopable report %+v, want FAILURE for %v", rd.Reports, path)
	}
	// Without a scope the value goes out as written.
	rd = encode(AttributeReport{Path: path, Value: AttributeValue{Value: bad}})
	if len(rd.Reports) != 1 || rd.Reports[0].IsStatus || !bytes.Equal(rd.Reports[0].Value.Value.([]byte), bad) {
		t.Errorf("unscoped report %+v", rd.Reports)
	}
}

// copyElement writes the element el opened, and what it contains, to enc
// under an anonymous tag.
func copyElement(enc *tlv.Encoder, el tlv.Element, dec *tlv.Decoder) error {
	var put func(tag tlv.Tag, el tlv.Element) error
	put = func(tag tlv.Tag, el tlv.Element) error {
		switch el.Type {
		case tlv.TypeArray, tlv.TypeList, tlv.TypeStructure:
			switch el.Type {
			case tlv.TypeArray:
				enc.StartArray(tag)
			case tlv.TypeList:
				enc.StartList(tag)
			default:
				enc.StartStruct(tag)
			}
			for {
				c, err := dec.Next()
				if err != nil {
					return err
				}
				if c.IsEndContainer {
					return enc.EndContainer()
				}
				if err := put(c.Tag, c); err != nil {
					return err
				}
			}
		case tlv.TypeUnsignedInt1, tlv.TypeUnsignedInt2, tlv.TypeUnsignedInt4, tlv.TypeUnsignedInt8:
			enc.PutUint(tag, el.Uint)
		default:
			enc.PutNull(tag)
		}
		return nil
	}
	return put(tlv.AnonymousTag(), el)
}

// sensitiveEvent is a record of a fabric-sensitive event: the IM layer
// reads its FabricIndex field by name.
type sensitiveEvent struct{ FabricIndex uint8 }

// indexlessEvent is a record without a FabricIndex (AccountLogin
// LoggedOut's payload carries none).
type indexlessEvent struct{ Node uint64 }

// TestEveryFabricSensitiveEventOfTheModelIsGated holds the event gate to
// the generated table for every event of parity/schema.json whose access
// is fabric-sensitive ("S"): a record of another fabric, and a record
// without a FabricIndex, is not delivered; the reader's own is. An event
// without the flag is delivered whatever its payload.
func TestEveryFabricSensitiveEventOfTheModelIsGated(t *testing.T) {
	t.Parallel()
	var snap struct {
		Clusters []struct {
			ID     uint32 `json:"id"`
			Name   string `json:"name"`
			Events []struct {
				ID        uint32 `json:"id"`
				Name      string `json:"name"`
				Effective struct {
					Access struct {
						Fabric string `json:"fabric"`
					} `json:"access"`
				} `json:"effective"`
			} `json:"events"`
		} `json:"clusters"`
	}
	if err := json.Unmarshal(parity.SchemaJSON(), &snap); err != nil {
		t.Fatal(err)
	}
	auth := EventReadAuthorizer{Checker: fakeEventACL{allowFabric: 2, maxPriv: 5}, FabricIndex: 2} // the ACL gate passes
	sensitive := 0
	for _, c := range snap.Clusters {
		for _, e := range c.Events {
			path := ConcreteEventPath{Endpoint: 1, Cluster: c.ID, Event: e.ID, HasEndpoint: true, HasCluster: true, HasEvent: true}
			report := func(payload any) EventReport {
				return EventReport{Path: path, Number: 1, Priority: EventPriorityInfo, Data: AttributeValue{Value: payload}}
			}
			got := AuthorizeEventReports(context.Background(), auth, []EventReport{
				report(sensitiveEvent{1}), report(indexlessEvent{}), report(sensitiveEvent{2}),
			})
			if e.Effective.Access.Fabric == "S" {
				sensitive++
				if len(got) != 1 || got[0].Data.Value != (sensitiveEvent{2}) {
					t.Errorf("%s.%s (S): delivered %+v, want the reader's record alone", c.Name, e.Name, got)
				}
				continue
			}
			if len(got) != 3 {
				t.Errorf("%s.%s: delivered %d of 3 records", c.Name, e.Name, len(got))
			}
		}
	}
	if sensitive != 10 {
		t.Errorf("%d fabric-sensitive events in the snapshot, the pin's 10", sensitive)
	}
}
