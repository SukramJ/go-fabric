// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cover_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/cover"
	wcdef "github.com/SukramJ/go-fabric/cluster/spec/windowcovering"
	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
)

// axesMover records every GoTo with both targets.
type axesMover struct {
	mu    sync.Mutex
	calls []axesCall
	err   error
}

type axesCall struct {
	op         string
	lift, tilt *uint16
}

func (m *axesMover) GoTo(_ context.Context, lift, tilt *uint16) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, axesCall{op: "goto", lift: cloneU16(lift), tilt: cloneU16(tilt)})
	return m.err
}

func (m *axesMover) Stop(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, axesCall{op: "stop"})
	return m.err
}

func (m *axesMover) recorded() []axesCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]axesCall(nil), m.calls...)
}

func cloneU16(p *uint16) *uint16 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func u16(v uint16) *uint16 { return &v }

// TestTilt_ConfigStatusAndAttributes: a tilt axis sets TiltPositionAware
// (window-covering-cluster.element.ts:114 constraint "4"; matter.js
// WindowCoveringServer.ts:128-132 initialize()) and serves the tilt
// position attributes; a lift-only server does neither.
func TestTilt_ConfigStatusAndAttributes(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		fm         uint32
		configBits uint8
		lift, tilt bool
	}{
		{cover.ServedFeatureMap, 1<<0 | 1<<3, true, false},
		{cover.ServedTiltFeatureMap, 1<<0 | 1<<4, false, true},
		{cover.ServedLiftTiltFeatureMap, 1<<0 | 1<<3 | 1<<4, true, true},
	} {
		srv := cover.NewWindowCoveringServer(cover.Config{Type: uint8(wcdef.TypeUnknown), EndProductType: uint8(wcdef.EndProductTypeUnknown), FeatureMap: c.fm, InitialPositionPercent100ths: 1000, InitialTiltPositionPercent100ths: 2000})
		if v, _ := srv.MatterRead(wcdef.AttrConfigStatus); v != c.configBits {
			t.Errorf("0x%X: ConfigStatus %v, want 0x%02X", c.fm, v, c.configBits)
		}
		_, liftOK := srv.MatterRead(wcdef.AttrCurrentPositionLiftPercent100ths)
		_, tiltOK := srv.MatterRead(wcdef.AttrCurrentPositionTiltPercent100ths)
		if liftOK != c.lift || tiltOK != c.tilt {
			t.Errorf("0x%X: lift served %v tilt served %v, want %v %v", c.fm, liftOK, tiltOK, c.lift, c.tilt)
		}
		if c.tilt {
			if v, _ := srv.MatterRead(wcdef.AttrCurrentPositionTiltPercent100ths); v != uint16(2000) {
				t.Errorf("0x%X: tilt current %v, want 2000", c.fm, v)
			}
			if v, _ := srv.MatterRead(wcdef.AttrTargetPositionTiltPercent100ths); v != uint16(2000) {
				t.Errorf("0x%X: tilt target %v, want 2000 (synced at start, WindowCoveringServer.ts:138-140)", c.fm, v)
			}
			if v, _ := srv.MatterRead(wcdef.AttrCurrentPositionTiltPercentage); v != uint8(20) {
				t.Errorf("0x%X: tilt percentage %v, want 20", c.fm, v)
			}
		}
		accepted := srv.MatterAcceptedCommands()
		hasTiltCmd := false
		for _, id := range accepted {
			if id == wcdef.CmdGoToTiltPercentage {
				hasTiltCmd = true
			}
		}
		if hasTiltCmd != c.tilt {
			t.Errorf("0x%X: GoToTiltPercentage accepted %v, want %v", c.fm, hasTiltCmd, c.tilt)
		}
	}
}

// TestTilt_CommandsMoveBothAxes: UpOrOpen and DownOrClose set both targets
// (WindowCoveringServer.ts:521-535, :545-559 at 0ceb202d) and reach the
// Mover as one GoTo with both; GoToTiltPercentage moves tilt alone
// (:598-606); StopMotion snaps both targets to the positions (:491-496).
func TestTilt_CommandsMoveBothAxes(t *testing.T) {
	t.Parallel()
	m := &axesMover{}
	srv, err := cover.New(cover.Config{Type: uint8(wcdef.TypeTiltBlindLift), EndProductType: uint8(wcdef.EndProductTypeInteriorVenetianBlind), FeatureMap: cover.ServedLiftTiltFeatureMap, InitialPositionPercent100ths: 5000, InitialTiltPositionPercent100ths: 5000, Mover: m})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	invoke := func(cmd uint32, fields any) {
		t.Helper()
		if _, err := srv.MatterInvoke(ctx, cmd, fields); err != nil {
			t.Fatalf("command 0x%02X: %v", cmd, err)
		}
	}
	invoke(wcdef.CmdUpOrOpen, nil)
	invoke(wcdef.CmdDownOrClose, nil)
	invoke(wcdef.CmdGoToTiltPercentage, wcdef.GoToTiltPercentageRequest{TiltPercent100thsValue: 2500})
	invoke(wcdef.CmdGoToLiftPercentage, wcdef.GoToLiftPercentageRequest{LiftPercent100thsValue: 7500})
	if v, _ := srv.MatterRead(wcdef.AttrTargetPositionTiltPercent100ths); v != uint16(2500) {
		t.Errorf("tilt target %v, want 2500", v)
	}
	if v, _ := srv.MatterRead(wcdef.AttrTargetPositionLiftPercent100ths); v != uint16(7500) {
		t.Errorf("lift target %v, want 7500", v)
	}
	srv.SetCurrentPosition(u16(7500), u16(2600))
	invoke(wcdef.CmdStopMotion, nil)
	if v, _ := srv.MatterRead(wcdef.AttrTargetPositionTiltPercent100ths); v != uint16(2600) {
		t.Errorf("tilt target after stop %v, want 2600 (the position)", v)
	}
	want := []axesCall{
		{"goto", u16(0), u16(0)},
		{"goto", u16(10000), u16(10000)},
		{"goto", nil, u16(2500)},
		{"goto", u16(7500), nil},
		{"stop", nil, nil},
	}
	if got := m.recorded(); !reflect.DeepEqual(got, want) {
		t.Errorf("Mover calls %+v, want %+v", got, want)
	}
}

// TestTilt_OnlyServerRefusesLiftCommand: GoToLiftPercentage on a tilt-only
// server is UNSUPPORTED_COMMAND, and GoToTiltPercentage on a lift-only one.
func TestTilt_OnlyServerRefusesLiftCommand(t *testing.T) {
	t.Parallel()
	tilt := cover.NewWindowCoveringServer(cover.Config{Type: uint8(wcdef.TypeTiltBlindTiltOnly), EndProductType: uint8(wcdef.EndProductTypeTiltOnlyInteriorBlind), FeatureMap: cover.ServedTiltFeatureMap})
	lift := cover.NewWindowCoveringServer(cover.Config{})
	for _, c := range []struct {
		srv *cover.WindowCoveringServer
		cmd uint32
		req any
	}{
		{tilt, wcdef.CmdGoToLiftPercentage, wcdef.GoToLiftPercentageRequest{LiftPercent100thsValue: 1}},
		{lift, wcdef.CmdGoToTiltPercentage, wcdef.GoToTiltPercentageRequest{TiltPercent100thsValue: 1}},
	} {
		_, err := c.srv.MatterInvoke(context.Background(), c.cmd, c.req)
		if sce, ok := errors.AsType[im.StatusCodeError](err); !ok || sce.MatterStatusCode() != im.StatusUnsupportedCommand {
			t.Errorf("command 0x%02X: %v, want UNSUPPORTED_COMMAND", c.cmd, err)
		}
	}
	if _, err := tilt.MatterInvoke(context.Background(), wcdef.CmdGoToTiltPercentage, wcdef.GoToTiltPercentageRequest{TiltPercent100thsValue: 12000}); err == nil {
		t.Error("tilt 12000 accepted, want CONSTRAINT_ERROR (element :76 max 10000)")
	}
}

// TestTilt_SelfContainedTravelAndGlobalStatus: without a Mover a tilt
// movement travels like a lift one, with its status in bits 4-5 and the
// global following lift while it moves, else tilt (matter.js
// #handleOperationalStatusChanging, WindowCoveringServer.ts:203-214).
func TestTilt_SelfContainedTravelAndGlobalStatus(t *testing.T) {
	t.Parallel()
	srv := cover.NewWindowCoveringServer(cover.Config{Type: uint8(wcdef.TypeTiltBlindLift), EndProductType: uint8(wcdef.EndProductTypeInteriorVenetianBlind), FeatureMap: cover.ServedLiftTiltFeatureMap, InitialPositionPercent100ths: 0, InitialTiltPositionPercent100ths: 10000, MoveStep: time.Hour})
	ctx := context.Background()
	if _, err := srv.MatterInvoke(ctx, wcdef.CmdGoToTiltPercentage, wcdef.GoToTiltPercentageRequest{TiltPercent100thsValue: 0}); err != nil {
		t.Fatal(err)
	}
	// tilt Opening (1) in bits 4-5, lift stopped, global follows tilt.
	if v, _ := srv.MatterRead(wcdef.AttrOperationalStatus); v != uint8(1<<4|1) {
		t.Fatalf("OperationalStatus %v, want 0x11 (tilt Opening, global Opening)", v)
	}
	if _, err := srv.MatterInvoke(ctx, wcdef.CmdGoToLiftPercentage, wcdef.GoToLiftPercentageRequest{LiftPercent100thsValue: 10000}); err != nil {
		t.Fatal(err)
	}
	// lift Closing (2) in bits 2-3 wins the global.
	if v, _ := srv.MatterRead(wcdef.AttrOperationalStatus); v != uint8(1<<4|2<<2|2) {
		t.Fatalf("OperationalStatus %v, want 0x1A (tilt Opening, lift Closing, global Closing)", v)
	}
	if _, err := srv.MatterInvoke(ctx, wcdef.CmdStopMotion, nil); err != nil {
		t.Fatal(err)
	}
	if v, _ := srv.MatterRead(wcdef.AttrOperationalStatus); v != uint8(0) {
		t.Fatalf("OperationalStatus after stop %v, want 0", v)
	}
	srv2 := cover.NewWindowCoveringServer(cover.Config{Type: uint8(wcdef.TypeTiltBlindTiltOnly), EndProductType: uint8(wcdef.EndProductTypeTiltOnlyInteriorBlind), FeatureMap: cover.ServedTiltFeatureMap, InitialTiltPositionPercent100ths: 10000})
	if _, err := srv2.MatterInvoke(ctx, wcdef.CmdUpOrOpen, nil); err != nil {
		t.Fatal(err)
	}
	if v, _ := srv2.MatterRead(wcdef.AttrCurrentPositionTiltPercent100ths); v != uint16(0) {
		t.Errorf("tilt-only UpOrOpen: current %v, want 0 (moves at once without a MoveStep)", v)
	}
}

// TestSetTargetPosition_StoresAndReportsWithoutTheMover: the inbound target
// setter (issue #76) records a host-inferred target per served axis,
// clamps above 10000, ignores an unserved axis and nil, reports through
// the change notifier and never calls the Mover or touches
// OperationalStatus.
func TestSetTargetPosition_StoresAndReportsWithoutTheMover(t *testing.T) {
	t.Parallel()
	m := &axesMover{}
	srv, err := cover.New(cover.Config{Mover: m, InitialPositionPercent100ths: 5000})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var reported []uint32
	unsub := srv.OnMatterAttributesChanged(func(ids []uint32) {
		mu.Lock()
		reported = append(reported, ids...)
		mu.Unlock()
	})
	defer unsub()
	srv.SetOperationalStatus(0b0000_0101) // opening, as the host reports it
	srv.SetTargetPosition(u16(0), u16(0)) // tilt ignored on a lift-only server
	srv.SetTargetPosition(u16(0), nil)    // unchanged: not reported
	srv.SetTargetPosition(nil, nil)
	srv.SetTargetPosition(u16(12000), nil) // clamped
	if v, _ := srv.MatterRead(wcdef.AttrTargetPositionLiftPercent100ths); v != uint16(10000) {
		t.Errorf("target %v, want 10000", v)
	}
	if v, _ := srv.MatterRead(wcdef.AttrOperationalStatus); v != uint8(0b0000_0101) {
		t.Errorf("OperationalStatus %v, want unchanged 0x05 (the host's)", v)
	}
	if n := len(m.recorded()); n != 0 {
		t.Errorf("Mover called %d times by the target setter, want 0", n)
	}
	mu.Lock()
	got := append([]uint32(nil), reported...)
	mu.Unlock()
	want := []uint32{clusterwire.WindowCoveringAttrOperationalStatus, wcdef.AttrTargetPositionLiftPercent100ths, wcdef.AttrTargetPositionLiftPercent100ths}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("reported %v, want %v", got, want)
	}
}

// settingMover reports a new target from inside GoTo and then fails: the
// overlap rule — the host's report, stored while the Mover ran, stands;
// the failed command does not restore its own target over it
// (BD-Matter-HostPort-OutsideTheSetterLock).
type settingMover struct {
	srv *cover.WindowCoveringServer
}

func (m *settingMover) GoTo(context.Context, *uint16, *uint16) error {
	m.srv.SetTargetPosition(u16(4242), nil)
	return errors.New("drive refused")
}

func (m *settingMover) Stop(context.Context) error { return nil }

func TestSetTargetPosition_DuringAFailingMoverStands(t *testing.T) {
	t.Parallel()
	m := &settingMover{}
	srv, err := cover.New(cover.Config{Mover: m, InitialPositionPercent100ths: 5000})
	if err != nil {
		t.Fatal(err)
	}
	m.srv = srv
	if _, err := srv.MatterInvoke(context.Background(), wcdef.CmdUpOrOpen, nil); err == nil {
		t.Fatal("UpOrOpen succeeded, want the Mover's failure")
	}
	if v, _ := srv.MatterRead(wcdef.AttrTargetPositionLiftPercent100ths); v != uint16(4242) {
		t.Errorf("target %v, want 4242 (the host's report during the Mover)", v)
	}
	// Without an overlap the failed command restores the previous target.
	m2 := &axesMover{err: errors.New("drive refused")}
	srv2, _ := cover.New(cover.Config{Mover: m2, InitialPositionPercent100ths: 5000})
	if _, err := srv2.MatterInvoke(context.Background(), wcdef.CmdUpOrOpen, nil); err == nil {
		t.Fatal("UpOrOpen succeeded, want the Mover's failure")
	}
	if v, _ := srv2.MatterRead(wcdef.AttrTargetPositionLiftPercent100ths); v != uint16(5000) {
		t.Errorf("target %v, want 5000 restored", v)
	}
}

// TestTilt_GoToTiltPercentageAcceptsEveryPayloadShape: the generated and
// the wire request, by value and pointer, the bridge's tag map and the
// bare value; a nil pointer is refused. A failing Mover restores the tilt
// target it moved.
func TestTilt_GoToTiltPercentageAcceptsEveryPayloadShape(t *testing.T) {
	t.Parallel()
	srv := cover.NewWindowCoveringServer(cover.Config{Type: uint8(wcdef.TypeTiltBlindTiltOnly), EndProductType: uint8(wcdef.EndProductTypeTiltOnlyInteriorBlind), FeatureMap: cover.ServedTiltFeatureMap})
	ctx := context.Background()
	for i, req := range []any{
		wcdef.GoToTiltPercentageRequest{TiltPercent100thsValue: 100},
		&wcdef.GoToTiltPercentageRequest{TiltPercent100thsValue: 200},
		clusterwire.GoToTiltPercentageRequest{TiltPercent100thsValue: 300},  //nolint:staticcheck // SA1019: the deprecated in-process form, removal in v0.12.0
		&clusterwire.GoToTiltPercentageRequest{TiltPercent100thsValue: 400}, //nolint:staticcheck // SA1019: the deprecated in-process form, removal in v0.12.0
		map[uint8]any{0: uint64(500)},
		uint16(600),
	} {
		if _, err := srv.MatterInvoke(ctx, wcdef.CmdGoToTiltPercentage, req); err != nil {
			t.Fatalf("shape %d (%T): %v", i, req, err)
		}
		want := uint16((i + 1) * 100)
		if v, _ := srv.MatterRead(wcdef.AttrTargetPositionTiltPercent100ths); v != want {
			t.Errorf("shape %d: tilt target %v, want %d", i, v, want)
		}
	}
	for _, req := range []any{(*wcdef.GoToTiltPercentageRequest)(nil), (*clusterwire.GoToTiltPercentageRequest)(nil)} { //nolint:staticcheck // SA1019: the deprecated in-process form, removal in v0.12.0
		if _, err := srv.MatterInvoke(ctx, wcdef.CmdGoToTiltPercentage, req); err == nil {
			t.Errorf("%T accepted", req)
		}
	}
	m := &axesMover{err: errors.New("drive refused")}
	moved, _ := cover.New(cover.Config{Type: uint8(wcdef.TypeTiltBlindLift), EndProductType: uint8(wcdef.EndProductTypeInteriorVenetianBlind), FeatureMap: cover.ServedLiftTiltFeatureMap, InitialTiltPositionPercent100ths: 5000, Mover: m})
	if _, err := moved.MatterInvoke(ctx, wcdef.CmdGoToTiltPercentage, uint16(1)); err == nil {
		t.Fatal("GoToTiltPercentage succeeded, want the Mover's failure")
	}
	if v, _ := moved.MatterRead(wcdef.AttrTargetPositionTiltPercent100ths); v != uint16(5000) {
		t.Errorf("tilt target %v, want 5000 restored", v)
	}
}
