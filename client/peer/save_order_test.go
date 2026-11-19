// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"testing"
	"testing/synctest"
)

// gatedStore holds the first SavePeer of a descriptor whose
// OperationalHost is hold until release is closed.
type gatedStore struct {
	*MemoryStore
	hold    string
	entered chan struct{}
	release chan struct{}
}

func (s *gatedStore) SavePeer(d Descriptor) error {
	if d.OperationalHost == s.hold && s.entered != nil {
		close(s.entered)
		s.entered = nil
		<-s.release
	}
	return s.MemoryStore.SavePeer(d)
}

// TestSaveKeepsTheNewestDescriptor: a change made while an earlier save
// is still in the store is saved after it, so the store ends with the
// newest descriptor. Before, each change after the clone started its own
// save, and a slow first save overwrote the newer one — the cause of
// TestOperationalHostLearned's "timed out waiting for the saved host"
// under load (review II phase 7 backlog).
func TestSaveKeepsTheNewestDescriptor(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		gs := &gatedStore{MemoryStore: &MemoryStore{}, hold: "a.local", entered: make(chan struct{}), release: make(chan struct{})}
		h := newHarness(t, func(c *Context) { c.Store = gs })
		p, _ := h.set.For(testAddr)
		entered := gs.entered
		p.mergeKnown(Descriptor{OperationalHost: "a.local"})
		<-entered // the first save cloned "a.local" and is in the store
		p.mergeKnown(Descriptor{OperationalHost: "b.local"})
		synctest.Wait()
		close(gs.release)
		synctest.Wait()
		ds, _ := gs.LoadPeers(testAddr.FabricIndex)
		if len(ds) != 1 || ds[0].OperationalHost != "b.local" {
			t.Fatalf("stored %+v, want the newest host b.local", ds)
		}
	})
}
