// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

// energyMarketTestEventTrigger answers the CommodityPrice,
// ElectricalGridConditions, CommodityTariff and CommodityMetering test
// event triggers of the energy-market devices (fleet_energy_market.go);
// false for any other. The owner merges it into control.go's dispatch:
//
//	if f.garage.testEventTrigger(trigger) || … || f.energyMarketTestEventTrigger(trigger) {
func (f *fleet) energyMarketTestEventTrigger(trigger uint64) bool {
	return f.energyTariff.testEventTrigger(trigger) || f.electricalMeter.testEventTrigger(trigger)
}
