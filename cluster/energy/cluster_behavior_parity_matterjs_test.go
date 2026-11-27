// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package energy_test

import (
	"testing"

	"thingfabric.dev/go-fabric/cluster/energy"
	cp "thingfabric.dev/go-fabric/cluster/spec/commodityprice"
	dem "thingfabric.dev/go-fabric/cluster/spec/deviceenergymanagement"
)

// TestParityMatterJS_ClusterBehaviorNullableStructDefault ports
// matter.js packages/node/test/behavior/cluster/ClusterBehaviorTest.ts:323
// at db91886c, "assigns null to a nullable struct attribute without a
// stated default" (#5058): a nullable struct attribute without a stated
// default starts null, not as a struct of its members' defaults
// (packages/model/src/logic/SelectDefaultValue.ts:145-152) —
// CommodityPrice's CurrentPrice, and DeviceEnergyManagement's Forecast
// with PowerForecastReporting.
func TestParityMatterJS_ClusterBehaviorNullableStructDefault(t *testing.T) {
	t.Parallel()
	price, err := energy.NewCommodityPrice(energy.CommodityPriceConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if v := read(t, price, cp.AttrCurrentPrice); v != nil {
		t.Errorf("CommodityPrice CurrentPrice starts %v, want null", v)
	}
	if got := price.CurrentPrice(); got != nil {
		t.Errorf("CurrentPrice() = %+v, want nil", got)
	}
	management, err := energy.NewDeviceEnergyManagement(energy.DeviceEnergyConfig{
		Features: energy.DemFeaturePowerForecastReporting, Manager: energy.NopDemManager{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if v := read(t, management, dem.AttrForecast); v != nil {
		t.Errorf("DeviceEnergyManagement Forecast starts %v, want null", v)
	}
	if got := management.Forecast(); got != nil {
		t.Errorf("Forecast() = %+v, want nil", got)
	}
}
