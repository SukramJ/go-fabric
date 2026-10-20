// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package archcheck

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// modulePath is this module's import path; every edge is reduced to a path
// relative to it.
const modulePath = "github.com/SukramJ/go-fabric"

//go:embed layers.json
var layersJSON []byte

type layerDef struct {
	Name     string   `json:"name"`
	Packages []string `json:"packages"`
}

type exception struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Reason string `json:"reason"`
}

type rules struct {
	Layers     []layerDef  `json:"layers"`
	Exceptions []exception `json:"exceptions"`
}

// matcher assigns a module-relative package path to a layer index.
type matcher struct {
	rules rules
}

// specificity scores how closely pattern matches path, or -1 for no match.
// "x/..." matches x and everything below it; the score is the length of x, and
// an exact pattern outranks a wildcard of the same base.
func specificity(pattern, path string) int {
	if base, ok := strings.CutSuffix(pattern, "/..."); ok {
		if path == base || strings.HasPrefix(path, base+"/") {
			return 2 * len(base)
		}
		return -1
	}
	if path == pattern {
		return 2*len(pattern) + 1
	}
	return -1
}

// layerOf returns the index of the layer path belongs to, or -1.
func (m matcher) layerOf(path string) int {
	best, bestScore := -1, -1
	for i, l := range m.rules.Layers {
		for _, p := range l.Packages {
			if s := specificity(p, path); s > bestScore {
				best, bestScore = i, s
			}
		}
	}
	return best
}

// relPath reduces an import path to a module-relative one; ok is false for a
// path outside the module.
func relPath(importPath string) (string, bool) {
	if importPath == modulePath {
		return ".", true
	}
	return strings.CutPrefix(importPath, modulePath+"/")
}

// check returns the sorted violation lines for the edges of a graph
// (module-relative package -> its module-relative imports).
func check(r rules, graph map[string][]string) []string {
	m := matcher{r}
	var out []string
	seenEx := map[[2]string]bool{}
	allowed := map[[2]string]bool{}
	for _, e := range r.Exceptions {
		allowed[[2]string{e.From, e.To}] = true
	}

	pkgs := make([]string, 0, len(graph))
	for p := range graph {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)
	for _, p := range pkgs {
		if m.layerOf(p) < 0 {
			out = append(out, fmt.Sprintf("unassigned package: %s matches no layer in layers.json", p))
		}
	}
	for _, from := range pkgs {
		lf := m.layerOf(from)
		for _, to := range graph[from] {
			lt := m.layerOf(to)
			if lf < 0 || lt < 0 || lt <= lf {
				continue
			}
			key := [2]string{from, to}
			if allowed[key] {
				seenEx[key] = true
				continue
			}
			out = append(out, fmt.Sprintf("%s -> %s (%s -> %s)", from, to, r.Layers[lf].Name, r.Layers[lt].Name))
		}
	}
	for _, e := range r.Exceptions {
		if !seenEx[[2]string{e.From, e.To}] {
			out = append(out, fmt.Sprintf("stale exception: %s -> %s no longer occurs as an upward edge; remove it from layers.json", e.From, e.To))
		}
	}
	sort.Strings(out)
	return out
}

func loadRules(t *testing.T) rules {
	t.Helper()
	var r rules
	if err := json.Unmarshal(layersJSON, &r); err != nil {
		t.Fatalf("layers.json: %v", err)
	}
	if len(r.Layers) == 0 {
		t.Fatal("layers.json: no layers")
	}
	names := map[string]bool{}
	for _, l := range r.Layers {
		if l.Name == "" || names[l.Name] {
			t.Fatalf("layers.json: empty or duplicate layer name %q", l.Name)
		}
		names[l.Name] = true
	}
	for _, e := range r.Exceptions {
		if e.From == "" || e.To == "" || strings.TrimSpace(e.Reason) == "" {
			t.Fatalf("layers.json: exception %+v needs from, to and a reason", e)
		}
	}
	return r
}

// moduleGraph loads every package of the module and returns its non-test
// imports inside the module, module-relative.
func moduleGraph(t *testing.T) map[string][]string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedImports | packages.NeedFiles,
		Dir:  root,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		t.Fatalf("packages.Load: %v", err)
	}
	graph := map[string][]string{}
	for _, p := range pkgs {
		for _, e := range p.Errors {
			t.Errorf("load %s: %v", p.PkgPath, e)
		}
		from, ok := relPath(p.PkgPath)
		if !ok {
			continue
		}
		deps := []string{}
		for ip := range p.Imports {
			if to, ok := relPath(ip); ok {
				deps = append(deps, to)
			}
		}
		sort.Strings(deps)
		graph[from] = deps
	}
	if len(graph) == 0 {
		t.Fatal("no packages loaded")
	}
	return graph
}

// TestLayering holds ADR 0026 principle 1: a package imports only its own
// layer or a lower one, apart from the exceptions in layers.json.
func TestLayering(t *testing.T) {
	r := loadRules(t)
	if v := check(r, moduleGraph(t)); len(v) > 0 {
		t.Errorf("%d layering violation(s):\n%s", len(v), strings.Join(v, "\n"))
	}
}

func TestSpecificity(t *testing.T) {
	r := rules{Layers: []layerDef{
		{Name: "low", Packages: []string{"cluster/spec/...", "clock/clocktest"}},
		{Name: "mid", Packages: []string{"cluster/...", "clock"}},
	}}
	m := matcher{r}
	for path, want := range map[string]int{
		"cluster/spec":         0,
		"cluster/spec/gen":     0,
		"cluster/specx":        1,
		"cluster/onoff":        1,
		"cluster":              1,
		"clock":                1,
		"clock/clocktest":      0,
		"clock/clocktest/deep": -1,
		"other":                -1,
	} {
		if got := m.layerOf(path); got != want {
			t.Errorf("layerOf(%q) = %d, want %d", path, got, want)
		}
	}
}

func TestCheckReportsUpwardStaleAndUnassigned(t *testing.T) {
	r := rules{
		Layers: []layerDef{{Name: "low", Packages: []string{"a"}}, {Name: "high", Packages: []string{"b"}}},
		Exceptions: []exception{
			{From: "a", To: "b", Reason: "r"},
			{From: "b", To: "a", Reason: "r"},
		},
	}
	graph := map[string][]string{"a": {"b"}, "b": {"a"}, "c": nil}
	got := strings.Join(check(r, graph), "\n")
	for _, want := range []string{"unassigned package: c", "stale exception: b -> a"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "a -> b (") {
		t.Errorf("excepted edge reported:\n%s", got)
	}
	r.Exceptions = nil
	if got := strings.Join(check(r, map[string][]string{"a": {"b"}, "b": nil}), "\n"); got != "a -> b (low -> high)" {
		t.Errorf("violation line = %q", got)
	}
}
