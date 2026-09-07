// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package mrp_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// constructorsWithoutCaller is the ratchet of exported constructors in this
// package that no non-test code in the module calls. Each entry carries the
// reason it is still here; a new constructor lands with a caller or with an
// entry, never silently.
var constructorsWithoutCaller = map[string]string{
	"NewRetransmitter": "unwired stand-alone tracker; the bridge keeps its own per-session one " +
		"(bridge/outbound_reliable.go). Kept as public API under README.md's deprecation " +
		"policy — removing or deprecating it is a CHANGELOG-announced decision, not a cleanup.",
}

// TestEveryConstructorHasAProductionCaller guards this package against a
// second unwired primitive whose doc comment names a consumer that does
// not exist. The Retransmitter's did for a long time ("the UDP loop wakes
// the retransmitter"), and a reader attributed MRP retransmission to a
// type nothing constructs.
//
// The check is the module's own wiring rule turned on a library package:
// an exported New* in a non-test file here must be referenced as
// `mrp.New*` from a non-test file somewhere else in the module, or be
// listed in [constructorsWithoutCaller] with a reason.
func TestEveryConstructorHasAProductionCaller(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)
	pkgDir := filepath.Join(root, "transport", "mrp")

	constructors := exportedConstructors(t, pkgDir)
	if len(constructors) == 0 {
		t.Fatal("no exported constructors found in transport/mrp — the scan is broken")
	}

	callers := make(map[string]bool, len(constructors))
	references := make(map[string]*regexp.Regexp, len(constructors))
	for _, name := range constructors {
		references[name] = regexp.MustCompile(`\bmrp\.` + name + `\b`)
	}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch {
			case d.Name() == ".git", d.Name() == "testdata", d.Name() == "node_modules",
				strings.HasPrefix(d.Name(), "."), path == pkgDir:
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		for name, re := range references {
			if re.Match(src) {
				callers[name] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk module: %v", err)
	}

	for _, name := range constructors {
		_, ratcheted := constructorsWithoutCaller[name]
		switch {
		case callers[name] && ratcheted:
			t.Errorf("mrp.%s has a production caller now — drop it from constructorsWithoutCaller", name)
		case !callers[name] && !ratcheted:
			t.Errorf("mrp.%s is exported but nothing outside transport/mrp constructs it. Either wire it, "+
				"or add it to constructorsWithoutCaller with the reason it stays", name)
		}
	}
	for name := range constructorsWithoutCaller {
		found := false
		for _, c := range constructors {
			if c == name {
				found = true
			}
		}
		if !found {
			t.Errorf("constructorsWithoutCaller names %s, which no longer exists — drop the entry", name)
		}
	}
}

// exportedConstructors returns the names of the exported package-level
// `func New*` declarations in dir's non-test files.
func exportedConstructors(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	fset := token.NewFileSet()
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !fn.Name.IsExported() || !strings.HasPrefix(fn.Name.Name, "New") {
				continue
			}
			out = append(out, fn.Name.Name)
		}
	}
	return out
}

// moduleRoot walks up from the test's working directory to the go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}
