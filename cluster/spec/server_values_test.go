// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	hepa "github.com/SukramJ/go-fabric/cluster/spec/hepafiltermonitoring"
	"github.com/SukramJ/go-fabric/cluster/spec/icdmanagement"
	"github.com/SukramJ/go-fabric/cluster/spec/smokecoalarm"
	"github.com/SukramJ/go-fabric/cluster/spec/temperaturemeasurement"
	"github.com/SukramJ/go-fabric/im"
)

// TestServerDefaults starts a served attribute the host gives no value at
// its model default (a uint16 0, a bool false, a null), and leaves one
// whose definition states no default absent.
func TestServerDefaults(t *testing.T) {
	t.Parallel()
	temp, err := spec.NewServer(temperaturemeasurement.Definition, spec.Options{Attributes: []uint32{temperaturemeasurement.AttrTolerance}}, spec.ServerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := temp.MatterRead(temperaturemeasurement.AttrTolerance); !ok || v != uint16(0) {
		t.Errorf("Tolerance %#v (%v)", v, ok)
	}
	if v, ok := temp.MatterRead(temperaturemeasurement.AttrMeasuredValue); ok {
		t.Errorf("MeasuredValue states no default, got %#v", v)
	}
	smoke, err := spec.NewServer(smokecoalarm.Definition, spec.Options{Features: uint32(smokecoalarm.FeatureSmokeAlarm), Attributes: []uint32{smokecoalarm.AttrUnmounted}}, spec.ServerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := smoke.MatterRead(smokecoalarm.AttrUnmounted); !ok || v != false {
		t.Errorf("Unmounted %#v (%v)", v, ok)
	}
	if _, err := spec.NewServer(icdmanagement.Definition, spec.Options{}, spec.ServerConfig{}); err != nil {
		t.Errorf("an unserved fabric-scoped attribute does not matter: %v", err)
	}
}

// TestServerListAndStructConstraints holds a list value to its length
// constraint and each struct entry to its field constraints.
func TestServerListAndStructConstraints(t *testing.T) {
	t.Parallel()
	srv := hepaServer(t, spec.ServerConfig{})
	for _, c := range []struct {
		name  string
		value any
	}{
		{"six entries, max 5", slices.Repeat(products, 6)},
		{"identifier longer than 20", spec.List[hepa.ReplacementProductStruct]{{ProductIdentifierValue: "123456789012345678901"}}},
		{"identifier type not defined", spec.List[hepa.ReplacementProductStruct]{{ProductIdentifierType: 9}}},
		{"not a list", "x"},
		{"entries not structs", []int{1}},
	} {
		if err := srv.Set(hepa.AttrReplacementProductList, c.value); !errors.Is(err, spec.ErrInvalidValue) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	if err := srv.Set(hepa.AttrReplacementProductList, slices.Repeat(products, 5)); err != nil {
		t.Errorf("five entries: %v", err)
	}
	if _, err := srv.CheckValue(0x30, nil, nil); statusOf(err) != im.StatusUnsupportedAttribute {
		t.Errorf("an attribute the cluster does not define: %v", err)
	}
}
