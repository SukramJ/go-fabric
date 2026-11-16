// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package quiescecheck guards that a cluster server package which arms
// timers can stop them (review II, R2-P1-clu-02, guard G5).
//
// A server that leaves the topology — a reassembly that drops its
// endpoint, the bridge's Stop — is told so through
// contract.ClusterQuiescer.MatterQuiesce, which ends its timers so none
// outlives the bridge's use of the server (contract/matter.go). A package
// under cluster/ whose production code arms a timer (a call of a method
// named AfterFunc, NewTimer or NewTicker) and declares no MatterQuiesce
// method has no way to be told; its timers run on after the server is
// gone.
//
// The rule is per package directory and syntactic: it sees a timer call
// and a MatterQuiesce declaration, not whether the declaration stops that
// timer, nor whether a timer outlives Stop at all — a timer every path
// stops by itself needs no quiesce. Such a package is an entry of
// [exceptions] with its reason. The list is exact: a new package with
// timers and no MatterQuiesce fails, and so does an entry whose package
// no longer needs it.
package quiescecheck

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// repoRoot is the module root, two levels above this package.
const repoRoot = "../.."

// exceptions are the packages under cluster/ that arm timers and declare
// no MatterQuiesce, with the reason each may. Measured on 2026-11-15
// (review II, phase 5a); whether each timer outlives a Stop is the open
// question of N-clumisc-4 (phase 6), so the reasons below say what the
// timer is, not that it is safe.
var exceptions = map[string]string{
	"cluster":            "attribute_changes.go: the Quieter's throttle timer, held by the server that embeds the Quieter (N-clumisc-4)",
	"cluster/cover":      "windowcovering_server.go: the WindowCovering movement step timer (N-clumisc-4)",
	"cluster/transition": "transition.go: the Engine's step timer, held by the server that drives the Engine (N-clumisc-4)",
}

var timerMethods = map[string]bool{"AfterFunc": true, "NewTimer": true, "NewTicker": true}

type pkgFacts struct {
	timers  []string // file:line of each timer call
	quiesce bool
}

// TestTimerPackagesQuiesce holds the rule of the package documentation.
func TestTimerPackagesQuiesce(t *testing.T) {
	t.Parallel()
	root := filepath.Join(repoRoot, "cluster")
	pkgs := map[string]*pkgFacts{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && (d.Name() == "testdata" || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(repoRoot, filepath.Dir(p))
		rel = filepath.ToSlash(rel)
		facts := pkgs[rel]
		if facts == nil {
			facts = &pkgFacts{}
			pkgs[rel] = facts
		}
		for _, decl := range f.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok && fd.Recv != nil && fd.Name.Name == "MatterQuiesce" {
				facts.quiesce = true
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && timerMethods[sel.Sel.Name] {
				pos := fset.Position(call.Pos())
				facts.timers = append(facts.timers, filepath.Base(pos.Filename)+":"+strconv.Itoa(pos.Line))
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) < 20 {
		t.Fatalf("only %d packages under cluster/: the walk broke", len(pkgs))
	}
	names := make([]string, 0, len(pkgs))
	for name := range pkgs {
		names = append(names, name)
	}
	sort.Strings(names)
	quiescers := 0
	for _, name := range names {
		facts := pkgs[name]
		if facts.quiesce {
			quiescers++
		}
		needs := len(facts.timers) > 0 && !facts.quiesce
		_, excepted := exceptions[name]
		switch {
		case needs && !excepted:
			t.Errorf("%s arms timers (%s) and declares no MatterQuiesce: implement contract.ClusterQuiescer so the timers end with the server, or add an exception with the reason none outlives it",
				name, strings.Join(facts.timers, ", "))
		case !needs && excepted:
			t.Errorf("%s is an exception but no longer needs one (timers: %d, MatterQuiesce: %v): delete the entry", name, len(facts.timers), facts.quiesce)
		}
	}
	for name := range exceptions {
		if _, ok := pkgs[name]; !ok {
			t.Errorf("exception %s names no package under cluster/: delete the entry", name)
		}
	}
	if quiescers == 0 {
		t.Fatal("no package under cluster/ declares MatterQuiesce: the match broke")
	}
}
