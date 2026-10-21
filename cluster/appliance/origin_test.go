// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package appliance_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/appliance"
	mwo "github.com/SukramJ/go-fabric/cluster/spec/microwaveovencontrol"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/contract"
)

// TestMicrowaveOvenControlOrigins is the origin audit of the
// MicrowaveOvenControl server: the device's own CookTime is
// OriginHostSet, a SetCookingParameters OriginCommand, a controller's
// write of the read-only CookTime nothing (the cluster has no writable
// attribute).
func TestMicrowaveOvenControlOrigins(t *testing.T) {
	t.Parallel()
	spectest.CheckOrigins(t, func(tb testing.TB) contract.ClusterServer {
		tb.Helper()

		return newMicrowave(t, &oven{}, appliance.MicrowaveFeaturePowerAsNumber|appliance.MicrowaveFeaturePowerNumberLimits)
	},
		spectest.OriginCase{Name: "host set", Want: contract.OriginHostSet, Act: func(tb testing.TB, srv contract.ClusterServer) {
			tb.Helper()
			if err := mwo.Wrap(srv).Set(mwo.Patch{CookTime: ptr(uint32(120))}); err != nil {
				tb.Fatal(err)
			}
		}},
		spectest.OriginCase{Name: "command", Want: contract.OriginCommand, Act: func(tb testing.TB, srv contract.ClusterServer) {
			tb.Helper()
			req := mwo.SetCookingParametersRequest{CookTime: ptr(uint32(100)), PowerSetting: ptr(uint8(60))}
			if _, err := srv.MatterInvoke(context.Background(), mwo.CmdSetCookingParameters, req); err != nil {
				tb.Fatal(err)
			}
		}},
		spectest.OriginCase{Name: "controller write", Act: func(tb testing.TB, srv contract.ClusterServer) {
			tb.Helper()
			if err := srv.MatterWrite(context.Background(), mwo.AttrCookTime, uint32(60)); err == nil {
				tb.Fatal("a write of the read-only CookTime succeeded")
			}
		}},
	)
}
