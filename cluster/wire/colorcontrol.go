// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package wire

// ColorControlClusterID is the Matter cluster ID for ColorControl.
//
// Deprecated: use ClusterID of cluster/spec/colorcontrol, the
// generated id: one source of ids (review II, R2-P6-par-1262,
// decision 140). Removable in v0.25.0.
const ColorControlClusterID uint32 = 0x0300

// ColorControl attribute IDs per Matter §3.2.6.
const (
	// ColorCtrlAttrCurrentHue duplicates a generated constant.
	//
	// Deprecated: use AttrCurrentHue of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in v0.25.0.
	ColorCtrlAttrCurrentHue uint32 = 0x0000
	// ColorCtrlAttrCurrentSaturation duplicates a generated constant.
	//
	// Deprecated: use AttrCurrentSaturation of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in v0.25.0.
	ColorCtrlAttrCurrentSaturation uint32 = 0x0001
	// ColorCtrlAttrCurrentX duplicates a generated constant.
	//
	// Deprecated: use AttrCurrentX of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in v0.25.0.
	ColorCtrlAttrCurrentX uint32 = 0x0003
	// ColorCtrlAttrCurrentY duplicates a generated constant.
	//
	// Deprecated: use AttrCurrentY of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in v0.25.0.
	ColorCtrlAttrCurrentY uint32 = 0x0004
	// ColorCtrlAttrRemainingTime is RemainingTime (0x0002): conformance "O"
	// on the cluster, made mandatory by the ColorTemperatureLight device
	// type (color-temperature-light.element.ts element requirement).
	//
	// Deprecated: use AttrRemainingTime of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in v0.25.0.
	ColorCtrlAttrRemainingTime uint32 = 0x0002
	// ColorCtrlAttrColorTemperatureMireds duplicates a generated constant.
	//
	// Deprecated: use AttrColorTemperatureMireds of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in v0.25.0.
	ColorCtrlAttrColorTemperatureMireds uint32 = 0x0007
	// ColorCtrlAttrColorMode duplicates a generated constant.
	//
	// Deprecated: use AttrColorMode of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in v0.25.0.
	ColorCtrlAttrColorMode uint32 = 0x0008
	// ColorCtrlAttrOptions duplicates a generated constant.
	//
	// Deprecated: use AttrOptions of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in v0.25.0.
	ColorCtrlAttrOptions uint32 = 0x000F
	// ColorCtrlAttrNumberOfPrimaries duplicates a generated constant.
	//
	// Deprecated: use AttrNumberOfPrimaries of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in v0.25.0.
	ColorCtrlAttrNumberOfPrimaries uint32 = 0x0010 // nullable uint8, quality X; mandatory per spec §3.2.6
	// ColorCtrlAttrEnhancedColorMode duplicates a generated constant.
	//
	// Deprecated: use AttrEnhancedColorMode of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in v0.25.0.
	ColorCtrlAttrEnhancedColorMode uint32 = 0x4001
	// ColorCtrlAttrColorCapabilities duplicates a generated constant.
	//
	// Deprecated: use AttrColorCapabilities of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in v0.25.0.
	ColorCtrlAttrColorCapabilities uint32 = 0x400A
	// ColorCtrlAttrColorTempPhysicalMin duplicates a generated constant.
	//
	// Deprecated: use AttrColorTempPhysicalMinMireds of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in v0.25.0.
	ColorCtrlAttrColorTempPhysicalMin uint32 = 0x400B
	// ColorCtrlAttrColorTempPhysicalMax duplicates a generated constant.
	//
	// Deprecated: use AttrColorTempPhysicalMaxMireds of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in v0.25.0.
	ColorCtrlAttrColorTempPhysicalMax uint32 = 0x400C
	// ColorCtrlAttrCoupleColorTempToLevelMinMireds and
	// ColorCtrlAttrStartUpColorTemperatureMireds carry conformance "CT"
	// (matter.js color-control.element.ts:183-189).
	//
	// Deprecated: use AttrCoupleColorTempToLevelMinMireds of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in v0.25.0.
	ColorCtrlAttrCoupleColorTempToLevelMinMireds uint32 = 0x400D
	// ColorCtrlAttrStartUpColorTemperatureMireds duplicates a generated constant.
	//
	// Deprecated: use AttrStartUpColorTemperatureMireds of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in v0.25.0.
	ColorCtrlAttrStartUpColorTemperatureMireds uint32 = 0x4010
)

// ColorControl command IDs per Matter §3.2.7.
const (
	// ColorCtrlCmdMoveToHue duplicates a generated constant.
	//
	// Deprecated: use CmdMoveToHue of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in v0.25.0.
	ColorCtrlCmdMoveToHue uint32 = 0x00
	// ColorCtrlCmdMoveHue duplicates a generated constant.
	//
	// Deprecated: use CmdMoveHue of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in v0.25.0.
	ColorCtrlCmdMoveHue uint32 = 0x01
	// ColorCtrlCmdStepHue duplicates a generated constant.
	//
	// Deprecated: use CmdStepHue of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in v0.25.0.
	ColorCtrlCmdStepHue uint32 = 0x02
	// ColorCtrlCmdMoveToSaturation duplicates a generated constant.
	//
	// Deprecated: use CmdMoveToSaturation of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in v0.25.0.
	ColorCtrlCmdMoveToSaturation uint32 = 0x03
	// ColorCtrlCmdMoveSaturation duplicates a generated constant.
	//
	// Deprecated: use CmdMoveSaturation of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in v0.25.0.
	ColorCtrlCmdMoveSaturation uint32 = 0x04
	// ColorCtrlCmdStepSaturation duplicates a generated constant.
	//
	// Deprecated: use CmdStepSaturation of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in v0.25.0.
	ColorCtrlCmdStepSaturation uint32 = 0x05
	// ColorCtrlCmdMoveToHueAndSaturation duplicates a generated constant.
	//
	// Deprecated: use CmdMoveToHueAndSaturation of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in v0.25.0.
	ColorCtrlCmdMoveToHueAndSaturation uint32 = 0x06
	// ColorCtrlCmdMoveToColor duplicates a generated constant.
	//
	// Deprecated: use CmdMoveToColor of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in v0.25.0.
	ColorCtrlCmdMoveToColor uint32 = 0x07
	// ColorCtrlCmdMoveColor duplicates a generated constant.
	//
	// Deprecated: use CmdMoveColor of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in v0.25.0.
	ColorCtrlCmdMoveColor uint32 = 0x08
	// ColorCtrlCmdStepColor duplicates a generated constant.
	//
	// Deprecated: use CmdStepColor of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in v0.25.0.
	ColorCtrlCmdStepColor uint32 = 0x09
	// ColorCtrlCmdMoveToColorTemperature duplicates a generated constant.
	//
	// Deprecated: use CmdMoveToColorTemperature of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in v0.25.0.
	ColorCtrlCmdMoveToColorTemperature uint32 = 0x0A
	// ColorCtrlCmdStopMoveStep duplicates a generated constant.
	//
	// Deprecated: use CmdStopMoveStep of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in v0.25.0.
	ColorCtrlCmdStopMoveStep uint32 = 0x47
	// ColorCtrlCmdMoveColorTemperature duplicates a generated constant.
	//
	// Deprecated: use CmdMoveColorTemperature of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in v0.25.0.
	ColorCtrlCmdMoveColorTemperature uint32 = 0x4B
	// ColorCtrlCmdStepColorTemperature duplicates a generated constant.
	//
	// Deprecated: use CmdStepColorTemperature of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in v0.25.0.
	ColorCtrlCmdStepColorTemperature uint32 = 0x4C
)

// HueDirection values per Matter §3.2.7.4.2.
const (
	// ColorHueDirShortest duplicates a generated constant.
	//
	// Deprecated: use DirectionShortest of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in v0.25.0.
	ColorHueDirShortest uint8 = 0
	// ColorHueDirLongest duplicates a generated constant.
	//
	// Deprecated: use DirectionLongest of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in v0.25.0.
	ColorHueDirLongest uint8 = 1
	// ColorHueDirUp duplicates a generated constant.
	//
	// Deprecated: use DirectionUp of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in v0.25.0.
	ColorHueDirUp uint8 = 2
	// ColorHueDirDown duplicates a generated constant.
	//
	// Deprecated: use DirectionDown of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in v0.25.0.
	ColorHueDirDown uint8 = 3
)

// ColorMoveMode values per Matter §3.2.7.5 / §3.2.7.8.
const (
	// ColorMoveModeStop duplicates a generated constant.
	//
	// Deprecated: use MoveModeStop of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in v0.25.0.
	ColorMoveModeStop uint8 = 0
	// ColorMoveModeUp duplicates a generated constant.
	//
	// Deprecated: use MoveModeUp of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in v0.25.0.
	ColorMoveModeUp uint8 = 1
	// ColorMoveModeDown duplicates a generated constant.
	//
	// Deprecated: use MoveModeDown of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in v0.25.0.
	ColorMoveModeDown uint8 = 3
)

// ColorStepMode values per Matter §3.2.7.6 / §3.2.7.9.
const (
	// ColorStepModeUp duplicates a generated constant.
	//
	// Deprecated: use StepModeUp of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in v0.25.0.
	ColorStepModeUp uint8 = 1
	// ColorStepModeDown duplicates a generated constant.
	//
	// Deprecated: use StepModeDown of cluster/spec/colorcontrol, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in v0.25.0.
	ColorStepModeDown uint8 = 3
)
