// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"context"
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	eem "thingfabric.dev/go-fabric/cluster/spec/electricalenergymeasurement"
	tm "thingfabric.dev/go-fabric/cluster/spec/temperaturemeasurement"
)

// TestSetAttributesJudgesBoundsAgainstItsOwnPatch holds a host's
// SetAttributes to the state its own patch makes: a value whose bound names
// a sibling ("minMeasuredValue to maxMeasuredValue") is judged against the
// sibling set in the same call, not the stored one. Before, a coherent new
// range was refused because MeasuredValue was judged against the old range
// (review II, R2-P6-fsm-1303), while NewServer judged the same values with
// every sibling in place.
func TestSetAttributesJudgesBoundsAgainstItsOwnPatch(t *testing.T) {
	t.Parallel()
	newSrv := func(t *testing.T) *spec.Server {
		t.Helper()
		s, err := spec.NewServer(tm.Definition, spec.Options{}, spec.ServerConfig{Initial: map[uint32]any{
			tm.AttrMinMeasuredValue: int16(0), tm.AttrMaxMeasuredValue: int16(50), tm.AttrMeasuredValue: int16(20),
		}})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	// A coherent new state the stored range would refuse.
	s := newSrv(t)
	if err := s.SetAttributes(map[uint32]any{tm.AttrMinMeasuredValue: int16(100), tm.AttrMaxMeasuredValue: int16(200), tm.AttrMeasuredValue: int16(150)}); err != nil {
		t.Errorf("SetAttributes{min 100, max 200, measured 150} over {0, 50, 20}: %v, want nil", err)
	}
	if v, _ := s.Value(tm.AttrMeasuredValue); v != int16(150) {
		t.Errorf("MeasuredValue = %v, want 150", v)
	}

	// An incoherent new state the stored range would admit: judged as
	// NewServer judges it, against the patch's own siblings.
	s = newSrv(t)
	err := s.SetAttributes(map[uint32]any{tm.AttrMinMeasuredValue: int16(100), tm.AttrMaxMeasuredValue: int16(200), tm.AttrMeasuredValue: int16(20)})
	if !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("SetAttributes{min 100, max 200, measured 20}: %v, want ErrInvalidValue", err)
	}
	if v, _ := s.Value(tm.AttrMinMeasuredValue); v != int16(0) {
		t.Errorf("a refused patch stored MinMeasuredValue %v, want the old 0", v)
	}
	if _, err := spec.NewServer(tm.Definition, spec.Options{}, spec.ServerConfig{Initial: map[uint32]any{
		tm.AttrMinMeasuredValue: int16(100), tm.AttrMaxMeasuredValue: int16(200), tm.AttrMeasuredValue: int16(20),
	}}); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("the same state as Initial: %v, want ErrInvalidValue", err)
	}

	// A single Set is still judged against the stored siblings.
	s = newSrv(t)
	if err := s.Set(tm.AttrMeasuredValue, int16(51)); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("Set(MeasuredValue 51) in {0, 50}: %v, want ErrInvalidValue", err)
	}
}

// TestIntegerBoundsCompareExactly holds a 64-bit value to a bound beyond
// 2^53 exactly: "0 to 2^62" on ElectricalEnergyMeasurement's served
// CumulativeEnergyImported admits 2^62 and refuses 2^62 + 1. Before, the
// bound was compared as float64 and every value up to 2^62 + 512 passed
// (review II, R2-P6-par-1306).
func TestIntegerBoundsCompareExactly(t *testing.T) {
	t.Parallel()
	s, err := spec.NewServer(eem.Definition, spec.Options{Features: uint32(eem.FeatureImportedEnergy | eem.FeatureCumulativeEnergy)}, spec.ServerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		energy int64
		ok     bool
	}{
		{0, true},
		{1 << 62, true},
		{1<<62 - 1, true},
		{1<<62 + 1, false},
		{1<<62 + 512, false},
		{1<<62 + 513, false},
		{-1, false},
	} {
		err := s.Set(eem.AttrCumulativeEnergyImported, eem.EnergyMeasurementStruct{Energy: c.energy})
		if (err == nil) != c.ok {
			t.Errorf("Energy %d against \"0 to 2^62\": %v, want accepted %v", c.energy, err, c.ok)
		}
	}
}

// conformanceProbeStruct is the Go form of conformanceProbeDefinition's
// struct attribute: B is optional, its conformance the feature LT.
type conformanceProbeStruct struct {
	A uint8
	B *uint8
}

func conformanceProbeDefinition() *spec.Cluster {
	rw := spec.Access{RW: "RW", Read: spec.PrivilegeView, Write: spec.PrivilegeOperate}
	u8 := spec.Type{Name: "uint8", Kind: spec.KindUint, Bits: 8}
	return &spec.Cluster{
		ID: 0xFFF1FC01, Name: "Probe", Revision: 1,
		Features: []spec.Feature{{Name: "LT", Title: "Lift", Bit: 0, Conformance: spec.Conformance{Text: "O", Op: spec.ConfOptional}}},
		Attributes: []spec.Attribute{
			{ID: 0, Name: "A", Type: u8, Conformance: spec.Conformance{Text: "M", Op: spec.ConfMandatory}, Access: rw},
			{ID: 2, Name: "C", Type: spec.Type{Name: "ProbeStruct", Kind: spec.KindStruct, Struct: &spec.Struct{Name: "ProbeStruct", Fields: []spec.Field{
				{ID: 0, Name: "A", Type: u8, Conformance: spec.Conformance{Text: "M", Op: spec.ConfMandatory}},
				{ID: 1, Name: "B", Type: u8, Conformance: spec.Conformance{Text: "LT", Op: spec.ConfName, Name: "LT"}},
			}}}, Conformance: spec.Conformance{Text: "M", Op: spec.ConfMandatory}, Access: rw},
		},
	}
}

// TestHostValuesAreHeldToFieldConformance holds a host's Set and a
// server's Initial values to their struct fields' conformance, as a
// controller's write is: a field the feature selection disallows is
// refused on every path into the state. Before, the controller's write was
// refused and the host's Set and Initial stored, served and reported the
// field (review II, R2-P6-par-1304). matter.js runs every field's
// validator on each validate of a struct
// (packages/node/src/behavior/state/validation/ValueValidator.ts:406-441) and
// the assignment calls it unless acceptInvalid (StructManager.ts:278-291).
func TestHostValuesAreHeldToFieldConformance(t *testing.T) {
	t.Parallel()
	two := uint8(2)
	disallowed := conformanceProbeStruct{A: 1, B: &two}
	allowed := conformanceProbeStruct{A: 1}

	s, err := spec.NewServer(conformanceProbeDefinition(), spec.Options{}, spec.ServerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MatterWrite(context.Background(), 2, disallowed); !errors.Is(err, spec.ErrConformance) {
		t.Errorf("controller write of C{A:1, B:2} with LT off: %v, want ErrConformance", err)
	}
	if err := s.Set(2, disallowed); !errors.Is(err, spec.ErrConformance) || !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("host Set of C{A:1, B:2} with LT off: %v, want ErrInvalidValue and ErrConformance", err)
	}
	if err := s.Set(2, allowed); err != nil {
		t.Errorf("host Set of C{A:1} with LT off: %v, want nil", err)
	}
	if _, err := spec.NewServer(conformanceProbeDefinition(), spec.Options{}, spec.ServerConfig{Initial: map[uint32]any{2: disallowed}}); !errors.Is(err, spec.ErrConformance) {
		t.Errorf("Initial C{A:1, B:2} with LT off: %v, want ErrConformance", err)
	}

	// With LT selected the field is mandatory on both paths.
	s, err = spec.NewServer(conformanceProbeDefinition(), spec.Options{Features: 1}, spec.ServerConfig{Initial: map[uint32]any{2: disallowed}})
	if err != nil {
		t.Fatalf("Initial C{A:1, B:2} with LT on: %v", err)
	}
	if err := s.Set(2, allowed); !errors.Is(err, spec.ErrConformance) {
		t.Errorf("host Set of C{A:1} with LT on: %v, want ErrConformance (B is mandatory)", err)
	}
	if err := s.Set(2, disallowed); err != nil {
		t.Errorf("host Set of C{A:1, B:2} with LT on: %v, want nil", err)
	}
}
