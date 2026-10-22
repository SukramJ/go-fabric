// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/contract"
)

// TestThermostatOrigins is the origin audit of the Thermostat server
// (gate-2 consumer, defect 3, brief BY-1 §2): the host's Set is
// OriginHostSet, a SetpointRaiseLower OriginCommand, and a controller's
// write of each writable attribute reaches the change listeners — the
// host's OnChanged — as the written attribute from OriginControllerWrite,
// as the matter.js Datasource queues a change notification on the
// write's commit (packages/node/src/behavior/state/managed/Datasource.ts:1473-1476).
func TestThermostatOrigins(t *testing.T) {
	t.Parallel()
	cfg := thermo.DefaultThermostatConfig()
	cfg.Features |= thermo.ThermostatFeatureOCC
	spectest.CheckOrigins(t, func(tb testing.TB) contract.ClusterServer {
		tb.Helper()
		return thermo.NewThermostatServer(cfg)
	},
		spectest.OriginCase{Name: "host set", Want: contract.OriginHostSet, Attributes: []uint32{tdef.AttrOccupiedHeatingSetpoint}, Act: func(tb testing.TB, srv contract.ClusterServer) {
			tb.Helper()
			sp := int16(2100)
			if err := tdef.Wrap(srv).Set(tdef.Patch{OccupiedHeatingSetpoint: &sp}); err != nil {
				tb.Fatal(err)
			}
		}},
		spectest.OriginCase{Name: "command", Want: contract.OriginCommand, Attributes: []uint32{tdef.AttrOccupiedHeatingSetpoint}, Act: func(tb testing.TB, srv contract.ClusterServer) {
			tb.Helper()
			ctx := contract.WithOrigin(context.Background(), contract.OriginCommand)
			if _, err := srv.MatterInvoke(ctx, tdef.CmdSetpointRaiseLower, tdef.SetpointRaiseLowerRequest{Mode: tdef.SetpointRaiseLowerModeEnum(0), Amount: 5}); err != nil {
				tb.Fatal(err)
			}
		}},
		spectest.Write(tdef.AttrOccupiedHeatingSetpoint, int64(2250)),
		spectest.Write(tdef.AttrOccupiedCoolingSetpoint, int64(2700)),
		spectest.Write(tdef.AttrUnoccupiedHeatingSetpoint, int64(1800)),
		spectest.Write(tdef.AttrUnoccupiedCoolingSetpoint, int64(2800)),
		spectest.Write(tdef.AttrMinHeatSetpointLimit, int64(800)),
		spectest.Write(tdef.AttrMaxHeatSetpointLimit, int64(2900)),
		spectest.Write(tdef.AttrMinCoolSetpointLimit, int64(1700)),
		spectest.Write(tdef.AttrMaxCoolSetpointLimit, int64(3100)),
		spectest.Write(tdef.AttrLocalTemperatureCalibration, int64(-5)),
		spectest.Write(tdef.AttrSystemMode, uint64(4)), // Heat
	)
}
