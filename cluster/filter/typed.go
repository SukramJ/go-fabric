// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package filter

import (
	"fmt"
	"maps"
	"reflect"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
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
// [ErrInvalidValue]), stored and reported; no LastChangedTimeWriter is
// called. DegradationDirection and ReplacementProductList are the
// configuration's: a different value is refused ([spec.ErrNotHeld]).
func (s *Server) SetAttributes(values map[uint32]any) error {
	for _, id := range slices.Sorted(maps.Keys(values)) {
		v, err := s.CheckSet(id, values[id], nil)
		if err != nil {
			return err
		}
		switch id {
		case hepa.AttrDegradationDirection, hepa.AttrReplacementProductList:
			if fixed, _ := s.srv.Value(id); !reflect.DeepEqual(v, fixed) {
				return fmt.Errorf("%w: filter: attribute 0x%04X is the configuration's", spec.ErrNotHeld, id)
			}
		case hepa.AttrChangeIndication:
			if n, _ := v.(uint8); !s.EnumSupported(hepa.ChangeIndicationEnumDef, uint64(n)) {
				return fmt.Errorf("%w: ChangeIndication %d", ErrInvalidValue, n)
			}
		}
	}
	return s.srv.SetAttributes(values)
}
