// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package filter

import (
	"fmt"
	"maps"
	"reflect"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
	carbon "github.com/SukramJ/go-fabric/cluster/spec/activatedcarbonfiltermonitoring"
	hepa "github.com/SukramJ/go-fabric/cluster/spec/hepafiltermonitoring"
)

// The typed server's optional interfaces: Handle, Emit and SetBeforeWrite
// come from the embedded [spec.SelfHeld]; SetAttributes holds the
// configuration's attributes fixed.
var (
	_ spec.AttributeSetter   = (*Server)(nil)
	_ spec.CommandRegistrar  = (*Server)(nil)
	_ spec.EventEmitting     = (*Server)(nil)
	_ spec.BeforeWriteSetter = (*Server)(nil)
)

// SetAttributes implements [spec.AttributeSetter]: the host's own change of
// Condition, ChangeIndication, InPlaceIndicator and LastChangedTime — the
// attributes [Server.SetState] sets — all or nothing, checked against the
// definition (a ChangeIndication its feature selection does not admit is
// [spec.ErrInvalidValue]), stored and reported; no LastChangedTimeWriter is
// called. DegradationDirection and ReplacementProductList are the
// configuration's: a different value is refused ([spec.ErrNotHeld]).
func (s *Server) SetAttributes(values map[uint32]any) error {
	for _, id := range slices.Sorted(maps.Keys(values)) {
		v, err := s.CheckSet(id, values[id], nil)
		if err != nil {
			return err
		}
		switch id {
		case hepa.AttrDegradationDirection:
			if fixed, _ := s.srv.Value(id); !reflect.DeepEqual(v, fixed) {
				return fmt.Errorf("%w: filter: attribute 0x%04X is the configuration's", spec.ErrNotHeld, id)
			}
		case hepa.AttrReplacementProductList:
			if got, ok := productsOf(v); !ok || !slices.Equal(got, s.cfg.ReplacementProducts) {
				return fmt.Errorf("%w: filter: attribute 0x%04X is the configuration's", spec.ErrNotHeld, id)
			}
		}
	}
	return s.srv.SetAttributes(values)
}

// productsOf is a ReplacementProductList as a host gives it — either
// cluster's generated entries — or as the server stores it, converted
// entry by entry to [ReplacementProduct].
func productsOf(v any) ([]ReplacementProduct, bool) {
	switch l := v.(type) {
	case spec.List[ReplacementProduct]:
		return []ReplacementProduct(l), true
	case []ReplacementProduct:
		return l, true
	case []carbon.ReplacementProductStruct:
		out := make([]ReplacementProduct, len(l))
		for i, e := range l {
			out[i] = ReplacementProduct{ProductIdentifierType: hepa.ProductIdentifierTypeEnum(e.ProductIdentifierType), ProductIdentifierValue: e.ProductIdentifierValue}
		}
		return out, true
	default:
		return nil, false
	}
}
