// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package boolcfg_test

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/boolcfg"
	def "github.com/SukramJ/go-fabric/cluster/spec/booleanstateconfiguration"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/internal/paritytest"
)

// TestParityMatterJS_BooleanStateConfigurationServers builds every feature
// selection and optional set and holds each server against the matter.js
// conformance of its definition (spectest.CheckServer): FeatureMap,
// ClusterRevision, the attribute, command and event lists, the privileges
// and the read-only writes. A selection with SPRS but neither VIS nor AUD
// breaks SPRS's "[VIS | AUD]" and is refused.
func TestParityMatterJS_BooleanStateConfigurationServers(t *testing.T) {
	t.Parallel()
	for features := range boolcfg.Feature(1 << 5) {
		for optional := range boolcfg.Optional(1 << 3) {
			srv, err := boolcfg.New(boolcfg.Config{Features: features, Optional: optional, SupportedSensitivityLevels: 3})
			sprsOnly := features&boolcfg.FeatureAlarmSuppress != 0 && features&(boolcfg.FeatureVisual|boolcfg.FeatureAudible) == 0
			if sprsOnly {
				if !errors.Is(err, boolcfg.ErrInvalidConfig) {
					t.Errorf("0b%05b: SPRS without VIS or AUD accepted (%v)", features, err)
				}
				continue
			}
			if err != nil {
				t.Fatalf("0b%05b 0b%03b: %v", features, optional, err)
			}
			spectest.CheckServer(t, srv, def.Definition, uint32(features))
		}
	}
}

// TestParityMatterJS_BooleanStateConfigurationSnapshot pins the snapshot
// facts the server rests on: revision 2, the writable sensitivity level
// and its bound, the two status-only commands and their conformance, and
// the event conformance.
func TestParityMatterJS_BooleanStateConfigurationSnapshot(t *testing.T) {
	t.Parallel()
	js := paritytest.ClusterSnapshot(t, boolcfg.ClusterID)
	if js.Revision != def.Revision {
		t.Errorf("revision %d, generated %d", js.Revision, def.Revision)
	}
	if a := js.Attribute(t, "CurrentSensitivityLevel"); a.Conformance != "SENSLVL" || a.Constraint != "max supportedSensitivityLevels - 1" {
		t.Errorf("CurrentSensitivityLevel %+v", a)
	}
	if a := js.Attribute(t, "SupportedSensitivityLevels"); a.Constraint != "2 to 10" {
		t.Errorf("SupportedSensitivityLevels %+v", a)
	}
	if c := js.Command(t, "SuppressAlarm"); c.Response != "status" || c.Conformance != "SPRS" {
		t.Errorf("SuppressAlarm %+v", c)
	}
	if c := js.Command(t, "EnableDisableAlarm"); c.Response != "status" || c.Conformance != "VIS | AUD" {
		t.Errorf("EnableDisableAlarm %+v", c)
	}
	spectest.RoundTripEvent(t, def.Definition, def.EventAlarmsStateChanged, def.AlarmsStateChangedEvent{AlarmsActive: def.AlarmModeVisual, AlarmsSuppressed: spectest.Ptr(def.AlarmModeVisual)})
	spectest.RoundTripEvent(t, def.Definition, def.EventSensorFault, def.SensorFaultEvent{SensorFault: def.SensorFaultGeneralFault})
}
