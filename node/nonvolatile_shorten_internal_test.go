// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"encoding/hex"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	userlabeldef "thingfabric.dev/go-fabric/cluster/spec/userlabel"
	"thingfabric.dev/go-fabric/tlv"
)

// TestRestoreShortensStoredStrings: a nonvolatile value stored while a
// string bound counted UTF-16 code units is restored with every string
// over its numeric maximum in UTF-8 bytes cut to it between two runes —
// at the top, in a list (to the smaller of the entry type's and the list's
// entry maximum, "max 3[max 4]") and in a struct — as matter.js's
// datasource shortens stored values at load
// (packages/node/src/behavior/state/managed/Datasource.ts:124-220 at
// b25ed960, #4995); a value within its bounds, and a bound that is not a
// number, cut nothing.
func TestRestoreShortensStoredStrings(t *testing.T) {
	t.Parallel()
	str := spec.Type{Kind: spec.KindString}
	bound := func(n int64) spec.Constraint {
		return spec.Constraint{Max: &spec.Bound{Int: n}}
	}
	stored := func(t *testing.T, put func(*tlv.Encoder)) string {
		t.Helper()
		enc := tlv.NewEncoder()
		put(enc)
		b, err := enc.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		return hex.EncodeToString(b)
	}
	texts := func(n spec.Node) []string {
		out := make([]string, 0, len(n.Children))
		for _, ch := range n.Children {
			out = append(out, ch.El.String)
		}
		return out
	}
	asNode := func(n spec.Node) (any, error) { return n, nil }

	t.Run("a string", func(t *testing.T) {
		t.Parallel()
		a := &spec.Attribute{Name: "Label", Type: str, Constraint: bound(4)}
		v, cut, err := decodeNonVolatile(a, stored(t, func(e *tlv.Encoder) { e.PutUTF8(tlv.AnonymousTag(), "aéé") }))
		if err != nil || cut != 1 || v != "aé" {
			t.Fatalf("restored %q, %d cut, %v; want \"aé\", 1", v, cut, err)
		}
		v, cut, err = decodeNonVolatile(a, stored(t, func(e *tlv.Encoder) { e.PutUTF8(tlv.AnonymousTag(), "éé") }))
		if err != nil || cut != 0 || v != "éé" {
			t.Fatalf("restored %q, %d cut, %v; want \"éé\" whole", v, cut, err)
		}
	})

	t.Run("a bound that is not a number", func(t *testing.T) {
		t.Parallel()
		a := &spec.Attribute{Name: "Location", Type: str, Constraint: spec.Constraint{Value: &spec.Bound{Int: 2}}}
		v, cut, err := decodeNonVolatile(a, stored(t, func(e *tlv.Encoder) { e.PutUTF8(tlv.AnonymousTag(), "日本") }))
		if err != nil || cut != 0 || v != "日本" {
			t.Fatalf("restored %q, %d cut, %v; want it whole", v, cut, err)
		}
	})

	t.Run("the entries of a list, to the smaller maximum", func(t *testing.T) {
		t.Parallel()
		a := &spec.Attribute{
			Name:       "Names",
			Type:       spec.Type{Kind: spec.KindList, Entry: &spec.Field{Type: str, Constraint: bound(4)}},
			Constraint: spec.Constraint{Max: &spec.Bound{Int: 10}, Entry: &spec.Constraint{Max: &spec.Bound{Int: 3}}},
			Decode:     asNode,
		}
		v, cut, err := decodeNonVolatile(a, stored(t, func(e *tlv.Encoder) {
			e.StartArray(tlv.AnonymousTag())
			for _, s := range []string{"ab", "éé", "abcdef"} {
				e.PutUTF8(tlv.AnonymousTag(), s)
			}
			_ = e.EndContainer()
		}))
		if err != nil || cut != 2 {
			t.Fatalf("%d cut, %v; want 2", cut, err)
		}
		if got := strings.Join(texts(v.(spec.Node)), ","); got != "ab,é,abc" {
			t.Fatalf("restored %s, want ab,é,abc", got)
		}
	})

	t.Run("the fields of a struct", func(t *testing.T) {
		t.Parallel()
		a := &spec.Attribute{
			Name: "Pair",
			Type: spec.Type{Kind: spec.KindStruct, Struct: &spec.Struct{Fields: []spec.Field{
				{ID: 0, Name: "Label", Type: str, Constraint: bound(16)},
				{ID: 1, Name: "Value", Type: str, Constraint: bound(16)},
			}}},
			Decode: asNode,
		}
		v, cut, err := decodeNonVolatile(a, stored(t, func(e *tlv.Encoder) {
			e.StartStruct(tlv.AnonymousTag())
			e.PutUTF8(tlv.ContextTag(0), strings.Repeat("é", 9))
			e.PutUTF8(tlv.ContextTag(1), "v")
			_ = e.EndContainer()
		}))
		if err != nil || cut != 1 {
			t.Fatalf("%d cut, %v; want 1", cut, err)
		}
		if got := strings.Join(texts(v.(spec.Node)), ","); got != strings.Repeat("é", 8)+",v" {
			t.Fatalf("restored %s, want 8 × é,v", got)
		}
	})
}

// TestShortenStoredLabels: a UserLabel list stored as JSON while "max 16"
// counted UTF-16 code units restores with its Label and Value cut to 16
// bytes; a list within its bounds comes back unchanged.
func TestShortenStoredLabels(t *testing.T) {
	t.Parallel()
	in := []userlabeldef.LabelStruct{{Label: strings.Repeat("é", 9), Value: "v"}, {Label: "k", Value: "a" + strings.Repeat("é", 8)}}
	got := shortenStoredLabels(slog.New(slog.DiscardHandler), "k", in)
	want := []userlabeldef.LabelStruct{{Label: strings.Repeat("é", 8), Value: "v"}, {Label: "k", Value: "a" + strings.Repeat("é", 7)}}
	if !slices.Equal(got, want) {
		t.Fatalf("restored %+v, want %+v", got, want)
	}
	ok := []userlabeldef.LabelStruct{{Label: strings.Repeat("é", 8), Value: "v"}}
	if got := shortenStoredLabels(slog.New(slog.DiscardHandler), "k", ok); !slices.Equal(got, ok) {
		t.Fatalf("restored %+v, want it unchanged", got)
	}
}
