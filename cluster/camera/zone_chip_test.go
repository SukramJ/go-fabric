// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// ZoneManagement: a port of connectedhomeip
// src/app/clusters/zone-management-server/tests/TestZoneManagementCluster.cpp
// at 6170af84, by meaning — kept for the cases matter.js's
// ZoneManagementServerTest.ts at db91886c has no counterpart for
// (zone_parity_matterjs_test.go ports that file; its polygon table
// contains TestZonePolygon.cpp's and replaces it). chip's ClusterTester
// reads, writes and invokes the cluster directly; here the server's
// MatterRead, MatterWrite and MatterInvoke do, the MockDelegate (:38-145)
// is mockZones, chip's attribute storage is the host's (the change
// notification and ZonesConfig). TestZoneManagementClusterBackwardsCompatibility.cpp
// tests chip's own storage migration and is left out.
package camera_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/cluster/camera"
	zmdef "thingfabric.dev/go-fabric/cluster/spec/zonemanagement"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
)

// The fixture's limits (:31-35).
const (
	zoneMaxUserDefined = 5
	zoneMaxZones       = 8
	zoneSensitivityMax = 4
)

var zoneTwoDMax = camera.Vertex{X: 640, Y: 480}

// mockZones is chip's MockDelegate (:38-145): the calls counted, the
// zone IDs the server assigned kept (chip's delegate assigned them, from
// 1; the server does since pin lift b25ed960, decision 290), the
// triggers kept.
type mockZones struct {
	mu                         sync.Mutex
	ids                        []uint16
	created, updated, removed  int
	triggers                   []camera.ZoneTrigger
	createErr, createTriggerEr error
}

func (m *mockZones) CreateTwoDCartesianZone(_ context.Context, id uint16, _ camera.TwoDZone) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createErr != nil {
		return m.createErr
	}
	m.ids = append(m.ids, id)
	m.created++
	return nil
}

func (m *mockZones) UpdateTwoDCartesianZone(context.Context, uint16, camera.TwoDZone) error {
	m.mu.Lock()
	m.updated++
	m.mu.Unlock()
	return nil
}

func (m *mockZones) RemoveZone(context.Context, uint16) error {
	m.mu.Lock()
	m.removed++
	m.mu.Unlock()
	return nil
}

func (m *mockZones) CreateTrigger(_ context.Context, t camera.ZoneTrigger) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createTriggerEr != nil {
		return m.createTriggerEr
	}
	m.triggers = append(m.triggers, t)
	return nil
}

func (m *mockZones) UpdateTrigger(_ context.Context, t camera.ZoneTrigger) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.triggers {
		if m.triggers[i].ZoneId == t.ZoneId {
			m.triggers[i] = t
		}
	}
	return nil
}

func (m *mockZones) RemoveTrigger(_ context.Context, id uint16) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.triggers = slices.DeleteFunc(m.triggers, func(t camera.ZoneTrigger) bool { return t.ZoneId == id })
	return nil
}

// newChipZones is CreateCluster (:150-170) with the fixture's limits.
func newChipZones(t *testing.T, features camera.ZoneFeature, edit ...func(*camera.ZonesConfig)) (*camera.Zones, *mockZones) {
	t.Helper()
	m := &mockZones{}
	cfg := camera.ZonesConfig{
		Features: features, MaxUserDefinedZones: zoneMaxUserDefined, MaxZones: zoneMaxZones,
		SensitivityMax: zoneSensitivityMax, TwoDCartesianMax: zoneTwoDMax, Port: m,
	}
	for _, e := range edit {
		e(&cfg)
	}
	z, err := camera.NewZones(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return z, m
}

// triangle is MakeTriangle (:177-182).
func triangle(offset uint16) []camera.Vertex {
	return []camera.Vertex{{X: 10 + offset, Y: 20 + offset}, {X: 30 + offset, Y: 20 + offset}, {X: 20 + offset, Y: 40 + offset}}
}

// createZoneRequest is MakeCreateZoneRequest (:184-193).
func createZoneRequest(name string, vertices []camera.Vertex) zmdef.CreateTwoDCartesianZoneRequest {
	return zmdef.CreateTwoDCartesianZoneRequest{Zone: camera.TwoDZone{Name: name, Use: zmdef.ZoneUseMotion, Vertices: vertices}}
}

func invokeZones(z *camera.Zones, cmdID uint32, req any) (any, im.StatusCode) {
	resp, err := z.MatterInvoke(context.Background(), cmdID, req)
	return resp, statusOf(err)
}

func sortedEqual(got, want []uint32) bool {
	got, want = slices.Clone(got), slices.Clone(want)
	slices.Sort(got)
	slices.Sort(want)
	return slices.Equal(got, want)
}

const twoDUser = camera.FeatureUserDefined | camera.FeatureTwoDimensionalCartesianZone

func TestChipZoneManagement(t *testing.T) { //nolint:funlen // one subtest per chip TEST_F
	allCommands := []uint32{
		zmdef.CmdCreateTwoDCartesianZone, zmdef.CmdUpdateTwoDCartesianZone, zmdef.CmdRemoveZone,
		zmdef.CmdCreateOrUpdateTrigger, zmdef.CmdRemoveTrigger,
	}
	t.Run("AcceptedCommandsAppendElementsGrowsBeyondInitialCapacity", func(t *testing.T) { // :198-221
		z, _ := newChipZones(t, twoDUser)
		if !sortedEqual(z.MatterAcceptedCommands(), allCommands) {
			t.Fatalf("accepted %v", z.MatterAcceptedCommands())
		}
	})
	t.Run("AcceptedCommandsFollowFeatureGates", func(t *testing.T) { // :223-239
		// chip builds USERDEFINED without TWODCART; the definition makes
		// USERDEFINED optional only with TWODCART ([TWODCART]), so the
		// cluster's model refuses that selection — the gate is held on
		// TWODCART alone, which accepts no zone command.
		if _, err := camera.NewZones(camera.ZonesConfig{
			Features: camera.FeatureUserDefined, MaxUserDefinedZones: 5, MaxZones: 8, SensitivityMax: 4, Port: &mockZones{},
		}); !errors.Is(err, camera.ErrZonesConfig) {
			t.Fatalf("USERDEFINED without TWODCART: %v", err)
		}
		z, _ := newChipZones(t, camera.FeatureTwoDimensionalCartesianZone)
		if !sortedEqual(z.MatterAcceptedCommands(), []uint32{zmdef.CmdCreateOrUpdateTrigger, zmdef.CmdRemoveTrigger}) {
			t.Fatalf("TWODCART accepted %v", z.MatterAcceptedCommands())
		}
		none, _ := newChipZones(t, 0)
		if !sortedEqual(none.MatterAcceptedCommands(), []uint32{zmdef.CmdCreateOrUpdateTrigger, zmdef.CmdRemoveTrigger}) {
			t.Fatalf("no features: accepted %v", none.MatterAcceptedCommands())
		}
	})
	t.Run("AttributeListFollowsFeatureGates", func(t *testing.T) { // :241-255
		z, _ := newChipZones(t, twoDUser)
		var got []uint32
		for _, id := range z.MatterAttributes() {
			if id < 0xFFF0 {
				got = append(got, id)
			}
		}
		want := []uint32{
			zmdef.AttrMaxUserDefinedZones, zmdef.AttrMaxZones, zmdef.AttrZones, zmdef.AttrTriggers,
			zmdef.AttrSensitivityMax, zmdef.AttrSensitivity, zmdef.AttrTwoDCartesianMax,
		}
		if !sortedEqual(got, want) {
			t.Fatalf("attributes %v", got)
		}
	})
	t.Run("FeatureMapAttributeEncodesConfiguredFeatures", func(t *testing.T) { // :257-265
		f := camera.FeatureTwoDimensionalCartesianZone | camera.FeatureUserDefined | camera.FeatureFocusZones
		z, _ := newChipZones(t, f)
		if v, _ := z.MatterRead(0xFFFC); v != uint32(f) {
			t.Fatalf("FeatureMap %v", v)
		}
	})
	t.Run("SensitivityWritePersistsAndReloadsOnStartup", func(t *testing.T) { // :267-287
		z, _ := newChipZones(t, 0)
		var persisted any
		z.OnAttributeChange(func(c contract.Change) {
			if slices.Contains(c.Attributes, zmdef.AttrSensitivity) {
				persisted, _ = z.MatterRead(zmdef.AttrSensitivity)
			}
		})
		if err := z.MatterWrite(context.Background(), zmdef.AttrSensitivity, uint64(3)); err != nil {
			t.Fatal(err)
		}
		if z.Sensitivity() != 3 || persisted != uint8(3) {
			t.Fatalf("Sensitivity %d, reported %v", z.Sensitivity(), persisted)
		}
		reloaded, _ := newChipZones(t, 0, func(cfg *camera.ZonesConfig) { cfg.Sensitivity = 3 })
		if reloaded.Sensitivity() != 3 {
			t.Fatalf("reloaded Sensitivity %d", reloaded.Sensitivity())
		}
	})
	t.Run("GeneratedCommandsAppendElementsGrowsBeyondInitialCapacity", func(t *testing.T) { // :289-306
		z, _ := newChipZones(t, twoDUser)
		if !slices.Equal(z.MatterGeneratedCommands(), []uint32{zmdef.CmdCreateTwoDCartesianZoneResponse}) {
			t.Fatalf("generated %v", z.MatterGeneratedCommands())
		}
	})
	t.Run("GeneratedCommandsEmptyWhenCreateIsNotSupported", func(t *testing.T) { // :308-315
		z, _ := newChipZones(t, camera.FeatureTwoDimensionalCartesianZone)
		if len(z.MatterGeneratedCommands()) != 0 {
			t.Fatalf("TWODCART generated %v", z.MatterGeneratedCommands())
		}
	})
	t.Run("StartupLoadsPersistedZonesAndTriggersFromDelegate", func(t *testing.T) { // :317-347
		tri := triangle(0)
		// A 2D Cartesian zone needs TWODCART (R2-P6-par-1304).
		z, _ := newChipZones(t, camera.FeatureTwoDimensionalCartesianZone, func(cfg *camera.ZonesConfig) {
			cfg.Zones = []camera.Zone{{
				ZoneId: 55, ZoneType: zmdef.ZoneTypeTwoDcartZone, ZoneSource: zmdef.ZoneSourceMfg,
				TwoDCartesianZone: &camera.TwoDZone{Name: "Entry", Use: zmdef.ZoneUseMotion, Vertices: tri},
			}}
			cfg.Triggers = []camera.ZoneTrigger{{ZoneId: 55, InitialDuration: 10, AugmentationDuration: 5, MaxDuration: 10}}
		})
		if len(z.ZoneList()) != 1 || len(z.TriggerList()) != 1 {
			t.Fatalf("zones %d, triggers %d", len(z.ZoneList()), len(z.TriggerList()))
		}
		if _, ok := z.TriggerFor(55); !ok {
			t.Fatal("no trigger for zone 55")
		}
	})
	t.Run("InvokeCreateUpdateAndRemoveZoneCommands", func(t *testing.T) { // :349-397
		z, m := newChipZones(t, twoDUser)
		resp, st := invokeZones(z, zmdef.CmdCreateTwoDCartesianZone, createZoneRequest("Zone One", triangle(0)))
		wantCode(t, "create", st, im.StatusSuccess)
		// chip's delegate answered 1; the server assigns 0 to the first
		// zone (ZoneManagementServer.ts:243-250 at db91886c).
		if r, _ := resp.(zmdef.CreateTwoDCartesianZoneResponse); r.ZoneId != 0 || m.created != 1 || !slices.Equal(m.ids, []uint16{0}) {
			t.Fatalf("response %+v, created %d", resp, m.created)
		}
		zones := z.ZoneList()
		if len(zones) != 1 || zones[0].TwoDCartesianZone == nil || zones[0].TwoDCartesianZone.Name != "Zone One" {
			t.Fatalf("zones %+v", zones)
		}
		_, st = invokeZones(z, zmdef.CmdUpdateTwoDCartesianZone, zmdef.UpdateTwoDCartesianZoneRequest{
			ZoneId: 0, Zone: camera.TwoDZone{Name: "Zone Two", Use: zmdef.ZoneUseMotion, Vertices: triangle(100)},
		})
		wantCode(t, "update", st, im.StatusSuccess)
		if zones := z.ZoneList(); m.updated != 1 || len(zones) != 1 || zones[0].TwoDCartesianZone.Name != "Zone Two" {
			t.Fatalf("updated %d, zones %+v", m.updated, zones)
		}
		_, st = invokeZones(z, zmdef.CmdRemoveZone, zmdef.RemoveZoneRequest{ZoneId: 0})
		wantCode(t, "remove", st, im.StatusSuccess)
		if m.removed != 1 || len(z.ZoneList()) != 0 {
			t.Fatalf("removed %d, zones %d", m.removed, len(z.ZoneList()))
		}
	})
	t.Run("StartupFailsForInvalidConfiguration", func(t *testing.T) { // :399-408
		for _, cfg := range []camera.ZonesConfig{
			{MaxUserDefinedZones: zoneMaxUserDefined, MaxZones: zoneMaxZones, SensitivityMax: 1, TwoDCartesianMax: zoneTwoDMax},
			{Features: camera.FeatureUserDefined | camera.FeatureTwoDimensionalCartesianZone, MaxUserDefinedZones: 4, MaxZones: zoneMaxZones, SensitivityMax: zoneSensitivityMax, TwoDCartesianMax: zoneTwoDMax},
		} {
			cfg.Port = &mockZones{}
			if _, err := camera.NewZones(cfg); err == nil {
				t.Fatalf("%+v accepted", cfg)
			}
		}
	})
	t.Run("EventGenerationProducesTriggeredAndStoppedEvents", func(t *testing.T) { // :410-444
		z, _ := newChipZones(t, 0)
		em := &zoneEvents{}
		z.SetMatterEventEmitter(em)
		z.SetEndpoint(0) // mounted, as the bridge mounts it (EmitEvent needs the endpoint)
		if err := z.ZoneTriggered(7, zmdef.ZoneEventTriggeredReasonMotion); err != nil {
			t.Fatal(err)
		}
		if err := z.ZoneStopped(7, zmdef.ZoneEventStoppedReasonActionStopped); err != nil {
			t.Fatal(err)
		}
		want := []any{
			zmdef.ZoneTriggeredEvent{Zone: 7, Reason: zmdef.ZoneEventTriggeredReasonMotion},
			zmdef.ZoneStoppedEvent{Zone: 7, Reason: zmdef.ZoneEventStoppedReasonActionStopped},
		}
		if !slices.Equal(em.events, want) {
			t.Fatalf("events %+v", em.events)
		}
	})
}

type zoneEvents struct{ events []any }

func (e *zoneEvents) MatterEmitEvent(_ uint16, _, _ uint32, data any, _ contract.EventPriority) {
	e.events = append(e.events, data)
}
