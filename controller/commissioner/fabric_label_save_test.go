// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/controller/ca"
)

// countingFabricStore counts SaveFabric calls over a MemoryStore and
// fails them while fail is set.
type countingFabricStore struct {
	ca.MemoryStore
	mu    sync.Mutex
	saves int
	fail  bool
}

var errSaveRefused = errors.New("save refused")

func (s *countingFabricStore) SaveFabric(r ca.FabricRecord) error {
	s.mu.Lock()
	s.saves++
	fail := s.fail
	s.mu.Unlock()
	if fail {
		return errSaveRefused
	}
	return s.MemoryStore.SaveFabric(r)
}

func (s *countingFabricStore) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saves
}

// countingStoreWithLabel is storeWithLabel over a countingFabricStore,
// its counter reset.
func countingStoreWithLabel(t *testing.T, label string, signer *opSigner) (*ca.CA, *countingFabricStore) {
	t.Helper()
	st := &countingFabricStore{}
	c, err := ca.New(ca.Config{Store: st})
	if err != nil {
		t.Fatal(err)
	}
	cfg := FabricConfig{FabricID: 1, RootNodeID: 1, Store: st}
	if signer != nil {
		cfg.OperationalSigner = signer
	}
	if _, err := NewFabric(c, cfg); err != nil {
		t.Fatal(err)
	}
	rec, _, _ := st.LoadFabric()
	rec.Label = label
	if err := st.MemoryStore.SaveFabric(rec); err != nil {
		t.Fatal(err)
	}
	st.mu.Lock()
	st.saves = 0
	st.mu.Unlock()
	return c, st
}

// TestStoredFabricLabelCutIsSavedOnceAtLoad: a stored label over the bound
// is cut and the record saved once at load — on the key path, which saves
// nothing else at load, and on the signer path that keeps its NOC — with
// every other field of the record as stored; a label within the bound
// saves nothing; the next load neither saves nor warns
// (R4-par-4403, decision 597).
func TestStoredFabricLabelCutIsSavedOnceAtLoad(t *testing.T) {
	t.Parallel()
	stored, want := strings.Repeat("ü", 32), strings.Repeat("ü", 16)
	for _, signed := range []bool{false, true} {
		name := "key"
		if signed {
			name = "signer"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var s *opSigner
			if signed {
				s = newOpSigner(t)
			}
			cfgFor := func(st *countingFabricStore, log *slog.Logger) FabricConfig {
				cfg := FabricConfig{Store: st, Logger: log}
				if s != nil {
					cfg.OperationalSigner = s
				}
				return cfg
			}
			c, st := countingStoreWithLabel(t, stored, s)
			before, _, _ := st.LoadFabric()
			var out syncBuffer
			f, err := NewFabric(c, cfgFor(st, slog.New(slog.NewTextHandler(&out, nil))))
			if err != nil {
				t.Fatal(err)
			}
			if got := f.CurrentLabel(); got != want {
				t.Errorf("served label %q, want %q", got, want)
			}
			if n := st.count(); n != 1 {
				t.Errorf("the load saved the record %d times, want once", n)
			}
			after, _, _ := st.LoadFabric()
			if after.Label != want {
				t.Errorf("the record holds label %q after the load, want the cut %q", after.Label, want)
			}
			after.Label = before.Label
			if !reflect.DeepEqual(after, before) {
				t.Errorf("the save at load changed more than the label:\nbefore %+v\nafter  %+v", before, after)
			}
			if !strings.Contains(out.String(), "matter.controller.fabric.stored_label_shortened") {
				t.Errorf("the first load did not warn the cut; log:\n%s", out.String())
			}
			// The next start: nothing to cut, nothing saved, nothing warned.
			var again syncBuffer
			if _, err := NewFabric(c, cfgFor(st, slog.New(slog.NewTextHandler(&again, nil)))); err != nil {
				t.Fatal(err)
			}
			if n := st.count(); n != 1 {
				t.Errorf("the second load saved again: %d saves in all, want 1", n)
			}
			if again.String() != "" {
				t.Errorf("the second load logged:\n%s", again.String())
			}
		})
	}
}

// TestStoredFabricLabelCutSaveFailureIsWarned: a failed save of the cut
// label at load is warned and the load continues with the cut label; the
// record keeps the stored label, cut again at the next start
// (R4-par-4403, decision 597).
func TestStoredFabricLabelCutSaveFailureIsWarned(t *testing.T) {
	t.Parallel()
	stored, want := strings.Repeat("ü", 32), strings.Repeat("ü", 16)
	c, st := countingStoreWithLabel(t, stored, nil)
	st.mu.Lock()
	st.fail = true
	st.mu.Unlock()
	var out syncBuffer
	f, err := NewFabric(c, FabricConfig{Store: st, Logger: slog.New(slog.NewTextHandler(&out, nil))})
	if err != nil {
		t.Fatalf("a failed save of the cut label failed the load: %v", err)
	}
	if got := f.CurrentLabel(); got != want {
		t.Errorf("served label %q, want %q", got, want)
	}
	if n := st.count(); n != 1 {
		t.Errorf("%d save attempts at load, want 1", n)
	}
	if rec, _, _ := st.LoadFabric(); rec.Label != stored {
		t.Errorf("the record holds %q after a failed save, want the stored %q", rec.Label, stored)
	}
	log := out.String()
	if !strings.Contains(log, "level=WARN") || !strings.Contains(log, "matter.controller.fabric.stored_label_save_failed") ||
		!strings.Contains(log, errSaveRefused.Error()) {
		t.Errorf("the failed save was not warned with its error; log:\n%s", log)
	}
}
