// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fan_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/fan"
	fandef "github.com/SukramJ/go-fabric/cluster/spec/fancontrol"
	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
)

// genPort is a fan host port in the generated types: its state, and every
// Patch the server applied.
type genPort struct {
	state   fandef.State
	patches []fandef.Patch
	steps   []fandef.StepRequest
}

func (p *genPort) FanState() fandef.State { return p.state }

func (p *genPort) ApplyFanSettings(_ context.Context, patch fandef.Patch) error {
	p.patches = append(p.patches, patch)
	return nil
}

func (p *genPort) Step(_ context.Context, req fandef.StepRequest) error {
	p.steps = append(p.steps, req)
	return nil
}

// TestPortInTheGeneratedTypes: a host port in the generated types
// (Config.Port, Config.Stepper — what fancontroldef.Declare carries)
// receives every setting as a Patch, a null setting in Null, the rock,
// wind and airflow settings too, and the Step command as the generated
// request.
func TestPortInTheGeneratedTypes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	p := &genPort{state: fandef.State{FanMode: fandef.FanModeLow, PercentCurrent: 33}}
	srv, err := fan.NewServer(fan.Config{
		Port: p, Stepper: p,
		Features:    fandef.FeatureRocking | fandef.FeatureWind | fandef.FeatureAirflowDirection | fandef.FeatureStep | fandef.FeatureAuto,
		Sequence:    fandef.FanModeSequenceOffLowMedHighAuto,
		RockSupport: fandef.RockRockLeftRight, WindSupport: fandef.WindSleepWind,
	})
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := srv.MatterRead(fan.AttrPercentCurrent); v != uint8(33) && v != uint64(33) {
		t.Errorf("PercentCurrent %v, want the port's 33", v)
	}
	writes := []struct {
		attr  uint32
		value any
	}{
		{fan.AttrFanMode, uint64(fan.FanModeAuto)},
		{fan.AttrPercentSetting, uint64(40)},
		{fandef.AttrRockSetting, uint64(fandef.RockRockLeftRight)},
		{fandef.AttrWindSetting, uint64(fandef.WindSleepWind)},
		{fandef.AttrAirflowDirection, uint64(fandef.AirflowDirectionReverse)},
	}
	for _, w := range writes {
		if err := srv.MatterWrite(ctx, w.attr, w.value); err != nil {
			t.Fatalf("write 0x%04X: %v", w.attr, err)
		}
	}
	if len(p.patches) != len(writes) {
		t.Fatalf("the port saw %d patches, want %d: %+v", len(p.patches), len(writes), p.patches)
	}
	if auto := p.patches[0]; auto.FanMode == nil || len(auto.Null) == 0 {
		t.Errorf("FanMode Auto as %+v, want the mode and the nulled settings", auto)
	}
	if pct := p.patches[1]; pct.PercentSetting == nil || *pct.PercentSetting != 40 {
		t.Errorf("PercentSetting 40 as %+v", pct)
	}
	if p.patches[2].RockSetting == nil || p.patches[3].WindSetting == nil || p.patches[4].AirflowDirection == nil {
		t.Errorf("rock, wind, airflow as %+v", p.patches[2:])
	}
	if _, err := srv.MatterInvoke(ctx, fan.CmdStep, clusterwire.FanStepRequest{Direction: 0, Wrap: true}); err != nil {
		t.Fatal(err)
	}
	if len(p.steps) != 1 || p.steps[0].Wrap == nil || !*p.steps[0].Wrap {
		t.Errorf("the Stepper saw %+v, want one Step with Wrap", p.steps)
	}
}
