// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/cover"
	"github.com/SukramJ/go-fabric/cluster/levelcontrol"
	"github.com/SukramJ/go-fabric/cluster/light"
	"github.com/SukramJ/go-fabric/cluster/lock"
	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/cluster/spec"
	lockdef "github.com/SukramJ/go-fabric/cluster/spec/doorlock"
	levelcontroldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	swdef "github.com/SukramJ/go-fabric/cluster/spec/switchcluster"
	wcdef "github.com/SukramJ/go-fabric/cluster/spec/windowcovering"
	switchserver "github.com/SukramJ/go-fabric/cluster/switch"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/contract"
)

// TestSelfHeldDefaults: every cluster of the kit's list of self-held
// defaults mounts its self-held server on a device type that mandates it
// (with the feature selection its server serves), and the server lists the
// commands the selection makes mandatory — the generated stub listed none
// (gate 1, G1-1).
func TestSelfHeldDefaults(t *testing.T) {
	t.Parallel()
	cases := []struct {
		cluster uint32
		build   func() (*Device, error)
		want    any
	}{
		{0x0006, func() (*Device, error) { return New(&OnOffLightType, testOptions("oo")) }, (*onoff.Server)(nil)},
		{0x0008, func() (*Device, error) { return New(&DimmableLightType, testOptions("lvl")) }, (*levelcontrol.Server)(nil)},
		{0x0300, func() (*Device, error) { return New(&ColorTemperatureLightType, testOptions("cc")) }, (*light.ColorControlServer)(nil)},
		{0x001E, func() (*Device, error) { return New(&OnOffLightSwitchType, testOptions("bind")) }, (*core.Binding)(nil)},
		{0x0750, func() (*Device, error) { return New(&BridgedNodeType, testOptions("eco"), WithOptional(0x0750)) }, (*core.EcosystemInformation)(nil)},
		{0x003B, func() (*Device, error) {
			return New(&GenericSwitchType, testOptions("sw"), WithFeatures(0x003B, uint32(swdef.FeatureMomentarySwitch)))
		}, (*switchserver.Server)(nil)},
		{0x0101, func() (*Device, error) {
			return New(&DoorLockType, testOptions("dl"), WithFeatures(0x0101, uint32(lockdef.FeatureUnbolting)))
		}, (*lock.DoorLockServer)(nil)},
		{0x0102, func() (*Device, error) {
			return New(&WindowCoveringType, testOptions("wc"), WithFeatures(0x0102, cover.ServedFeatureMap))
		}, (*cover.WindowCoveringServer)(nil)},
		{0x0201, func() (*Device, error) {
			return New(&ThermostatType, testOptions("th"), WithFeatures(0x0201, thermo.ThermostatFeatureHEAT))
		}, (*thermo.ThermostatServer)(nil)},
	}
	covered := map[uint32]bool{}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("0x%04X", tc.cluster), func(t *testing.T) {
			t.Parallel()
			d, err := tc.build()
			if err != nil {
				t.Fatal(err)
			}
			srv := d.Server(tc.cluster)
			if reflect.TypeOf(srv) != reflect.TypeOf(tc.want) {
				t.Fatalf("mounts %T, want the self-held %T", srv, tc.want)
			}
			req, _ := d.Type().Server(tc.cluster)
			fm, _ := srv.MatterRead(spec.AttrFeatureMap)
			features, _ := fm.(uint32)
			inst, err := spec.New(req.Definition, spec.Options{Features: features})
			if err != nil {
				t.Fatal(err)
			}
			var got []uint32 // a server without a lister accepts no command
			if l, ok := srv.(contract.ClusterCommandLister); ok {
				got = l.MatterAcceptedCommands()
			}
			for _, id := range inst.MatterAcceptedCommands() {
				if !slices.Contains(got, id) {
					t.Errorf("AcceptedCommandList %v lacks command 0x%02X the selection 0x%X accepts", got, id, features)
				}
			}
		})
		covered[tc.cluster] = true
	}
	for id := range selfHeldDefaults {
		if !covered[id] {
			t.Errorf("self-held default 0x%04X has no case", id)
		}
	}
}

// TestSelfHeldDefaultKeepsTheGeneratedServerForASelectionItDoesNotServe:
// WindowCovering's default selection (Lift without PositionAware) and a
// DoorLock without features are selections their self-held servers do not
// serve, so the cluster keeps the generated server, as before.
func TestSelfHeldDefaultKeepsTheGeneratedServerForASelectionItDoesNotServe(t *testing.T) {
	t.Parallel()
	wc, err := New(&WindowCoveringType, testOptions("wc"), WithFeatures(0x0102, uint32(wcdef.FeatureLift)))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := wc.Server(0x0102).(*spec.Server); !ok {
		t.Errorf("WindowCovering Lift mounts %T, want the generated server", wc.Server(0x0102))
	}
	dl, err := New(&DoorLockType, testOptions("dl"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := dl.Server(0x0101).(*spec.Server); !ok {
		t.Errorf("DoorLock without features mounts %T, want the generated server", dl.Server(0x0101))
	}
}

// TestDimmableLightCouplesItsServers: the held LevelControl of a
// DimmableLight follows its OnOff — MoveToLevelWithOnOff switches it on,
// OnOff turning on moves CurrentLevel to OnLevel — as matter.js's
// DimmableLightDevice's LevelControlServer and OnOffServer do.
func TestDimmableLightCouplesItsServers(t *testing.T) {
	t.Parallel()
	d, err := NewDimmableLight(testOptions("dim"), WithInitial(0x0008, map[uint32]any{0x0011: uint8(80)}))
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	if _, err := d.Server(0x0008).MatterInvoke(ctx, 0x04, levelcontrolMoveWithOnOff(150)); err != nil {
		t.Fatal(err)
	}
	if !d.OnOff().Get().OnOff {
		t.Error("MoveToLevelWithOnOff left the light off")
	}
	if lvl := d.LevelControl().Get().CurrentLevel; lvl == nil || *lvl != 150 {
		t.Errorf("CurrentLevel %v, want 150", lvl)
	}
	if _, err := d.Server(0x0006).MatterInvoke(ctx, 0x00, nil); err != nil { // Off
		t.Fatal(err)
	}
	if _, err := d.Server(0x0006).MatterInvoke(ctx, 0x01, nil); err != nil { // On
		t.Fatal(err)
	}
	if lvl := d.LevelControl().Get().CurrentLevel; lvl == nil || *lvl != 80 {
		t.Errorf("CurrentLevel after On %v, want OnLevel 80", lvl)
	}
}

// levelcontrolMoveWithOnOff is a MoveToLevelWithOnOff request to level,
// at once.
func levelcontrolMoveWithOnOff(level uint8) levelcontroldef.MoveToLevelWithOnOffRequest {
	return levelcontroldef.MoveToLevelWithOnOffRequest{Level: level, TransitionTime: spec.NullOf[uint16]()}
}
