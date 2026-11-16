// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"context"
	"slices"
	"sync"
	"testing"

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
