// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabricscopecheck

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	"thingfabric.dev/go-fabric/cluster/spec/spectest"
	"thingfabric.dev/go-fabric/parity"
	"thingfabric.dev/go-fabric/schema"

	// Every generated definition linked, so spec.Lookup knows each.
	_ "thingfabric.dev/go-fabric/controller/client"
)

// The redaction rule: the dispatcher scopes every fabric-scoped attribute
// of the model (endpoint.TopologyDispatcher attaches the generated scope,
// im.MaskFabricScoped applies it), and no server withholds a field by hand.
//
//   - (a) [TestNoHandWrittenRedactionOutsideTheMask]: no struct field named
//     Redacted but the deprecated ones, no code that sets one, no reader of
//     one but the bridge writer's deprecated branches, and no type whose
//     name says it redacts — the second-encoder shape the servers used.
//   - (b) [TestEveryFabricSensitivePathHasARowAndAWalkCase]: every list of
//     parity/schema.json with fabric-sensitive data — a fabric-scoped list
//     whose entry struct has a fabric-sensitive field (class A), a list
//     whose own access is fabric-sensitive (class B) — has its row in the
//     generated scope table and is selected by the node walk
//     (spectest.FabricSensitiveLists over its generated definition), which
//     node's TestFabricSensitiveFieldsWithheldFromOtherFabrics checks on
//     the wire for every list it selects.

// deprecatedRedactedFields are the Redacted fields kept, deprecated, until
// their removal: "file:Type".
var deprecatedRedactedFields = []string{
	"cluster/core/access_control.go:AccessControlAuxiliaryEntryStruct",
	"cluster/core/access_control.go:AccessControlEntryStruct",
	"cluster/core/access_control.go:AccessControlExtensionEntry",
	"cluster/core/scenes_management.go:SceneInfoStruct",
}

// redactedReaders are the functions that still honour a deprecated
// Redacted field — the bridge writer's branches — with the number of
// reads each holds: "file:func" -> reads.
var redactedReaders = map[string]int{
	"bridge/reply.go:defaultAttributeValueWriter":         3,
	"bridge/fields_reader_scenes.go:encodeScenesResponse": 1,
}

// redactionSites lists what (a) finds in one parsed file: "field
// file:Type", "set file:line", "read file:func" and "type file:Name".
func redactionSites(fset *token.FileSet, rel string, f *ast.File) []string {
	var out []string
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, sp := range gd.Specs {
			ts := sp.(*ast.TypeSpec)
			if strings.Contains(strings.ToLower(ts.Name.Name), "redact") {
				out = append(out, "type "+rel+":"+ts.Name.Name)
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			for _, field := range st.Fields.List {
				for _, n := range field.Names {
					if n.Name == "Redacted" {
						out = append(out, "field "+rel+":"+ts.Name.Name)
					}
				}
			}
		}
	}
	set := map[ast.Node]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			for _, lhs := range x.Lhs {
				if sel, ok := lhs.(*ast.SelectorExpr); ok && sel.Sel.Name == "Redacted" {
					set[sel] = true
					out = append(out, fmt.Sprintf("set %s:%d", rel, fset.Position(sel.Pos()).Line))
				}
			}
		case *ast.KeyValueExpr:
			if k, ok := x.Key.(*ast.Ident); ok && k.Name == "Redacted" {
				out = append(out, fmt.Sprintf("set %s:%d", rel, fset.Position(k.Pos()).Line))
			}
		}
		return true
	})
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		calls := map[ast.Node]bool{}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				calls[c.Fun] = true
			}
			if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "Redacted" && !set[sel] && !calls[sel] {
				out = append(out, "read "+rel+":"+fd.Name.Name)
			}
			return true
		})
	}
	return out
}

// scanRedactions walks the module's non-test, non-generated Go files —
// outside script, docs and notes — and returns what (a) finds.
func scanRedactions(root string) ([]string, error) {
	var out []string
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			switch {
			case rel == ".":
				return nil
			case strings.HasPrefix(d.Name(), ".") || d.Name() == "testdata",
				rel == "script", rel == "notes", rel == "docs":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution|parser.ParseComments)
		if err != nil {
			return fmt.Errorf("fabricscopecheck: %w", err)
		}
		if ast.IsGenerated(f) {
			return nil
		}
		out = append(out, redactionSites(fset, rel, f)...)
		return nil
	})
	return out, err
}

// checkRedactions holds sites to the allow-lists and returns the sorted
// violations.
func checkRedactions(sites, fields []string, readers map[string]int) []string {
	var out []string
	reads := map[string]int{}
	seenFields := map[string]bool{}
	for _, s := range sites {
		kind, where, _ := strings.Cut(s, " ")
		switch kind {
		case "field":
			seenFields[where] = true
			if !slices.Contains(fields, where) {
				out = append(out, where+": a Redacted field outside the deprecated ones — return the entry whole, the dispatcher scopes it")
			}
		case "set":
			out = append(out, where+": sets a Redacted field — return the entry whole, the dispatcher scopes it")
		case "read":
			reads[where]++
		case "type":
			out = append(out, where+": a type that redacts — return the entry whole, the dispatcher scopes it")
		}
	}
	for where, n := range reads {
		if want, ok := readers[where]; !ok || n != want {
			out = append(out, fmt.Sprintf("%s: reads a Redacted field %d times, the allow-list %d", where, n, want))
		}
	}
	for where, want := range readers {
		if reads[where] == 0 {
			out = append(out, fmt.Sprintf("stale reader: %s no longer reads a Redacted field (the allow-list %d)", where, want))
		}
	}
	for _, f := range fields {
		if !seenFields[f] {
			out = append(out, fmt.Sprintf("stale field: %s holds no Redacted field", f))
		}
	}
	sort.Strings(out)
	return out
}

// TestNoHandWrittenRedactionOutsideTheMask is guard (a).
func TestNoHandWrittenRedactionOutsideTheMask(t *testing.T) {
	t.Parallel()
	sites, err := scanRedactions(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(sites, "field cluster/core/access_control.go:AccessControlEntryStruct") {
		t.Fatalf("the scan found no deprecated Redacted field: the scan is broken (%q)", sites)
	}
	for _, v := range checkRedactions(sites, deprecatedRedactedFields, redactedReaders) {
		t.Error(v)
	}
}

// redactionFixture is a server that redacts by hand in each way (a) knows.
const redactionFixture = `package fixture

type Entry struct {
	Data     []byte
	Redacted bool
}

type redactedEntry struct{ fabricIndex uint8 }

func read(own bool) []Entry {
	e := Entry{Data: []byte{1}}
	if !own {
		e.Redacted = true
	}
	return []Entry{e, {Redacted: true}}
}

func write(e Entry) bool { return e.Redacted }

func call(u interface{ Redacted() string }) string { return u.Redacted() }
`

// TestCheckFindsAHandWrittenRedaction is guard (a)'s negative case.
func TestCheckFindsAHandWrittenRedaction(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "fixture.go", redactionFixture, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	sites := redactionSites(fset, "x/fixture.go", f)
	got := checkRedactions(sites, []string{"x/fixture.go:Kept"}, map[string]int{"x/old.go:writer": 1})
	want := []string{
		"stale field: x/fixture.go:Kept holds no Redacted field",
		"stale reader: x/old.go:writer no longer reads a Redacted field (the allow-list 1)",
		"x/fixture.go:13: sets a Redacted field — return the entry whole, the dispatcher scopes it",
		"x/fixture.go:15: sets a Redacted field — return the entry whole, the dispatcher scopes it",
		"x/fixture.go:Entry: a Redacted field outside the deprecated ones — return the entry whole, the dispatcher scopes it",
		"x/fixture.go:redactedEntry: a type that redacts — return the entry whole, the dispatcher scopes it",
		"x/fixture.go:write: reads a Redacted field 1 times, the allow-list 0",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("violations:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// sensitivePath is a list of the snapshot with fabric-sensitive data.
type sensitivePath struct {
	cluster, attribute uint32
	name               string
}

// fabricSensitivePaths derives classes A and B from parity/schema.json:
// the readable lists whose access is "F" and whose entry struct has an
// "S" field, and those whose own access is "S".
func fabricSensitivePaths(snapshot []byte) ([]sensitivePath, error) {
	type datatype struct {
		Name   string `json:"name"`
		Fields []struct {
			Access struct {
				Fabric string `json:"fabric"`
			} `json:"access"`
		} `json:"fields"`
	}
	var doc struct {
		Clusters []struct {
			ID         uint32 `json:"id"`
			Name       string `json:"name"`
			Attributes []struct {
				ID        uint32 `json:"id"`
				Name      string `json:"name"`
				Effective struct {
					Access struct {
						RW     string `json:"rw"`
						Fabric string `json:"fabric"`
					} `json:"access"`
					Entry *struct {
						Type string `json:"type"`
					} `json:"entry"`
				} `json:"effective"`
			} `json:"attributes"`
			Datatypes []datatype `json:"datatypes"`
		} `json:"clusters"`
		GlobalDatatypes []datatype `json:"globalDatatypes"`
		Definitions     []struct {
			Name      string     `json:"name"`
			Datatypes []datatype `json:"datatypes"`
		} `json:"definitions"`
	}
	if err := json.Unmarshal(snapshot, &doc); err != nil {
		return nil, fmt.Errorf("fabricscopecheck: schema: %w", err)
	}
	types := map[string]datatype{}
	for _, d := range doc.GlobalDatatypes {
		types[d.Name] = d
	}
	for _, def := range doc.Definitions {
		for _, d := range def.Datatypes {
			types[def.Name+"."+d.Name] = d
		}
	}
	var out []sensitivePath
	for _, c := range doc.Clusters {
		own := map[string]datatype{}
		for _, d := range c.Datatypes {
			own[d.Name] = d
		}
		for _, a := range c.Attributes {
			acc := a.Effective.Access
			if acc.RW == "W" || (acc.Fabric != "F" && acc.Fabric != "S") {
				continue
			}
			sensitive := acc.Fabric == "S"
			if e := a.Effective.Entry; e != nil && !sensitive {
				d, ok := own[e.Type]
				if !ok {
					d = types[e.Type]
				}
				for _, f := range d.Fields {
					sensitive = sensitive || f.Access.Fabric == "S"
				}
			}
			if sensitive {
				out = append(out, sensitivePath{c.ID, a.ID, c.Name + "." + a.Name})
			}
		}
	}
	return out, nil
}

// checkSensitivePaths returns the paths without a scope row (row) or not
// selected by the node walk (walked), sorted.
func checkSensitivePaths(paths []sensitivePath, row, walked func(cluster, attribute uint32) bool) []string {
	var out []string
	for _, p := range paths {
		if !row(p.cluster, p.attribute) {
			out = append(out, fmt.Sprintf("%s (0x%04X/0x%04X): no row in the generated scope table — regenerate schema/", p.name, p.cluster, p.attribute))
		}
		if !walked(p.cluster, p.attribute) {
			out = append(out, fmt.Sprintf("%s (0x%04X/0x%04X): the node walk does not select it (no generated definition, or spectest.FabricSensitiveLists leaves it out)", p.name, p.cluster, p.attribute))
		}
	}
	sort.Strings(out)
	return out
}

// walkSelects reports whether the node walk selects the list: its
// generated definition's spectest.FabricSensitiveLists names it.
func walkSelects(cluster, attribute uint32) bool {
	def := spec.Lookup(cluster)
	if def == nil {
		return false
	}
	return slices.ContainsFunc(spectest.FabricSensitiveLists(def), func(l spectest.FabricSensitiveList) bool { return l.Attribute.ID == attribute })
}

// TestEveryFabricSensitivePathHasARowAndAWalkCase is guard (b).
func TestEveryFabricSensitivePathHasARowAndAWalkCase(t *testing.T) {
	t.Parallel()
	paths, err := fabricSensitivePaths(parity.SchemaJSON())
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 16 {
		t.Errorf("%d lists with fabric-sensitive data in the snapshot, the pin's 16 (10 class A, 6 class B): read the moved ones", len(paths))
	}
	row := func(c, a uint32) bool { _, ok := schema.AttributeFabricScope(c, a); return ok }
	for _, v := range checkSensitivePaths(paths, row, walkSelects) {
		t.Error(v)
	}
}

// TestCheckFindsAPathWithoutARowOrAWalkCase is guard (b)'s negative case.
func TestCheckFindsAPathWithoutARowOrAWalkCase(t *testing.T) {
	t.Parallel()
	paths := []sensitivePath{{0x0097, 0x0000, "Messages.Messages"}, {0xFFF1, 0x0001, "Vendor.List"}}
	got := checkSensitivePaths(paths, func(c, _ uint32) bool { return c == 0x0097 }, walkSelects)
	want := []string{
		"Vendor.List (0xFFF1/0x0001): no row in the generated scope table — regenerate schema/",
		"Vendor.List (0xFFF1/0x0001): the node walk does not select it (no generated definition, or spectest.FabricSensitiveLists leaves it out)",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("violations:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestGeneratedDefinitionsDeclareTheTablesScope: the scope spec.Instance
// declares from a generated definition (contract.FabricScopeDeclarer) is
// the generated table's for every attribute of every cluster the table
// holds, so a vendor definition built the same way is scoped by the same
// rule as a standard one.
func TestGeneratedDefinitionsDeclareTheTablesScope(t *testing.T) {
	t.Parallel()
	paths, err := fabricSensitivePaths(parity.SchemaJSON())
	if err != nil {
		t.Fatal(err)
	}
	compared := 0
	for id := range schema.ClusterNames {
		def := spec.Lookup(id)
		if def == nil {
			continue
		}
		scoped := slices.ContainsFunc(def.Attributes, func(a spec.Attribute) bool {
			_, ok := schema.AttributeFabricScope(id, a.ID)
			return ok
		})
		// The declaration reads the definition alone; a feature selection
		// that spec.New accepts is all it needs.
		inst, err := spec.New(def, spec.Options{})
		if err != nil {
			inst, err = spec.New(def, spec.Options{Features: def.FeatureMask()})
		}
		if err != nil {
			if scoped {
				t.Fatalf("%s: %v", def.Name, err)
			}
			continue // a feature choice the default refuses; no scope to compare
		}
		for i := range def.Attributes {
			a := def.Attributes[i].ID
			want, wantOK := schema.AttributeFabricScope(id, a)
			got, gotOK := inst.MatterFabricScope(a)
			if wantOK != gotOK || want.Sensitive != got.Sensitive || !slices.Equal(want.Fields, got.Fields) {
				t.Errorf("%s.%s: declared %+v (%v), the table %+v (%v)", def.Name, def.Attributes[i].Name, got, gotOK, want, wantOK)
			}
			if wantOK {
				compared++
			}
		}
	}
	if compared < len(paths) {
		t.Errorf("%d scoped attributes compared, fewer than the %d lists with fabric-sensitive data", compared, len(paths))
	}
}
