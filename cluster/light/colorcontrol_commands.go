// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package light

import (
	"context"
	"math"

	ccdef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
	"github.com/SukramJ/go-fabric/cluster/transition"
	"github.com/SukramJ/go-fabric/im"
)

// MatterInvoke handles the commands of the feature selection as matter.js
// ColorControlServer does; a command the selection does not serve is
// UNSUPPORTED_COMMAND. Each mirrors the matter.js command of the same name
// and the *Logic method it delegates to: the request is validated, then
// the rate or step size the command forbids to be 0, then the ExecuteIfOff
// gate (#optionsAllowExecution), then the colour mode switch, then the
// transition.
func (s *ColorControlServer) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	if !s.inst.Accepts(cmdID) {
		return nil, im.UnsupportedCommandf("colorcontrol: command 0x%02X is not supported", cmdID)
	}
	s.cmd.Lock()
	defer s.cmd.Unlock()
	return nil, s.invoke(ctx, cmdID, fields)
}

func (s *ColorControlServer) invoke(ctx context.Context, cmdID uint32, fields any) error { //nolint:gocyclo // a dispatch table: one case per command
	switch cmdID {
	case ccdef.CmdMoveToHue:
		return s.moveToHue(ctx, fields)
	case ccdef.CmdMoveHue:
		return s.moveHue(ctx, fields)
	case ccdef.CmdStepHue:
		return s.stepHue(ctx, fields)
	case ccdef.CmdMoveToSaturation:
		return s.moveToSaturation(ctx, fields)
	case ccdef.CmdMoveSaturation:
		return s.moveSaturation(ctx, fields)
	case ccdef.CmdStepSaturation:
		return s.stepSaturation(ctx, fields)
	case ccdef.CmdMoveToHueAndSaturation:
		return s.moveToHueAndSaturation(ctx, fields)
	case ccdef.CmdMoveToColor:
		return s.moveToColor(ctx, fields)
	case ccdef.CmdMoveColor:
		return s.moveColor(ctx, fields)
	case ccdef.CmdStepColor:
		return s.stepColor(ctx, fields)
	case ccdef.CmdMoveToColorTemperature:
		return s.moveToColorTemperature(ctx, fields)
	case ccdef.CmdEnhancedMoveToHue:
		return s.enhancedMoveToHue(ctx, fields)
	case ccdef.CmdEnhancedMoveHue:
		return s.enhancedMoveHue(ctx, fields)
	case ccdef.CmdEnhancedStepHue:
		return s.enhancedStepHue(ctx, fields)
	case ccdef.CmdEnhancedMoveToHueAndSaturation:
		return s.enhancedMoveToHueAndSaturation(ctx, fields)
	case ccdef.CmdColorLoopSet:
		return s.colorLoopSet(ctx, fields)
	case ccdef.CmdStopMoveStep:
		return s.stopMoveStep(ctx, fields)
	case ccdef.CmdMoveColorTemperature:
		return s.moveColorTemperature(ctx, fields)
	case ccdef.CmdStepColorTemperature:
		return s.stepColorTemperature(ctx, fields)
	}
	return im.UnsupportedCommandf("colorcontrol: command 0x%02X is not supported", cmdID)
}

// assertRate is matter.js #assertRate.
func assertRate(mode ccdef.MoveModeEnum, rate float64) error {
	if (mode == ccdef.MoveModeUp || mode == ccdef.MoveModeDown) && rate == 0 {
		return colorControlInvalidCommandErr("colorcontrol: Rate must not be 0 when moving Up or Down")
	}
	return nil
}

// assertStepSize is matter.js #assertStepSize.
func assertStepSize(step float64, what string) error {
	if step == 0 {
		return colorControlInvalidCommandErr("colorcontrol: " + what + " step size must not be 0")
	}
	return nil
}

// moveSign is +1 for Up, -1 otherwise (matter.js `moveMode === Up ? 1 : -1`).
func moveSign[T ~uint8](mode, up T) float64 {
	if mode == up {
		return 1
	}
	return -1
}

// --- hue and saturation (HS, EHUE) -----------------------------------------

func (s *ColorControlServer) moveToHue(ctx context.Context, fields any) error {
	r, err := moveToHueFields(fields)
	if err != nil {
		return err
	}
	if !s.executes(uint8(r.OptionsMask), uint8(r.OptionsOverride)) {
		return nil
	}
	if err := s.setColorMode(ctx, ColorModeHueSaturation); err != nil {
		return err
	}
	return s.moveToHueLogic(float64(r.Hue), r.Direction, float64(r.TransitionTime), false)
}

// moveToHueLogic is matter.js moveToHueLogic: the rate from the distance
// the direction implies (HueDistance) over TransitionTime, the distance
// rule carried into the cyclic transition.
func (s *ColorControlServer) moveToHueLogic(target float64, direction ccdef.DirectionEnum, transitionTime float64, enhanced bool) error {
	name, maxHue := propHue, float64(maxHueValue)
	if enhanced {
		name, maxHue = propEnhancedHue, maxEnhancedHueValue
	}
	current, _ := s.state().property(name)
	dir := transition.HueDirection(direction)
	return s.startTransition(transition.Transition{
		Name:   name,
		Rate:   transition.HueDistance(current, target, dir, maxHue) / transitionTime * 10,
		Target: target,
		CyclicDistance: func(c, t float64) float64 {
			return transition.HueDistance(c, t, dir, maxHue)
		},
	})
}

func (s *ColorControlServer) moveHue(ctx context.Context, fields any) error {
	r, err := moveHueFields(fields)
	if err != nil {
		return err
	}
	return s.moveHueCommand(ctx, r.MoveMode, float64(r.Rate), uint8(r.OptionsMask), uint8(r.OptionsOverride), false)
}

// moveHueCommand is matter.js moveHue / enhancedMoveHue: Stop ends the hue
// and saturation movements, Up and Down move without end at Rate per
// second.
func (s *ColorControlServer) moveHueCommand(ctx context.Context, mode ccdef.MoveModeEnum, rate float64, mask, override uint8, enhanced bool) error {
	if err := assertRate(mode, rate); err != nil {
		return err
	}
	if !s.executes(mask, override) {
		return nil
	}
	if err := s.setModeFor(ctx, enhanced); err != nil {
		return err
	}
	if mode == ccdef.MoveModeStop {
		s.stopHueAndSaturationMovement()
		return nil
	}
	return s.moveHueLogic(mode, rate, enhanced)
}

// setModeFor switches to the hue and saturation mode, enhanced or not.
func (s *ColorControlServer) setModeFor(ctx context.Context, enhanced bool) error {
	if enhanced {
		return s.setEnhancedColorMode(ctx, ColorModeEnhancedHueSaturation)
	}
	return s.setColorMode(ctx, ColorModeHueSaturation)
}

// moveHueLogic is matter.js moveHueLogic.
func (s *ColorControlServer) moveHueLogic(mode ccdef.MoveModeEnum, rate float64, enhanced bool) error {
	name := propHue
	if enhanced {
		name = propEnhancedHue
	}
	return s.startTransition(transition.Transition{Name: name, Rate: rate * moveSign(mode, ccdef.MoveModeUp), NoTarget: true})
}

func (s *ColorControlServer) stepHue(ctx context.Context, fields any) error {
	r, err := stepHueFields(fields)
	if err != nil {
		return err
	}
	return s.stepHueCommand(ctx, r.StepMode, float64(r.StepSize), float64(r.TransitionTime), uint8(r.OptionsMask), uint8(r.OptionsOverride), false)
}

// stepHueCommand is matter.js stepHue / enhancedStepHue.
func (s *ColorControlServer) stepHueCommand(ctx context.Context, mode ccdef.StepModeEnum, step, transitionTime float64, mask, override uint8, enhanced bool) error {
	what := "Hue"
	if enhanced {
		what = "Enhanced Hue"
	}
	if err := assertStepSize(step, what); err != nil {
		return err
	}
	if !s.executes(mask, override) {
		return nil
	}
	if err := s.setModeFor(ctx, enhanced); err != nil {
		return err
	}
	return s.stepHueLogic(mode, step, transitionTime, enhanced)
}

// stepHueLogic is matter.js stepHueLogic: StepSize round the wheel from
// the current hue (addValueWithOverflow), over TransitionTime.
func (s *ColorControlServer) stepHueLogic(mode ccdef.StepModeEnum, step, transitionTime float64, enhanced bool) error {
	name, maxHue := propHue, float64(maxHueValue)
	if enhanced {
		name, maxHue = propEnhancedHue, maxEnhancedHueValue
	}
	current, _ := s.state().property(name)
	sign := moveSign(mode, ccdef.StepModeUp)
	dir := transition.HueDown
	if mode == ccdef.StepModeUp {
		dir = transition.HueUp
	}
	return s.startTransition(transition.Transition{
		Name:   name,
		Rate:   step / transitionTime * 10 * sign,
		Target: transition.AddWithOverflow(current, step*sign, minHueValue, maxHue),
		CyclicDistance: func(c, t float64) float64 {
			return transition.HueDistance(c, t, dir, maxHue)
		},
	})
}

func (s *ColorControlServer) moveToSaturation(ctx context.Context, fields any) error {
	r, err := moveToSaturationFields(fields)
	if err != nil {
		return err
	}
	if !s.executes(uint8(r.OptionsMask), uint8(r.OptionsOverride)) {
		return nil
	}
	if err := s.setColorMode(ctx, ColorModeHueSaturation); err != nil {
		return err
	}
	return s.moveToSaturationLogic(float64(r.Saturation), float64(r.TransitionTime))
}

// moveToSaturationLogic is matter.js moveToSaturationLogic.
func (s *ColorControlServer) moveToSaturationLogic(target, transitionTime float64) error {
	// The rate is computed from the value the engine reads under its lock
	// (transition.Transition.RateFrom), as matter.js computes it in the
	// same synchronous handler that starts the transition.
	return s.startTransition(transition.Transition{
		Name: propSaturation, Target: target,
		RateFrom: func(current float64) float64 { return (target - current) / transitionTime * 10 },
	})
}

func (s *ColorControlServer) moveSaturation(ctx context.Context, fields any) error {
	r, err := moveSaturationFields(fields)
	if err != nil {
		return err
	}
	if err := assertRate(r.MoveMode, float64(r.Rate)); err != nil {
		return err
	}
	if !s.executes(uint8(r.OptionsMask), uint8(r.OptionsOverride)) {
		return nil
	}
	if err := s.setColorMode(ctx, ColorModeHueSaturation); err != nil {
		return err
	}
	if r.MoveMode == ccdef.MoveModeStop {
		s.stopHueAndSaturationMovement()
		return nil
	}
	// matter.js moveSaturationLogic: to the bound in the direction of travel.
	return s.startTransition(transition.Transition{
		Name: propSaturation, Rate: float64(r.Rate) * moveSign(r.MoveMode, ccdef.MoveModeUp), NoTarget: true,
	})
}

func (s *ColorControlServer) stepSaturation(ctx context.Context, fields any) error {
	r, err := stepSaturationFields(fields)
	if err != nil {
		return err
	}
	if err := assertStepSize(float64(r.StepSize), "Saturation"); err != nil {
		return err
	}
	if !s.executes(uint8(r.OptionsMask), uint8(r.OptionsOverride)) {
		return nil
	}
	if err := s.setColorMode(ctx, ColorModeHueSaturation); err != nil {
		return err
	}
	// matter.js stepSaturationLogic: the target may lie beyond a bound, where
	// the transition ends.
	sign := moveSign(r.StepMode, ccdef.StepModeUp)
	step := float64(r.StepSize)
	return s.startTransition(transition.Transition{
		Name: propSaturation, Rate: step / float64(r.TransitionTime) * 10 * sign,
		Target: float64(s.state().saturation) + step*sign,
	})
}

func (s *ColorControlServer) moveToHueAndSaturation(ctx context.Context, fields any) error {
	r, err := moveToHueAndSaturationFields(fields)
	if err != nil {
		return err
	}
	if !s.executes(uint8(r.OptionsMask), uint8(r.OptionsOverride)) {
		return nil
	}
	if err := s.setColorMode(ctx, ColorModeHueSaturation); err != nil {
		return err
	}
	return s.moveToHueAndSaturationLogic(float64(r.Hue), float64(r.Saturation), float64(r.TransitionTime), false)
}

// moveToHueAndSaturationLogic is matter.js moveToHueAndSaturationLogic and
// moveToEnhancedHueAndSaturationLogic: the shortest way round to the hue,
// then the saturation, over the same TransitionTime.
func (s *ColorControlServer) moveToHueAndSaturationLogic(hue, saturation, transitionTime float64, enhanced bool) error {
	if err := s.moveToHueLogic(hue, ccdef.DirectionShortest, transitionTime, enhanced); err != nil {
		return err
	}
	return s.moveToSaturationLogic(saturation, transitionTime)
}

func (s *ColorControlServer) enhancedMoveToHue(ctx context.Context, fields any) error {
	r, err := enhancedMoveToHueFields(fields)
	if err != nil {
		return err
	}
	if !s.executes(uint8(r.OptionsMask), uint8(r.OptionsOverride)) {
		return nil
	}
	if err := s.setEnhancedColorMode(ctx, ColorModeEnhancedHueSaturation); err != nil {
		return err
	}
	return s.moveToHueLogic(float64(r.EnhancedHue), r.Direction, float64(r.TransitionTime), true)
}

func (s *ColorControlServer) enhancedMoveHue(ctx context.Context, fields any) error {
	r, err := enhancedMoveHueFields(fields)
	if err != nil {
		return err
	}
	return s.moveHueCommand(ctx, r.MoveMode, float64(r.Rate), uint8(r.OptionsMask), uint8(r.OptionsOverride), true)
}

func (s *ColorControlServer) enhancedStepHue(ctx context.Context, fields any) error {
	r, err := enhancedStepHueFields(fields)
	if err != nil {
		return err
	}
	return s.stepHueCommand(ctx, r.StepMode, float64(r.StepSize), float64(r.TransitionTime), uint8(r.OptionsMask), uint8(r.OptionsOverride), true)
}

func (s *ColorControlServer) enhancedMoveToHueAndSaturation(ctx context.Context, fields any) error {
	r, err := enhancedMoveToHueAndSaturationFields(fields)
	if err != nil {
		return err
	}
	if !s.executes(uint8(r.OptionsMask), uint8(r.OptionsOverride)) {
		return nil
	}
	if err := s.setEnhancedColorMode(ctx, ColorModeEnhancedHueSaturation); err != nil {
		return err
	}
	return s.moveToHueAndSaturationLogic(float64(r.EnhancedHue), float64(r.Saturation), float64(r.TransitionTime), true)
}

// --- xy (XY) ------------------------------------------------------------------

func (s *ColorControlServer) moveToColor(ctx context.Context, fields any) error {
	r, err := moveToColorFields(fields)
	if err != nil {
		return err
	}
	if !s.executes(uint8(r.OptionsMask), uint8(r.OptionsOverride)) {
		return nil
	}
	if err := s.setColorMode(ctx, ColorModeXY); err != nil {
		return err
	}
	return s.moveToColorLogic(float64(r.ColorX), float64(r.ColorY), float64(r.TransitionTime))
}

// moveToColorLogic is matter.js moveToColorLogic: x and y each at the rate
// that reaches its target in TransitionTime.
func (s *ColorControlServer) moveToColorLogic(x, y, transitionTime float64) error {
	st := s.state()
	if err := s.startTransition(transition.Transition{
		Name: propX, Rate: (x - float64(st.x)) / transitionTime * 10, Target: x,
	}); err != nil {
		return err
	}
	return s.startTransition(transition.Transition{
		Name: propY, Rate: (y - float64(s.state().y)) / transitionTime * 10, Target: y,
	})
}

// moveColor is matter.js moveColor and moveColorLogic: x and y at RateX /
// RateY per second towards the bound in their direction; both rates 0
// stop every colour movement and end the transition, without a mode
// switch.
func (s *ColorControlServer) moveColor(ctx context.Context, fields any) error {
	r, err := moveColorFields(fields)
	if err != nil {
		return err
	}
	if !s.executes(uint8(r.OptionsMask), uint8(r.OptionsOverride)) {
		return nil
	}
	if r.RateX == 0 && r.RateY == 0 {
		s.stopAllColorMovement()
		s.endTransitions()
		return nil
	}
	if err := s.setColorMode(ctx, ColorModeXY); err != nil {
		return err
	}
	if r.RateX != 0 {
		if err := s.startTransition(transition.Transition{Name: propX, Rate: float64(r.RateX), NoTarget: true}); err != nil {
			return err
		}
	}
	if r.RateY != 0 {
		return s.startTransition(transition.Transition{Name: propY, Rate: float64(r.RateY), NoTarget: true})
	}
	return nil
}

// stepColor is matter.js stepColor and stepColorLogic: StepX / StepY from
// the current x / y over TransitionTime; both 0 is refused before the
// ExecuteIfOff gate.
func (s *ColorControlServer) stepColor(ctx context.Context, fields any) error {
	r, err := stepColorFields(fields)
	if err != nil {
		return err
	}
	if r.StepX == 0 && r.StepY == 0 {
		return colorControlInvalidCommandErr("colorcontrol: Color step sizes must not be 0")
	}
	if !s.executes(uint8(r.OptionsMask), uint8(r.OptionsOverride)) {
		return nil
	}
	if err := s.setColorMode(ctx, ColorModeXY); err != nil {
		return err
	}
	tt := float64(r.TransitionTime)
	if r.StepX != 0 {
		step := float64(r.StepX)
		if err := s.startTransition(transition.Transition{
			Name: propX, Rate: step / tt * 10, Target: float64(s.state().x) + step,
		}); err != nil {
			return err
		}
	}
	if r.StepY != 0 {
		step := float64(r.StepY)
		return s.startTransition(transition.Transition{
			Name: propY, Rate: step / tt * 10, Target: float64(s.state().y) + step,
		})
	}
	return nil
}

// --- colour temperature (CT) --------------------------------------------------

// moveToColorTemperature is matter.js moveToColorTemperature: a rate from
// TransitionTime (a zero one is infinite, so the target applies at once),
// the target cropped to the physical range where it lies beyond it.
func (s *ColorControlServer) moveToColorTemperature(ctx context.Context, fields any) error {
	r, err := moveToColorTemperatureFields(fields)
	if err != nil {
		return err
	}
	if !s.executes(uint8(r.OptionsMask), uint8(r.OptionsOverride)) {
		return nil
	}
	if err := s.setColorMode(ctx, ColorModeColorTemperature); err != nil {
		return err
	}
	return s.moveToColorTemperatureLogic(float64(r.ColorTemperatureMireds), float64(r.TransitionTime))
}

// moveToColorTemperatureLogic is matter.js moveToColorTemperatureLogic.
func (s *ColorControlServer) moveToColorTemperatureLogic(target, transitionTime float64) error {
	// Rate from the engine's own read of the current value, see
	// moveToSaturationLogic.
	return s.startTransition(transition.Transition{
		Name: propColorTemperature, Target: target,
		RateFrom: func(current float64) float64 { return (target - current) / transitionTime * 10 },
	})
}

// moveColorTemperature is matter.js moveColorTemperature and
// moveColorTemperatureLogic: Up or Down at Rate mireds per second towards
// the bound the command names (0 is the physical one), a zero Rate
// refused. Stop ends the hue and saturation movements only — as in
// matter.js, a colour temperature move keeps running.
func (s *ColorControlServer) moveColorTemperature(ctx context.Context, fields any) error {
	r, err := moveColorTemperatureFields(fields)
	if err != nil {
		return err
	}
	if err := assertRate(r.MoveMode, float64(r.Rate)); err != nil {
		return err
	}
	if !s.executes(uint8(r.OptionsMask), uint8(r.OptionsOverride)) {
		return nil
	}
	if err := s.setColorMode(ctx, ColorModeColorTemperature); err != nil {
		return err
	}
	if r.MoveMode == ccdef.MoveModeStop {
		s.stopHueAndSaturationMovement()
		return nil
	}
	lower, upper := s.commandBounds(r.ColorTemperatureMinimumMireds, r.ColorTemperatureMaximumMireds)
	target := upper
	if r.MoveMode != ccdef.MoveModeUp {
		target = lower
	}
	return s.startTransition(transition.Transition{
		Name: propColorTemperature, Rate: float64(r.Rate) * moveSign(r.MoveMode, ccdef.MoveModeUp), Target: target,
	})
}

// stepColorTemperature is matter.js stepColorTemperature and
// stepColorTemperatureLogic: StepSize from the current value, cropped to
// the bounds the command names, over TransitionTime; a zero StepSize is
// refused.
func (s *ColorControlServer) stepColorTemperature(ctx context.Context, fields any) error {
	r, err := stepColorTemperatureFields(fields)
	if err != nil {
		return err
	}
	step := float64(r.StepSize)
	if err := assertStepSize(step, "ColorTemperature"); err != nil {
		return err
	}
	if !s.executes(uint8(r.OptionsMask), uint8(r.OptionsOverride)) {
		return nil
	}
	if err := s.setColorMode(ctx, ColorModeColorTemperature); err != nil {
		return err
	}
	lower, upper := s.commandBounds(r.ColorTemperatureMinimumMireds, r.ColorTemperatureMaximumMireds)
	sign := moveSign(r.StepMode, ccdef.StepModeUp)
	return s.startTransition(transition.Transition{
		Name: propColorTemperature, Rate: step / float64(r.TransitionTime) * 10 * sign,
		Target: cropValue(float64(s.state().mireds)+step*sign, lower, upper),
	})
}

// commandBounds is the colour temperature range a Move / Step command
// names: a zero bound is the physical one, and both are cropped to the
// physical range (matter.js moveColorTemperature / stepColorTemperature).
func (s *ColorControlServer) commandBounds(minimum, maximum uint16) (lower, upper float64) {
	if minimum == 0 {
		minimum = s.minimumMireds()
	}
	if maximum == 0 {
		maximum = s.maximumMireds()
	}
	return s.cropMireds(float64(minimum)), s.cropMireds(float64(maximum))
}

// --- colour loop (CL) and StopMoveStep ---------------------------------------

// colorLoopSet is matter.js colorLoopSet: the flags select which of
// direction, time and start hue are taken; a changed direction or time
// restarts a running loop from the current hue; Activate switches to the
// enhanced hue mode, keeps the current enhanced hue to restore and loops
// from the start or the current hue; Deactivate stops a running loop and
// restores the kept hue.
func (s *ColorControlServer) colorLoopSet(ctx context.Context, fields any) error {
	r, err := colorLoopSetFields(fields)
	if err != nil {
		return err
	}
	if !s.executes(uint8(r.OptionsMask), uint8(r.OptionsOverride)) {
		return nil
	}
	changed := false
	if err := s.update(ctx, func(st *colorState) {
		if r.UpdateFlags&ccdef.UpdateFlagsUpdateDirection != 0 && st.loopDirection != uint8(r.Direction) {
			st.loopDirection = uint8(r.Direction)
			changed = true
		}
		if r.UpdateFlags&ccdef.UpdateFlagsUpdateTime != 0 && st.loopTime != r.Time {
			st.loopTime = r.Time
			changed = true
		}
		if r.UpdateFlags&ccdef.UpdateFlagsUpdateStartHue != 0 {
			st.loopStart = r.StartHue
		}
	}); err != nil {
		return err
	}
	st := s.state()
	switch {
	case r.UpdateFlags&ccdef.UpdateFlagsUpdateAction != 0 && r.Action == ccdef.ColorLoopActionDeactivate:
		if st.loopActive == colorLoopActiveActive {
			return s.stopColorLoop(ctx)
		}
		return nil
	case r.UpdateFlags&ccdef.UpdateFlagsUpdateAction != 0:
		if err := s.setEnhancedColorMode(ctx, ColorModeEnhancedHueSaturation); err != nil {
			return err
		}
		if err := s.update(ctx, func(st *colorState) {
			st.loopStored = st.enhancedHue
			st.loopActive = colorLoopActiveActive
		}); err != nil {
			return err
		}
		st = s.state()
		if r.Action == ccdef.ColorLoopActionActivateFromColorLoopStartEnhancedHue {
			return s.startColorLoopLogic(ctx, st.loopStart)
		}
		return s.startColorLoopLogic(ctx, st.enhancedHue)
	case changed && st.loopActive == colorLoopActiveActive:
		return s.startColorLoopLogic(ctx, st.enhancedHue)
	}
	return nil
}

// stopColorLoop is matter.js #stopColorLoop: the loop's transition stops,
// ColorLoopActive becomes inactive and the kept enhanced hue returns.
func (s *ColorControlServer) stopColorLoop(ctx context.Context) error {
	s.engine.Stop(propEnhancedHue) // stopColorLoopLogic
	return s.update(ctx, func(st *colorState) {
		if st.loopActive == colorLoopActiveActive {
			st.loopActive = colorLoopActiveInactive
			st.enhancedHue = st.loopStored
		}
	})
}

// startColorLoopLogic is matter.js startColorLoopLogic: the enhanced hue
// starts at startHue and moves without end, one turn per ColorLoopTime
// seconds, in the loop's direction.
func (s *ColorControlServer) startColorLoopLogic(ctx context.Context, startHue uint16) error {
	if err := s.update(ctx, func(st *colorState) { st.enhancedHue = startHue }); err != nil {
		return err
	}
	st := s.state()
	mode := ccdef.MoveModeUp
	if st.loopDirection == uint8(ccdef.ColorLoopDirectionDecrement) {
		mode = ccdef.MoveModeDown
	}
	return s.moveHueLogic(mode, math.Floor(maxEnhancedHueValue/float64(st.loopTime)), true)
}

// stopMoveStep is matter.js stopMoveStep and stopMoveStepLogic: every
// movement stops where it is and RemainingTime reports 0 — except a running
// colour loop, which only ColorLoopSet stops, so its transition and the
// remaining time survive.
func (s *ColorControlServer) stopMoveStep(_ context.Context, fields any) error {
	r, err := stopMoveStepFields(fields)
	if err != nil {
		return err
	}
	if !s.executes(uint8(r.OptionsMask), uint8(r.OptionsOverride)) {
		return nil
	}
	// Without the ColorLoop feature the attribute reads undefined in
	// matter.js, so only an explicitly active loop is carved out.
	looping := s.has(ColorFeatureColorLoop) && s.state().loopActive == colorLoopActiveActive
	if !looping {
		s.engine.Stop(propEnhancedHue)
	}
	s.engine.Stop(propHue)
	s.engine.Stop(propSaturation)
	s.engine.Stop(propX)
	s.engine.Stop(propY)
	s.engine.Stop(propColorTemperature)
	if looping {
		return nil
	}
	s.endTransitions()
	return nil
}
