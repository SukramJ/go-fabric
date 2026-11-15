// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestNoWriteChangesOutsideEndpoint: the write verdict travels in the
// write's result (im.WriteResult.Commit, im.HandleWrite); the deprecated
// context record (WriteChanges, WithWriteChanges) is used by this package
// and its tests alone, so its removal touches nothing else (review II,
// R2-P5-write-01).
func TestNoWriteChangesOutsideEndpoint(t *testing.T) {
	t.Parallel()
	use := regexp.MustCompile(`\b(WithWriteChanges|WriteChanges\{)`)
	root := ".."
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("module root not at %s: %v", root, err)
	}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "testdata":
				return filepath.SkipDir
			}
			if filepath.Clean(path) == filepath.Clean(filepath.Join(root, "endpoint")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if loc := use.FindIndex(b); loc != nil {
			t.Errorf("%s uses the deprecated write record; read im.WriteResult.Commit from im.HandleWrite instead", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
