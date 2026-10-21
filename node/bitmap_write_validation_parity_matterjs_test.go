// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/BitmapWriteValidationTest.ts at
// 862ad75f: a write of WindowCovering.Mode (a map8 with bits 0..3 defined)
// — a reserved bit refused with CONSTRAINT_ERROR, a defined bit accepted,
// on a covering with lift and tilt and on one with tilt only.
//
// matter.js writes on a MockServerNode as administrator; the port writes
// over the wire from a commissioned fabrictest pair. The covering is a
// bridged endpoint below the Aggregator, where matter.js has endpoint 1.
package node_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/cover"
	wcdef "github.com/SukramJ/go-fabric/cluster/spec/windowcovering"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

func TestBitmapWriteValidationParity(t *testing.T) {
	t.Parallel()

	// TestWindowCoveringDevice: Lift, Tilt, PositionAwareLift,
	// PositionAwareTilt, TiltBlindLift, InteriorVenetianBlind.
	liftTilt := cover.Config{
		FeatureMap: cover.ServedLiftTiltFeatureMap, Type: uint8(wcdef.TypeTiltBlindLift),
		EndProductType: uint8(wcdef.EndProductTypeInteriorVenetianBlind),
	}
	// TiltOnlyWindowCoveringDevice.
	tiltOnly := cover.Config{
		FeatureMap: cover.ServedTiltFeatureMap, Type: uint8(wcdef.TypeTiltBlindTiltOnly),
		EndProductType: uint8(wcdef.EndProductTypeTiltOnlyInteriorBlind),
	}

	for _, c := range []struct {
		name  string
		cfg   cover.Config
		value uint64
		want  im.StatusCode
	}{
		{"rejects a reserved bit with ConstraintError", liftTilt, 0x10, im.StatusConstraintError},
		{"accepts a defined bit", liftTilt, 0x01, im.StatusSuccess},
		// ConfigStatus.LiftMovementReversed states the Lift feature, so a
		// covering with no lift must not mirror the mode into it.
		{"accepts the motor direction bit on a covering with no lift", tiltOnly, 0x01, im.StatusSuccess},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			site := quietSite(t)
			wc, err := device.NewWindowCovering(device.Options{StableKey: endpoint.StringKey("cover:1"), Name: "cover", Clock: site.Clock()},
				device.WithServer(cover.NewWindowCoveringServer(c.cfg)))
			if err != nil {
				t.Fatal(err)
			}
			p := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{wc}}})
			ep, _ := p.Device.EndpointOf("cover:1")
			m := &mockPair{site: site, pair: p}
			mode := attrPath(ep, wcdef.ClusterID, wcdef.AttrMode)
			got := writeStatuses(parityCtx(t), t, m, interaction.WriteRequest{Path: mode, Value: uintVal(c.value)})
			assertWriteStatuses(t, got, []interaction.WriteStatus{{Path: mode, Status: c.want}})
		})
	}
}
