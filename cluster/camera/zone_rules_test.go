// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// The ZoneManagement rules no chip case reaches, each against the
// ZoneManagementCluster.cpp line it holds.
package camera_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/camera"
	"github.com/SukramJ/go-fabric/cluster/spec"
	zmdef "github.com/SukramJ/go-fabric/cluster/spec/zonemanagement"
	"github.com/SukramJ/go-fabric/im"
)

func TestZoneConfigRules(t *testing.T) {
	if _, err := camera.NewZones(camera.ZonesConfig{MaxZones: 1, SensitivityMax: 2}); !errors.Is(err, camera.ErrNoZonePort) {
		t.Fatalf("no port: %v", err)
	}
	for name, cfg := range map[string]camera.ZonesConfig{
		"MaxZones 0 :97":                    {SensitivityMax: 2},
		"SensitivityMax 11 :101":            {MaxZones: 1, SensitivityMax: 11},
		"MaxZones below MaxUserDefined :91": {Features: twoDUser, MaxUserDefinedZones: 6, MaxZones: 5, SensitivityMax: 2},
		"Sensitivity above its max :63-68":  {MaxZones: 1, SensitivityMax: 2, Sensitivity: 3},
	} {
		cfg.Port = &mockZones{}
		if _, err := camera.NewZones(cfg); !errors.Is(err, camera.ErrZonesConfig) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestZoneCommandRules(t *testing.T) { //nolint:funlen // one rule per block
	color := func(s string) *string { return &s }
	t.Run("the zone's checks :445-460, :472-504", func(t *testing.T) {
		z, m := newChipZones(t, twoDUser)
		for _, c := range []struct {
			what string
			zone camera.TwoDZone
			want im.StatusCode
		}{
			{"name of 33", camera.TwoDZone{Name: strings.Repeat("x", 33), Vertices: triangle(0)}, im.StatusConstraintError},
			{"unknown use", camera.TwoDZone{Use: 9, Vertices: triangle(0)}, im.StatusConstraintError},
			{"two vertices", camera.TwoDZone{Vertices: triangle(0)[:2]}, im.StatusConstraintError},
			{"colour of 6", camera.TwoDZone{Vertices: triangle(0), Color: color("#FFFFF")}, im.StatusConstraintError},
			{"Focus without FOCUSZONES", camera.TwoDZone{Use: zmdef.ZoneUseFocus, Vertices: triangle(0)}, im.StatusConstraintError},
			{"a vertex beyond the max", camera.TwoDZone{Vertices: []camera.Vertex{{X: 0, Y: 0}, {X: 641, Y: 0}, {X: 0, Y: 10}}}, im.StatusDynamicConstraintError},
			{"collinear", camera.TwoDZone{Vertices: []camera.Vertex{{X: 0, Y: 0}, {X: 1, Y: 1}, {X: 2, Y: 2}}}, im.StatusDynamicConstraintError},
		} {
			_, st := invokeZones(z, zmdef.CmdCreateTwoDCartesianZone, zmdef.CreateTwoDCartesianZoneRequest{Zone: c.zone})
			wantCode(t, c.what, st, c.want)
		}
		_, st := invokeZones(z, zmdef.CmdCreateTwoDCartesianZone, zmdef.CreateTwoDCartesianZoneRequest{Zone: camera.TwoDZone{Vertices: triangle(0), Color: color("#FF00FF")}})
		wantCode(t, "create", st, im.StatusSuccess)
		_, st = invokeZones(z, zmdef.CmdCreateTwoDCartesianZone, zmdef.CreateTwoDCartesianZoneRequest{Zone: camera.TwoDZone{Name: "again", Vertices: triangle(0)}})
		wantCode(t, "duplicate", st, im.StatusAlreadyExists)
		m.createErr = spec.Errorf(im.StatusResourceExhausted, "full")
		_, st = invokeZones(z, zmdef.CmdCreateTwoDCartesianZone, createZoneRequest("port", triangle(50)))
		wantCode(t, "the port's status", st, im.StatusResourceExhausted)
	})
	t.Run("the user-defined budget :473", func(t *testing.T) {
		z, _ := newChipZones(t, twoDUser)
		for i := range zoneMaxUserDefined {
			_, st := invokeZones(z, zmdef.CmdCreateTwoDCartesianZone, createZoneRequest("z", triangle(uint16(i*50)))) //nolint:gosec // a small loop
			wantCode(t, "create", st, im.StatusSuccess)
		}
		_, st := invokeZones(z, zmdef.CmdCreateTwoDCartesianZone, createZoneRequest("z", triangle(300)))
		wantCode(t, "a sixth", st, im.StatusResourceExhausted)
		_, st = invokeZones(z, zmdef.CmdRemoveZone, zmdef.RemoveZoneRequest{ZoneId: 1})
		wantCode(t, "remove", st, im.StatusSuccess)
		_, st = invokeZones(z, zmdef.CmdCreateTwoDCartesianZone, createZoneRequest("z", triangle(300)))
		wantCode(t, "after a removal", st, im.StatusSuccess)
	})
	t.Run("update and remove of a zone :523-610", func(t *testing.T) {
		mfg := camera.Zone{ZoneId: 9, ZoneType: zmdef.ZoneTypeTwoDcartZone, ZoneSource: zmdef.ZoneSourceMfg,
			TwoDCartesianZone: &camera.TwoDZone{Name: "door", Use: zmdef.ZoneUseMotion, Vertices: triangle(200)}}
		z, _ := newChipZones(t, twoDUser, func(cfg *camera.ZonesConfig) { cfg.Zones = []camera.Zone{mfg} })
		update := func(id uint16, zone camera.TwoDZone) im.StatusCode {
			_, st := invokeZones(z, zmdef.CmdUpdateTwoDCartesianZone, zmdef.UpdateTwoDCartesianZoneRequest{ZoneId: id, Zone: zone})
			return st
		}
		wantCode(t, "update unknown", update(1, camera.TwoDZone{Vertices: triangle(0)}), im.StatusNotFound)
		wantCode(t, "update Mfg", update(9, camera.TwoDZone{Vertices: triangle(0)}), im.StatusConstraintError)
		wantCode(t, "update invalid", update(9, camera.TwoDZone{Vertices: triangle(0)[:1]}), im.StatusConstraintError)
		_, st := invokeZones(z, zmdef.CmdCreateTwoDCartesianZone, createZoneRequest("a", triangle(0)))
		wantCode(t, "create", st, im.StatusSuccess)
		wantCode(t, "update onto Mfg's", update(1, camera.TwoDZone{Use: zmdef.ZoneUseMotion, Vertices: triangle(200)}), im.StatusAlreadyExists)
		wantCode(t, "update onto itself", update(1, camera.TwoDZone{Name: "b", Use: zmdef.ZoneUseMotion, Vertices: triangle(0)}), im.StatusSuccess)
		wantCode(t, "update beyond the max", update(1, camera.TwoDZone{Vertices: []camera.Vertex{{X: 0, Y: 481}, {X: 1, Y: 0}, {X: 0, Y: 1}}}), im.StatusDynamicConstraintError)
		wantCode(t, "update hourglass", update(1, camera.TwoDZone{Vertices: []camera.Vertex{{X: 10, Y: 10}, {X: 20, Y: 20}, {X: 20, Y: 10}, {X: 10, Y: 20}}}), im.StatusDynamicConstraintError)
		remove := func(id uint16) im.StatusCode {
			_, st := invokeZones(z, zmdef.CmdRemoveZone, zmdef.RemoveZoneRequest{ZoneId: id})
			return st
		}
		wantCode(t, "remove unknown", remove(5), im.StatusNotFound)
		wantCode(t, "remove Mfg", remove(9), im.StatusConstraintError)
		_, st = invokeZones(z, zmdef.CmdCreateOrUpdateTrigger, zmdef.CreateOrUpdateTriggerRequest{Trigger: camera.ZoneTrigger{ZoneId: 1, InitialDuration: 5, MaxDuration: 5}})
		wantCode(t, "trigger", st, im.StatusSuccess)
		wantCode(t, "remove with a trigger", remove(1), im.StatusInvalidInState)
	})
	t.Run("the trigger's checks :612-659", func(t *testing.T) {
		privacy := camera.Zone{ZoneId: 3, ZoneType: zmdef.ZoneTypeTwoDcartZone, ZoneSource: zmdef.ZoneSourceMfg,
			TwoDCartesianZone: &camera.TwoDZone{Use: zmdef.ZoneUsePrivacy, Vertices: triangle(0)}}
		motion := camera.Zone{ZoneId: 4, ZoneType: zmdef.ZoneTypeTwoDcartZone, ZoneSource: zmdef.ZoneSourceMfg,
			TwoDCartesianZone: &camera.TwoDZone{Use: zmdef.ZoneUseMotion, Vertices: triangle(50)}}
		z, m := newChipZones(t, camera.FeatureTwoDimensionalCartesianZone|camera.FeaturePerZoneSensitivity, func(cfg *camera.ZonesConfig) {
			cfg.Zones = []camera.Zone{privacy, motion}
		})
		two := uint8(2)
		set := func(tr camera.ZoneTrigger) im.StatusCode {
			_, st := invokeZones(z, zmdef.CmdCreateOrUpdateTrigger, zmdef.CreateOrUpdateTriggerRequest{Trigger: tr})
			return st
		}
		wantCode(t, "initial 0", set(camera.ZoneTrigger{ZoneId: 4, Sensitivity: &two}), im.StatusConstraintError)
		wantCode(t, "augmentation above initial", set(camera.ZoneTrigger{ZoneId: 4, InitialDuration: 5, AugmentationDuration: 6, MaxDuration: 9, Sensitivity: &two}), im.StatusConstraintError)
		wantCode(t, "max below initial", set(camera.ZoneTrigger{ZoneId: 4, InitialDuration: 5, MaxDuration: 4, Sensitivity: &two}), im.StatusConstraintError)
		wantCode(t, "no sensitivity with PERZONESENS", set(camera.ZoneTrigger{ZoneId: 4, InitialDuration: 5, MaxDuration: 5}), im.StatusInvalidCommand)
		five := uint8(5)
		wantCode(t, "sensitivity 5 of 4", set(camera.ZoneTrigger{ZoneId: 4, InitialDuration: 5, MaxDuration: 5, Sensitivity: &five}), im.StatusConstraintError)
		wantCode(t, "unknown zone", set(camera.ZoneTrigger{ZoneId: 8, InitialDuration: 5, MaxDuration: 5, Sensitivity: &two}), im.StatusNotFound)
		wantCode(t, "privacy zone", set(camera.ZoneTrigger{ZoneId: 3, InitialDuration: 5, MaxDuration: 5, Sensitivity: &two}), im.StatusConstraintError)
		wantCode(t, "create", set(camera.ZoneTrigger{ZoneId: 4, InitialDuration: 5, MaxDuration: 5, Sensitivity: &two}), im.StatusSuccess)
		wantCode(t, "update", set(camera.ZoneTrigger{ZoneId: 4, InitialDuration: 6, MaxDuration: 9, Sensitivity: &two}), im.StatusSuccess)
		if tr, ok := z.TriggerFor(4); !ok || tr.MaxDuration != 9 || len(m.triggers) != 1 || m.triggers[0].MaxDuration != 9 {
			t.Fatalf("trigger %+v, port %+v", tr, m.triggers)
		}
		_, st := invokeZones(z, zmdef.CmdRemoveTrigger, zmdef.RemoveTriggerRequest{ZoneId: 3})
		wantCode(t, "remove unknown trigger", st, im.StatusNotFound)
		_, st = invokeZones(z, zmdef.CmdRemoveTrigger, zmdef.RemoveTriggerRequest{ZoneId: 4})
		wantCode(t, "remove trigger", st, im.StatusSuccess)
		if len(z.TriggerList()) != 0 || len(m.triggers) != 0 {
			t.Fatal("trigger kept")
		}
		m.createTriggerEr = errors.New("storage full")
		wantCode(t, "the port's refusal", set(camera.ZoneTrigger{ZoneId: 4, InitialDuration: 5, MaxDuration: 5, Sensitivity: &two}), im.StatusFailure)
	})
	t.Run("Sensitivity :207-284", func(t *testing.T) {
		z, _ := newChipZones(t, 0)
		if err := z.MatterWrite(context.Background(), zmdef.AttrSensitivity, uint64(5)); statusOf(err) != im.StatusConstraintError {
			t.Fatalf("write 5 of 4: %v", err)
		}
		if err := z.SetSensitivity(0); !errors.Is(err, spec.ErrInvalidValue) {
			t.Fatalf("host 0: %v", err)
		}
		if err := z.SetSensitivity(4); err != nil || z.Sensitivity() != 4 {
			t.Fatalf("host 4: %v, %d", err, z.Sensitivity())
		}
		if err := z.SetAttributes(map[uint32]any{zmdef.AttrMaxZones: uint8(2)}); !errors.Is(err, spec.ErrNotHeld) {
			t.Fatalf("MaxZones set: %v", err)
		}
		pz, _ := newChipZones(t, camera.FeatureTwoDimensionalCartesianZone|camera.FeaturePerZoneSensitivity)
		if err := pz.SetSensitivity(2); err == nil {
			t.Fatal("Sensitivity without its attribute")
		}
	})
}
