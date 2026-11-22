// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cluster_test

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	fandef "thingfabric.dev/go-fabric/cluster/spec/fancontrol"
	pumpdef "thingfabric.dev/go-fabric/cluster/spec/pumpconfigurationandcontrol"
	smokedef "thingfabric.dev/go-fabric/cluster/spec/smokecoalarm"
	tdef "thingfabric.dev/go-fabric/cluster/spec/thermostat"
	valvedef "thingfabric.dev/go-fabric/cluster/spec/valveconfigurationandcontrol"
	wcdef "thingfabric.dev/go-fabric/cluster/spec/windowcovering"
	"thingfabric.dev/go-fabric/cluster/valve"
	"thingfabric.dev/go-fabric/contract"
)

// vetoedServer is a self-held server whose writes run the host's veto.
type vetoedServer interface {
	contract.ClusterServer
	contract.ClusterAttributeLister
	SetBeforeWrite(fn func(ctx context.Context, attrID uint32, value any) error)
}

// valveHost is a ValveConfigurationAndControl host with Level that keeps
// both defaults it is given.
type valveHost struct {
	duration *uint32
	level    uint8
}

func (*valveHost) CurrentState() (valvedef.ValveStateEnum, bool) {
	return valvedef.ValveStateClosed, true
}
func (*valveHost) TargetState() (valvedef.ValveStateEnum, bool)     { return 0, false }
func (*valveHost) OpenDuration() (uint32, bool)                     { return 0, false }
func (*valveHost) RemainingDuration() (uint32, bool)                { return 0, false }
func (*valveHost) Open(context.Context, valvedef.OpenRequest) error { return nil }
func (*valveHost) Close(context.Context) error                      { return nil }
func (*valveHost) CurrentLevel() (uint8, bool)                      { return 0, true }
func (*valveHost) TargetLevel() (uint8, bool)                       { return 0, false }
func (h *valveHost) DefaultOpenLevel() uint8                        { return h.level }

func (h *valveHost) DefaultOpenDuration() (uint32, bool) {
	if h.duration == nil {
		return 0, false
	}
	return *h.duration, true
}

func (h *valveHost) SetDefaultOpenDuration(_ context.Context, s *uint32) error {
	h.duration = s
	return nil
}

func (h *valveHost) SetDefaultOpenLevel(_ context.Context, p uint8) error {
	h.level = p
	return nil
}

func newValveServer() *valve.Server {
	srv, err := valve.New(valve.Config{Source: &valveHost{level: 100}, Features: valve.FeatureLevel, Attributes: []uint32{valvedef.AttrDefaultOpenLevel}})
	if err != nil {
		panic(err)
	}
	return srv
}

// vetoServers are the self-held servers that run the host's veto
// themselves, each with a write per writable scalar attribute it serves
// that changes the held value; skip names the ones no write of this
// configuration can change, with the reason.
var vetoServers = []struct {
	name   string
	build  func() vetoedServer
	writes map[uint32]any
	skip   map[uint32]string
}{
	{"FanControl", func() vetoedServer { return newFanServer() }, map[uint32]any{
		fandef.AttrFanMode: uint64(1), fandef.AttrPercentSetting: uint64(50),
		fandef.AttrSpeedSetting: uint64(2), fandef.AttrRockSetting: uint64(1),
	}, nil},
	{"PumpConfigurationAndControl", func() vetoedServer { return newPumpServer() }, map[uint32]any{
		pumpdef.AttrControlMode: uint64(1), pumpdef.AttrLifetimeRunningHours: uint64(5),
	}, map[uint32]string{pumpdef.AttrOperationMode: "Normal is the only mode without SPD or LOCAL"}},
	{"SmokeCoAlarm", func() vetoedServer { return newSmokeAlarmServer() }, map[uint32]any{
		smokedef.AttrSmokeSensitivityLevel: uint64(2),
	}, nil},
	{"WindowCovering", func() vetoedServer { return newWindowCoveringServer() }, map[uint32]any{
		wcdef.AttrMode: uint64(wcdef.ModeMaintenanceMode | wcdef.ModeCalibrationMode), // stored as MaintenanceMode alone
	}, nil},
	{"ValveConfigurationAndControl", func() vetoedServer { return newValveServer() }, map[uint32]any{
		valvedef.AttrDefaultOpenDuration: uint64(30), valvedef.AttrDefaultOpenLevel: uint64(50),
	}, nil},
	{"Thermostat", func() vetoedServer { return newHeatOnlyServer() }, map[uint32]any{
		tdef.AttrOccupiedHeatingSetpoint: int64(2100), tdef.AttrMinHeatSetpointLimit: int64(800),
		tdef.AttrMaxHeatSetpointLimit: int64(2900), tdef.AttrSystemMode: uint64(tdef.SystemModeOff),
		tdef.AttrLocalTemperatureCalibration: int64(10),
	}, map[uint32]string{tdef.AttrControlSequenceOfOperation: "derived from the features; only its held value is admitted"}},
}

// TestEverySelfHeldVetoHearsTheStoredForm (R2-P6-api-1505): a self-held
// server hands its host's veto the value in its stored form, as
// [spec.WriteVeto.Check] promises — the Go type its MatterRead answers
// after the write. Each server lists a write for every writable scalar
// attribute it serves, so an attribute added to one of them is a failure
// here until it has a case.
func TestEverySelfHeldVetoHearsTheStoredForm(t *testing.T) {
	t.Parallel()
	for _, sc := range vetoServers {
		t.Run(sc.name, func(t *testing.T) {
			t.Parallel()
			probe := sc.build()
			def := spec.Lookup(probe.MatterClusterID())
			for _, id := range probe.MatterAttributes() {
				a := def.Attribute(id)
				if a == nil || !a.Access.Writable() || a.Type.Kind == spec.KindList || a.Type.Kind == spec.KindStruct {
					continue
				}
				_, listed := sc.writes[id]
				if _, skipped := sc.skip[id]; !listed && !skipped {
					t.Errorf("writable attribute %s (0x%04X) has no case", a.Name, id)
				}
			}
			for _, id := range slices.Sorted(maps.Keys(sc.writes)) {
				srv := sc.build()
				var heard any
				calls := 0
				srv.SetBeforeWrite(func(_ context.Context, attrID uint32, value any) error {
					if attrID == id {
						heard, calls = value, calls+1
					}
					return nil
				})
				if err := srv.MatterWrite(context.Background(), id, sc.writes[id]); err != nil {
					t.Errorf("attribute 0x%04X: write: %v", id, err)
					continue
				}
				if calls != 1 {
					t.Errorf("attribute 0x%04X: the veto ran %d times, want once", id, calls)
					continue
				}
				read, _ := srv.MatterRead(id)
				if reflect.TypeOf(heard) != reflect.TypeOf(read) || fmt.Sprint(heard) != fmt.Sprint(read) {
					t.Errorf("attribute 0x%04X: the veto heard %T %v, a read answers %T %v", id, heard, heard, read, read)
				}
			}
		})
	}
}
