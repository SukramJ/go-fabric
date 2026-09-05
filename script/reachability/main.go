// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package main implements the exported-API reachability analyzer for go-fabric.
//
// It loads every package of the module through golang.org/x/tools/go/packages,
// builds an SSA program, and runs Rapid Type Analysis (RTA) from a set of entry
// points to find the exported identifiers no entry point can reach.
//
// # The root set, and why it differs from the reference daemon's
//
// This analyzer is a port of the one in the reference daemon's script/reachability,
// where the root set is the main and init functions of that repository's three
// cmd/ binaries and the question is "which exported API does the running daemon
// never reach".
//
// That question is not askable here, and asking it anyway would produce a tool
// that measures nothing. go-fabric is a library module: it has no cmd/ tree and
// no package named main outside this analyzer and the //go:build ignore schema
// generator. A production-only run would therefore start from an EMPTY root set,
// RTA would reach nothing at all, and every exported identifier in the module
// would be reported unreachable. The count would be a restatement of the module's
// size, and would move only when the exported surface changed — never in response
// to anything the analysis is supposed to detect.
//
// So the root set here is the module's TEST entry points: every Test*, Benchmark*,
// Fuzz* and Example* function in a test package. The question that answers is
//
//	which exported API does no test reach?
//
// which is the meaningful one for a module whose entire purpose is to be consumed
// from outside: untested exported surface is exactly the surface a host consumer
// can break without this module noticing. The reference daemon seeds the same
// roots in its non-production mode (its `-production-only=false` path); here that
// mode is the only mode, and it is the default.
//
// # When this changes
//
// If a reference application is ever added to this module — an example binary
// that drives the library end to end — that binary becomes a genuine production
// entry point, and the loom-style question ("which exported API does the reference
// app not reach") becomes answerable. At that point a production-only mode is
// worth adding back, seeded from that binary's main and init. Until such a binary
// exists there is nothing to seed it from, so -production-only is a documented
// error rather than a silent empty run. Do not "fix" it back by pointing it at an
// empty pattern list.
//
// # Output
//
//   - script/reachability/inventory.json — the full inventory
//   - script/reachability/summary.md     — a human-readable summary
//
// Both are deterministic for a given tree: every slice is sorted by a stable key,
// and neither file carries a timestamp or a git revision. That omission is the
// point rather than an oversight — the snapshot is compared byte-for-byte against
// the committed one, and a revision written into the file could only ever name the
// commit before the one that carries it, so it would report a provenance that is
// wrong by construction and leave the comparison permanently stale. The commit
// holding the file is its provenance.
//
// # Whitelist
//
// An exported identifier annotated with
//
//	// fabric:reachable:reason="why this is reached without a test calling it"
//
// is reported as whitelisted instead of unreachable. The annotation is only
// recognised on the line DIRECTLY ABOVE the declaration — see
// [findWhitelistComment]. An annotation placed higher up in a doc comment block,
// with prose lines between it and the declaration, is silently ignored and the
// identifier is still reported unreachable. Put it on the last line of the doc
// comment, immediately before the `func` / `type` / `var` keyword.
//
// A small set of identifiers is whitelisted automatically: declarations in
// _test.go files, identifiers whose name begins with mock/fake/stub/dummy, and
// type aliases (every use of an alias resolves to the aliased type, so RTA can
// never observe the alias itself as reachable). Nothing else. The reference
// daemon carries a long list of path-keyed auto-whitelist rules for its
// reflectively mounted REST handlers and registry-dispatched device profiles;
// none of those constructs exist here, and a rule that can never fire is dead
// weight that reads like a live exemption.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"golang.org/x/tools/go/callgraph/rta"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

// modulePath is this module's import path. Every path-keyed decision below
// derives from this constant rather than repeating the literal: the reference
// daemon's analyzer spelled its own module path out three times and two of them
// went stale when the analyzed subtree moved, which silently narrowed the
// population the tool measured.
const modulePath = "github.com/SukramJ/go-fabric"

// whitelistMarker is the comment prefix that marks an exported identifier as
// reachable-by-inspection. See the package doc for its placement rule.
const whitelistMarker = "fabric:reachable:reason="

// WhitelistEntry is an identifier explicitly or automatically marked reachable.
type WhitelistEntry struct {
	Package    string `json:"package"`
	Identifier string `json:"identifier"`
	Reason     string `json:"reason"`
	File       string `json:"file"`
	Line       int    `json:"line"`
}

// UnreachableEntry is an exported identifier no entry point reaches.
type UnreachableEntry struct {
	Package    string `json:"package"`
	Identifier string `json:"identifier"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	Kind       string `json:"kind"` // "func", "type", "var", "unknown"
}

// PackageSummary aggregates the unreachable counts of one package.
type PackageSummary struct {
	Package          string `json:"package"`
	UnreachableFuncs int    `json:"unreachable_funcs"`
	UnreachableTypes int    `json:"unreachable_types"`
	UnreachableOther int    `json:"unreachable_other"`
}

// Summary is the top-level count block of the inventory.
type Summary struct {
	TotalExported int `json:"total_exported"`
	Reachable     int `json:"reachable"`
	Whitelisted   int `json:"whitelisted"`
	Unreachable   int `json:"unreachable"`
}

// Inventory is the output document written to inventory.json.
type Inventory struct {
	RootSet      string             `json:"root_set"`
	EntryPoints  int                `json:"entry_points"`
	EntryPkgs    []string           `json:"entry_packages"`
	Summary      Summary            `json:"summary"`
	ByPackage    []PackageSummary   `json:"by_package"`
	Unreachable  []UnreachableEntry `json:"unreachable"`
	Whitelisted  []WhitelistEntry   `json:"whitelisted"`
	Disagreement int                `json:"variant_disagreements"`
}

// identKey identifies one exported identifier across all SSA package variants.
type identKey struct {
	pkg  string
	name string
}

// autoWhitelistReason names why an item was whitelisted without an annotation.
type autoWhitelistReason string

const (
	autoWhitelistTestFile   autoWhitelistReason = "auto-whitelist:pattern=test-file"
	autoWhitelistMockPrefix autoWhitelistReason = "auto-whitelist:pattern=mock-fake-stub-dummy"
	autoWhitelistTypeAlias  autoWhitelistReason = "auto-whitelist:pattern=type-alias"
)

// rootSetDescription is copied into the inventory so a reader of the JSON alone
// knows what the numbers were measured against.
const rootSetDescription = "test-seeded: Test*/Benchmark*/Fuzz*/Example* functions of every test package " +
	"(this module is a library and has no production main to seed from)"

// errNoProductionRoots is returned for -production-only. It is an error rather
// than an empty run on purpose; the package doc explains why.
var errNoProductionRoots = errors.New(
	"-production-only is not available in this module: go-fabric is a library with no cmd/ tree, " +
		"so a production-only run would start from an empty root set and report every exported " +
		"identifier as unreachable. The default (test-seeded) mode is the meaningful one here. " +
		"See the package doc of script/reachability for the reasoning and for when this changes",
)

func main() {
	outPath := flag.String("out", "script/reachability/inventory.json", "output path for the inventory")
	summaryPath := flag.String("summary", "script/reachability/summary.md", "output path for the markdown summary")
	repoRoot := flag.String("root", ".", "module root (default: cwd)")
	verbose := flag.Bool("verbose", false, "verbose logging")
	productionOnly := flag.Bool("production-only", false,
		"NOT AVAILABLE in this module — see the package doc; kept as a named error so a ported invocation explains itself")
	flag.Parse()

	// Info by default: the progress lines say how much was loaded, how many
	// entry points were found and how large the whitelist is — the numbers that
	// tell a reader whether the run measured what they think it did.
	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	if *productionOnly {
		logger.Error("refusing to run", "err", errNoProductionRoots)
		os.Exit(1)
	}

	if err := run(logger, *repoRoot, *outPath, *summaryPath); err != nil {
		logger.Error("analyzer failed", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger, repoRoot, outPath, summaryPath string) error {
	absRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return fmt.Errorf("resolve module root: %w", err)
	}
	logger.Debug("module root", "path", absRoot)

	pkgs, err := loadPackages(logger, absRoot)
	if err != nil {
		return err
	}

	logger.Info("building the SSA program")
	prog, ssaPkgs := ssautil.AllPackages(pkgs, ssa.InstantiateGenerics)
	prog.Build()
	logger.Info("SSA built", "packages", len(ssaPkgs))

	entryFuncs, entryPkgs := collectTestRoots(prog, ssaPkgs)
	logger.Info("entry points collected", "count", len(entryFuncs), "tested_packages", len(entryPkgs))
	if len(entryFuncs) == 0 {
		// A silent empty root set is the one failure mode this port exists to
		// avoid: RTA over no roots reaches nothing, so every exported
		// identifier would be reported dead and the number would look like a
		// finding. Refuse instead.
		return errors.New("no test entry points found — the root set is empty, so every exported " +
			"identifier would be reported unreachable; that is a broken run, not a measurement")
	}

	logger.Info("running RTA")
	rtaResult := rta.Analyze(entryFuncs, true)
	logger.Info("RTA complete")

	reachableFuncs := unifyReachableVariants(rtaResult, ssaPkgs)
	refs := buildReferenceIndex(reachableFuncs)
	logger.Info("reachable code indexed",
		"functions", len(reachableFuncs), "named_types", len(refs.namedTypes), "globals", len(refs.globals))

	whitelisted := make(map[identKey]WhitelistEntry)
	collectWhitelisted(pkgs, absRoot, whitelisted, logger)
	logger.Info("annotation whitelist loaded", "entries", len(whitelisted))

	inv := classify(prog, ssaPkgs, absRoot, reachableFuncs, refs, whitelisted)
	inv.RootSet = rootSetDescription
	inv.EntryPoints = len(entryFuncs)
	inv.EntryPkgs = entryPkgs

	if err := writeInventory(absRoot, outPath, inv); err != nil {
		return err
	}
	if summaryPath != "" {
		abs := summaryPath
		if !filepath.IsAbs(summaryPath) {
			abs = filepath.Join(absRoot, summaryPath)
		}
		if err := writeSummaryMD(abs, inv); err != nil {
			return fmt.Errorf("write summary markdown: %w", err)
		}
		fmt.Printf("summary.md    written: %s\n", abs)
	}

	printReport(inv)
	return nil
}

// loadPackages loads every package of this module, including its test variants.
func loadPackages(logger *slog.Logger, absRoot string) ([]*packages.Package, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedImports | packages.NeedDeps | packages.NeedTypes |
			packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedTypesSizes,
		Dir:   absRoot,
		Tests: true,
	}

	pattern := modulePath + "/..."
	logger.Info("loading packages", "pattern", pattern)
	pkgs, err := packages.Load(cfg, pattern)
	if err != nil {
		return nil, fmt.Errorf("packages.Load: %w", err)
	}

	var loadErrors int
	packages.Visit(pkgs, func(p *packages.Package) bool {
		for _, e := range p.Errors {
			loadErrors++
			logger.Warn("package load error", "pkg", p.PkgPath, "err", e)
		}
		return true
	}, nil)
	if loadErrors > 0 {
		logger.Warn("packages with load errors", "count", loadErrors)
	}
	logger.Info("packages loaded", "count", len(pkgs))
	return pkgs, nil
}

// collectTestRoots gathers the module's test entry points: the Test*, Benchmark*,
// Fuzz* and Example* functions `go test` invokes. These are the roots — see the
// package doc for why there are no production roots to add to them.
//
// A root is identified by the file it is DECLARED IN, not by its package path.
// Go has two kinds of test package, and only one of them is visible in the path:
// an external test package carries the `_test` suffix, while an in-package test
// (`package schema` in schema/schema_lookup_test.go) is compiled into a variant
// whose path is the production path, character for character. Gating on the path
// therefore drops every in-package test from the root set — which, measured on
// this module, silently turned schema.ClusterName, tlv.ImplicitTag and
// mdns.Diagnose into "unreached" although each is called by name from a test two
// directories away. The file suffix is what `go test` itself keys on.
func collectTestRoots(prog *ssa.Program, ssaPkgs []*ssa.Package) (roots []*ssa.Function, testedPkgs []string) {
	var entryFuncs []*ssa.Function
	seenFn := make(map[string]bool)
	seenPkg := make(map[string]bool)
	var entryPkgs []string

	for _, p := range ssaPkgs {
		if p == nil || p.Pkg == nil {
			continue
		}
		pkgPath := p.Pkg.Path()
		if !strings.HasPrefix(pkgPath, modulePath) {
			continue
		}
		for name, mem := range p.Members {
			fn, ok := mem.(*ssa.Function)
			if !ok || !isTestRootName(name) || !fn.Pos().IsValid() {
				continue
			}
			if !strings.HasSuffix(prog.Fset.Position(fn.Pos()).Filename, "_test.go") {
				continue
			}
			// One logical test function exists once per SSA package variant;
			// seeding every copy would make entry_points a count of
			// compilations rather than of tests. Seeding one is enough:
			// unifyReachableVariants folds the other variants' callees back in
			// by RelString afterwards.
			sig := fn.RelString(nil)
			if seenFn[sig] {
				continue
			}
			seenFn[sig] = true
			entryFuncs = append(entryFuncs, fn)

			home := relPackage(strings.TrimSuffix(pkgPath, "_test"))
			if !seenPkg[home] {
				seenPkg[home] = true
				entryPkgs = append(entryPkgs, home)
			}
		}
	}
	sort.Strings(entryPkgs)
	return entryFuncs, entryPkgs
}

// isTestRootName reports whether name is one of the four function-name shapes
// `go test` invokes directly.
func isTestRootName(name string) bool {
	return strings.HasPrefix(name, "Test") ||
		strings.HasPrefix(name, "Benchmark") ||
		strings.HasPrefix(name, "Fuzz") ||
		strings.HasPrefix(name, "Example")
}

// unifyReachableVariants folds RTA's per-object answer across the SSA package
// variants go/packages produces.
//
// go/packages loads a package again for every test binary that links it, so one
// logical function has several *ssa.Function instances. RTA reaches only the
// instance on the live call path, while classification below may inspect a
// different variant — which would then read as unreachable although the function
// is live. RelString(nil) (package + receiver + name) is unique per logical
// function, so keying on it can never conflate two distinct functions.
func unifyReachableVariants(res *rta.Result, ssaPkgs []*ssa.Package) map[*ssa.Function]bool {
	reachable := make(map[*ssa.Function]bool, len(res.Reachable))
	for fn := range res.Reachable {
		reachable[fn] = true
	}
	sig := make(map[string]bool, len(reachable))
	for fn := range reachable {
		sig[fn.RelString(nil)] = true
	}
	for _, p := range ssaPkgs {
		if p == nil {
			continue
		}
		for _, mem := range p.Members {
			fn, ok := mem.(*ssa.Function)
			if !ok || reachable[fn] {
				continue
			}
			if sig[fn.RelString(nil)] {
				reachable[fn] = true
			}
		}
	}
	return reachable
}

// identClass is the per-identifier verdict, folded across package variants.
type identClass struct {
	reachable   bool
	whitelisted *WhitelistEntry
	unreachable *UnreachableEntry
}

// classify enumerates every exported identifier of this module and folds the
// verdicts of all its SSA package variants into one: whitelisted wins, then
// reachable in ANY variant, then unreachable. Reachable from somewhere is
// reachable.
//
// The fold is not cosmetic. Without it the counts multiply by the number of test
// binaries that link a package, so the total can grow while the set of dead
// identifiers shrinks, and one variant can call a symbol reachable while another
// does not — whichever copy landed in the output would decide the verdict.
func classify(
	prog *ssa.Program,
	ssaPkgs []*ssa.Package,
	absRoot string,
	reachableFuncs map[*ssa.Function]bool,
	refs *referenceIndex,
	whitelisted map[identKey]WhitelistEntry,
) Inventory {
	classes := make(map[identKey]*identClass)
	var order []identKey
	disagreements := 0
	totalExported := 0

	for _, p := range ssaPkgs {
		if p == nil || p.Pkg == nil {
			continue
		}
		pkgPath := p.Pkg.Path()
		if !isOwnAnalyzablePkg(pkgPath) || isTestPkg(p) {
			continue
		}
		relPkg := relPackage(pkgPath)

		for name, member := range p.Members {
			if !ast.IsExported(name) {
				continue
			}
			key := identKey{pkg: relPkg, name: name}
			cls := classes[key]
			if cls == nil {
				cls = &identClass{}
				classes[key] = cls
				order = append(order, key)
				totalExported++
			}

			pos := prog.Fset.Position(member.Pos())
			relFile := strings.TrimPrefix(pos.Filename, absRoot+string(filepath.Separator))

			if reason, ok := autoWhitelist(member, relFile, name); ok {
				if cls.whitelisted == nil {
					cls.whitelisted = &WhitelistEntry{
						Package: relPkg, Identifier: name,
						Reason: string(reason), File: relFile, Line: pos.Line,
					}
				}
				continue
			}
			if entry, ok := whitelisted[key]; ok {
				if cls.whitelisted == nil {
					e := entry
					cls.whitelisted = &e
				}
				continue
			}

			if isReachable(member, reachableFuncs, refs, p) {
				if cls.unreachable != nil {
					disagreements++
				}
				cls.reachable = true
				continue
			}
			if cls.reachable {
				disagreements++
				continue
			}
			if cls.unreachable == nil {
				cls.unreachable = &UnreachableEntry{
					Package: relPkg, Identifier: name,
					File: relFile, Line: pos.Line, Kind: memberKind(member),
				}
			}
		}
	}

	return foldClasses(order, classes, totalExported, disagreements)
}

// foldClasses turns the per-identifier verdicts into the inventory document,
// preserving the precedence whitelisted > reachable > unreachable.
func foldClasses(order []identKey, classes map[identKey]*identClass, totalExported, disagreements int) Inventory {
	var unreachableItems []UnreachableEntry
	var whitelistedItems []WhitelistEntry
	reachableCount := 0
	for _, key := range order {
		cls := classes[key]
		switch {
		case cls.whitelisted != nil:
			whitelistedItems = append(whitelistedItems, *cls.whitelisted)
		case cls.reachable:
			reachableCount++
		case cls.unreachable != nil:
			unreachableItems = append(unreachableItems, *cls.unreachable)
		}
	}

	sortUnreachable(unreachableItems)
	sortWhitelisted(whitelistedItems)

	return Inventory{
		Summary: Summary{
			TotalExported: totalExported,
			Reachable:     reachableCount,
			Whitelisted:   len(whitelistedItems),
			Unreachable:   len(unreachableItems),
		},
		ByPackage:    buildPackageSummary(unreachableItems),
		Unreachable:  unreachableItems,
		Whitelisted:  whitelistedItems,
		Disagreement: disagreements,
	}
}

// isOwnAnalyzablePkg reports whether pkgPath is a package of this module whose
// exported identifiers form the analyzed population.
//
// Only this module's own identifiers are classified; a dependency's exported
// surface is not this analyzer's business. script/ is excluded too — it holds
// build tooling (this analyzer among it), which no test is expected to reach.
func isOwnAnalyzablePkg(pkgPath string) bool {
	if pkgPath != modulePath && !strings.HasPrefix(pkgPath, modulePath+"/") {
		return false
	}
	return !strings.HasPrefix(relPackage(pkgPath), "script/") && relPackage(pkgPath) != "script"
}

// relPackage strips the module path, so the root package reads as "." and a
// subpackage as its directory path.
func relPackage(pkgPath string) string {
	if pkgPath == modulePath {
		return "."
	}
	return strings.TrimPrefix(pkgPath, modulePath+"/")
}

// autoWhitelist reports whether a member is whitelisted without an annotation.
// The three rules are deliberately generic; see the package doc.
func autoWhitelist(member ssa.Member, relFile, identifier string) (autoWhitelistReason, bool) {
	// A type alias re-exports another type under a local name. Every use
	// resolves to the aliased type, so RTA can never observe the alias itself
	// as reachable — listing them would be pure noise.
	if t, isType := member.(*ssa.Type); isType {
		if tn, isName := t.Object().(*types.TypeName); isName && tn.IsAlias() {
			return autoWhitelistTypeAlias, true
		}
	}
	if strings.HasSuffix(relFile, "_test.go") {
		return autoWhitelistTestFile, true
	}
	lower := strings.ToLower(identifier)
	if strings.HasPrefix(lower, "mock") || strings.HasPrefix(lower, "fake") ||
		strings.HasPrefix(lower, "stub") || strings.HasPrefix(lower, "dummy") {
		return autoWhitelistMockPrefix, true
	}
	return "", false
}

// referenceIndex records what reachable code mentions, keyed by strings that are
// stable across the SSA package variants go/packages produces (the *types.Named
// and *ssa.Global objects themselves are distinct per variant).
type referenceIndex struct {
	// namedTypes holds types.TypeString of every named type reachable code
	// names — in a signature, an allocation, an operand, a conversion.
	namedTypes map[string]bool
	// globals holds "<pkgpath>.<name>" of every package-level var reachable
	// code loads from or stores to.
	globals map[string]bool
}

// buildReferenceIndex walks every reachable function and records the named types
// and package-level vars its code actually mentions.
//
// This exists because RTA answers one question — is this FUNCTION called — and
// the other two member kinds need their own mechanism rather than a convention
// dressed up as one:
//
//   - A package-level var is never a call target. Judging one by "is it a
//     global" would report every exported var in the module as unreached, a
//     number that restates how many exported vars exist and cannot change in
//     response to a test being written. Sentinel errors are the whole of that
//     class here, and a test that asserts errors.Is(err, bridge.ErrNotStarted)
//     does reach one.
//   - A type whose reachability is judged only by its method set is judged by
//     whether it HAS methods: a plain struct DTO carried through a reachable
//     signature would read as unreached forever. Naming a type in reachable
//     code is the observable fact, so that is what is measured; the method-set
//     rule is kept alongside it because a type reached only through interface
//     dispatch shows up there and not in a signature.
func buildReferenceIndex(reachable map[*ssa.Function]bool) *referenceIndex {
	idx := &referenceIndex{
		namedTypes: make(map[string]bool),
		globals:    make(map[string]bool),
	}
	seenType := make(map[types.Type]bool)

	for fn := range reachable {
		if fn == nil {
			continue
		}
		idx.markType(fn.Signature, seenType)
		for _, p := range fn.Params {
			idx.markType(p.Type(), seenType)
		}
		for _, l := range fn.Locals {
			idx.markType(l.Type(), seenType)
		}
		for _, b := range fn.Blocks {
			for _, instr := range b.Instrs {
				if v, ok := instr.(ssa.Value); ok {
					idx.markType(v.Type(), seenType)
				}
				for _, op := range instr.Operands(nil) {
					if op == nil || *op == nil {
						continue
					}
					if g, ok := (*op).(*ssa.Global); ok && g.Pkg != nil && g.Pkg.Pkg != nil {
						idx.globals[g.Pkg.Pkg.Path()+"."+g.Name()] = true
					}
					idx.markType((*op).Type(), seenType)
				}
			}
		}
	}
	return idx
}

// markType records every named type reachable from t, descending through the
// type constructors that can wrap one.
func (idx *referenceIndex) markType(t types.Type, seen map[types.Type]bool) { //nolint:gocognit // one branch per type constructor
	if t == nil || seen[t] {
		return
	}
	seen[t] = true

	switch u := t.(type) {
	case *types.Named:
		idx.namedTypes[types.TypeString(u, nil)] = true
		for i := range u.TypeArgs().Len() {
			idx.markType(u.TypeArgs().At(i), seen)
		}
	case *types.Alias:
		idx.markType(types.Unalias(u), seen)
	case *types.Pointer:
		idx.markType(u.Elem(), seen)
	case *types.Slice:
		idx.markType(u.Elem(), seen)
	case *types.Array:
		idx.markType(u.Elem(), seen)
	case *types.Chan:
		idx.markType(u.Elem(), seen)
	case *types.Map:
		idx.markType(u.Key(), seen)
		idx.markType(u.Elem(), seen)
	case *types.Tuple:
		for i := range u.Len() {
			idx.markType(u.At(i).Type(), seen)
		}
	case *types.Signature:
		idx.markType(u.Params(), seen)
		idx.markType(u.Results(), seen)
	case *types.Struct:
		for i := range u.NumFields() {
			idx.markType(u.Field(i).Type(), seen)
		}
	}
}

// isReachable reports whether an SSA member is reachable from an entry point.
func isReachable(member ssa.Member, reachable map[*ssa.Function]bool, refs *referenceIndex, pkg *ssa.Package) bool {
	switch m := member.(type) {
	case *ssa.Function:
		return reachable[m]
	case *ssa.Type:
		named, ok := m.Type().(*types.Named)
		if !ok {
			// A defined type whose underlying object is not *types.Named is a
			// shape SSA does not hand back here; conservatively reachable
			// rather than silently counted as dead.
			return true
		}
		if refs.namedTypes[types.TypeString(named, nil)] {
			return true
		}
		for method := range named.Methods() {
			if fn := pkg.Prog.FuncValue(method); fn != nil && reachable[fn] {
				return true
			}
		}
		mset := types.NewMethodSet(types.NewPointer(named))
		for sel := range mset.Methods() {
			if sel == nil {
				continue
			}
			fn, ok := sel.Obj().(*types.Func)
			if !ok {
				continue
			}
			if ssaFn := pkg.Prog.FuncValue(fn); ssaFn != nil && reachable[ssaFn] {
				return true
			}
		}
		return false
	case *ssa.Global:
		if m.Pkg == nil || m.Pkg.Pkg == nil {
			return true
		}
		return refs.globals[m.Pkg.Pkg.Path()+"."+m.Name()]
	default:
		// *ssa.NamedConst: a constant is folded into its use site, so no SSA
		// instruction ever names it and there is nothing to observe. The
		// measurement is not performable for constants, so they are reported
		// as reachable rather than counted as dead on no evidence.
		return true
	}
}

// memberKind maps an SSA member onto the inventory's kind string.
func memberKind(member ssa.Member) string {
	switch member.(type) {
	case *ssa.Function:
		return "func"
	case *ssa.Type:
		return "type"
	case *ssa.Global:
		return "var"
	default:
		return "unknown"
	}
}

// buildPackageSummary aggregates the unreachable entries per package, most
// unreachable functions first.
func buildPackageSummary(items []UnreachableEntry) []PackageSummary {
	m := make(map[string]*PackageSummary)
	for _, item := range items {
		ps, ok := m[item.Package]
		if !ok {
			ps = &PackageSummary{Package: item.Package}
			m[item.Package] = ps
		}
		switch item.Kind {
		case "func":
			ps.UnreachableFuncs++
		case "type":
			ps.UnreachableTypes++
		default:
			ps.UnreachableOther++
		}
	}
	result := make([]PackageSummary, 0, len(m))
	for _, v := range m {
		result = append(result, *v)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].UnreachableFuncs != result[j].UnreachableFuncs {
			return result[i].UnreachableFuncs > result[j].UnreachableFuncs
		}
		return result[i].Package < result[j].Package
	})
	return result
}

// sortUnreachable orders the unreachable entries by a stable key so re-running
// at the same commit produces a byte-identical file.
func sortUnreachable(items []UnreachableEntry) {
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i], items[j]
		switch {
		case a.Package != b.Package:
			return a.Package < b.Package
		case a.Identifier != b.Identifier:
			return a.Identifier < b.Identifier
		case a.File != b.File:
			return a.File < b.File
		default:
			return a.Line < b.Line
		}
	})
}

// sortWhitelisted orders the whitelist entries by the same stable key.
func sortWhitelisted(items []WhitelistEntry) {
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i], items[j]
		switch {
		case a.Package != b.Package:
			return a.Package < b.Package
		case a.Identifier != b.Identifier:
			return a.Identifier < b.Identifier
		case a.File != b.File:
			return a.File < b.File
		default:
			return a.Line < b.Line
		}
	})
}

// writeInventory encodes the inventory as indented JSON.
func writeInventory(absRoot, outPath string, inv Inventory) error {
	absOut := outPath
	if !filepath.IsAbs(outPath) {
		absOut = filepath.Join(absRoot, outPath)
	}
	if err := os.MkdirAll(filepath.Dir(absOut), 0o755); err != nil { //nolint:gosec // G301: 0755 is the standard permission for a tool's output directory
		return fmt.Errorf("mkdir output dir: %w", err)
	}
	f, err := os.Create(absOut) //nolint:gosec // G304: absOut comes from a flag; the tool intentionally writes where it is told
	if err != nil {
		return fmt.Errorf("create output file: %w", err)
	}
	defer func() { _ = f.Close() }()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(inv); err != nil {
		return fmt.Errorf("encode inventory: %w", err)
	}
	fmt.Printf("inventory.json written: %s\n", absOut)
	return nil
}

// printReport prints the counts a reader needs to judge the run.
func printReport(inv Inventory) {
	fmt.Printf("  root_set:       %s\n", inv.RootSet)
	fmt.Printf("  entry_points:   %d\n", inv.EntryPoints)
	fmt.Printf("  total_exported: %d\n", inv.Summary.TotalExported)
	fmt.Printf("  reachable:      %d\n", inv.Summary.Reachable)
	fmt.Printf("  whitelisted:    %d\n", inv.Summary.Whitelisted)
	fmt.Printf("  unreachable:    %d\n", inv.Summary.Unreachable)
	if inv.Disagreement > 0 {
		// Printed with the counts rather than logged, because it qualifies
		// them: each of these is an identifier one SSA variant called
		// reachable and another did not.
		fmt.Printf("  (%d identifiers were reachable in one package variant and not in another; resolved as reachable)\n",
			inv.Disagreement)
	}
	if len(inv.ByPackage) > 0 {
		fmt.Println("\nTop 10 packages by unreachable count:")
		for _, ps := range inv.ByPackage[:min(len(inv.ByPackage), 10)] {
			fmt.Printf("  %-40s funcs=%d types=%d other=%d\n",
				ps.Package, ps.UnreachableFuncs, ps.UnreachableTypes, ps.UnreachableOther)
		}
	}
}

// summaryTemplate is the markdown report rendered next to the JSON inventory.
const summaryTemplate = `# Exported-API reachability summary

Root set: {{.RootSet}}
Entry points: {{.EntryPoints}} across {{len .EntryPkgs}} test packages.

## Overview

| Metric | Count |
|---|---|
| Total exported | {{.Summary.TotalExported}} |
| Reached by a test | {{.Summary.Reachable}} |
| Whitelisted | {{.Summary.Whitelisted}} |
| **Unreached** | **{{.Summary.Unreachable}}** |

## Top-20 packages by unreached exported identifiers

| Package | Funcs | Types | Other |
|---|---|---|---|
{{- range .Top20Packages}}
| {{.Package}} | {{.UnreachableFuncs}} | {{.UnreachableTypes}} | {{.UnreachableOther}} |
{{- end}}

## First 50 unreached functions

| Package | Identifier | File | Line |
|---|---|---|---|
{{- range .Top50Funcs}}
| {{.Package}} | {{.Identifier}} | {{.File}} | {{.Line}} |
{{- end}}

## Full by-package breakdown

| Package | Funcs | Types | Other |
|---|---|---|---|
{{- range .ByPackage}}
| {{.Package}} | {{.UnreachableFuncs}} | {{.UnreachableTypes}} | {{.UnreachableOther}} |
{{- end}}
`

// writeSummaryMD renders the markdown summary.
func writeSummaryMD(path string, inv Inventory) error {
	type templateData struct {
		RootSet       string
		EntryPoints   int
		EntryPkgs     []string
		Summary       Summary
		Top20Packages []PackageSummary
		Top50Funcs    []UnreachableEntry
		ByPackage     []PackageSummary
	}

	top20 := inv.ByPackage
	if len(top20) > 20 {
		top20 = top20[:20]
	}
	var funcs []UnreachableEntry
	for _, item := range inv.Unreachable {
		if item.Kind != "func" {
			continue
		}
		funcs = append(funcs, item)
		if len(funcs) >= 50 {
			break
		}
	}

	tmpl, err := template.New("summary").Parse(summaryTemplate)
	if err != nil {
		return fmt.Errorf("parse summary template: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // G301: 0755 is the standard permission for a tool's output directory
		return fmt.Errorf("mkdir summary dir: %w", err)
	}
	f, err := os.Create(path) //nolint:gosec // G304: path comes from a flag; the tool intentionally writes where it is told
	if err != nil {
		return fmt.Errorf("create summary file: %w", err)
	}
	defer func() { _ = f.Close() }()

	return tmpl.Execute(f, templateData{
		RootSet:     inv.RootSet,
		EntryPoints: inv.EntryPoints, EntryPkgs: inv.EntryPkgs,
		Summary: inv.Summary, Top20Packages: top20,
		Top50Funcs: funcs, ByPackage: inv.ByPackage,
	})
}

// isTestPkg reports whether an SSA package is one of go/packages' test variants.
func isTestPkg(p *ssa.Package) bool {
	if p.Pkg == nil {
		return false
	}
	path := p.Pkg.Path()
	return strings.HasSuffix(path, "_test") || strings.Contains(path, ".test")
}

// collectWhitelisted parses every Go file of the module and records the exported
// identifiers carrying a whitelistMarker annotation.
func collectWhitelisted(pkgs []*packages.Package, absRoot string, out map[identKey]WhitelistEntry, logger *slog.Logger) {
	seen := make(map[string]bool)

	packages.Visit(pkgs, func(p *packages.Package) bool {
		if !isOwnAnalyzablePkg(p.PkgPath) {
			return true
		}
		relPkg := relPackage(p.PkgPath)
		for _, file := range p.GoFiles {
			if seen[file] {
				continue
			}
			seen[file] = true

			fset := token.NewFileSet()
			astFile, err := parser.ParseFile(fset, file, nil, parser.ParseComments)
			if err != nil {
				logger.Debug("parse error", "file", file, "err", err)
				continue
			}
			for _, decl := range astFile.Decls {
				reason, ok := findWhitelistComment(astFile, fset, decl)
				if !ok {
					continue
				}
				relFile := strings.TrimPrefix(fset.Position(decl.Pos()).Filename, absRoot+string(filepath.Separator))
				recordWhitelistDecl(out, decl, fset, relPkg, relFile, reason)
			}
		}
		return true
	}, nil)
}

// recordWhitelistDecl writes one annotated declaration's exported identifiers
// into the whitelist map.
func recordWhitelistDecl(
	out map[identKey]WhitelistEntry,
	decl ast.Decl,
	fset *token.FileSet,
	relPkg, relFile, reason string,
) {
	add := func(name string, line int) {
		if !ast.IsExported(name) {
			return
		}
		out[identKey{pkg: relPkg, name: name}] = WhitelistEntry{
			Package: relPkg, Identifier: name, Reason: reason, File: relFile, Line: line,
		}
	}
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if d.Name != nil {
			add(d.Name.Name, fset.Position(d.Pos()).Line)
		}
	case *ast.GenDecl:
		for _, spec := range d.Specs {
			switch s := spec.(type) {
			case *ast.TypeSpec:
				add(s.Name.Name, fset.Position(s.Pos()).Line)
			case *ast.ValueSpec:
				for _, name := range s.Names {
					add(name.Name, fset.Position(s.Pos()).Line)
				}
			}
		}
	}
}

// findWhitelistComment looks for a whitelistMarker comment belonging to decl.
//
// The annotation is only recognised on the line DIRECTLY ABOVE the declaration
// (`commentLine == declLine-1`). An annotation placed higher in a doc-comment
// block — with any prose line between it and the `func` / `type` / `var` keyword
// — is silently ignored, and the identifier is then reported as unreachable
// although its author believes it is whitelisted. That silence has cost a full
// debugging cycle before; it is kept rather than widened because a window of
// several lines makes an annotation on one declaration leak onto the next.
//
// So: put the annotation on the LAST line of the doc comment.
func findWhitelistComment(f *ast.File, fset *token.FileSet, decl ast.Decl) (string, bool) {
	declLine := fset.Position(decl.Pos()).Line

	for _, cg := range f.Comments {
		for _, c := range cg.List {
			if fset.Position(c.Pos()).Line != declLine-1 {
				continue
			}
			text := strings.TrimSpace(strings.TrimPrefix(c.Text, "//"))
			if after, ok := strings.CutPrefix(text, whitelistMarker); ok {
				return strings.Trim(after, `"`), true
			}
		}
	}
	return "", false
}
