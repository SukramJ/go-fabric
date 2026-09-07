// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package cover_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/cover"
	"github.com/SukramJ/go-fabric/cluster/wire"
)

// TestParity_WindowCovering_ConfigStatus_Bitmap verifies that
// ConfigStatus (0x0007) advertises Operational and LiftPositionAware —
// the bitmap matter.js derives for a position-aware lift-only cover
// (packages/node/src/behaviors/window-covering/WindowCoveringServer.ts:121-125
// sets operational and liftPositionAware in initialize()) — and does not
// claim LiftMovementReversed, which matter.js only sets from
// Mode.MotorDirectionReversed (WindowCoveringServer.ts:189).
//
// Bit positions come from the ConfigStatusBitmap datatype in
// packages/model/src/standard/elements/window-covering-cluster.element.ts:109-116:
// Operational constraint "0", OnlineReserved "1", LiftMovementReversed "2",
// LiftPositionAware "3". Bits are sparse-by-name here, so they are read
// from the element file rather than counted from the field order.
func TestParity_WindowCovering_ConfigStatus_Bitmap(t *testing.T) {
	t.Parallel()
	srv := cover.NewWindowCoveringServer(cover.Config{
		Type:           0,
		EndProductType: 0,
		FeatureMap:     0x05, // LF (bit 0) + PA_LF (bit 2)
	})

	v, ok := srv.MatterRead(wire.WindowCoveringAttrConfigStatus)
	if !ok {
		t.Fatal("ConfigStatus: ok=false")
	}
	got := v.(uint8)
	const (
		bitOperational          uint8 = 1 << 0 // window-covering-cluster.element.ts:110 constraint "0"
		bitLiftMovementReversed uint8 = 1 << 2 // window-covering-cluster.element.ts:112 constraint "2"
		bitLiftPosAware         uint8 = 1 << 3 // window-covering-cluster.element.ts:113 constraint "3"
		wantConfigStatus              = bitOperational | bitLiftPosAware
	)
	if got != wantConfigStatus {
		t.Errorf("ConfigStatus = 0x%02X, want 0x%02X (Operational | LiftPositionAware)", got, wantConfigStatus)
	}
	if got&bitLiftMovementReversed != 0 {
		t.Errorf("ConfigStatus = 0x%02X claims LiftMovementReversed (bit 2); matter.js sets that only from Mode.MotorDirectionReversed", got)
	}
}
