// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// This file, gridconditions.go, commoditymetering.go and
// commoditytariff.go add the energy-market clusters the ElectricalEnergyTariff
// (0x0513) and ElectricalMeter (0x0514) device types carry: CommodityPrice
// (0x0095), ElectricalGridConditions (0x00A0), CommodityMetering (0x0B07)
// and CommodityTariff (0x0700). Each is built on a generated cluster
// definition (ADR 0013; cluster/spec/commodityprice, …) and the generated
// server ([spec.Server]). matter.js's servers add nothing to the generated
// behaviors (packages/node/src/behaviors/commodity-price/
// CommodityPriceServer.ts:14, electrical-grid-conditions/
// ElectricalGridConditionsServer.ts:14, commodity-metering/
// CommodityMeteringServer.ts:14, commodity-tariff/CommodityTariffServer.ts:17);
// the rules here are connectedhomeip's, read at the harness pin
// 6170af8461b10b1766044122ac83332c6d00ab20, each cited where it is applied.

package energy

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	cp "github.com/SukramJ/go-fabric/cluster/spec/commodityprice"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// CommodityPrice (0x0095). One chip source carries its rules, cited below
// by file name and line: CommodityPriceCluster.cpp and
// CommodityPriceCluster.h — src/app/clusters/commodity-price-server/.

// ClusterIDCommodityPrice is the CommodityPrice cluster id.
const ClusterIDCommodityPrice = cp.ClusterID

// The CommodityPrice datatypes, the generated ones.
type (
	// CommodityPriceFeature is a CommodityPrice FeatureMap bit.
	CommodityPriceFeature = cp.Feature
	// CommodityPrice is the CommodityPriceStruct.
	CommodityPrice = cp.CommodityPriceStruct
	// CommodityPriceComponent is the CommodityPriceComponentStruct.
	CommodityPriceComponent = cp.CommodityPriceComponentStruct
	// PriceCurrency is the Globals CurrencyStruct as CommodityPrice
	// carries it.
	PriceCurrency = cp.Currency
	// PriceTariffUnit is the Globals TariffUnitEnum as CommodityPrice
	// carries it.
	PriceTariffUnit = cp.TariffUnitEnum
	// CommodityPriceDetail is the CommodityPriceDetailBitmap.
	CommodityPriceDetail = cp.CommodityPriceDetailBitmap
)

// CommodityPriceFeatureForecasting is the FORE feature.
const CommodityPriceFeatureForecasting = cp.FeatureForecasting

// The spec constraints chip checks (CommodityPriceCluster.h:43-46).
const (
	commodityPriceMaxForecastEntries         = 56
	commodityPriceMaxDescriptionLength       = 32
	commodityPriceMaxComponentsPerPriceEntry = 10
	commodityPriceMaxCurrencyValue           = 999
)

// CommodityPriceConfig carries the construction parameters: chip's
// CommodityPriceCluster::Config (CommodityPriceCluster.h:58-62).
type CommodityPriceConfig struct {
	Features CommodityPriceFeature
	// DetailedPrice accepts GetDetailedPriceRequest; DetailedForecast
	// accepts GetDetailedForecastRequest, with FORE only (the
	// OptionalCommandSet, CommodityPriceCluster.h:52-53, :77-84).
	DetailedPrice    bool
	DetailedForecast bool
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// CommodityPriceServer implements [contract.ClusterServer] for
// CommodityPrice. Reads, the data version and the change notifications are
// the generated server's; the price storage, the detail filter and the
// PriceChange event are chip's.
type CommodityPriceServer struct {
	*spec.Instance
	// SelfHeld is the typed server's command handlers, event path and
	// write veto over srv (typed.go).
	spec.SelfHeld

	srv *spec.Server

	// mu guards the full prices — the attributes are served without
	// their descriptions and components, the detailed requests with them
	// (CommodityPriceCluster.cpp:296, :301, :344, :367).
	mu       sync.Mutex
	endpoint uint16
	current  *CommodityPrice
	forecast []CommodityPrice
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                 = (*CommodityPriceServer)(nil)
	_ contract.ClusterDataVersion            = (*CommodityPriceServer)(nil)
	_ contract.ClusterAttributeLister        = (*CommodityPriceServer)(nil)
	_ contract.ClusterCommandLister          = (*CommodityPriceServer)(nil)
	_ contract.ClusterEventLister            = (*CommodityPriceServer)(nil)
	_ contract.ClusterCommandInvokePrivilege = (*CommodityPriceServer)(nil)
	_ contract.AttributeChangeNotifier       = (*CommodityPriceServer)(nil)
	_ contract.EventReceiver                 = (*CommodityPriceServer)(nil)
)

// NewCommodityPrice builds a CommodityPrice server. TariffUnit starts kWh,
// Currency and CurrentPrice null and PriceForecast empty, as chip's
// cluster starts (CommodityPriceCluster.h:174-181). PriceChange is
// emitted: chip generates it on every CurrentPrice change
// (CommodityPriceCluster.cpp:434).
func NewCommodityPrice(cfg CommodityPriceConfig) (*CommodityPriceServer, error) {
	opts := spec.Options{Features: uint32(cfg.Features), Events: []uint32{cp.EventPriceChange}}
	if cfg.DetailedPrice {
		opts.Commands = append(opts.Commands, cp.CmdGetDetailedPriceRequest)
	}
	if cfg.DetailedForecast && cfg.Features&cp.FeatureForecasting != 0 {
		opts.Commands = append(opts.Commands, cp.CmdGetDetailedForecastRequest)
	}
	initial := map[uint32]any{
		cp.AttrTariffUnit:   cp.TariffUnitKWh,
		cp.AttrCurrency:     nil,
		cp.AttrCurrentPrice: nil,
	}
	if cfg.Features&cp.FeatureForecasting != 0 {
		initial[cp.AttrPriceForecast] = []CommodityPrice{}
	}
	s := &CommodityPriceServer{}
	handlers := map[uint32]spec.CommandHandler{
		cp.CmdGetDetailedPriceRequest:    s.getDetailedPrice,
		cp.CmdGetDetailedForecastRequest: s.getDetailedForecast,
	}
	srv, err := spec.NewServer(cp.Definition, opts, spec.ServerConfig{Handlers: handlers, DataVersion: cfg.DataVersion, Initial: initial})
	if err != nil {
		return nil, fmt.Errorf("energy: %w", err)
	}
	s.Instance, s.srv, s.SelfHeld = srv.Instance, srv, spec.NewSelfHeld(srv)
	return s, nil
}

// filterDetails is FilterDetails (CommodityPriceCluster.cpp:54-70): the
// description and the components only where details asks for them.
func filterDetails(p CommodityPrice, details CommodityPriceDetail) CommodityPrice {
	if details&cp.CommodityPriceDetailDescription == 0 {
		p.Description = nil
	}
	if details&cp.CommodityPriceDetailComponents == 0 {
		p.Components = nil
	}
	return p
}

// validDetails is IsValidDetails (CommodityPriceCluster.cpp:73-83): no bit
// beyond Description and Components.
func validDetails(details CommodityPriceDetail) bool {
	return details&^(cp.CommodityPriceDetailDescription|cp.CommodityPriceDetailComponents) == 0
}

// validatePrice is ValidatePrice (CommodityPriceCluster.cpp:91-112).
func validatePrice(p CommodityPrice) error {
	constraint := func(msg string) error {
		return statusError{im.StatusConstraintError, "energy: CommodityPrice: " + msg}
	}
	if !p.PeriodEnd.Null && p.PeriodStart > p.PeriodEnd.Value { // :93-94
		return constraint("PeriodEnd before PeriodStart")
	}
	if p.Price == nil && p.PriceLevel == nil { // :97
		return constraint("neither Price nor PriceLevel")
	}
	if p.Description != nil && len(*p.Description) > commodityPriceMaxDescriptionLength { // :98
		return constraint("Description too long")
	}
	if p.Components != nil { // :100-109
		if len(*p.Components) > commodityPriceMaxComponentsPerPriceEntry {
			return constraint("too many Components")
		}
		for _, c := range *p.Components {
			if c.Description != nil && len(*c.Description) > commodityPriceMaxDescriptionLength {
				return constraint("component Description too long")
			}
		}
	}
	return nil
}

// --- device-side setters ------------------------------------------------

// SetTariffUnit records the TariffUnit; a value outside the enum is
// CONSTRAINT_ERROR (CommodityPriceCluster.cpp:381-389).
func (s *CommodityPriceServer) SetTariffUnit(unit PriceTariffUnit) error {
	if unit > cp.TariffUnitKVAh {
		return statusError{im.StatusConstraintError, "energy: CommodityPrice: TariffUnit outside the enum"}
	}
	return s.srv.Set(cp.AttrTariffUnit, unit)
}

// SetCurrency records the Currency; nil is null, a currency above 999
// CONSTRAINT_ERROR (CommodityPriceCluster.cpp:391-408).
func (s *CommodityPriceServer) SetCurrency(c *PriceCurrency) error {
	if c == nil {
		return s.srv.Set(cp.AttrCurrency, nil)
	}
	if c.Currency > commodityPriceMaxCurrencyValue {
		return statusError{im.StatusConstraintError, "energy: CommodityPrice: Currency above 999"}
	}
	return s.srv.Set(cp.AttrCurrency, *c)
}

// SetCurrentPrice records the CurrentPrice — nil is null — and emits
// PriceChange carrying it without its description or components
// (CommodityPriceCluster.cpp:410-436, GeneratePriceChangeEvent :457-469).
// A price ValidatePrice refuses leaves the attribute as it was.
func (s *CommodityPriceServer) SetCurrentPrice(p *CommodityPrice) error {
	if p != nil {
		if err := validatePrice(*p); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var stored any
	event := cp.PriceChangeEvent{CurrentPrice: spec.NullOf[CommodityPrice]()}
	if p != nil {
		full := *p
		s.current = &full
		filtered := filterDetails(full, 0)
		stored = filtered
		event.CurrentPrice = spec.ValueOf(filtered)
	} else {
		s.current = nil
	}
	if err := s.srv.Set(cp.AttrCurrentPrice, stored); err != nil {
		return err
	}
	return s.srv.Emit(s.endpoint, cp.EventPriceChange, event)
}

// SetForecast records the PriceForecast: FORE only, at most 56 entries,
// each one ValidatePrice accepts (CommodityPriceCluster.cpp:439-455,
// PriceStorage::Set :116-137). The attribute carries the entries without
// their descriptions and components (:298-304).
func (s *CommodityPriceServer) SetForecast(forecast []CommodityPrice) error {
	if !s.HasFeature("FORE") {
		return errors.New("energy: CommodityPrice: PriceForecast needs the Forecasting feature")
	}
	if len(forecast) > commodityPriceMaxForecastEntries {
		return statusError{im.StatusConstraintError, "energy: CommodityPrice: more than 56 forecast entries"}
	}
	for _, p := range forecast {
		if err := validatePrice(p); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.forecast = append([]CommodityPrice(nil), forecast...)
	filtered := make([]CommodityPrice, len(forecast))
	for i, p := range forecast {
		filtered[i] = filterDetails(p, 0)
	}
	return s.srv.Set(cp.AttrPriceForecast, filtered)
}

// CurrentPrice returns the full current price; nil while null.
func (s *CommodityPriceServer) CurrentPrice() *CommodityPrice {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		return nil
	}
	p := *s.current
	return &p
}

// --- commands -------------------------------------------------------------

func priceDetails(name string, fields any) (CommodityPriceDetail, error) {
	switch r := fields.(type) {
	case cp.GetDetailedPriceRequestRequest:
		return r.Details, nil
	case *cp.GetDetailedPriceRequestRequest:
		if r != nil {
			return r.Details, nil
		}
	case cp.GetDetailedForecastRequestRequest:
		return r.Details, nil
	case *cp.GetDetailedForecastRequestRequest:
		if r != nil {
			return r.Details, nil
		}
	}
	return 0, statusError{im.StatusInvalidCommand, fmt.Sprintf("energy: %s fields %T", name, fields)}
}

// getDetailedPrice is HandleGetDetailedPriceRequest
// (CommodityPriceCluster.cpp:337-349): Details outside the bitmap is
// INVALID_COMMAND; the answer is CurrentPrice with what Details asks for.
func (s *CommodityPriceServer) getDetailedPrice(_ context.Context, fields any) (any, error) {
	details, err := priceDetails("GetDetailedPriceRequest", fields)
	if err != nil {
		return nil, err
	}
	if !validDetails(details) { // :341
		return nil, statusError{im.StatusInvalidCommand, "energy: GetDetailedPriceRequest: invalid Details"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	resp := cp.GetDetailedPriceResponse{CurrentPrice: spec.NullOf[CommodityPrice]()}
	if s.current != nil { // GetCurrentPrice, :272-284
		resp.CurrentPrice = spec.ValueOf(filterDetails(*s.current, details))
	}
	return resp, nil
}

// getDetailedForecast is HandleGetDetailedForecastRequest
// (CommodityPriceCluster.cpp:351-379).
func (s *CommodityPriceServer) getDetailedForecast(_ context.Context, fields any) (any, error) {
	details, err := priceDetails("GetDetailedForecastRequest", fields)
	if err != nil {
		return nil, err
	}
	if !validDetails(details) { // :355
		return nil, statusError{im.StatusInvalidCommand, "energy: GetDetailedForecastRequest: invalid Details"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]CommodityPrice, len(s.forecast))
	for i, p := range s.forecast { // :361-371
		out[i] = filterDetails(p, details)
	}
	return cp.GetDetailedForecastResponse{PriceForecast: out}, nil
}

// --- plumbing -------------------------------------------------------------

// SetEndpoint stamps the endpoint the events are addressed to; the bridge
// calls it at reassembly.
func (s *CommodityPriceServer) SetEndpoint(endpoint uint16) {
	s.mu.Lock()
	s.endpoint = endpoint
	s.mu.Unlock()
}

// SetMatterEventEmitter implements [contract.EventReceiver].
func (s *CommodityPriceServer) SetMatterEventEmitter(emitter contract.EventEmitter) {
	s.srv.SetMatterEventEmitter(emitter)
}

// MatterDataVersion implements [contract.ClusterDataVersion].
func (s *CommodityPriceServer) MatterDataVersion() uint32 { return s.srv.MatterDataVersion() }

// OnMatterAttributesChanged implements [contract.AttributeChangeNotifier].
func (s *CommodityPriceServer) OnMatterAttributesChanged(cb func(attrIDs []uint32)) (unsubscribe func()) {
	return s.srv.OnMatterAttributesChanged(cb)
}

// MatterRead resolves an attribute; a list as its generated list
// (commodityWireValue).
func (s *CommodityPriceServer) MatterRead(attrID uint32) (any, bool) {
	return commodityRead(s.srv.MatterRead(attrID))
}

// MatterWrite answers every write: the cluster has no writable attribute.
func (s *CommodityPriceServer) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	return s.srv.MatterWrite(ctx, attrID, value)
}

// MatterInvoke dispatches the two detailed requests.
func (s *CommodityPriceServer) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	return spec.InvokeHeld(ctx, &s.SelfHeld, cmdID, fields) // a host's handler first ([spec.SelfHeld.Handle])
}
