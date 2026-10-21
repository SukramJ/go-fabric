// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	tc "github.com/SukramJ/go-fabric/cluster/spec/temperaturecontrol"
	"github.com/SukramJ/go-fabric/contract"
)

// TestTemperatureControlOrigins is the origin audit of the
// TemperatureControl server: the device's own TemperatureSetpoint is
// OriginHostSet, a SetTemperature OriginCommand, a controller's write of
// the read-only TemperatureSetpoint nothing (the cluster has no writable
// attribute).
func TestTemperatureControlOrigins(t *testing.T) {
	t.Parallel()
	spectest.CheckOrigins(t, func(tb testing.TB) contract.ClusterServer {
		tb.Helper()
		return newTC(t, numberConfig(&setter{}))
	},
		spectest.OriginCase{Name: "host set", Want: contract.OriginHostSet, Act: func(tb testing.TB, srv contract.ClusterServer) {
			tb.Helper()
			if err := tc.Wrap(srv).Set(tc.Patch{TemperatureSetpoint: ptr(int16(-1500))}); err != nil {
				tb.Fatal(err)
			}
		}},
		spectest.OriginCase{Name: "command", Want: contract.OriginCommand, Act: func(tb testing.TB, srv contract.ClusterServer) {
			tb.Helper()
			if _, err := srv.MatterInvoke(context.Background(), tc.CmdSetTemperature, tc.SetTemperatureRequest{TargetTemperature: ptr(int16(-1700))}); err != nil {
				tb.Fatal(err)
			}
		}},
		spectest.OriginCase{Name: "controller write", Act: func(tb testing.TB, srv contract.ClusterServer) {
			tb.Helper()
			if err := srv.MatterWrite(context.Background(), tc.AttrTemperatureSetpoint, int16(-1500)); err == nil {
				tb.Fatal("a write of the read-only TemperatureSetpoint succeeded")
			}
		}},
	)
}
