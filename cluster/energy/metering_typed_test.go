// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package energy_test

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/energy"
	"github.com/SukramJ/go-fabric/cluster/spec"
	commtr "github.com/SukramJ/go-fabric/cluster/spec/commoditymetering"
)

// TestTypedCommodityMeteringServer: Set keeps the setters' rules — a
// MeteredQuantity needs MaximumMeteredQuantities and fits in it — and the
// cluster has no command and no event.
func TestTypedCommodityMeteringServer(t *testing.T) {
	t.Parallel()
	srv, err := energy.NewCommodityMetering(energy.CommodityMeteringConfig{})
	if err != nil {
		t.Fatal(err)
	}
	s := commtr.Wrap(srv)
	one := []commtr.MeteredQuantityStruct{{TariffComponentIds: []uint32{1}, Quantity: 5}}
	if err := s.Set(commtr.Patch{MeteredQuantity: &one}); !errors.Is(err, energy.ErrMeteringNoMaximum) {
		t.Errorf("without a maximum: %v", err)
	}
	if err := s.Set(commtr.Patch{MaximumMeteredQuantities: typedPtr(uint16(1)), MeteredQuantity: &one}); err != nil {
		t.Fatal(err)
	}
	if st := s.Get(); st.MaximumMeteredQuantities == nil || *st.MaximumMeteredQuantities != 1 || st.MeteredQuantity == nil || len(*st.MeteredQuantity) != 1 {
		t.Errorf("state %+v", st)
	}
	two := []commtr.MeteredQuantityStruct{one[0], one[0]}
	if err := s.Set(commtr.Patch{MeteredQuantity: &two}); !errors.Is(err, energy.ErrMeteringListLength) {
		t.Errorf("two quantities, maximum 1: %v", err)
	}
	if err := srv.Handle(0, nil); !errors.Is(err, spec.ErrNotAccepted) {
		t.Errorf("a command the cluster does not have: %v", err)
	}
	if err := spec.EmitOn(srv, 1, 0, nil); !errors.Is(err, spec.ErrNotEmitted) {
		t.Errorf("an event the cluster does not define: %v", err)
	}
}
