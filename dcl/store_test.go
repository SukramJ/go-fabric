// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestFileStoreRoundTrip: FileStore writes through a synced temporary file
// and a rename; what Save stored, Load returns, and no temporary file is
// left beside it.
func TestFileStoreRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fs := FileStore{Path: filepath.Join(dir, "store.json")}
	for _, data := range []string{"first", "second, longer"} {
		if err := fs.Save(context.Background(), []byte(data)); err != nil {
			t.Fatal(err)
		}
		got, err := fs.Load(context.Background())
		if err != nil || string(got) != data {
			t.Fatalf("Load = %q, %v; want %q", got, err, data)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("%d files beside the store, want only it", len(entries))
	}
}
