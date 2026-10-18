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

// rawTime are the time-package functions that read or wait on the wall
// clock. Production code takes them from a clock.Clock instead.
var rawTime = map[string]bool{
	"Now": true, "AfterFunc": true, "NewTimer": true, "NewTicker": true,
	"Sleep": true, "After": true, "Tick": true, "Since": true, "Until": true,
}

// rawTimeException is a production site allowed to keep a raw time call:
// how many uses of the function the file has, and why.
type rawTimeException struct {
	n      int
	reason string
}

// rawTimeExceptions are keyed "path/to/file.go:Func". The goal is an
// empty list; every entry names the reason a clock.Clock cannot stand in.
var rawTimeExceptions = map[string]rawTimeException{
	"mdns/subtype_responder.go:Now": {2, "the two receive loops' socket read deadlines " +
		"(SetReadDeadline): the kernel judges a deadline against the wall clock, so a " +
		"fake clock's instant would expire it at once and spin the loop; the deadline " +
		"only bounds a blocking read so the loop can see its done channel"},
}

// skipDirs are the trees the guard does not read: this package (it is the
// port), the runnable examples and the build scripts (package main, not
// part of the library).
var skipDirs = []string{"clock", "examples", "script"}

// TestNoRawTimeOutsideClock fails on a call of — or a reference to — one of
// the [rawTime] functions in any non-test .go file of the module outside
// [skipDirs]. A nested module (its own go.mod) is not this module.
func TestNoRawTimeOutsideClock(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	used := map[string]int{}
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
			if strings.HasPrefix(name, ".") || name == "testdata" || name == "node_modules" ||
				slices.Contains(skipDirs, rel) {
				return filepath.SkipDir
			}
			if _, statErr := os.Stat(filepath.Join(path, "go.mod")); statErr == nil {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		files++
		for _, site := range rawTimeSites(t, path) {
			key := rel + ":" + site.fn
			if _, ok := rawTimeExceptions[key]; ok {
				used[key]++
				continue
			}
			found = append(found, rel+":"+strconv.Itoa(site.line)+": time."+site.fn)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk module: %v", err)
	}
	if files < 100 {
		t.Fatalf("read %d production files; the walk is not seeing the module", files)
	}
	for _, f := range found {
		t.Errorf("%s: production code takes its time from a clock.Clock (package clock), not the time package", f)
	}
	for key, ex := range rawTimeExceptions {
		if used[key] != ex.n {
			t.Errorf("exception %s (%s) allows %d uses but the file has %d; the list must match the code", key, ex.reason, ex.n, used[key])
		}
	}
}

type rawSite struct {
	fn   string
	line int
}

// rawTimeSites lists the uses of the time package's wall-clock functions
// in one file, under whatever name the file imports "time" as.
func rawTimeSites(t *testing.T, path string) []rawSite {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	name := ""
	for _, imp := range file.Imports {
		if imp.Path.Value != `"time"` {
			continue
		}
		name = "time"
		if imp.Name != nil {
			name = imp.Name.Name
		}
	}
	if name == "" || name == "_" {
		return nil
	}
	var sites []rawSite
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok || id.Name != name || !rawTime[sel.Sel.Name] {
			return true
		}
		sites = append(sites, rawSite{fn: sel.Sel.Name, line: fset.Position(sel.Pos()).Line})
		return true
	})
	return sites
}
