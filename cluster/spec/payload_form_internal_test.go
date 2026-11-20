// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import "testing"

type pfGenerated struct{ Level uint16 }

type pfForeign struct{ Level uint16 } // the generated names, by coincidence

type pfDeclared struct{ Lvl uint16 }

// TestPayloadFormIsJudgedByConstructionNotByName (A3 of the P2-H
// post-read): the request gate and the response check judge a payload in
// the definition's own generated form — the type its decoder returns — or
// in a declared form ([MapFields], [DefinitionFormer]); a foreign type is
// not judged, even when its Go field names match the definition's.
func TestPayloadFormIsJudgedByConstructionNotByName(t *testing.T) {
	t.Parallel()
	fields := []Field{{ID: 0, Name: "Level", Type: Type{Kind: KindUint, Bits: 16}, Conformance: Conformance{Op: ConfMandatory}}}
	cmd := &Command{ID: 0, Name: "Add", Direction: Request, Fields: fields, Decode: func(Node) (any, error) { return pfGenerated{}, nil }}
	MapFields[pfDeclared](&Struct{Name: "Add", Fields: fields}, FieldMap{"Level": "Lvl"})
	for _, c := range []struct {
		name string
		v    any
		ok   bool
	}{
		{"the generated form", pfGenerated{Level: 1}, true},
		{"a pointer to it", &pfGenerated{Level: 1}, true},
		{"a declared form", pfDeclared{Lvl: 1}, true},
		{"a foreign type with the same names", pfForeign{Level: 1}, false},
		{"absent", nil, true},
	} {
		if _, ok := payloadValue(cmd, c.v, false); ok != c.ok {
			t.Errorf("%s: judged %v, want %v", c.name, ok, c.ok)
		}
	}
	if _, ok := payloadValue(&Command{Fields: fields}, pfGenerated{}, false); ok {
		t.Error("a command without a decoder judged a payload it states no form for")
	}
}

// TestPayloadFormOfAPointerDecoder (L1 of the P2 final read): a
// hand-written decoder that returns a pointer to its payload type — or a
// typed nil one on the empty structure — states that type as the form
// judged, as a value-returning one does; only an untyped nil leaves the
// command's payloads unjudged ([Command.Decode]).
func TestPayloadFormOfAPointerDecoder(t *testing.T) {
	t.Parallel()
	fields := []Field{{ID: 0, Name: "Level", Type: Type{Kind: KindUint, Bits: 16}, Conformance: Conformance{Op: ConfMandatory}}}
	for _, c := range []struct {
		name   string
		decode func(Node) (any, error)
		judged bool
	}{
		{"a pointer", func(Node) (any, error) { return &pfGenerated{}, nil }, true},
		{"a typed nil pointer", func(Node) (any, error) { return (*pfGenerated)(nil), nil }, true},
		{"an untyped nil", func(Node) (any, error) { return nil, nil }, false},
	} {
		cmd := &Command{ID: 0, Name: "Add", Direction: Request, Fields: fields, Decode: c.decode}
		for _, v := range []any{pfGenerated{Level: 1}, &pfGenerated{Level: 1}} {
			if _, ok := payloadValue(cmd, v, false); ok != c.judged {
				t.Errorf("%s decoder, payload %T: judged %v, want %v", c.name, v, ok, c.judged)
			}
		}
		if _, ok := payloadValue(cmd, pfForeign{Level: 1}, false); ok {
			t.Errorf("%s decoder: a foreign type judged", c.name)
		}
	}
}
