// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// The standing guard of R2-P6-conc-1003's class: no function of the
// package that releases nocMu by a plain `defer o.nocMu.Unlock()` reports a
// rollback (reportRolledBack runs the host's fabric hooks, which the rule
// at nocMu keeps out of the lock). A function that must report releases
// nocMu first, in one deferred func (AddNOC, UpdateNOC).
func TestNOCLockNeverHeldAcrossRollbackReport(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			plainDefer, reports := false, false
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.DeferStmt:
					if sel, ok := n.Call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Unlock" {
						if inner, ok := sel.X.(*ast.SelectorExpr); ok && inner.Sel.Name == "nocMu" {
							plainDefer = true
						}
					}
				case *ast.SelectorExpr:
					if n.Sel.Name == "reportRolledBack" {
						reports = true
					}
				}
				return true
			})
			if plainDefer && reports {
				t.Errorf("%s: %s defers nocMu.Unlock and reports a rollback: the report would run with nocMu held", fset.Position(fn.Pos()), fn.Name.Name)
			}
		}
	}
}
