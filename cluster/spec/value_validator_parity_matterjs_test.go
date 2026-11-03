// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	wcdef "github.com/SukramJ/go-fabric/cluster/spec/windowcovering"
	"github.com/SukramJ/go-fabric/im"
)

// Parity with matter.js
// packages/node/test/behavior/state/validation/ValueValidatorTest.ts at
// 862ad75f. matter.js validates a value against a session: a local one (the
// device's own state, [spec.Instance.CheckValue] / CheckSet here), a wire
// one (a controller's write, [spec.Instance.ValidateWrite]) and a client
// peer one, which the Go client path does not validate at all. A refusal
// matter.js raises as ConformanceError, ConstraintError, DatatypeError or
// IntegerRangeError is CONSTRAINT_ERROR here, as on the wire; an
// attribute the conformance does not allow is not served and answers
// UNSUPPORTED_ATTRIBUTE.

// bitConformanceCluster is ValueValidatorTest.ts:136-162 bitmapCluster: a
// map16 Modes whose bits are gated on the feature LT (Latching, bit 0)
// by every conformance form the bitmap validator meets.
func bitConformanceCluster(t *testing.T) *spec.Cluster {
	t.Helper()
	member := func(name string, bit, width uint, conformance string) spec.BitmapMember {
		return spec.BitmapMember{Name: name, Bit: bit, Width: width, Conformance: specConformance(t, conformance)}
	}
	modes := &spec.Bitmap{Name: "Modes", Bits: 16, Members: []spec.BitmapMember{
		member("IfFeature", 0, 1, "LT"),
		member("Always", 1, 1, "O"),
		member("Deprecated", 2, 1, "D"),
		member("Choice", 3, 1, "[LT].b+"),
		member("Named", 4, 1, "Other"),
		member("Range", 5, 2, "LT"),
		member("Never", 7, 1, "X"),
		member("Unreadable", 8, 1, "LT > 4"),
		member("Grouped", 9, 1, "LT, X"),
	}}
	return &spec.Cluster{
		ID: 0xFFF1_FC03, Name: "Test", Revision: 1,
		Features: []spec.Feature{{Name: "LT", Title: "Latching", Bit: 0, Conformance: op(spec.ConfOptional)}},
		Attributes: []spec.Attribute{
			rw(0, "Modes", spec.Type{Kind: spec.KindBitmap, Bits: 16, Bitmap: modes}, spec.Quality{}, spec.Constraint{}),
		},
	}
}

// reservedBitmapCluster is ValueValidatorTest.ts:47-52: a map8 whose
// multiA occupies bits 0–2 and multiB bits 4–6; bits 3 and 7 are
// reserved.
var reservedBitmapCluster = &spec.Cluster{
	ID: 0xFFF1_FC04, Name: "Reserved", Revision: 1,
	Attributes: []spec.Attribute{
		rw(1, "TestBitmap", spec.Type{Kind: spec.KindBitmap, Bits: 8, Bitmap: &spec.Bitmap{Name: "TestBitmap", Bits: 8, Members: []spec.BitmapMember{
			{Name: "MultiA", Bit: 0, Width: 3}, {Name: "MultiB", Bit: 4, Width: 3},
		}}}, spec.Quality{}, spec.Constraint{}),
	},
}

func mustInstance(t *testing.T, def *spec.Cluster, features uint32) *spec.Instance {
	t.Helper()
	inst, err := spec.New(def, spec.Options{Features: features})
	if err != nil {
		t.Fatal(err)
	}
	return inst
}

// wantConstraint asserts err is a CONSTRAINT_ERROR whose message holds
// each of words.
func wantConstraint(t *testing.T, err error, words ...string) {
	t.Helper()
	if statusOf(err) != im.StatusConstraintError {
		t.Fatalf("err = %v, want CONSTRAINT_ERROR", err)
	}
	for _, w := range words {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("err %q lacks %q", err, w)
		}
	}
}

func TestValueValidatorParityMatterJS(t *testing.T) {
	t.Parallel()

	// "bitmap reserved bits" (:45-81).
	reserved := mustInstance(t, reservedBitmapCluster, 0)
	t.Run("accepts defined multi-bit field values", func(t *testing.T) { // :56-58
		t.Parallel()
		if _, err := reserved.ValidateWrite(1, uint64(0b0110_0101), nil); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("rejects a reserved gap bit", func(t *testing.T) { // :60-65
		t.Parallel()
		_, err := reserved.ValidateWrite(1, uint64(0b0000_1000), nil)
		wantConstraint(t, err, "reserved")
	})

	// "bitmap bit conformance" (:134-309): judged on a value the device
	// sets, never on a controller's write.
	lt := uint32(1)
	local := func(t *testing.T, features uint32, value uint64) error {
		t.Helper()
		_, err := mustInstance(t, bitConformanceCluster(t), features).CheckValue(0, value, nil)
		return err
	}
	accepts := func(t *testing.T, features uint32, value uint64) {
		t.Helper()
		if err := local(t, features, value); err != nil {
			t.Errorf("0x%X refused: %v", value, err)
		}
	}
	refuses := func(t *testing.T, features uint32, value uint64) {
		t.Helper()
		err := local(t, features, value)
		wantConstraint(t, err, "Matter does not allow you to set this value")
		if !errors.Is(err, spec.ErrConformance) {
			t.Errorf("err %v is not ErrConformance", err)
		}
	}
	t.Run("accepts a bit the value leaves clear", func(t *testing.T) { t.Parallel(); accepts(t, 0, 0) }) // :186-190
	t.Run("accepts a bit that states no gate", func(t *testing.T) { t.Parallel(); accepts(t, 0, 1<<1) }) // :192-196
	t.Run("accepts a deprecated bit", func(t *testing.T) { t.Parallel(); accepts(t, 0, 1<<2) })          // :199-203
	t.Run("rejects a bit whose choice is gated on an unsupported feature", func(t *testing.T) {          // :205-209
		t.Parallel()
		refuses(t, 0, 1<<3)
	})
	t.Run("rejects a bit a group disallows once its gate fails", func(t *testing.T) { t.Parallel(); refuses(t, 0, 1<<9) }) // :212-216
	t.Run("rejects a bit the specification disallows", func(t *testing.T) { t.Parallel(); refuses(t, lt, 1<<7) })          // :218-222
	t.Run("accepts a bit whose conformance names something a value would decide", func(t *testing.T) {                     // :226-230
		t.Parallel()
		accepts(t, 0, 1<<4)
	})
	t.Run("accepts a bit whose conformance the compiler cannot read", func(t *testing.T) { t.Parallel(); accepts(t, 0, 1<<8) }) // :233-237
	t.Run("judges a range of bits by whether the write sets any of them", func(t *testing.T) {                                  // :239-244
		t.Parallel()
		accepts(t, 0, 0)
		refuses(t, 0, 1<<5)
		refuses(t, 0, 1<<6)
		accepts(t, lt, 3<<5)
	})
	// The gated bit itself, which the cases above take for granted
	// (:176-184).
	refuses(t, 0, 1<<0)
	accepts(t, lt, 1<<0)

	// :248-257: WindowCovering.OperationalStatus with Lift and
	// PositionAwareLift — Lift (bits 2–3) may be set, Tilt (bits 4–5) not.
	// The attribute is read-only, so the device sets it (CheckValue).
	t.Run("judges a range of a shipped cluster by its feature alone", func(t *testing.T) {
		t.Parallel()
		wc := mustInstance(t, wcdef.Definition, uint32(wcdef.FeatureLift|wcdef.FeaturePositionAwareLift))
		if _, err := wc.CheckValue(wcdef.AttrOperationalStatus, uint64(3<<2), nil); err != nil {
			t.Errorf("lift 3 refused: %v", err)
		}
		_, err := wc.CheckValue(wcdef.AttrOperationalStatus, uint64(1<<4), nil)
		wantConstraint(t, err, "Tilt", `"TL"`)
	})

	// :261-274: the bits of a feature map are the selection the others are
	// judged against, so a FeatureMap value (0xFFFC) is not judged by its
	// bits' conformance; the same bitmap on another attribute is.
	t.Run("does not judge the bits of a feature map", func(t *testing.T) {
		t.Parallel()
		features := &spec.Bitmap{Name: "Feature", Bits: 32, Members: []spec.BitmapMember{
			{Name: "Latching", Bit: 0, Width: 1},
			{Name: "RemoteLatching", Bit: 1, Width: 1, Conformance: specConformance(t, "LT")},
		}}
		typ := spec.Type{Kind: spec.KindBitmap, Bits: 32, Bitmap: features}
		def := &spec.Cluster{
			ID: 0xFFF1_FC05, Name: "Test", Revision: 1,
			Features: []spec.Feature{
				{Name: "LT", Title: "Latching", Bit: 0, Conformance: op(spec.ConfOptional)},
				{Name: "RL", Title: "RemoteLatching", Bit: 1, Conformance: op(spec.ConfOptional)},
			},
			Attributes: []spec.Attribute{
				{ID: 0xFFFC, Name: "FeatureMap", Type: typ, Conformance: op(spec.ConfMandatory), Access: spec.Access{RW: "R"}},
				{ID: 0, Name: "Copy", Type: typ, Conformance: op(spec.ConfMandatory), Access: spec.Access{RW: "R"}},
			},
		}
		inst := mustInstance(t, def, 1<<1)
		if _, err := inst.CheckValue(0xFFFC, uint64(1<<1), nil); err != nil {
			t.Errorf("feature map judged: %v", err)
		}
		_, err := inst.CheckValue(0, uint64(1<<1), nil)
		wantConstraint(t, err, "RemoteLatching")
	})

	// :287-291: a write from the wire is not judged by a bit's conformance.
	t.Run("ignores the bit on a write from the wire", func(t *testing.T) {
		t.Parallel()
		inst := mustInstance(t, bitConformanceCluster(t), 0)
		if _, err := inst.ValidateWrite(0, uint64(1<<0|1<<3|1<<5|1<<7|1<<9), nil); err != nil {
			t.Errorf("wire write refused: %v", err)
		}
	})

	// "list constraint of the type a list derives from" (:311-335): the
	// generator resolves a list's constraint inherited from its named
	// datatype into the attribute's (script/clustergen), so the attribute
	// carries "max 2".
	listCluster := &spec.Cluster{ID: 0xFFF1_FC06, Name: "Test", Revision: 1, Attributes: []spec.Attribute{
		rw(1, "History", spec.Type{Kind: spec.KindList, Entry: &spec.Field{Type: spec.Type{Kind: spec.KindUint, Bits: 8}}}, spec.Quality{}, spec.Constraint{Text: "max 2", Max: &spec.Bound{Int: 2}}),
	}}
	list := mustInstance(t, listCluster, 0)
	t.Run("accepts a list within the bound", func(t *testing.T) { // :327-329
		t.Parallel()
		if _, err := list.ValidateWrite(1, []uint8{1, 2}, nil); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("rejects a list beyond the bound and names the bound", func(t *testing.T) { // :331-334
		t.Parallel()
		_, err := list.ValidateWrite(1, []uint8{1, 2, 3}, nil)
		wantConstraint(t, err, `"max 2"`)
	})

	// "obsolete elements" (:337-379): an obsolete (Z) attribute is not
	// served, an obsolete enum value is not supported.
	obsolete := mustInstance(t, &spec.Cluster{ID: 0xFFF1_FC07, Name: "Test", Revision: 1, Attributes: []spec.Attribute{
		{ID: 0, Name: "Legacy", Type: spec.Type{Kind: spec.KindUint, Bits: 8}, Conformance: op(spec.ConfObsolete), Access: spec.Access{RW: "RW", Write: spec.PrivilegeOperate}},
		rw(1, "Mode", spec.Type{Kind: spec.KindEnum, Bits: 8, Enum: &spec.Enum{Name: "Mode", Bits: 8, Values: []spec.EnumValue{
			{Value: 0, Name: "On", Conformance: op(spec.ConfObsolete)}, {Value: 1, Name: "Off"},
		}}}, spec.Quality{}, spec.Constraint{}),
	}}, 0)
	t.Run("rejects an obsolete attribute on a server write", func(t *testing.T) { // :360-363
		t.Parallel()
		if _, err := obsolete.ValidateWrite(0, uint64(1), nil); statusOf(err) != im.StatusUnsupportedAttribute {
			t.Errorf("err = %v, want UNSUPPORTED_ATTRIBUTE", err)
		}
	})
	t.Run("rejects an obsolete enum value on a server write", func(t *testing.T) { // :370-373
		t.Parallel()
		_, err := obsolete.ValidateWrite(1, uint64(0), nil)
		wantConstraint(t, err)
		if _, err := obsolete.ValidateWrite(1, uint64(1), nil); err != nil {
			t.Errorf("Off refused: %v", err)
		}
	})

	// "client peer leniency" (:381-500): the server-side halves; the Go
	// client path validates nothing, so every forwarding case has no
	// subject.
	t.Run("still rejects a structurally invalid bitmap field even alongside reserved bits on a peer write", func(t *testing.T) { // :471-476
		t.Parallel()
		// multiA = 99 does not fit bits 0–2: it sets bits outside every
		// member, alongside reserved bit 7.
		_, err := reserved.ValidateWrite(1, uint64(99|0b1000_0000), nil)
		wantConstraint(t, err)
	})
	uint8Cluster := &spec.Cluster{
		ID: 0xFFF1_FC08, Name: "Test", Revision: 1,
		Features: []spec.Feature{{Name: "FT", Title: "Feature", Bit: 0, Conformance: op(spec.ConfOptional)}},
		Attributes: []spec.Attribute{
			rw(0, "Foo", spec.Type{Kind: spec.KindUint, Bits: 8}, spec.Quality{}, spec.Constraint{}),
			rw(1, "Bounded", spec.Type{Kind: spec.KindUint, Bits: 8}, spec.Quality{}, spec.Constraint{Text: "0 to 10", Min: &spec.Bound{Int: 0}, Max: &spec.Bound{Int: 10}}),
			func() spec.Attribute {
				a := rw(2, "Gated", spec.Type{Kind: spec.KindUint, Bits: 8}, spec.Quality{}, spec.Constraint{})
				a.Conformance = name("FT")
				return a
			}(),
		},
	}
	ints := mustInstance(t, uint8Cluster, 0)
	t.Run("still rejects a wrong-datatype value on a client peer write", func(t *testing.T) { // :478-480
		t.Parallel()
		_, err := ints.ValidateWrite(0, "nope", nil)
		wantConstraint(t, err)
	})
	t.Run("still rejects a value-range constraint on a client peer write", func(t *testing.T) { // :482-484
		t.Parallel()
		_, err := ints.ValidateWrite(0, uint64(0x1ff), nil)
		wantConstraint(t, err)
	})
	t.Run("still rejects a schema-constraint violation on a client peer write", func(t *testing.T) { // :486-488
		t.Parallel()
		_, err := ints.ValidateWrite(1, uint64(20), nil)
		wantConstraint(t, err, `"0 to 10"`)
	})
	t.Run("rejects a feature-disallowed attribute on a server write", func(t *testing.T) { // :490-492
		t.Parallel()
		if _, err := ints.ValidateWrite(2, uint64(5), nil); statusOf(err) != im.StatusUnsupportedAttribute {
			t.Errorf("err = %v, want UNSUPPORTED_ATTRIBUTE", err)
		}
	})
	t.Run("still validates datatype when a forwarded conformance failure would otherwise skip it", func(t *testing.T) { // :498-500
		t.Parallel()
		if _, err := ints.ValidateWrite(2, "nope", nil); err == nil {
			t.Error("wrong datatype on a feature-disallowed attribute accepted")
		}
	})
}
