// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package closure_test

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/closure"
	"github.com/SukramJ/go-fabric/cluster/spec"
	cd "github.com/SukramJ/go-fabric/cluster/spec/closuredimension"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/internal/paritytest"
	"github.com/SukramJ/go-fabric/schema"
)

// TestParityMatterJS_DimensionServer builds every FeatureMap the
// definition admits and holds each server against the matter.js
// conformance of closure-dimension.element.ts (spectest.CheckServer); every
// FeatureMap it refuses, the server refuses too.
func TestParityMatterJS_DimensionServer(t *testing.T) {
	t.Parallel()
	built := 0
	for features := range closure.DimensionFeature(1 << 8) {
		srv, err := closure.NewDimension(closure.DimensionConfig{
			Features: features, Handler: &panel{}, Resolution: 1, StepValue: 1,
			LimitRange: closure.LimitRange{Max: closure.Percent100thsMax},
		})
		if spec.CheckFeatures(cd.Definition, uint32(features)) != nil {
			if err == nil {
				t.Errorf("FeatureMap 0x%02X: accepted, the definition refuses it", features)
			}
			continue
		}
		if err != nil {
			t.Fatalf("FeatureMap 0x%02X: %v", features, err)
		}
		spectest.CheckServer(t, srv, cd.Definition, uint32(features))
		built++
	}
	if built == 0 {
		t.Fatal("no FeatureMap was admitted")
	}
}

// TestParityMatterJS_DimensionSnapshot pins the snapshot facts the server
// rests on: revision, the timed commands, CurrentState's quality Q, the
// Position constraint, and ClosurePanel mandating the cluster.
func TestParityMatterJS_DimensionSnapshot(t *testing.T) {
	t.Parallel()
	spectest.CheckDefinition(t, cd.Definition)
	js := paritytest.ClusterSnapshot(t, closure.ClusterIDClosureDimension)
	if js.Revision != cd.Revision {
		t.Errorf("revision %d", js.Revision)
	}
	for _, name := range []string{"SetTarget", "Step"} {
		c := js.Command(t, name)
		if c.Response != "status" || c.Effective == nil || c.Effective.Access == nil || !c.Effective.Access.Timed || c.Effective.Access.WritePriv != "O" {
			t.Errorf("%s %+v", name, c)
		}
	}
	if a := js.Attribute(t, "CurrentState"); a.Quality != "X Q" {
		t.Errorf("CurrentState quality %q", a.Quality)
	}
	if c := cd.Definition.Command(cd.CmdSetTarget, spec.Request).Fields[0].Constraint.Text; c != "max 10000" {
		t.Errorf("SetTarget Position constraint %q", c)
	}
	allowed, known := schema.DeviceTypeAllowsServerCluster(uint32(closure.DeviceTypeClosurePanel), closure.ClusterIDClosureDimension)
	if !allowed || !known {
		t.Error("ClosurePanel does not offer ClosureDimension")
	}
	if !errors.Is(spec.CheckFeatures(cd.Definition, uint32(closure.DimensionFeaturePositioning)), spec.ErrFeatureSelection) {
		t.Error("PS alone passes the definition's check; the [PS].b rule moved")
	}
}

// TestParityMatterJS_DimensionPayloads round-trips the command payloads
// and the struct attributes through the generated codecs.
func TestParityMatterJS_DimensionPayloads(t *testing.T) {
	t.Parallel()
	speed := cd.ThreeLevelAutoHigh
	spectest.RoundTripCommand(t, cd.Definition, cd.CmdSetTarget, spec.Request, cd.SetTargetRequest{Position: spectest.Ptr[uint16](5000), Latch: spectest.Ptr(false), Speed: &speed})
	spectest.RoundTripCommand(t, cd.Definition, cd.CmdStep, spec.Request, cd.StepRequest{Direction: cd.StepDirectionIncrease, NumberOfSteps: 2})
	spectest.RoundTripAttribute(t, cd.Definition, cd.AttrLimitRange, closure.LimitRange{Min: 100, Max: 9000})
	spectest.RoundTrip(t, closure.UnitRange{Min: 0, Max: 10000}, spec.DecodeStruct[closure.UnitRange])
	spectest.RoundTrip(t, closure.DimensionState{Position: &spec.Nullable[uint16]{Value: 100}, Latch: &spec.Nullable[bool]{Null: true}, Speed: &speed}, spec.DecodeStruct[closure.DimensionState])
}
