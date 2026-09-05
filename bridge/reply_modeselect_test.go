// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// White-box test for the ModeSelect.SupportedModes encoding added to
// defaultAttributeValueWriter. Lives in package bridge because the
// writer is unexported.
//
// The writer's default branch encodes anything it does not recognise as
// TLV null, so a list-of-struct attribute without a case of its own
// reads as "exists, no value" on every controller — a well-formed reply
// that carries nothing, and no error anywhere to say so.
package bridge

import (
	"testing"

	mattermodeselect "github.com/SukramJ/go-fabric/cluster/modeselect"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// decodedSemanticTag mirrors one decoded SemanticTagStruct.
type decodedSemanticTag struct {
	mfgCode uint16
	value   uint16
}

// decodedModeOption mirrors one decoded ModeOptionStruct.
//
// tagsPresent is what separates "the encoder wrote SemanticTags and it
// was empty" from "the encoder never wrote the field": the decoded
// slice is len 0 in both cases, so an assertion on length alone passes
// against an encoder that drops the conformance-M field entirely.
type decodedModeOption struct {
	label       string
	mode        uint8
	tagsPresent bool
	tags        []decodedSemanticTag
}

// decodeSupportedModes encodes v through the writer and reads the array
// back, failing when the value did not encode as a TLV array of
// structures.
func decodeSupportedModes(t *testing.T, v im.AttributeValue) []decodedModeOption {
	t.Helper()
	enc := tlv.NewEncoder()
	defaultAttributeValueWriter(enc, tlv.AnonymousTag(), v)
	b, err := enc.Bytes()
	if err != nil {
		t.Fatalf("encoder.Bytes: %v", err)
	}
	d := tlv.NewDecoder(b)
	open, err := d.Next()
	if err != nil {
		t.Fatalf("decoder.Next: %v", err)
	}
	if open.Type != tlv.TypeArray {
		t.Fatalf("encoded as type 0x%02X (isNull=%v), want an array — an attribute the writer "+
			"does not recognise falls through to the null default", open.Type, open.IsNull)
	}

	out := []decodedModeOption{}
	for {
		el, nerr := d.Next()
		if nerr != nil {
			t.Fatalf("decoder.Next: %v", nerr)
		}
		if el.IsEndContainer {
			return out
		}
		if el.Type != tlv.TypeStructure {
			t.Fatalf("array entry has type 0x%02X, want a structure", el.Type)
		}
		out = append(out, decodeOneModeOption(t, d))
	}
}

// decodeOneModeOption reads the fields of one ModeOptionStruct, the
// opening structure element having already been consumed.
func decodeOneModeOption(t *testing.T, d *tlv.Decoder) decodedModeOption {
	t.Helper()
	// tags stays nil until the field is actually seen; pre-seeding it
	// with an empty slice is what made the "must ride even when empty"
	// assertion unable to fail.
	var opt decodedModeOption
	for {
		el, err := d.Next()
		if err != nil {
			t.Fatalf("decoder.Next: %v", err)
		}
		if el.IsEndContainer {
			return opt
		}
		if el.Tag.Kind != tlv.TagKindContext {
			t.Fatalf("ModeOptionStruct field carries tag kind %v, want a context tag", el.Tag.Kind)
		}
		switch uint8(el.Tag.Number) {
		case mattermodeselect.ModeOptionFieldLabel:
			opt.label = el.String
		case mattermodeselect.ModeOptionFieldMode:
			opt.mode = uint8(el.Uint)
		case mattermodeselect.ModeOptionFieldSemanticTags:
			if el.Type != tlv.TypeArray {
				t.Fatalf("SemanticTags has type 0x%02X, want an array", el.Type)
			}
			opt.tagsPresent = true
			opt.tags = decodeSemanticTags(t, d)
		default:
			t.Fatalf("unexpected ModeOptionStruct field tag %d", el.Tag.Number)
		}
	}
}

// decodeSemanticTags reads a SemanticTagStruct array, the opening array
// element having already been consumed.
func decodeSemanticTags(t *testing.T, d *tlv.Decoder) []decodedSemanticTag {
	t.Helper()
	tags := []decodedSemanticTag{}
	for {
		el, err := d.Next()
		if err != nil {
			t.Fatalf("decoder.Next: %v", err)
		}
		if el.IsEndContainer {
			return tags
		}
		if el.Type != tlv.TypeStructure {
			t.Fatalf("SemanticTags entry has type 0x%02X, want a structure", el.Type)
		}
		var tag decodedSemanticTag
		for {
			f, ferr := d.Next()
			if ferr != nil {
				t.Fatalf("decoder.Next: %v", ferr)
			}
			if f.IsEndContainer {
				break
			}
			switch uint8(f.Tag.Number) {
			case mattermodeselect.SemanticTagFieldMfgCode:
				tag.mfgCode = uint16(f.Uint)
			case mattermodeselect.SemanticTagFieldValue:
				tag.value = uint16(f.Uint)
			default:
				t.Fatalf("unexpected SemanticTagStruct field tag %d", f.Tag.Number)
			}
		}
		tags = append(tags, tag)
	}
}

// TestDefaultAttrWriter_ModeSelectSupportedModes pins that
// SupportedModes round-trips: labels, mode values and semantic tags all
// come back, and a mode with no tags still carries the conformance-M
// SemanticTags field as an empty array rather than dropping it.
func TestDefaultAttrWriter_ModeSelectSupportedModes(t *testing.T) {
	t.Parallel()

	modes := []mattermodeselect.ModeOptionStruct{
		{Label: "None", Mode: 0},
		{
			Label: "Some",
			Mode:  1,
			SemanticTags: []mattermodeselect.SemanticTagStruct{
				{MfgCode: 0x1234, Value: 9},
				{MfgCode: 0x1234, Value: 300},
			},
		},
	}
	got := decodeSupportedModes(t, im.AttributeValue{Value: modes})

	if len(got) != len(modes) {
		t.Fatalf("decoded %d modes, want %d", len(got), len(modes))
	}
	for i, want := range modes {
		if got[i].label != want.Label {
			t.Errorf("mode %d label = %q, want %q", i, got[i].label, want.Label)
		}
		if got[i].mode != want.Mode {
			t.Errorf("mode %d value = %d, want %d", i, got[i].mode, want.Mode)
		}
		if !got[i].tagsPresent {
			t.Fatalf("mode %d carries no SemanticTags field — the field is conformance M "+
				"(mode-select-cluster.element.ts:66) and must ride even when the mode has no tags", i)
		}
		if len(got[i].tags) != len(want.SemanticTags) {
			t.Fatalf("mode %d decoded %d semantic tags, want %d — SemanticTags is conformance M "+
				"and must ride even when empty", i, len(got[i].tags), len(want.SemanticTags))
		}
		for j, wantTag := range want.SemanticTags {
			if got[i].tags[j].mfgCode != wantTag.MfgCode {
				t.Errorf("mode %d tag %d MfgCode = %#04x, want %#04x", i, j, got[i].tags[j].mfgCode, wantTag.MfgCode)
			}
			if got[i].tags[j].value != wantTag.Value {
				t.Errorf("mode %d tag %d Value = %d, want %d", i, j, got[i].tags[j].value, wantTag.Value)
			}
		}
	}
}

// TestDefaultAttrWriter_ModeSelectListBounds pins the two list
// constraints matter.js states, the way Label's byte bound is already
// pinned: SupportedModes is "max 255"
// (mode-select-cluster.element.ts:36) and SemanticTags is "max 64"
// (:66). A host over either bound is trimmed at encode time rather than
// putting a list on the wire that a controller must reject.
func TestDefaultAttrWriter_ModeSelectListBounds(t *testing.T) {
	t.Parallel()

	modes := make([]mattermodeselect.ModeOptionStruct, mattermodeselect.SupportedModesMaxEntries+1)
	for i := range modes {
		modes[i] = mattermodeselect.ModeOptionStruct{Label: "m", Mode: uint8(i)} //nolint:gosec // index bounded by the 256-entry list
	}
	modes[0].SemanticTags = make([]mattermodeselect.SemanticTagStruct, mattermodeselect.SemanticTagsMaxEntries+1)

	got := decodeSupportedModes(t, im.AttributeValue{Value: modes})

	if len(got) != mattermodeselect.SupportedModesMaxEntries {
		t.Errorf("encoded %d modes, want %d — SupportedModes carries constraint \"max 255\"",
			len(got), mattermodeselect.SupportedModesMaxEntries)
	}
	if len(got[0].tags) != mattermodeselect.SemanticTagsMaxEntries {
		t.Errorf("encoded %d semantic tags, want %d — SemanticTags carries constraint \"max 64\"",
			len(got[0].tags), mattermodeselect.SemanticTagsMaxEntries)
	}
}

// TestDefaultAttrWriter_ModeSelectEmptyList pins that a host with no
// modes yet encodes as an empty array. Null would parse as "missing",
// which is a different statement than "present and empty".
func TestDefaultAttrWriter_ModeSelectEmptyList(t *testing.T) {
	t.Parallel()

	got := decodeSupportedModes(t, im.AttributeValue{Value: []mattermodeselect.ModeOptionStruct{}})
	if len(got) != 0 {
		t.Errorf("decoded %d modes, want 0", len(got))
	}
}
