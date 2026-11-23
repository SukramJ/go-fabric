// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package archcheck

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// modulePath is this module's import path; every edge is reduced to a path
// relative to it.
const modulePath = "thingfabric.dev/go-fabric"

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

// forbidden is a rule the layer order cannot state: a package matching one
// of From must not import a package matching one of To, whatever their
// layers. A pattern is a layers.json pattern ("x" or "x/..."). Except names
// the packages a from-pattern matches that the rule still does not hold, each
// with its own reason (decision 583).
type forbidden struct {
	From   []string          `json:"from"`
	To     []string          `json:"to"`
	Except []forbiddenExcept `json:"except"`
	Reason string            `json:"reason"`
}

// forbiddenExcept is one reasoned exception of a forbidden rule: Package (an
// exact module-relative path) may import what the rule's to-patterns match.
type forbiddenExcept struct {
	Package string `json:"package"`
	Reason  string `json:"reason"`
}

type rules struct {
	Layers     []layerDef  `json:"layers"`
	Exceptions []exception `json:"exceptions"`
	Forbidden  []forbidden `json:"forbidden"`
}

// matchesAny reports whether path matches one of patterns.
func matchesAny(patterns []string, path string) bool {
	for _, p := range patterns {
		if specificity(p, path) >= 0 {
			return true
		}
	}
	return false
}

// excepted reports whether pkg is one of f's reasoned exceptions.
func excepted(f forbidden, pkg string) bool {
	return slices.ContainsFunc(f.Except, func(e forbiddenExcept) bool { return e.Package == pkg })
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
	usedExcept := map[[2]string]bool{} // {rule reason, package}
	for _, from := range pkgs {
		for _, f := range r.Forbidden {
			if !matchesAny(f.From, from) {
				continue
			}
			for _, reach := range forbiddenReach(graph, from, f.To) {
				if excepted(f, from) {
					usedExcept[[2]string{f.Reason, from}] = true
					continue
				}
				to := reach[len(reach)-1]
				if len(reach) == 1 {
					out = append(out, fmt.Sprintf("forbidden import: %s -> %s (%s)", from, to, f.Reason))
					continue
				}
				out = append(out, fmt.Sprintf("forbidden import: %s -> %s via %s (%s)", from, to, strings.Join(reach[:len(reach)-1], " -> "), f.Reason))
			}
		}
	}
	for _, f := range r.Forbidden {
		for _, e := range f.Except {
			if !usedExcept[[2]string{f.Reason, e.Package}] {
				out = append(out, fmt.Sprintf("stale forbidden exception: %s no longer depends on what the rule forbids (%s); remove it from layers.json", e.Package, f.Reason))
			}
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

// forbiddenReach returns, for every package matching one of to that from
// depends on — directly or through any chain of non-test imports in graph
// (R4-test-4801, decision 601: the ADR 0020 rule judges the transitive
// dependencies, so a seam between from and controller/ does not hide the
// edge) — the shortest import chain to it, from's own import first and the
// forbidden package last. The walk does not continue past a forbidden
// package: what it imports in turn is its own dependency, reached only
// through it.
func forbiddenReach(graph map[string][]string, from string, to []string) [][]string {
	prev := map[string]string{from: ""}
	queue := []string{from}
	var hits []string
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		for _, next := range graph[p] {
			if _, seen := prev[next]; seen {
				continue
			}
			prev[next] = p
			if matchesAny(to, next) {
				hits = append(hits, next)
				continue
			}
			queue = append(queue, next)
		}
	}
	sort.Strings(hits)
	out := make([][]string, 0, len(hits))
	for _, h := range hits {
		var chain []string
		for p := h; p != from; p = prev[p] {
			chain = append(chain, p)
		}
		slices.Reverse(chain)
		out = append(out, chain)
	}
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
	for _, f := range r.Forbidden {
		if len(f.From) == 0 || len(f.To) == 0 || strings.TrimSpace(f.Reason) == "" {
			t.Fatalf("layers.json: forbidden rule %+v needs from, to and a reason", f)
		}
		for _, e := range f.Except {
			if e.Package == "" || strings.HasSuffix(e.Package, "/...") || strings.TrimSpace(e.Reason) == "" {
				t.Fatalf("layers.json: forbidden exception %+v needs an exact package and a reason", e)
			}
			if !matchesAny(f.From, e.Package) {
				t.Fatalf("layers.json: forbidden exception %s matches none of the rule's from-patterns %q", e.Package, f.From)
			}
		}
	}
	return r
}

// moduleGraph loads every package of the module and of each nested module
// (every go.mod below the root: ble/bluez, ble/esphome, wifi/wpasupplicant,
// examples/ble-wifi-light), and returns their non-test imports inside the
// repository, repository-relative. A nested module is released on its own
// (R2-P6-test-2808): the root's ./... does not reach it, so it is loaded from
// its own directory. Every load is for GOOS=linux, the platform CI runs on and
// the one ble/bluez and wifi/wpasupplicant implement (elsewhere a stub).
func moduleGraph(t *testing.T) map[string][]string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	nested, err := nestedModules(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(nested) == 0 {
		t.Fatal("no nested module found; the walk is broken")
	}
	dirs := append([]string{root}, nested...)
	graph := map[string][]string{}
	for _, dir := range dirs {
		cfg := &packages.Config{
			Mode: packages.NeedName | packages.NeedImports | packages.NeedFiles,
			Dir:  dir,
			Env:  append(os.Environ(), "GOOS=linux"),
		}
		pkgs, err := packages.Load(cfg, "./...")
		if err != nil {
			t.Fatalf("packages.Load in %s: %v", dir, err)
		}
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
	}
	if len(graph) == 0 {
		t.Fatal("no packages loaded")
	}
	return graph
}

// nestedModules returns the directory of every go.mod below root, skipping
// dot-directories (.git, .claude's worktrees) and testdata, which holds
// fixture modules.
func nestedModules(root string) ([]string, error) {
	var dirs []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (strings.HasPrefix(name, ".") || name == "testdata" || name == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "go.mod" && filepath.Dir(path) != root {
			dirs = append(dirs, filepath.Dir(path))
		}
		return nil
	})
	sort.Strings(dirs)
	return dirs, err
}

// TestLayering holds ADR 0026 principle 1: a package imports only its own
// layer or a lower one, apart from the exceptions in layers.json; and the
// forbidden rules of layers.json, among them ADR 0020 decision 1's sibling
// rule (review II, R3-api-3780, decision 556).
func TestLayering(t *testing.T) {
	r := loadRules(t)
	if v := check(r, moduleGraph(t)); len(v) > 0 {
		t.Errorf("%d layering violation(s):\n%s", len(v), strings.Join(v, "\n"))
	}
}

func TestSpecificity(t *testing.T) {
	r := rules{Layers: []layerDef{
		{Name: "low", Packages: []string{"cluster", "cluster/spec/...", "clock/clocktest"}},
		{Name: "mid", Packages: []string{"cluster/...", "clock"}},
	}}
	m := matcher{r}
	for path, want := range map[string]int{
		"cluster/spec":         0,
		"cluster/spec/gen":     0,
		"cluster/specx":        1,
		"cluster/onoff":        1,
		"cluster":              0, // exact beats cluster/... of the same base
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

// TestCheckReportsForbiddenSubPackages is the fixture of decision 583
// (review II, R3-api-3780): a from-pattern bridge/... holds every non-test
// sub-package of bridge, so bridge/x importing controller/... is reported,
// while bridge/bridgetest, the rule's one reasoned exception, is not; and an
// exception whose package no longer depends on what the rule forbids is reported
// as stale.
func TestCheckReportsForbiddenSubPackages(t *testing.T) {
	r := rules{
		Layers: []layerDef{
			{Name: "roles", Packages: []string{"bridge", "bridge/x", "controller/..."}},
			{Name: "facade", Packages: []string{"node"}},
			{Name: "support", Packages: []string{"bridge/bridgetest", "node/nodetest"}},
		},
		Forbidden: []forbidden{{
			From: []string{"bridge/...", "node/..."},
			To:   []string{"controller/..."},
			Except: []forbiddenExcept{
				{Package: "bridge/bridgetest", Reason: "test support"},
				{Package: "node/nodetest", Reason: "test support"},
			},
			Reason: "siblings",
		}},
	}
	graph := map[string][]string{
		"bridge":                 nil,
		"bridge/x":               {"controller/ca"},
		"bridge/bridgetest":      {"controller/ca", "controller/attestation"},
		"node":                   nil,
		"node/nodetest":          nil,
		"controller/ca":          nil,
		"controller/attestation": nil,
	}
	got := check(r, graph)
	want := []string{
		"forbidden import: bridge/x -> controller/ca (siblings)",
		"stale forbidden exception: node/nodetest no longer depends on what the rule forbids (siblings); remove it from layers.json",
	}
	if !slices.Equal(got, want) {
		t.Errorf("check = %q, want %q", got, want)
	}
}

// TestCheckReportsForbiddenImports is the forbidden rule's fixture
// (review II, R3-api-3780, decision 556): an edge the layer order allows is
// still reported when a forbidden rule names both ends, whichever layer
// either end is in; an edge from a package no from-pattern names is not.
func TestCheckReportsForbiddenImports(t *testing.T) {
	r := rules{
		Layers: []layerDef{
			{Name: "low", Packages: []string{"dcl"}},
			{Name: "roles", Packages: []string{"bridge", "controller/..."}},
			{Name: "facade", Packages: []string{"node"}},
			{Name: "support", Packages: []string{"bridge/bridgetest"}},
		},
		Forbidden: []forbidden{{From: []string{"bridge", "node/..."}, To: []string{"controller/..."}, Reason: "siblings"}},
	}
	graph := map[string][]string{
		"node":              {"controller/dcl", "dcl"},
		"bridge":            {"controller"},
		"bridge/bridgetest": {"controller/ca"},
		"controller":        {"dcl"},
		"controller/dcl":    {"dcl"},
		"controller/ca":     nil,
		"dcl":               nil,
	}
	got := check(r, graph)
	want := []string{
		"forbidden import: bridge -> controller (siblings)",
		"forbidden import: node -> controller/dcl (siblings)",
	}
	if !slices.Equal(got, want) {
		t.Errorf("check = %q, want %q", got, want)
	}
}

// TestADR0020SiblingRuleIsInTheRules holds the rule file to decisions 556
// and 583: the forbidden rule that keeps bridge, node, device and endpoint
// and every sub-package of them off controller/... is present, so deleting
// or narrowing it in layers.json fails here rather than silently disarming
// TestLayering; and its only exception is bridge/bridgetest, with a reason.
func TestADR0020SiblingRuleIsInTheRules(t *testing.T) {
	r := loadRules(t)
	for _, from := range []string{
		"bridge", "bridge/x", "node", "node/metrics/prom",
		"device", "device/light", "endpoint", "endpoint/sqlitestore", "endpoint/endpointtest",
	} {
		for _, to := range []string{"controller", "controller/dcl", "controller/attestation"} {
			hit := false
			for _, f := range r.Forbidden {
				hit = hit || (matchesAny(f.From, from) && matchesAny(f.To, to) && !excepted(f, from))
			}
			if !hit {
				t.Errorf("no forbidden rule in layers.json keeps %s off %s (ADR 0020 decision 1, decisions 556, 583)", from, to)
			}
		}
	}
	var exc []string
	for _, f := range r.Forbidden {
		if matchesAny(f.To, "controller") {
			for _, e := range f.Except {
				exc = append(exc, e.Package)
			}
		}
	}
	if !slices.Equal(exc, []string{"bridge/bridgetest"}) {
		t.Errorf("ADR 0020 rule exceptions = %q, want only bridge/bridgetest (decision 583)", exc)
	}
}

// TestCheckReportsTransitiveForbidden is R4-test-4801's fixture (decision
// 601): the forbidden rule judges what a package depends on, not only what
// it imports. node reaching controller/dcl through an internal seam is
// reported with the chain, once per forbidden package and by its shortest
// chain; a package the walk reaches only through a forbidden one is not
// reported again; an excepted package's own reach stays allowed, while a
// package that reaches controller/ through the excepted one is reported.
func TestCheckReportsTransitiveForbidden(t *testing.T) {
	r := rules{
		Layers: []layerDef{
			{Name: "roles", Packages: []string{"controller/...", "internal/nodeseam", "internal/other"}},
			{Name: "facade", Packages: []string{"node", "node/y"}},
			{Name: "support", Packages: []string{"bridge/bridgetest"}},
		},
		// node/y -> bridge/bridgetest is an upward edge; the exception
		// keeps this fixture about the forbidden rule.
		Exceptions: []exception{{From: "node/y", To: "bridge/bridgetest", Reason: "fixture"}},
		Forbidden: []forbidden{{
			From:   []string{"bridge/...", "node/..."},
			To:     []string{"controller/..."},
			Except: []forbiddenExcept{{Package: "bridge/bridgetest", Reason: "test support"}},
			Reason: "siblings",
		}},
	}
	graph := map[string][]string{
		"node":              {"internal/nodeseam", "internal/other"},
		"internal/nodeseam": {"controller/dcl", "internal/other"},
		"internal/other":    {"internal/nodeseam"},
		"controller/dcl":    {"controller/inner"},
		"controller/inner":  nil,
		"node/y":            {"bridge/bridgetest"},
		"bridge/bridgetest": {"internal/other"},
	}
	got := check(r, graph)
	want := []string{
		"forbidden import: node -> controller/dcl via internal/nodeseam (siblings)",
		"forbidden import: node/y -> controller/dcl via bridge/bridgetest -> internal/other -> internal/nodeseam (siblings)",
	}
	if !slices.Equal(got, want) {
		t.Errorf("check =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
