// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package docscheck

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// deprecatedIdent is one exported identifier whose doc comment carries a
// paragraph beginning "Deprecated:".
type deprecatedIdent struct {
	name    string // qualified: module-relative package dir + "." + Name[.Member]
	kind    string // "const", "var", "type", "func", "method", "field"
	release string // from the paragraph's own "Removable in vX.Y.Z"; "" when it states none
	pos     string // file:line, for the failure message
}

// removableIn reads the release a Deprecated: paragraph names.
var removableIn = regexp.MustCompile(`(?i)\bremovable in (v\d+\.\d+\.\d+)\b`)

// deprecationParagraph returns the paragraph of doc that begins with
// "Deprecated:", whitespace-collapsed, and whether there is one.
func deprecationParagraph(doc *ast.CommentGroup) (string, bool) {
	if doc == nil {
		return "", false
	}
	for _, p := range strings.Split(doc.Text(), "\n\n") {
		p = strings.Join(strings.Fields(p), " ")
		if strings.HasPrefix(p, "Deprecated:") {
			return p, true
		}
	}
	return "", false
}

// deprecatedIn returns the deprecated exported identifiers one parsed file
// declares; dir is its module-relative package directory.
func deprecatedIn(fset *token.FileSet, f *ast.File, dir, rel string) []deprecatedIdent {
	var out []deprecatedIdent
	add := func(doc *ast.CommentGroup, name, kind string, at token.Pos) {
		p, ok := deprecationParagraph(doc)
		if !ok {
			return
		}
		d := deprecatedIdent{name: dir + "." + name, kind: kind, pos: fmt.Sprintf("%s:%d", rel, fset.Position(at).Line)}
		if m := removableIn.FindStringSubmatch(p); m != nil {
			d.release = m[1]
		}
		out = append(out, d)
	}
	// members adds the deprecated fields of a struct or the deprecated
	// methods of an interface the exported type typeName declares.
	members := func(typeName string, t ast.Expr) {
		var list *ast.FieldList
		kind := "field"
		switch t := t.(type) {
		case *ast.StructType:
			list = t.Fields
		case *ast.InterfaceType:
			list, kind = t.Methods, "method"
		default:
			return
		}
		for _, fld := range list.List {
			names := fld.Names
			if len(names) == 0 && kind == "field" { // embedded: the type's name is the field's
				if id := embeddedName(fld.Type); id != nil {
					names = []*ast.Ident{id}
				}
			}
			for _, n := range names {
				if n.IsExported() {
					add(fld.Doc, typeName+"."+n.Name, kind, n.Pos())
				}
			}
		}
	}
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if !d.Name.IsExported() {
				continue
			}
			if d.Recv == nil {
				add(d.Doc, d.Name.Name, "func", d.Name.Pos())
				continue
			}
			recv := receiverName(d.Recv.List[0].Type)
			if recv != nil && recv.IsExported() {
				add(d.Doc, recv.Name+"."+d.Name.Name, "method", d.Name.Pos())
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if !s.Name.IsExported() {
						continue
					}
					doc := s.Doc
					if _, ok := deprecationParagraph(doc); !ok {
						doc = d.Doc
					}
					add(doc, s.Name.Name, "type", s.Name.Pos())
					members(s.Name.Name, s.Type)
				case *ast.ValueSpec:
					kind := d.Tok.String()
					for _, n := range s.Names {
						if !n.IsExported() {
							continue
						}
						doc := s.Doc
						if _, ok := deprecationParagraph(doc); !ok {
							doc = d.Doc // a marker on a const / var group covers each spec
						}
						add(doc, n.Name, kind, n.Pos())
					}
				}
			}
		}
	}
	return out
}

// receiverName is the type name of a method receiver (T, *T, T[P], *T[P]).
func receiverName(e ast.Expr) *ast.Ident {
	for {
		switch t := e.(type) {
		case *ast.StarExpr:
			e = t.X
		case *ast.IndexExpr:
			e = t.X
		case *ast.IndexListExpr:
			e = t.X
		case *ast.Ident:
			return t
		default:
			return nil
		}
	}
}

// embeddedName is the field name of an embedded field (T, *T, pkg.T).
func embeddedName(e ast.Expr) *ast.Ident {
	switch t := e.(type) {
	case *ast.StarExpr:
		return embeddedName(t.X)
	case *ast.SelectorExpr:
		return t.Sel
	case *ast.IndexExpr:
		return embeddedName(t.X)
	case *ast.IndexListExpr:
		return embeddedName(t.X)
	case *ast.Ident:
		return t
	}
	return nil
}

// skippedDirs are the top-level directories whose identifiers are not
// public API held to the table: internal/ (unimportable), script/ (not
// API), examples/ (programs, not importable API).
var skippedDirs = map[string]bool{"internal": true, "script": true, "examples": true}

// treeDeprecations parses every non-test .go file of the root module and
// the nested modules (ble/bluez, ble/esphome, wifi/wpasupplicant are below
// the root and walked with it), outside internal/, script/, examples/,
// testdata and hidden directories, and returns the deprecated exported
// identifiers, sorted by name.
func treeDeprecations(t *testing.T) []deprecatedIdent {
	t.Helper()
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var out []deprecatedIdent
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			name := d.Name()
			if path != root && (strings.HasPrefix(name, ".") || name == "testdata" || name == "node_modules" || skippedDirs[rel]) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		if f.Name.Name == "main" {
			return nil
		}
		dir := filepath.ToSlash(filepath.Dir(rel))
		out = append(out, deprecatedIn(fset, f, dir, rel)...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// tableRow is one row of README's "Currently deprecated" table: the
// backticked names of its first column and the release of its third.
type tableRow struct {
	line    int      // README line, for the failure message
	names   []string // column 1, each backticked token
	release string   // column 3, the backticked vX.Y.Z; "" when the cell holds none
}

// deprecatedTableHeader is the header row the table is found by.
const deprecatedTableHeader = "| Deprecated | Replacement | Removable in |"

var (
	releaseCell = regexp.MustCompile("^`(v\\d+\\.\\d+\\.\\d+)`$")
	// qualifiedName is the row grammar's exact form: the module-relative
	// package directory, then the identifier, then at most one member.
	qualifiedName = regexp.MustCompile(`^[a-z][a-z0-9]*(?:/[a-z][a-z0-9]*)*\.[A-Z]\w*(?:\.[A-Z]\w*)?$`)
	// tableCell splits a row into cells: a pipe with a space on each side.
	tableCell = regexp.MustCompile(`\s\|\s`)
)

// parseDeprecatedTable reads the table that follows deprecatedTableHeader
// in readme: every row up to the first line that does not start with "|".
func parseDeprecatedTable(readme string) ([]tableRow, error) {
	lines := strings.Split(readme, "\n")
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == deprecatedTableHeader {
			if start >= 0 {
				return nil, fmt.Errorf("two tables start with %q", deprecatedTableHeader)
			}
			start = i
		}
	}
	if start < 0 || start+1 >= len(lines) || !strings.HasPrefix(lines[start+1], "| ---") {
		return nil, fmt.Errorf("no table starts with %q; the section moved, adjust the test", deprecatedTableHeader)
	}
	var rows []tableRow
	for i := start + 2; i < len(lines) && strings.HasPrefix(lines[i], "|"); i++ {
		inner := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(lines[i]), "| "), " |")
		cells := tableCell.Split(inner, -1)
		if len(cells) != 3 {
			return nil, fmt.Errorf("README.md:%d: %d cells, want 3 (a pipe inside a cell?)", i+1, len(cells))
		}
		r := tableRow{line: i + 1}
		for _, m := range backticked.FindAllStringSubmatch(cells[0], -1) {
			r.names = append(r.names, m[1])
		}
		if m := releaseCell.FindStringSubmatch(strings.TrimSpace(cells[2])); m != nil {
			r.release = m[1]
		}
		rows = append(rows, r)
	}
	return rows, nil
}

// groupPlaceholders are the placeholders a group pattern may print, each
// with the expression it stands for. A "*" stands for the rest of an
// identifier.
var groupPlaceholders = map[string]string{
	"<cluster>": `[a-z0-9]+`,                // one cluster/spec/<cluster> package directory
	"<package>": `[a-z0-9]+(?:/[a-z0-9]+)*`, // a package directory below the prefix printed before it
	"<Config>":  `(?:[A-Z]\w*)?Config`,      // a config struct's type name
	"<Event>":   `[A-Z]\w*`,                 // an event's name
}

var placeholder = regexp.MustCompile(`<[A-Za-z]+>`)

// groupMatcher translates a group pattern, as the README prints it, into
// an expression over qualified names.
func groupMatcher(pattern string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	rest := pattern
	for rest != "" {
		loc := placeholder.FindStringIndex(rest)
		lit := rest
		if loc != nil {
			lit = rest[:loc[0]]
		}
		for i, part := range strings.Split(lit, "*") {
			if i > 0 {
				b.WriteString(`\w*`)
			}
			b.WriteString(regexp.QuoteMeta(part))
		}
		if loc == nil {
			break
		}
		expr, ok := groupPlaceholders[rest[loc[0]:loc[1]]]
		if !ok {
			return nil, fmt.Errorf("group pattern %q: unknown placeholder %s", pattern, rest[loc[0]:loc[1]])
		}
		b.WriteString(expr)
		rest = rest[loc[1]:]
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

// isGroupPattern reports whether a column-1 name is written as a group.
func isGroupPattern(name string) bool {
	return strings.ContainsAny(name, "*<")
}

// deprecatedGroups are the group patterns the table may print, each with
// the exact number of deprecated identifiers it must match. A group the
// table prints is resolved by this list only, never by a convention: a
// name that is neither qualified nor listed here fails the test, and a
// listed group whose count moved fails it too.
var deprecatedGroups = map[string]int{
	"cluster/spec/<cluster>.Feature":             39,  // the generated clusters without features
	"cluster/spec/<cluster>.Server.Emit<Event>":  133, // script/clustergen's typed emit
	"cluster/<package>.<Config>.DataVersion":     58,  // decision 165's host-owned trackers
	"cluster/<package>.Server.MatterDataVersion": 3,
	"controller/dcl.*":                           40, // the forwarders of the moved ledger client (decision 553)
	"cluster/wire.Schedules*":                    12,
	"cluster/wire.Color*":                        39,
	"cluster/wire.FanControl*":                   2,
	"cluster/wire.MatterEvent*":                  7,
	"cluster/wire.OperationalState*":             6,
	"cluster/wire.RvcOperationalState*":          2,
	"cluster/wire.ThermostatSetpointMode*":       3,
	"cluster/wire.WindowCoveringAttr*":           9,
}

// releaseNotStated are the deprecated identifiers whose Deprecated:
// paragraph names no "Removable in vX.Y.Z": their row's release is not
// checkable, and is reported by name instead of guessed from the row. The
// list is held to the tree both ways — a new paragraph without a release
// fails, and an entry whose paragraph now states one (or that is gone)
// fails as stale.
var releaseNotStated = map[string]bool{}

// checkDeprecatedTable holds the table to the tree's deprecated
// identifiers, both directions, and returns one line per difference.
func checkDeprecatedTable(idents []deprecatedIdent, rows []tableRow, groups map[string]int, noRelease map[string]bool) []string {
	var out []string
	byName := map[string]deprecatedIdent{}
	for _, d := range idents {
		byName[d.name] = d
	}
	namedBy := map[string][]string{} // identifier -> the table names that name it
	printed := map[string]bool{}
	for _, r := range rows {
		if r.release == "" {
			out = append(out, fmt.Sprintf("README.md:%d: the third column holds no backticked vX.Y.Z", r.line))
		}
		if len(r.names) == 0 {
			out = append(out, fmt.Sprintf("README.md:%d: the first column names nothing", r.line))
		}
		for _, n := range r.names {
			var matched []deprecatedIdent
			switch {
			case isGroupPattern(n):
				want, ok := groups[n]
				if !ok {
					out = append(out, fmt.Sprintf("README.md:%d: %#q is a group pattern the test's deprecatedGroups does not list", r.line, n))
					continue
				}
				printed[n] = true
				re, err := groupMatcher(n)
				if err != nil {
					out = append(out, fmt.Sprintf("README.md:%d: %v", r.line, err))
					continue
				}
				for _, d := range idents {
					if re.MatchString(d.name) {
						matched = append(matched, d)
					}
				}
				if len(matched) != want {
					out = append(out, fmt.Sprintf("README.md:%d: group %#q matches %d deprecated identifiers, deprecatedGroups says %d", r.line, n, len(matched), want))
				}
			case qualifiedName.MatchString(n):
				d, ok := byName[n]
				if !ok {
					out = append(out, fmt.Sprintf("README.md:%d: %#q is not a deprecated identifier of the tree (removed, renamed, or the paragraph lost its Deprecated:)", r.line, n))
					continue
				}
				matched = []deprecatedIdent{d}
			default:
				out = append(out, fmt.Sprintf("README.md:%d: %#q is neither a qualified name (pkg/path.Name[.Member]) nor a group pattern", r.line, n))
				continue
			}
			for _, d := range matched {
				namedBy[d.name] = append(namedBy[d.name], n)
				if d.release != "" && r.release != "" && d.release != r.release {
					out = append(out, fmt.Sprintf("README.md:%d: %#q is removable in %s by the row, in %s by its paragraph (%s)", r.line, d.name, r.release, d.release, d.pos))
				}
			}
		}
	}
	for _, d := range idents {
		switch n := len(namedBy[d.name]); {
		case n == 0:
			out = append(out, fmt.Sprintf("%s: %s is deprecated and named by no row of README.md's \"Currently deprecated\" table", d.pos, d.name))
		case n > 1:
			out = append(out, fmt.Sprintf("%s: %s is named %d times in the table (%s), want once", d.pos, d.name, n, strings.Join(namedBy[d.name], ", ")))
		}
		if d.release == "" && !noRelease[d.name] {
			out = append(out, fmt.Sprintf("%s: the Deprecated: paragraph of %s states no \"Removable in vX.Y.Z\"", d.pos, d.name))
		}
	}
	for n := range noRelease {
		if d, ok := byName[n]; !ok || d.release != "" {
			out = append(out, fmt.Sprintf("releaseNotStated lists %s, whose paragraph states a release or which is no longer deprecated; delete the entry", n))
		}
	}
	for g := range groups {
		if !printed[g] {
			out = append(out, fmt.Sprintf("deprecatedGroups lists %#q, which the table no longer prints; delete the entry", g))
		}
	}
	sort.Strings(out)
	return out
}

// TestReadmeDeprecatedTable holds README §"The deprecation window"'s table
// to the tree (decisions 588, 596; review II, R4-doc-4501, R4-doc-4802):
// every exported identifier whose doc comment has a paragraph beginning
// "Deprecated:" is named by exactly one row, every name of the table
// resolves to one, and the row's release equals the paragraph's own
// "Removable in". The table had fallen behind three times — a fix that
// marked an identifier and announced it in CHANGELOG.md, but whose row
// nothing asked for.
func TestReadmeDeprecatedTable(t *testing.T) {
	idents := treeDeprecations(t)
	if len(idents) < 100 {
		t.Fatalf("only %d deprecated identifiers in the tree; the walk is broken", len(idents))
	}
	raw, err := os.ReadFile(filepath.Join(repoRoot, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := parseDeprecatedTable(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range idents {
		if d.release == "" {
			t.Logf("not checkable: the Deprecated: paragraph of %s (%s) states no release", d.name, d.pos)
		}
	}
	for _, v := range checkDeprecatedTable(idents, rows, deprecatedGroups, releaseNotStated) {
		t.Error(v)
	}
}

// TestDeprecatedInReadsEveryDeclarationKind is the tree half's fixture:
// a Deprecated: paragraph on a func, a method of a generic receiver, a
// type, a struct field, an embedded field, an interface method, a const
// group (covering each spec) and one var spec is read with its qualified
// name and its release; a mention of "Deprecated:" inside a paragraph, an
// unexported name, an embedded interface and an undeprecated member are
// not.
func TestDeprecatedInReadsEveryDeclarationKind(t *testing.T) {
	// The markers are spelled DEP here and replaced below, so the module's
	// own marker scan (deprecation_policy_test.go) does not read this
	// fixture as a deprecation of the module.
	src := `package demo

// F does a thing.
//
// DEP use G. Removable in v0.9.0.
func F() {}

// DEP use N.
// Removable in
// v0.9.1.
func (s *S[T]) M() {}

// DEP gone. Removable in v0.9.2.
type T struct{}

// S holds.
type S[T any] struct {
	// DEP use B. removable in v0.9.3.
	A int
	// DEP embedded. Removable in v0.9.4.
	*T
	// B says Deprecated: in a sentence, which is no marker.
	B int
	// DEP unexported. Removable in v0.9.5.
	c int
}

// I is an interface.
type I interface {
	// DEP use N. Removable in v0.9.6.
	Old()
	// DEP an embedded interface is no method. Removable in v0.9.9.
	Base
}

// DEP the group. Removable in v0.9.7.
const (
	C1 = 1
	C2, c3 = 2, 3
)

var (
	V1 = 1
	// DEP one spec.
	V2 = 2
)

// DEP unexported. Removable in v0.9.8.
func h() {}
`
	src = strings.ReplaceAll(src, "// DEP ", "// "+"Deprecated: ")
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "demo.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	found := deprecatedIn(fset, f, "x/demo", "x/demo/demo.go")
	got := make([]string, 0, len(found))
	for _, d := range found {
		got = append(got, d.name+" "+d.kind+" "+d.release)
	}
	want := []string{
		"x/demo.F func v0.9.0",
		"x/demo.S.M method v0.9.1",
		"x/demo.T type v0.9.2",
		"x/demo.S.A field v0.9.3",
		"x/demo.S.T field v0.9.4",
		"x/demo.I.Old method v0.9.6",
		"x/demo.C1 const v0.9.7",
		"x/demo.C2 const v0.9.7",
		"x/demo.V2 var ",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("deprecatedIn:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestCheckDeprecatedTableBothDirections is the table half's fixture: a
// clean table passes, and each defect the guard exists for is reported —
// an identifier no row names, one named twice, a stale exact name, a
// stale group count, an abbreviated name, an unlisted group, a release
// that differs from the paragraph's, a paragraph without a release that
// releaseNotStated does not list, and a stale list entry.
func TestCheckDeprecatedTableBothDirections(t *testing.T) {
	idents := []deprecatedIdent{
		{name: "a.F", release: "v0.3.0", pos: "a/f.go:1"},
		{name: "a.T.M", release: "v0.3.0", pos: "a/f.go:2"},
		{name: "g/one.Feature", release: "v0.4.0", pos: "g/one/f.go:1"},
		{name: "g/two.Feature", release: "v0.4.0", pos: "g/two/f.go:1"},
		{name: "b.Old", pos: "b/f.go:1"},
	}
	groups := map[string]int{"g/<cluster>.Feature": 2}
	noRelease := map[string]bool{"b.Old": true}
	clean := []tableRow{
		{line: 1, names: []string{"a.F", "a.T.M"}, release: "v0.3.0"},
		{line: 2, names: []string{"g/<cluster>.Feature"}, release: "v0.4.0"},
		{line: 3, names: []string{"b.Old"}, release: "v0.1.0"},
	}
	if got := checkDeprecatedTable(idents, clean, groups, noRelease); len(got) != 0 {
		t.Fatalf("clean table reported:\n%s", strings.Join(got, "\n"))
	}
	for _, tc := range []struct {
		name      string
		idents    []deprecatedIdent
		rows      []tableRow
		groups    map[string]int
		noRelease map[string]bool
		want      string
	}{
		{"unnamed", idents, clean[1:], groups, noRelease, "a.F is deprecated and named by no row"},
		{"twice", idents, append([]tableRow{{line: 9, names: []string{"a.F"}, release: "v0.3.0"}}, clean...), groups, noRelease, "a.F is named 2 times"},
		{"stale name", idents, append([]tableRow{{line: 9, names: []string{"a.Gone"}, release: "v0.3.0"}}, clean...), groups, noRelease, "`a.Gone` is not a deprecated identifier"},
		{"stale count", idents, clean, map[string]int{"g/<cluster>.Feature": 3}, noRelease, "matches 2 deprecated identifiers, deprecatedGroups says 3"},
		{"abbreviated", idents, []tableRow{{line: 1, names: []string{"a.F", "T.M"}, release: "v0.3.0"}, clean[1], clean[2]}, groups, noRelease, "`T.M` is neither a qualified name"},
		{"unlisted group", idents, clean, map[string]int{}, noRelease, "`g/<cluster>.Feature` is a group pattern the test's deprecatedGroups does not list"},
		{"release", idents, []tableRow{{line: 1, names: []string{"a.F", "a.T.M"}, release: "v0.2.0"}, clean[1], clean[2]}, groups, noRelease, "`a.F` is removable in v0.2.0 by the row, in v0.3.0 by its paragraph"},
		{"no release", idents, clean, groups, map[string]bool{}, "the Deprecated: paragraph of b.Old states no"},
		{"stale no release", idents, clean, groups, map[string]bool{"b.Old": true, "a.F": true}, "releaseNotStated lists a.F"},
		{"group not printed", idents, clean, map[string]int{"g/<cluster>.Feature": 2, "x.*": 1}, noRelease, "deprecatedGroups lists `x.*`, which the table no longer prints"},
	} {
		got := strings.Join(checkDeprecatedTable(tc.idents, tc.rows, tc.groups, tc.noRelease), "\n")
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s: want %q in:\n%s", tc.name, tc.want, got)
		}
	}
}

// TestGroupMatcher pins the placeholder translation: a placeholder stands
// for its expression only, and "*" for the rest of an identifier, not for
// a further member or package.
func TestGroupMatcher(t *testing.T) {
	for pattern, cases := range map[string]map[string]bool{
		"cluster/spec/<cluster>.Server.Emit<Event>": {
			"cluster/spec/onoff.Server.EmitStateChanged": true,
			"cluster/spec/on/off.Server.EmitX":           false,
			"cluster/spec/onoff.Server.Emit":             false,
		},
		"cluster/<package>.<Config>.DataVersion": {
			"cluster/fan.Config.DataVersion":                true,
			"cluster/core.UserLabelConfig.DataVersion":      true,
			"cluster/levelcontrol.Server.DataVersion":       false,
			"cluster/a/b.ThreadDirectoryConfig.DataVersion": true,
		},
		"cluster/wire.Color*": {
			"cluster/wire.ColorCtrlAttrOptions": true,
			"cluster/wire.Color":                true,
			"cluster/wire.ColorX.Member":        false,
			"cluster/wireX.Color":               false,
		},
	} {
		re, err := groupMatcher(pattern)
		if err != nil {
			t.Fatal(err)
		}
		for name, want := range cases {
			if got := re.MatchString(name); got != want {
				t.Errorf("%s matches %s = %v, want %v", pattern, name, got, want)
			}
		}
	}
	if _, err := groupMatcher("a.<Unknown>"); err == nil {
		t.Error("an unknown placeholder compiled")
	}
}
