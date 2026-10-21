// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package energy

import (
	"fmt"
	"maps"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
	cp "github.com/SukramJ/go-fabric/cluster/spec/commodityprice"
	egc "github.com/SukramJ/go-fabric/cluster/spec/electricalgridconditions"
	"github.com/SukramJ/go-fabric/im"
)

// The typed server's optional interfaces of the self-held CommodityPrice
// and ElectricalGridConditions servers: Handle, Emit and SetBeforeWrite
// come from the embedded [spec.SelfHeld]; SetAttributes is their setters'.
var (
	_ spec.AttributeSetter   = (*CommodityPriceServer)(nil)
	_ spec.CommandRegistrar  = (*CommodityPriceServer)(nil)
	_ spec.EventEmitting     = (*CommodityPriceServer)(nil)
	_ spec.BeforeWriteSetter = (*CommodityPriceServer)(nil)
	_ spec.AttributeSetter   = (*GridConditionsServer)(nil)
	_ spec.CommandRegistrar  = (*GridConditionsServer)(nil)
	_ spec.EventEmitting     = (*GridConditionsServer)(nil)
	_ spec.BeforeWriteSetter = (*GridConditionsServer)(nil)
)

// checkedSet checks every value against its attribute's type and
// constraint ([spec.Instance.CheckSet]) and returns them in their stored
// form, in id order.
func checkedSet(inst *spec.Instance, values map[uint32]any) (ids []uint32, checked map[uint32]any, err error) {
	ids = slices.Sorted(maps.Keys(values))
	checked = make(map[uint32]any, len(ids))
	for _, id := range ids {
		v, cerr := inst.CheckSet(id, values[id], nil)
		if cerr != nil {
			return nil, nil, cerr
		}
		checked[id] = v
	}
	return ids, checked, nil
}

// structOf is a checked struct value, by value or pointer; nil is null.
func structOf[T any](v any) (*T, bool) {
	switch x := v.(type) {
	case nil:
		return nil, true
	case *T:
		return x, true
	case T:
		return &x, true
	}
	return nil, false
}

// listOf is a checked list value.
func listOf[T spec.Encodable](v any) ([]T, bool) {
	switch x := v.(type) {
	case []T:
		return x, true
	case spec.List[T]:
		return x, true
	case nil:
		return nil, true
	}
	return nil, false
}

// SetAttributes implements [spec.AttributeSetter]: the host's own change,
// all or nothing — every value is checked against the definition and the
// rules of its setter before any is applied — through
// [CommodityPriceServer.SetTariffUnit], [CommodityPriceServer.SetCurrency],
// [CommodityPriceServer.SetCurrentPrice] (which emits PriceChange) and
// [CommodityPriceServer.SetForecast].
func (s *CommodityPriceServer) SetAttributes(values map[uint32]any) error {
	ids, checked, err := checkedSet(s.Instance, values)
	if err != nil {
		return err
	}
	var apply []func() error
	for _, id := range ids {
		v := checked[id]
		switch id {
		case cp.AttrTariffUnit:
			n, _ := v.(uint8)
			if PriceTariffUnit(n) > cp.TariffUnitKVAh {
				return statusError{im.StatusConstraintError, "energy: CommodityPrice: TariffUnit outside the enum"}
			}
			apply = append(apply, func() error { return s.SetTariffUnit(PriceTariffUnit(n)) })
		case cp.AttrCurrency:
			c, ok := structOf[PriceCurrency](v)
			if !ok {
				return fmt.Errorf("%w: CommodityPrice Currency %T", spec.ErrInvalidValue, v)
			}
			if c != nil && c.Currency > commodityPriceMaxCurrencyValue {
				return statusError{im.StatusConstraintError, "energy: CommodityPrice: Currency above 999"}
			}
			apply = append(apply, func() error { return s.SetCurrency(c) })
		case cp.AttrCurrentPrice:
			p, ok := structOf[CommodityPrice](v)
			if !ok {
				return fmt.Errorf("%w: CommodityPrice CurrentPrice %T", spec.ErrInvalidValue, v)
			}
			if p != nil {
				if err := validatePrice(*p); err != nil {
					return err
				}
			}
			apply = append(apply, func() error { return s.SetCurrentPrice(p) })
		case cp.AttrPriceForecast:
			f, ok := listOf[CommodityPrice](v)
			if !ok {
				return fmt.Errorf("%w: CommodityPrice PriceForecast %T", spec.ErrInvalidValue, v)
			}
			if len(f) > commodityPriceMaxForecastEntries {
				return statusError{im.StatusConstraintError, "energy: CommodityPrice: more than 56 forecast entries"}
			}
			for _, p := range f {
				if err := validatePrice(p); err != nil {
					return err
				}
			}
			apply = append(apply, func() error { return s.SetForecast(f) })
		}
	}
	for _, fn := range apply {
		if err := fn(); err != nil {
			return err
		}
	}
	return nil
}

// SetAttributes implements [spec.AttributeSetter]: the host's own change,
// all or nothing, through [GridConditionsServer.SetLocalGenerationAvailable],
// [GridConditionsServer.SetCurrentConditions] (which emits
// CurrentConditionsChanged) and [GridConditionsServer.SetForecastConditions].
func (s *GridConditionsServer) SetAttributes(values map[uint32]any) error {
	ids, checked, err := checkedSet(s.Instance, values)
	if err != nil {
		return err
	}
	var apply []func() error
	for _, id := range ids {
		v := checked[id]
		switch id {
		case egc.AttrLocalGenerationAvailable:
			var available *bool
			if b, ok := v.(bool); ok {
				available = &b
			}
			apply = append(apply, func() error { return s.SetLocalGenerationAvailable(available) })
		case egc.AttrCurrentConditions:
			c, ok := structOf[GridConditions](v)
			if !ok {
				return fmt.Errorf("%w: ElectricalGridConditions CurrentConditions %T", spec.ErrInvalidValue, v)
			}
			if c != nil {
				if err := validGridConditions(*c); err != nil {
					return err
				}
			}
			apply = append(apply, func() error { return s.SetCurrentConditions(c) })
		case egc.AttrForecastConditions:
			f, ok := listOf[GridConditions](v)
			if !ok {
				return fmt.Errorf("%w: ElectricalGridConditions ForecastConditions %T", spec.ErrInvalidValue, v)
			}
			apply = append(apply, func() error { return s.SetForecastConditions(f) })
		}
	}
	for _, fn := range apply {
		if err := fn(); err != nil {
			return err
		}
	}
	return nil
}
