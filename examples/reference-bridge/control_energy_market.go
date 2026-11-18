// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

// energyMarketTestEventTrigger answers the CommodityPrice,
// ElectricalGridConditions, CommodityTariff and CommodityMetering test
// event triggers of the energy-market devices (fleet_energy_market.go);
// false for any other. control.go's testEventTrigger dispatch asks it
// after the other devices' handlers.
func (f *fleet) energyMarketTestEventTrigger(trigger uint64) bool {
	return f.energyTariff.testEventTrigger(trigger) || f.electricalMeter.testEventTrigger(trigger)
}
