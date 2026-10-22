// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package energy_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/energy"
	ct "github.com/SukramJ/go-fabric/cluster/spec/commoditytariff"
	dem "github.com/SukramJ/go-fabric/cluster/spec/deviceenergymanagement"
	ep "github.com/SukramJ/go-fabric/cluster/spec/energypreference"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	whm "github.com/SukramJ/go-fabric/cluster/spec/waterheatermanagement"
	"github.com/SukramJ/go-fabric/contract"
)

// TestWaterHeaterManagementOrigins is the origin audit of the
// WaterHeaterManagement server: the device's own TankPercentage is
// OriginHostSet, a Boost OriginCommand, a controller's write of the
// read-only BoostState nothing.
func TestWaterHeaterManagementOrigins(t *testing.T) {
	t.Parallel()
	spectest.CheckOrigins(t, func(tb testing.TB) contract.ClusterServer {
		tb.Helper()

		return newWaterHeater(t, energy.WaterHeaterFeatureTankPercent, &booster{})
	},
		spectest.OriginCase{Name: "host set", Want: contract.OriginHostSet, Act: func(tb testing.TB, srv contract.ClusterServer) {
			tb.Helper()
			if err := whm.Wrap(srv).Set(whm.Patch{TankPercentage: typedPtr(uint8(80))}); err != nil {
				tb.Fatal(err)
			}
		}},
		spectest.OriginCase{Name: "command", Want: contract.OriginCommand, Act: func(tb testing.TB, srv contract.ClusterServer) {
			tb.Helper()
			req := whm.BoostRequest{BoostInfo: energy.BoostInfo{Duration: 600, OneShot: typedPtr(true)}}
			if _, err := srv.MatterInvoke(context.Background(), whm.CmdBoost, req); err != nil {
				tb.Fatal(err)
			}
		}},
		spectest.OriginCase{Name: "controller write", Act: func(tb testing.TB, srv contract.ClusterServer) {
			tb.Helper()
			if err := srv.MatterWrite(context.Background(), whm.AttrBoostState, uint8(1)); err == nil {
				tb.Fatal("a write of the read-only BoostState succeeded")
			}
		}},
	)
}

// TestCommodityTariffOrigins is the origin audit of the CommodityTariff
// server: the host's tariff (a typed Set of the tariff attributes) is
// OriginHostSet, a controller's write of the read-only TariffUnit
// nothing. No command row: GetTariffComponent and GetDayEntry only read.
func TestCommodityTariffOrigins(t *testing.T) {
	t.Parallel()
	spectest.CheckOrigins(t, func(tb testing.TB) contract.ClusterServer {
		tb.Helper()

		srv, _ := newTariff(t)
		return srv
	},
		spectest.OriginCase{Name: "host set", Want: contract.OriginHostSet, Act: func(tb testing.TB, srv contract.ClusterServer) {
			tb.Helper()
			tariff := sample2()
			err := ct.Wrap(srv).Set(ct.Patch{
				TariffInfo:                 tariff.TariffInfo,
				TariffUnit:                 tariff.TariffUnit,
				StartDate:                  tariff.StartDate,
				DefaultRandomizationOffset: tariff.DefaultRandomizationOffset,
				DefaultRandomizationType:   tariff.DefaultRandomizationType,
				DayEntries:                 &tariff.DayEntries,
				DayPatterns:                &tariff.DayPatterns,
				TariffComponents:           &tariff.TariffComponents,
				TariffPeriods:              &tariff.TariffPeriods,
				IndividualDays:             &tariff.IndividualDays,
				CalendarPeriods:            &tariff.CalendarPeriods,
			})
			if err != nil {
				tb.Fatal(err)
			}
		}},
		spectest.OriginCase{Name: "controller write", Act: func(tb testing.TB, srv contract.ClusterServer) {
			tb.Helper()
			if err := srv.MatterWrite(context.Background(), ct.AttrTariffUnit, uint8(1)); err == nil {
				tb.Fatal("a write of the read-only TariffUnit succeeded")
			}
		}},
	)
}

// TestDeviceEnergyManagementOrigins is the origin audit of the
// DeviceEnergyManagement server: the device's own AbsMaxPower is
// OriginHostSet, a PowerAdjustRequest OriginCommand, a controller's write
// of the read-only EsaState nothing.
func TestDeviceEnergyManagementOrigins(t *testing.T) {
	t.Parallel()
	spectest.CheckOrigins(t, func(tb testing.TB) contract.ClusterServer {
		tb.Helper()

		srv, _ := newDEM(t, demPFRSet, &demHost{})
		if err := srv.SetPowerAdjustmentCapability(triggerCapability()); err != nil {
			tb.Fatal(err)
		}
		return srv
	},
		spectest.OriginCase{Name: "host set", Want: contract.OriginHostSet, Act: func(tb testing.TB, srv contract.ClusterServer) {
			tb.Helper()
			if err := dem.Wrap(srv).Set(dem.Patch{AbsMaxPower: typedPtr(int64(9000000))}); err != nil {
				tb.Fatal(err)
			}
		}},
		spectest.OriginCase{Name: "command", Want: contract.OriginCommand, Act: func(tb testing.TB, srv contract.ClusterServer) {
			tb.Helper()
			req := dem.PowerAdjustRequestRequest{Power: 10000000, Duration: 20, Cause: dem.AdjustmentCauseLocalOptimization}
			if _, err := srv.MatterInvoke(context.Background(), dem.CmdPowerAdjustRequest, req); err != nil {
				tb.Fatal(err)
			}
		}},
		spectest.OriginCase{Name: "controller write", Act: func(tb testing.TB, srv contract.ClusterServer) {
			tb.Helper()
			if err := srv.MatterWrite(context.Background(), dem.AttrEsaState, uint8(0)); err == nil {
				tb.Fatal("a write of the read-only EsaState succeeded")
			}
		}},
	)
}

// TestEnergyPreferenceOrigins is the origin audit of the EnergyPreference
// server: the device's own CurrentEnergyBalance is OriginHostSet, a
// controller's write of it OriginControllerWrite. No command row: the
// cluster defines none.
func TestEnergyPreferenceOrigins(t *testing.T) {
	t.Parallel()
	spectest.CheckOrigins(t, func(tb testing.TB) contract.ClusterServer {
		tb.Helper()

		srv, err := energy.NewEnergyPreference(preferenceConfig(energy.PreferenceFeatureEnergyBalance))
		if err != nil {
			tb.Fatal(err)
		}
		return srv
	},
		spectest.OriginCase{Name: "host set", Want: contract.OriginHostSet, Act: func(tb testing.TB, srv contract.ClusterServer) {
			tb.Helper()
			if err := ep.Wrap(srv).Set(ep.Patch{CurrentEnergyBalance: typedPtr(uint8(1))}); err != nil {
				tb.Fatal(err)
			}
		}},
		spectest.Write(ep.AttrCurrentEnergyBalance, uint8(2)),
	)
}
