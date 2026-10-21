// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package levelcontrol_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	levelcontroldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

func ptr[T any](v T) *T { return &v }

// typedServer is a LevelControl server with MinLevel 10 and MaxLevel 200,
// MoveToLevel left to the typed handler.
func typedServer(t *testing.T) *levelcontroldef.Server {
	t.Helper()
	srv, err := spec.NewServer(levelcontroldef.Definition, spec.Options{
		Features:   uint32(levelcontroldef.FeatureOnOff),
		Attributes: []uint32{levelcontroldef.AttrMinLevel, levelcontroldef.AttrMaxLevel},
	}, spec.ServerConfig{
		Initial:       map[uint32]any{levelcontroldef.AttrMinLevel: uint8(10), levelcontroldef.AttrMaxLevel: uint8(200)},
		Unimplemented: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return levelcontroldef.Wrap(srv)
}

func TestTypedSetGetOnChanged(t *testing.T) {
	t.Parallel()
	s := typedServer(t)
	var changes [][]uint32
	var states []levelcontroldef.State
	unsubscribe := s.OnChanged(func(st levelcontroldef.State, changed []uint32) {
		states = append(states, st)
		changes = append(changes, changed)
	})
	defer unsubscribe()

	if err := s.Set(levelcontroldef.Patch{CurrentLevel: ptr(uint8(128)), OnLevel: ptr(uint8(100))}); err != nil {
		t.Fatal(err)
	}
	st := s.Get()
	if st.CurrentLevel == nil || *st.CurrentLevel != 128 || st.OnLevel == nil || *st.OnLevel != 100 || st.MinLevel != 10 || st.MaxLevel != 200 {
		t.Errorf("state %+v", st)
	}
	if len(changes) != 1 || !slices.Equal(changes[0], []uint32{levelcontroldef.AttrCurrentLevel, levelcontroldef.AttrOnLevel}) {
		t.Fatalf("changes %v", changes)
	}
	if !reflect.DeepEqual(states[0], st) {
		t.Errorf("reported %+v, Get %+v", states[0], st)
	}

	// OnLevel outside "minLevel to maxLevel": neither value is stored.
	if err := s.Set(levelcontroldef.Patch{CurrentLevel: ptr(uint8(50)), OnLevel: ptr(uint8(250))}); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("a patch outside the constraint: %v", err)
	}
	if got := s.Get(); *got.CurrentLevel != 128 || *got.OnLevel != 100 || len(changes) != 1 {
		t.Errorf("a refused patch stored %+v (%d changes)", got, len(changes))
	}

	if err := s.Set(levelcontroldef.Patch{Null: []uint32{levelcontroldef.AttrOnLevel}}); err != nil {
		t.Fatal(err)
	}
	if got := s.Get(); got.OnLevel != nil {
		t.Errorf("OnLevel after the null patch: %d", *got.OnLevel)
	}
}

func TestTypedHandleMoveToLevel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := typedServer(t)
	if s.Underlying().(*spec.Server).Accepts(levelcontroldef.CmdMoveToLevel) {
		t.Fatal("MoveToLevel accepted before its handler")
	}
	refusal := spec.Errorf(im.StatusConstraintError, "no")
	var got []levelcontroldef.MoveToLevelRequest
	if err := s.HandleMoveToLevel(func(_ context.Context, req levelcontroldef.MoveToLevelRequest) (struct{}, error) {
		got = append(got, req)
		if req.Level > 200 {
			return struct{}{}, refusal
		}
		return struct{}{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if !s.Underlying().(*spec.Server).Accepts(levelcontroldef.CmdMoveToLevel) {
		t.Fatal("MoveToLevel not accepted with its handler")
	}

	// The request as the bridge hands it over: decoded by the definition.
	want := levelcontroldef.MoveToLevelRequest{Level: 42, TransitionTime: spec.Nullable[uint16]{Value: 10}, OptionsMask: 1, OptionsOverride: 1}
	enc := tlv.NewEncoder()
	want.EncodeTLV(enc, tlv.AnonymousTag())
	b, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	n, err := spec.ReadElement(tlv.NewDecoder(b))
	if err != nil {
		t.Fatal(err)
	}
	fields, err := levelcontroldef.Definition.Command(levelcontroldef.CmdMoveToLevel, spec.Request).Decode(n)
	if err != nil {
		t.Fatal(err)
	}
	if resp, err := s.Underlying().MatterInvoke(ctx, levelcontroldef.CmdMoveToLevel, fields); resp != nil || err != nil {
		t.Errorf("MoveToLevel answered %#v, %v", resp, err)
	}
	if len(got) != 1 || got[0] != want {
		t.Errorf("the handler received %+v, want %+v", got, want)
	}
	if _, err := s.Underlying().MatterInvoke(ctx, levelcontroldef.CmdMoveToLevel, levelcontroldef.MoveToLevelRequest{Level: 250}); !errors.Is(err, refusal) {
		t.Errorf("the handler's refusal: %v", err)
	}

	if err := s.HandleMoveToLevel(nil); err != nil {
		t.Fatal(err)
	}
	if s.Underlying().(*spec.Server).Accepts(levelcontroldef.CmdMoveToLevel) {
		t.Error("MoveToLevel accepted after its handler was removed")
	}
}

func TestTypedOnWrite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := typedServer(t)
	refusal := errors.New("the dimmer is locked")
	var next []levelcontroldef.State
	if err := s.OnWrite(func(_ context.Context, st levelcontroldef.State, attrID uint32) error {
		next = append(next, st)
		if attrID == levelcontroldef.AttrOnLevel && *st.OnLevel > 150 {
			return refusal
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Underlying().MatterWrite(ctx, levelcontroldef.AttrOnLevel, uint64(160)); !errors.Is(err, refusal) {
		t.Fatalf("a vetoed write: %v", err)
	}
	// A plain veto carries no status: the dispatcher answers FAILURE.
	var sce im.StatusCodeError
	if err := s.Underlying().MatterWrite(ctx, levelcontroldef.AttrOnLevel, uint64(160)); errors.As(err, &sce) {
		t.Errorf("a plain veto carries status %v", sce.MatterStatusCode())
	}
	if got := s.Get(); got.OnLevel != nil {
		t.Errorf("a vetoed write stored %d", *got.OnLevel)
	}
	if err := s.Underlying().MatterWrite(ctx, levelcontroldef.AttrOnLevel, uint64(120)); err != nil {
		t.Fatal(err)
	}
	if got := s.Get(); got.OnLevel == nil || *got.OnLevel != 120 {
		t.Errorf("an accepted write: %+v", got)
	}
	if len(next) != 3 || *next[2].OnLevel != 120 {
		t.Errorf("the veto saw %d states", len(next))
	}
	if err := s.OnWrite(nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Underlying().MatterWrite(ctx, levelcontroldef.AttrOnLevel, uint64(180)); err != nil {
		t.Errorf("a removed veto: %v", err)
	}
}

// TestTypedOverTheSelfHeldServer: the typed server over a server that is
// not the generated one reads and reports through it and refuses what it
// cannot reach — here a LevelControl reduced to contract.ClusterServer.
func TestTypedOverTheSelfHeldServer(t *testing.T) {
	t.Parallel()
	inner := typedServer(t).Underlying()
	s := levelcontroldef.Wrap(struct{ contract.ClusterServer }{inner})
	if got := s.Get(); got.MinLevel != 10 || got.MaxLevel != 200 {
		t.Errorf("Get through MatterRead: %+v", got)
	}
	if err := s.Set(levelcontroldef.Patch{OnLevel: ptr(uint8(20))}); !errors.Is(err, spec.ErrNotSettable) {
		t.Errorf("Set: %v", err)
	}
	if err := s.HandleMoveToLevel(nil); !errors.Is(err, spec.ErrNoHandlers) {
		t.Errorf("HandleMoveToLevel: %v", err)
	}
	if err := s.OnWrite(nil); !errors.Is(err, spec.ErrNoWriteVeto) {
		t.Errorf("OnWrite: %v", err)
	}
}

// TestTypedServerIsNotMountable: the typed server is no
// contract.ClusterServer, so a host cannot mount it in place of the
// server it wraps.
func TestTypedServerIsNotMountable(t *testing.T) {
	t.Parallel()
	if reflect.TypeFor[*levelcontroldef.Server]().Implements(reflect.TypeFor[contract.ClusterServer]()) {
		t.Error("*levelcontroldef.Server implements contract.ClusterServer")
	}
}
