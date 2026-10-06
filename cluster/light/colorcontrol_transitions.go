// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package light

import (
	"context"
	"fmt"
	"math"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/transition"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
)

// propColorTemperature names ColorTemperatureMireds in the engine.
const propColorTemperature = "colorTemperatureMireds"

// The bounds matter.js puts on a colour temperature whose physical bound
// is unset (ColorControlServer.ts MIN_TEMPERATURE_VALUE /
// MAX_TEMPERATURE_VALUE).
const (
	minTemperatureMireds uint16 = 0
	maxTemperatureMireds uint16 = 0xFEFF
)

// The level range the colour temperature couples to (ColorControlServer.ts
// MIN_CURRENT_LEVEL / MAX_CURRENT_LEVEL).
const (
	minCurrentLevel = 0x01
	maxCurrentLevel = 0xFE
)

// MoveModeEnum and StepModeEnum (color-control.element.ts): Stop 0, Up 1,
// Down 3.
const (
	colorModeStop uint8 = 0
	colorModeUp   uint8 = 1
	colorModeDown uint8 = 3
)

// optionExecuteIfOff is the ColorControl OptionsBitmap's only bit.
const optionExecuteIfOff uint8 = 0x01

// minimumMireds is matter.js minimumColorTemperatureMireds: the physical
// minimum, or 0 when none is set.
func (s *ColorControlServer) minimumMireds() uint16 {
	if s.cfg.MinMireds == 0 {
		return minTemperatureMireds
	}
	return s.cfg.MinMireds
}

// maximumMireds is matter.js maximumColorTemperatureMireds: the physical
// maximum, or 0xFEFF when none is set.
func (s *ColorControlServer) maximumMireds() uint16 {
	if s.cfg.MaxMireds == 0 {
		return maxTemperatureMireds
	}
	return s.cfg.MaxMireds
}

// cropMireds is matter.js #cropColorTemperature.
func (s *ColorControlServer) cropMireds(v float64) float64 {
	return min(max(v, float64(s.minimumMireds())), float64(s.maximumMireds()))
}

// applyMireds pushes a colour temperature the engine applies to the
// device sink, then makes it the attribute's value and reports it by the
// quieter rules of its "Q" quality. A refused value leaves the attribute
// where it was.
func (s *ColorControlServer) applyMireds(changes []transition.Change) error {
	for _, c := range changes {
		mireds := uint16(c.Value) //nolint:gosec // the engine keeps the value between the physical bounds
		s.mu.Lock()
		w := s.writer
		s.mu.Unlock()
		if w != nil {
			if err := w.SetColorTemperatureMireds(context.Background(), mireds); err != nil {
				return fmt.Errorf("colorcontrol: ColorTemperatureMireds write-through: %w", err)
			}
		}
		s.mu.Lock()
		changed := s.current != mireds
		s.current = mireds
		s.mu.Unlock()
		if changed {
			s.quiet.Changed(false, false)
		}
	}
	return nil
}

// OnMatterAttributesChanged implements [contract.AttributeChangeNotifier]:
// ColorTemperatureMireds by the quieter rules of its "Q" quality — at most
// once a second while a transition runs, the end flushing the last value
// — and RemainingTime when matter.js's #updateRemainingTime would report
// it.
func (s *ColorControlServer) OnMatterAttributesChanged(cb func(attrIDs []uint32)) (unsubscribe func()) {
	return s.changes.OnMatterAttributesChanged(cb)
}

// MatterSelfReportedAttributes implements
// [contract.SelfReportedAttributeLister].
func (*ColorControlServer) MatterSelfReportedAttributes() []uint32 {
	return []uint32{wire.ColorCtrlAttrColorTemperatureMireds, wire.ColorCtrlAttrRemainingTime}
}

// MatterQuiesce implements [contract.ClusterQuiescer]: a transition stops
// where it is, without a report (matter.js ColorControlServer
// [Symbol.asyncDispose]).
func (s *ColorControlServer) MatterQuiesce() { s.engine.StopAll() }

// executes is matter.js #optionsAllowExecution.
func (s *ColorControlServer) executes(mask, override uint8) bool {
	s.mu.Lock()
	options := ((s.options &^ mask) | (override & mask)) & colorOptionsDefinedBits
	s.mu.Unlock()
	return options&optionExecuteIfOff != 0 || s.onOff == nil || s.onOff.OnOff()
}

// MatterInvoke handles the CT commands as matter.js ColorControlServer
// does. Every one switches the colour mode to ColorTemperatureMireds,
// which is the only mode a CT-only server has.
func (s *ColorControlServer) MatterInvoke(_ context.Context, cmdID uint32, fields any) (any, error) {
	s.cmd.Lock()
	defer s.cmd.Unlock()
	switch cmdID {
	case wire.ColorCtrlCmdMoveToColorTemperature:
		return nil, s.moveToColorTemperature(fields)
	case wire.ColorCtrlCmdMoveColorTemperature:
		return nil, s.moveColorTemperature(fields)
	case wire.ColorCtrlCmdStepColorTemperature:
		return nil, s.stepColorTemperature(fields)
	case wire.ColorCtrlCmdStopMoveStep:
		s.stopMoveStep(fields)
		return nil, nil
	default:
		return nil, fmt.Errorf("colorcontrol: unknown command 0x%02X", cmdID)
	}
}

// moveToColorTemperature is matter.js moveToColorTemperature and
// moveToColorTemperatureLogic: a rate from TransitionTime (a zero one is
// infinite, so the target applies at once), the target cropped to the
// physical range where it lies beyond it.
func (s *ColorControlServer) moveToColorTemperature(fields any) error {
	var req wire.MoveToColorTemperatureRequest
	switch v := fields.(type) {
	case wire.MoveToColorTemperatureRequest:
		req = v
	case map[uint8]any:
		req.ColorTemperatureMireds = uint16(tagUint(v, 0, 0xFFFF)) //nolint:gosec // bounded by tagUint
		req.TransitionTime = uint16(tagUint(v, 1, 0xFFFF))         //nolint:gosec // bounded by tagUint
		req.OptionsMask = uint8(tagUint(v, 2, 0xFF))               //nolint:gosec // bounded by tagUint
		req.OptionsOverride = uint8(tagUint(v, 3, 0xFF))           //nolint:gosec // bounded by tagUint
	default:
		return colorControlInvalidCommandErr(fmt.Sprintf("colorcontrol: MoveToColorTemperature carried %T", fields))
	}
	if !s.executes(req.OptionsMask, req.OptionsOverride) {
		return nil
	}
	return s.moveToColorTemperatureLogic(float64(req.ColorTemperatureMireds), float64(req.TransitionTime))
}

// moveToColorTemperatureLogic is matter.js moveToColorTemperatureLogic.
func (s *ColorControlServer) moveToColorTemperatureLogic(target, transitionTime float64) error {
	s.mu.Lock()
	current := float64(s.current)
	s.mu.Unlock()
	return s.engine.Start(transition.Transition{
		Name: propColorTemperature, Rate: (target - current) / transitionTime * 10, Target: target,
	})
}

// moveColorTemperature is matter.js moveColorTemperature and
// moveColorTemperatureLogic: Up or Down at Rate mireds per second towards
// the bound the command names (0 is the physical one), a zero Rate
// refused. Stop ends hue and saturation moves only, which a CT-only
// server has none of — so, as in matter.js, it leaves a colour temperature
// move running.
func (s *ColorControlServer) moveColorTemperature(fields any) error {
	m, ok := fields.(map[uint8]any)
	if !ok {
		return colorControlInvalidCommandErr(fmt.Sprintf("colorcontrol: MoveColorTemperature carried %T", fields))
	}
	mode := uint8(tagUint(m, 0, 0xFF))     //nolint:gosec // bounded by tagUint
	rate := float64(tagUint(m, 1, 0xFFFF)) //nolint:gosec // bounded by tagUint
	minimum, maximum := tagUint(m, 2, 0xFFFF), tagUint(m, 3, 0xFFFF)
	mask, override := uint8(tagUint(m, 4, 0xFF)), uint8(tagUint(m, 5, 0xFF)) //nolint:gosec // bounded by tagUint
	if err := checkColorMode("MoveColorTemperature MoveMode", mode, true); err != nil {
		return err
	}
	// matter.js #assertRate.
	if mode != colorModeStop && rate == 0 {
		return colorControlInvalidCommandErr("colorcontrol: Rate must not be 0 when moving Up or Down")
	}
	if !s.executes(mask, override) || mode == colorModeStop {
		return nil
	}
	lower, upper := s.commandBounds(minimum, maximum)
	target, sign := upper, 1.0
	if mode == colorModeDown {
		target, sign = lower, -1
	}
	return s.engine.Start(transition.Transition{Name: propColorTemperature, Rate: rate * sign, Target: target})
}

// stepColorTemperature is matter.js stepColorTemperature and
// stepColorTemperatureLogic: StepSize from the current value, cropped to
// the bounds the command names, over TransitionTime; a zero StepSize is
// refused.
func (s *ColorControlServer) stepColorTemperature(fields any) error {
	m, ok := fields.(map[uint8]any)
	if !ok {
		return colorControlInvalidCommandErr(fmt.Sprintf("colorcontrol: StepColorTemperature carried %T", fields))
	}
	mode := uint8(tagUint(m, 0, 0xFF)) //nolint:gosec // bounded by tagUint
	step := float64(tagUint(m, 1, 0xFFFF))
	transitionTime := float64(tagUint(m, 2, 0xFFFF))
	minimum, maximum := tagUint(m, 3, 0xFFFF), tagUint(m, 4, 0xFFFF)
	mask, override := uint8(tagUint(m, 5, 0xFF)), uint8(tagUint(m, 6, 0xFF)) //nolint:gosec // bounded by tagUint
	// The request's enum is validated before the command runs (matter.js
	// ValueValidator, ConstraintError), the step size by the command
	// (#assertStepSize).
	if err := checkColorMode("StepColorTemperature StepMode", mode, false); err != nil {
		return err
	}
	if step == 0 {
		return colorControlInvalidCommandErr("colorcontrol: ColorTemperature step size must not be 0")
	}
	if !s.executes(mask, override) {
		return nil
	}
	lower, upper := s.commandBounds(minimum, maximum)
	sign := 1.0
	if mode == colorModeDown {
		sign = -1
	}
	s.mu.Lock()
	current := float64(s.current)
	s.mu.Unlock()
	return s.engine.Start(transition.Transition{
		Name: propColorTemperature, Rate: step / transitionTime * 10 * sign,
		Target: min(max(current+step*sign, lower), upper),
	})
}

// commandBounds is the colour temperature range a Move / Step command
// names: a zero bound is the physical one, and both are cropped to the
// physical range (matter.js moveColorTemperature / stepColorTemperature).
func (s *ColorControlServer) commandBounds(minimum, maximum uint64) (lower, upper float64) {
	if minimum == 0 {
		minimum = uint64(s.minimumMireds())
	}
	if maximum == 0 {
		maximum = uint64(s.maximumMireds())
	}
	return s.cropMireds(float64(minimum)), s.cropMireds(float64(maximum))
}

// stopMoveStep is matter.js stopMoveStep and stopMoveStepLogic: every
// movement stops where it is and RemainingTime reports 0 (no colour loop
// runs on a CT-only server).
func (s *ColorControlServer) stopMoveStep(fields any) {
	var mask, override uint8
	if m, ok := fields.(map[uint8]any); ok {
		mask, override = uint8(tagUint(m, 0, 0xFF)), uint8(tagUint(m, 1, 0xFF)) //nolint:gosec // bounded by tagUint
	}
	if !s.executes(mask, override) {
		return
	}
	s.engine.Stop(propColorTemperature)
	s.engine.CancelAll()
}

// SyncColorTemperatureWithLevel moves the colour temperature to the value
// level maps to while LevelControl's CoupleColorTempToLevel option is in
// effect: matter.js ColorControlServer.syncColorTemperatureWithLevel and
// syncColorTemperatureWithLevelLogic. The minimum level maps to the
// physical maximum, the maximum level to CoupleColorTempToLevelMinMireds,
// a level in between linearly; the value applies at once. It implements
// levelcontrol.ColorTemperatureCoupling.
func (s *ColorControlServer) SyncColorTemperatureWithLevel(_ context.Context, level uint8) error {
	s.cmd.Lock()
	defer s.cmd.Unlock()
	coupleMin := float64(s.minimumMireds()) // CoupleColorTempToLevelMinMireds reads the physical minimum
	physMax := float64(s.maximumMireds())
	var mireds float64
	switch {
	case level <= minCurrentLevel:
		mireds = physMax
	case level >= maxCurrentLevel:
		mireds = coupleMin
	default:
		mireds = physMax - math.Floor((physMax-coupleMin)*float64(level)/(maxCurrentLevel-minCurrentLevel+1))
	}
	return s.moveToColorTemperatureLogic(mireds, 0)
}

// checkColorMode refuses a MoveModeEnum / StepModeEnum value the enum
// does not define (Stop only for MoveMode).
func checkColorMode(field string, mode uint8, allowStop bool) error {
	if mode == colorModeUp || mode == colorModeDown || (allowStop && mode == colorModeStop) {
		return nil
	}
	return colorControlConstraintErr(fmt.Sprintf("colorcontrol: %s %d is not defined", field, mode))
}

// tagUint reads an unsigned field of the generic tag map the bridge hands
// over, 0 when absent or wider than the field (the bridge decodes the
// payload with the field's own width, so a wider value is malformed).
func tagUint(m map[uint8]any, tag uint8, limit uint64) uint64 {
	v, ok := cluster.AsUintMax(m[tag], limit)
	if !ok {
		return 0
	}
	return v
}

// colorControlInvalidCommandErr answers a command with INVALID_COMMAND.
type colorControlInvalidCommandErr string

func (e colorControlInvalidCommandErr) Error() string                 { return string(e) }
func (colorControlInvalidCommandErr) MatterStatusCode() im.StatusCode { return im.StatusInvalidCommand }
