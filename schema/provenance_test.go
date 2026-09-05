// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package schema_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/SukramJ/go-fabric/parity"
	"github.com/SukramJ/go-fabric/schema"
)

// TestMatterSchemaSnapshotHashMatchesEmbedded recomputes the SHA-256 of the
// embedded matter.js schema snapshot at test time and asserts it equals
// schema.SchemaSnapshotSHA256 — the constant the generator emits. A
// divergence means either:
//   - a generated constant was hand-edited without re-running the generator,
//   - the snapshot was refreshed without regenerating the Go schema files.
//
// The snapshot the generator reads and the snapshot parity embeds are the
// same bytes: parity/schema.json is what both the generator and every
// parity test consume, so hashing the embed is hashing the generator's own
// input rather than a second copy of it.
func TestMatterSchemaSnapshotHashMatchesEmbedded(t *testing.T) {
	t.Parallel()

	sum := sha256.Sum256(parity.SchemaJSON())
	got := hex.EncodeToString(sum[:])

	if got != schema.SchemaSnapshotSHA256 {
		t.Errorf("snapshot SHA-256 mismatch:\n  embedded:  %s\n  generated: %s\n\nRegenerate the schema package from parity/schema.json to resync.",
			got, schema.SchemaSnapshotSHA256)
	}
}

// moduleRoot walks up from this source file until it finds go.mod. Counting
// parent directories instead would silently resolve to the wrong tree the
// day a file moves; walking to the module marker is independent both of the
// working directory `go test` ran from and of where this package sits.
func moduleRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed: cannot locate the module root")
	}
	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found above %s", filepath.Dir(thisFile))
		}
		dir = parent
	}
}
