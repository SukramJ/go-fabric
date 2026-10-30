// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"math/bits"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/surface"
	"github.com/SukramJ/go-fabric/surface/surfacetest"
)

// TestSurfaceProfile runs a synthetic profile through the test seam: a
// Reproduced remove rule removes an optional OnOff feature and keeps the
// type-mandated one with the rewritten reason; a warn rule keeps an
// element with its warning row.
func TestSurfaceProfile(t *testing.T) {
	r, _ := OnOffLightType.Server(onOffID)
	base, bit := optionalFeature(t, r)
	mandated := uint32(1) << bits.TrailingZeros32(r.Features)
	feature := func(id uint32) surface.ElementPattern {
		return surface.ElementPattern{Kind: surface.KindFeature, Cluster: onOffID, ID: id}
	}
	prof := surface.Profile{Name: "synthetic", Rules: []surface.ProfileRule{
		{Element: feature(bit), Effect: surface.EffectRemove, Confidence: surface.Reproduced, Source: "src-opt", Reason: "optional"},
		{Element: feature(mandated), Effect: surface.EffectRemove, Confidence: surface.Reproduced, Source: "src-man", Reason: "mandated"},
		{Element: surface.ElementPattern{Kind: surface.KindDeviceType, ID: uint32(OnOffLightType.ID)}, Effect: surface.EffectWarn, Confidence: surface.Inherited, Source: "src-dt", Reason: "type"},
	}}
	policies := surfacetest.WithProfiles(surface.Policies{}, prof)
	opts := testOptions("light")
	opts.Surface = &policies
	d, err := New(&OnOffLightType, opts, withFeatures(onOffID, base|bit))
	if err != nil {
		t.Fatal(err)
	}
	if fm := featureMapOf(t, d); fm != base || fm&mandated == 0 {
		t.Fatalf("FeatureMap 0x%X, want 0x%X: without 0x%X, with the mandated 0x%X", fm, base, bit, mandated)
	}
	rep := d.Surface()
	if rep.Policies != policies.Digest() || !slices.Equal(rep.Profiles, []string{"synthetic"}) || rep.LevelOrigin != "pin" {
		t.Fatalf("report head %x %v %s", rep.Policies, rep.Profiles, rep.LevelOrigin)
	}
	want := map[surface.Kind]map[uint32]struct {
		verdict surface.Verdict
		reason  string
	}{
		surface.KindFeature: {
			bit:      {surface.Remove, "removed by profile synthetic: optional (src-opt)"},
			mandated: {surface.Keep, "kept: mandatory at pin (removed by profile synthetic: mandated (src-man))"},
		},
		surface.KindDeviceType: {
			uint32(OnOffLightType.ID): {surface.Keep, "warning (Inherited) profile synthetic: type (src-dt)"},
		},
	}
	n := 0
	for _, dec := range rep.Decisions {
		if dec.Policy != "profile:synthetic" {
			continue
		}
		w, ok := want[dec.Kind][dec.ID]
		if !ok || dec.Verdict != w.verdict || dec.Reason != w.reason {
			t.Errorf("decision %+v", dec)
		}
		n++
	}
	if n != 3 {
		t.Fatalf("%d profile decisions, want 3: %+v", n, rep.Decisions)
	}
}
