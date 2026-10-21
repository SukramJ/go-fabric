// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package levelcontrol

import (
	"context"

	"github.com/SukramJ/go-fabric/cluster/spec"
	lvldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	"github.com/SukramJ/go-fabric/cluster/wire"
)

// portSource is a [lvldef.LevelSource] — the port in the generated request
// types, what levelcontroldef.Declare carries — as the [LevelSource] the
// server drives: each request converted to the generated form, field by
// field.
type portSource struct{ p lvldef.LevelSource }

var _ LevelSource = portSource{}

func (s portSource) CurrentLevel() (uint8, bool) { return s.p.CurrentLevel() }
func (s portSource) Options() uint8              { return uint8(s.p.Options()) }
func (s portSource) OnLevel() (uint8, bool)      { return s.p.OnLevel() }

func (s portSource) SetOptions(ctx context.Context, options uint8) error {
	return s.p.SetOptions(ctx, lvldef.OptionsBitmap(options))
}

func (s portSource) SetOnLevel(ctx context.Context, level *uint8) error {
	return s.p.SetOnLevel(ctx, level)
}

func (s portSource) MoveToLevel(ctx context.Context, req wire.MoveToLevelRequest) error {
	return s.p.MoveToLevel(ctx, moveToLevelOf(req))
}

func (s portSource) MoveToLevelWithOnOff(ctx context.Context, req wire.MoveToLevelRequest) error {
	return s.p.MoveToLevelWithOnOff(ctx, lvldef.MoveToLevelWithOnOffRequest(moveToLevelOf(req)))
}

func (s portSource) Move(ctx context.Context, req wire.MoveRequest) error {
	return s.p.Move(ctx, moveOf(req))
}

func (s portSource) MoveWithOnOff(ctx context.Context, req wire.MoveRequest) error {
	return s.p.MoveWithOnOff(ctx, lvldef.MoveWithOnOffRequest(moveOf(req)))
}

func (s portSource) Step(ctx context.Context, req wire.StepRequest) error {
	return s.p.Step(ctx, stepOf(req))
}

func (s portSource) StepWithOnOff(ctx context.Context, req wire.StepRequest) error {
	return s.p.StepWithOnOff(ctx, lvldef.StepWithOnOffRequest(stepOf(req)))
}

func (s portSource) Stop(ctx context.Context, req wire.StopRequest) error {
	return s.p.Stop(ctx, stopOf(req))
}

func (s portSource) StopWithOnOff(ctx context.Context, req wire.StopRequest) error {
	return s.p.StopWithOnOff(ctx, lvldef.StopWithOnOffRequest(stopOf(req)))
}

func nullableOf[T any](p *T) spec.Nullable[T] {
	if p == nil {
		return spec.Nullable[T]{Null: true}
	}
	return spec.Nullable[T]{Value: *p}
}

func moveToLevelOf(r wire.MoveToLevelRequest) lvldef.MoveToLevelRequest {
	return lvldef.MoveToLevelRequest{
		Level: r.Level, TransitionTime: nullableOf(r.TransitionTime),
		OptionsMask: lvldef.OptionsBitmap(r.OptionsMask), OptionsOverride: lvldef.OptionsBitmap(r.OptionsOverride),
	}
}

func moveOf(r wire.MoveRequest) lvldef.MoveRequest {
	return lvldef.MoveRequest{
		MoveMode: lvldef.MoveModeEnum(r.MoveMode), Rate: nullableOf(r.Rate),
		OptionsMask: lvldef.OptionsBitmap(r.OptionsMask), OptionsOverride: lvldef.OptionsBitmap(r.OptionsOverride),
	}
}

func stepOf(r wire.StepRequest) lvldef.StepRequest {
	return lvldef.StepRequest{
		StepMode: lvldef.StepModeEnum(r.StepMode), StepSize: r.StepSize, TransitionTime: nullableOf(r.TransitionTime),
		OptionsMask: lvldef.OptionsBitmap(r.OptionsMask), OptionsOverride: lvldef.OptionsBitmap(r.OptionsOverride),
	}
}

func stopOf(r wire.StopRequest) lvldef.StopRequest {
	return lvldef.StopRequest{OptionsMask: lvldef.OptionsBitmap(r.OptionsMask), OptionsOverride: lvldef.OptionsBitmap(r.OptionsOverride)}
}
