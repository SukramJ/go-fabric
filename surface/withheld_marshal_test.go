// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package surface_test

import (
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/schema"
	"thingfabric.dev/go-fabric/surface"
)

// TestWithheldSynthesisedClusterMarshals holds every report Evaluate
// returns to its JSON form: a feature candidate whose cluster candidate is
// absent withholds the endpoint with a cluster element Evaluate makes up,
// and that element carries the features' provenance, so the withheld
// report marshals. Before, the made-up element had Provenance 0 and
// Report.MarshalJSON failed with "no name for surface.Provenance 0"
// (review II, R2-P6-err-1361). Every level below the newest and every
// cluster feature the level does not know is tried.
func TestWithheldSynthesisedClusterMarshals(t *testing.T) {
	t.Parallel()
	levels := schema.Levels()
	found := 0
	for _, l := range levels[:len(levels)-1] {
		for cid := range uint32(0x0600) {
			if !schema.ClusterLevels(cid).Has(l) {
				continue
			}
			for _, f := range schema.ClusterFeatures(cid) {
				if schema.ClusterFeatureLevels(cid, f.Bit).Has(l) {
					continue
				}
				el := surface.Element{Endpoint: "ep", Cluster: cid, Kind: surface.KindFeature, ID: uint32(1) << f.Bit, Provenance: surface.ProvenanceHostDeclared}
				el.Levels = surface.ElementLevels(el)
				if el.Levels == 0 {
					continue
				}
				_, _, err := surface.Evaluate([]surface.Element{el}, surface.Policies{Level: l})
				var we *surface.WithheldError
				if !errors.As(err, &we) {
					continue
				}
				found++
				if _, err := we.Report.MarshalJSON(); err != nil {
					t.Errorf("level %s cluster 0x%04X feature %s: withheld %v; Report.MarshalJSON = %v", l, cid, f.Name, we.Elements, err)
				}
				for _, w := range we.Elements {
					if w.Kind == surface.KindCluster && w.Provenance != surface.ProvenanceHostDeclared {
						t.Errorf("level %s cluster 0x%04X: the made-up cluster element has provenance %v, want the feature's", l, cid, w.Provenance)
					}
				}
			}
		}
	}
	if found == 0 {
		t.Fatal("no feature candidate withheld its endpoint: the case this test holds no longer occurs")
	}
}
