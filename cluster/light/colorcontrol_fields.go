// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package light

import (
	"fmt"
	"math"
	"slices"

	"github.com/SukramJ/go-fabric/cluster"
	ccdef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
	"github.com/SukramJ/go-fabric/cluster/wire"
)

// A command's fields reach the server in one of three shapes: the
// generated request struct the bridge decodes through the ColorControl
// definition (cluster/spec/colorcontrol, with matter.js's TlvSchema checks
// and statuses), the hand-written cluster/wire structs a caller in this
// module builds (the ScenesManagement recall), or the generic tag map an
// in-process caller hands over. Every shape is normalised to the generated
// struct, and its values are checked against the field constraints and
// enums of color-control.element.ts — CONSTRAINT_ERROR, as matter.js's
// ValueValidator answers a request field out of range — so a shape that
// bypassed the decoder is held to the same rules.

// requestOf normalises fields to T: T itself, a *T, a legacy struct legacy
// converts, or a tag map fromMap reads.
func requestOf[T any](fields any, name string, legacy func(any) (T, bool), fromMap func(map[uint8]any) T) (T, error) {
	switch v := fields.(type) {
	case map[uint8]any:
		return fromMap(v), nil
	case *T:
		if v != nil {
			return *v, nil
		}
	case T:
		return v, nil
	default:
		if legacy != nil {
			if r, ok := legacy(fields); ok {
				return r, nil
			}
		}
	}
	var zero T
	return zero, colorControlInvalidCommandErr(fmt.Sprintf("colorcontrol: %s carried %T", name, fields))
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

// tagInt reads a signed field of the generic tag map, 0 when absent or
// outside lo..hi.
func tagInt(m map[uint8]any, tag uint8, lo, hi int64) int64 {
	var n int64
	switch v := m[tag].(type) {
	case int64:
		n = v
	case int:
		n = int64(v)
	case int32:
		n = int64(v)
	case int16:
		n = int64(v)
	case int8:
		n = int64(v)
	case uint64:
		if v > math.MaxInt64 {
			return 0
		}
		n = int64(v)
	default:
		return 0
	}
	if n < lo || n > hi {
		return 0
	}
	return n
}

// u8 / u16 read bounded tag map fields.
func u8(m map[uint8]any, tag uint8) uint8 {
	return uint8(tagUint(m, tag, math.MaxUint8)) //nolint:gosec // bounded by tagUint
}

func u16(m map[uint8]any, tag uint8) uint16 {
	return uint16(tagUint(m, tag, math.MaxUint16)) //nolint:gosec // bounded by tagUint
}

func i16(m map[uint8]any, tag uint8) int16 {
	return int16(tagInt(m, tag, math.MinInt16, math.MaxInt16)) //nolint:gosec // bounded by tagInt
}

func opts(m map[uint8]any, tag uint8) ccdef.OptionsBitmap { return ccdef.OptionsBitmap(u8(m, tag)) }

// The field constraints of color-control.element.ts.
const (
	maxHueField        = 254   // Hue, "max 254"
	maxSaturationField = 254   // Saturation, "max 254"
	maxTransitionTime  = 65534 // TransitionTime (uint16), "max 65534"
	maxColorXY         = 65279 // ColorX / ColorY, "max 65279"
	maxMiredsField     = 65279 // ColorTemperatureMireds and the Move / Step bounds, "max 65279"
)

// checkMax refuses a field above its constraint.
func checkMax(cmd, field string, v, limit uint64) error {
	if v > limit {
		return colorControlConstraintErr(fmt.Sprintf("colorcontrol: %s %s %d exceeds %d", cmd, field, v, limit))
	}
	return nil
}

// checkEnum refuses a value the enum does not define.
func checkEnum[T ~uint8](cmd, field string, v T, defined ...T) error {
	if !slices.Contains(defined, v) {
		return colorControlConstraintErr(fmt.Sprintf("colorcontrol: %s %s %d is not defined", cmd, field, v))
	}
	return nil
}

func checkDirection(cmd string, d ccdef.DirectionEnum) error {
	return checkEnum(cmd, "Direction", d, ccdef.DirectionShortest, ccdef.DirectionLongest, ccdef.DirectionUp, ccdef.DirectionDown)
}

func checkMoveMode(cmd string, m ccdef.MoveModeEnum) error {
	return checkEnum(cmd, "MoveMode", m, ccdef.MoveModeStop, ccdef.MoveModeUp, ccdef.MoveModeDown)
}

func checkStepMode(cmd string, m ccdef.StepModeEnum) error {
	return checkEnum(cmd, "StepMode", m, ccdef.StepModeUp, ccdef.StepModeDown)
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func moveToHueFields(fields any) (ccdef.MoveToHueRequest, error) {
	r, err := requestOf(fields, "MoveToHue",
		func(v any) (ccdef.MoveToHueRequest, bool) {
			w, ok := v.(wire.MoveToHueRequest)
			return ccdef.MoveToHueRequest{
				Hue: w.Hue, Direction: ccdef.DirectionEnum(w.Direction), TransitionTime: w.TransitionTime,
				OptionsMask: ccdef.OptionsBitmap(w.OptionsMask), OptionsOverride: ccdef.OptionsBitmap(w.OptionsOverride),
			}, ok
		},
		func(m map[uint8]any) ccdef.MoveToHueRequest {
			return ccdef.MoveToHueRequest{
				Hue: u8(m, 0), Direction: ccdef.DirectionEnum(u8(m, 1)), TransitionTime: u16(m, 2),
				OptionsMask: opts(m, 3), OptionsOverride: opts(m, 4),
			}
		})
	if err != nil {
		return r, err
	}
	return r, firstErr(
		checkMax("MoveToHue", "Hue", uint64(r.Hue), maxHueField),
		checkDirection("MoveToHue", r.Direction),
		checkMax("MoveToHue", "TransitionTime", uint64(r.TransitionTime), maxTransitionTime),
	)
}

func moveHueFields(fields any) (ccdef.MoveHueRequest, error) {
	r, err := requestOf(fields, "MoveHue",
		func(v any) (ccdef.MoveHueRequest, bool) {
			w, ok := v.(wire.MoveHueRequest)
			return ccdef.MoveHueRequest{
				MoveMode: ccdef.MoveModeEnum(w.MoveMode), Rate: w.Rate,
				OptionsMask: ccdef.OptionsBitmap(w.OptionsMask), OptionsOverride: ccdef.OptionsBitmap(w.OptionsOverride),
			}, ok
		},
		func(m map[uint8]any) ccdef.MoveHueRequest {
			return ccdef.MoveHueRequest{
				MoveMode: ccdef.MoveModeEnum(u8(m, 0)), Rate: u8(m, 1), OptionsMask: opts(m, 2), OptionsOverride: opts(m, 3),
			}
		})
	if err != nil {
		return r, err
	}
	return r, checkMoveMode("MoveHue", r.MoveMode)
}

func stepHueFields(fields any) (ccdef.StepHueRequest, error) {
	r, err := requestOf(fields, "StepHue",
		func(v any) (ccdef.StepHueRequest, bool) {
			w, ok := v.(wire.StepHueRequest)
			return ccdef.StepHueRequest{
				StepMode: ccdef.StepModeEnum(w.StepMode), StepSize: w.StepSize,
				TransitionTime: uint8(min(w.TransitionTime, math.MaxUint8)), //nolint:gosec // bounded by MaxUint8
				OptionsMask:    ccdef.OptionsBitmap(w.OptionsMask), OptionsOverride: ccdef.OptionsBitmap(w.OptionsOverride),
			}, ok && w.TransitionTime <= math.MaxUint8
		},
		func(m map[uint8]any) ccdef.StepHueRequest {
			return ccdef.StepHueRequest{
				StepMode: ccdef.StepModeEnum(u8(m, 0)), StepSize: u8(m, 1), TransitionTime: u8(m, 2),
				OptionsMask: opts(m, 3), OptionsOverride: opts(m, 4),
			}
		})
	if err != nil {
		return r, err
	}
	return r, checkStepMode("StepHue", r.StepMode)
}

func moveToSaturationFields(fields any) (ccdef.MoveToSaturationRequest, error) {
	r, err := requestOf(fields, "MoveToSaturation",
		func(v any) (ccdef.MoveToSaturationRequest, bool) {
			w, ok := v.(wire.MoveToSaturationRequest)
			return ccdef.MoveToSaturationRequest{
				Saturation: w.Saturation, TransitionTime: w.TransitionTime,
				OptionsMask: ccdef.OptionsBitmap(w.OptionsMask), OptionsOverride: ccdef.OptionsBitmap(w.OptionsOverride),
			}, ok
		},
		func(m map[uint8]any) ccdef.MoveToSaturationRequest {
			return ccdef.MoveToSaturationRequest{
				Saturation: u8(m, 0), TransitionTime: u16(m, 1), OptionsMask: opts(m, 2), OptionsOverride: opts(m, 3),
			}
		})
	if err != nil {
		return r, err
	}
	return r, firstErr(
		checkMax("MoveToSaturation", "Saturation", uint64(r.Saturation), maxSaturationField),
		checkMax("MoveToSaturation", "TransitionTime", uint64(r.TransitionTime), maxTransitionTime),
	)
}

func moveSaturationFields(fields any) (ccdef.MoveSaturationRequest, error) {
	r, err := requestOf(fields, "MoveSaturation",
		func(v any) (ccdef.MoveSaturationRequest, bool) {
			w, ok := v.(wire.MoveSaturationRequest)
			return ccdef.MoveSaturationRequest{
				MoveMode: ccdef.MoveModeEnum(w.MoveMode), Rate: w.Rate,
				OptionsMask: ccdef.OptionsBitmap(w.OptionsMask), OptionsOverride: ccdef.OptionsBitmap(w.OptionsOverride),
			}, ok
		},
		func(m map[uint8]any) ccdef.MoveSaturationRequest {
			return ccdef.MoveSaturationRequest{
				MoveMode: ccdef.MoveModeEnum(u8(m, 0)), Rate: u8(m, 1), OptionsMask: opts(m, 2), OptionsOverride: opts(m, 3),
			}
		})
	if err != nil {
		return r, err
	}
	return r, checkMoveMode("MoveSaturation", r.MoveMode)
}

func stepSaturationFields(fields any) (ccdef.StepSaturationRequest, error) {
	r, err := requestOf(fields, "StepSaturation",
		func(v any) (ccdef.StepSaturationRequest, bool) {
			w, ok := v.(wire.StepSaturationRequest)
			return ccdef.StepSaturationRequest{
				StepMode: ccdef.StepModeEnum(w.StepMode), StepSize: w.StepSize,
				TransitionTime: uint8(min(w.TransitionTime, math.MaxUint8)), //nolint:gosec // bounded by MaxUint8
				OptionsMask:    ccdef.OptionsBitmap(w.OptionsMask), OptionsOverride: ccdef.OptionsBitmap(w.OptionsOverride),
			}, ok && w.TransitionTime <= math.MaxUint8
		},
		func(m map[uint8]any) ccdef.StepSaturationRequest {
			return ccdef.StepSaturationRequest{
				StepMode: ccdef.StepModeEnum(u8(m, 0)), StepSize: u8(m, 1), TransitionTime: u8(m, 2),
				OptionsMask: opts(m, 3), OptionsOverride: opts(m, 4),
			}
		})
	if err != nil {
		return r, err
	}
	return r, checkStepMode("StepSaturation", r.StepMode)
}

func moveToHueAndSaturationFields(fields any) (ccdef.MoveToHueAndSaturationRequest, error) {
	r, err := requestOf(fields, "MoveToHueAndSaturation",
		func(v any) (ccdef.MoveToHueAndSaturationRequest, bool) {
			w, ok := v.(wire.MoveToHueAndSaturationRequest)
			return ccdef.MoveToHueAndSaturationRequest{
				Hue: w.Hue, Saturation: w.Saturation, TransitionTime: w.TransitionTime,
				OptionsMask: ccdef.OptionsBitmap(w.OptionsMask), OptionsOverride: ccdef.OptionsBitmap(w.OptionsOverride),
			}, ok
		},
		func(m map[uint8]any) ccdef.MoveToHueAndSaturationRequest {
			return ccdef.MoveToHueAndSaturationRequest{
				Hue: u8(m, 0), Saturation: u8(m, 1), TransitionTime: u16(m, 2), OptionsMask: opts(m, 3), OptionsOverride: opts(m, 4),
			}
		})
	if err != nil {
		return r, err
	}
	return r, firstErr(
		checkMax("MoveToHueAndSaturation", "Hue", uint64(r.Hue), maxHueField),
		checkMax("MoveToHueAndSaturation", "Saturation", uint64(r.Saturation), maxSaturationField),
		checkMax("MoveToHueAndSaturation", "TransitionTime", uint64(r.TransitionTime), maxTransitionTime),
	)
}

func moveToColorFields(fields any) (ccdef.MoveToColorRequest, error) {
	r, err := requestOf(fields, "MoveToColor", nil,
		func(m map[uint8]any) ccdef.MoveToColorRequest {
			return ccdef.MoveToColorRequest{
				ColorX: u16(m, 0), ColorY: u16(m, 1), TransitionTime: u16(m, 2), OptionsMask: opts(m, 3), OptionsOverride: opts(m, 4),
			}
		})
	if err != nil {
		return r, err
	}
	return r, firstErr(
		checkMax("MoveToColor", "ColorX", uint64(r.ColorX), maxColorXY),
		checkMax("MoveToColor", "ColorY", uint64(r.ColorY), maxColorXY),
		checkMax("MoveToColor", "TransitionTime", uint64(r.TransitionTime), maxTransitionTime),
	)
}

func moveColorFields(fields any) (ccdef.MoveColorRequest, error) {
	return requestOf(fields, "MoveColor", nil,
		func(m map[uint8]any) ccdef.MoveColorRequest {
			return ccdef.MoveColorRequest{RateX: i16(m, 0), RateY: i16(m, 1), OptionsMask: opts(m, 2), OptionsOverride: opts(m, 3)}
		})
}

func stepColorFields(fields any) (ccdef.StepColorRequest, error) {
	r, err := requestOf(fields, "StepColor", nil,
		func(m map[uint8]any) ccdef.StepColorRequest {
			return ccdef.StepColorRequest{
				StepX: i16(m, 0), StepY: i16(m, 1), TransitionTime: u16(m, 2), OptionsMask: opts(m, 3), OptionsOverride: opts(m, 4),
			}
		})
	if err != nil {
		return r, err
	}
	return r, checkMax("StepColor", "TransitionTime", uint64(r.TransitionTime), maxTransitionTime)
}

func moveToColorTemperatureFields(fields any) (ccdef.MoveToColorTemperatureRequest, error) {
	r, err := requestOf(fields, "MoveToColorTemperature",
		func(v any) (ccdef.MoveToColorTemperatureRequest, bool) {
			w, ok := v.(wire.MoveToColorTemperatureRequest)
			return ccdef.MoveToColorTemperatureRequest{
				ColorTemperatureMireds: w.ColorTemperatureMireds, TransitionTime: w.TransitionTime,
				OptionsMask: ccdef.OptionsBitmap(w.OptionsMask), OptionsOverride: ccdef.OptionsBitmap(w.OptionsOverride),
			}, ok
		},
		func(m map[uint8]any) ccdef.MoveToColorTemperatureRequest {
			return ccdef.MoveToColorTemperatureRequest{
				ColorTemperatureMireds: u16(m, 0), TransitionTime: u16(m, 1), OptionsMask: opts(m, 2), OptionsOverride: opts(m, 3),
			}
		})
	if err != nil {
		return r, err
	}
	return r, firstErr(
		checkMax("MoveToColorTemperature", "ColorTemperatureMireds", uint64(r.ColorTemperatureMireds), maxMiredsField),
		checkMax("MoveToColorTemperature", "TransitionTime", uint64(r.TransitionTime), maxTransitionTime),
	)
}

func moveColorTemperatureFields(fields any) (ccdef.MoveColorTemperatureRequest, error) {
	r, err := requestOf(fields, "MoveColorTemperature", nil,
		func(m map[uint8]any) ccdef.MoveColorTemperatureRequest {
			return ccdef.MoveColorTemperatureRequest{
				MoveMode: ccdef.MoveModeEnum(u8(m, 0)), Rate: u16(m, 1), ColorTemperatureMinimumMireds: u16(m, 2),
				ColorTemperatureMaximumMireds: u16(m, 3), OptionsMask: opts(m, 4), OptionsOverride: opts(m, 5),
			}
		})
	if err != nil {
		return r, err
	}
	return r, firstErr(
		checkMoveMode("MoveColorTemperature", r.MoveMode),
		checkMax("MoveColorTemperature", "ColorTemperatureMinimumMireds", uint64(r.ColorTemperatureMinimumMireds), maxMiredsField),
		checkMax("MoveColorTemperature", "ColorTemperatureMaximumMireds", uint64(r.ColorTemperatureMaximumMireds), maxMiredsField),
	)
}

func stepColorTemperatureFields(fields any) (ccdef.StepColorTemperatureRequest, error) {
	r, err := requestOf(fields, "StepColorTemperature", nil,
		func(m map[uint8]any) ccdef.StepColorTemperatureRequest {
			return ccdef.StepColorTemperatureRequest{
				StepMode: ccdef.StepModeEnum(u8(m, 0)), StepSize: u16(m, 1), TransitionTime: u16(m, 2),
				ColorTemperatureMinimumMireds: u16(m, 3), ColorTemperatureMaximumMireds: u16(m, 4),
				OptionsMask: opts(m, 5), OptionsOverride: opts(m, 6),
			}
		})
	if err != nil {
		return r, err
	}
	return r, firstErr(
		checkStepMode("StepColorTemperature", r.StepMode),
		checkMax("StepColorTemperature", "TransitionTime", uint64(r.TransitionTime), maxTransitionTime),
		checkMax("StepColorTemperature", "ColorTemperatureMinimumMireds", uint64(r.ColorTemperatureMinimumMireds), maxMiredsField),
		checkMax("StepColorTemperature", "ColorTemperatureMaximumMireds", uint64(r.ColorTemperatureMaximumMireds), maxMiredsField),
	)
}

func enhancedMoveToHueFields(fields any) (ccdef.EnhancedMoveToHueRequest, error) {
	r, err := requestOf(fields, "EnhancedMoveToHue", nil,
		func(m map[uint8]any) ccdef.EnhancedMoveToHueRequest {
			return ccdef.EnhancedMoveToHueRequest{
				EnhancedHue: u16(m, 0), Direction: ccdef.DirectionEnum(u8(m, 1)), TransitionTime: u16(m, 2),
				OptionsMask: opts(m, 3), OptionsOverride: opts(m, 4),
			}
		})
	if err != nil {
		return r, err
	}
	return r, firstErr(
		checkDirection("EnhancedMoveToHue", r.Direction),
		checkMax("EnhancedMoveToHue", "TransitionTime", uint64(r.TransitionTime), maxTransitionTime),
	)
}

func enhancedMoveHueFields(fields any) (ccdef.EnhancedMoveHueRequest, error) {
	r, err := requestOf(fields, "EnhancedMoveHue", nil,
		func(m map[uint8]any) ccdef.EnhancedMoveHueRequest {
			return ccdef.EnhancedMoveHueRequest{
				MoveMode: ccdef.MoveModeEnum(u8(m, 0)), Rate: u16(m, 1), OptionsMask: opts(m, 2), OptionsOverride: opts(m, 3),
			}
		})
	if err != nil {
		return r, err
	}
	return r, checkMoveMode("EnhancedMoveHue", r.MoveMode)
}

func enhancedStepHueFields(fields any) (ccdef.EnhancedStepHueRequest, error) {
	r, err := requestOf(fields, "EnhancedStepHue", nil,
		func(m map[uint8]any) ccdef.EnhancedStepHueRequest {
			return ccdef.EnhancedStepHueRequest{
				StepMode: ccdef.StepModeEnum(u8(m, 0)), StepSize: u16(m, 1), TransitionTime: u16(m, 2),
				OptionsMask: opts(m, 3), OptionsOverride: opts(m, 4),
			}
		})
	if err != nil {
		return r, err
	}
	return r, firstErr(
		checkStepMode("EnhancedStepHue", r.StepMode),
		checkMax("EnhancedStepHue", "TransitionTime", uint64(r.TransitionTime), maxTransitionTime),
	)
}

func enhancedMoveToHueAndSaturationFields(fields any) (ccdef.EnhancedMoveToHueAndSaturationRequest, error) {
	r, err := requestOf(fields, "EnhancedMoveToHueAndSaturation", nil,
		func(m map[uint8]any) ccdef.EnhancedMoveToHueAndSaturationRequest {
			return ccdef.EnhancedMoveToHueAndSaturationRequest{
				EnhancedHue: u16(m, 0), Saturation: u8(m, 1), TransitionTime: u16(m, 2),
				OptionsMask: opts(m, 3), OptionsOverride: opts(m, 4),
			}
		})
	if err != nil {
		return r, err
	}
	return r, firstErr(
		checkMax("EnhancedMoveToHueAndSaturation", "Saturation", uint64(r.Saturation), maxSaturationField),
		checkMax("EnhancedMoveToHueAndSaturation", "TransitionTime", uint64(r.TransitionTime), maxTransitionTime),
	)
}

func colorLoopSetFields(fields any) (ccdef.ColorLoopSetRequest, error) {
	r, err := requestOf(fields, "ColorLoopSet", nil,
		func(m map[uint8]any) ccdef.ColorLoopSetRequest {
			return ccdef.ColorLoopSetRequest{
				UpdateFlags: ccdef.UpdateFlagsBitmap(u8(m, 0)), Action: ccdef.ColorLoopActionEnum(u8(m, 1)),
				Direction: ccdef.ColorLoopDirectionEnum(u8(m, 2)), Time: u16(m, 3), StartHue: u16(m, 4),
				OptionsMask: opts(m, 5), OptionsOverride: opts(m, 6),
			}
		})
	if err != nil {
		return r, err
	}
	return r, firstErr(
		checkEnum("ColorLoopSet", "Action", r.Action, ccdef.ColorLoopActionDeactivate,
			ccdef.ColorLoopActionActivateFromColorLoopStartEnhancedHue, ccdef.ColorLoopActionActivateFromEnhancedCurrentHue),
		checkEnum("ColorLoopSet", "Direction", r.Direction, ccdef.ColorLoopDirectionDecrement, ccdef.ColorLoopDirectionIncrement),
	)
}

// stopMoveStepFields reads StopMoveStep's options; a call without fields
// uses none.
func stopMoveStepFields(fields any) (ccdef.StopMoveStepRequest, error) {
	if fields == nil {
		return ccdef.StopMoveStepRequest{}, nil
	}
	return requestOf(fields, "StopMoveStep", nil,
		func(m map[uint8]any) ccdef.StopMoveStepRequest {
			return ccdef.StopMoveStepRequest{OptionsMask: opts(m, 0), OptionsOverride: opts(m, 1)}
		})
}
