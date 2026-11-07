// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster/spec"
	thermostatdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	tsdef "github.com/SukramJ/go-fabric/cluster/spec/timesynchronization"
	valvedef "github.com/SukramJ/go-fabric/cluster/spec/valveconfigurationandcontrol"
	"github.com/SukramJ/go-fabric/cluster/valve"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/surface"
	"github.com/SukramJ/go-fabric/tlv"
)

// timedValve is a valve StateSource that is open with a known remaining
// duration after an Open, so AutoCloseTime is served.
type timedValve struct{}

func (timedValve) CurrentState() (valvedef.ValveStateEnum, bool) {
	return valvedef.ValveStateOpen, true
}

func (timedValve) TargetState() (valvedef.ValveStateEnum, bool) { return valvedef.ValveStateOpen, true }
func (timedValve) OpenDuration() (uint32, bool)                 { return 600, true }
func (timedValve) RemainingDuration() (uint32, bool)            { return 600, true }
func (timedValve) DefaultOpenDuration() (uint32, bool)          { return 600, true }
func (timedValve) SetDefaultOpenDuration(context.Context, *uint32) error {
	return nil
}
func (timedValve) Open(context.Context, valvedef.OpenRequest) error { return nil }
func (timedValve) Close(context.Context) error                      { return nil }

// TestOneMatterTimePerNode (#428): the node hands the root
// TimeSynchronization's UTC to the Thermostat's suggestions and the
// Valve's AutoCloseTime as their endpoints join the topology, so a
// controller reads one time from the node — connectedhomeip's servers read
// the one system clock SetUTCTime sets (SystemClock.cpp:284-306,
// TimeSynchronizationCluster.cpp:856-872 at 6170af84). While the node has
// no time (its host clock is no source) a suggestion answers
// INVALID_IN_STATE and an Open leaves AutoCloseTime null; SetUTCTime moves
// the root's UTCTime, and the suggestion's EffectiveTime and the
// AutoCloseTime follow it, not the host clock — twice; the opening made
// before the time arrived gets its AutoCloseTime without a new Open, and
// it moves again with the second SetUTCTime.
func TestOneMatterTimePerNode(t *testing.T) {
	t.Parallel()
	rows := surface.NewExtensions(append([]string{"BD-chip-TimeSynchronization-Features", "BD-chip-Thermostat-Suggestions"}, valve.Ahead.IDs()...)...)
	handle := []byte("Home")
	heat := int16(2100)
	count := uint8(1)
	types := []thermostatdef.PresetTypeStruct{{PresetScenario: thermostatdef.PresetScenarioOccupied, NumberOfPresets: 1}}
	presets := []thermostatdef.PresetStruct{{PresetHandle: spec.ValueOf(handle), PresetScenario: thermostatdef.PresetScenarioOccupied, HeatingSetpoint: &heat, BuiltIn: spec.ValueOf(true)}}
	thermostat, err := device.NewThermostat(device.Options{StableKey: device.StringKey("thermostat"), Name: "Thermostat", Surface: surface.Policies{Extensions: rows}},
		thermostatdef.Declare{
			Features: thermostatdef.FeatureHeating | thermostatdef.FeaturePresets | thermostatdef.FeatureThermostatSuggestions,
			Initial:  thermostatdef.Patch{NumberOfPresets: &count, PresetTypes: &types, Presets: &presets},
		})
	if err != nil {
		t.Fatal(err)
	}
	waterValve, err := device.NewWaterValve(device.Options{StableKey: device.StringKey("valve"), Name: "Valve", Surface: surface.Policies{Extensions: rows}},
		valvedef.Declare{Features: valvedef.FeatureTimeSync, StateSource: timedValve{}})
	if err != nil {
		t.Fatal(err)
	}
	site := fabrictest.NewSite(t, fabrictest.Options{Clock: clock.System{}})
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{
		Bridged: []node.Endpoint{thermostat, waterValve},
		Configure: func(cfg *node.Config) {
			cfg.TimeSynchronization.Config = &node.TimeSynchronizationConfig{
				PlatformTime: func() (tsdef.TimeSourceEnum, tsdef.GranularityEnum) {
					return tsdef.TimeSourceNone, tsdef.GranularityNoTimeGranularity
				},
			}
			cfg.Surface.Extensions = rows
		},
	}})
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	tstatEP, _ := thermostat.EndpointNumber()
	valveEP, _ := waterValve.EndpointNumber()

	suggest := func() (im.StatusCode, error) {
		res, err := fabrictest.Invoke(ctx, pair, im.ConcreteCommandPath{Endpoint: tstatEP, Cluster: thermostatdef.ClusterID, Command: thermostatdef.CmdAddThermostatSuggestion},
			thermostatdef.AddThermostatSuggestionRequest{PresetHandle: handle, EffectiveTime: spec.Nullable[uint32]{Null: true}, ExpirationInMinutes: 30})
		return res.Status, err
	}
	autoClose := func() tlv.Element {
		t.Helper()
		v, err := fabrictest.Read(ctx, pair, client.AttributeAt(valveEP, valvedef.ClusterID, valvedef.AttrAutoCloseTime))
		if err != nil {
			t.Fatal(err)
		}
		return v.Value.El
	}
	autoCloseAfterOpen := func() tlv.Element {
		t.Helper()
		res, err := fabrictest.Invoke(ctx, pair, im.ConcreteCommandPath{Endpoint: valveEP, Cluster: valvedef.ClusterID, Command: valvedef.CmdOpen},
			valvedef.OpenRequest{OpenDuration: &spec.Nullable[uint32]{Value: 600}})
		if err != nil || res.Status != im.StatusSuccess {
			t.Fatalf("Open: %+v %v", res, err)
		}
		return autoClose()
	}
	setUTC := func(at time.Time) {
		t.Helper()
		res, err := fabrictest.Invoke(ctx, pair, im.ConcreteCommandPath{Endpoint: 0, Cluster: tsdef.ClusterID, Command: tsdef.CmdSetUtcTime},
			tsdef.SetUtcTimeRequest{UtcTime: uint64(at.UnixMicro() - 946684800_000_000), Granularity: tsdef.GranularityMillisecondsGranularity}) //nolint:gosec // after the epoch
		if err != nil || res.Status != im.StatusSuccess {
			t.Fatalf("SetUTCTime: %+v %v", res, err)
		}
	}
	near := func(epochS uint64, at time.Time) bool {
		d := int64(epochS) - (at.Unix() - 946684800) //nolint:gosec // epoch seconds of this century
		return d > -60 && d < 60
	}

	// No time: UTCTime null, the suggestion refused, AutoCloseTime null.
	if v, err := fabrictest.Read(ctx, pair, client.AttributeAt(0, tsdef.ClusterID, tsdef.AttrUtcTime)); err != nil || v.Value.El.Type != tlv.TypeNull {
		t.Fatalf("UTCTime before SetUTCTime %+v %v, want null", v.Value, err)
	}
	if st, err := suggest(); st != im.StatusInvalidInState {
		t.Errorf("a suggestion without the node's time: %v %v, want INVALID_IN_STATE", st, err)
	}
	if el := autoCloseAfterOpen(); el.Type != tlv.TypeNull {
		t.Errorf("AutoCloseTime without the node's time %+v, want null", el)
	}

	for i, at := range []time.Time{time.Now().Add(30 * 24 * time.Hour), time.Now().Add(60 * 24 * time.Hour)} {
		setUTC(at)
		v, err := fabrictest.Read(ctx, pair, client.AttributeAt(0, tsdef.ClusterID, tsdef.AttrUtcTime))
		if err != nil || !near(v.Value.El.Uint/1_000_000, at) {
			t.Fatalf("#%d: UTCTime %+v %v, want about %v", i, v.Value, err, at)
		}
		// The opening still counting down takes the moved time without a
		// new Open: once the time arrives (#0), and as it moves (#1) —
		// chip's UpdateAutoCloseTime on UTCTimeAvailabilityChanged.
		if err := site.Eventually(ctx, "AutoCloseTime follows the node's UTC", func() bool {
			el := autoClose()
			return el.Type != tlv.TypeNull && near(el.Uint/1_000_000, at.Add(600*time.Second))
		}); err != nil {
			t.Errorf("#%d: AutoCloseTime %+v without a new Open, want the node's UTC %v plus 600 s", i, autoClose(), at)
		}
		if el := autoCloseAfterOpen(); el.Type == tlv.TypeNull || !near(el.Uint/1_000_000, at.Add(600*time.Second)) {
			t.Errorf("#%d: AutoCloseTime %+v, want the node's UTC %v plus 600 s", i, el, at)
		}
		if i > 0 {
			continue
		}
		if st, err := suggest(); err != nil || st != im.StatusSuccess {
			t.Fatalf("a suggestion on the node's time: %v %v", st, err)
		}
		values, err := fabrictest.ReadCluster(ctx, pair, tstatEP, thermostatdef.ClusterID)
		if err != nil {
			t.Fatal(err)
		}
		state, _ := thermostatdef.StateFrom(values)
		if len(state.ThermostatSuggestions) != 1 || !near(uint64(state.ThermostatSuggestions[0].EffectiveTime), at) {
			t.Errorf("ThermostatSuggestions %+v, want one effective at the node's UTC %v", state.ThermostatSuggestions, at)
		}
	}
}
