// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	tsdef "github.com/SukramJ/go-fabric/cluster/spec/timesynchronization"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/surface"
)

// Config.TimeSynchronization serves the features on the root over the
// wire: SetTimeZone answers SetTimeZoneResponse, SetTrustedTimeSource
// stores the source with the accessing fabric, a list beyond its
// capacity is refused, and the AttributeList carries the features'
// attributes (TimeSynchronizationCluster.cpp at 6170af84).
func TestTimeSynchronizationFeaturesOnTheWire(t *testing.T) {
	site := fabrictest.NewSite(t, fabrictest.Options{})
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Configure: func(cfg *node.Config) {
		cfg.TimeSynchronization = &node.TimeSynchronizationConfig{Features: tsdef.FeatureTimeZone | tsdef.FeatureTimeSyncClient}
		// The features are ahead of matter.js (core.TimeSynchronizationAhead).
		cfg.Surface.Extensions = surface.NewExtensions("BD-chip-TimeSynchronization-Features")
	}}})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := func(id uint32) im.ConcreteCommandPath {
		return im.ConcreteCommandPath{Endpoint: 0, Cluster: tsdef.ClusterID, Command: id}
	}
	berlin := "Europe/Berlin"
	res, err := fabrictest.Invoke(ctx, pair, cmd(tsdef.CmdSetTimeZone), tsdef.SetTimeZoneRequest{TimeZone: []tsdef.TimeZoneStruct{{Offset: 3600, Name: &berlin}}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != im.StatusSuccess || !res.HasFields || res.Command != tsdef.CmdSetTimeZoneResponse {
		t.Fatalf("SetTimeZone answered %+v, want SetTimeZoneResponse", res)
	}
	h := uint64(time.Hour / time.Microsecond)
	res, err = fabrictest.Invoke(ctx, pair, cmd(tsdef.CmdSetTimeZone), tsdef.SetTimeZoneRequest{TimeZone: []tsdef.TimeZoneStruct{{}, {ValidAt: h}, {ValidAt: 2 * h}}})
	// The request's own constraint ("1 to 2", time-synchronization.element.ts)
	// refuses the list before the handler, CONSTRAINT_ERROR; chip's handler
	// answers RESOURCE_EXHAUSTED for it (:1168-1171), which the server's
	// own check keeps for a host-side caller.
	if res.Status != im.StatusConstraintError {
		t.Fatalf("three time zones answered %+v %v, want ConstraintError", res, err)
	}
	res, err = fabrictest.Invoke(ctx, pair, cmd(tsdef.CmdSetTrustedTimeSource), tsdef.SetTrustedTimeSourceRequest{
		TrustedTimeSource: spec.ValueOf(tsdef.FabricScopedTrustedTimeSourceStruct{NodeId: pair.Controller.Fabric().RootNodeID, Endpoint: 0}),
	})
	if err != nil || res.Status != im.StatusSuccess {
		t.Fatalf("SetTrustedTimeSource: %+v %v", res, err)
	}
	tts, ok := pair.Device.Node().Root().TimeSynchronization.TrustedTimeSource()
	if !ok || tts.FabricIndex != 1 || tts.NodeId != pair.Controller.Fabric().RootNodeID {
		t.Errorf("TrustedTimeSource %+v %v", tts, ok)
	}
	for _, id := range []uint32{tsdef.AttrTimeZone, tsdef.AttrDstOffset, tsdef.AttrLocalTime, tsdef.AttrTrustedTimeSource} {
		if _, err := fabrictest.Read(ctx, pair, im.ConcreteAttributePath{Endpoint: 0, Cluster: tsdef.ClusterID, Attribute: id}); err != nil {
			t.Errorf("read 0x%04X: %v", id, err)
		}
	}
}
