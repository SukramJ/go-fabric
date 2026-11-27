// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
	unitdef "thingfabric.dev/go-fabric/cluster/spec/unitlocalization"
)

// TestUnitLocalizationRefusesADuplicateUnit (review II, R2-P6-err-1268):
// SupportedTemperatureUnits holds each unit once — the model's text "Each
// entry in the list shall be unique" (matter.js
// unit-localization.resource.ts:43-45 at db91886c) — so a configuration
// with a duplicate is refused at construction.
func TestUnitLocalizationRefusesADuplicateUnit(t *testing.T) {
	t.Parallel()
	_, err := core.NewUnitLocalization(core.UnitLocalizationConfig{
		SupportedTemperatureUnits: []unitdef.TempUnitEnum{unitdef.TempUnitCelsius, unitdef.TempUnitCelsius},
	})
	if !errors.Is(err, core.ErrLocalizationConfig) {
		t.Fatalf("err %v, want ErrLocalizationConfig", err)
	}
}
