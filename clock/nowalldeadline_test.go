// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package clock_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// pacedDirs are the packages a fabrictest site paces on its clock: the
// device's stack (bridge, node, im, groups, transport, mdns) and the
// client machinery its controllers run (client). A deadline in them is a
// deadline of the component's clock.
var pacedDirs = []string{"bridge", "node", "im", "client", "groups", "transport", "mdns"}

// wallDeadline are the context constructors whose deadline the standard
// library measures on the wall clock. clock.WithTimeout and
// clock.WithDeadline measure it on a clock.Clock. The wall-clock time
// functions (time.After, time.NewTimer, time.Sleep, …) are
// TestNoRawTimeOutsideClock's, module-wide.
var wallDeadline = map[string]bool{
	"WithTimeout": true, "WithDeadline": true, "WithTimeoutCause": true, "WithDeadlineCause": true,
}

// wallDeadlineKept are the trees under pacedDirs allowed a wall-clock
// deadline, and why. The mdns socket read deadlines are
// TestNoRawTimeOutsideClock's exception (they read time.Now, not a
// context).
var wallDeadlineKept = map[string]string{
	"bridge/bridgetest": "a test helper on the wall clock by design: its sessions and " +
		"commissioner bound a test's wait on the machine's time, the clock the test's " +
		"own go test -timeout runs on; it paces no stack of its own",
}

// TestNoWallClockDeadlineInPacedCode fails on a context.WithTimeout,
// context.WithDeadline or their Cause forms in non-test code of
// [pacedDirs] outside [wallDeadlineKept]: under a site on a fake clock such
// a deadline runs on the machine's time while the stack waits on the
// fake, so a loaded machine expires it (brief CG §1). A nested module
// (its own go.mod) is not this module.
func TestNoWallClockDeadlineInPacedCode(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	kept := map[string]int{}
	files := 0
	for _, dir := range pacedDirs {
		err = filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			if d.IsDir() {
				if d.Name() == "testdata" || strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				if _, statErr := os.Stat(filepath.Join(path, "go.mod")); statErr == nil && rel != dir {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
				return nil
			}
			files++
			for _, line := range wallDeadlineSites(t, path) {
				if k := keptTree(rel); k != "" {
					kept[k]++
					continue
				}
				found = append(found, rel+":"+strconv.Itoa(line))
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
	if files < 100 {
		t.Fatalf("read %d production files; the walk is not seeing the paced packages", files)
	}
	for _, f := range found {
		t.Errorf("%s: a wall-clock context deadline in paced code; use clock.WithTimeout / clock.WithDeadline on the component's clock", f)
	}
	for k, reason := range wallDeadlineKept {
		if kept[k] == 0 {
			t.Errorf("kept tree %s (%s) has no wall-clock deadline left; drop it from the list", k, reason)
		}
	}
}

func keptTree(rel string) string {
	for k := range wallDeadlineKept {
		if rel == k || strings.HasPrefix(rel, k+"/") {
			return k
		}
	}
	return ""
}

// wallDeadlineSites lists the lines of one file that name a [wallDeadline]
// constructor of package context, under whatever name the file imports it.
func wallDeadlineSites(t *testing.T, path string) []int {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	name := ""
	for _, imp := range file.Imports {
		if imp.Path.Value != `"context"` {
			continue
		}
		name = "context"
		if imp.Name != nil {
			name = imp.Name.Name
		}
	}
	if name == "" || name == "_" {
		return nil
	}
	var lines []int
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok || id.Name != name || !wallDeadline[sel.Sel.Name] {
			return true
		}
		lines = append(lines, fset.Position(sel.Pos()).Line)
		return true
	})
	return slices.Clip(lines)
}
