// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package valve_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	valvedef "github.com/SukramJ/go-fabric/cluster/spec/valveconfigurationandcontrol"
	"github.com/SukramJ/go-fabric/cluster/valve"
	"github.com/SukramJ/go-fabric/im"
)

// The Level and TimeSync rules, ported by meaning from connectedhomeip
// src/app/clusters/valve-configuration-and-control-server/tests/TestValveConfigurationAndControlCluster.cpp
// at 6170af84 — the server is ahead of matter.js, whose
// ValveConfigurationAndControlServer.ts:14 is the empty subclass
// (notes/parity/by_design.md BD-chip-Valve-RulesInServer).

// levelHost is a valve with a level and a default open level.
type levelHost struct {
	*fakeHost
	current, target           uint8
	currentKnown, targetKnown bool
	defaultLevel              uint8
	levelSets                 []uint8
}

var (
	_ valve.LevelSource            = (*levelHost)(nil)
	_ valve.DefaultOpenLevelSource = (*levelHost)(nil)
)

func (h *levelHost) CurrentLevel() (uint8, bool) { return h.current, h.currentKnown }
func (h *levelHost) TargetLevel() (uint8, bool)  { return h.target, h.targetKnown }
func (h *levelHost) DefaultOpenLevel() uint8     { return h.defaultLevel }
func (h *levelHost) SetDefaultOpenLevel(_ context.Context, p uint8) error {
	h.levelSets = append(h.levelSets, p)
	h.defaultLevel = p
	return nil
}

func newLevelHost() *levelHost {
	return &levelHost{fakeHost: fullyObservedHost(), current: 0, currentKnown: true, defaultLevel: 100}
}

func status(err error) im.StatusCode {
	if err == nil {
		return im.StatusSuccess
	}
	if sce, ok := errors.AsType[im.StatusCodeError](err); ok {
		return sce.MatterStatusCode()
	}
	return im.StatusFailure
}

// TestValveSelectionRefusalNamesTheInterface: a declared LVL or
// DefaultOpenLevel the source or the configuration cannot carry is
// ErrSelection naming what is missing. TS is not refused without a time:
// the node hands its time as the server joins the topology
// (TestValveTimeSyncTakesTheNodesTime).
func TestValveSelectionRefusalNamesTheInterface(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		cfg  valve.Config
		want string
	}{
		{valve.Config{Source: fullyObservedHost(), Features: valve.FeatureLevel}, "LevelSource"},
		{valve.Config{Source: fullyObservedHost(), Attributes: []uint32{valvedef.AttrDefaultOpenLevel}, Features: valve.FeatureLevel}, "LevelSource"},
		{valve.Config{Source: newLevelHost(), Features: valve.FeatureLevel, Attributes: []uint32{valvedef.AttrLevelStep}, LevelStep: 51}, "LevelStep"},
		{valve.Config{Source: newLevelHost(), Features: 1 << 2}, "undefined"},
		{valve.Config{Source: newLevelHost(), Attributes: []uint32{valvedef.AttrLevelStep}}, "valve:"},
	} {
		if _, err := valve.New(c.cfg); !errors.Is(err, valve.ErrSelection) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: %v, want ErrSelection naming %s", c.cfg, err, c.want)
		}
	}
	defer func() {
		if recover() == nil {
			t.Error("NewServer accepted a selection New refuses")
		}
	}()
	valve.NewServer(valve.Config{Source: fullyObservedHost(), Features: valve.FeatureLevel})
}

// TestValveLevelServerMatchesTheGeneratedDefinition holds the featured
// selections against matter.js's element (spectest.CheckServer), as
// chip's AttributeTestLevelStep, AttributeTestDefaultOpenLevel,
// AttributeTestTimeSync and AttributeTestAll list them.
func TestValveLevelServerMatchesTheGeneratedDefinition(t *testing.T) {
	t.Parallel()
	for _, cfg := range []valve.Config{
		{Source: newLevelHost(), Features: valve.FeatureLevel},
		{Source: newLevelHost(), Features: valve.FeatureTimeSync},
		{
			Source: newLevelHost(), Features: valve.FeatureLevel | valve.FeatureTimeSync,
			Attributes: []uint32{valvedef.AttrDefaultOpenLevel, valvedef.AttrLevelStep}, LevelStep: 10,
		},
	} {
		srv := valve.NewServer(cfg)
		spectest.CheckServer(t, srv, valvedef.Definition, cfg.Features)
		if srv.FeatureMap() != cfg.Features {
			t.Errorf("FeatureMap = 0x%X, want 0x%X", srv.FeatureMap(), cfg.Features)
		}
	}
}

// TestValveReadLevelChip ports ReadAttributeTestLevel and
// ReadAttributeTestDefaultOpenLevel: the levels are the source's, null
// while unknown; DefaultOpenLevel and LevelStep are served where declared.
func TestValveReadLevelChip(t *testing.T) {
	t.Parallel()
	h := newLevelHost()
	h.current, h.targetKnown = 40, false
	srv := valve.NewServer(valve.Config{
		Source: h, Features: valve.FeatureLevel,
		Attributes: []uint32{valvedef.AttrDefaultOpenLevel, valvedef.AttrLevelStep}, LevelStep: 20,
	})
	for id, want := range map[uint32]any{
		valvedef.AttrCurrentLevel: uint8(40), valvedef.AttrTargetLevel: nil,
		valvedef.AttrDefaultOpenLevel: uint8(100), valvedef.AttrLevelStep: uint8(20),
	} {
		if v, ok := srv.MatterRead(id); !ok || v != want {
			t.Errorf("attribute 0x%X = %v, %v; want %v", id, v, ok, want)
		}
	}
	if got := srv.MatterReportable(); !slices.Contains(got, valvedef.AttrCurrentLevel) || !slices.Contains(got, valvedef.AttrTargetLevel) {
		t.Errorf("MatterReportable = %v, want the levels", got)
	}
	plain := valve.NewServer(valve.Config{Source: fullyObservedHost()})
	if _, ok := plain.MatterRead(valvedef.AttrCurrentLevel); ok {
		t.Error("CurrentLevel answered without the Level feature")
	}
}

// TestValveWriteDefaultOpenLevelChip ports
// WriteAttributeTestDefaultOpenLevelOutOfRange and the LevelStep check of
// the write (ValveConfigurationAndControlCluster.cpp:175-192).
func TestValveWriteDefaultOpenLevelChip(t *testing.T) {
	t.Parallel()
	h := newLevelHost()
	srv := valve.NewServer(valve.Config{
		Source: h, Features: valve.FeatureLevel,
		Attributes: []uint32{valvedef.AttrDefaultOpenLevel, valvedef.AttrLevelStep}, LevelStep: 20,
	})
	ctx := context.Background()
	for _, c := range []struct {
		value uint64
		want  im.StatusCode
	}{{0, im.StatusConstraintError}, {101, im.StatusConstraintError}, {30, im.StatusConstraintError}, {40, im.StatusSuccess}, {100, im.StatusSuccess}} {
		if got := status(srv.MatterWrite(ctx, valvedef.AttrDefaultOpenLevel, c.value)); got != c.want {
			t.Errorf("DefaultOpenLevel %d = %v, want %v", c.value, got, c.want)
		}
	}
	if !slices.Equal(h.levelSets, []uint8{40, 100}) {
		t.Errorf("SetDefaultOpenLevel calls = %v, want [40 100]", h.levelSets)
	}
	// Unchanged: a no-op success, no call (chip :182).
	if err := srv.MatterWrite(ctx, valvedef.AttrDefaultOpenLevel, uint64(100)); err != nil || len(h.levelSets) != 2 {
		t.Errorf("unchanged write: %v, calls %v", err, h.levelSets)
	}
}

// TestValveOpenLevelChip ports OpenCommandFieldsValidation and the Level
// rules of HandleOpenCommand (:312-341): an absent TargetLevel opens to
// DefaultOpenLevel where served, else 100; a TargetLevel off the LevelStep
// grid is CONSTRAINT_ERROR; without LVL a TargetLevel is refused.
func TestValveOpenLevelChip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	open := func(srv *valve.Server, req valve.OpenRequest) error {
		_, err := srv.MatterInvoke(ctx, valve.CmdOpen, req)
		return err
	}
	lvl := func(v uint8) *uint8 { return &v }

	bare := newLevelHost()
	srv := valve.NewServer(valve.Config{Source: bare, Features: valve.FeatureLevel})
	if err := open(srv, valve.OpenRequest{}); err != nil || *bare.openCalls[0].TargetLevel != 100 {
		t.Errorf("Open without TargetLevel or DefaultOpenLevel: %v, %+v; want level 100", err, bare.openCalls)
	}

	h := newLevelHost()
	h.defaultLevel = 60
	stepped := valve.NewServer(valve.Config{
		Source: h, Features: valve.FeatureLevel,
		Attributes: []uint32{valvedef.AttrDefaultOpenLevel, valvedef.AttrLevelStep}, LevelStep: 20,
	})
	if err := open(stepped, valve.OpenRequest{}); err != nil || *h.openCalls[0].TargetLevel != 60 {
		t.Errorf("Open without TargetLevel: %v, %+v; want DefaultOpenLevel 60", err, h.openCalls)
	}
	if got := status(open(stepped, valve.OpenRequest{TargetLevel: lvl(30)})); got != im.StatusConstraintError || len(h.openCalls) != 1 {
		t.Errorf("TargetLevel 30 off a 20 step = %v, calls %d; want CONSTRAINT_ERROR and no call", got, len(h.openCalls))
	}
	for _, l := range []uint8{40, 100} {
		if err := open(stepped, valve.OpenRequest{TargetLevel: lvl(l)}); err != nil {
			t.Errorf("TargetLevel %d: %v", l, err)
		}
	}
	// The tag map's TargetLevel: "1 to 100" (element :62).
	if _, err := stepped.MatterInvoke(ctx, valve.CmdOpen, map[uint8]any{1: uint64(0)}); status(err) != im.StatusConstraintError {
		t.Errorf("tag-map TargetLevel 0 = %v, want CONSTRAINT_ERROR", err)
	}
	if _, err := stepped.MatterInvoke(ctx, valve.CmdOpen, map[uint8]any{1: uint64(80)}); err != nil || *h.openCalls[len(h.openCalls)-1].TargetLevel != 80 {
		t.Errorf("tag-map TargetLevel 80: %v", err)
	}

	plain := valve.NewServer(valve.Config{Source: fullyObservedHost()})
	if got := status(open(plain, valve.OpenRequest{TargetLevel: lvl(50)})); got != im.StatusConstraintError {
		t.Errorf("TargetLevel without LVL = %v, want CONSTRAINT_ERROR", got)
	}
}

// TestValveAutoCloseTimeChip ports SetAutoCloseTimeNoIntegerOverflow and
// the TimeSync rules of OpenValve/CloseValve (:243-247, :350-354,
// :391-413): an Open sets AutoCloseTime to the Matter time plus the
// opening's duration (the request's, else DefaultOpenDuration) in
// epoch-us, without overflow; a null duration or an unsynchronized time
// leaves it null; a Close clears it.
func TestValveAutoCloseTimeChip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const now = uint32(1700000000 - 946684800)
	synced := true
	h := fullyObservedHost()
	srv := valve.NewServer(valve.Config{Source: h, Features: valve.FeatureTimeSync})
	srv.AttachMatterTime(func() (uint32, bool) { return now, synced })
	read := func() any { v, _ := srv.MatterRead(valvedef.AttrAutoCloseTime); return v }
	if v := read(); v != nil {
		t.Errorf("AutoCloseTime before an Open = %v, want null", v)
	}
	if _, err := srv.MatterInvoke(ctx, valve.CmdOpen, valve.OpenRequest{OpenDuration: &spec.Nullable[uint32]{Value: 5000}}); err != nil {
		t.Fatal(err)
	}
	if want := (uint64(now) + 5000) * 1_000_000; read() != want {
		t.Errorf("AutoCloseTime = %v, want %d (no uint32 overflow)", read(), want)
	}
	// The valve closed by itself: RemainingDuration null → null.
	h.remainingKnown = false
	if v := read(); v != nil {
		t.Errorf("AutoCloseTime after the self-close = %v, want null", v)
	}
	h.remainingKnown = true
	if _, err := srv.MatterInvoke(ctx, valve.CmdOpen, valve.OpenRequest{}); err != nil {
		t.Fatal(err)
	}
	if want := (uint64(now) + 300) * 1_000_000; read() != want {
		t.Errorf("AutoCloseTime from DefaultOpenDuration = %v, want %d", read(), want)
	}
	if _, err := srv.MatterInvoke(ctx, valve.CmdClose, nil); err != nil || read() != nil {
		t.Errorf("AutoCloseTime after Close = %v (%v), want null", read(), err)
	}
	if _, err := srv.MatterInvoke(ctx, valve.CmdOpen, valve.OpenRequest{OpenDuration: &spec.Nullable[uint32]{Null: true}}); err != nil || read() != nil {
		t.Errorf("AutoCloseTime for an indefinite opening = %v (%v), want null", read(), err)
	}
	synced = false
	if _, err := srv.MatterInvoke(ctx, valve.CmdOpen, valve.OpenRequest{OpenDuration: &spec.Nullable[uint32]{Value: 60}}); err != nil || read() != nil {
		t.Errorf("AutoCloseTime without a synchronized time = %v (%v), want null", read(), err)
	}
	h.openErr = errors.New("jammed")
	synced = true
	if _, err := srv.MatterInvoke(ctx, valve.CmdOpen, valve.OpenRequest{OpenDuration: &spec.Nullable[uint32]{Value: 60}}); err == nil || read() != nil {
		t.Errorf("a refused Open set AutoCloseTime %v", read())
	}
	if got := srv.MatterReportable(); !slices.Contains(got, valvedef.AttrAutoCloseTime) {
		t.Errorf("MatterReportable = %v, want AutoCloseTime", got)
	}
}

// TestValveTimeSyncTakesTheNodesTime: a TS valve without a time leaves
// AutoCloseTime null, as connectedhomeip's SetAutoCloseTime does while
// the node's TimeSynchronization has no time
// (ValveConfigurationAndControlCluster.cpp:391-400, CodegenIntegration.cpp:
// 46-58 at 6170af84); AttachMatterTime is its one input of the time, and
// a nil attach changes nothing.
func TestValveTimeSyncTakesTheNodesTime(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := fullyObservedHost()
	srv, err := valve.New(valve.Config{Source: h, Features: valve.FeatureTimeSync})
	if err != nil {
		t.Fatalf("TS without a time: %v, want a server", err)
	}
	read := func() any { v, _ := srv.MatterRead(valvedef.AttrAutoCloseTime); return v }
	open := func() {
		t.Helper()
		if _, err := srv.MatterInvoke(ctx, valve.CmdOpen, valve.OpenRequest{OpenDuration: &spec.Nullable[uint32]{Value: 60}}); err != nil {
			t.Fatal(err)
		}
	}
	open()
	if v := read(); v != nil {
		t.Errorf("AutoCloseTime without a time = %v, want null", v)
	}
	srv.AttachMatterTime(nil)
	open()
	if v := read(); v != nil {
		t.Errorf("AutoCloseTime after a nil attach = %v, want null", v)
	}
	srv.AttachMatterTime(func() (uint32, bool) { return 1000, true })
	open()
	if want := uint64(1060) * 1_000_000; read() != want {
		t.Errorf("AutoCloseTime on the node's time = %v, want %d", read(), want)
	}
	srv.AttachMatterTime(func() (uint32, bool) { return 2000, true })
	open()
	if want := uint64(2060) * 1_000_000; read() != want {
		t.Errorf("AutoCloseTime after a second attach = %v, want %d", read(), want)
	}
}

// TestValveAutoCloseTimeFollowsTheMatterTime ports chip's UpdateAutoCloseTime
// (ValveConfigurationAndControlCluster.cpp:415-423 at 6170af84), which the
// reference application's UTCTimeAvailabilityChanged calls
// (examples/all-clusters-app/linux/ValveControlDelegate.cpp:70-73): when
// the Matter time moves, AutoCloseTime becomes the new time plus
// RemainingDuration — also for an opening that began without a time —
// and stays as it is without a time, while the valve does not count down,
// or without TS.
func TestValveAutoCloseTimeFollowsTheMatterTime(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := fullyObservedHost() // RemainingDuration 120
	srv := valve.NewServer(valve.Config{Source: h, Features: valve.FeatureTimeSync})
	read := func() any { v, _ := srv.MatterRead(valvedef.AttrAutoCloseTime); return v }
	if _, err := srv.MatterInvoke(ctx, valve.CmdOpen, valve.OpenRequest{OpenDuration: &spec.Nullable[uint32]{Value: 600}}); err != nil {
		t.Fatal(err)
	}
	srv.MatterTimeChanged() // no time attached
	if v := read(); v != nil {
		t.Fatalf("AutoCloseTime without a time = %v, want null", v)
	}
	now, synced := uint32(1000), false
	srv.AttachMatterTime(func() (uint32, bool) { return now, synced })
	srv.MatterTimeChanged() // not synchronized
	if v := read(); v != nil {
		t.Fatalf("AutoCloseTime without a synchronized time = %v, want null", v)
	}
	synced = true
	srv.MatterTimeChanged()
	if want := uint64(1120) * 1_000_000; read() != want {
		t.Errorf("AutoCloseTime once the time arrives = %v, want %d", read(), want)
	}
	now = 5000
	srv.MatterTimeChanged()
	if want := uint64(5120) * 1_000_000; read() != want {
		t.Errorf("AutoCloseTime after the time moved = %v, want %d", read(), want)
	}
	h.remaining = 0
	now = 9000
	srv.MatterTimeChanged()
	h.remaining = 120
	if want := uint64(5120) * 1_000_000; read() != want {
		t.Errorf("AutoCloseTime moved while the valve did not count down: %v, want %d", read(), want)
	}
	plain := valve.NewServer(valve.Config{Source: fullyObservedHost()})
	plain.AttachMatterTime(func() (uint32, bool) { return 1000, true })
	plain.MatterTimeChanged()
	if v, ok := plain.MatterRead(valvedef.AttrAutoCloseTime); ok && v != nil {
		t.Errorf("AutoCloseTime without TS = %v", v)
	}
}
