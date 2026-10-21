// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package energy_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/energy"
	"github.com/SukramJ/go-fabric/cluster/spec"
	cp "github.com/SukramJ/go-fabric/cluster/spec/commodityprice"
	egc "github.com/SukramJ/go-fabric/cluster/spec/electricalgridconditions"
	"github.com/SukramJ/go-fabric/contract"
)

func typedPtr[T any](v T) *T { return &v }

// TestTypedCommodityPriceServer drives the self-held CommodityPrice server
// through its typed server: Set goes through the setters (PriceChange goes
// out with a new CurrentPrice), a refused price changes nothing, a host
// handler replaces GetDetailedPriceRequest, and the attributes are
// read-only.
func TestTypedCommodityPriceServer(t *testing.T) {
	t.Parallel()
	srv, err := energy.NewCommodityPrice(energy.CommodityPriceConfig{DetailedPrice: true})
	if err != nil {
		t.Fatal(err)
	}
	em := &emitter{}
	srv.SetMatterEventEmitter(em)
	s := cp.Wrap(srv)
	changes := 0
	defer s.OnChanged(func(cp.State, contract.Change) { changes++ })()

	price := cp.CommodityPriceStruct{PeriodStart: 10, PeriodEnd: spec.NullOf[uint32](), Price: typedPtr(int64(25))}
	if err := s.Set(cp.Patch{CurrentPrice: &price, TariffUnit: typedPtr(cp.TariffUnitKVAh)}); err != nil {
		t.Fatal(err)
	}
	if got := srv.CurrentPrice(); got == nil || *got.Price != 25 || s.Get().TariffUnit != cp.TariffUnitKVAh || changes == 0 {
		t.Errorf("CurrentPrice %+v, TariffUnit %v, %d changes", got, s.Get().TariffUnit, changes)
	}
	if evs := em.take(); len(evs) != 1 || evs[0].id != cp.EventPriceChange {
		t.Errorf("events %+v", evs)
	}
	// Neither Price nor PriceLevel: refused, the TariffUnit with it.
	if err := s.Set(cp.Patch{CurrentPrice: &cp.CommodityPriceStruct{PeriodEnd: spec.NullOf[uint32]()}, TariffUnit: typedPtr(cp.TariffUnitKWh)}); err == nil || s.Get().TariffUnit != cp.TariffUnitKVAh {
		t.Errorf("a refused patch: %v, TariffUnit %v", err, s.Get().TariffUnit)
	}

	called := 0
	if err := s.HandleGetDetailedPriceRequest(func(context.Context, cp.GetDetailedPriceRequestRequest) (cp.GetDetailedPriceResponse, error) {
		called++
		return cp.GetDetailedPriceResponse{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.MatterInvoke(context.Background(), cp.CmdGetDetailedPriceRequest, cp.GetDetailedPriceRequestRequest{}); err != nil || called != 1 {
		t.Errorf("host handler: %v, called %d", err, called)
	}
	if err := s.EmitPriceChange(3, cp.PriceChangeEvent{CurrentPrice: spec.NullOf[cp.CommodityPriceStruct]()}); err != nil {
		t.Fatal(err)
	}
	if evs := em.take(); len(evs) != 1 || evs[0].endpoint != 3 {
		t.Errorf("events %+v", evs)
	}
	if err := s.OnWrite(func(context.Context, cp.State, uint32) error { return nil }); err != nil {
		t.Fatal(err)
	}
}

// TestTypedGridConditionsServer: Set of CurrentConditions emits
// CurrentConditionsChanged; a carbon level outside its enum is refused.
func TestTypedGridConditionsServer(t *testing.T) {
	t.Parallel()
	srv, err := energy.NewElectricalGridConditions(energy.GridConditionsConfig{})
	if err != nil {
		t.Fatal(err)
	}
	em := &emitter{}
	srv.SetMatterEventEmitter(em)
	s := egc.Wrap(srv)
	c := egc.ElectricalGridConditionsStruct{PeriodStart: 1, PeriodEnd: spec.NullOf[uint32](), GridCarbonLevel: egc.ThreeLevelHigh}
	if err := s.Set(egc.Patch{CurrentConditions: &c, LocalGenerationAvailable: typedPtr(true)}); err != nil {
		t.Fatal(err)
	}
	if st := s.Get(); st.LocalGenerationAvailable == nil || !*st.LocalGenerationAvailable || st.CurrentConditions == nil {
		t.Errorf("state %+v", st)
	}
	if evs := em.take(); len(evs) != 1 || evs[0].id != egc.EventCurrentConditionsChanged {
		t.Errorf("events %+v", evs)
	}
	bad := c
	bad.LocalCarbonLevel = 7
	if err := s.Set(egc.Patch{CurrentConditions: &bad}); err == nil {
		t.Error("a carbon level outside the enum was set")
	}
	if err := srv.Handle(0, nil); !errors.Is(err, spec.ErrNotAccepted) {
		t.Errorf("a command the cluster does not have: %v", err)
	}
}
