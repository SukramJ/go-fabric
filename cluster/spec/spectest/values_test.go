// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spectest_test

import (
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	"thingfabric.dev/go-fabric/cluster/spec/spectest"
)

type (
	feature uint32
	enum8   uint8
	map8    uint8
)

func TestCheckFeatures(t *testing.T) {
	t.Parallel()
	// Sparse bits: the constant is the definition's bit, not the position.
	def := &spec.Cluster{Name: "Sparse", Features: []spec.Feature{{Name: "A", Bit: 0}, {Name: "B", Bit: 4}}}
	if errs := run(t, func(r *recorder) { spectest.CheckFeatures(r, def, feature(1<<0), feature(1<<4)) }); len(errs) != 0 {
		t.Errorf("matching constants reported: %q", errs)
	}
	wantReport(t, "position as bit", run(t, func(r *recorder) { spectest.CheckFeatures(r, def, feature(1<<0), feature(1<<1)) }), "feature B constant 0x2, definition bit 4")
	wantReport(t, "missing constant", run(t, func(r *recorder) { spectest.CheckFeatures(r, def, feature(1<<0)) }), "1 Feature constants, definition 2 features")
	if errs := run(t, func(r *recorder) { spectest.CheckFeatures[feature](r, &spec.Cluster{Name: "None"}) }); len(errs) != 0 {
		t.Errorf("a cluster without features reported: %q", errs)
	}
}

func TestCheckEnum(t *testing.T) {
	t.Parallel()
	def := &spec.Enum{Name: "E", Bits: 8, Values: []spec.EnumValue{{Value: 0, Name: "Zero"}, {Value: 2, Name: "Two"}}}
	if errs := run(t, func(r *recorder) { spectest.CheckEnum(r, def, enum8(0), enum8(2)) }); len(errs) != 0 {
		t.Errorf("matching constants reported: %q", errs)
	}
	wantReport(t, "wrong value", run(t, func(r *recorder) { spectest.CheckEnum(r, def, enum8(0), enum8(1)) }), "Two constant 1, descriptor 2")
	wantReport(t, "count", run(t, func(r *recorder) { spectest.CheckEnum(r, def, enum8(0)) }), "1 constants, descriptor 2 values")
	wide := &spec.Enum{Name: "W", Bits: 8, Values: []spec.EnumValue{{Value: 256, Name: "Big"}}}
	wantReport(t, "too wide", run(t, func(r *recorder) { spectest.CheckEnum(r, wide, uint16(256)) }), "does not fit 8 bits")
}

func TestCheckBitmap(t *testing.T) {
	t.Parallel()
	def := &spec.Bitmap{Name: "B", Bits: 8, Members: []spec.BitmapMember{{Name: "Flag", Bit: 0, Width: 1}, {Name: "Range", Bit: 2, Width: 3}}}
	if errs := run(t, func(r *recorder) { spectest.CheckBitmap(r, def, map8(1<<0), map8(0x1C)) }); len(errs) != 0 {
		t.Errorf("matching constants reported: %q", errs)
	}
	wantReport(t, "range as one bit", run(t, func(r *recorder) { spectest.CheckBitmap(r, def, map8(1<<0), map8(1<<2)) }), "Range constant 0x4, descriptor mask 0x1C")
	wantReport(t, "count", run(t, func(r *recorder) { spectest.CheckBitmap(r, def, map8(1)) }), "1 constants, descriptor 2 members")
	wide := &spec.Bitmap{Name: "W", Bits: 8, Members: []spec.BitmapMember{{Name: "High", Bit: 8, Width: 1}}}
	wantReport(t, "too wide", run(t, func(r *recorder) { spectest.CheckBitmap(r, wide, uint16(1<<8)) }), "does not fit 8 bits")
}
