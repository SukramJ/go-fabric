// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package clock_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// sleepRatchet is the number of time.Sleep calls in the module's test files
// that do not import testing/synctest, and afterRatchet the number of
// time.After calls in the same files (phase 35, issue #395). A test that
// waits on the wall clock waits for a machine's speed: on a loaded runner the
// window it guesses is too short, on an idle one it only costs time. The
// three replacements are the site's quiet protocol (Site.Settle, Step, Run,
// Eventually), a clocktest.Fake the subject takes through its clock seam,
// and a synctest bubble, whose clock is virtual and advances only when every
// goroutine of the bubble is durably blocked.
//
// The rule is per file, because that is what a static check can state
// honestly: a sleep inside a synctest bubble is virtual time and
// deterministic, but whether a given call runs inside a bubble is a question
// of the call graph at run time (a helper called from synctest.Test and from
// a plain test is both). A file that imports testing/synctest is taken as
// running its sleeps in a bubble; every other file's sleeps count. Both
// constants are exact, in both directions: a new wall-clock wait fails the
// test, and a removed one fails it until the constant is lowered in the same
// commit, so the count only falls.
const (
	sleepRatchet = 90
	afterRatchet = 134
)

// TestSleepRatchet counts the wall-clock waits in every *_test.go of the
// module, under the walk rules of [TestNoRawTimeOutsideClock] (dot
// directories, testdata, node_modules and nested modules are not read), and
// holds the two counts at [sleepRatchet] and [afterRatchet].
func TestSleepRatchet(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	sleeps, afters := map[string]int{}, map[string]int{}
	files := 0
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if path == root {
				return nil
			}
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "testdata" || name == "node_modules" {
				return filepath.SkipDir
			}
			if _, statErr := os.Stat(filepath.Join(path, "go.mod")); statErr == nil {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		files++
		s, a := wallClockWaits(t, path)
		if s > 0 {
			sleeps[rel] = s
		}
		if a > 0 {
			afters[rel] = a
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk module: %v", err)
	}
	if files < 100 {
		t.Fatalf("read %d test files; the walk is not seeing the module", files)
	}
	checkRatchet(t, "time.Sleep", "sleepRatchet", sleeps, sleepRatchet)
	checkRatchet(t, "time.After", "afterRatchet", afters, afterRatchet)
}

func checkRatchet(t *testing.T, call, name string, perFile map[string]int, ratchet int) {
	t.Helper()
	got := 0
	for _, n := range perFile {
		got += n
	}
	if got == ratchet {
		return
	}
	keys := make([]string, 0, len(perFile))
	for k := range perFile {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "\n\t%s: %d", k, perFile[k])
	}
	if got > ratchet {
		t.Errorf("%d %s calls in test files outside synctest, ratchet %s = %d: a new wall-clock wait in a test: use the site's quiet protocol, a fake clock or a synctest bubble%s",
			got, call, name, ratchet, b.String())
		return
	}
	t.Errorf("%d %s calls in test files outside synctest, ratchet %s = %d: lower the ratchet to %d in the same commit%s",
		got, call, name, ratchet, got, b.String())
}

// wallClockWaits counts the time.Sleep and time.After calls of one test
// file, under whatever name it imports "time" as; a file that imports
// testing/synctest counts none.
func wallClockWaits(t *testing.T, path string) (sleeps, afters int) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	name := ""
	for _, imp := range file.Imports {
		switch imp.Path.Value {
		case `"testing/synctest"`:
			return 0, 0
		case `"time"`:
			name = "time"
			if imp.Name != nil {
				name = imp.Name.Name
			}
		}
	}
	if name == "" || name == "_" {
		return 0, 0
	}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); !ok || id.Name != name {
			return true
		}
		switch sel.Sel.Name {
		case "Sleep":
			sleeps++
		case "After":
			afters++
		}
		return true
	})
	return sleeps, afters
}
