// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package energy

import (
	"fmt"

	"github.com/SukramJ/go-fabric/cluster/spec"
	commtr "github.com/SukramJ/go-fabric/cluster/spec/commoditymetering"
	"github.com/SukramJ/go-fabric/im"
)

// The CommodityMetering server embeds the generated server, which gives it
// Handle, Emit and SetBeforeWrite; SetAttributes is its setters'.
var (
	_ spec.AttributeSetter   = (*CommodityMeteringServer)(nil)
	_ spec.CommandRegistrar  = (*CommodityMeteringServer)(nil)
	_ spec.EventEmitting     = (*CommodityMeteringServer)(nil)
	_ spec.BeforeWriteSetter = (*CommodityMeteringServer)(nil)
)

// Set records a change the device made to attrID ([CommodityMeteringServer.SetAttributes]).
func (s *CommodityMeteringServer) Set(attrID uint32, value any) error {
	return s.SetAttributes(map[uint32]any{attrID: value})
}

// SetAttributes implements [spec.AttributeSetter]: the host's own change,
// all or nothing, with the rule of [CommodityMeteringServer.SetMeteredQuantity]
// (no longer than MaximumMeteredQuantities, which it needs) and
// [CommodityMeteringServer.SetTariffUnit] (inside the enum) on top of the
// definition's checks, which already refuse a MaximumMeteredQuantities
// below 1 and more than 128 tariff component ids (constraints "min 1",
// "max 128").
func (s *CommodityMeteringServer) SetAttributes(values map[uint32]any) error {
	ids, checked, err := checkedSet(s.Instance, values)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	maximum, hasMaximum := s.Value(commtr.AttrMaximumMeteredQuantities)
	if v, set := checked[commtr.AttrMaximumMeteredQuantities]; set {
		maximum, hasMaximum = v, true
	}
	for _, id := range ids {
		v := checked[id]
		switch id {
		case commtr.AttrMeteredQuantity:
			if v == nil {
				continue
			}
			quantities, ok := listOf[MeteredQuantity](v)
			if !ok {
				return fmt.Errorf("%w: CommodityMetering MeteredQuantity %T", spec.ErrInvalidValue, v)
			}
			n, isMax := maximum.(uint16)
			if !hasMaximum || !isMax {
				return ErrMeteringNoMaximum
			}
			if len(quantities) > int(n) {
				return ErrMeteringListLength
			}
			out := make([]MeteredQuantity, len(quantities))
			for i, q := range quantities {
				out[i] = MeteredQuantity{TariffComponentIds: append([]uint32{}, q.TariffComponentIds...), Quantity: q.Quantity}
			}
			checked[id] = out
		case commtr.AttrTariffUnit:
			if u, ok := v.(uint8); ok && MeteringTariffUnit(u) > commtr.TariffUnitKVAh {
				return statusError{im.StatusConstraintError, "energy: CommodityMetering: TariffUnit outside the enum"}
			}
		}
	}
	return s.Server.SetAttributes(checked)
}
