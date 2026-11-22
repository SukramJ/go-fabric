// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/thermostat/ThermostatBehaviorTest.ts
// at b25ed960: describe("SetpointRaiseLower through the interaction
// layer") (:490-530) — the request gate before the handler
// (packages/node/src/node/integration/ProtocolService.ts:671-704 at
// b25ed960): a member its conformance disallows only gets a warning and
// the handler refuses it, a value the enum does not define is refused
// before the handler. matter.js invokes over the wire of a MockServerNode;
// the port invokes the server, whose request gate is the same.

package thermo_test

import (
	"context"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	tdef "thingfabric.dev/go-fabric/cluster/spec/thermostat"
	"thingfabric.dev/go-fabric/cluster/thermo"
	"thingfabric.dev/go-fabric/im"
)

func TestThermostatRequestCheckParityMatterJS(t *testing.T) {
	t.Parallel()
	// raiseLowerOnCoolingThermostat is :491-519: a thermostat with the
	// Cooling feature alone, CoolingOnly and SystemMode Cool.
	raiseLowerOnCoolingThermostat := func(t *testing.T, mode tdef.SetpointRaiseLowerModeEnum) im.StatusCode {
		t.Helper()
		srv := thermo.NewThermostatServer(thermo.ThermostatConfig{
			Features:                thermo.ThermostatFeatureCOOL,
			AbsMinCoolSetpointLimit: 1600,
			AbsMaxCoolSetpointLimit: 3200,
			InitialCoolingSetpoint:  2600,
		})
		// The request passes the endpoint dispatcher's gate first, as an
		// interaction-layer invoke does (decision 339, endpoint
		// requestHeld): the server's FeatureMap and attributes.
		ctx, req := context.Background(), tdef.SetpointRaiseLowerRequest{Mode: mode, Amount: 5}
		fm, _ := srv.MatterRead(0xFFFC)
		features, _ := fm.(uint32)
		if err := spec.AdmitRequest(ctx, tdef.Definition, features, tdef.CmdSetpointRaiseLower, req, srv.MatterRead); err != nil {
			return spec.StatusOf(err)
		}
		_, err := srv.MatterInvoke(ctx, tdef.CmdSetpointRaiseLower, req)
		return spec.StatusOf(err)
	}
	t.Run("SetpointRaiseLower through the interaction layer", func(t *testing.T) {
		t.Parallel()
		t.Run("leaves refusing Heat without the Heating feature to the handler, with INVALID_COMMAND", func(t *testing.T) { // :521-525
			t.Parallel()
			if got := raiseLowerOnCoolingThermostat(t, tdef.SetpointRaiseLowerModeHeat); got != im.StatusInvalidCommand {
				t.Errorf("status %v, want INVALID_COMMAND", got)
			}
		})
		t.Run("refuses a mode the enum does not define with CONSTRAINT_ERROR", func(t *testing.T) { // :527-529
			t.Parallel()
			if got := raiseLowerOnCoolingThermostat(t, tdef.SetpointRaiseLowerModeEnum(3)); got != im.StatusConstraintError {
				t.Errorf("status %v, want CONSTRAINT_ERROR", got)
			}
		})
	})
}
