// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPackageDocNamesEveryConstructor (review II, R2-P6-doc-1260): the
// package doc's list of servers names every exported New… constructor of
// the package, so a server added — or one the doc calls unmounted — cannot
// drift from the list again.
func TestPackageDocNamesEveryConstructor(t *testing.T) {
	t.Parallel()
	doc, err := os.ReadFile("doc.go")
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range af.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !fn.Name.IsExported() || !strings.HasPrefix(fn.Name.Name, "New") {
				continue
			}
			if !strings.Contains(string(doc), "["+fn.Name.Name+"]") {
				t.Errorf("doc.go does not name %s (%s)", fn.Name.Name, f)
			}
		}
	}
}
