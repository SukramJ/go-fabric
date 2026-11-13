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
// A plain path concatenated with its parameters — path+"?_pragma=…" —
// has the same hazard: modernc.org/sqlite cuts a DSN without the `file:`
// prefix at its first `?`, so a path holding one names another file and
// the pragmas are read from the wrong place (R2-P0-rv-C09).
//
// The test parses every Go file of the module with go/parser (no type
// checking), test files and generated files included, outside notes/,
// docs/, .claude/, .git/ and any testdata directory, and refuses
//
//   - a string concatenation (a chain of +, or += onto a string) whose
//     first operand starts with "file:", or one of whose later operands is
//     a SQLite query that starts with "?" and a parameter of the driver
//     ([sqliteQuery]: _pragma, mode, cache, …);
//   - the same parts joined by strings.Join over a []string literal, the
//     separator counted as a part between them;
//   - a fmt.Sprintf whose format starts with "file:" and holds a %s, %v or
//     %q verb (a format with %d only names no path), or holds such a verb
//     with a SQLite query after it.
//
// An operand is read as a string when it is a literal or a name the file
// binds to a literal (const, var, := or =; a package-level const or var
// of another file of the package too) — by name, without type checking:
// a name bound to several literals is read as each of them, so a shadowed
// name is judged by every literal it was given.
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

// sqliteParam is a URI parameter modernc.org/sqlite or SQLite itself reads
// from a DSN's query, followed by its "=".
const sqliteParam = `(?:_pragma|_txlock|_time_format|mode|cache|vfs|immutable|nolock|psow)=`

var (
	// sqliteQuery matches a part that opens a DSN's query.
	sqliteQuery = regexp.MustCompile(`^\?` + sqliteParam)
	// sqliteParamStart matches a part that starts with a parameter — the
	// part after a "?" separator of strings.Join.
	sqliteParamStart = regexp.MustCompile(`^` + sqliteParam)
	// pathThenQuery matches a format that formats a path and then opens a
	// DSN's query.
	pathThenQuery = regexp.MustCompile(`%[-+# 0-9.]*[svq][^?]*\?` + sqliteParam)
)

// parsed is one parsed Go file.
type parsed struct {
	fset *token.FileSet
	f    *ast.File
	rel  string
}

func TestNoFileURIIsConcatenated(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	var (
		files int
		sites = map[string][]string{}
		byDir = map[string][]parsed{}
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
		dir := filepath.Dir(rel)
		byDir[dir] = append(byDir[dir], parsed{fset: fset, f: f, rel: filepath.ToSlash(rel)})
		return nil
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}
	for _, pkg := range byDir {
		for _, p := range pkg {
			if found := concatenations(p.fset, p.f, p.rel, packageStrings(pkg, p.f)); len(found) > 0 {
				sites[p.rel] = found
			}
		}
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
			t.Errorf("%s: a SQLite DSN is built by store.OpenSQLite (net/url), not by concatenation — a path holding '#' or '?' ends the file name early and drops the pragmas", site)
		}
	}
	for rel, want := range keyReferences {
		if _, ok := sites[rel]; !ok {
			t.Errorf("%s holds no \"file:\" site, keyReferences counts %d: remove the entry", rel, want)
		}
	}
	t.Logf("parsed %d Go files", files)
}

// names maps a name to every string literal it is bound to.
type names map[string][]string

// bind records the string literals rhs binds to the identifiers of lhs,
// position by position.
func (n names) bind(lhs, rhs []ast.Expr) {
	if len(lhs) != len(rhs) {
		return
	}
	for i, l := range lhs {
		id, ok := l.(*ast.Ident)
		if !ok || id.Name == "_" {
			continue
		}
		if text, ok := stringLiteral(rhs[i]); ok {
			n[id.Name] = append(n[id.Name], text)
		}
	}
}

// bindSpec records a const or var spec's string literals.
func (n names) bindSpec(spec ast.Spec) {
	vs, ok := spec.(*ast.ValueSpec)
	if !ok {
		return
	}
	lhs := make([]ast.Expr, len(vs.Names))
	for i, id := range vs.Names {
		lhs[i] = id
	}
	n.bind(lhs, vs.Values)
}

// packageStrings returns the names f binds to string literals at any
// level, and the names the other files of its package bind at package
// level.
func packageStrings(pkg []parsed, f *ast.File) names {
	n := names{}
	for _, p := range pkg {
		if p.f == f {
			continue
		}
		for _, d := range p.f.Decls {
			if gd, ok := d.(*ast.GenDecl); ok {
				for _, spec := range gd.Specs {
					n.bindSpec(spec)
				}
			}
		}
	}
	ast.Inspect(f, func(node ast.Node) bool {
		switch x := node.(type) {
		case *ast.ValueSpec:
			n.bindSpec(x)
		case *ast.AssignStmt:
			if x.Tok == token.DEFINE || x.Tok == token.ASSIGN {
				n.bind(x.Lhs, x.Rhs)
			}
		}
		return true
	})
	return n
}

// fileStrings is packageStrings for a lone file.
func fileStrings(f *ast.File) names {
	return packageStrings([]parsed{{f: f}}, f)
}

// concatenations returns the position and text of every site of f that
// builds a SQLite DSN from parts; n are the strings names are bound to.
func concatenations(fset *token.FileSet, f *ast.File, rel string, n names) []string {
	var out []string
	at := func(pos token.Pos, what string) {
		out = append(out, fmt.Sprintf("%s:%d: %s", rel, fset.Position(pos).Line, what))
	}
	values := func(e ast.Expr) []string {
		e = unparen(e)
		if text, ok := stringLiteral(e); ok {
			return []string{text}
		}
		if id, ok := e.(*ast.Ident); ok {
			return n[id.Name]
		}
		return nil
	}
	some := func(e ast.Expr, match func(string) bool) (string, bool) {
		for _, v := range values(e) {
			if match(v) {
				return v, true
			}
		}
		return "", false
	}
	isFile := func(v string) bool { return strings.HasPrefix(v, "file:") }
	// chain judges the parts of one concatenation and reports it once.
	chain := func(pos token.Pos, parts []ast.Expr, how string) {
		if v, ok := some(parts[0], isFile); ok {
			at(pos, how+": "+strconv.Quote(v)+" + …")
			return
		}
		for _, p := range parts[1:] {
			if v, ok := some(p, sqliteQuery.MatchString); ok {
				at(pos, how+": … + "+strconv.Quote(v))
				return
			}
		}
	}
	var visit func(node ast.Node) bool
	inspectAll := func(parts []ast.Expr) {
		for _, p := range parts {
			ast.Inspect(p, visit)
		}
	}
	visit = func(node ast.Node) bool {
		switch x := node.(type) {
		case *ast.BinaryExpr:
			if x.Op != token.ADD {
				return true
			}
			parts := flatten(x)
			chain(x.Pos(), parts, "+")
			inspectAll(parts)
			return false
		case *ast.AssignStmt:
			if x.Tok != token.ADD_ASSIGN || len(x.Lhs) != 1 || len(x.Rhs) != 1 {
				return true
			}
			parts := append([]ast.Expr{x.Lhs[0]}, flatten(x.Rhs[0])...)
			chain(x.Pos(), parts, "+=")
			inspectAll(parts)
			return false
		case *ast.CallExpr:
			switch {
			case isCall(x, "strings", "Join") && len(x.Args) == 2:
				lit, ok := unparen(x.Args[0]).(*ast.CompositeLit)
				if !ok || len(lit.Elts) == 0 {
					return true
				}
				if _, ok := some(x.Args[1], func(v string) bool { return v == "?" }); ok {
					for _, e := range lit.Elts[1:] {
						if v, ok := some(e, sqliteParamStart.MatchString); ok {
							at(x.Pos(), "strings.Join: … \"?\" "+strconv.Quote(v))
							return true
						}
					}
				}
				chain(x.Pos(), lit.Elts, "strings.Join")
			case isCall(x, "fmt", "Sprintf") && len(x.Args) > 0:
				if v, ok := some(x.Args[0], func(v string) bool {
					return (isFile(v) && pathVerb.MatchString(v)) || pathThenQuery.MatchString(v)
				}); ok {
					at(x.Pos(), "fmt.Sprintf("+strconv.Quote(v)+", …)")
				}
			}
		}
		return true
	}
	ast.Inspect(f, visit)
	return out
}

// flatten returns the operands of a chain of string additions, left to
// right, through parentheses.
func flatten(e ast.Expr) []ast.Expr {
	e = unparen(e)
	if b, ok := e.(*ast.BinaryExpr); ok && b.Op == token.ADD {
		return append(flatten(b.X), flatten(b.Y)...)
	}
	return []ast.Expr{e}
}

// isCall reports whether c calls pkg.name.
func isCall(c *ast.CallExpr, pkg, name string) bool {
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == pkg
}

// unparen strips the parentheses around e.
func unparen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}

// stringLiteral returns the value of e when it is a string literal,
// parenthesised or not.
func stringLiteral(e ast.Expr) (string, bool) {
	lit, ok := unparen(e).(*ast.BasicLit)
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
		name  string
		expr  string
		want  int
		decls string
	}{
		{"path and pragmas", `"file:" + path + "?_pragma=foreign_keys(1)"`, 1, ""},
		{"path alone", `"file:" + path`, 1, ""},
		{"parenthesised literal", `("file:") + path`, 1, ""},
		{"raw literal", "`file:` + path", 1, ""},
		{"authority form", `"file://" + path`, 1, ""},
		{"Sprintf with a string verb", `fmt.Sprintf("file:%s?mode=ro", path)`, 1, ""},
		{"Sprintf with a quoted verb", `fmt.Sprintf("file:%q", path)`, 1, ""},
		{"Sprintf with a number only", `fmt.Sprintf("file:mem-%d?mode=memory&cache=shared", n)`, 0, ""},
		{"the literal alone", `"file:rel.db"`, 0, ""},
		{"the literal on the right", `prefix + "file:"`, 0, ""},
		{"another scheme", `"https:" + path`, 0, ""},
		{"a prefix check", `strings.HasPrefix(ref, "file:")`, 0, ""},
		// R2-P0-rv-C09: the plain-path form, a named prefix, += and
		// strings.Join.
		{"plain path and pragmas", `path + "?_pragma=foreign_keys(1)"`, 1, ""},
		{"plain path and mode", `dir + "/x.db" + "?mode=ro"`, 1, ""},
		{"parenthesised chain", `(path + "/x.db") + ("?cache=shared")`, 1, ""},
		{"a URL query", `base + "?page=2"`, 0, ""},
		{"a query in the middle", `"?_pragma=" + v`, 0, ""},
		{"named prefix const", `prefix + path`, 1, `const prefix = "file:"`},
		{"named prefix in a group", `prefix + path`, 1, "const (\n\tother = 1\n\tprefix = \"file:\"\n)"},
		{"named prefix var", `p + path`, 1, `var p = "file:"`},
		{"named query", `path + q`, 1, `const q = "?_pragma=busy_timeout(5000)"`},
		{"named non-prefix", `prefix + path`, 0, `const prefix = "https:"`},
		{"+= a query", `func() string { dsn := path; dsn += "?_pragma=foreign_keys(1)"; return dsn }()`, 1, ""},
		{"+= onto a named prefix", `func() string { dsn := "file:"; dsn += path; return dsn }()`, 1, ""},
		{"+= anything else", `func() string { s := "a"; s += path; return s }()`, 0, ""},
		{"strings.Join from file:", `strings.Join([]string{"file:", path, "?_pragma=x(1)"}, "")`, 1, ""},
		{"strings.Join with a query", `strings.Join([]string{path, "?mode=ro"}, "")`, 1, ""},
		{"strings.Join on ?", `strings.Join([]string{path, "_pragma=foreign_keys(1)"}, "?")`, 1, ""},
		{"strings.Join of a path", `strings.Join([]string{dir, name}, "/")`, 0, ""},
		{"Sprintf of a path and a query", `fmt.Sprintf("%s?_pragma=foreign_keys(1)", path)`, 1, ""},
		{"Sprintf of a named file: format", `fmt.Sprintf(f, path)`, 1, `const f = "file:%s"`},
		{"one site for a whole chain", `"file:" + path + "?_pragma=a(1)" + "&_pragma=b(1)"`, 1, ""},
		{"a nested chain", `g("file:" + path) + "x"`, 1, ""},
	} {
		src := "package p\n\n" + c.decls + "\n\nvar _ = " + c.expr + "\n"
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "p.go", src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := concatenations(fset, f, "p.go", fileStrings(f)); len(got) != c.want {
			t.Errorf("%s: %d sites %q, want %d", c.name, len(got), got, c.want)
		}
	}
}
