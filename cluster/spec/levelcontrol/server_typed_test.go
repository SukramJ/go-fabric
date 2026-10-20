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
	if s.Accepts(levelcontroldef.CmdMoveToLevel) {
		t.Fatal("MoveToLevel accepted before its handler")
	}
	refusal := spec.Errorf(im.StatusConstraintError, "no")
	var got []levelcontroldef.MoveToLevelRequest
	s.HandleMoveToLevel(func(_ context.Context, req levelcontroldef.MoveToLevelRequest) (struct{}, error) {
		got = append(got, req)
		if req.Level > 200 {
			return struct{}{}, refusal
		}
		return struct{}{}, nil
	})
	if !s.Accepts(levelcontroldef.CmdMoveToLevel) {
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
	if resp, err := s.MatterInvoke(ctx, levelcontroldef.CmdMoveToLevel, fields); resp != nil || err != nil {
		t.Errorf("MoveToLevel answered %#v, %v", resp, err)
	}
	if len(got) != 1 || got[0] != want {
		t.Errorf("the handler received %+v, want %+v", got, want)
	}
	if _, err := s.MatterInvoke(ctx, levelcontroldef.CmdMoveToLevel, levelcontroldef.MoveToLevelRequest{Level: 250}); !errors.Is(err, refusal) {
		t.Errorf("the handler's refusal: %v", err)
	}

	s.HandleMoveToLevel(nil)
	if s.Accepts(levelcontroldef.CmdMoveToLevel) {
		t.Error("MoveToLevel accepted after its handler was removed")
	}
}

func TestTypedOnWrite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := typedServer(t)
	refusal := errors.New("the dimmer is locked")
	var next []levelcontroldef.State
	s.OnWrite(func(_ context.Context, st levelcontroldef.State, attrID uint32) error {
		next = append(next, st)
		if attrID == levelcontroldef.AttrOnLevel && *st.OnLevel > 150 {
			return refusal
		}
		return nil
	})
	if err := s.MatterWrite(ctx, levelcontroldef.AttrOnLevel, uint64(160)); !errors.Is(err, refusal) {
		t.Fatalf("a vetoed write: %v", err)
	}
	var sce im.StatusCodeError
	if err := s.MatterWrite(ctx, levelcontroldef.AttrOnLevel, uint64(160)); !errors.As(err, &sce) || sce.MatterStatusCode() != im.StatusFailure {
		t.Errorf("a plain veto answers %v", err)
	}
	if got := s.Get(); got.OnLevel != nil {
		t.Errorf("a vetoed write stored %d", *got.OnLevel)
	}
	if err := s.MatterWrite(ctx, levelcontroldef.AttrOnLevel, uint64(120)); err != nil {
		t.Fatal(err)
	}
	if got := s.Get(); got.OnLevel == nil || *got.OnLevel != 120 {
		t.Errorf("an accepted write: %+v", got)
	}
	if len(next) != 3 || *next[2].OnLevel != 120 {
		t.Errorf("the veto saw %d states", len(next))
	}
	s.OnWrite(nil)
	if err := s.MatterWrite(ctx, levelcontroldef.AttrOnLevel, uint64(180)); err != nil {
		t.Errorf("a removed veto: %v", err)
	}
}

// TestTypedServerForwardsTheMethodSet holds the typed server to the
// embedded one: every method of *spec.Server is promoted with its
// signature — Set excepted, which the typed server declares over a Patch
// and which no contract interface names — so the dispatcher's assertions
// on a mounted server find what they find on *spec.Server.
func TestTypedServerForwardsTheMethodSet(t *testing.T) {
	t.Parallel()
	shadowed := map[string]bool{"Set": true}
	inner, outer := reflect.TypeFor[*spec.Server](), reflect.TypeFor[*levelcontroldef.Server]()
	for m := range inner.Methods() {
		got, ok := outer.MethodByName(m.Name)
		switch {
		case !ok:
			t.Errorf("%s is not promoted", m.Name)
		case shadowed[m.Name]:
		case got.Type.NumIn() != m.Type.NumIn() || got.Type.NumOut() != m.Type.NumOut():
			t.Errorf("%s: %v, want %v", m.Name, got.Type, m.Type)
		default:
			for i := 1; i < m.Type.NumIn(); i++ {
				if got.Type.In(i) != m.Type.In(i) {
					t.Errorf("%s: %v, want %v", m.Name, got.Type, m.Type)
				}
			}
		}
	}
	var srv contract.ClusterServer = typedServer(t)
	for _, iface := range []reflect.Type{
		reflect.TypeFor[contract.ClusterDataVersion](),
		reflect.TypeFor[contract.ClusterAttributeLister](),
		reflect.TypeFor[contract.ClusterCommandLister](),
		reflect.TypeFor[contract.ClusterEventLister](),
		reflect.TypeFor[contract.ClusterAttributeReadPrivilege](),
		reflect.TypeFor[contract.ClusterAttributeWritePrivilege](),
		reflect.TypeFor[contract.ClusterCommandInvokePrivilege](),
		reflect.TypeFor[contract.TimedCommander](),
		reflect.TypeFor[contract.AttributeChangeNotifier](),
		reflect.TypeFor[contract.EventReceiver](),
	} {
		if !reflect.TypeOf(srv).Implements(iface) {
			t.Errorf("the typed server does not implement %v", iface)
		}
	}
	for _, name := range []string{"Set"} {
		for _, iface := range []reflect.Type{reflect.TypeFor[contract.ClusterServer](), reflect.TypeFor[contract.AttributeChangeNotifier]()} {
			if _, named := iface.MethodByName(name); named {
				t.Errorf("%v names the shadowed %s", iface, name)
			}
		}
	}
}
