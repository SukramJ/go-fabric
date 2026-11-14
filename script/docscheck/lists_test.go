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
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The three tests of this file hold hand-kept lists to the tree. Each list
// was corrected by the review of 2026-11 (W6-doc-04, -05, -12) and was
// stale again twelve days later, because a package or a key was added and
// nothing compared the list with it.

var backticked = regexp.MustCompile("`([^`]+)`")

// paragraph returns the paragraph of file (blank-line delimited) that
// starts with prefix, as one line.
func paragraph(t *testing.T, file, prefix string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, file))
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, p := range strings.Split(string(raw), "\n\n") {
		if strings.HasPrefix(p, prefix) {
			found = append(found, strings.Join(strings.Fields(p), " "))
		}
	}
	if len(found) != 1 {
		t.Fatalf("%s: %d paragraphs start with %q, want one; the page moved, adjust the test", file, len(found), prefix)
	}
	return found[0]
}

// TestReadmeInternalList: the packages README §API stability enumerates
// under `internal/` are the directories of internal/, no more and no
// fewer. The paragraph names each as a backticked bare name.
func TestReadmeInternalList(t *testing.T) {
	para := paragraph(t, "README.md", "**Every exported identifier outside `internal/` is public API.**")
	named := map[string]bool{}
	for _, m := range backticked.FindAllStringSubmatch(para, -1) {
		if !strings.Contains(m[1], "/") {
			named[m[1]] = true
		}
	}
	entries, err := os.ReadDir(filepath.Join(repoRoot, "internal"))
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() {
			have[e.Name()] = true
		}
	}
	if len(have) < 10 {
		t.Fatalf("only %d directories under internal/; the walk is broken", len(have))
	}
	for name := range have {
		if !named[name] {
			t.Errorf("README.md §API stability does not name internal/%s; add it to the kind of package it is", name)
		}
	}
	for name := range named {
		if !have[name] {
			t.Errorf("README.md §API stability names `%s` under internal/, and internal/%s does not exist", name, name)
		}
	}
}

// outsideDependList are the top-level directories that hold an importable
// package of this module and are deliberately not in README's "Depend
// freely on" list, each with the backticked form under which the same
// section says what it is instead.
var outsideDependList = map[string]string{
	"internal":    "internal/",                         // unimportable from outside
	"conformance": "conformance/",                      // "still not API"
	"script":      "script/",                           // "still not API"
	"examples":    "examples/reference-controller/cli", // "still not API": a program's own package
	"fabrictest":  "fabrictest",                        // the test-support paragraph: public API on the same terms
}

// TestReadmeDependList: README's "Depend freely on" list names every
// top-level directory that holds an importable package of this module —
// a non-test file of a package other than main, outside a nested module
// and outside testdata — except the ones [outsideDependList] accounts
// for, and names nothing else. The list is held at its first path
// segment: `im/subscription` and `client/…` stand for im and client.
func TestReadmeDependList(t *testing.T) {
	para := paragraph(t, "README.md", "Depend freely on ")
	listed := map[string]bool{}
	for _, m := range backticked.FindAllStringSubmatch(para, -1) {
		if first, _, ok := strings.Cut(m[1], "/"); ok {
			listed[first] = true
		}
	}
	section := apiStabilitySection(t)
	have := importableTopDirs(t)
	if len(have) < 20 {
		t.Fatalf("only %d top-level directories with an importable package; the walk is broken", len(have))
	}
	for dir := range have {
		form, outside := outsideDependList[dir]
		switch {
		case outside && listed[dir]:
			t.Errorf("%s/ is in the \"Depend freely on\" list and in outsideDependList", dir)
		case outside && !strings.Contains(section, "`"+form+"`"):
			t.Errorf("README.md §API stability no longer names `%s`, which outsideDependList relies on", form)
		case !outside && !listed[dir]:
			t.Errorf("README.md: %s/ holds an importable package and is not in the \"Depend freely on\" list (or, if it is not API, in outsideDependList with where the README says so)", dir)
		}
	}
	for dir := range listed {
		if !have[dir] {
			t.Errorf("README.md: the \"Depend freely on\" list names `%s/`, which holds no importable package of this module", dir)
		}
	}
	for dir := range outsideDependList {
		if !have[dir] {
			t.Errorf("outsideDependList names %s/, which holds no importable package of this module; delete the entry", dir)
		}
	}
}

// apiStabilitySection is README's text from "## API stability" to the
// next second-level heading.
func apiStabilitySection(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, rest, ok := strings.Cut(string(raw), "\n## API stability\n")
	if !ok {
		t.Fatal("README.md has no \"## API stability\" heading")
	}
	section, _, _ := strings.Cut(rest, "\n## ")
	return section
}

// importableTopDirs are the top-level directories under which this module
// has a package another module could import, were it not internal: a
// non-test .go file whose package clause is not main. A directory with its
// own go.mod is another module and is not entered.
func importableTopDirs(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(repoRoot, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == "." {
				return nil
			}
			name := d.Name()
			if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata" {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
				return filepath.SkipDir
			}
			return nil
		}
		first, _, nested := strings.Cut(rel, "/")
		if !nested || out[first] || !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.PackageClauseOnly)
		if err != nil {
			return err
		}
		if f.Name.Name != "main" {
			out[first] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// settingsPage is the page whose tables list the settings keys.
const settingsPage = "docs/guides/persistence.md"

// settingsSkipped are the trees whose SetSetting calls are not this
// module's keys: programs and tooling, and the store's conformance suite,
// which writes keys of its own tests.
var settingsSkipped = []string{"examples/", "script/", "notes/", "docs/", "store/storetest/"}

// variableKeySites are the functions that call SetSetting with a key the
// call itself does not state — a parameter, a field, a loop variable, the
// result of a function — or SetSettings with a map built before the call.
// What reaches such a call is not readable from the call, so this half of
// the guard is a reading, not a measurement: each site was read on
// 2026-11-13 (the SetSettings sites on 2026-11-14) and is listed with the keys of the page it
// writes, and the test holds only that the set of sites is exactly this
// one and that the page still carries those keys. A key added behind an
// existing site (a new entry in a map a loop saves, a new caller of a
// helper) is not seen. A site whose key the host supplies lists none.
var variableKeySites = map[string][]string{
	// aclExtensionSettingKey(fabric)
	"cluster/core/access_control.go:AccessControl.persistExtension": {"access_control.extension.<fabric>"},
	// the four timeSyncKey… constants, from its callers
	"cluster/core/time_synchronization_features.go:TimeSynchronization.save": {
		"timesync.trusted_time_source", "timesync.default_ntp", "timesync.time_zone", "timesync.dst_offset",
	},
	// vidSettingKey(fabricIndex, "statement" | "vvsc")
	"cluster/core/vid_verification.go:OperationalCredentials.storeVidVerification": {
		"matter.fabric.<fabric>.vid.statement", "matter.fabric.<fabric>.vid.vvsc",
	},
	// settingPrefix + DatastoreConfig.Key, set by the constructor
	"cluster/jointfabric/datastore.go:Datastore.saveLocked": {"jointfabric.datastore:<key>"},
	// ThermostatUIConfig.SettingKey, DefaultKeypadLockoutSetting when empty
	"cluster/thermo/thermostatuserinterfaceconfiguration.go:persistingSink.MatterWriteAttribute": {"thermostat_user_interface_configuration.keypad_lockout"},
	"cluster/thermo/thermostatuserinterfaceconfiguration.go:ThermostatUIServer.SetAttributes":    {"thermostat_user_interface_configuration.keypad_lockout"},
	// settingCertificates + Key and settingEndpoints + Key, set by the two constructors
	"cluster/tls/persist.go:save": {"tls.certificates:<key>", "tls.endpoints:<key>"},
	// SettingStore.Key, DefaultSettingKey when empty, and FabricKey
	"controller/ca/persist.go:SettingStore.save": {"controller.ca", "controller.ca.fabric"},
	// SettingStore.Key: the host's
	"controller/dcl/store.go:SettingStore.Save": nil,
	// nonVolatileKey: nonVolatileKeyRoot + key or "@" + the endpoint
	// number, the cluster and the attribute; or bridgedUserLabelKeyRoot + key
	"node/nonvolatile.go:nonVolatile.PersistWrite": {
		"nonvolatile.<key>.<cluster>.<attribute>", "nonvolatile.@<endpoint>.<cluster>.<attribute>", "bridged_user_label.<key>",
	},
	// bindingKeyRoot + key, or + "@" + the endpoint number
	"node/persist.go:persisted.bindingConfig": {"binding.<key>", "binding.@<endpoint>"},
	// SetSettings of configVersionValues: rootConfigVersionKey and
	// endpointConfigVersionKey + key
	"node/persist.go:persisted.storeConfigVersions": {"basic_information.configuration_version", "bridged_configuration_version.<key>"},
	// rootUserLabelsKey and aggregatorUserLabelsKey, from its two callers
	"node/root.go:buildLabels": {"user_label.root", "user_label.aggregator"},
	// rootSurfaceKey, endpointSurfaceKey + key, or + "@" + the endpoint number
	"node/surface_stability.go:persisted.storeSurface":  {"basic_information.surface_digest", "bridged_surface_digest.<key>", "bridged_surface_digest.@<endpoint>"},
	"node/surface_stability.go:persisted.forgetSurface": {"bridged_surface_digest.<key>"},
	// SetSettings of configVersionValues and each drift's storeKey
	// (surfaceKeys: rootSurfaceKey, endpointSurfaceKey + key or + "@" +
	// the endpoint number)
	"node/surface_stability.go:Node.announceSurface": {
		"basic_information.configuration_version", "bridged_configuration_version.<key>",
		"basic_information.surface_digest", "bridged_surface_digest.<key>", "bridged_surface_digest.@<endpoint>",
	},
	// bridgedUserLabelKeyRoot + key
	"node/user_labels.go:persisted.userLabel": {"bridged_user_label.<key>"},
}

// TestSettingsKeysDocumented: every settings key this module writes with a
// key constant is in a table of docs/guides/persistence.md.
//
// The test parses the module's non-test Go files (no type checking) and
// judges every call of a method named SetSetting by its key argument, and
// every call of SetSettings by each key of its map literal (a map built
// before the call is a variable site): a
// string literal, a package-level string constant of the same package or
// of an imported package of this module, or such a constant followed by
// "+ …" (a prefix). An exact key must be a backticked entry in the first
// column of a table of the page; a prefix must be the part before "<" of
// one (`scenes.<key>`). Every other call is a row of [variableKeySites];
// see there for what that half does not hold.
func TestSettingsKeysDocumented(t *testing.T) {
	exact, heads := documentedKeys(t)
	tree, sites := settingCalls(t, repoRoot)
	if len(sites) < 20 {
		t.Fatalf("only %d SetSetting calls found; the walk or the match is broken", len(sites))
	}
	problems, resolved := judgeSettings(sites, tree, exact, heads, variableKeySites)
	for _, p := range problems {
		t.Error(p)
	}
	if resolved < 8 {
		t.Fatalf("only %d SetSetting calls state their key; the resolution is broken", resolved)
	}
}

// judgeSettings holds the calls to the documented keys and the variable
// sites to the calls: each problem is one message; resolved counts the
// keys that were read.
func judgeSettings(sites []settingCall, tree settingsTree, exact, heads map[string]bool, variable map[string][]string) (problems []string, resolved int) {
	seen := map[string]bool{}
	for _, s := range sites {
		key, prefix, ok := resolveKey(s, tree)
		if !ok {
			id := s.file + ":" + s.fn
			seen[id] = true
			if _, listed := variable[id]; !listed {
				problems = append(problems, fmt.Sprintf("%s:%d: %s calls %s with a key the call does not state; read what reaches it, add the keys to %s and the site to variableKeySites", s.file, s.line, s.fn, s.method, settingsPage))
			}
			continue
		}
		resolved++
		switch {
		case prefix && !heads[key]:
			problems = append(problems, fmt.Sprintf("%s:%d: the settings keys under the prefix %q are in no table of %s (an entry `%s<…>`)", s.file, s.line, key, settingsPage, key))
		case !prefix && !exact[key]:
			problems = append(problems, fmt.Sprintf("%s:%d: the settings key %q is in no table of %s", s.file, s.line, key, settingsPage))
		}
	}
	for id, keys := range variable {
		if !seen[id] {
			problems = append(problems, fmt.Sprintf("variableKeySites: %s no longer calls SetSetting or SetSettings with a variable key; delete the entry", id))
		}
		for _, k := range keys {
			if head, _, isPrefix := strings.Cut(k, "<"); (isPrefix && !heads[head]) || (!isPrefix && !exact[k]) {
				problems = append(problems, fmt.Sprintf("variableKeySites: %s is listed with the key %#q, which is in no table of %s", id, k, settingsPage))
			}
		}
	}
	sort.Strings(problems)
	return problems, resolved
}

// TestSettingsKeysGuardSeesSetSettings: the guard judges the keys of a
// SetSettings call as it judges SetSetting's — each key of a map literal,
// and a map built before the call as a variable site — so an undocumented
// key written in one batch is reported (review II, rework of R2-P2-persist).
func TestSettingsKeysGuardSeesSetSettings(t *testing.T) {
	root := t.TempDir()
	src := `package fake

import "context"

type settings interface {
	SetSetting(ctx context.Context, key, value string) error
	SetSettings(ctx context.Context, values map[string]string) error
}

const (
	documented   = "doc.key"
	undocumented = "undoc.key"
)

func write(ctx context.Context, s settings, values map[string]string) {
	_ = s.SetSetting(ctx, documented, "")
	_ = s.SetSettings(ctx, map[string]string{documented: "", undocumented: ""})
	_ = s.SetSettings(ctx, values)
}
`
	if err := os.MkdirAll(filepath.Join(root, "fake"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "fake", "fake.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	tree, sites := settingCalls(t, root)
	problems, resolved := judgeSettings(sites, tree, map[string]bool{"doc.key": true}, map[string]bool{}, nil)
	want := []string{
		`fake/fake.go:17: the settings key "undoc.key" is in no table of`,
		"fake/fake.go:18: write calls SetSettings with a key the call does not state",
	}
	for _, w := range want {
		if !slices.ContainsFunc(problems, func(p string) bool { return strings.HasPrefix(p, w) }) {
			t.Errorf("no problem %q among %q", w, problems)
		}
	}
	if len(problems) != len(want) || resolved != 3 {
		t.Errorf("problems %q, %d keys read; want %d problems, 3 keys", problems, resolved, len(want))
	}
}

// documentedKeys reads the page's tables: the backticked entries of each
// row's first column, and for an entry with a placeholder the part before
// its first "<".
func documentedKeys(t *testing.T) (exact, heads map[string]bool) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, settingsPage))
	if err != nil {
		t.Fatal(err)
	}
	exact, heads = map[string]bool{}, map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		cell, _, _ := strings.Cut(line[2:], " | ")
		for _, m := range backticked.FindAllStringSubmatch(cell, -1) {
			if head, _, ok := strings.Cut(m[1], "<"); ok {
				heads[head] = true
			} else {
				exact[m[1]] = true
			}
		}
	}
	if len(exact) < 15 || len(heads) < 8 {
		t.Fatalf("%s: %d keys and %d prefixes read; the tables moved, adjust the test", settingsPage, len(exact), len(heads))
	}
	return exact, heads
}

// settingCall is one SetSetting call, or one key of a SetSettings call.
type settingCall struct {
	file, dir, fn string
	// method is SetSetting or SetSettings.
	method  string
	line    int
	key     ast.Expr
	imports []moduleImport
}

// moduleImport is an import of a package of this module: the name the
// file gives it ("" when it gives none, and the package's own name
// applies) and the package's directory.
type moduleImport struct{ name, dir string }

// settingsTree is what [settingCalls] read: the package-level string
// constants and the package name of every directory.
type settingsTree struct {
	consts   map[string]map[string]string
	pkgNames map[string]string
}

// modulePath is the module's import path.
const modulePath = "github.com/SukramJ/go-fabric"

// settingCalls returns the package-level string constants of the module,
// by directory, and every SetSetting call — and every key of a
// SetSettings call — in its non-test files under root.
func settingCalls(t *testing.T, root string) (tree settingsTree, sites []settingCall) {
	t.Helper()
	tree = settingsTree{consts: map[string]map[string]string{}, pkgNames: map[string]string{}}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == "." {
				return nil
			}
			name := d.Name()
			if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata" {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
				return filepath.SkipDir
			}
			for _, skip := range settingsSkipped {
				if rel+"/" == skip {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		dir := filepath.ToSlash(filepath.Dir(rel))
		collectConsts(f, dir, tree.consts)
		tree.pkgNames[dir] = f.Name.Name
		var imports []moduleImport
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if !strings.HasPrefix(p, modulePath+"/") {
				continue
			}
			local := ""
			if imp.Name != nil {
				local = imp.Name.Name
			}
			imports = append(imports, moduleImport{name: local, dir: strings.TrimPrefix(p, modulePath+"/")})
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			fn := fd.Name.Name
			if fd.Recv != nil && len(fd.Recv.List) == 1 {
				fn = recvName(fd.Recv.List[0].Type) + "." + fn
			}
			locals := localNames(fd)
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				site := func(method string, key ast.Expr) {
					if id, ok := rootIdent(key); ok && locals[id] {
						key = nil // a local name shadows any constant
					}
					sites = append(sites, settingCall{file: rel, dir: dir, fn: fn, method: method, line: fset.Position(call.Pos()).Line, key: key, imports: imports})
				}
				switch {
				case sel.Sel.Name == "SetSetting" && len(call.Args) == 3:
					site("SetSetting", call.Args[1])
				case sel.Sel.Name == "SetSettings" && len(call.Args) == 2:
					// Each key of a map literal is a call of its own; a map
					// built before the call is a variable site.
					lit, ok := call.Args[1].(*ast.CompositeLit)
					if !ok {
						site("SetSettings", nil)
						return true
					}
					for _, el := range lit.Elts {
						if kv, ok := el.(*ast.KeyValueExpr); ok {
							site("SetSettings", kv.Key)
						} else {
							site("SetSettings", nil)
						}
					}
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(sites, func(i, j int) bool {
		if sites[i].file != sites[j].file {
			return sites[i].file < sites[j].file
		}
		return sites[i].line < sites[j].line
	})
	return tree, sites
}

// collectConsts records the file's package-level constants whose value is
// a string literal.
func collectConsts(f *ast.File, dir string, consts map[string]map[string]string) {
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs := spec.(*ast.ValueSpec)
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				v, err := strconv.Unquote(lit.Value)
				if err != nil {
					continue
				}
				if consts[dir] == nil {
					consts[dir] = map[string]string{}
				}
				consts[dir][name.Name] = v
			}
		}
	}
}

// resolveKey reads the key a call states: a literal, a constant, or a
// constant prefix of a concatenation.
func resolveKey(s settingCall, tree settingsTree) (key string, prefix, ok bool) {
	switch e := s.key.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false, false
		}
		v, err := strconv.Unquote(e.Value)
		return v, false, err == nil
	case *ast.Ident:
		v, ok := tree.consts[s.dir][e.Name]
		return v, false, ok
	case *ast.SelectorExpr:
		pkg, isIdent := e.X.(*ast.Ident)
		if !isIdent {
			return "", false, false
		}
		for _, imp := range s.imports {
			local := imp.name
			if local == "" {
				local = tree.pkgNames[imp.dir] // the imported package's own package clause
			}
			if local == pkg.Name {
				v, ok := tree.consts[imp.dir][e.Sel.Name]
				return v, false, ok
			}
		}
		return "", false, false
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false, false
		}
		left := s
		left.key = e.X
		v, _, ok := resolveKey(left, tree)
		return v, true, ok
	}
	return "", false, false
}

// rootIdent is the identifier an expression starts with: x, x.f, x + y.
func rootIdent(e ast.Expr) (string, bool) {
	switch e := e.(type) {
	case *ast.Ident:
		return e.Name, true
	case *ast.SelectorExpr:
		return rootIdent(e.X)
	case *ast.BinaryExpr:
		return rootIdent(e.X)
	}
	return "", false
}

// localNames are the names a function declares itself — receiver,
// parameters, results and every name defined or declared in its body
// (closures included) — any of which hides a package-level constant or an
// import of the same name.
func localNames(fd *ast.FuncDecl) map[string]bool {
	out := map[string]bool{}
	fields := func(fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, f := range fl.List {
			for _, n := range f.Names {
				out[n.Name] = true
			}
		}
	}
	fields(fd.Recv)
	fields(fd.Type.Params)
	fields(fd.Type.Results)
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			fields(n.Type.Params)
			fields(n.Type.Results)
		case *ast.AssignStmt:
			if n.Tok == token.DEFINE {
				for _, l := range n.Lhs {
					if id, ok := l.(*ast.Ident); ok {
						out[id.Name] = true
					}
				}
			}
		case *ast.RangeStmt:
			if n.Tok == token.DEFINE {
				for _, l := range []ast.Expr{n.Key, n.Value} {
					if id, ok := l.(*ast.Ident); ok {
						out[id.Name] = true
					}
				}
			}
		case *ast.ValueSpec:
			for _, id := range n.Names {
				out[id.Name] = true
			}
		}
		return true
	})
	return out
}

// recvName is a receiver's type name.
func recvName(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.StarExpr:
		return recvName(e.X)
	case *ast.IndexExpr:
		return recvName(e.X)
	case *ast.Ident:
		return e.Name
	}
	return fmt.Sprintf("%T", e)
}
