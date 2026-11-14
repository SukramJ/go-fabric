// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	epmem "github.com/SukramJ/go-fabric/endpoint/memstore"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/store/memstore"
	"github.com/SukramJ/go-fabric/surface"
)

// refusingKeys is a credential store that refuses, while refuse is set,
// every write of a key with one of the prefixes in keys — a single
// SetSetting and a SetSettings that names one alike, which then writes
// nothing (store.Settings.SetSettings).
type refusingKeys struct {
	store.Port
	keys   []string
	refuse atomic.Bool
	// refuseRead refuses the reads of the same keys.
	refuseRead atomic.Bool
}

var errKeyRefused = errors.New("key refused")

func (s *refusingKeys) refused(key string) bool {
	if !s.refuse.Load() {
		return false
	}
	for _, k := range s.keys {
		if strings.HasPrefix(key, k) {
			return true
		}
	}
	return false
}

func (s *refusingKeys) GetSetting(ctx context.Context, key string) (value string, found bool, err error) {
	if s.refuseRead.Load() {
		for _, k := range s.keys {
			if strings.HasPrefix(key, k) {
				return "", false, errKeyRefused
			}
		}
	}
	return s.Port.GetSetting(ctx, key)
}

func (s *refusingKeys) SetSetting(ctx context.Context, key, value string) error {
	if s.refused(key) {
		return errKeyRefused
	}
	return s.Port.SetSetting(ctx, key, value)
}

func (s *refusingKeys) SetSettings(ctx context.Context, values map[string]string) error {
	for k := range values {
		if s.refused(k) {
			return errKeyRefused
		}
	}
	return s.Port.SetSettings(ctx, values)
}

// TestRootLabelWriteLandsWholeOrNotAtAll: the root's NodeLabel and
// Location are stored in one write, so a refused write of one leaves the
// other as it was — a NodeLabel that answered FAILURE is not served after
// a restart (R2-P2-store-06).
func TestRootLabelWriteLandsWholeOrNotAtAll(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	st := &refusingKeys{Port: memstore.New(), keys: []string{"basic_information.location"}}
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Credentials: st, Endpoints: epmem.New()}
	n := newNode(t, cfg)
	st.refuse.Store(true)
	if err := n.Root().BasicInformation.SetNodeLabel(ctx, "refused"); !errors.Is(err, errKeyRefused) {
		t.Fatalf("SetNodeLabel with the Location write refused: %v, want the store's error", err)
	}
	if v, ok, err := st.GetSetting(ctx, "basic_information.node_label"); err != nil || (ok && v == "refused") {
		t.Errorf("stored NodeLabel %q (found %v, %v) after a write that answered FAILURE", v, ok, err)
	}
}

// TestSurfaceStoredOnlyWithItsRaisedVersions: a surface change whose
// raised ConfigurationVersions the store refuses stores no surface either,
// so the next start finds the change again and raises again — the
// version a controller saw does not go back down (R2-P2-store-03 a).
func TestSurfaceStoredOnlyWithItsRaisedVersions(t *testing.T) {
	db := openDB(t, filepath.Join(t.TempDir(), "versions.db"))
	startRun(t, db, surface.Policies{}, nil, light(t, "light", nil), thermostat(t, "thermo", false, nil)).stop(t)

	p := surface.Policies{Extensions: surface.NewExtensions(mschRow)}
	refuse := func(cfg *node.Config) {
		r := &refusingKeys{Port: cfg.Store.Credentials, keys: []string{"basic_information.configuration_version", "bridged_configuration_version."}}
		r.refuse.Store(true)
		cfg.Store.Credentials = r
	}
	run := startRun(t, db, p, refuse, light(t, "light", &p), thermostat(t, "thermo", true, &p))
	served := rootVersion(t, run.n)
	if served != 2 {
		t.Fatalf("root ConfigurationVersion %d after the change, want 2", served)
	}
	run.stop(t)

	again := startRun(t, db, p, nil, light(t, "light", &p), thermostat(t, "thermo", true, &p))
	if got := rootVersion(t, again.n); got < served {
		t.Errorf("root ConfigurationVersion %d after the restart, %d served before it", got, served)
	}
	if got := bridged(t, again.n, "thermo").ConfigurationVersion(); got < 2 {
		t.Errorf("thermostat ConfigurationVersion %d after the restart, 2 served before it", got)
	}
}

// TestSurfaceUnreadIsNotAFirstAssembly: a stored surface the store fails
// to read is no first assembly: nothing is stored over it, and the next
// start announces the change (R2-P2-store-03 b).
func TestSurfaceUnreadIsNotAFirstAssembly(t *testing.T) {
	db := openDB(t, filepath.Join(t.TempDir(), "unread.db"))
	startRun(t, db, surface.Policies{}, nil, light(t, "light", nil), thermostat(t, "thermo", false, nil)).stop(t)

	p := surface.Policies{Extensions: surface.NewExtensions(mschRow)}
	unread := func(cfg *node.Config) {
		r := &refusingKeys{Port: cfg.Store.Credentials, keys: []string{"bridged_surface_digest.thermo"}}
		r.refuseRead.Store(true)
		cfg.Store.Credentials = r
	}
	startRun(t, db, p, unread, light(t, "light", &p), thermostat(t, "thermo", true, &p)).stop(t)

	again := startRun(t, db, p, nil, light(t, "light", &p), thermostat(t, "thermo", true, &p))
	if got := bridged(t, again.n, "thermo").ConfigurationVersion(); got != 2 {
		t.Errorf("thermostat ConfigurationVersion %d after a changed surface, want 2", got)
	}
}
