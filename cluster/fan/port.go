// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fan

import (
	"context"

	fandef "github.com/SukramJ/go-fabric/cluster/spec/fancontrol"
	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
)

// portSource is the host port in the generated types — fandef.StateSource
// and, optionally, fandef.Stepper, what fancontroldef.Declare carries — as
// the [StateSource] the server reads and drives: FanState is the generated
// State, every setting the server applies one ApplyFanSettings with a
// Patch, the rock, wind and airflow settings included.
type portSource struct{ p fandef.StateSource }

// portStepper is a [portSource] whose host also steps ([Stepper]).
type portStepper struct {
	portSource
	step fandef.Stepper
}

var (
	_ StateSource            = portSource{}
	_ RockSetter             = portSource{}
	_ WindSetter             = portSource{}
	_ AirflowDirectionSetter = portSource{}
	_ Stepper                = portStepper{}
)

// sourceOf returns the port as the server's source.
func sourceOf(p fandef.StateSource, step fandef.Stepper) StateSource {
	if step != nil {
		return portStepper{portSource{p}, step}
	}
	return portSource{p}
}

// FanState implements [StateSource].
func (s portSource) FanState() State {
	st := s.p.FanState()
	return State{
		FanMode: st.FanMode, PercentSetting: st.PercentSetting, PercentCurrent: st.PercentCurrent,
		SpeedSetting: st.SpeedSetting, SpeedCurrent: st.SpeedCurrent,
		RockSetting: st.RockSetting, WindSetting: st.WindSetting, AirflowDirection: st.AirflowDirection,
	}
}

// ApplyFanSettings implements [StateSource]: the settings as a Patch, a
// null setting in Null.
func (s portSource) ApplyFanSettings(ctx context.Context, set Settings) error {
	p := fandef.Patch{FanMode: set.FanMode}
	setting := func(v *Setting, attr uint32) *uint8 {
		switch {
		case v == nil:
			return nil
		case v.Null:
			p.Null = append(p.Null, attr)
			return nil
		default:
			value := v.Value
			return &value
		}
	}
	p.PercentSetting = setting(set.PercentSetting, fandef.AttrPercentSetting)
	p.SpeedSetting = setting(set.SpeedSetting, fandef.AttrSpeedSetting)
	return s.p.ApplyFanSettings(ctx, p)
}

// SetRockSetting implements [RockSetter].
func (s portSource) SetRockSetting(ctx context.Context, rock RockBitmap) error {
	return s.p.ApplyFanSettings(ctx, fandef.Patch{RockSetting: &rock})
}

// SetWindSetting implements [WindSetter].
func (s portSource) SetWindSetting(ctx context.Context, wind WindBitmap) error {
	return s.p.ApplyFanSettings(ctx, fandef.Patch{WindSetting: &wind})
}

// SetAirflowDirection implements [AirflowDirectionSetter].
func (s portSource) SetAirflowDirection(ctx context.Context, dir AirflowDirection) error {
	return s.p.ApplyFanSettings(ctx, fandef.Patch{AirflowDirection: &dir})
}

// Step implements [Stepper].
func (s portStepper) Step(ctx context.Context, req fandef.StepRequest) error {
	return s.step.Step(ctx, req)
}

// stepRequestOf is the decoded Step as the generated request a [Stepper]
// takes.
func stepRequestOf(req clusterwire.FanStepRequest) fandef.StepRequest {
	wrap, lowestOff := req.Wrap, req.LowestOff
	return fandef.StepRequest{Direction: fandef.StepDirectionEnum(req.Direction), Wrap: &wrap, LowestOff: &lowestOff}
}
