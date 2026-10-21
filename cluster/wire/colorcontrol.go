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
	ColorCtrlCmdMoveToHue              uint32 = 0x00
	ColorCtrlCmdMoveHue                uint32 = 0x01
	ColorCtrlCmdStepHue                uint32 = 0x02
	ColorCtrlCmdMoveToSaturation       uint32 = 0x03
	ColorCtrlCmdMoveSaturation         uint32 = 0x04
	ColorCtrlCmdStepSaturation         uint32 = 0x05
	ColorCtrlCmdMoveToHueAndSaturation uint32 = 0x06
	ColorCtrlCmdMoveToColor            uint32 = 0x07
	ColorCtrlCmdMoveColor              uint32 = 0x08
	ColorCtrlCmdStepColor              uint32 = 0x09
	ColorCtrlCmdMoveToColorTemperature uint32 = 0x0A
	ColorCtrlCmdStopMoveStep           uint32 = 0x47
	ColorCtrlCmdMoveColorTemperature   uint32 = 0x4B
	ColorCtrlCmdStepColorTemperature   uint32 = 0x4C
)

// HueDirection values per Matter §3.2.7.4.2.
const (
	ColorHueDirShortest uint8 = 0
	ColorHueDirLongest  uint8 = 1
	ColorHueDirUp       uint8 = 2
	ColorHueDirDown     uint8 = 3
)

// ColorMoveMode values per Matter §3.2.7.5 / §3.2.7.8.
const (
	ColorMoveModeStop uint8 = 0
	ColorMoveModeUp   uint8 = 1
	ColorMoveModeDown uint8 = 3
)

// ColorStepMode values per Matter §3.2.7.6 / §3.2.7.9.
const (
	ColorStepModeUp   uint8 = 1
	ColorStepModeDown uint8 = 3
)
