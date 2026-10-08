// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package fan_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/fan"
	fandef "github.com/SukramJ/go-fabric/cluster/spec/fancontrol"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
)

// TestServerMatchesTheGeneratedDefinition holds every feature selection
// against matter.js fan-control.element.ts (spectest.CheckServer): each of
// the 64 FeatureMaps, with a sequence its Auto bit admits.
func TestServerMatchesTheGeneratedDefinition(t *testing.T) {
	t.Parallel()
	for f := range fan.Feature(64) {
		seq := fan.SequenceOffLowMedHigh
		if f&fan.FeatureAuto != 0 {
			seq = fan.SequenceOffLowMedHighAuto
		}
		srv, err := fan.NewServer(fan.Config{
			Source: &device{}, Features: f, Sequence: seq, SpeedMax: 10,
			RockSupport: fan.RockLeftRight, WindSupport: fan.WindSleep,
		})
		if err != nil {
			t.Fatalf("features 0x%X: %v", f, err)
		}
		spectest.CheckServer(t, srv, fandef.Definition, uint32(f))
	}
}

// TestStepTakesTheGeneratedRequest pins the in-process shape: the
// generated StepRequest is carried over with the element defaults (Wrap
// false, LowestOff true) for its absent fields.
func TestStepTakesTheGeneratedRequest(t *testing.T) {
	t.Parallel()
	d := &device{}
	srv := newFull(t, d)
	if _, err := srv.MatterInvoke(context.Background(), fan.CmdStep, fandef.StepRequest{Direction: fandef.StepDirectionIncrease}); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if got := d.last(); got.SpeedSetting == nil || got.SpeedSetting.Value != 1 {
		t.Errorf("Step from 0 resolved to %+v, want SpeedSetting 1", got)
	}
	no, yes := false, true
	req := fandef.StepRequest{Direction: fandef.StepDirectionDecrease, Wrap: &yes, LowestOff: &no}
	if _, err := srv.MatterInvoke(context.Background(), fan.CmdStep, req); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if got := d.last(); got.SpeedSetting == nil || got.SpeedSetting.Value != 10 {
		t.Errorf("Step down with Wrap and without LowestOff from 0 resolved to %+v, want SpeedSetting 10", got)
	}
}

// refusingDevice is a device whose three setters fail.
type refusingDevice struct{ device }

var errRefused = errors.New("refused")

func (*refusingDevice) SetRockSetting(context.Context, fan.RockBitmap) error { return errRefused }

func (*refusingDevice) SetWindSetting(context.Context, fan.WindBitmap) error { return errRefused }

func (*refusingDevice) SetAirflowDirection(context.Context, fan.AirflowDirection) error {
	return errRefused
}

// TestSetterRefusalReachesTheController pins that a host refusing a
// checked RockSetting, WindSetting or AirflowDirection write fails the
// write instead of reporting success.
func TestSetterRefusalReachesTheController(t *testing.T) {
	t.Parallel()
	srv, err := fan.NewServer(fan.Config{
		Source: &refusingDevice{}, Features: fan.FeatureRocking | fan.FeatureWind | fan.FeatureAirflowDirection,
		Sequence: fan.SequenceOffHigh, RockSupport: fan.RockLeftRight, WindSupport: fan.WindSleep,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []struct {
		attr  uint32
		value any
	}{{fan.AttrRockSetting, uint8(1)}, {fan.AttrWindSetting, uint8(1)}, {fan.AttrAirflowDirection, uint8(1)}} {
		if err := srv.MatterWrite(context.Background(), w.attr, w.value); !errors.Is(err, errRefused) {
			t.Errorf("attribute 0x%04X: %v, want the host's refusal", w.attr, err)
		}
	}
}
