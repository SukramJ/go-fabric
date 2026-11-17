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
		offenders = append(offenders, openAtNames(fset, f)...)
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

// openAtNames returns where f names the seam's OpenAt: a selector on the
// name f imports the seam under, and — under a dot import, which names
// OpenAt bare — every identifier OpenAt that names no field (a
// selector's, a struct type's or a composite literal key's; review II,
// R2-P6-test-2417).
func openAtNames(fset *token.FileSet, f *ast.File) []string {
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
	var out []string
	fields := map[*ast.Ident]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.Field:
			for _, name := range x.Names {
				fields[name] = true
			}
		case *ast.KeyValueExpr:
			if id, ok := x.Key.(*ast.Ident); ok {
				fields[id] = true
			}
		case *ast.SelectorExpr:
			fields[x.Sel] = true
			if id, ok := x.X.(*ast.Ident); ok && x.Sel.Name == "OpenAt" && id.Name == local {
				out = append(out, fset.Position(x.Pos()).String())
			}
		case *ast.Ident:
			if local == "." && x.Name == "OpenAt" && !fields[x] {
				out = append(out, fset.Position(x.Pos()).String())
			}
		}
		return true
	})
	return out
}

// TestOpenAtNamesFindsEveryForm (review II, R2-P6-test-2417): the guard
// finds OpenAt named through the import's name, an alias and a dot
// import, and not a field of another value named OpenAt.
func TestOpenAtNamesFindsEveryForm(t *testing.T) {
	t.Parallel()
	for src, want := range map[string]int{
		"package p\nimport \"" + seamPath + "\"\nvar _ = sessionseam.OpenAt\n":                     1,
		"package p\nimport s \"" + seamPath + "\"\nvar _ = s.OpenAt\n":                             1,
		"package p\nimport . \"" + seamPath + "\"\nvar _ = OpenAt\n":                               1,
		"package p\nimport . \"" + seamPath + "\"\nvar x struct{ OpenAt int }\nvar _ = x.OpenAt\n": 0,
		"package p\nvar x struct{ OpenAt int }\nvar _ = x.OpenAt\n":                                0,
	} {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "p.go", src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		if got := len(openAtNames(fset, f)); got != want {
			t.Errorf("%q: %d names of OpenAt, want %d", src, got, want)
		}
	}
}
