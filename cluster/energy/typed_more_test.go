// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package energy_test

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/energy"
	"github.com/SukramJ/go-fabric/cluster/spec"
	commtr "github.com/SukramJ/go-fabric/cluster/spec/commoditymetering"
	cp "github.com/SukramJ/go-fabric/cluster/spec/commodityprice"
	egc "github.com/SukramJ/go-fabric/cluster/spec/electricalgridconditions"
	"github.com/SukramJ/go-fabric/im"
)

func statusOf(err error) im.StatusCode {
	var sce im.StatusCodeError
	if errors.As(err, &sce) {
		return sce.MatterStatusCode()
	}
	return im.StatusSuccess
}

// TestTypedCommodityPriceSetRules: the host's Currency, PriceForecast and a
// null CurrentPrice go through their setters; a Currency above 999, a
// forecast entry without a price and an unserved attribute are refused
// before anything is stored.
func TestTypedCommodityPriceSetRules(t *testing.T) {
	t.Parallel()
	srv, err := energy.NewCommodityPrice(energy.CommodityPriceConfig{Features: energy.CommodityPriceFeatureForecasting})
	if err != nil {
		t.Fatal(err)
	}
	s := cp.Wrap(srv)
	if err := srv.SetAttributes(map[uint32]any{0x7F: uint8(1)}); !errors.Is(err, spec.ErrNotServed) {
		t.Errorf("an unserved attribute: %v", err)
	}
	eur := cp.Currency{Currency: 978, DecimalPoints: 2}
	if err := s.Set(cp.Patch{Currency: &eur}); err != nil {
		t.Fatal(err)
	}
	if got := s.Get().Currency; got == nil || got.Currency != 978 {
		t.Errorf("Currency %+v", got)
	}
	if err := srv.SetAttributes(map[uint32]any{cp.AttrCurrency: &cp.Currency{Currency: 840, DecimalPoints: 2}}); err != nil || s.Get().Currency.Currency != 840 {
		t.Errorf("Currency by pointer: %v, %+v", err, s.Get().Currency)
	}
	if err := srv.SetAttributes(map[uint32]any{cp.AttrCurrency: nil}); err != nil || s.Get().Currency != nil {
		t.Errorf("null Currency: %v, %+v", err, s.Get().Currency)
	}
	if err := s.Set(cp.Patch{Currency: &cp.Currency{Currency: 1000}}); err == nil {
		t.Error("a Currency above 999 was set")
	}

	forecast := []cp.CommodityPriceStruct{
		{PeriodStart: 10, PeriodEnd: spec.ValueOf(uint32(20)), Price: typedPtr(int64(5))},
		{PeriodStart: 20, PeriodEnd: spec.NullOf[uint32](), Price: typedPtr(int64(6))},
	}
	if err := s.Set(cp.Patch{PriceForecast: &forecast}); err != nil {
		t.Fatal(err)
	}
	if got := s.Get().PriceForecast; len(got) != 2 {
		t.Errorf("PriceForecast %+v", got)
	}
	bad := []cp.CommodityPriceStruct{{PeriodStart: 1, PeriodEnd: spec.NullOf[uint32]()}}
	if err := s.Set(cp.Patch{PriceForecast: &bad}); err == nil || len(s.Get().PriceForecast) != 2 {
		t.Errorf("a forecast entry without a price: %v, forecast %d", err, len(s.Get().PriceForecast))
	}
	if err := srv.SetAttributes(map[uint32]any{cp.AttrCurrentPrice: nil}); err != nil || srv.CurrentPrice() != nil {
		t.Errorf("null CurrentPrice: %v, %+v", err, srv.CurrentPrice())
	}
}

// TestTypedGridConditionsForecast: the host's ForecastConditions and a null
// CurrentConditions go through their setters; an unserved attribute is
// refused.
func TestTypedGridConditionsForecast(t *testing.T) {
	t.Parallel()
	srv, err := energy.NewElectricalGridConditions(energy.GridConditionsConfig{Features: energy.GridConditionsFeatureForecasting})
	if err != nil {
		t.Fatal(err)
	}
	s := egc.Wrap(srv)
	if err := srv.SetAttributes(map[uint32]any{0x7F: uint8(1)}); !errors.Is(err, spec.ErrNotServed) {
		t.Errorf("an unserved attribute: %v", err)
	}
	f := []egc.ElectricalGridConditionsStruct{{PeriodStart: 1, PeriodEnd: spec.ValueOf(uint32(2)), GridCarbonLevel: egc.ThreeLevelLow}}
	if err := s.Set(egc.Patch{ForecastConditions: &f}); err != nil {
		t.Fatal(err)
	}
	if got := s.Get().ForecastConditions; len(got) != 1 {
		t.Errorf("ForecastConditions %+v", got)
	}
	if err := srv.SetAttributes(map[uint32]any{egc.AttrCurrentConditions: nil}); err != nil || s.Get().CurrentConditions != nil {
		t.Errorf("null CurrentConditions: %v, %+v", err, s.Get().CurrentConditions)
	}
}

// TestTypedCommodityMeteringRules: a maximum below 1 and more than 128
// tariff component ids are refused by the definition, a null
// MeteredQuantity is taken, a TariffUnit outside the enum is refused, and
// Set is SetAttributes for one attribute.
func TestTypedCommodityMeteringRules(t *testing.T) {
	t.Parallel()
	srv, err := energy.NewCommodityMetering(energy.CommodityMeteringConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.SetAttributes(map[uint32]any{0x7F: uint8(1)}); !errors.Is(err, spec.ErrNotServed) {
		t.Errorf("an unserved attribute: %v", err)
	}
	if err := srv.Set(commtr.AttrMaximumMeteredQuantities, uint16(0)); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("a maximum of 0: %v", err)
	}
	if err := srv.Set(commtr.AttrMaximumMeteredQuantities, uint16(2)); err != nil {
		t.Fatal(err)
	}
	if err := srv.Set(commtr.AttrMeteredQuantity, nil); err != nil {
		t.Errorf("a null MeteredQuantity: %v", err)
	}
	long := []commtr.MeteredQuantityStruct{{TariffComponentIds: make([]uint32, 130), Quantity: 1}}
	if err := commtr.Wrap(srv).Set(commtr.Patch{MeteredQuantity: &long}); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("130 tariff component ids: %v", err)
	}
	full := []commtr.MeteredQuantityStruct{{TariffComponentIds: make([]uint32, 128), Quantity: 1}}
	if err := commtr.Wrap(srv).Set(commtr.Patch{MeteredQuantity: &full}); err != nil {
		t.Fatal(err)
	}
	if q := commtr.Wrap(srv).Get().MeteredQuantity; q == nil || len(*q) != 1 || len((*q)[0].TariffComponentIds) != 128 {
		t.Errorf("MeteredQuantity %+v", q)
	}
	if err := srv.Set(commtr.AttrTariffUnit, uint8(7)); statusOf(err) != im.StatusConstraintError && !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("TariffUnit 7: %v", err)
	}
}
