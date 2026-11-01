// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cover_test

import (
	"context"
	"errors"
	"slices"
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
		srv, err := cover.New(cover.Config{Type: uint8(wcdef.TypeUnknown), EndProductType: uint8(wcdef.EndProductTypeUnknown), FeatureMap: c.cfg})
		if err != nil {
			t.Fatalf("FeatureMap 0x%X: %v", c.cfg, err)
		}
		spectest.CheckServer(t, srv, wcdef.Definition, c.want)
	}
}

// TestFeatureMapIsDerivedFromWhatTheServerServes pins the FeatureMaps the
// server serves — every combination of Lift and Tilt, each with or
// without its position-aware feature, as matter.js's WindowCoveringServer
// branches per feature (WindowCoveringServer.ts:123-141 at 862ad75f) —
// and the refusal of the rest, New with ErrFeatureMap,
// NewWindowCoveringServer with a panic: AbsolutePosition, a position-aware
// feature without its axis, no axis.
func TestFeatureMapIsDerivedFromWhatTheServerServes(t *testing.T) {
	t.Parallel()
	lf, tl := uint32(wcdef.FeatureLift), uint32(wcdef.FeatureTilt)
	palf, patl := uint32(wcdef.FeaturePositionAwareLift), uint32(wcdef.FeaturePositionAwareTilt)
	tiltType := uint8(wcdef.TypeTiltBlindLift)
	for _, fm := range []uint32{lf, lf | palf, tl, tl | patl, lf | tl, lf | palf | tl, lf | tl | patl, lf | palf | tl | patl} {
		cfg := cover.Config{FeatureMap: fm}
		switch {
		case fm&lf != 0 && fm&tl != 0:
			cfg.Type, cfg.EndProductType = tiltType, uint8(wcdef.EndProductTypeInteriorBlind)
		case fm&tl != 0:
			cfg.Type, cfg.EndProductType = uint8(wcdef.TypeShutter), uint8(wcdef.EndProductTypeSwingingShutter)
		}
		if _, err := cover.New(cfg); err != nil {
			t.Errorf("FeatureMap 0x%X: %v", fm, err)
		}
	}
	for _, fm := range []uint32{
		palf,
		lf | patl,
		tl | palf,
		cover.ServedLiftTiltFeatureMap | 1<<3, // AbsolutePosition (bit 3)
	} {
		if _, err := cover.New(cover.Config{FeatureMap: fm, Type: tiltType}); !errors.Is(err, cover.ErrFeatureMap) {
			t.Errorf("FeatureMap 0x%X: New error %v, want ErrFeatureMap", fm, err)
		}
	}
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewWindowCoveringServer accepted a FeatureMap New refuses")
		}
	}()
	cover.NewWindowCoveringServer(cover.Config{FeatureMap: palf})
}

// TestLiftWithoutPositionAwareness: a Lift-only covering (FeatureMap 0x1)
// serves no position attribute and no GoToLiftPercentage, leaves
// LiftPositionAware out of ConfigStatus, and moves by direction —
// UpOrOpen Opening, DownOrClose Closing, StopMotion Stopped — as matter.js's
// WindowCoveringServer does without position awareness
// (WindowCoveringServer.ts:123-127, :420-446, :487-504, :518-560 at
// 862ad75f).
func TestLiftWithoutPositionAwareness(t *testing.T) {
	t.Parallel()
	srv, err := cover.New(cover.Config{FeatureMap: uint32(wcdef.FeatureLift)})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range []uint32{wcdef.AttrCurrentPositionLiftPercent100ths, wcdef.AttrTargetPositionLiftPercent100ths, wcdef.AttrCurrentPositionLiftPercentage} {
		if _, ok := srv.MatterRead(a); ok || slices.Contains(srv.MatterAttributes(), a) {
			t.Errorf("attribute 0x%04X served", a)
		}
	}
	// GoToLiftPercentage is served without position awareness, as
	// matter.js's server implements it for every axis
	// (WindowCoveringServer.ts:576-596; W3-club1-04).
	if !slices.Contains(srv.MatterAcceptedCommands(), wcdef.CmdGoToLiftPercentage) {
		t.Error("GoToLiftPercentage not accepted")
	}
	if st, _ := srv.MatterRead(wcdef.AttrConfigStatus); st.(uint8)&(1<<3) != 0 {
		t.Errorf("ConfigStatus 0x%02X names LiftPositionAware", st)
	}
	status := func() uint8 { v, _ := srv.MatterRead(wcdef.AttrOperationalStatus); return v.(uint8) }
	ctx := context.Background()
	for _, c := range []struct {
		cmd  uint32
		want uint8
	}{
		{wcdef.CmdUpOrOpen, 1 | 1<<2},
		{wcdef.CmdStopMotion, 0},
		{wcdef.CmdDownOrClose, 2 | 2<<2},
		{wcdef.CmdStopMotion, 0},
	} {
		if _, err := srv.MatterInvoke(ctx, c.cmd, nil); err != nil {
			t.Fatalf("command 0x%02X: %v", c.cmd, err)
		}
		if got := status(); got != c.want {
			t.Fatalf("after command 0x%02X OperationalStatus 0x%02X, want 0x%02X", c.cmd, got, c.want)
		}
	}
	// GoToLiftPercentage 0 is UpOrOpen, any other value DownOrClose
	// (WindowCoveringServer.ts:586-590 at 862ad75f; W3-club1-04).
	for _, c := range []struct {
		pct  uint16
		want uint8
	}{{0, 1 | 1<<2}, {2500, 2 | 2<<2}} {
		if _, err := srv.MatterInvoke(ctx, wcdef.CmdStopMotion, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := srv.MatterInvoke(ctx, wcdef.CmdGoToLiftPercentage, wcdef.GoToLiftPercentageRequest{LiftPercent100thsValue: c.pct}); err != nil {
			t.Fatalf("GoToLiftPercentage(%d): %v", c.pct, err)
		}
		if got := status(); got != c.want {
			t.Fatalf("after GoToLiftPercentage(%d) OperationalStatus 0x%02X, want 0x%02X", c.pct, got, c.want)
		}
	}
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
