// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package cover_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/cover"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	wcdef "github.com/SukramJ/go-fabric/cluster/spec/windowcovering"
	"github.com/SukramJ/go-fabric/im"
)

// TestServerMatchesTheGeneratedDefinition holds the server against matter.js
// window-covering-cluster.element.ts (spectest.CheckServer) for each
// FeatureMap it serves — LF | PA_LF (also when the host leaves it zero),
// TL | PA_TL and both.
func TestServerMatchesTheGeneratedDefinition(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ cfg, want uint32 }{
		{0, cover.ServedFeatureMap},
		{cover.ServedFeatureMap, cover.ServedFeatureMap},
		{cover.ServedTiltFeatureMap, cover.ServedTiltFeatureMap},
		{cover.ServedLiftTiltFeatureMap, cover.ServedLiftTiltFeatureMap},
	} {
		srv, err := cover.New(cover.Config{FeatureMap: c.cfg})
		if err != nil {
			t.Fatalf("FeatureMap 0x%X: %v", c.cfg, err)
		}
		spectest.CheckServer(t, srv, wcdef.Definition, c.want)
	}
}

// TestFeatureMapIsDerivedFromWhatTheServerServes pins the refusal of every
// other FeatureMap — New with ErrFeatureMap, NewWindowCoveringServer with a
// panic: an axis without its position-aware feature, and a feature the
// server has no state for.
func TestFeatureMapIsDerivedFromWhatTheServerServes(t *testing.T) {
	t.Parallel()
	for _, fm := range []uint32{
		uint32(wcdef.FeatureLift),
		uint32(wcdef.FeatureTilt),
		cover.ServedFeatureMap | uint32(wcdef.FeatureTilt),
		cover.ServedTiltFeatureMap | uint32(wcdef.FeatureLift),
		cover.ServedLiftTiltFeatureMap | 1<<3, // AbsolutePosition (bit 3)
	} {
		if _, err := cover.New(cover.Config{FeatureMap: fm}); !errors.Is(err, cover.ErrFeatureMap) {
			t.Errorf("FeatureMap 0x%X: New error %v, want ErrFeatureMap", fm, err)
		}
	}
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewWindowCoveringServer accepted a FeatureMap New refuses")
		}
	}()
	cover.NewWindowCoveringServer(cover.Config{FeatureMap: uint32(wcdef.FeatureTilt)})
}

// TestGoToLiftPercentageTakesTheGeneratedRequest pins the shape the bridge
// now hands over, and that a read-only attribute's write is
// UNSUPPORTED_WRITE (matter.js AttributeWriteResponse), an unserved one's
// UNSUPPORTED_ATTRIBUTE.
func TestGoToLiftPercentageTakesTheGeneratedRequest(t *testing.T) {
	t.Parallel()
	srv := cover.NewWindowCoveringServer(cover.Config{})
	for _, req := range []any{wcdef.GoToLiftPercentageRequest{LiftPercent100thsValue: 2500}, &wcdef.GoToLiftPercentageRequest{LiftPercent100thsValue: 2500}} {
		if _, err := srv.MatterInvoke(context.Background(), wcdef.CmdGoToLiftPercentage, req); err != nil {
			t.Fatalf("GoToLiftPercentage(%T): %v", req, err)
		}
		if v, _ := srv.MatterRead(wcdef.AttrTargetPositionLiftPercent100ths); v != uint16(2500) {
			t.Errorf("TargetPositionLiftPercent100ths = %v, want 2500", v)
		}
	}
	if _, err := srv.MatterInvoke(context.Background(), wcdef.CmdGoToLiftPercentage, (*wcdef.GoToLiftPercentageRequest)(nil)); err == nil {
		t.Error("GoToLiftPercentage(nil) accepted")
	}
	for attr, want := range map[uint32]im.StatusCode{
		wcdef.AttrType:                          im.StatusUnsupportedWrite,
		wcdef.AttrCurrentPositionTiltPercentage: im.StatusUnsupportedAttribute,
	} {
		err := srv.MatterWrite(context.Background(), attr, uint8(0))
		if sce, ok := errors.AsType[im.StatusCodeError](err); !ok || sce.MatterStatusCode() != want {
			t.Errorf("write 0x%04X: %v, want status 0x%02X", attr, err, want)
		}
	}
}
