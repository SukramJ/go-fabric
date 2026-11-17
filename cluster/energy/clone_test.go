// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package energy_test

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/energy"
	"github.com/SukramJ/go-fabric/cluster/spec"
	evse "github.com/SukramJ/go-fabric/cluster/spec/energyevse"
)

// TestEnergyStateIsHandedOutAsDeepCopies is the package's guard of
// decision 142: an edit through a pointer or a list of what a getter
// returned changes nothing the server serves. Before, Forecast() shared
// the stored forecast's slots and CurrentPrice() its lists (review II,
// R2-P6-err-1460's class).
func TestEnergyStateIsHandedOutAsDeepCopies(t *testing.T) {
	t.Parallel()
	d, _ := newDEM(t, demPFRSet, &demHost{})
	if err := d.SetForecast(triggerForecast(demEpoch(time.Now()))); err != nil {
		t.Fatal(err)
	}
	*d.Forecast().Slots[0].NominalPower = 1
	if got := *d.Forecast().Slots[0].NominalPower; got == 1 {
		t.Error("the DEM forecast changed through Forecast()")
	}
	handed := d.Forecast()
	if err := d.SetForecast(handed); err != nil {
		t.Fatal(err)
	}
	handed.Slots[0].SlotIsPausable = nil // after the store took it
	if d.Forecast().Slots[0].SlotIsPausable == nil {
		t.Error("the DEM forecast changed through the forecast SetForecast took")
	}

	e, _, _ := newEvse(t, energy.EvseFeatureChargingPreferences|energy.EvseFeatureSoCReporting)
	fifty := uint8(50)
	if err := demInvoke(e, evse.CmdSetTargets, evse.SetTargetsRequest{ChargingTargetSchedules: []energy.ChargingTargetSchedule{{DayOfWeekForSequence: 1, ChargingTargets: []energy.ChargingTarget{target(60, &fifty, nil)}}}}); err != nil {
		t.Fatal(err)
	}
	*e.Targets()[0].ChargingTargets[0].TargetSoC = 99
	if got := *e.Targets()[0].ChargingTargets[0].TargetSoC; got != 50 {
		t.Errorf("TargetSoC %d after an edit of Targets(), want 50", got)
	}

	p, err := energy.NewCommodityPrice(energy.CommodityPriceConfig{})
	if err != nil {
		t.Fatal(err)
	}
	price, desc := int64(10), "base"
	if err := p.SetCurrentPrice(&energy.CommodityPrice{PeriodStart: 1, PeriodEnd: spec.NullOf[uint32](), Price: &price, Description: &desc}); err != nil {
		t.Fatal(err)
	}
	desc = "handed in"
	*p.CurrentPrice().Description = "handed out"
	if got := *p.CurrentPrice().Description; got != "base" {
		t.Errorf("CurrentPrice Description %q, want base", got)
	}
}
