// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package light

import (
	"context"
	"fmt"
	"math"
	"slices"

	ccdef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
	"github.com/SukramJ/go-fabric/cluster/transition"
	"github.com/SukramJ/go-fabric/contract"
)

// The transitionable properties, named as matter.js names them in its
// Transitions configuration (ColorControlServer.ts #initializeTransitions).
const (
	propHue              = "currentHue"
	propEnhancedHue      = "enhancedCurrentHue"
	propSaturation       = "currentSaturation"
	propColorTemperature = "colorTemperatureMireds"
	propX                = "currentX"
	propY                = "currentY"
)

// propAttribute maps a property to its attribute.
var propAttribute = map[string]uint32{
	propHue:              ccdef.AttrCurrentHue,
	propEnhancedHue:      ccdef.AttrEnhancedCurrentHue,
	propSaturation:       ccdef.AttrCurrentSaturation,
	propColorTemperature: ccdef.AttrColorTemperatureMireds,
	propX:                ccdef.AttrCurrentX,
	propY:                ccdef.AttrCurrentY,
}

// quietAttributes are the attributes with the "Q" quality the server
// changes (color-control.element.ts): reported at most once a second
// while they change, the end of a transition flushing the last value.
var quietAttributes = []uint32{
	ccdef.AttrCurrentHue, ccdef.AttrCurrentSaturation, ccdef.AttrCurrentX, ccdef.AttrCurrentY,
	ccdef.AttrColorTemperatureMireds, ccdef.AttrEnhancedCurrentHue,
}

// The value ranges of ColorControlServer.ts (MIN_/MAX_ constants).
const (
	minCIEXYValue       = 0
	maxCIEXYValue       = 0xFEFF // "comes directly from the ZCL specification table 5.3"
	minHueValue         = 0
	maxHueValue         = 0xFE
	maxEnhancedHueValue = 0xFFFF
	minSaturationValue  = 0
	maxSaturationValue  = 0xFE
)

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

// optionExecuteIfOff is the ColorControl OptionsBitmap's only bit.
const optionExecuteIfOff uint8 = 0x01

// property reads a transitionable property.
func (st colorState) property(name string) (float64, bool) {
	switch name {
	case propHue:
		return float64(st.hue), true
	case propEnhancedHue:
		return float64(st.enhancedHue), true
	case propSaturation:
		return float64(st.saturation), true
	case propColorTemperature:
		return float64(st.mireds), true
	case propX:
		return float64(st.x), true
	case propY:
		return float64(st.y), true
	}
	return 0, false
}

// setProperty writes a transitionable property; v is rounded and within
// the property's bounds (the engine keeps it there).
func (st *colorState) setProperty(name string, v float64) {
	switch name {
	case propHue:
		st.hue = uint8(cropValue(v, minHueValue, maxHueValue))
	case propEnhancedHue:
		st.enhancedHue = uint16(cropValue(v, minHueValue, maxEnhancedHueValue))
	case propSaturation:
		st.saturation = uint8(cropValue(v, minSaturationValue, maxSaturationValue))
	case propColorTemperature:
		st.mireds = uint16(cropValue(v, 0, float64(maxTemperatureMireds)))
	case propX:
		st.x = uint16(cropValue(v, minCIEXYValue, maxCIEXYValue))
	case propY:
		st.y = uint16(cropValue(v, minCIEXYValue, maxCIEXYValue))
	}
}

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
	return cropValue(v, float64(s.minimumMireds()), float64(s.maximumMireds()))
}

// applyChanges is the engine's Apply: the values of one step, or of a
// transition that applies at once.
func (s *ColorControlServer) applyChanges(changes []transition.Change) error {
	return s.update(contract.WithOrigin(context.Background(), contract.OriginCommand), func(st *colorState) {
		for _, c := range changes {
			st.setProperty(c.Name, c.Value)
		}
	})
}

// update changes the colour state: it pushes the new colour temperature to
// the [ColorTemperatureWriter] and the new colour to the [ColorWriter],
// then makes the state the attributes' values and reports what changed —
// the "Q" attributes by their quieter rules, the rest at once. A refused
// value leaves every attribute where it was.
//
// The writers run with commit held but not mu, so a writer may call
// [ColorControlServer.Set] (brief AA's rule). A Set that stored the
// device's colour while the writers ran wins: the update then stores
// nothing and reports nothing.
func (s *ColorControlServer) update(ctx context.Context, change func(st *colorState)) error {
	s.commit.Lock()
	defer s.commit.Unlock()
	s.mu.Lock()
	prev := s.st
	next := prev
	change(&next)
	ct, cw := s.writer, s.colorWriter
	gen := s.setGen
	if next != prev {
		inflight := next
		s.inflight = &inflight
	}
	s.mu.Unlock()
	if next == prev {
		return nil
	}
	if err := s.write(ctx, ct, cw, prev, next); err != nil {
		s.mu.Lock()
		s.inflight = nil
		s.mu.Unlock()
		return err
	}
	s.mu.Lock()
	s.inflight = nil
	if s.setGen != gen {
		s.mu.Unlock()
		return nil
	}
	s.st = next
	s.mu.Unlock()
	s.report(contract.OriginFrom(ctx), prev, next)
	return nil
}

// write pushes what changed between prev and next to the writers.
func (s *ColorControlServer) write(ctx context.Context, ct ColorTemperatureWriter, cw ColorWriter, prev, next colorState) error {
	if ct != nil && next.mireds != prev.mireds {
		if err := ct.SetColorTemperatureMireds(ctx, next.mireds); err != nil {
			return fmt.Errorf("light: ColorTemperatureMireds write-through: %w", err)
		}
	}
	if cw != nil && next.color() != prev.color() {
		if err := cw.SetColor(ctx, next.color()); err != nil {
			return fmt.Errorf("light: colour write-through: %w", err)
		}
	}
	return nil
}

// report is the one place a change of the colour state is reported: the
// attributes that differ between prev and next, from origin. The change
// listeners see all of them at once, as matter.js's change observers do
// — the Datasource queues each committed change's notification with its
// session (Datasource.ts:1473-1476), and a QuietEvent's own observers are
// not throttled (Events.ts:267-297); on the wire the "Q"
// attributes follow their quieter rules (ServerBehaviorBacking.ts:265-273)
// — a quieter report it defers carries the origin of the last change
// before it — and the rest go at once.
func (s *ColorControlServer) report(origin contract.Origin, prev, next colorState) {
	s.cause.Store(uint32(origin))
	var all, quiet, plain []uint32
	add := func(id uint32, changed, isQuiet bool) {
		if !changed || !s.inst.Serves(id) {
			return
		}
		all = append(all, id)
		if isQuiet {
			quiet = append(quiet, id)
		} else {
			plain = append(plain, id)
		}
	}
	add(ccdef.AttrCurrentHue, prev.hue != next.hue, true)
	add(ccdef.AttrCurrentSaturation, prev.saturation != next.saturation, true)
	add(ccdef.AttrCurrentX, prev.x != next.x, true)
	add(ccdef.AttrCurrentY, prev.y != next.y, true)
	add(ccdef.AttrColorTemperatureMireds, prev.mireds != next.mireds, true)
	add(ccdef.AttrEnhancedCurrentHue, prev.enhancedHue != next.enhancedHue, true)
	add(ccdef.AttrColorMode, prev.mode != next.mode, false)
	add(ccdef.AttrEnhancedColorMode, prev.enhancedMode != next.enhancedMode, false)
	add(ccdef.AttrColorLoopActive, prev.loopActive != next.loopActive, false)
	add(ccdef.AttrColorLoopDirection, prev.loopDirection != next.loopDirection, false)
	add(ccdef.AttrColorLoopTime, prev.loopTime != next.loopTime, false)
	add(ccdef.AttrColorLoopStartEnhancedHue, prev.loopStart != next.loopStart, false)
	add(ccdef.AttrColorLoopStoredEnhancedHue, prev.loopStored != next.loopStored, false)
	if len(all) == 0 {
		return
	}
	slices.Sort(all)
	s.changes.ChangedFrom(origin, all...)
	for _, id := range quiet {
		s.quiet[id].Changed(false, false)
	}
	s.changes.ReportFrom(origin, plain...)
}

// OnAttributeChange implements [contract.AttributeChangeNotifier]: every
// change at once — the colour, its modes and its loop whoever changed
// them ([ColorControlServer.report]), Options and
// StartUpColorTemperatureMireds, and RemainingTime when matter.js's
// #updateRemainingTime would report it.
func (s *ColorControlServer) OnAttributeChange(cb func(change contract.Change)) (unsubscribe func()) {
	return s.changes.OnAttributeChange(cb)
}

// OnAttributeReport implements [contract.AttributeReportNotifier]: what
// goes on the wire — the "Q" attributes by their quieter rules, at most
// once a second while a transition runs and the end flushing the last
// value, every other change at once.
func (s *ColorControlServer) OnAttributeReport(cb func(change contract.Change)) (unsubscribe func()) {
	return s.changes.OnAttributeReport(cb)
}

// MatterSelfReportedAttributes implements
// [contract.SelfReportedAttributeLister]: every attribute the server
// changes is reported by the server — the "Q" ones, RemainingTime, the
// colour modes where the server has more than one, and the colour loop's.
func (s *ColorControlServer) MatterSelfReportedAttributes() []uint32 {
	ids := append(slices.Clone(quietAttributes), ccdef.AttrRemainingTime)
	modes := 0
	for _, m := range []ColorMode{ColorModeHueSaturation, ColorModeXY, ColorModeColorTemperature, ColorModeEnhancedHueSaturation} {
		if s.supportsColorMode(m) {
			modes++
		}
	}
	if modes > 1 {
		ids = append(ids, ccdef.AttrColorMode, ccdef.AttrEnhancedColorMode)
	}
	ids = append(ids, ccdef.AttrColorLoopActive, ccdef.AttrColorLoopDirection, ccdef.AttrColorLoopTime,
		ccdef.AttrColorLoopStartEnhancedHue, ccdef.AttrColorLoopStoredEnhancedHue)
	out := []uint32{}
	for _, id := range ids {
		if s.inst.Serves(id) {
			out = append(out, id)
		}
	}
	return out
}

// MatterQuiesce implements [contract.ClusterQuiescer]: a transition stops
// where it is, without a report (matter.js ColorControlServer
// [Symbol.asyncDispose]).
func (s *ColorControlServer) MatterQuiesce() { s.engine.StopAll() }

// executes is matter.js #optionsAllowExecution.
func (s *ColorControlServer) executes(mask, override uint8) bool {
	s.mu.Lock()
	options := ((s.options &^ mask) | (override & mask)) & optionExecuteIfOff
	s.mu.Unlock()
	return options&optionExecuteIfOff != 0 || s.onOff == nil || s.onOff.OnOff()
}

// state returns a copy of the colour state.
func (s *ColorControlServer) state() colorState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st
}

// startTransition is matter.js #startTransition: a hue transition stops
// the other hue's, then the engine starts it.
func (s *ColorControlServer) startTransition(t transition.Transition) error {
	return s.startTransitions(t)
}

// startTransitions starts the transitions of one command as one
// (matter.js starts them inside the command's transaction, so the
// immediate ones commit together — one update, one write to the
// ColorWriter, #84): #startTransition's hue rule for each, then
// [transition.Engine.StartAll].
func (s *ColorControlServer) startTransitions(ts ...transition.Transition) error {
	for _, t := range ts {
		switch t.Name {
		case propHue:
			s.engine.Stop(propEnhancedHue)
		case propEnhancedHue:
			s.engine.Stop(propHue)
		}
	}
	return s.engine.StartAll(ts...)
}

// stopHueAndSaturationMovement is matter.js stopHueAndSaturationMovement.
func (s *ColorControlServer) stopHueAndSaturationMovement() {
	s.engine.Stop(propHue)
	s.engine.Stop(propEnhancedHue)
	s.engine.Stop(propSaturation)
}

// stopAllColorMovement is matter.js stopAllColorMovement.
func (s *ColorControlServer) stopAllColorMovement() {
	s.engine.Stop(propX)
	s.engine.Stop(propY)
	s.engine.Stop(propColorTemperature)
	s.stopHueAndSaturationMovement()
}

// endTransitions is matter.js #endTransitions: the remaining time becomes
// zero and is reported. (The application-stated transitionEndTime it also
// clears is not ported, BD-Matter-LevelControl-NativeRamp.)
func (s *ColorControlServer) endTransitions() { s.engine.CancelAll() }

// SyncColorTemperatureWithLevel moves the colour temperature to the value
// level maps to while LevelControl's CoupleColorTempToLevel option is in
// effect and the light is in the colour temperature mode: matter.js
// ColorControlServer.syncColorTemperatureWithLevel and
// syncColorTemperatureWithLevelLogic. The minimum level maps to the
// physical maximum, the maximum level to CoupleColorTempToLevelMinMireds,
// a level in between linearly; the value applies at once. It implements
// levelcontrol.ColorTemperatureCoupling.
func (s *ColorControlServer) SyncColorTemperatureWithLevel(_ context.Context, level uint8) error {
	s.cmd.Lock()
	defer s.cmd.Unlock()
	if st := s.state(); st.mode != ColorModeColorTemperature && st.enhancedMode != ColorModeColorTemperature {
		return nil
	}
	// Mirrors matter.js
	// packages/node/src/behaviors/color-control/ColorControlServer.ts:1582
	// (tempCoupleMin: coupleColorTempToLevelMinMireds, else the minimum).
	coupleMin := float64(s.minimumMireds())
	if m := s.cfg.CoupleColorTempToLevelMinMireds; m != 0 {
		coupleMin = float64(m)
	}
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
