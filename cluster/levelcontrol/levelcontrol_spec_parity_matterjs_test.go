// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package levelcontrol_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/levelcontrol"
	"github.com/SukramJ/go-fabric/cluster/spec"
	lvldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
)

// TestServerMatchesTheGeneratedDefinition holds both selections the server
// serves, OO and OO | LT, against matter.js level-control.element.ts
// (spectest.CheckServer).
func TestServerMatchesTheGeneratedDefinition(t *testing.T) {
	t.Parallel()
	for _, lighting := range []bool{false, true} {
		srv := levelcontrol.NewServer(levelcontrol.Config{Source: &fakeHost{}, Lighting: lighting})
		want := levelcontrol.FeatureOnOff
		if lighting {
			want |= levelcontrol.FeatureLighting
		}
		spectest.CheckServer(t, srv, lvldef.Definition, want)
	}
}

// TestCommandsTakeTheGeneratedRequests pins that an in-process caller may
// hand over the generated request structs of all eight commands, and the
// host receives the cluster/wire struct with a null carried over as nil.
// (The bridge keeps its lenient decoding of all eight, for Google Home's
// absent TransitionTime.)
func TestCommandsTakeTheGeneratedRequests(t *testing.T) {
	t.Parallel()
	tt := spec.ValueOf[uint16](20)
	rate := spec.ValueOf[uint8](5)
	null16, null8 := spec.NullOf[uint16](), spec.NullOf[uint8]()
	twenty, five := uint16(20), uint8(5)
	cases := []struct {
		cmd    uint32
		fields any
		call   string
		want   any
	}{
		{
			levelcontrol.CmdMoveToLevel,
			lvldef.MoveToLevelRequest{Level: 7, TransitionTime: tt, OptionsMask: 1, OptionsOverride: 1},
			"MoveToLevel",
			levelcontrol.MoveToLevelRequest{Level: 7, TransitionTime: &twenty, OptionsMask: 1, OptionsOverride: 1},
		},
		{
			levelcontrol.CmdMoveToLevelWithOnOff,
			lvldef.MoveToLevelWithOnOffRequest{Level: 7, TransitionTime: null16},
			"MoveToLevelWithOnOff",
			levelcontrol.MoveToLevelRequest{Level: 7},
		},
		{
			levelcontrol.CmdMove,
			lvldef.MoveRequest{MoveMode: lvldef.MoveModeDown, Rate: rate},
			"Move",
			levelcontrol.MoveRequest{MoveMode: levelcontrol.MoveModeDown, Rate: &five},
		},
		{
			levelcontrol.CmdMoveWithOnOff,
			lvldef.MoveWithOnOffRequest{MoveMode: lvldef.MoveModeUp, Rate: null8},
			"MoveWithOnOff",
			levelcontrol.MoveRequest{MoveMode: levelcontrol.MoveModeUp},
		},
		{
			levelcontrol.CmdStep,
			lvldef.StepRequest{StepMode: lvldef.StepModeDown, StepSize: 3, TransitionTime: tt},
			"Step",
			levelcontrol.StepRequest{StepMode: levelcontrol.StepModeDown, StepSize: 3, TransitionTime: &twenty},
		},
		{
			levelcontrol.CmdStepWithOnOff,
			lvldef.StepWithOnOffRequest{StepSize: 3, TransitionTime: null16, OptionsMask: 1},
			"StepWithOnOff",
			levelcontrol.StepRequest{StepSize: 3, OptionsMask: 1},
		},
		{
			levelcontrol.CmdStop,
			lvldef.StopRequest{OptionsMask: 1, OptionsOverride: 1},
			"Stop",
			levelcontrol.StopRequest{OptionsMask: 1, OptionsOverride: 1},
		},
		{
			levelcontrol.CmdStopWithOnOff,
			lvldef.StopWithOnOffRequest{},
			"StopWithOnOff",
			levelcontrol.StopRequest{},
		},
	}
	for _, tc := range cases {
		host := &fakeHost{}
		srv := levelcontrol.NewServer(levelcontrol.Config{Source: host})
		if _, err := srv.MatterInvoke(context.Background(), tc.cmd, tc.fields); err != nil {
			t.Fatalf("%s: %v", tc.call, err)
		}
		if len(host.calls) != 1 || host.calls[0] != tc.call {
			t.Fatalf("%s: host calls %v", tc.call, host.calls)
		}
		if !reflect.DeepEqual(host.payloads[0], tc.want) {
			t.Errorf("%s: host received %+v, want %+v", tc.call, host.payloads[0], tc.want)
		}
	}
}
