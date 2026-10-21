// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package filter_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/filter"
	"github.com/SukramJ/go-fabric/cluster/spec"
	carbon "github.com/SukramJ/go-fabric/cluster/spec/activatedcarbonfiltermonitoring"
	hepa "github.com/SukramJ/go-fabric/cluster/spec/hepafiltermonitoring"
)

// TestTypedSetRefusalsAndFixedValues: an attribute the server does not
// serve is refused, and the configured DegradationDirection is accepted as
// its own value.
func TestTypedSetRefusalsAndFixedValues(t *testing.T) {
	t.Parallel()
	srv := full(t, &host{})
	if err := srv.SetAttributes(map[uint32]any{0x7F: uint8(1)}); !errors.Is(err, spec.ErrNotServed) {
		t.Errorf("an unserved attribute: %v", err)
	}
	s := hepa.Wrap(srv)
	own := s.Get().DegradationDirection
	if err := s.Set(hepa.Patch{DegradationDirection: &own}); err != nil {
		t.Errorf("the configured DegradationDirection itself: %v", err)
	}
	list := s.Get().ReplacementProductList
	if len(list) == 0 {
		t.Fatal("no ReplacementProductList configured")
	}
	if err := s.Set(hepa.Patch{ReplacementProductList: &list}); err != nil {
		t.Errorf("the configured ReplacementProductList itself: %v", err)
	}
}

// TestTypedChangeIndicationNeedsItsFeature: Warning is a ChangeIndication
// of the WRN feature; without it the host's Set of Warning is refused.
func TestTypedChangeIndicationNeedsItsFeature(t *testing.T) {
	t.Parallel()
	srv, err := filter.NewHepaFilterMonitoring(filter.Config{DegradationDirection: hepa.DegradationDirectionDown})
	if err != nil {
		t.Fatal(err)
	}
	if err := hepa.Wrap(srv).Set(hepa.Patch{ChangeIndication: ptrOf(hepa.ChangeIndicationWarning)}); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("Warning without WRN: %v", err)
	}
}

// TestTypedReplacementProductList: the configured list is the
// configuration's in every form it arrives in — the stored list, either
// cluster's generated entries — and any other list is refused.
func TestTypedReplacementProductList(t *testing.T) {
	t.Parallel()
	srv := full(t, &host{})
	stored, _ := srv.MatterRead(hepa.AttrReplacementProductList)
	if err := srv.SetAttributes(map[uint32]any{hepa.AttrReplacementProductList: stored}); err != nil {
		t.Errorf("the stored list: %v", err)
	}
	if err := srv.SetAttributes(map[uint32]any{hepa.AttrReplacementProductList: slices.Clone(products)}); err != nil {
		t.Errorf("the stored list as a plain slice: %v", err)
	}
	other := []hepa.ReplacementProductStruct{{ProductIdentifierType: hepa.ProductIdentifierTypeUpc, ProductIdentifierValue: "000000000000"}}
	if err := hepa.Wrap(srv).Set(hepa.Patch{ReplacementProductList: &other}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("another list: %v", err)
	}

	carbonSrv, err := filter.NewActivatedCarbonFilterMonitoring(filter.Config{
		Features:             filter.FeatureReplacementProductList,
		DegradationDirection: hepa.DegradationDirectionDown, ReplacementProducts: products,
	})
	if err != nil {
		t.Fatal(err)
	}
	same := make([]carbon.ReplacementProductStruct, len(products))
	for i, p := range products {
		same[i] = carbon.ReplacementProductStruct{ProductIdentifierType: carbon.ProductIdentifierTypeEnum(p.ProductIdentifierType), ProductIdentifierValue: p.ProductIdentifierValue}
	}
	if err := carbon.Wrap(carbonSrv).Set(carbon.Patch{ReplacementProductList: &same}); err != nil {
		t.Errorf("ActivatedCarbon's own list: %v", err)
	}
	// The plain slice a host's untyped Set hands over is the same list.
	if err := carbonSrv.SetAttributes(map[uint32]any{carbon.AttrReplacementProductList: same}); err != nil {
		t.Errorf("ActivatedCarbon's own list as a plain slice: %v", err)
	}
}

// TestTypedReplacementProductListOfAnotherForm: a list the definition
// accepts in a form the server does not compare (untyped entries) is
// refused as not the configuration's rather than stored.
func TestTypedReplacementProductListOfAnotherForm(t *testing.T) {
	t.Parallel()
	srv := full(t, &host{})
	untyped := []any{products[0], products[1]}
	if err := srv.SetAttributes(map[uint32]any{hepa.AttrReplacementProductList: untyped}); err == nil {
		t.Error("an untyped list was taken as the configured one")
	}
}
