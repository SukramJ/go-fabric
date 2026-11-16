// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package wire

// ColorControlClusterID is the Matter cluster ID for ColorControl.
const ColorControlClusterID uint32 = 0x0300

// ColorControl attribute IDs per Matter §3.2.6.
const (
	ColorCtrlAttrCurrentHue        uint32 = 0x0000
	ColorCtrlAttrCurrentSaturation uint32 = 0x0001
	ColorCtrlAttrCurrentX          uint32 = 0x0003
	ColorCtrlAttrCurrentY          uint32 = 0x0004
	// ColorCtrlAttrRemainingTime is RemainingTime (0x0002): conformance "O"
	// on the cluster, made mandatory by the ColorTemperatureLight device
	// type (color-temperature-light.element.ts element requirement).
	ColorCtrlAttrRemainingTime          uint32 = 0x0002
	ColorCtrlAttrColorTemperatureMireds uint32 = 0x0007
	ColorCtrlAttrColorMode              uint32 = 0x0008
	ColorCtrlAttrOptions                uint32 = 0x000F
	ColorCtrlAttrNumberOfPrimaries      uint32 = 0x0010 // nullable uint8, quality X; mandatory per spec §3.2.6
	ColorCtrlAttrEnhancedColorMode      uint32 = 0x4001
	ColorCtrlAttrColorCapabilities      uint32 = 0x400A
	ColorCtrlAttrColorTempPhysicalMin   uint32 = 0x400B
	ColorCtrlAttrColorTempPhysicalMax   uint32 = 0x400C
	// ColorCtrlAttrCoupleColorTempToLevelMinMireds and
	// ColorCtrlAttrStartUpColorTemperatureMireds carry conformance "CT"
	// (matter.js color-control.element.ts:183-189).
	ColorCtrlAttrCoupleColorTempToLevelMinMireds uint32 = 0x400D
	ColorCtrlAttrStartUpColorTemperatureMireds   uint32 = 0x4010
)

// ColorControl command IDs per Matter §3.2.7.
const (
	// ColorCtrlCmdMoveToHue duplicates a generated constant.
	//
	// Deprecated: use CmdMoveToHue of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in the next minor release.
	ColorCtrlCmdMoveToHue uint32 = 0x00
	// ColorCtrlCmdMoveHue duplicates a generated constant.
	//
	// Deprecated: use CmdMoveHue of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in the next minor release.
	ColorCtrlCmdMoveHue uint32 = 0x01
	// ColorCtrlCmdStepHue duplicates a generated constant.
	//
	// Deprecated: use CmdStepHue of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in the next minor release.
	ColorCtrlCmdStepHue uint32 = 0x02
	// ColorCtrlCmdMoveToSaturation duplicates a generated constant.
	//
	// Deprecated: use CmdMoveToSaturation of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in the next minor release.
	ColorCtrlCmdMoveToSaturation uint32 = 0x03
	// ColorCtrlCmdMoveSaturation duplicates a generated constant.
	//
	// Deprecated: use CmdMoveSaturation of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in the next minor release.
	ColorCtrlCmdMoveSaturation uint32 = 0x04
	// ColorCtrlCmdStepSaturation duplicates a generated constant.
	//
	// Deprecated: use CmdStepSaturation of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in the next minor release.
	ColorCtrlCmdStepSaturation uint32 = 0x05
	// ColorCtrlCmdMoveToHueAndSaturation duplicates a generated constant.
	//
	// Deprecated: use CmdMoveToHueAndSaturation of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in the next minor release.
	ColorCtrlCmdMoveToHueAndSaturation uint32 = 0x06
	// ColorCtrlCmdMoveToColor duplicates a generated constant.
	//
	// Deprecated: use CmdMoveToColor of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in the next minor release.
	ColorCtrlCmdMoveToColor uint32 = 0x07
	// ColorCtrlCmdMoveColor duplicates a generated constant.
	//
	// Deprecated: use CmdMoveColor of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in the next minor release.
	ColorCtrlCmdMoveColor uint32 = 0x08
	// ColorCtrlCmdStepColor duplicates a generated constant.
	//
	// Deprecated: use CmdStepColor of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in the next minor release.
	ColorCtrlCmdStepColor              uint32 = 0x09
	ColorCtrlCmdMoveToColorTemperature uint32 = 0x0A
	ColorCtrlCmdStopMoveStep           uint32 = 0x47
	ColorCtrlCmdMoveColorTemperature   uint32 = 0x4B
	ColorCtrlCmdStepColorTemperature   uint32 = 0x4C
)

// HueDirection values per Matter §3.2.7.4.2.
const (
	// ColorHueDirShortest duplicates a generated constant.
	//
	// Deprecated: use DirectionShortest of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in the next minor release.
	ColorHueDirShortest uint8 = 0
	// ColorHueDirLongest duplicates a generated constant.
	//
	// Deprecated: use DirectionLongest of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in the next minor release.
	ColorHueDirLongest uint8 = 1
	// ColorHueDirUp duplicates a generated constant.
	//
	// Deprecated: use DirectionUp of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in the next minor release.
	ColorHueDirUp uint8 = 2
	// ColorHueDirDown duplicates a generated constant.
	//
	// Deprecated: use DirectionDown of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in the next minor release.
	ColorHueDirDown uint8 = 3
)

// ColorMoveMode values per Matter §3.2.7.5 / §3.2.7.8.
const (
	// ColorMoveModeStop duplicates a generated constant.
	//
	// Deprecated: use MoveModeStop of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in the next minor release.
	ColorMoveModeStop uint8 = 0
	// ColorMoveModeUp duplicates a generated constant.
	//
	// Deprecated: use MoveModeUp of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in the next minor release.
	ColorMoveModeUp uint8 = 1
	// ColorMoveModeDown duplicates a generated constant.
	//
	// Deprecated: use MoveModeDown of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in the next minor release.
	ColorMoveModeDown uint8 = 3
)

// ColorStepMode values per Matter §3.2.7.6 / §3.2.7.9.
const (
	// ColorStepModeUp duplicates a generated constant.
	//
	// Deprecated: use StepModeUp of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in the next minor release.
	ColorStepModeUp uint8 = 1
	// ColorStepModeDown duplicates a generated constant.
	//
	// Deprecated: use StepModeDown of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in the next minor release.
	ColorStepModeDown uint8 = 3
)
