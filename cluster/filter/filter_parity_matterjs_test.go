// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package filter_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/filter"
	"github.com/SukramJ/go-fabric/cluster/spec"
	carbon "github.com/SukramJ/go-fabric/cluster/spec/activatedcarbonfiltermonitoring"
	hepa "github.com/SukramJ/go-fabric/cluster/spec/hepafiltermonitoring"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/internal/paritytest"
	"github.com/SukramJ/go-fabric/schema"
)

// TestParityMatterJS_FilterServers builds every feature selection and
// optional set of both derivations and holds each server against the
// matter.js conformance of its definition: FeatureMap, ClusterRevision,
// the attribute, command and event lists, the privileges and the
// read-only writes (spectest.CheckServer).
func TestParityMatterJS_FilterServers(t *testing.T) {
	t.Parallel()
	for _, d := range []struct {
		def *spec.Cluster
		new func(filter.Config) (*filter.Server, error)
	}{
		{hepa.Definition, filter.NewHepaFilterMonitoring},
		{carbon.Definition, filter.NewActivatedCarbonFilterMonitoring},
	} {
		for features := range filter.Feature(1 << 3) {
			for optional := range filter.Optional(1 << 3) {
				srv, err := d.new(filter.Config{Features: features, Optional: optional, Resetter: &host{}})
				if err != nil {
					t.Fatalf("%s 0b%03b 0b%03b: %v", d.def.Name, features, optional, err)
				}
				spectest.CheckServer(t, srv, d.def, uint32(features))
			}
		}
	}
}

// TestParityMatterJS_FilterSnapshot pins the snapshot facts the server's
// own rules rest on: both derivations inherit ResourceMonitoring
// unchanged, ChangeIndication's Warning value hangs on WRN, the product
// list holds at most five entries of at most 20 characters, ResetCondition
// is answered with a status, and the AirPurifier and ExtractorHood device
// types offer both clusters.
func TestParityMatterJS_FilterSnapshot(t *testing.T) {
	t.Parallel()
	for _, id := range []uint32{filter.ClusterIDHepaFilterMonitoring, filter.ClusterIDActivatedCarbonFilterMonitoring} {
		js := paritytest.ClusterSnapshot(t, id)
		if js.Base != "ResourceMonitoring" {
			t.Errorf("0x%04X derives from %q", id, js.Base)
		}
		if c := js.Command(t, "ResetCondition"); c.Response != "status" || c.Conformance != "O" {
			t.Errorf("ResetCondition %+v", c)
		}
		if a := js.Attribute(t, "ReplacementProductList"); a.Constraint != "max 5" || a.Conformance != "REP" {
			t.Errorf("ReplacementProductList %+v", a)
		}
		if a := js.Attribute(t, "LastChangedTime"); a.Access != "RW VO" || a.Quality != "X N" {
			t.Errorf("LastChangedTime %+v", a)
		}
		for _, dt := range []uint32{0x002D, 0x007A} { // AirPurifier, ExtractorHood
			if allowed, known := schema.DeviceTypeAllowsServerCluster(dt, id); !allowed || !known {
				t.Errorf("device type 0x%04X does not offer 0x%04X", dt, id)
			}
		}
	}
	warning := hepa.ChangeIndicationEnumDef.Values[1]
	if warning.Name != "Warning" || warning.Conformance.Text != "WRN" {
		t.Errorf("Warning %+v", warning)
	}
	if c := hepa.ReplacementProductStructDef.Fields[1].Constraint; c.Text != "max 20" {
		t.Errorf("ProductIdentifierValue %q", c.Text)
	}
}
