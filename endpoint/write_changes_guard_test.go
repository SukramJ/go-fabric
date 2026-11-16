// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// writeRecordUse matches every use of the deprecated write record outside
// this package: the context helper, and the type by its name — a composite
// literal, new(…), a declaration.
var writeRecordUse = regexp.MustCompile(`\b(WithWriteChanges|WriteChanges)\b`)

// TestNoWriteChangesOutsideEndpoint: the write verdict travels in the
// write's result (im.WriteResult.Commit, im.HandleWrite); the deprecated
// context record (WriteChanges, WithWriteChanges) is used by this package
// and its tests alone, so its removal touches nothing else (review II,
// R2-P5-write-01).
func TestNoWriteChangesOutsideEndpoint(t *testing.T) {
	t.Parallel()
	root := ".."
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("module root not at %s: %v", root, err)
	}
	uses, err := writeRecordUses(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range uses {
		t.Errorf("%s uses the deprecated write record; read im.WriteResult.Commit from im.HandleWrite instead", path)
	}
}

// TestWriteRecordWalkStaysInTheModule: the walk reads the module's own
// files — the nested modules of the repository included — and not a
// directory whose name starts with a dot: an agent's worktree under
// .claude/worktrees/ is another checkout, whose endpoint package the walk
// would otherwise read as a use outside this one.
func TestWriteRecordWalkStaysInTheModule(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for path, src := range map[string]string{
		"go.mod":                                        "module example\n",
		"bridge/a.go":                                   "package bridge\n\nvar w = new(endpoint.WriteChanges)\n",
		"endpoint/own.go":                               "package endpoint\n\nvar w WriteChanges\n",
		"nested/go.mod":                                 "module nested\n",
		"nested/b.go":                                   "package nested\n\nvar w endpoint.WriteChanges\n",
		".claude/worktrees/other/endpoint/x.go":         "package endpoint\n\nvar w WriteChanges\n",
		".claude/worktrees/other/bridge/y.go":           "package bridge\n\nvar w = endpoint.WithWriteChanges\n",
		"testdata/z.go":                                 "package z\n\nvar w endpoint.WriteChanges\n",
		"bridge/unrelated.go":                           "package bridge\n\nfunc MatterWriteChanged() {}\n",
		".github/workflows/notgo.go":                    "package workflows\n\nvar w endpoint.WriteChanges\n",
		"node_modules/pkg/c.go":                         "package pkg\n\nvar w endpoint.WriteChanges\n",
		"examples/reference-bridge/endpoint/inner.go":   "package endpoint\n\nvar w WriteChanges\n",
		"examples/reference-bridge/endpoint/inner.txt":  "WriteChanges\n",
		"examples/reference-bridge/endpoint/inner_t.go": "package endpoint\n",
	} {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	uses, err := writeRecordUses(root)
	if err != nil {
		t.Fatal(err)
	}
	for i, u := range uses {
		uses[i] = filepath.ToSlash(strings.TrimPrefix(u, root+string(filepath.Separator)))
	}
	want := []string{"bridge/a.go", "examples/reference-bridge/endpoint/inner.go", "nested/b.go"}
	if !slices.Equal(uses, want) {
		t.Errorf("uses = %v, want %v", uses, want)
	}
}

// writeRecordUses lists the .go files under root, outside root's own
// endpoint directory, that use the deprecated write record. It skips the
// directories that hold no source of the repository: a name starting with
// a dot below root (.git, .github, an agent's .claude/worktrees/), testdata
// and node_modules.
func writeRecordUses(root string) ([]string, error) {
	var uses []string
	own := filepath.Clean(filepath.Join(root, "endpoint"))
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && strings.HasPrefix(name, ".") || name == "testdata" || name == "node_modules" ||
				filepath.Clean(path) == own {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		b, err := os.ReadFile(path) //nolint:gosec // paths come from walking the module root
		if err != nil {
			return err
		}
		if writeRecordUse.Match(b) {
			uses = append(uses, path)
		}
		return nil
	})
	slices.Sort(uses)
	return uses, err
}
