// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package levelcontrol_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/levelcontrol"
	"github.com/SukramJ/go-fabric/cluster/spec"
	lvldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	"github.com/SukramJ/go-fabric/contract"
)

// typedOnOff is the endpoint's On/Off state a held server couples to: it
// reports its changes, as onoff.Server does.
type typedOnOff struct {
	mu      sync.Mutex
	on      bool
	changes cluster.AttributeChanges
}

func (o *typedOnOff) OnOff() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.on
}

func (o *typedOnOff) SetOnOff(_ context.Context, on bool) error {
	o.mu.Lock()
	changed := o.on != on
	o.on = on
	o.mu.Unlock()
	if changed {
		o.changes.Notify(0x0000)
	}
	return nil
}

func (o *typedOnOff) OnMatterAttributesChanged(cb func([]uint32)) func() {
	return o.changes.OnMatterAttributesChanged(cb)
}

func heldServer(t *testing.T, onOff *typedOnOff) (typed *lvldef.Server, srv *levelcontrol.Server) {
	t.Helper()
	cfg := levelcontrol.HeldConfig{Features: levelcontrol.FeatureOnOff | levelcontrol.FeatureLighting, Clock: clocktest.New(time.Unix(0, 0))}
	if onOff != nil {
		cfg.OnOff = onOff
	}
	srv, err := levelcontrol.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return lvldef.Wrap(srv), srv
}

func levelOf(st lvldef.State) int {
	if st.CurrentLevel == nil {
		return -1
	}
	return int(*st.CurrentLevel)
}

// TestHeldTypedSetGetOnChanged sets the held server's state through the
// typed server and reads it back; the change is reported.
func TestHeldTypedSetGetOnChanged(t *testing.T) {
	t.Parallel()
	s, _ := heldServer(t, nil)
	var changed [][]uint32
	defer s.OnChanged(func(_ lvldef.State, change contract.Change) { ids := change.Attributes; changed = append(changed, ids) })()

	if err := s.Set(lvldef.Patch{CurrentLevel: ptr(uint8(100)), OnLevel: ptr(uint8(50))}); err != nil {
		t.Fatal(err)
	}
	st := s.Get()
	if levelOf(st) != 100 || st.OnLevel == nil || *st.OnLevel != 50 || st.MinLevel != 1 || st.MaxLevel != 254 {
		t.Errorf("state %+v", st)
	}
	var all []uint32
	for _, ids := range changed {
		all = append(all, ids...)
	}
	if !slices.Contains(all, lvldef.AttrCurrentLevel) || !slices.Contains(all, lvldef.AttrOnLevel) {
		t.Errorf("changes %v", changed)
	}
	// MinLevel is fixed by the feature selection; RemainingTime derived.
	if err := s.Set(lvldef.Patch{MinLevel: ptr(uint8(5))}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("MinLevel 5: %v", err)
	}
	if err := s.Set(lvldef.Patch{RemainingTime: ptr(uint16(5))}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("RemainingTime: %v", err)
	}
	// All or nothing: a refused value leaves the other one unset.
	if err := s.Set(lvldef.Patch{CurrentLevel: ptr(uint8(30)), MinLevel: ptr(uint8(5))}); err == nil || levelOf(s.Get()) != 100 {
		t.Errorf("a refused patch: %v, level %d", err, levelOf(s.Get()))
	}
}

// TestHeldServerRunsTheCommands runs MoveToLevel (generated request, value
// and pointer) and MoveToLevelWithOnOff on the held server: the level moves
// at once (matter.js's default, no managed transitions) and the "with
// On/Off" form switches the endpoint on, which applies OnLevel first.
func TestHeldServerRunsTheCommands(t *testing.T) {
	t.Parallel()
	onOff := &typedOnOff{on: true}
	s, srv := heldServer(t, onOff)
	ctx := context.Background()
	if _, err := srv.MatterInvoke(ctx, lvldef.CmdMoveToLevel, lvldef.MoveToLevelRequest{Level: 200}); err != nil {
		t.Fatal(err)
	}
	if levelOf(s.Get()) != 200 {
		t.Fatalf("MoveToLevel: level %d", levelOf(s.Get()))
	}
	if _, err := srv.MatterInvoke(ctx, lvldef.CmdMoveToLevel, &lvldef.MoveToLevelRequest{Level: 120}); err != nil || levelOf(s.Get()) != 120 {
		t.Fatalf("MoveToLevel by pointer: %v, level %d", err, levelOf(s.Get()))
	}

	// Off, OnLevel 80: MoveToLevelWithOnOff to 150 switches on (OnLevel
	// applied by the coupling) and ends at 150.
	_ = onOff.SetOnOff(ctx, false)
	if err := s.Set(lvldef.Patch{OnLevel: ptr(uint8(80))}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.MatterInvoke(ctx, lvldef.CmdMoveToLevelWithOnOff, lvldef.MoveToLevelWithOnOffRequest{Level: 150}); err != nil {
		t.Fatal(err)
	}
	if !onOff.OnOff() || levelOf(s.Get()) != 150 {
		t.Errorf("MoveToLevelWithOnOff: on %t, level %d", onOff.OnOff(), levelOf(s.Get()))
	}
}

// TestHeldServerFollowsOnOff is matter.js handleOnOffChange: the endpoint
// turning on moves CurrentLevel to a non-null OnLevel; a null OnLevel
// leaves it.
func TestHeldServerFollowsOnOff(t *testing.T) {
	t.Parallel()
	onOff := &typedOnOff{}
	s, _ := heldServer(t, onOff)
	ctx := context.Background()
	if err := s.Set(lvldef.Patch{CurrentLevel: ptr(uint8(10))}); err != nil {
		t.Fatal(err)
	}
	_ = onOff.SetOnOff(ctx, true)
	if levelOf(s.Get()) != 10 {
		t.Errorf("null OnLevel: level %d", levelOf(s.Get()))
	}
	_ = onOff.SetOnOff(ctx, false)
	if err := s.Set(lvldef.Patch{OnLevel: ptr(uint8(90))}); err != nil {
		t.Fatal(err)
	}
	_ = onOff.SetOnOff(ctx, true)
	if levelOf(s.Get()) != 90 {
		t.Errorf("OnLevel 90: level %d", levelOf(s.Get()))
	}
}

// TestHeldTypedHandleEmitOnWrite: a host handler replaces the server's
// own MoveToLevel (the level stays) and nil hands it back; LevelControl
// has no event; OnWrite vetoes a controller's write.
func TestHeldTypedHandleEmitOnWrite(t *testing.T) {
	t.Parallel()
	s, srv := heldServer(t, nil)
	ctx := context.Background()
	var got []lvldef.MoveToLevelRequest
	if err := s.HandleMoveToLevel(func(_ context.Context, req lvldef.MoveToLevelRequest) (struct{}, error) {
		got = append(got, req)
		return struct{}{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.MatterInvoke(ctx, lvldef.CmdMoveToLevel, lvldef.MoveToLevelRequest{Level: 77}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Level != 77 || levelOf(s.Get()) != -1 {
		t.Errorf("host handler saw %v, level %d", got, levelOf(s.Get()))
	}
	if err := s.HandleMoveToLevel(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.MatterInvoke(ctx, lvldef.CmdMoveToLevel, lvldef.MoveToLevelRequest{Level: 77}); err != nil || levelOf(s.Get()) != 77 {
		t.Errorf("handed back: %v, level %d", err, levelOf(s.Get()))
	}
	if err := srv.Handle(0x7F, nil); !errors.Is(err, spec.ErrNotAccepted) {
		t.Errorf("an unaccepted command: %v", err)
	}
	if err := spec.EmitOn(srv, 1, 0x00, nil); !errors.Is(err, spec.ErrNotEmitted) {
		t.Errorf("an event LevelControl does not define: %v", err)
	}

	refusal := errors.New("not now")
	if err := s.OnWrite(func(_ context.Context, next lvldef.State, attrID uint32) error {
		if attrID == lvldef.AttrOnLevel && next.OnLevel != nil && *next.OnLevel > 100 {
			return refusal
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := srv.MatterWrite(ctx, lvldef.AttrOnLevel, uint64(150)); !errors.Is(err, refusal) {
		t.Errorf("a vetoed write: %v", err)
	}
	if err := srv.MatterWrite(ctx, lvldef.AttrOnLevel, uint64(60)); err != nil {
		t.Errorf("an allowed write: %v", err)
	}
	if st := s.Get(); st.OnLevel == nil || *st.OnLevel != 60 {
		t.Errorf("OnLevel %v", st.OnLevel)
	}
}

// TestPortServerDoesNotHoldCurrentLevel: over a host's LevelSource the
// level is the port's, so the host's Set of CurrentLevel is refused.
func TestPortServerDoesNotHoldCurrentLevel(t *testing.T) {
	t.Parallel()
	s := lvldef.Wrap(levelcontrol.NewServer(levelcontrol.Config{Source: &fakeHost{}}))
	if err := s.Set(lvldef.Patch{CurrentLevel: ptr(uint8(3))}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("CurrentLevel over a port: %v", err)
	}
}

func ptr[T any](v T) *T { return &v }
