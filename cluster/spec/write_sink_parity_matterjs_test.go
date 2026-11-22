// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"context"
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/contract"

	"thingfabric.dev/go-fabric/cluster/spec"
	hepa "thingfabric.dev/go-fabric/cluster/spec/hepafiltermonitoring"
)

// TestSinkHearsNoWriteOfTheHeldValue: a controller's write of the value
// the server holds reaches no Sink, stores nothing and answers unchanged;
// a write of another value reaches the Sink, then the store, and answers
// changed. matter.js emits neither `$Changing` for a field whose value
// equals the held one (Datasource.ts:1406-1407 at db021da0,
// #computePreCommitChange) nor `$Changed` (:1479, #noteIfChanged), and
// stores only the noted fields (:1447-1451) — a write of the held value
// reaches no observer.
func TestSinkHearsNoWriteOfTheHeldValue(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	sk := &sink{}
	srv := hepaServer(t, spec.ServerConfig{Sink: sk})
	for i, c := range []struct {
		value       any
		wantChanged bool
		wantSink    []any
	}{
		{uint64(1000), true, []any{uint32(1000)}},
		{uint32(1000), false, []any{uint32(1000)}}, // the held value, as written in another width
		{uint64(1000), false, []any{uint32(1000)}}, // again
		{nil, true, []any{uint32(1000), nil}},      // another value
		{nil, false, []any{uint32(1000), nil}},     // the held null
		{uint64(7), true, []any{uint32(1000), nil, uint32(7)}},
	} {
		v0 := srv.MatterDataVersion()
		changed, err := srv.MatterWriteChanged(ctx, hepa.AttrLastChangedTime, c.value)
		if err != nil {
			t.Fatalf("write %d of %#v: %v", i+1, c.value, err)
		}
		advanced := srv.MatterDataVersion() != v0
		if changed != c.wantChanged || advanced != c.wantChanged || !slices.Equal(sk.writes, c.wantSink) {
			t.Errorf("write %d of %#v: changed %v, version advanced %v, sink heard %#v; want changed %v and %#v",
				i+1, c.value, changed, advanced, sk.writes, c.wantChanged, c.wantSink)
		}
	}

	// A Source answering another value than the store holds: the host's
	// state differs from the written value, so the Sink hears it (the
	// comparison skips the Sink only when the server's every answer
	// equals the written value).
	sk2 := &sink{}
	src := source{hepa.AttrLastChangedTime: uint32(9)}
	srv2 := hepaServer(t, spec.ServerConfig{Sink: sk2, Source: src})
	if _, err := srv2.MatterWriteChanged(ctx, hepa.AttrLastChangedTime, uint64(5)); err != nil {
		t.Fatal(err)
	}
	if _, err := srv2.MatterWriteChanged(ctx, hepa.AttrLastChangedTime, uint64(5)); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(sk2.writes, []any{uint32(5), uint32(5)}) {
		t.Errorf("with a Source answering 9, the Sink heard %#v; want both writes of 5", sk2.writes)
	}
}

// TestWriteCommitNamesTheChangedAttribute: Server's change naming
// (contract.ChangeNamingWriter) names the written attribute when its
// stored value moved and nothing for a write of the held value, the same
// verdict MatterWriteChanged gives; a refused write names nothing and
// carries its error. matter.js notes a name in a commit's change list only
// when its value differs (Datasource.ts:1476-1484 at db021da0).
func TestWriteCommitNamesTheChangedAttribute(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srv := hepaServer(t, spec.ServerConfig{})
	for i, c := range []struct {
		value any
		want  []uint32
	}{
		{uint64(1000), []uint32{hepa.AttrLastChangedTime}},
		{uint32(1000), []uint32{}},
		{nil, []uint32{hepa.AttrLastChangedTime}},
	} {
		got, err := srv.MatterWriteCommit(ctx, hepa.AttrLastChangedTime, c.value)
		if err != nil || got == nil || !slices.Equal(got, c.want) {
			t.Errorf("write %d of %#v: %v, %v; want %v", i+1, c.value, got, err, c.want)
		}
	}
	if got, err := srv.MatterWriteCommit(ctx, hepa.AttrLastChangedTime, "not a number"); err == nil || len(got) != 0 {
		t.Errorf("a refused write: %v, %v; want an error and no names", got, err)
	}
}

// siblingSink moves Condition when LastChangedTime is written, through
// the server's Set, from inside the write.
type siblingSink struct{ srv *spec.Server }

func (k *siblingSink) MatterWriteAttribute(_ context.Context, attrID uint32, _ any) error {
	if attrID != hepa.AttrLastChangedTime {
		return nil
	}
	v, _ := k.srv.MatterRead(hepa.AttrCondition)
	c, _ := v.(uint8)
	return k.srv.Set(hepa.AttrCondition, c+1)
}

// TestWriteCommitNamesTheSiblingItsSinkMoves (review II, Fable read
// 5b-write F1): a sibling the Sink sets inside a controller's write is
// part of the write's commit — named beside the written attribute — and
// the server's version advances once for the write, not once per Set.
// matter.js advances a datasource's version once per transaction for every
// field it noted (Datasource.ts:1456-1457, :1476-1484 at db021da0).
func TestWriteCommitNamesTheSiblingItsSinkMoves(t *testing.T) {
	t.Parallel()
	sk := &siblingSink{}
	srv := hepaServer(t, spec.ServerConfig{Sink: sk})
	sk.srv = srv
	v0 := srv.MatterDataVersion()
	got, err := srv.MatterWriteCommit(context.Background(), hepa.AttrLastChangedTime, uint64(1000))
	if err != nil {
		t.Fatal(err)
	}
	if want := []uint32{hepa.AttrCondition, hepa.AttrLastChangedTime}; !slices.Equal(got, want) {
		t.Errorf("the write named %v, want %v", got, want)
	}
	if d := srv.MatterDataVersion() - v0; d != 1 {
		t.Errorf("the version advanced %d times for one write, want once", d)
	}
}

type panickingSiblingSink struct{ srv *spec.Server }

func (k *panickingSiblingSink) MatterWriteAttribute(_ context.Context, attrID uint32, _ any) error {
	if attrID != hepa.AttrLastChangedTime {
		return nil
	}
	if err := k.srv.Set(hepa.AttrCondition, uint8(7)); err != nil {
		return err
	}
	panic("sink fails after it moved a sibling")
}

// TestWriteCommitSettlesWhenTheSinkPanics (review II, Fable read
// 5b-write T3): a Sink that sets a sibling and then panics leaves the
// sibling changed — the version advances once and the change listener
// is told before the panic goes on.
func TestWriteCommitSettlesWhenTheSinkPanics(t *testing.T) {
	t.Parallel()
	sk := &panickingSiblingSink{}
	srv := hepaServer(t, spec.ServerConfig{Sink: sk})
	sk.srv = srv
	var told []uint32
	srv.OnAttributeChange(func(ch contract.Change) { told = append(told, ch.Attributes...) })
	v0 := srv.MatterDataVersion()
	func() {
		defer func() { _ = recover() }()
		_, _ = srv.MatterWriteCommit(context.Background(), hepa.AttrLastChangedTime, uint64(1000))
	}()
	if d := srv.MatterDataVersion() - v0; d != 1 {
		t.Errorf("the version advanced %d times, want once", d)
	}
	if !slices.Contains(told, hepa.AttrCondition) {
		t.Errorf("the listener was told %v, want the sibling", told)
	}
	// The window is open again: a Set outside any write advances.
	v1 := srv.MatterDataVersion()
	if err := srv.Set(hepa.AttrCondition, uint8(9)); err != nil {
		t.Fatal(err)
	}
	if srv.MatterDataVersion() == v1 {
		t.Error("a Set after the panic advanced no version: the window stayed open")
	}
}
