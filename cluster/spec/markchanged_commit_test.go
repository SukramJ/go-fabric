// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/spec"
	hepa "github.com/SukramJ/go-fabric/cluster/spec/hepafiltermonitoring"
	"github.com/SukramJ/go-fabric/contract"
)

// TestMarkChangedInsideAWriteJoinsItsCommit holds MarkChanged to the
// commit a controller write keeps open while its Sink runs, as a Set there
// is: the write advances the version once for its own change and the
// marked one, and the marked attribute is told once the write left its
// window — not before the write's own store. Before, MarkChanged advanced
// the version and notified at once, outside the commit, so one write
// advanced the version twice (review II, R2-P6-conc-1308).
func TestMarkChangedInsideAWriteJoinsItsCommit(t *testing.T) {
	t.Parallel()
	sink := &funcSink{}
	srv := hepaServer(t, spec.ServerConfig{Sink: sink})
	var (
		mu   sync.Mutex
		told []uint32
	)
	srv.OnAttributeChange(func(c contract.Change) {
		mu.Lock()
		told = append(told, c.Attributes...)
		mu.Unlock()
	})
	sink.fn = func(context.Context, uint32, any) error {
		if err := srv.MarkChanged(hepa.AttrCondition); err != nil {
			t.Errorf("MarkChanged inside the Sink: %v", err)
		}
		mu.Lock()
		defer mu.Unlock()
		if len(told) != 0 {
			t.Errorf("inside the Sink %v was already told, want nothing before the write left its window", told)
		}
		return nil
	}
	v0 := srv.MatterDataVersion()
	changed, err := srv.MatterWriteCommit(context.Background(), hepa.AttrLastChangedTime, uint32(1000))
	if err != nil {
		t.Fatal(err)
	}
	if d := srv.MatterDataVersion() - v0; d != 1 {
		t.Errorf("one write with a MarkChanged in its Sink advanced the version %d times, want 1", d)
	}
	if !slices.Contains(changed, hepa.AttrCondition) || !slices.Contains(changed, hepa.AttrLastChangedTime) {
		t.Errorf("the write's change list %v, want the written and the marked attribute", changed)
	}
	mu.Lock()
	if !slices.Contains(told, hepa.AttrCondition) {
		t.Errorf("told %v, want the marked attribute", told)
	}
	mu.Unlock()

	// Outside a write MarkChanged still advances and tells at once.
	v1 := srv.MatterDataVersion()
	if err := srv.MarkChanged(hepa.AttrCondition); err != nil {
		t.Fatal(err)
	}
	if d := srv.MatterDataVersion() - v1; d != 1 {
		t.Errorf("MarkChanged outside a write advanced the version %d times, want 1", d)
	}
}

// TestMarkQuietChangedInsideAWriteJoinsItsCommit holds MarkQuietChanged to
// the same open commit as MarkChanged: called from a write's Sink, the
// quieter attribute's change listeners are told once the write left its
// window, not inside it (review II, R2-P6-conc-1308).
func TestMarkQuietChangedInsideAWriteJoinsItsCommit(t *testing.T) {
	t.Parallel()
	u8 := spec.Type{Name: "uint8", Kind: spec.KindUint, Bits: 8}
	m := spec.Conformance{Text: "M", Op: spec.ConfMandatory}
	def := &spec.Cluster{
		ID: 0xFFF1FC0D, Name: "QuietProbe", Revision: 1,
		Attributes: []spec.Attribute{
			{ID: 0, Name: "Written", Type: u8, Conformance: m, Access: spec.Access{RW: "RW", Read: spec.PrivilegeView, Write: spec.PrivilegeOperate}},
			{ID: 1, Name: "Computed", Type: u8, Conformance: m, Access: spec.Access{RW: "R", Read: spec.PrivilegeView}, Quality: spec.Quality{Quieter: true}, QuietInterval: time.Second},
		},
	}
	sink := &funcSink{}
	srv, err := spec.NewServer(def, spec.Options{}, spec.ServerConfig{Sink: sink, Clock: clocktest.New(time.Unix(1_700_000_000, 0))})
	if err != nil {
		t.Fatal(err)
	}
	var (
		mu   sync.Mutex
		told []uint32
	)
	srv.OnAttributeChange(func(c contract.Change) {
		mu.Lock()
		told = append(told, c.Attributes...)
		mu.Unlock()
	})
	sink.fn = func(context.Context, uint32, any) error {
		if err := srv.MarkQuietChanged(1); err != nil {
			t.Errorf("MarkQuietChanged inside the Sink: %v", err)
		}
		mu.Lock()
		defer mu.Unlock()
		if slices.Contains(told, 1) {
			t.Errorf("inside the Sink %v was already told, want nothing before the write left its window", told)
		}
		return nil
	}
	if _, err := srv.MatterWriteCommit(context.Background(), 0, uint8(5)); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Contains(told, 1) {
		t.Errorf("told %v, want the quieter attribute once the write left its window", told)
	}
}
