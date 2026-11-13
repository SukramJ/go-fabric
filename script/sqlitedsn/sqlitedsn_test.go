// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package sqlitedsn guards the one way a SQLite `file:` URI is built in
// this module: by store.OpenSQLite (store/sqlite.go), which assembles it
// with net/url. A URI concatenated from a path — "file:"+path+"?_pragma=…"
// — ends its file name at the first `#` or `?` of the path and drops the
// pragmas behind it (review 2026-11 W5-ex-01), and t.TempDir keeps `#` in
// the directory it derives from a test's name, which Go gives a subtest
// run twice under one name ("name#01"; review 2026-11-13 R2-P0-test-C02).
//
// The test parses every Go file of the module with go/parser (no type
// checking), test files and generated files included, outside notes/,
// docs/, .claude/, .git/ and any testdata directory, and refuses
//
//   - a string concatenation whose left operand is a literal that starts
//     with "file:";
//   - a fmt.Sprintf whose format literal starts with "file:" and holds a
//     %s, %v or %q verb (a format with %d only names no path).
//
// The exception is counted, not a rule: the `file:<path>` key reference of
// controller/ca/keyfile (Signer.KeyRef, the --ca-key flag of the reference
// controller) is a scheme of its own and no SQLite URI. [keyReferences]
// lists the files that build it with the number of sites each holds; a
// further site in one of them fails like a site anywhere else, and a
// removed one fails until its count is lowered in the same commit.
package sqlitedsn

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// minFiles is the floor on parsed files: fewer means the walk broke and
// the guard is silently checking nothing.
const minFiles = 2000

// keyReferences are the files that build the keyfile signer's
// `file:<path>` key reference, with the number of sites in each.
var keyReferences = map[string]int{
	"controller/ca/keyfile/keyfile.go":                1,
	"controller/ca/keyfile/keyfile_test.go":           1,
	"examples/reference-controller/cli/cakey_test.go": 3,
}

// skippedTop are the top-level directories the guard does not judge.
var skippedTop = map[string]bool{
	"notes":   true,
	"docs":    true,
	".claude": true,
	".git":    true,
}

// pathVerb matches a verb that formats a string operand: %s, %v or %q,
// tolerating flags and width between % and the verb.
var pathVerb = regexp.MustCompile(`%[-+# 0-9.]*[svq]`)

func TestNoFileURIIsConcatenated(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	var (
		files int
		sites = map[string][]string{}
	)
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() {
			if rel != "." && (skippedTop[strings.SplitN(rel, string(filepath.Separator), 2)[0]] || d.Name() == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			return fmt.Errorf("sqlitedsn: parse %s: %w", rel, perr)
		}
		files++
		rel = filepath.ToSlash(rel)
		if found := concatenations(fset, f, rel); len(found) > 0 {
			sites[rel] = found
		}
		return nil
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}
	if files < minFiles {
		t.Fatalf("only %d Go files parsed, want at least %d: the walk is broken", files, minFiles)
	}
	rels := make([]string, 0, len(sites))
	for rel := range sites {
		rels = append(rels, rel)
	}
	slices.Sort(rels)
	for _, rel := range rels {
		found := sites[rel]
		if want, ok := keyReferences[rel]; ok {
			if len(found) != want {
				t.Errorf("%s holds %d \"file:\" sites, keyReferences counts %d key references: a SQLite URI is opened with store.OpenSQLite, a removed key reference lowers the count\n  %s",
					rel, len(found), want, strings.Join(found, "\n  "))
			}
			continue
		}
		for _, site := range found {
			t.Errorf("%s: a SQLite file: URI is built by store.OpenSQLite (net/url), not by concatenation — a path holding '#' or '?' ends the file name early and drops the pragmas", site)
		}
	}
	for rel, want := range keyReferences {
		if _, ok := sites[rel]; !ok {
			t.Errorf("%s holds no \"file:\" site, keyReferences counts %d: remove the entry", rel, want)
		}
	}
	t.Logf("parsed %d Go files", files)
}

// concatenations returns the position and text of every site of f that
// builds a "file:" string from parts.
func concatenations(fset *token.FileSet, f *ast.File, rel string) []string {
	var out []string
	at := func(pos token.Pos, what string) {
		out = append(out, fmt.Sprintf("%s:%d: %s", rel, fset.Position(pos).Line, what))
	}
	ast.Inspect(f, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.BinaryExpr:
			if text, ok := stringLiteral(n.X); ok && n.Op == token.ADD && strings.HasPrefix(text, "file:") {
				at(n.Pos(), strconv.Quote(text)+" + …")
			}
		case *ast.CallExpr:
			sel, ok := n.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Sprintf" || len(n.Args) == 0 {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "fmt" {
				return true
			}
			if text, ok := stringLiteral(n.Args[0]); ok && strings.HasPrefix(text, "file:") && pathVerb.MatchString(text) {
				at(n.Pos(), "fmt.Sprintf("+strconv.Quote(text)+", …)")
			}
		}
		return true
	})
	return out
}

// stringLiteral returns the value of e when it is a string literal,
// parenthesised or not.
func stringLiteral(e ast.Expr) (string, bool) {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			break
		}
		e = p.X
	}
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	text, err := strconv.Unquote(lit.Value)
	return text, err == nil
}

// TestConcatenationsAreFound holds the matcher itself: what it refuses and
// what it lets through.
func TestConcatenationsAreFound(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		expr string
		want int
	}{
		{"path and pragmas", `"file:" + path + "?_pragma=foreign_keys(1)"`, 1},
		{"path alone", `"file:" + path`, 1},
		{"parenthesised literal", `("file:") + path`, 1},
		{"raw literal", "`file:` + path", 1},
		{"authority form", `"file://" + path`, 1},
		{"Sprintf with a string verb", `fmt.Sprintf("file:%s?mode=ro", path)`, 1},
		{"Sprintf with a quoted verb", `fmt.Sprintf("file:%q", path)`, 1},
		{"Sprintf with a number only", `fmt.Sprintf("file:mem-%d?mode=memory&cache=shared", n)`, 0},
		{"the literal alone", `"file:rel.db"`, 0},
		{"the literal on the right", `prefix + "file:"`, 0},
		{"another scheme", `"https:" + path`, 0},
		{"a prefix check", `strings.HasPrefix(ref, "file:")`, 0},
	} {
		src := "package p\n\nvar _ = " + c.expr + "\n"
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "p.go", src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := concatenations(fset, f, "p.go"); len(got) != c.want {
			t.Errorf("%s: %d sites %q, want %d", c.name, len(got), got, c.want)
		}
	}
}
