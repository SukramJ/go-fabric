// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store/memstore"
)

// TestFactoryResetLeavesNoEndpointRecordResidue extends
// store.TestStoreEraseLeavesNoResidue to the endpoint records the node
// keeps on its Store.Path file: after FactoryReset a recognisable stable
// key is in none of the database file, its -wal or its -shm — on a running
// and on a stopped node, with the node's own credential store on the file
// and with a host override in its place (so only the endpoint records are
// on the file).
func TestFactoryResetLeavesNoEndpointRecordResidue(t *testing.T) {
	t.Parallel()
	const marker = "RESET-ENDPOINT-RESIDUE-MARKER-3b7d0c41"
	for _, state := range []struct {
		name    string
		stopped bool
	}{{"running", false}, {"stopped", true}} {
		for _, creds := range []struct {
			name     string
			override bool
		}{{"credentials on the file", false}, {"credentials override", true}} {
			t.Run(state.name+"/"+creds.name, func(t *testing.T) {
				t.Parallel()
				ctx := t.Context()
				dir := t.TempDir()
				cfg := minimalConfig(nil)
				cfg.Store = node.Store{Path: filepath.Join(dir, "node.db")}
				if creds.override {
					cfg.Store.Credentials = memstore.New()
				}
				n, err := node.New(ctx, cfg)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = n.Stop(context.Background()) })
				if err := n.Aggregator().Add(ctx, newPlug(t, marker)); err != nil {
					t.Fatal(err)
				}
				resetLifecycle(t, n, state.stopped)
				if hits := dirFilesHolding(t, dir, marker); len(hits) == 0 {
					t.Fatal("the endpoint record's key is in no file before the reset: the test cannot see a residue")
				}
				if err := n.FactoryReset(ctx); err != nil {
					t.Fatalf("FactoryReset: %v", err)
				}
				if hits := dirFilesHolding(t, dir, marker); len(hits) > 0 {
					t.Errorf("after FactoryReset the endpoint record's bytes are still in %v", hits)
				}
			})
		}
	}
}

// dirFilesHolding names the files of dir whose bytes contain needle.
func dirFilesHolding(t *testing.T, dir, needle string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var hits []string
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name())) //nolint:gosec // the test's own temp directory
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(b, []byte(needle)) {
			hits = append(hits, e.Name())
		}
	}
	return hits
}
