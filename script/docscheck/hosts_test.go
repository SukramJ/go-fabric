// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package docscheck

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestPortsHostTableListsEveryHost: the `<cluster>def.Host` table of
// docs/guides/ports.md names exactly the clusters script/clustergen
// declares a Host for (the keys of its host maps), so a kit server that
// gains host inputs is on the page the host reads (review II,
// R2-P6-doc-3050: Thermostat's SetMeasuredTemperature and SetOccupied were
// missing).
func TestPortsHostTableListsEveryHost(t *testing.T) {
	want := map[string]bool{}
	for _, f := range []string{"hosts.go", "hosts_jointfabric.go"} {
		for _, k := range hostMapKeys(t, filepath.Join(repoRoot, "script", "clustergen", f)) {
			want[k] = true
		}
	}
	if len(want) < 20 {
		t.Fatalf("only %d host declarations found; the parse is broken", len(want))
	}
	raw, err := os.ReadFile(filepath.Join(repoRoot, "docs", "guides", "ports.md"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	in := false
	for _, line := range strings.Split(string(raw), "\n") {
		switch {
		case line == "| Cluster | `Host` |":
			in = true
		case in && strings.HasPrefix(line, "| --- "):
		case in && strings.HasPrefix(line, "| "):
			cell := strings.SplitN(strings.TrimPrefix(line, "| "), " |", 2)[0]
			for _, name := range strings.Split(cell, ", ") {
				got[strings.TrimSpace(name)] = true
			}
		case in:
			in = false
		}
	}
	if len(got) == 0 {
		t.Fatal("docs/guides/ports.md: the `Host` table is not found")
	}
	for _, d := range diffKeys(want, got) {
		t.Errorf("docs/guides/ports.md: the `Host` table lacks %s, which script/clustergen gives a Host", d)
	}
	for _, d := range diffKeys(got, want) {
		t.Errorf("docs/guides/ports.md: the `Host` table names %s, which script/clustergen gives no Host", d)
	}
}

// hostMapKeys returns the string keys of every map literal a top-level var
// of file holds.
func hostMapKeys(t *testing.T, file string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			for _, v := range spec.(*ast.ValueSpec).Values {
				lit, ok := v.(*ast.CompositeLit)
				if !ok {
					continue
				}
				if _, ok := lit.Type.(*ast.MapType); !ok {
					continue
				}
				for _, elt := range lit.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					if bl, ok := kv.Key.(*ast.BasicLit); ok && bl.Kind == token.STRING {
						s, err := strconv.Unquote(bl.Value)
						if err != nil {
							t.Fatal(err)
						}
						out = append(out, s)
					}
				}
			}
		}
	}
	return out
}

// diffKeys returns the keys of a that b lacks, sorted.
func diffKeys(a, b map[string]bool) []string {
	var out []string
	for k := range a {
		if !b[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
