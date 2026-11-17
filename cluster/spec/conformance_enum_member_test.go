// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	pushav "github.com/SukramJ/go-fabric/cluster/spec/pushavstreamtransport"
	zm "github.com/SukramJ/go-fabric/cluster/spec/zonemanagement"
)

// TestConformanceNamesAnEnumMemberAsMatterJS holds a field conformance
// that compares an enum sibling with a member name to the member the name
// means, matched as matter.js matches it (the camelized name, so
// "TwoDCARTZone" is the member "TwoDcartZone" and "CMAF" the member
// "Cmaf"). Before, the member was matched by its exact name, the name did
// not resolve, and a correct value was refused or a correct
// AllocatePushTransport logged as a misfit (Fable read of phase 6a, unit
// spec, class 2 and 3 of R2-P6-par-1304).
func TestConformanceNamesAnEnumMemberAsMatterJS(t *testing.T) {
	t.Parallel()
	zones, err := spec.New(zm.Definition, spec.Options{Features: uint32(zm.FeatureTwoDimensionalCartesianZone)})
	if err != nil {
		t.Fatal(err)
	}
	cartesian := zm.TwoDCartesianZoneStruct{Name: "z", Use: zm.ZoneUseMotion, Vertices: []zm.TwoDCartesianVertexStruct{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 0, Y: 10}}}
	zone := zm.ZoneInformationStruct{ZoneId: 1, ZoneType: zm.ZoneTypeTwoDcartZone, TwoDCartesianZone: &cartesian}
	if err := zones.CheckConformance(zm.AttrZones, []zm.ZoneInformationStruct{zone}, nil); err != nil {
		t.Errorf("a TwoDCART zone with its TwoDCartesianZone (\"TWODCART & ZoneType == TwoDCARTZone\"): %v, want nil", err)
	}
	zone.TwoDCartesianZone = nil
	if err := zones.CheckConformance(zm.AttrZones, []zm.ZoneInformationStruct{zone}, nil); !errors.Is(err, spec.ErrConformance) {
		t.Errorf("a TwoDCART zone without its TwoDCartesianZone: %v, want ErrConformance (the field is mandatory)", err)
	}

	rw := spec.Access{RW: "RW", Read: spec.PrivilegeView, Write: spec.PrivilegeOperate}
	probe, err := spec.New(&spec.Cluster{
		ID: 0xFFF1FC0C, Name: "ContainerProbe", Revision: 1,
		Attributes: []spec.Attribute{{
			ID: 0, Name: "Options", Type: spec.Type{Name: "ContainerOptionsStruct", Kind: spec.KindStruct, Struct: pushav.ContainerOptionsStructDef},
			Conformance: spec.Conformance{Text: "M", Op: spec.ConfMandatory}, Access: rw,
		}},
	}, spec.Options{})
	if err != nil {
		t.Fatal(err)
	}
	cmaf := pushav.ContainerOptionsStruct{ContainerType: pushav.ContainerFormatCmaf, CmafContainerOptions: &pushav.CMAFContainerOptionsStruct{}}
	if err := probe.CheckConformance(0, cmaf, nil); errors.Is(err, spec.ErrConformance) {
		t.Errorf("CMAF with its CmafContainerOptions (\"ContainerType == CMAF\"): %v, want no conformance refusal", err)
	}
	cmaf.CmafContainerOptions = nil
	if err := probe.CheckConformance(0, cmaf, nil); !errors.Is(err, spec.ErrConformance) {
		t.Errorf("CMAF without its CmafContainerOptions: %v, want ErrConformance (the field is mandatory)", err)
	}
}
