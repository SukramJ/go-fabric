// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/PeerBehaviorTest.ts at
// 862ad75f: the shape the controller derives for a peer's cluster from what
// the peer reports (PeerBehavior.ts). matter.js builds a behavior class per
// discovered shape; the Go client analyses the same input into a
// client.Cluster (BD-Matter-ClientNode-NoGeneratedTypes), so the port feeds
// the shape's values to a cluster (shaped) and asserts its revision, its
// supported and extra attributes and its names where matter.js asserts the
// behavior type's cluster, State and attribute keys.
package client

import (
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
)

// discovered is a PeerBehavior.DiscoveredClusterShape as the peer reports
// it: the revision (0: none), FeatureMap and AttributeList.
func discovered(id uint32, revision, features uint64, attributes ...uint64) *Cluster {
	values := map[uint32]spec.Node{
		spec.AttrFeatureMap:           uintNode(features),
		spec.AttrAttributeList:        listNode(attributes...),
		spec.AttrAcceptedCommandList:  listNode(),
		spec.AttrGeneratedCommandList: listNode(),
	}
	if revision != 0 {
		values[spec.AttrClusterRevision] = uintNode(revision)
	}
	return shaped(id, values)
}

// globalIDs are the global attributes a shape's AttributeList names.
var globalIDs = []uint64{65528, 65529, 65531, 65532, 65533}

const (
	clNetworkCommissioning = 0x31
	ethernetFeature        = 4
	malformedMEI           = 0xfff10001
	unknownCluster         = 0xfff1fc01
	revGated               = 0xfff1fc02
	clLevelControl         = 0x8
	clPump                 = 0x200
	pumpAlarmMask          = 0x22

	clOccupancySensing          = 0x406
	occupancyPIRFeature         = 2
	occupancyUltrasonicDelay    = 0x20
	levelControlLightingFeature = 2
	levelControlRemainingTime   = 0x1
	clRvcCleanMode              = 0x55
	rvcCleanModeOnMode          = 0x3
)

// occupancyMandatoryAttributes and rvcCleanModeMandatoryAttributes are
// PeerBehaviorTest.ts OCCUPANCY_MANDATORY_ATTRIBUTES and
// RVC_CLEAN_MODE_MANDATORY_ATTRIBUTES.
var (
	occupancyMandatoryAttributes    = []uint64{0x0, 0x1, 0x2, 65528, 65529, 65531, 65532, 65533}
	rvcCleanModeMandatoryAttributes = []uint64{0x0, 0x1, 65528, 65529, 65531, 65532, 65533}
)

// pumpMandatoryAttributes is PeerBehaviorTest.ts PUMP_MANDATORY_ATTRIBUTES.
var pumpMandatoryAttributes = []uint64{0x0, 0x1, 0x2, 0x11, 0x12, 0x20, 0x21, 65528, 65529, 65531, 65532, 65533}

func TestPeerBehaviorDiscoveredParity(t *testing.T) {
	t.Parallel()

	t.Run("builds a cluster from a well-formed AttributeList", func(t *testing.T) {
		t.Parallel()
		c := discovered(clNetworkCommissioning, 1, ethernetFeature, 4, 65528, 65529, 65530, 65531, 65532, 65533)
		if !c.Ready() || !c.Supports(4) {
			t.Fatalf("ready %v, InterfaceEnabled supported %v", c.Ready(), c.Supports(4))
		}
	})

	t.Run("builds a cluster for an ID that is not a legal MEI", func(t *testing.T) {
		t.Parallel()
		c := discovered(malformedMEI, 1, 0, globalIDs...)
		if !c.Ready() || c.ID() != malformedMEI {
			t.Fatalf("ready %v, id %#x", c.Ready(), c.ID())
		}
	})

	t.Run("distinguishes shapes whose attribute IDs differ by the block size", func(t *testing.T) {
		t.Parallel()
		first, second := discovered(unknownCluster, 1, 0, 1), discovered(unknownCluster, 1, 0, 33)
		a1, _ := first.Extra()
		a2, _ := second.Extra()
		if !slices.Equal(a1, []uint32{1}) || !slices.Equal(a2, []uint32{33}) {
			t.Fatalf("extra attributes %v and %v", a1, a2)
		}
		if unknownName("attr", a1[0]) != "attr$1" || unknownName("attr", a2[0]) != "attr$21" {
			t.Fatalf("names %q and %q", unknownName("attr", a1[0]), unknownName("attr", a2[0]))
		}
	})

	t.Run("reports the revision the peer sent", func(t *testing.T) {
		t.Parallel()
		if r := discovered(clLevelControl, 5, 0, 0, 65528, 65529, 65531, 65532, 65533).Revision(); r != 5 {
			t.Fatalf("revision %d, want 5", r)
		}
	})

	t.Run("reports a revision the standard cluster does not yet define", func(t *testing.T) {
		t.Parallel()
		// matter.js's custom model states revision 3; the Go analysis has no
		// model injection and reports the peer's value either way.
		if r := discovered(revGated, 4, 0, 0xfffc, 0xfffd).Revision(); r != 4 {
			t.Fatalf("revision %d, want 4", r)
		}
	})

	t.Run("reports the revision an unknown cluster sent", func(t *testing.T) {
		t.Parallel()
		if r := discovered(unknownCluster, 4, 0, 3).Revision(); r != 4 {
			t.Fatalf("revision %d, want 4", r)
		}
	})

	t.Run("exposes a deprecated attribute the peer reports", func(t *testing.T) {
		t.Parallel()
		c := discovered(clPump, 5, 0, append(slices.Clone(pumpMandatoryAttributes), pumpAlarmMask)...)
		if !c.Supports(pumpAlarmMask) {
			t.Fatal("AlarmMask not supported though the peer lists it")
		}
	})

	// PeerBehaviorTest.ts:79-96: the shape stands under an empty
	// AttributeList; the model's attributes stay defined.
	t.Run("builds a cluster even when the peer reports an empty AttributeList", func(t *testing.T) {
		t.Parallel()
		c := discovered(clNetworkCommissioning, 1, ethernetFeature)
		if !c.Ready() || c.Definition() == nil || c.Definition().Attribute(4) == nil {
			t.Fatalf("ready %v, definition %v", c.Ready(), c.Definition() != nil)
		}
	})

	// :274-285: "[HoldTime & US], D" without the Ultrasonic feature.
	t.Run("omits an attribute the peer reports whose conformance before a trailing deprecation excludes it", func(t *testing.T) {
		t.Parallel()
		c := discovered(clOccupancySensing, 5, occupancyPIRFeature, append(slices.Clone(occupancyMandatoryAttributes), occupancyUltrasonicDelay)...)
		if c.Definition() == nil {
			t.Fatal("OccupancySensing has no definition")
		}
		if c.Supports(occupancyUltrasonicDelay) {
			t.Fatal("UltrasonicOccupiedToUnoccupiedDelay supported without the Ultrasonic feature")
		}
	})

	// :287-301: RemainingTime is "LT", mandatory with the Lighting feature.
	t.Run("keeps the conformance of a reported attribute that the peer's features make mandatory", func(t *testing.T) {
		t.Parallel()
		c := discovered(clLevelControl, 0, levelControlLightingFeature, 0x0, 0x1, 0xf, 0x11, 65528, 65529, 65531, 65532, 65533)
		if !c.Supports(levelControlRemainingTime) {
			t.Fatal("RemainingTime not supported")
		}
		if a := c.Definition().Attribute(levelControlRemainingTime); a == nil || a.Conformance.Applicability(spec.Context(c.Definition(), levelControlLightingFeature)) != spec.ApplicabilityMandatory {
			t.Fatal("RemainingTime is not mandatory under the Lighting feature")
		}
	})

	// :328-337: OnMode is "X" in RVC Clean Mode.
	t.Run("omits a disallowed attribute the peer reports", func(t *testing.T) {
		t.Parallel()
		c := discovered(clRvcCleanMode, 0, 0, append(slices.Clone(rvcCleanModeMandatoryAttributes), rvcCleanModeOnMode)...)
		if c.Definition() == nil {
			t.Fatal("RVC Clean Mode has no definition")
		}
		if c.Supports(rvcCleanModeOnMode) {
			t.Fatal("OnMode supported though disallowed")
		}
	})

	// :339-349: no revision reported — the model's; 1 for a cluster the
	// model does not know.
	t.Run("tolerates a peer that does not report a revision", func(t *testing.T) {
		t.Parallel()
		if r := discovered(unknownCluster, 0, 0, 2).Revision(); r != 1 {
			t.Fatalf("revision %d, want 1", r)
		}
	})

	t.Run("omits a deprecated attribute the peer does not report", func(t *testing.T) {
		t.Parallel()
		if discovered(clPump, 5, 0, pumpMandatoryAttributes...).Supports(pumpAlarmMask) {
			t.Fatal("AlarmMask supported though the peer does not list it")
		}
	})
}
