// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
)

// bindingStore is a stored Binding list.
type bindingStore struct {
	mu      sync.Mutex
	list    []core.TargetStruct
	saves   int
	failing bool
}

func (s *bindingStore) load() ([]core.TargetStruct, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.list), nil
}

func (s *bindingStore) save(l []core.TargetStruct) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failing {
		return errors.New("disk full")
	}
	s.list, s.saves = slices.Clone(l), s.saves+1
	if s.list == nil {
		s.list = []core.TargetStruct{}
	}
	return nil
}

func (s *bindingStore) stored() []core.TargetStruct {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.list)
}

func (s *bindingStore) config() core.BindingConfig {
	return core.BindingConfig{Load: s.load, Save: s.save}
}

// TestBindingPersist: a write is saved, a new server restores it before
// anything registers, RemoveFabric is saved, a stored empty list
// restores as empty and nothing stored keeps the server's list.
func TestBindingPersist(t *testing.T) {
	t.Parallel()
	st := &bindingStore{}
	b := core.NewBinding()
	if err := b.Persist(st.config()); err != nil {
		t.Fatal(err)
	}
	entries := []core.TargetStruct{{Node: 2, Endpoint: 1, Cluster: 6}}
	if err := b.MatterWrite(fabricCtx(1), 0, entries); err != nil {
		t.Fatal(err)
	}
	if err := b.MatterWrite(fabricCtx(2), 0, []core.TargetStruct{{Group: 3}}); err != nil {
		t.Fatal(err)
	}
	if got := st.stored(); len(got) != 2 || got[0].FabricIndex != 1 || got[1].Group != 3 {
		t.Fatalf("stored %+v", got)
	}

	// The restart: a new server restores the list before an observer
	// registers (BindingServer.ts:120-126).
	restarted := core.NewBinding()
	if err := restarted.Persist(st.config()); err != nil {
		t.Fatal(err)
	}
	if got := coreEntries(restarted); !slices.Equal(got, st.stored()) {
		t.Fatalf("restored %+v, stored %+v", got, st.stored())
	}

	// A removed fabric's entries are saved gone.
	restarted.RemoveFabric(1)
	if got := st.stored(); len(got) != 1 || got[0].FabricIndex != 2 {
		t.Fatalf("stored after RemoveFabric %+v", got)
	}
	saves := st.saves
	restarted.RemoveFabric(9) // nothing to remove: nothing saved
	if st.saves != saves {
		t.Fatal("a RemoveFabric that removed nothing saved")
	}

	// Nothing stored keeps the list; a stored empty list empties it.
	kept := core.NewBinding()
	if err := kept.MatterWrite(fabricCtx(1), 0, entries); err != nil {
		t.Fatal(err)
	}
	if err := kept.Persist(core.BindingConfig{Load: func() ([]core.TargetStruct, error) { return nil, nil }}); err != nil {
		t.Fatal(err)
	}
	if len(coreEntries(kept)) != 1 {
		t.Fatal("nothing stored replaced the list")
	}
	r := &changeRecorder{}
	kept.OnChange(r.fn)
	if err := kept.Persist(core.BindingConfig{Load: func() ([]core.TargetStruct, error) { return []core.TargetStruct{}, nil }}); err != nil {
		t.Fatal(err)
	}
	if len(coreEntries(kept)) != 0 || len(r.take()) != 1 {
		t.Fatal("a stored empty list was not restored, or the observers did not hear it")
	}
	// Without Save, nothing is stored and the write commits.
	if err := kept.MatterWrite(fabricCtx(1), 0, entries); err != nil || len(coreEntries(kept)) != 1 {
		t.Fatalf("write without Save: %v", err)
	}
}

// TestBindingPersistFailures: a failed Load installs nothing; a failed
// Save refuses the write and changes nothing; RemoveFabric drops the
// entries whatever the save answers.
func TestBindingPersistFailures(t *testing.T) {
	t.Parallel()
	b := core.NewBinding()
	if err := b.Persist(core.BindingConfig{
		Load: func() ([]core.TargetStruct, error) { return nil, errors.New("corrupt") },
		Save: func([]core.TargetStruct) error { t.Error("Save installed by a failed Persist"); return nil },
	}); err == nil {
		t.Fatal("a failed Load restored")
	}
	if err := b.MatterWrite(fabricCtx(1), 0, []core.TargetStruct{{Node: 2, Endpoint: 1}}); err != nil {
		t.Fatal(err)
	}

	st := &bindingStore{}
	if err := b.Persist(core.BindingConfig{Save: st.save}); err != nil {
		t.Fatal(err)
	}
	st.failing = true
	r := &changeRecorder{}
	b.OnChange(r.fn)
	if err := b.MatterWrite(fabricCtx(1), 0, []core.TargetStruct{{Node: 3, Endpoint: 1}}); err == nil {
		t.Fatal("a write whose save failed committed")
	}
	if got := coreEntries(b); len(got) != 1 || got[0].Node != 2 || len(r.take()) != 0 {
		t.Fatalf("a refused write changed the list: %+v", got)
	}
	b.RemoveFabric(1)
	if len(coreEntries(b)) != 0 {
		t.Fatal("RemoveFabric kept a removed fabric's entries when the save failed")
	}
}
