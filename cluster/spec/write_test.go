// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
)

var (
	modeEnum = &spec.Enum{Name: "ModeEnum", Bits: 8, Values: []spec.EnumValue{
		{Value: 0, Name: "Off"},
		{Value: 1, Name: "Turbo", Conformance: name("TB")},
	}}
	flagsBitmap = &spec.Bitmap{Name: "Flags", Bits: 8, Members: []spec.BitmapMember{{Name: "A", Bit: 0, Width: 1}, {Name: "B", Bit: 1, Width: 2}}}
)

func rw(id uint32, n string, t spec.Type, q spec.Quality, c spec.Constraint) spec.Attribute {
	return spec.Attribute{
		ID: id, Name: n, Type: t, Conformance: op(spec.ConfMandatory),
		Access: spec.Access{RW: "RW", Read: spec.PrivilegeView, Write: spec.PrivilegeOperate}, Quality: q, Constraint: c,
	}
}

var writeCluster = &spec.Cluster{
	ID: 0xFFF2, Name: "Write", Revision: 1,
	Features: []spec.Feature{{Name: "TB", Title: "Turbo", Bit: 0, Conformance: op(spec.ConfOptional)}},
	Attributes: []spec.Attribute{
		rw(0, "Level", spec.Type{Kind: spec.KindUint, Bits: 8}, spec.Quality{}, spec.Constraint{Text: "1 to 200", Min: &spec.Bound{Int: 1}, Max: &spec.Bound{Int: 200}}),
		rw(1, "Hours", spec.Type{Kind: spec.KindUint, Bits: 24}, spec.Quality{Nullable: true}, spec.Constraint{}),
		rw(2, "Offset", spec.Type{Kind: spec.KindInt, Bits: 16}, spec.Quality{Nullable: true}, spec.Constraint{Text: "min -27315", Min: &spec.Bound{Int: -27315}}),
		rw(3, "Mode", spec.Type{Kind: spec.KindEnum, Bits: 8, Enum: modeEnum}, spec.Quality{}, spec.Constraint{}),
		rw(4, "Flags", spec.Type{Kind: spec.KindBitmap, Bits: 8, Bitmap: flagsBitmap}, spec.Quality{}, spec.Constraint{}),
		rw(5, "Scale", spec.Type{Kind: spec.KindFloat32}, spec.Quality{}, spec.Constraint{Text: "minScale to maxScale", Min: &spec.Bound{Ref: "minScale"}, Max: &spec.Bound{Ref: "maxScale"}}),
		rw(6, "On", spec.Type{Kind: spec.KindBool}, spec.Quality{}, spec.Constraint{}),
		rw(7, "Label", spec.Type{Kind: spec.KindString}, spec.Quality{}, spec.Constraint{Text: "max 4", Max: &spec.Bound{Int: 4}}),
		rw(8, "Key", spec.Type{Kind: spec.KindBytes}, spec.Quality{}, spec.Constraint{Text: "2", Value: &spec.Bound{Int: 2}}),
		rw(9, "List", spec.Type{Kind: spec.KindList}, spec.Quality{}, spec.Constraint{}),
		rw(10, "Choice", spec.Type{Kind: spec.KindUint, Bits: 8}, spec.Quality{}, spec.Constraint{Text: "0, 5 to 6", Parts: []spec.Constraint{
			{Value: &spec.Bound{Int: 0}}, {Min: &spec.Bound{Int: 5}, Max: &spec.Bound{Int: 6}},
		}}),
		rw(11, "Computed", spec.Type{Kind: spec.KindUint, Bits: 8}, spec.Quality{}, spec.Constraint{Text: "max x - 1", Max: &spec.Bound{Expr: "x - 1"}}),
		{ID: 12, Name: "MinScale", Type: spec.Type{Kind: spec.KindFloat32}, Conformance: op(spec.ConfMandatory), Access: spec.Access{RW: "R"}},
		{ID: 13, Name: "MaxScale", Type: spec.Type{Kind: spec.KindFloat32}, Conformance: op(spec.ConfMandatory), Access: spec.Access{RW: "R"}},
		rw(14, "Fixed", spec.Type{Kind: spec.KindUint, Bits: 8}, spec.Quality{Fixed: true}, spec.Constraint{}),
		rw(15, "Wide", spec.Type{Kind: spec.KindUint, Bits: 64}, spec.Quality{}, spec.Constraint{}),
		rw(16, "Signed", spec.Type{Kind: spec.KindInt, Bits: 64}, spec.Quality{}, spec.Constraint{}),
		{ID: 17, Name: "Unserved", Conformance: op(spec.ConfOptional), Access: spec.Access{RW: "RW"}},
		rw(18, "Bounded", spec.Type{Kind: spec.KindUint, Bits: 8}, spec.Quality{}, spec.Constraint{Text: "max unknownPeer", Max: &spec.Bound{Ref: "unknownPeer"}}),
	},
}

func statusOf(err error) im.StatusCode {
	var sce im.StatusCodeError
	if errors.As(err, &sce) {
		return sce.MatterStatusCode()
	}
	return im.StatusSuccess
}

func TestValidateWrite(t *testing.T) {
	t.Parallel()
	inst, err := spec.New(writeCluster, spec.Options{})
	if err != nil {
		t.Fatal(err)
	}
	peers := func(id uint32) (any, bool) {
		switch id {
		case 12:
			return float32(0.5), true
		case 13:
			return float32(2), true
		}
		return nil, false
	}
	ok := []struct {
		attr  uint32
		value any
		want  any
	}{
		{0, uint8(1), uint64(1)},
		{0, int64(200), uint64(200)},
		{1, nil, nil},
		{1, uint32(0xFFFFFE), uint64(0xFFFFFE)},
		{2, int16(-27315), int64(-27315)},
		{2, uint8(5), int64(5)},
		{3, uint64(0), uint64(0)},
		{4, uint8(0b111), uint64(0b111)},
		{5, float32(1.5), float64(1.5)},
		{5, int64(1), float64(1)},
		{6, true, true},
		{7, "abcd", "abcd"},
		{7, "äöü", "äöü"},
		{8, []byte{1, 2}, []byte{1, 2}},
		{9, []any{1}, []any{1}},
		{10, uint8(0), uint64(0)},
		{10, uint8(6), uint64(6)},
		{11, uint8(250), uint64(250)},
		{15, uint64(math.MaxUint64), uint64(math.MaxUint64)},
		{16, int64(math.MinInt64), int64(math.MinInt64)},
		{18, uint8(9), uint64(9)},
		{0, uint(3), uint64(3)},
		{2, int32(1), int64(1)},
		{2, int(1), int64(1)},
		{2, uint16(1), int64(1)},
		{0, int8(4), uint64(4)},
	}
	for _, tc := range ok {
		got, err := inst.ValidateWrite(tc.attr, tc.value, peers)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("attr %d = %v (%T): (%v %T, %v), want %v", tc.attr, tc.value, tc.value, got, got, err, tc.want)
		}
	}
	bad := []struct {
		attr   uint32
		value  any
		status im.StatusCode
	}{
		{99, uint8(1), im.StatusUnsupportedAttribute},
		{17, uint8(1), im.StatusUnsupportedAttribute},
		{12, float32(1), im.StatusUnsupportedWrite},
		{14, uint8(1), im.StatusUnsupportedWrite},
		{0, nil, im.StatusConstraintError},
		{0, uint8(0), im.StatusConstraintError},
		{0, uint16(256), im.StatusConstraintError},
		{0, int64(-1), im.StatusConstraintError},
		{0, "1", im.StatusConstraintError},
		{1, uint32(0xFFFFFF), im.StatusConstraintError},
		{2, int64(-32768), im.StatusConstraintError},
		{2, int64(-27316), im.StatusConstraintError},
		{2, uint64(math.MaxUint64), im.StatusConstraintError},
		{2, "x", im.StatusConstraintError},
		{3, uint8(1), im.StatusConstraintError}, // Turbo needs TB
		{3, uint8(7), im.StatusConstraintError},
		{4, uint8(0b1000), im.StatusConstraintError},
		{5, float32(3), im.StatusConstraintError},
		{5, math.NaN(), im.StatusConstraintError},
		{5, "x", im.StatusConstraintError},
		{6, uint8(1), im.StatusConstraintError},
		{7, "abcde", im.StatusConstraintError},
		{7, 5, im.StatusConstraintError},
		{8, []byte{1}, im.StatusConstraintError},
		{8, "ab", im.StatusConstraintError},
		{10, uint8(3), im.StatusConstraintError},
	}
	for _, tc := range bad {
		if _, err := inst.ValidateWrite(tc.attr, tc.value, peers); statusOf(err) != tc.status {
			t.Errorf("attr %d = %v: %v, want status 0x%02X", tc.attr, tc.value, err, uint8(tc.status))
		}
	}
	// Without peers, a reference bound admits every value.
	if _, err := inst.ValidateWrite(5, float32(9), nil); err != nil {
		t.Errorf("unresolved reference: %v", err)
	}
	turbo, err := spec.New(writeCluster, spec.Options{Features: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := turbo.ValidateWrite(3, uint8(1), nil); err != nil {
		t.Errorf("Turbo with TB: %v", err)
	}
	if e := (&spec.StatusError{Status: im.StatusFailure, Msg: "m"}); e.Error() != "m" || e.MatterStatusCode() != im.StatusFailure {
		t.Error("StatusError")
	}
}

func TestRanges(t *testing.T) {
	t.Parallel()
	if spec.UintMax(8, false) != 255 || spec.UintMax(8, true) != 254 || spec.UintMax(0, false) != math.MaxUint64 {
		t.Error("UintMax")
	}
	if lo, hi := spec.IntRange(8, false); lo != -128 || hi != 127 {
		t.Error("IntRange")
	}
	if lo, _ := spec.IntRange(16, true); lo != -32767 {
		t.Error("nullable IntRange")
	}
	if lo, hi := spec.IntRange(99, false); lo != math.MinInt64 || hi != math.MaxInt64 {
		t.Error("IntRange 64")
	}
	if spec.StringLength("a😀") != 3 {
		t.Error("StringLength counts UTF-16 code units")
	}
}
