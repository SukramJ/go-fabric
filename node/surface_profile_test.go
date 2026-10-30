// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/surface"
	"github.com/SukramJ/go-fabric/surface/surfacetest"
)

func TestNodeRejectsAnUnknownProfile(t *testing.T) {
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "profile.db")))
	cfg.Surface = surface.Policies{Profiles: []string{"nope"}}
	if _, err := node.New(cfg); !errors.Is(err, surface.ErrUnknownProfile) {
		t.Fatalf("node.New with an unknown profile: %v", err)
	}
}

// TestNodeProfile: a synthetic profile through the test seam. The node's
// report names it and carries the device's profile decisions — the
// type-mandated OnOff feature kept with the rewritten reason, the warning
// on the device type — and a device composed without the profile is
// refused.
func TestNodeProfile(t *testing.T) {
	ctx := context.Background()
	r, _ := device.OnOffLightType.Server(onoffdef.ClusterID)
	mandated := r.Features & -r.Features
	prof := surface.Profile{Name: "synthetic", Rules: []surface.ProfileRule{
		{Element: surface.ElementPattern{Kind: surface.KindFeature, Cluster: onoffdef.ClusterID, ID: mandated}, Effect: surface.EffectRemove, Confidence: surface.Reproduced, Source: "s1", Reason: "r1"},
		{Element: surface.ElementPattern{Kind: surface.KindDeviceType, ID: uint32(device.OnOffLightType.ID)}, Effect: surface.EffectWarn, Confidence: surface.Reported, Source: "s2", Reason: "r2"},
	}}
	p := surfacetest.WithProfiles(surface.Policies{}, prof)
	n := surfaceNode(t, p)
	if err := n.Aggregator().Add(ctx, light(t, "plain", nil)); !errors.Is(err, node.ErrPolicyMismatch) {
		t.Fatalf("Add of a device composed without the profile: %v", err)
	}
	if err := n.Aggregator().Add(ctx, light(t, "lamp", &p)); err != nil {
		t.Fatal(err)
	}
	rep := n.Surface()
	if rep.Policies != p.Digest() || !slices.Equal(rep.Profiles, []string{"synthetic"}) || rep.LevelOrigin != "pin" {
		t.Fatalf("report head %v %s", rep.Profiles, rep.LevelOrigin)
	}
	got := map[surface.Kind]string{}
	for _, d := range rep.Decisions {
		if d.Policy == "profile:synthetic" && d.Endpoint == "lamp" && (d.Verdict == surface.Keep || d.Verdict == surface.Warn) {
			got[d.Kind] = d.Reason
		}
	}
	if got[surface.KindFeature] != "kept: mandatory at pin (removed by profile synthetic: r1 (s1))" ||
		got[surface.KindDeviceType] != "warning (Reported) profile synthetic: r2 (s2)" {
		t.Fatalf("profile decisions %v", got)
	}
}
