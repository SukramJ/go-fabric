// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/SukramJ/go-fabric/endpoint"
	epmem "github.com/SukramJ/go-fabric/endpoint/memstore"
	"github.com/SukramJ/go-fabric/endpoint/sqlitestore"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/store/memstore"
)

// resetLifecycle runs n to the state a case resets it in: started, and
// stopped again when stopped is set.
func resetLifecycle(t *testing.T, n *node.Node, stopped bool) {
	t.Helper()
	ctx := t.Context()
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if stopped {
		if err := n.Stop(ctx); err != nil {
			t.Fatal(err)
		}
	}
}

// TestFactoryResetErasesTheStoresTheNodeUsed: a node on Store.Path whose
// credential store, endpoint store or both are host overrides has exactly
// those stores erased by FactoryReset — the override and what the node
// built on the file — whether the node still runs or was stopped before.
func TestFactoryResetErasesTheStoresTheNodeUsed(t *testing.T) {
	t.Parallel()
	for _, state := range []struct {
		name    string
		stopped bool
	}{{"running", false}, {"stopped", true}} {
		for _, override := range []struct {
			name                   string
			credentials, endpoints bool
		}{
			{"credentials override", true, false},
			{"endpoints override", false, true},
			{"both overrides", true, true},
		} {
			t.Run(state.name+"/"+override.name, func(t *testing.T) {
				t.Parallel()
				ctx := t.Context()
				path := filepath.Join(t.TempDir(), "path.db")
				cfg := minimalConfig(nil)
				cfg.Store = node.Store{Path: path}
				creds := memstore.New()
				eps := epmem.New()
				if override.credentials {
					cfg.Store.Credentials = creds
				}
				if override.endpoints {
					cfg.Store.Endpoints = eps
				}
				n, err := node.New(ctx, cfg)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = n.Stop(context.Background()) })
				if err := n.Store().SetSetting(ctx, "bs.secret", "kept"); err != nil {
					t.Fatal(err)
				}
				if err := n.Aggregator().Add(ctx, newPlug(t, "plug:1")); err != nil {
					t.Fatal(err)
				}
				resetLifecycle(t, n, state.stopped)
				if err := n.FactoryReset(ctx); err != nil {
					t.Fatalf("FactoryReset: %v", err)
				}

				db := openDB(t, path)
				st := store.Port(store.New(db))
				if override.credentials {
					st = creds
				}
				if v, ok, err := st.GetSetting(ctx, "bs.secret"); err != nil || ok {
					t.Errorf("credential store after FactoryReset: %q %v %v, want the setting erased", v, ok, err)
				}
				epStore := endpoint.Store(sqlitestore.New(db))
				if override.endpoints {
					epStore = eps
				}
				if _, err := epStore.GetEndpoint(ctx, endpoint.StringKey("plug:1")); !errors.Is(err, endpoint.ErrNotFound) {
					t.Errorf("endpoint record after FactoryReset: %v, want ErrNotFound", err)
				}
			})
		}
	}
}
