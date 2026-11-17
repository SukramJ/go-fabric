// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/store/memstore"
)

// gkmWriteLockProbe is a store that records, at every GroupKeyMap store
// write, whether the server's writeMu is held.
type gkmWriteLockProbe struct {
	*memstore.Store
	g        *GroupKeyManagement
	mu       sync.Mutex
	unlocked []string
	writes   int
}

func (p *gkmWriteLockProbe) note(op string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.writes++
	if p.g.writeMu.TryLock() {
		p.g.writeMu.Unlock()
		p.unlocked = append(p.unlocked, op)
	}
}

func (p *gkmWriteLockProbe) SetGroupKeyMapping(ctx context.Context, m store.GroupKeyMapping) error {
	p.note("SetGroupKeyMapping")
	return p.Store.SetGroupKeyMapping(ctx, m)
}

func (p *gkmWriteLockProbe) RemoveGroupKeyMapping(ctx context.Context, fabric uint8, groupID uint16) error {
	p.note("RemoveGroupKeyMapping")
	return p.Store.RemoveGroupKeyMapping(ctx, fabric, groupID)
}

func (p *gkmWriteLockProbe) ReplaceGroupKeyMappings(ctx context.Context, fabric uint8, m []store.GroupKeyMapping) error {
	p.note("ReplaceGroupKeyMappings")
	return p.Store.ReplaceGroupKeyMappings(ctx, fabric, m)
}

// TestGroupKeyMapEveryWriterHoldsWriteMu is the standing guard of
// F-db021da0-42: every path that changes a fabric's GroupKeyMap — a
// controller's write of the attribute and Groupcast's binding changes —
// reaches the store with writeMu held, so the Groupcast binding's check
// and store are one step against a concurrent write.
func TestGroupKeyMapEveryWriterHoldsWriteMu(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mem := memstore.New()
	fabric, err := mem.AddFabric(ctx, store.FabricRecord{FabricID: 1, NodeID: 2, RootPublicKey: []byte{4, 1}, VendorID: 0xFFF1})
	if err != nil {
		t.Fatal(err)
	}
	probe := &gkmWriteLockProbe{Store: mem}
	g, err := NewGroupKeyManagement(probe, GroupKeyMgmtConfig{})
	if err != nil {
		t.Fatal(err)
	}
	probe.g = g

	steps := []struct {
		name string
		run  func() error
	}{
		{"Groupcast binds", func() error { return g.setGroupKeyMapping(ctx, fabric, 0x0101, 7) }},
		{"Groupcast unbinds", func() error { return g.removeGroupKeyMapping(ctx, fabric, 0x0101) }},
		{"controller writes the attribute", func() error {
			_, err := g.MatterWriteChanged(im.WithFabricFilter(ctx, true, fabric), 0x0000,
				[]GroupKeyMapStruct{{GroupID: 0x0102, GroupKeySetID: 7, FabricIndex: fabric}})
			return err
		}},
	}
	for _, s := range steps {
		if err := s.run(); err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
	}
	if probe.writes != len(steps) {
		t.Fatalf("store GroupKeyMap writes = %d, want %d (one per step)", probe.writes, len(steps))
	}
	if len(probe.unlocked) != 0 {
		t.Fatalf("GroupKeyMap store writes without writeMu: %v", probe.unlocked)
	}
}
