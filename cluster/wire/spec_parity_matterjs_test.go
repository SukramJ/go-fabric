// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package wire_test

import (
	"context"
	"errors"
	"testing"

	admdef "github.com/SukramJ/go-fabric/cluster/spec/administratorcommissioning"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	swdef "github.com/SukramJ/go-fabric/cluster/spec/switchcluster"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
)

// TestServersMatchTheGeneratedDefinitions holds GenericSwitch (both
// feature selections it serves) and AdministratorCommissioning against
// matter.js switch.element.ts and administrator-commissioning.element.ts:
// FeatureMap and ClusterRevision, the mandatory attributes, commands and
// events listed and nothing disallowed, matter.js's invoke privileges,
// UNSUPPORTED_WRITE on every read-only attribute.
func TestServersMatchTheGeneratedDefinitions(t *testing.T) {
	t.Parallel()
	ms := uint32(swdef.FeatureMomentarySwitch | swdef.FeatureMomentarySwitchRelease)
	spectest.CheckServer(t, wire.NewGenericSwitch(1, &fakeGenericSwitchSource{positions: 2}), swdef.Definition, ms)
	spectest.CheckServer(t, wire.NewGenericSwitch(1, &fakeGenericSwitchSource{positions: 2, supportsLong: true}), swdef.Definition,
		ms|uint32(swdef.FeatureMomentarySwitchLongPress))
	spectest.CheckServer(t, wire.NewAdministratorCommissioning(), admdef.Definition, 0)
}

// TestSwitchEventsAreTheGeneratedPayloads round-trips the press payloads
// the server emits through the definition's event decoders.
func TestSwitchEventsAreTheGeneratedPayloads(t *testing.T) {
	t.Parallel()
	spectest.RoundTripEvent(t, swdef.Definition, wire.MatterEventInitialPress, wire.SwitchInitialPressEvent{NewPosition: 1})
	spectest.RoundTripEvent(t, swdef.Definition, wire.MatterEventLongPress, wire.SwitchLongPressEvent{NewPosition: 1})
	spectest.RoundTripEvent(t, swdef.Definition, wire.MatterEventShortRelease, wire.SwitchShortReleaseEvent{PreviousPosition: 1})
	spectest.RoundTripEvent(t, swdef.Definition, wire.MatterEventLongRelease, wire.SwitchLongReleaseEvent{PreviousPosition: 1})
}

// TestAdmCommTakesTheGeneratedOpenWindowRequest pins that an in-process
// caller may hand OpenCommissioningWindow the generated request: it
// reaches the same PAKE checks as the OpenWindowParams the bridge
// decodes.
func TestAdmCommTakesTheGeneratedOpenWindowRequest(t *testing.T) {
	t.Parallel()
	ctx := im.WithFabricFilter(context.Background(), true, 1)
	ac := wire.NewAdministratorCommissioning()
	for _, fields := range []any{
		admdef.OpenCommissioningWindowRequest{Iterations: 1},
		&admdef.OpenCommissioningWindowRequest{Iterations: 1},
	} {
		_, err := ac.MatterInvoke(ctx, admdef.CmdOpenCommissioningWindow, fields)
		if sce, ok := errors.AsType[im.MatterClusterStatusError](err); !ok || sce.MatterClusterStatus() != uint8(admdef.StatusCodePakeParameterError) {
			t.Errorf("%T: %v, want PAKEParameterError", fields, err)
		}
	}
	for _, fields := range []any{nil, (*admdef.OpenCommissioningWindowRequest)(nil), (*wire.OpenWindowParams)(nil)} {
		_, err := ac.MatterInvoke(ctx, admdef.CmdOpenCommissioningWindow, fields)
		if sce, ok := errors.AsType[im.StatusCodeError](err); !ok || sce.MatterStatusCode() != im.StatusInvalidCommand {
			t.Errorf("%T: %v, want INVALID_COMMAND", fields, err)
		}
	}
}
