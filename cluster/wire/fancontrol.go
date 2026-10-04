// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package wire

// FanControlClusterID is the FanControl cluster id
// (matter.js packages/model/src/standard/elements/fan-control.element.ts:19).
const FanControlClusterID uint32 = 0x0202

// FanControlCmdStep is the FanControl Step command: conformance STEP,
// response "status" (fan-control.element.ts:69-74).
const FanControlCmdStep uint32 = 0x00

// Step command field tags (fan-control.element.ts:71-73).
const (
	FanStepFieldDirection uint8 = 0 // StepDirectionEnum, M
	FanStepFieldWrap      uint8 = 1 // bool, O, default false
	FanStepFieldLowestOff uint8 = 2 // bool, O, default true
)

// FanStepRequest is the decoded Step payload. The two optional fields
// arrive with their element defaults applied — Wrap false, LowestOff
// true — so a server never has to tell "absent" from "the default".
type FanStepRequest struct {
	// Direction is the StepDirectionEnum: 0 Increase, 1 Decrease
	// (fan-control.element.ts:88-92).
	Direction uint8
	// Wrap: the speed-oriented attributes wrap between the highest and
	// the lowest step value.
	Wrap bool
	// LowestOff: off (FanMode Off, PercentSetting 0, SpeedSetting 0) is
	// one of the step values.
	LowestOff bool
}
