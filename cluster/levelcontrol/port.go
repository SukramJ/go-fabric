// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package levelcontrol

import (
	"context"

	"github.com/SukramJ/go-fabric/cluster/spec"
	lvldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
)

// The host port speaks the generated requests ([LevelSource]); the
// server's engine drives the cluster/wire form (source). portSource
// carries a host's port over, field by field; the module's [NativeSource]
// implements both and is driven directly.

// sourceOf returns the source the server drives for src: the module's own
// source as it is, a host's port through portSource (keeping its change
// notification), nil for nil.
func sourceOf(src LevelSource) source {
	switch s := src.(type) {
	case nil:
		return nil
	case interface{ wireSource() source }:
		return s.wireSource()
	case contract.ChangeNotifier:
		return notifyingPortSource{portSource{src}, s}
	default:
		return portSource{src}
	}
}

// portSource is a host's [LevelSource] as the source the server drives.
type portSource struct{ p LevelSource }

// notifyingPortSource is a portSource whose port notifies its changes.
type notifyingPortSource struct {
	portSource
	n contract.ChangeNotifier
}

var (
	_ source                  = portSource{}
	_ contract.ChangeNotifier = notifyingPortSource{}
)

// OnMatterValueChanged implements [contract.ChangeNotifier] through the
// port's own.
func (s notifyingPortSource) OnMatterValueChanged(cb func()) (unsubscribe func()) {
	return s.n.OnMatterValueChanged(cb)
}

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

func pointerOf[T any](n spec.Nullable[T]) *T {
	if n.Null {
		return nil
	}
	v := n.Value
	return &v
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

func wireMoveToLevel(r lvldef.MoveToLevelRequest) wire.MoveToLevelRequest {
	return wire.MoveToLevelRequest{
		Level: r.Level, TransitionTime: pointerOf(r.TransitionTime),
		OptionsMask: uint8(r.OptionsMask), OptionsOverride: uint8(r.OptionsOverride),
	}
}

func wireMove(r lvldef.MoveRequest) wire.MoveRequest {
	return wire.MoveRequest{
		MoveMode: uint8(r.MoveMode), Rate: pointerOf(r.Rate),
		OptionsMask: uint8(r.OptionsMask), OptionsOverride: uint8(r.OptionsOverride),
	}
}

func wireStep(r lvldef.StepRequest) wire.StepRequest {
	return wire.StepRequest{
		StepMode: uint8(r.StepMode), StepSize: r.StepSize, TransitionTime: pointerOf(r.TransitionTime),
		OptionsMask: uint8(r.OptionsMask), OptionsOverride: uint8(r.OptionsOverride),
	}
}

func wireStop(r lvldef.StopRequest) wire.StopRequest {
	return wire.StopRequest{OptionsMask: uint8(r.OptionsMask), OptionsOverride: uint8(r.OptionsOverride)}
}

// nativeWire is a [NativeSource] as the source the server drives.
type nativeWire struct{ *NativeSource }

var _ source = nativeWire{}

// wireSource is the NativeSource the server drives directly.
func (n *NativeSource) wireSource() source { return nativeWire{n} }

func (w nativeWire) Options() uint8 { return w.optionsWire() }

func (w nativeWire) SetOptions(ctx context.Context, options uint8) error {
	return w.setOptionsWire(ctx, options)
}

func (w nativeWire) MoveToLevel(ctx context.Context, req wire.MoveToLevelRequest) error {
	return w.moveToLevelWire(ctx, req)
}

func (w nativeWire) MoveToLevelWithOnOff(ctx context.Context, req wire.MoveToLevelRequest) error {
	return w.moveToLevelWithOnOffWire(ctx, req)
}

func (w nativeWire) Move(ctx context.Context, req wire.MoveRequest) error {
	return w.moveWire(ctx, req)
}

func (w nativeWire) MoveWithOnOff(ctx context.Context, req wire.MoveRequest) error {
	return w.moveWithOnOffWire(ctx, req)
}

func (w nativeWire) Step(ctx context.Context, req wire.StepRequest) error {
	return w.stepWire(ctx, req)
}

func (w nativeWire) StepWithOnOff(ctx context.Context, req wire.StepRequest) error {
	return w.stepWithOnOffWire(ctx, req)
}

func (w nativeWire) Stop(ctx context.Context, req wire.StopRequest) error {
	return w.stopWire(ctx, req)
}

func (w nativeWire) StopWithOnOff(ctx context.Context, req wire.StopRequest) error {
	return w.stopWithOnOffWire(ctx, req)
}

// Options implements [LevelSource]: the stored OptionsBitmap.
func (n *NativeSource) Options() lvldef.OptionsBitmap { return lvldef.OptionsBitmap(n.optionsWire()) }

// SetOptions implements [LevelSource]: the server has checked the bitmap.
func (n *NativeSource) SetOptions(ctx context.Context, options lvldef.OptionsBitmap) error {
	return n.setOptionsWire(ctx, uint8(options))
}

// MoveToLevel implements [LevelSource] (see moveToLevelWire).
func (n *NativeSource) MoveToLevel(ctx context.Context, req lvldef.MoveToLevelRequest) error {
	return n.moveToLevelWire(ctx, wireMoveToLevel(req))
}

// MoveToLevelWithOnOff implements [LevelSource].
func (n *NativeSource) MoveToLevelWithOnOff(ctx context.Context, req lvldef.MoveToLevelWithOnOffRequest) error {
	return n.moveToLevelWithOnOffWire(ctx, wireMoveToLevel(lvldef.MoveToLevelRequest(req)))
}

// Move implements [LevelSource].
func (n *NativeSource) Move(ctx context.Context, req lvldef.MoveRequest) error {
	return n.moveWire(ctx, wireMove(req))
}

// MoveWithOnOff implements [LevelSource].
func (n *NativeSource) MoveWithOnOff(ctx context.Context, req lvldef.MoveWithOnOffRequest) error {
	return n.moveWithOnOffWire(ctx, wireMove(lvldef.MoveRequest(req)))
}

// Step implements [LevelSource].
func (n *NativeSource) Step(ctx context.Context, req lvldef.StepRequest) error {
	return n.stepWire(ctx, wireStep(req))
}

// StepWithOnOff implements [LevelSource].
func (n *NativeSource) StepWithOnOff(ctx context.Context, req lvldef.StepWithOnOffRequest) error {
	return n.stepWithOnOffWire(ctx, wireStep(lvldef.StepRequest(req)))
}

// Stop implements [LevelSource].
func (n *NativeSource) Stop(ctx context.Context, req lvldef.StopRequest) error {
	return n.stopWire(ctx, wireStop(req))
}

// StopWithOnOff implements [LevelSource].
func (n *NativeSource) StopWithOnOff(ctx context.Context, req lvldef.StopWithOnOffRequest) error {
	return n.stopWithOnOffWire(ctx, wireStop(lvldef.StopRequest(req)))
}
