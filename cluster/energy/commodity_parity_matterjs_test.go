// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package energy_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/energy"
	"github.com/SukramJ/go-fabric/cluster/spec"
	commtr "github.com/SukramJ/go-fabric/cluster/spec/commoditymetering"
	cp "github.com/SukramJ/go-fabric/cluster/spec/commodityprice"
	ct "github.com/SukramJ/go-fabric/cluster/spec/commoditytariff"
	egc "github.com/SukramJ/go-fabric/cluster/spec/electricalgridconditions"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/schema"
)

// TestParityMatterJS_CommodityServers builds every feature selection of
// the four energy-market clusters and holds each server against the
// matter.js conformance (spectest.CheckServer): FeatureMap,
// ClusterRevision, the attribute, command and event lists, the privileges
// and the read-only writes. matter.js's servers are the generated
// behaviors (CommodityPriceServer.ts:14, ElectricalGridConditionsServer.ts:14,
// CommodityMeteringServer.ts:14, CommodityTariffServer.ts:17).
func TestParityMatterJS_CommodityServers(t *testing.T) {
	t.Parallel()
	for features := range energy.CommodityPriceFeature(1 << 1) {
		for _, optional := range []bool{false, true} {
			srv, err := energy.NewCommodityPrice(energy.CommodityPriceConfig{Features: features, DetailedPrice: optional, DetailedForecast: optional})
			if err != nil {
				t.Fatalf("CommodityPrice 0x%X: %v", features, err)
			}
			spectest.CheckServer(t, srv, cp.Definition, uint32(features))
			accepted := srv.MatterAcceptedCommands()
			if got, want := len(accepted), map[bool]int{false: 0, true: 1 + int(features)}[optional]; got != want {
				t.Errorf("CommodityPrice 0x%X optional=%v accepts %v", features, optional, accepted)
			}
		}
	}
	for features := range energy.GridConditionsFeature(1 << 1) {
		srv, err := energy.NewElectricalGridConditions(energy.GridConditionsConfig{Features: features})
		if err != nil {
			t.Fatalf("ElectricalGridConditions 0x%X: %v", features, err)
		}
		spectest.CheckServer(t, srv, egc.Definition, uint32(features))
	}
	meter, err := energy.NewCommodityMetering(energy.CommodityMeteringConfig{})
	if err != nil {
		t.Fatal(err)
	}
	spectest.CheckServer(t, meter, commtr.Definition, 0)
	built := 0
	for features := range energy.TariffFeature(1 << 6) {
		srv, err := energy.NewCommodityTariff(energy.CommodityTariffConfig{Features: features})
		if ferr := spec.CheckFeatures(ct.Definition, uint32(features)); ferr != nil {
			if err == nil {
				t.Errorf("CommodityTariff 0x%02X built, the definition refuses it: %v", features, ferr)
			}
			continue
		}
		if err != nil {
			t.Fatalf("CommodityTariff 0x%02X: %v", features, err)
		}
		spectest.CheckServer(t, srv, ct.Definition, uint32(features))
		built++
	}
	if built == 0 {
		t.Fatal("no CommodityTariff selection built")
	}
	// The device types that carry them (schema/devicetypes.go: 0x0513
	// ElectricalEnergyTariff, 0x0514 ElectricalMeter).
	for _, c := range []struct {
		deviceType uint32
		cluster    uint32
	}{
		{0x0513, energy.ClusterIDCommodityPrice},
		{0x0513, energy.ClusterIDElectricalGridConditions},
		{0x0513, energy.ClusterIDCommodityTariff},
		{0x0514, energy.ClusterIDCommodityMetering},
		{0x0514, energy.ClusterIDCommodityPrice},
	} {
		if allowed, known := schema.DeviceTypeAllowsServerCluster(c.deviceType, c.cluster); !allowed || !known {
			t.Errorf("device type 0x%04X does not offer cluster 0x%04X", c.deviceType, c.cluster)
		}
	}
}
