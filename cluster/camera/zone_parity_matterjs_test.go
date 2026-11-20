// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// ZoneManagement: a port of matter.js
// packages/node/test/behaviors/zone-management/ZoneManagementServerTest.ts
// at b25ed960, by meaning. Its 46 `it` cases (66 with the polygon table,
// the refused updates, the durations and the sensitivities expanded) are
// the subtests below, named as the `it`s. matter.js's MockEndpoint with
// UserZoneServer.set(ATTRIBUTES) (:87-117) is camera.NewZones with the
// same limits; endpoint.act(...).createTwoDCartesianZone and its siblings
// (:127-145) run through MatterInvoke; the state matter.js reads with
// stateOf is ZoneList and TriggerList. The server's nonvolatile lastZoneId
// is the host's ZonesConfig.LastZoneID (O-1): a restart of the node
// (:272-292) is a new server on the zones and the last ID the port saw.
// "through the interaction layer" (:751-778) runs over the wire on
// fabrictest.
package camera_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/camera"
	"github.com/SukramJ/go-fabric/cluster/spec"
	zmdef "github.com/SukramJ/go-fabric/cluster/spec/zonemanagement"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

// The fixtures of :37-95.
var (
	mjSquare   = []camera.Vertex{{X: 10, Y: 10}, {X: 20, Y: 10}, {X: 20, Y: 20}, {X: 10, Y: 20}}
	mjCrossing = []camera.Vertex{mjSquare[0], mjSquare[2], mjSquare[1], mjSquare[3]}
	mjPrivacy  = camera.TwoDZone{Name: "Window", Use: zmdef.ZoneUsePrivacy, Vertices: mjSquare}
	mjMotion   = camera.TwoDZone{Name: "Driveway", Use: zmdef.ZoneUseMotion, Vertices: mjSquare}
	mjMfgArea  = camera.TwoDZone{Name: "Factory", Use: zmdef.ZoneUseMotion, Vertices: []camera.Vertex{{X: 0, Y: 0}, {X: 50, Y: 0}, {X: 50, Y: 50}}}
	mjMfgZone  = camera.Zone{ZoneId: 1, ZoneType: zmdef.ZoneTypeTwoDcartZone, ZoneSource: zmdef.ZoneSourceMfg, TwoDCartesianZone: &mjMfgArea}
	mjTrigger  = camera.ZoneTrigger{ZoneId: 0, InitialDuration: 10, AugmentationDuration: 5, MaxDuration: 60, BlindDuration: 3}
)

// userZone is :99-106.
func userZone(id uint16, zone camera.TwoDZone) camera.Zone {
	return camera.Zone{ZoneId: id, ZoneType: zmdef.ZoneTypeTwoDcartZone, ZoneSource: zmdef.ZoneSourceUser, TwoDCartesianZone: &zone}
}

// shiftedZone is :108-113: the privacy zone moved right by offset.
func shiftedZone(offset uint16) camera.TwoDZone {
	z := mjPrivacy
	z.Vertices = make([]camera.Vertex, len(mjSquare))
	for i, v := range mjSquare {
		z.Vertices[i] = camera.Vertex{X: v.X + offset, Y: v.Y}
	}
	return z
}

// mjConfig is ATTRIBUTES (:87-95) on UserZoneServer's features (:35).
func mjConfig(port camera.ZonePort, edit ...func(*camera.ZonesConfig)) camera.ZonesConfig {
	cfg := camera.ZonesConfig{
		Features:            camera.FeatureTwoDimensionalCartesianZone | camera.FeatureUserDefined,
		MaxUserDefinedZones: 5, MaxZones: 6, SensitivityMax: 5, Sensitivity: 3,
		TwoDCartesianMax: camera.Vertex{X: 1919, Y: 1079}, Port: port,
	}
	for _, e := range edit {
		e(&cfg)
	}
	return cfg
}

// createEndpoint is :115-117.
func createEndpoint(t *testing.T, edit ...func(*camera.ZonesConfig)) (*camera.Zones, *mockZones) {
	t.Helper()
	m := &mockZones{}
	z, err := camera.NewZones(mjConfig(m, edit...))
	if err != nil {
		t.Fatal(err)
	}
	return z, m
}

// mjCreate is create (:127-129): the new zone's ID and the status.
func mjCreate(z *camera.Zones, zone camera.TwoDZone) (uint16, im.StatusCode) {
	resp, st := invokeZones(z, zmdef.CmdCreateTwoDCartesianZone, zmdef.CreateTwoDCartesianZoneRequest{Zone: zone})
	r, _ := resp.(zmdef.CreateTwoDCartesianZoneResponse)
	return r.ZoneId, st
}

func mjUpdate(z *camera.Zones, id uint16, zone camera.TwoDZone) im.StatusCode {
	_, st := invokeZones(z, zmdef.CmdUpdateTwoDCartesianZone, zmdef.UpdateTwoDCartesianZoneRequest{ZoneId: id, Zone: zone})
	return st
}

func mjRemove(z *camera.Zones, id uint16) im.StatusCode {
	_, st := invokeZones(z, zmdef.CmdRemoveZone, zmdef.RemoveZoneRequest{ZoneId: id})
	return st
}

func mjSetTrigger(z *camera.Zones, tr camera.ZoneTrigger) im.StatusCode {
	_, st := invokeZones(z, zmdef.CmdCreateOrUpdateTrigger, zmdef.CreateOrUpdateTriggerRequest{Trigger: tr})
	return st
}

func mjRemoveTrigger(z *camera.Zones, id uint16) im.StatusCode {
	_, st := invokeZones(z, zmdef.CmdRemoveTrigger, zmdef.RemoveTriggerRequest{ZoneId: id})
	return st
}

// wantCreated creates zone and requires the ID want.
func wantCreated(t *testing.T, z *camera.Zones, zone camera.TwoDZone, want uint16) {
	t.Helper()
	id, st := mjCreate(z, zone)
	wantCode(t, "create", st, im.StatusSuccess)
	if id != want {
		t.Fatalf("zone ID %d, want %d", id, want)
	}
}

func zonesEqual(got, want []camera.Zone) bool {
	return slices.EqualFunc(got, want, func(a, b camera.Zone) bool {
		return a.ZoneId == b.ZoneId && a.ZoneType == b.ZoneType && a.ZoneSource == b.ZoneSource &&
			(a.TwoDCartesianZone == nil) == (b.TwoDCartesianZone == nil) &&
			(a.TwoDCartesianZone == nil || twoDEqual(*a.TwoDCartesianZone, *b.TwoDCartesianZone))
	})
}

func twoDEqual(a, b camera.TwoDZone) bool {
	return a.Name == b.Name && a.Use == b.Use && slices.Equal(a.Vertices, b.Vertices) &&
		(a.Color == nil) == (b.Color == nil) && (a.Color == nil || *a.Color == *b.Color)
}

func triggersEqual(got, want []camera.ZoneTrigger) bool {
	return slices.EqualFunc(got, want, func(a, b camera.ZoneTrigger) bool {
		return a.ZoneId == b.ZoneId && a.InitialDuration == b.InitialDuration && a.AugmentationDuration == b.AugmentationDuration &&
			a.MaxDuration == b.MaxDuration && a.BlindDuration == b.BlindDuration &&
			(a.Sensitivity == nil) == (b.Sensitivity == nil) && (a.Sensitivity == nil || *a.Sensitivity == *b.Sensitivity)
	})
}

func TestParityMatterJS_ZoneManagementServerTest(t *testing.T) { //nolint:gocognit,gocyclo,funlen,maintidx // one subtest per matter.js case
	t.Run("does not start with repeated zone IDs", func(t *testing.T) { // :206-216
		_, err := camera.NewZones(mjConfig(&mockZones{}, func(c *camera.ZonesConfig) {
			c.Zones = []camera.Zone{mjMfgZone, userZone(1, mjPrivacy)}
		}))
		if !errors.Is(err, camera.ErrZonesConfig) || !strings.Contains(err.Error(), "ZoneID 1 is not unique") {
			t.Fatalf("NewZones: %v", err)
		}
	})
	t.Run("does not start with MaxZones less than MaxUserDefinedZones", func(t *testing.T) { // :218-227
		if _, err := camera.NewZones(mjConfig(&mockZones{}, func(c *camera.ZonesConfig) { c.MaxZones = 4 })); !errors.Is(err, camera.ErrZonesConfig) {
			t.Fatalf("NewZones: %v", err)
		}
	})
	t.Run("refuses a write that repeats a zone ID", func(t *testing.T) { // :229-237
		// The application's write of Zones: here the host has none — Zones
		// is the server's (ErrNotHeld), so no write can repeat an ID.
		z, _ := createEndpoint(t, func(c *camera.ZonesConfig) { c.Zones = []camera.Zone{mjMfgZone} })
		err := z.SetAttributes(map[uint32]any{zmdef.AttrZones: spec.List[camera.Zone]{mjMfgZone, userZone(1, mjPrivacy)}})
		if !errors.Is(err, spec.ErrNotHeld) {
			t.Fatalf("SetAttributes(Zones): %v", err)
		}
		if !zonesEqual(z.ZoneList(), []camera.Zone{mjMfgZone}) {
			t.Fatalf("zones %+v", z.ZoneList())
		}
	})

	t.Run("createTwoDCartesianZone", func(t *testing.T) {
		t.Run("adds a user-defined zone with zone ID 0 when there are no zones", func(t *testing.T) { // :240-246
			z, m := createEndpoint(t)
			wantCreated(t, z, mjPrivacy, 0)
			if !zonesEqual(z.ZoneList(), []camera.Zone{userZone(0, mjPrivacy)}) || !slices.Equal(m.ids, []uint16{0}) {
				t.Fatalf("zones %+v, port IDs %v", z.ZoneList(), m.ids)
			}
		})
		t.Run("continues after the manufacturer zones", func(t *testing.T) { // :248-254
			z, _ := createEndpoint(t, func(c *camera.ZonesConfig) { c.Zones = []camera.Zone{mjMfgZone} })
			wantCreated(t, z, mjPrivacy, 2)
			if !zonesEqual(z.ZoneList(), []camera.Zone{mjMfgZone, userZone(2, mjPrivacy)}) {
				t.Fatalf("zones %+v", z.ZoneList())
			}
		})
		t.Run("skips zone IDs in use", func(t *testing.T) { // :256-260
			last := uint16(0)
			z, _ := createEndpoint(t, func(c *camera.ZonesConfig) { c.Zones, c.LastZoneID = []camera.Zone{mjMfgZone}, &last })
			wantCreated(t, z, mjPrivacy, 2)
		})
		t.Run("does not reuse the ID of a removed zone", func(t *testing.T) { // :262-270
			z, _ := createEndpoint(t)
			for offset := range uint16(3) {
				wantCreated(t, z, shiftedZone(offset), offset)
			}
			wantCode(t, "remove", mjRemove(z, 2), im.StatusSuccess)
			wantCreated(t, z, shiftedZone(3), 3)
		})
		t.Run("does not reuse the ID of a removed zone after a restart", func(t *testing.T) { // :272-292
			z, m := createEndpoint(t)
			for offset := range uint16(3) {
				wantCreated(t, z, shiftedZone(offset), offset)
			}
			wantCode(t, "remove", mjRemove(z, 2), im.StatusSuccess)
			// The host persisted the zones and the last ID its port saw.
			last := m.ids[len(m.ids)-1]
			restarted, _ := createEndpoint(t, func(c *camera.ZonesConfig) { c.Zones, c.LastZoneID = z.ZoneList(), &last })
			wantCreated(t, restarted, shiftedZone(3), 3)
		})
		t.Run("wraps the zone ID to 0 after 65534", func(t *testing.T) { // :294-298
			last := uint16(0xfffe)
			z, _ := createEndpoint(t, func(c *camera.ZonesConfig) { c.LastZoneID = &last })
			wantCreated(t, z, mjPrivacy, 0)
		})
		t.Run("refuses a focus zone without the FocusZones feature", func(t *testing.T) { // :300-307
			z, _ := createEndpoint(t)
			focus := mjPrivacy
			focus.Use = zmdef.ZoneUseFocus
			_, st := mjCreate(z, focus)
			wantCode(t, "create", st, im.StatusConstraintError)
		})
		t.Run("adds a focus zone with the FocusZones feature", func(t *testing.T) { // :309-324
			z, _ := createEndpoint(t, func(c *camera.ZonesConfig) { c.Features |= camera.FeatureFocusZones })
			focus := mjPrivacy
			focus.Use = zmdef.ZoneUseFocus
			wantCreated(t, z, focus, 0)
		})
		t.Run("refuses a zone beyond MaxUserDefinedZones", func(t *testing.T) { // :326-333
			z, _ := createEndpoint(t, func(c *camera.ZonesConfig) { c.MaxZones = 10 })
			for offset := range uint16(5) {
				wantCreated(t, z, shiftedZone(offset), offset)
			}
			_, st := mjCreate(z, mjMotion)
			wantCode(t, "a sixth user-defined zone", st, im.StatusResourceExhausted)
		})
		t.Run("refuses a zone beyond MaxZones", func(t *testing.T) { // :335-342
			z, _ := createEndpoint(t, func(c *camera.ZonesConfig) {
				c.MaxZones = 5
				c.Zones = []camera.Zone{mjMfgZone}
				for _, id := range []uint16{2, 3, 4, 5} {
					c.Zones = append(c.Zones, userZone(id, shiftedZone(id)))
				}
			})
			_, st := mjCreate(z, mjPrivacy)
			wantCode(t, "a sixth zone", st, im.StatusResourceExhausted)
		})
		t.Run("checks the zone limit before the polygon check", func(t *testing.T) { // :344-354
			z, _ := createEndpoint(t, func(c *camera.ZonesConfig) {
				c.MaxZones = 5
				for id := range uint16(5) {
					c.Zones = append(c.Zones, userZone(id, shiftedZone(id)))
				}
			})
			crossing := mjPrivacy
			crossing.Vertices = mjCrossing
			_, st := mjCreate(z, crossing)
			wantCode(t, "create", st, im.StatusResourceExhausted)
		})
		t.Run("refuses a vertex beyond TwoDCartesianMax", func(t *testing.T) { // :356-363
			z, _ := createEndpoint(t)
			zone := mjPrivacy
			zone.Vertices = append(slices.Clone(mjSquare[:3]), camera.Vertex{X: 10, Y: 1080})
			_, st := mjCreate(z, zone)
			wantCode(t, "create", st, im.StatusDynamicConstraintError)
		})
		t.Run("accepts a vertex at TwoDCartesianMax", func(t *testing.T) { // :365-373
			z, _ := createEndpoint(t)
			zone := mjPrivacy
			zone.Vertices = append(slices.Clone(mjSquare[:3]), camera.Vertex{X: 10, Y: 1079})
			wantCreated(t, z, zone, 0)
		})
		t.Run("refuses a zone with the same use and vertices as another zone", func(t *testing.T) { // :375-380
			z, _ := createEndpoint(t)
			wantCreated(t, z, mjPrivacy, 0)
			other := mjPrivacy
			other.Name = "Other"
			_, st := mjCreate(z, other)
			wantCode(t, "create", st, im.StatusAlreadyExists)
		})
		t.Run("refuses a zone with the same use and vertices as a manufacturer zone", func(t *testing.T) { // :382-386
			z, _ := createEndpoint(t, func(c *camera.ZonesConfig) { c.Zones = []camera.Zone{mjMfgZone} })
			cp := mjMfgArea
			cp.Name = "Copy"
			_, st := mjCreate(z, cp)
			wantCode(t, "create", st, im.StatusAlreadyExists)
		})
		t.Run("adds a zone with the same vertices and another use", func(t *testing.T) { // :388-393
			z, _ := createEndpoint(t)
			wantCreated(t, z, mjPrivacy, 0)
			wantCreated(t, z, mjMotion, 1)
		})
		t.Run("discards the zone when an override refuses it after super", func(t *testing.T) { // :395-413
			// The override that refuses after super is the host's port
			// refusing the zone the server hands it.
			z, m := createEndpoint(t)
			m.createErr = spec.Errorf(im.StatusFailure, "Camera refused the zone")
			_, st := mjCreate(z, mjPrivacy)
			wantCode(t, "create", st, im.StatusFailure)
			if len(z.ZoneList()) != 0 {
				t.Fatalf("zones %+v", z.ZoneList())
			}
			// lastZoneId undefined: the next zone is still the first.
			m.createErr = nil
			wantCreated(t, z, mjPrivacy, 0)
		})

		t.Run("polygon check", func(t *testing.T) { // :415-538
			type polygon struct {
				name     string
				vertices []camera.Vertex
			}
			v := func(xy ...uint16) []camera.Vertex {
				out := make([]camera.Vertex, 0, len(xy)/2)
				for i := 0; i+1 < len(xy); i += 2 {
					out = append(out, camera.Vertex{X: xy[i], Y: xy[i+1]})
				}
				return out
			}
			valid := []polygon{ // :416-449
				{"triangle", v(10, 10, 20, 10, 20, 20)},
				{"square", mjSquare},
				{"convex", v(10, 10, 20, 10, 20, 20, 15, 25, 10, 20)},
				{"concave", v(10, 10, 20, 10, 20, 20, 15, 15, 10, 20)},
				{"arrow", v(10, 10, 20, 20, 30, 10, 20, 15)},
				{"collinear neighbors", v(10, 10, 20, 10, 30, 10, 20, 15)},
			}
			invalid := []polygon{ // :451-518
				{"bowtie", mjCrossing},
				{"crossing edges", v(10, 10, 20, 10, 20, 20, 15, 5, 10, 20)},
				{"overlapping edges", v(2, 2, 10, 2, 7, 2, 10, 1, 1, 1)},
				{"vertex on a non-adjacent edge", v(0, 0, 10, 0, 10, 10, 5, 0, 0, 10)},
				{"touching vertex", v(0, 0, 1, 0, 2, 0, 2, 1, 1, 0, 0, 1)},
				{"repeated vertex", v(0, 0, 2, 0, 2, 1, 2, 0, 1, 0)},
				{"repeated first vertex", v(0, 0, 1, 0, 0, 0, 0, 1)},
				{"zero-length edge", v(0, 0, 0, 0, 1, 0, 0, 1)},
				{"closing vertex", v(0, 0, 1, 0, 1, 1, 0, 0)},
				{"collinear triangle", v(0, 0, 2, 0, 1, 0)},
				{"collinear quadrilateral", v(0, 0, 2, 0, 1, 0, 3, 0)},
			}
			for _, p := range valid {
				t.Run("accepts a "+p.name, func(t *testing.T) { // :520-526
					z, _ := createEndpoint(t)
					zone := mjPrivacy
					zone.Vertices = p.vertices
					wantCreated(t, z, zone, 0)
					if camera.ZoneSelfIntersecting(p.vertices) {
						t.Fatal("ZoneSelfIntersecting")
					}
				})
			}
			for _, p := range invalid {
				t.Run("refuses "+p.name, func(t *testing.T) { // :528-537
					z, _ := createEndpoint(t)
					zone := mjPrivacy
					zone.Vertices = p.vertices
					_, st := mjCreate(z, zone)
					wantCode(t, "create", st, im.StatusDynamicConstraintError)
					if !camera.ZoneSelfIntersecting(p.vertices) {
						t.Fatal("not ZoneSelfIntersecting")
					}
				})
			}
		})
	})

	t.Run("updateTwoDCartesianZone", func(t *testing.T) {
		t.Run("replaces the zone and keeps its ID and source", func(t *testing.T) { // :542-550
			z, _ := createEndpoint(t)
			wantCreated(t, z, mjPrivacy, 0)
			changed := shiftedZone(5)
			red := "#FF0000"
			changed.Name, changed.Color = "Door", &red
			wantCode(t, "update", mjUpdate(z, 0, changed), im.StatusSuccess)
			if !zonesEqual(z.ZoneList(), []camera.Zone{userZone(0, changed)}) {
				t.Fatalf("zones %+v", z.ZoneList())
			}
		})
		t.Run("accepts the unchanged vertices of the zone itself", func(t *testing.T) { // :552-559
			z, _ := createEndpoint(t)
			wantCreated(t, z, mjPrivacy, 0)
			renamed := mjPrivacy
			renamed.Name = "Renamed"
			wantCode(t, "update", mjUpdate(z, 0, renamed), im.StatusSuccess)
			if got := z.ZoneList()[0].TwoDCartesianZone.Name; got != "Renamed" {
				t.Fatalf("name %q", got)
			}
		})
		t.Run("refuses an unknown zone before checking the vertices", func(t *testing.T) { // :561-565
			z, _ := createEndpoint(t)
			crossing := mjPrivacy
			crossing.Vertices = mjCrossing
			wantCode(t, "update", mjUpdate(z, 7, crossing), im.StatusNotFound)
		})
		t.Run("refuses a manufacturer zone before checking the vertices", func(t *testing.T) { // :567-574
			z, _ := createEndpoint(t, func(c *camera.ZonesConfig) { c.Zones = []camera.Zone{mjMfgZone} })
			crossing := mjPrivacy
			crossing.Vertices = mjCrossing
			wantCode(t, "update", mjUpdate(z, 1, crossing), im.StatusConstraintError)
		})
		refused := []struct { // :576-589
			name string
			edit func(*camera.TwoDZone)
			want im.StatusCode
		}{
			{"a focus zone without the FocusZones feature", func(z *camera.TwoDZone) { z.Use = zmdef.ZoneUseFocus }, im.StatusConstraintError},
			{"a vertex beyond TwoDCartesianMax", func(z *camera.TwoDZone) {
				z.Vertices = append(slices.Clone(mjSquare[:3]), camera.Vertex{X: 1920, Y: 20})
			}, im.StatusDynamicConstraintError},
			{"vertices another zone already has with the same use", func(z *camera.TwoDZone) { *z = shiftedZone(1) }, im.StatusAlreadyExists},
			{"crossing edges", func(z *camera.TwoDZone) { z.Vertices = mjCrossing }, im.StatusDynamicConstraintError},
		}
		for _, c := range refused {
			t.Run("refuses "+c.name, func(t *testing.T) { // :590-598
				z, _ := createEndpoint(t)
				wantCreated(t, z, mjPrivacy, 0)
				wantCreated(t, z, shiftedZone(1), 1)
				zone := mjPrivacy
				c.edit(&zone)
				wantCode(t, "update", mjUpdate(z, 0, zone), c.want)
			})
		}
	})

	t.Run("removeZone", func(t *testing.T) {
		t.Run("removes the zone", func(t *testing.T) { // :602-609
			z, _ := createEndpoint(t, func(c *camera.ZonesConfig) { c.Zones = []camera.Zone{mjMfgZone} })
			wantCreated(t, z, mjPrivacy, 2)
			wantCode(t, "remove", mjRemove(z, 2), im.StatusSuccess)
			if !zonesEqual(z.ZoneList(), []camera.Zone{mjMfgZone}) {
				t.Fatalf("zones %+v", z.ZoneList())
			}
		})
		t.Run("refuses an unknown zone", func(t *testing.T) { // :611-615
			z, _ := createEndpoint(t)
			wantCode(t, "remove", mjRemove(z, 7), im.StatusNotFound)
		})
		t.Run("refuses a manufacturer zone", func(t *testing.T) { // :617-621
			z, _ := createEndpoint(t, func(c *camera.ZonesConfig) { c.Zones = []camera.Zone{mjMfgZone} })
			wantCode(t, "remove", mjRemove(z, 1), im.StatusConstraintError)
		})
		t.Run("refuses a zone that a trigger uses", func(t *testing.T) { // :623-630
			z, _ := createEndpoint(t)
			wantCreated(t, z, mjMotion, 0)
			wantCode(t, "trigger", mjSetTrigger(z, mjTrigger), im.StatusSuccess)
			wantCode(t, "remove", mjRemove(z, 0), im.StatusInvalidInState)
			if n := len(z.ZoneList()); n != 1 {
				t.Fatalf("%d zones", n)
			}
		})
	})

	t.Run("createOrUpdateTrigger", func(t *testing.T) {
		t.Run("adds a trigger and replaces it on update", func(t *testing.T) { // :634-644
			z, _ := createEndpoint(t)
			wantCreated(t, z, mjMotion, 0)
			wantCode(t, "create", mjSetTrigger(z, mjTrigger), im.StatusSuccess)
			if !triggersEqual(z.TriggerList(), []camera.ZoneTrigger{mjTrigger}) {
				t.Fatalf("triggers %+v", z.TriggerList())
			}
			longer := mjTrigger
			longer.MaxDuration = 120
			wantCode(t, "update", mjSetTrigger(z, longer), im.StatusSuccess)
			if !triggersEqual(z.TriggerList(), []camera.ZoneTrigger{longer}) {
				t.Fatalf("triggers %+v", z.TriggerList())
			}
		})
		t.Run("adds a trigger for a manufacturer motion zone", func(t *testing.T) { // :646-652
			z, _ := createEndpoint(t, func(c *camera.ZonesConfig) { c.Zones = []camera.Zone{mjMfgZone} })
			tr := mjTrigger
			tr.ZoneId = 1
			wantCode(t, "trigger", mjSetTrigger(z, tr), im.StatusSuccess)
			if !triggersEqual(z.TriggerList(), []camera.ZoneTrigger{tr}) {
				t.Fatalf("triggers %+v", z.TriggerList())
			}
		})
		t.Run("refuses an unknown zone", func(t *testing.T) { // :654-658
			z, _ := createEndpoint(t)
			wantCode(t, "trigger", mjSetTrigger(z, mjTrigger), im.StatusNotFound)
		})
		t.Run("refuses a zone that is not a motion zone", func(t *testing.T) { // :660-665
			z, _ := createEndpoint(t)
			wantCreated(t, z, mjPrivacy, 0)
			wantCode(t, "trigger", mjSetTrigger(z, mjTrigger), im.StatusConstraintError)
		})
		for _, c := range []struct { // :667-678
			name string
			edit func(*camera.ZoneTrigger)
		}{
			{"an augmentation longer than the initial duration", func(tr *camera.ZoneTrigger) { tr.AugmentationDuration = 11 }},
			{"a maximum shorter than the initial duration", func(tr *camera.ZoneTrigger) { tr.MaxDuration = 9 }},
			{"an initial duration of 0", func(tr *camera.ZoneTrigger) { tr.InitialDuration, tr.AugmentationDuration = 0, 0 }},
		} {
			t.Run("refuses "+c.name, func(t *testing.T) {
				z, _ := createEndpoint(t)
				wantCreated(t, z, mjMotion, 0)
				tr := mjTrigger
				c.edit(&tr)
				wantCode(t, "trigger", mjSetTrigger(z, tr), im.StatusConstraintError)
			})
		}
		t.Run("refuses a sensitivity without the PerZoneSensitivity feature", func(t *testing.T) { // :680-685
			z, _ := createEndpoint(t)
			wantCreated(t, z, mjMotion, 0)
			two := uint8(2)
			tr := mjTrigger
			tr.Sensitivity = &two
			wantCode(t, "trigger", mjSetTrigger(z, tr), im.StatusInvalidCommand)
		})
		t.Run("with the PerZoneSensitivity feature", func(t *testing.T) { // :687-727
			// createPerZoneEndpoint (:694-697): no Sensitivity, one motion zone.
			createPerZone := func(t *testing.T) *camera.Zones {
				t.Helper()
				z, _ := createEndpoint(t, func(c *camera.ZonesConfig) {
					c.Features |= camera.FeaturePerZoneSensitivity
					c.Sensitivity = 0
					c.Zones = []camera.Zone{userZone(0, mjMotion)}
				})
				return z
			}
			t.Run("adds a trigger with a sensitivity", func(t *testing.T) { // :703-709
				z := createPerZone(t)
				five := uint8(5)
				tr := mjTrigger
				tr.Sensitivity = &five
				wantCode(t, "trigger", mjSetTrigger(z, tr), im.StatusSuccess)
				if !triggersEqual(z.TriggerList(), []camera.ZoneTrigger{tr}) {
					t.Fatalf("triggers %+v", z.TriggerList())
				}
			})
			t.Run("refuses a trigger without a sensitivity", func(t *testing.T) { // :711-715
				z := createPerZone(t)
				wantCode(t, "trigger", mjSetTrigger(z, mjTrigger), im.StatusInvalidCommand)
			})
			for _, s := range []uint8{0, 6} { // :717-726
				t.Run(fmt.Sprintf("refuses a sensitivity of %d", s), func(t *testing.T) {
					z := createPerZone(t)
					tr := mjTrigger
					tr.Sensitivity = &s
					wantCode(t, "trigger", mjSetTrigger(z, tr), im.StatusConstraintError)
				})
			}
		})
	})

	t.Run("removeTrigger", func(t *testing.T) {
		t.Run("removes the trigger, after which the zone can be removed", func(t *testing.T) { // :731-741
			z, _ := createEndpoint(t)
			wantCreated(t, z, mjMotion, 0)
			wantCode(t, "trigger", mjSetTrigger(z, mjTrigger), im.StatusSuccess)
			wantCode(t, "remove trigger", mjRemoveTrigger(z, 0), im.StatusSuccess)
			wantCode(t, "remove zone", mjRemove(z, 0), im.StatusSuccess)
			if len(z.TriggerList()) != 0 || len(z.ZoneList()) != 0 {
				t.Fatalf("triggers %+v, zones %+v", z.TriggerList(), z.ZoneList())
			}
		})
		t.Run("refuses a zone without a trigger", func(t *testing.T) { // :743-748
			z, _ := createEndpoint(t)
			wantCreated(t, z, mjMotion, 0)
			wantCode(t, "remove trigger", mjRemoveTrigger(z, 0), im.StatusNotFound)
		})
	})
}

// deviceTypeCamera is the Camera device type a test endpoint carries.
const deviceTypeCamera = 0x0142

// TestParityMatterJS_ZoneManagementServerTestInteraction is "through the
// interaction layer" (:751-778): the commands over the wire on a node
// whose endpoint serves UserZoneServer with one motion zone 0 (:166-196).
func TestParityMatterJS_ZoneManagementServerTestInteraction(t *testing.T) {
	z, _ := createEndpoint(t, func(c *camera.ZonesConfig) { c.Zones = []camera.Zone{userZone(0, mjMotion)} })
	site := fabrictest.NewSite(t, fabrictest.Options{Loopback: true})
	pair := site.CommissionedPair(fabrictest.PairOptions{
		Device: fabrictest.Topology{Bridged: []node.Endpoint{fabrictest.Endpoint("zones", deviceTypeCamera, z)}},
	})
	ep, ok := pair.Device.EndpointOf("zones")
	if !ok {
		t.Fatal("no zones endpoint")
	}
	invoke := func(t *testing.T, cmd uint32, fields spec.Encodable) fabrictest.Result {
		t.Helper()
		res, err := fabrictest.Invoke(wireCtx(t), pair, im.ConcreteCommandPath{Endpoint: ep, Cluster: zmdef.ClusterID, Command: cmd, HasEndpoint: true}, fields)
		if err != nil && res.Status == im.StatusSuccess { // a refusal comes back as its status and an error
			t.Fatal(err)
		}
		return res
	}
	t.Run("returns the ID of the new zone", func(t *testing.T) { // :752-754
		res := invoke(t, zmdef.CmdCreateTwoDCartesianZone, zmdef.CreateTwoDCartesianZoneRequest{Zone: mjPrivacy})
		var r zmdef.CreateTwoDCartesianZoneResponse
		if res.Status != im.StatusSuccess || !res.HasFields || r.DecodeTLV(res.Fields) != nil || r.ZoneId != 1 {
			t.Fatalf("result %+v, response %+v", res, r)
		}
	})
	t.Run("refuses a name longer than 32 bytes before the command handler", func(t *testing.T) { // :756-761
		zone := mjPrivacy
		zone.Name = strings.Repeat("X", 33)
		if res := invoke(t, zmdef.CmdCreateTwoDCartesianZone, zmdef.CreateTwoDCartesianZoneRequest{Zone: zone}); res.Status != im.StatusConstraintError || res.HasFields {
			t.Fatalf("result %+v", res)
		}
	})
	t.Run("refuses fewer than 3 vertices before the command handler", func(t *testing.T) { // :763-768
		zone := mjPrivacy
		zone.Vertices = mjSquare[:2]
		if res := invoke(t, zmdef.CmdCreateTwoDCartesianZone, zmdef.CreateTwoDCartesianZoneRequest{Zone: zone}); res.Status != im.StatusConstraintError || res.HasFields {
			t.Fatalf("result %+v", res)
		}
	})
	t.Run("returns INVALID_COMMAND for a trigger sensitivity without the PerZoneSensitivity feature", func(t *testing.T) { // :770-777
		two := uint8(2)
		tr := mjTrigger
		tr.Sensitivity = &two
		if res := invoke(t, zmdef.CmdCreateOrUpdateTrigger, zmdef.CreateOrUpdateTriggerRequest{Trigger: tr}); res.Status != im.StatusInvalidCommand || res.HasFields {
			t.Fatalf("result %+v", res)
		}
	})
}

// TestZonesLastZoneIDNilContinuesAfterTheHighest is the host that kept
// the zones but not LastZoneID (ZonesConfig.LastZoneID nil): the server
// continues after the highest ID in use, as matter.js before its first
// assignment (ZoneManagementServer.ts:245 at b25ed960) — so the ID of a
// removed highest zone comes back, which LastZoneID set prevents
// ("does not reuse the ID of a removed zone after a restart").
func TestZonesLastZoneIDNilContinuesAfterTheHighest(t *testing.T) {
	z, _ := createEndpoint(t)
	for offset := range uint16(3) {
		wantCreated(t, z, shiftedZone(offset), offset)
	}
	wantCode(t, "remove", mjRemove(z, 2), im.StatusSuccess)
	restarted, _ := createEndpoint(t, func(c *camera.ZonesConfig) { c.Zones = z.ZoneList() })
	wantCreated(t, restarted, shiftedZone(3), 2)
}

// TestZonesRefusedCreateKeepsTheLastID: a create the port refuses does not
// advance the ID sequence, also after IDs were assigned (matter.js
// ZoneManagementServer.ts:61-63 at b25ed960: the state super wrote is
// discarded).
func TestZonesRefusedCreateKeepsTheLastID(t *testing.T) {
	z, m := createEndpoint(t)
	wantCreated(t, z, shiftedZone(0), 0)
	wantCode(t, "remove", mjRemove(z, 0), im.StatusSuccess)
	m.createErr = errors.New("storage full")
	if _, st := mjCreate(z, shiftedZone(1)); st != im.StatusFailure {
		t.Fatalf("refused create: %v", st)
	}
	m.createErr = nil
	wantCreated(t, z, shiftedZone(1), 1)
	if !slices.Equal(m.ids, []uint16{0, 1}) {
		t.Fatalf("port IDs %v", m.ids)
	}
}
