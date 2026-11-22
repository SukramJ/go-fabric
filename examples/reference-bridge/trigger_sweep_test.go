// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/schema"
	"thingfabric.dev/go-fabric/surface"
)

// sweptTriggers is every TestEventTrigger constant of the daemon;
// TestSweptTriggersAreEveryTriggerConstant keeps the list whole.
var sweptTriggers = []struct {
	name string
	v    uint64
}{
	{"triggerBoolCfgSensorTrigger", triggerBoolCfgSensorTrigger},
	{"triggerBoolCfgSensorUntrigger", triggerBoolCfgSensorUntrigger},
	{"triggerClearBatteryLevelLow", triggerClearBatteryLevelLow},
	{"triggerClearCO", triggerClearCO},
	{"triggerClearCOInterconnect", triggerClearCOInterconnect},
	{"triggerClearContamination", triggerClearContamination},
	{"triggerClearEndOfLife", triggerClearEndOfLife},
	{"triggerClearMalfunction", triggerClearMalfunction},
	{"triggerClearSensitivity", triggerClearSensitivity},
	{"triggerClearSilence", triggerClearSilence},
	{"triggerClearSmoke", triggerClearSmoke},
	{"triggerClearSmokeInterconnect", triggerClearSmokeInterconnect},
	{"triggerClearUnmounted", triggerClearUnmounted},
	{"triggerClosureClear", triggerClosureClear},
	{"triggerClosureError", triggerClosureError},
	{"triggerClosureSetupRequired", triggerClosureSetupRequired},
	{"triggerDemConstraintBasedAdjustment", triggerDemConstraintBasedAdjustment},
	{"triggerDemConstraintBasedAdjustmentClear", triggerDemConstraintBasedAdjustmentClear},
	{"triggerDemForecast", triggerDemForecast},
	{"triggerDemForecastAdjustment", triggerDemForecastAdjustment},
	{"triggerDemForecastAdjustmentClear", triggerDemForecastAdjustmentClear},
	{"triggerDemForecastAdjustmentNextSlot", triggerDemForecastAdjustmentNextSlot},
	{"triggerDemForecastClear", triggerDemForecastClear},
	{"triggerDemPausable", triggerDemPausable},
	{"triggerDemPausableClear", triggerDemPausableClear},
	{"triggerDemPausableNextSlot", triggerDemPausableNextSlot},
	{"triggerDemPowerAdjustment", triggerDemPowerAdjustment},
	{"triggerDemPowerAdjustmentClear", triggerDemPowerAdjustmentClear},
	{"triggerDemPowerRangeAdjustment", triggerDemPowerRangeAdjustment},
	{"triggerDemPowerRangeAdjustmentClear", triggerDemPowerRangeAdjustmentClear},
	{"triggerDemStartTimeAdjustment", triggerDemStartTimeAdjustment},
	{"triggerDemStartTimeAdjustmentClear", triggerDemStartTimeAdjustmentClear},
	{"triggerDemUserOptOutClearAll", triggerDemUserOptOutClearAll},
	{"triggerDemUserOptOutGridOptimization", triggerDemUserOptOutGridOptimization},
	{"triggerDemUserOptOutLocalOptimization", triggerDemUserOptOutLocalOptimization},
	{"triggerDrawOffHotWater", triggerDrawOffHotWater},
	{"triggerEvseBasicFunctionality", triggerEvseBasicFunctionality},
	{"triggerEvseBasicFunctionalityClear", triggerEvseBasicFunctionalityClear},
	{"triggerEvseChargeDemand", triggerEvseChargeDemand},
	{"triggerEvseChargeDemandClear", triggerEvseChargeDemandClear},
	{"triggerEvseDiagnosticsComplete", triggerEvseDiagnosticsComplete},
	{"triggerEvseFaultClear", triggerEvseFaultClear},
	{"triggerEvseGroundFault", triggerEvseGroundFault},
	{"triggerEvseOverTemperatureFault", triggerEvseOverTemperatureFault},
	{"triggerEvsePluggedIn", triggerEvsePluggedIn},
	{"triggerEvsePluggedInClear", triggerEvsePluggedInClear},
	{"triggerEvseSetSoCClear", triggerEvseSetSoCClear},
	{"triggerEvseSetSoCHigh", triggerEvseSetSoCHigh},
	{"triggerEvseSetSoCLow", triggerEvseSetSoCLow},
	{"triggerEvseSetVehicleID", triggerEvseSetVehicleID},
	{"triggerEvseTimeOfUseMode", triggerEvseTimeOfUseMode},
	{"triggerEvseTimeOfUseModeClear", triggerEvseTimeOfUseModeClear},
	{"triggerEvseTriggerRFID", triggerEvseTriggerRFID},
	{"triggerFakeReadingsGenStart", triggerFakeReadingsGenStart},
	{"triggerFakeReadingsLoadStart", triggerFakeReadingsLoadStart},
	{"triggerFakeReadingsStop", triggerFakeReadingsStop},
	{"triggerForceCOCritical", triggerForceCOCritical},
	{"triggerForceCOInterconnect", triggerForceCOInterconnect},
	{"triggerForceCOWarning", triggerForceCOWarning},
	{"triggerForceContaminationHigh", triggerForceContaminationHigh},
	{"triggerForceContaminationLow", triggerForceContaminationLow},
	{"triggerForceEndOfLife", triggerForceEndOfLife},
	{"triggerForceLowBatteryCrit", triggerForceLowBatteryCrit},
	{"triggerForceLowBatteryWarning", triggerForceLowBatteryWarning},
	{"triggerForceMalfunction", triggerForceMalfunction},
	{"triggerForceSensitivityHigh", triggerForceSensitivityHigh},
	{"triggerForceSensitivityLow", triggerForceSensitivityLow},
	{"triggerForceSilence", triggerForceSilence},
	{"triggerForceSmokeCritical", triggerForceSmokeCritical},
	{"triggerForceSmokeInterconnect", triggerForceSmokeInterconnect},
	{"triggerForceSmokeWarning", triggerForceSmokeWarning},
	{"triggerForceUnmounted", triggerForceUnmounted},
	{"triggerGeneric", triggerGeneric},
	{"triggerGenericAlias", triggerGenericAlias},
	{"triggerGridCurrentConditions", triggerGridCurrentConditions},
	{"triggerGridForecastConditions", triggerGridForecastConditions},
	{"triggerICDAddActiveMode", triggerICDAddActiveMode},
	{"triggerICDDSLSForceSIT", triggerICDDSLSForceSIT},
	{"triggerICDDSLSWithdrawSIT", triggerICDDSLSWithdrawSIT},
	{"triggerICDInvalidateAllCounterVals", triggerICDInvalidateAllCounterVals},
	{"triggerICDInvalidateHalfCounterVals", triggerICDInvalidateHalfCounterVals},
	{"triggerICDRemoveActiveMode", triggerICDRemoveActiveMode},
	{"triggerMeterAttributesValueUpdate", triggerMeterAttributesValueUpdate},
	{"triggerMeterAttributesValueUpdateClear", triggerMeterAttributesValueUpdateClear},
	{"triggerMeteringAttributesClear", triggerMeteringAttributesClear},
	{"triggerMeteringAttributesUpdate", triggerMeteringAttributesUpdate},
	{"triggerPriceForecastUpdate", triggerPriceForecastUpdate},
	{"triggerPriceUpdate", triggerPriceUpdate},
	{"triggerTariffDataClear", triggerTariffDataClear},
	{"triggerTariffDataUpdated", triggerTariffDataUpdated},
	{"triggerTariffTimeShift24h", triggerTariffTimeShift24h},
	{"triggerTariffTimeShift4h", triggerTariffTimeShift4h},
	{"triggerTariffTimeShiftDisable", triggerTariffTimeShiftDisable},
	{"triggerWaterHeaterInstallation", triggerWaterHeaterInstallation},
	{"triggerWaterHeaterInstallationClear", triggerWaterHeaterInstallationClear},
	{"triggerWaterHeaterManual", triggerWaterHeaterManual},
	{"triggerWaterHeaterOff", triggerWaterHeaterOff},
	{"triggerWaterTemperature20C", triggerWaterTemperature20C},
	{"triggerWaterTemperature61C", triggerWaterTemperature61C},
	{"triggerWaterTemperature66C", triggerWaterTemperature66C},
	{"triggerWiFiAssociationFailure", triggerWiFiAssociationFailure},
	{"triggerWiFiConnectionStatus", triggerWiFiConnectionStatus},
	{"triggerWiFiDisconnection", triggerWiFiDisconnection},
}

// TestEveryTriggerAtEveryLevelAnswersWithoutPanic holds R2-P6-err-2650:
// every TestEventTrigger the daemon knows, sent to a fleet composed at
// every specification level, returns — a device the level withheld is not
// built, and its handler answers not handled instead of reaching a nil
// server.
func TestEveryTriggerAtEveryLevelAnswersWithoutPanic(t *testing.T) {
	t.Parallel()
	for _, level := range append([]schema.Level{""}, schema.Levels()...) {
		for _, tr := range sweptTriggers {
			f := newFleet()
			f.surface = surface.Policies{Level: level}
			if _, err := f.endpoints(); err != nil {
				t.Fatalf("level %q: endpoints: %v", level, err)
			}
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("level %q %s (%#x): panic %v", level, tr.name, tr.v, r)
					}
				}()
				_ = f.testEventTrigger(context.Background(), tr.v)
			}()
		}
	}
}

// TestSweptTriggersAreEveryTriggerConstant: sweptTriggers names every
// constant whose name starts with "trigger" in the package's non-test
// files, so a new trigger joins the sweep.
func TestSweptTriggersAreEveryTriggerConstant(t *testing.T) {
	t.Parallel()
	listed := map[string]bool{}
	for _, tr := range sweptTriggers {
		listed[tr.name] = true
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var missing []string
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				for _, id := range spec.(*ast.ValueSpec).Names {
					if strings.HasPrefix(id.Name, "trigger") && !listed[id.Name] {
						missing = append(missing, fmt.Sprintf("%s (%s)", id.Name, name))
					}
				}
			}
		}
	}
	if len(missing) > 0 {
		t.Errorf("trigger constants not in sweptTriggers: %s", strings.Join(missing, ", "))
	}
}

// TestStateForecastEsaTakesNoPowerAdjustmentCapability holds
// R2-P6-err-2655: the ESA without PowerAdjustment runs the shared DEM
// trigger handler, and its PowerAdjustment triggers write no capability
// it does not serve — nothing is refused, nothing is logged. Not
// parallel: it replaces the default logger.
func TestStateForecastEsaTakesNoPowerAdjustmentCapability(t *testing.T) {
	var logs bytes.Buffer
	saved := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(saved) })
	e := newDemoEsa("Charger ESA")
	if _, err := e.device(bridged("esa", e.name)); err != nil {
		t.Fatal(err)
	}
	for _, tr := range []uint64{triggerDemPowerAdjustment, triggerDemPowerAdjustmentClear, triggerDemPausableClear, triggerDemForecastAdjustmentClear, triggerDemConstraintBasedAdjustmentClear} {
		e.testEventTrigger(tr)
	}
	if strings.Contains(logs.String(), "evse.dem.capability") {
		t.Fatalf("the ESA was handed a capability it does not serve:\n%s", logs.String())
	}
}
