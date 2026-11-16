// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package wire_test

import (
	"testing"

	colordef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
	fandef "github.com/SukramJ/go-fabric/cluster/spec/fancontrol"
	opdef "github.com/SukramJ/go-fabric/cluster/spec/operationalstate"
	rvcdef "github.com/SukramJ/go-fabric/cluster/spec/rvcoperationalstate"
	swdef "github.com/SukramJ/go-fabric/cluster/spec/switchcluster"
	thdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	wcdef "github.com/SukramJ/go-fabric/cluster/spec/windowcovering"
	"github.com/SukramJ/go-fabric/cluster/wire"
)

// TestWireIDsEqualTheGeneratedOnes (review II, R2-P6-par-1262,
// R2-P6-api-1261, decision 140): every hand-written id of cluster/wire that
// the generated cluster/spec/<name> also names — kept for a consumer or
// deprecated — carries the generated value. The five command ids absent
// from the snapshot have no counterpart and are deprecated instead.
func TestWireIDsEqualTheGeneratedOnes(t *testing.T) { //nolint:staticcheck,funlen // the deprecated ids are compared for their window
	t.Parallel()
	for name, c := range map[string][2]uint64{
		"ColorControlClusterID":                              {uint64(wire.ColorControlClusterID), uint64(colordef.ClusterID)},
		"ColorCtrlAttrCurrentHue":                            {uint64(wire.ColorCtrlAttrCurrentHue), uint64(colordef.AttrCurrentHue)},
		"ColorCtrlAttrCurrentSaturation":                     {uint64(wire.ColorCtrlAttrCurrentSaturation), uint64(colordef.AttrCurrentSaturation)},
		"ColorCtrlAttrCurrentX":                              {uint64(wire.ColorCtrlAttrCurrentX), uint64(colordef.AttrCurrentX)},
		"ColorCtrlAttrCurrentY":                              {uint64(wire.ColorCtrlAttrCurrentY), uint64(colordef.AttrCurrentY)},
		"ColorCtrlAttrRemainingTime":                         {uint64(wire.ColorCtrlAttrRemainingTime), uint64(colordef.AttrRemainingTime)},
		"ColorCtrlAttrColorTemperatureMireds":                {uint64(wire.ColorCtrlAttrColorTemperatureMireds), uint64(colordef.AttrColorTemperatureMireds)},
		"ColorCtrlAttrColorMode":                             {uint64(wire.ColorCtrlAttrColorMode), uint64(colordef.AttrColorMode)},
		"ColorCtrlAttrOptions":                               {uint64(wire.ColorCtrlAttrOptions), uint64(colordef.AttrOptions)},
		"ColorCtrlAttrNumberOfPrimaries":                     {uint64(wire.ColorCtrlAttrNumberOfPrimaries), uint64(colordef.AttrNumberOfPrimaries)},
		"ColorCtrlAttrEnhancedColorMode":                     {uint64(wire.ColorCtrlAttrEnhancedColorMode), uint64(colordef.AttrEnhancedColorMode)},
		"ColorCtrlAttrColorCapabilities":                     {uint64(wire.ColorCtrlAttrColorCapabilities), uint64(colordef.AttrColorCapabilities)},
		"ColorCtrlAttrColorTempPhysicalMin":                  {uint64(wire.ColorCtrlAttrColorTempPhysicalMin), uint64(colordef.AttrColorTempPhysicalMinMireds)},
		"ColorCtrlAttrColorTempPhysicalMax":                  {uint64(wire.ColorCtrlAttrColorTempPhysicalMax), uint64(colordef.AttrColorTempPhysicalMaxMireds)},
		"ColorCtrlAttrCoupleColorTempToLevelMinMireds":       {uint64(wire.ColorCtrlAttrCoupleColorTempToLevelMinMireds), uint64(colordef.AttrCoupleColorTempToLevelMinMireds)},
		"ColorCtrlAttrStartUpColorTemperatureMireds":         {uint64(wire.ColorCtrlAttrStartUpColorTemperatureMireds), uint64(colordef.AttrStartUpColorTemperatureMireds)},
		"ColorCtrlCmdMoveToHue":                              {uint64(wire.ColorCtrlCmdMoveToHue), uint64(colordef.CmdMoveToHue)},
		"ColorCtrlCmdMoveHue":                                {uint64(wire.ColorCtrlCmdMoveHue), uint64(colordef.CmdMoveHue)},
		"ColorCtrlCmdStepHue":                                {uint64(wire.ColorCtrlCmdStepHue), uint64(colordef.CmdStepHue)},
		"ColorCtrlCmdMoveToSaturation":                       {uint64(wire.ColorCtrlCmdMoveToSaturation), uint64(colordef.CmdMoveToSaturation)},
		"ColorCtrlCmdMoveSaturation":                         {uint64(wire.ColorCtrlCmdMoveSaturation), uint64(colordef.CmdMoveSaturation)},
		"ColorCtrlCmdStepSaturation":                         {uint64(wire.ColorCtrlCmdStepSaturation), uint64(colordef.CmdStepSaturation)},
		"ColorCtrlCmdMoveToHueAndSaturation":                 {uint64(wire.ColorCtrlCmdMoveToHueAndSaturation), uint64(colordef.CmdMoveToHueAndSaturation)},
		"ColorCtrlCmdMoveToColor":                            {uint64(wire.ColorCtrlCmdMoveToColor), uint64(colordef.CmdMoveToColor)},
		"ColorCtrlCmdMoveColor":                              {uint64(wire.ColorCtrlCmdMoveColor), uint64(colordef.CmdMoveColor)},
		"ColorCtrlCmdStepColor":                              {uint64(wire.ColorCtrlCmdStepColor), uint64(colordef.CmdStepColor)},
		"ColorCtrlCmdMoveToColorTemperature":                 {uint64(wire.ColorCtrlCmdMoveToColorTemperature), uint64(colordef.CmdMoveToColorTemperature)},
		"ColorCtrlCmdStopMoveStep":                           {uint64(wire.ColorCtrlCmdStopMoveStep), uint64(colordef.CmdStopMoveStep)},
		"ColorCtrlCmdMoveColorTemperature":                   {uint64(wire.ColorCtrlCmdMoveColorTemperature), uint64(colordef.CmdMoveColorTemperature)},
		"ColorCtrlCmdStepColorTemperature":                   {uint64(wire.ColorCtrlCmdStepColorTemperature), uint64(colordef.CmdStepColorTemperature)},
		"ColorHueDirShortest":                                {uint64(wire.ColorHueDirShortest), uint64(colordef.DirectionShortest)},
		"ColorHueDirLongest":                                 {uint64(wire.ColorHueDirLongest), uint64(colordef.DirectionLongest)},
		"ColorHueDirUp":                                      {uint64(wire.ColorHueDirUp), uint64(colordef.DirectionUp)},
		"ColorHueDirDown":                                    {uint64(wire.ColorHueDirDown), uint64(colordef.DirectionDown)},
		"ColorMoveModeStop":                                  {uint64(wire.ColorMoveModeStop), uint64(colordef.MoveModeStop)},
		"ColorMoveModeUp":                                    {uint64(wire.ColorMoveModeUp), uint64(colordef.MoveModeUp)},
		"ColorMoveModeDown":                                  {uint64(wire.ColorMoveModeDown), uint64(colordef.MoveModeDown)},
		"ColorStepModeUp":                                    {uint64(wire.ColorStepModeUp), uint64(colordef.StepModeUp)},
		"ColorStepModeDown":                                  {uint64(wire.ColorStepModeDown), uint64(colordef.StepModeDown)},
		"FanControlClusterID":                                {uint64(wire.FanControlClusterID), uint64(fandef.ClusterID)},
		"FanControlCmdStep":                                  {uint64(wire.FanControlCmdStep), uint64(fandef.CmdStep)},
		"MatterEventSwitchLatched":                           {uint64(wire.MatterEventSwitchLatched), uint64(swdef.EventSwitchLatched)},
		"MatterEventInitialPress":                            {uint64(wire.MatterEventInitialPress), uint64(swdef.EventInitialPress)},
		"MatterEventLongPress":                               {uint64(wire.MatterEventLongPress), uint64(swdef.EventLongPress)},
		"MatterEventShortRelease":                            {uint64(wire.MatterEventShortRelease), uint64(swdef.EventShortRelease)},
		"MatterEventLongRelease":                             {uint64(wire.MatterEventLongRelease), uint64(swdef.EventLongRelease)},
		"MatterEventMultiPressOngoing":                       {uint64(wire.MatterEventMultiPressOngoing), uint64(swdef.EventMultiPressOngoing)},
		"MatterEventMultiPressComplete":                      {uint64(wire.MatterEventMultiPressComplete), uint64(swdef.EventMultiPressComplete)},
		"OperationalStateClusterID":                          {uint64(wire.OperationalStateClusterID), uint64(opdef.ClusterID)},
		"RvcOperationalStateClusterID":                       {uint64(wire.RvcOperationalStateClusterID), uint64(rvcdef.ClusterID)},
		"OperationalStateCmdPause":                           {uint64(wire.OperationalStateCmdPause), uint64(opdef.CmdPause)},
		"OperationalStateCmdStop":                            {uint64(wire.OperationalStateCmdStop), uint64(opdef.CmdStop)},
		"OperationalStateCmdStart":                           {uint64(wire.OperationalStateCmdStart), uint64(opdef.CmdStart)},
		"OperationalStateCmdResume":                          {uint64(wire.OperationalStateCmdResume), uint64(opdef.CmdResume)},
		"OperationalStateCmdOperationalCommandResponse":      {uint64(wire.OperationalStateCmdOperationalCommandResponse), uint64(opdef.CmdOperationalCommandResponse)},
		"RvcOperationalStateCmdGoHome":                       {uint64(wire.RvcOperationalStateCmdGoHome), uint64(rvcdef.CmdGoHome)},
		"ThermostatCmdSetpointRaiseLower":                    {uint64(wire.ThermostatCmdSetpointRaiseLower), uint64(thdef.CmdSetpointRaiseLower)},
		"ThermostatSetpointModeHeat":                         {uint64(wire.ThermostatSetpointModeHeat), uint64(thdef.SetpointRaiseLowerModeHeat)},
		"ThermostatSetpointModeCool":                         {uint64(wire.ThermostatSetpointModeCool), uint64(thdef.SetpointRaiseLowerModeCool)},
		"ThermostatSetpointModeBoth":                         {uint64(wire.ThermostatSetpointModeBoth), uint64(thdef.SetpointRaiseLowerModeBoth)},
		"WindowCoveringClusterID":                            {uint64(wire.WindowCoveringClusterID), uint64(wcdef.ClusterID)},
		"WindowCoveringAttrType":                             {uint64(wire.WindowCoveringAttrType), uint64(wcdef.AttrType)},
		"WindowCoveringAttrCurrentPositionLiftPercentage":    {uint64(wire.WindowCoveringAttrCurrentPositionLiftPercentage), uint64(wcdef.AttrCurrentPositionLiftPercentage)},
		"WindowCoveringAttrOperationalStatus":                {uint64(wire.WindowCoveringAttrOperationalStatus), uint64(wcdef.AttrOperationalStatus)},
		"WindowCoveringAttrTargetPositionLiftPercent100ths":  {uint64(wire.WindowCoveringAttrTargetPositionLiftPercent100ths), uint64(wcdef.AttrTargetPositionLiftPercent100ths)},
		"WindowCoveringAttrCurrentPositionLiftPercent100ths": {uint64(wire.WindowCoveringAttrCurrentPositionLiftPercent100ths), uint64(wcdef.AttrCurrentPositionLiftPercent100ths)},
		"WindowCoveringAttrEndProductType":                   {uint64(wire.WindowCoveringAttrEndProductType), uint64(wcdef.AttrEndProductType)},
		"WindowCoveringAttrConfigStatus":                     {uint64(wire.WindowCoveringAttrConfigStatus), uint64(wcdef.AttrConfigStatus)},
		"WindowCoveringAttrMode":                             {uint64(wire.WindowCoveringAttrMode), uint64(wcdef.AttrMode)},
		"WindowCoveringAttrSafetyStatus":                     {uint64(wire.WindowCoveringAttrSafetyStatus), uint64(wcdef.AttrSafetyStatus)},
		"WindowCoveringCmdUpOrOpen":                          {uint64(wire.WindowCoveringCmdUpOrOpen), uint64(wcdef.CmdUpOrOpen)},
		"WindowCoveringCmdDownOrClose":                       {uint64(wire.WindowCoveringCmdDownOrClose), uint64(wcdef.CmdDownOrClose)},
		"WindowCoveringCmdStopMotion":                        {uint64(wire.WindowCoveringCmdStopMotion), uint64(wcdef.CmdStopMotion)},
		"WindowCoveringCmdGoToLiftPercentage":                {uint64(wire.WindowCoveringCmdGoToLiftPercentage), uint64(wcdef.CmdGoToLiftPercentage)},
		"WindowCoveringCmdGoToTiltPercentage":                {uint64(wire.WindowCoveringCmdGoToTiltPercentage), uint64(wcdef.CmdGoToTiltPercentage)},
	} {
		if c[0] != c[1] {
			t.Errorf("wire.%s = 0x%X, the generated id 0x%X", name, c[0], c[1])
		}
	}
}
