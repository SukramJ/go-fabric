// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package sessionseam_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const seamPath = "github.com/SukramJ/go-fabric/internal/sessionseam"

// installer is the one package outside the tests that may name OpenAt: it
// installs the hook.
const installer = "secure/operational"

// TestOpenAtIsNamedByTestsOnly (review II, decision 160) holds "the install
// under a chosen id is test support": no non-test Go file of the tree —
// nested modules included — names sessionseam.OpenAt, except the package
// that installs it. A production install takes a Stake.
func TestOpenAtIsNamedByTestsOnly(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	var offenders []string
	files := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (strings.HasPrefix(name, ".") || name == "testdata" || name == "vendor" || name == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if filepath.ToSlash(filepath.Dir(rel)) == installer {
			return nil
		}
		files++
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		local := ""
		for _, imp := range f.Imports {
			if p, _ := strconv.Unquote(imp.Path.Value); p == seamPath {
				local = "sessionseam"
				if imp.Name != nil {
					local = imp.Name.Name
				}
			}
		}
		if local == "" {
			return nil
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "OpenAt" {
				return true
			}
			if x, ok := sel.X.(*ast.Ident); ok && x.Name == local {
				offenders = append(offenders, fset.Position(sel.Pos()).String())
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 500 {
		t.Fatalf("only %d non-test Go files under the module root; the walk is broken", files)
	}
	for _, o := range offenders {
		t.Errorf("%s names sessionseam.OpenAt outside a test: the install under a chosen id is test support; a production install takes a Stake (decision 160)", o)
	}
}
