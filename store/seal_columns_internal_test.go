// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// descriptorDriven are the functions whose seal calls take the table and
// column from secretTables itself rather than naming them.
var descriptorDriven = []string{"sealTable", "openOneSealed", "resealTable"}

// TestSecretColumnsAreTheSealCallSites (R2-P6-store-0109) holds the class
// "which columns are secrets is written in several places": every
// sealColumn and openColumn call of the package names a (table, column)
// of secretTables — by a constant, or by an index into a constant array —
// or sits in a function that iterates secretTables; and every column of
// secretTables is named by at least one call, so a secret added to a write
// without the descriptor, or a descriptor row no write uses, fails here.
func TestSecretColumnsAreTheSealCallSites(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var files []*ast.File
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, e.Name(), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	consts := map[string]string{}   // a string constant's value
	arrays := map[string][]string{} // a string array variable's elements
	for _, f := range files {
		for _, d := range f.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, sp := range g.Specs {
				v, ok := sp.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range v.Names {
					if i >= len(v.Values) {
						continue
					}
					switch x := v.Values[i].(type) {
					case *ast.BasicLit:
						if s, err := strconv.Unquote(x.Value); err == nil && g.Tok == token.CONST {
							consts[name.Name] = s
						}
					case *ast.CompositeLit:
						var elems []string
						for _, el := range x.Elts {
							if b, ok := el.(*ast.BasicLit); ok {
								if s, err := strconv.Unquote(b.Value); err == nil {
									elems = append(elems, s)
								}
							}
						}
						if len(elems) == len(x.Elts) && len(elems) > 0 {
							arrays[name.Name] = elems
						}
					}
				}
			}
		}
	}
	resolve := func(e ast.Expr) []string {
		switch x := e.(type) {
		case *ast.BasicLit:
			if s, err := strconv.Unquote(x.Value); err == nil {
				return []string{s}
			}
		case *ast.Ident:
			if s, ok := consts[x.Name]; ok {
				return []string{s}
			}
		case *ast.IndexExpr:
			if id, ok := x.X.(*ast.Ident); ok {
				return arrays[id.Name]
			}
		}
		return nil
	}
	want := map[string]bool{}
	for _, tc := range secretTables {
		for _, c := range tc.secrets {
			want[tc.table+"."+c] = true
		}
	}
	named := map[string]bool{}
	driven := map[string]bool{}
	for _, f := range files {
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				var ti int
				switch sel.Sel.Name {
				case "sealColumn":
					ti = 1
				case "openColumn":
					ti = 2
				default:
					return true
				}
				pos := fset.Position(call.Pos())
				tables, columns := resolve(call.Args[ti]), resolve(call.Args[ti+1])
				if len(tables) == 0 || len(columns) == 0 {
					if !slices.Contains(descriptorDriven, fn.Name.Name) {
						t.Errorf("%s: %s in %s names its table or column by an expression this test cannot read; name a constant or iterate secretTables", pos, sel.Sel.Name, fn.Name.Name)
					}
					driven[fn.Name.Name] = true
					return true
				}
				for _, tb := range tables {
					for _, c := range columns {
						if !want[tb+"."+c] {
							t.Errorf("%s: %s of %s.%s, which secretTables does not list", pos, sel.Sel.Name, tb, c)
						}
						named[tb+"."+c] = true
					}
				}
				return true
			})
		}
	}
	for _, col := range slices.Sorted(maps.Keys(want)) {
		if !named[col] {
			t.Errorf("secretTables lists %s, which no sealColumn or openColumn call names", col)
		}
	}
	for _, fn := range descriptorDriven {
		if !driven[fn] {
			t.Errorf("descriptorDriven names %s, which makes no seal call by secretTables (stale)", fn)
		}
	}
}
