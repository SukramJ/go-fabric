// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// The tests in this file check the COMMITTED inventory.json snapshot: that it
// exists and parses, that its counters agree with the arrays they count, and —
// the one test here that says something about the tree rather than about the
// document — that the number of unreached exported identifiers sits exactly on
// its ratchet.
//
// None of them re-runs the analyzer. A green run says the last committed
// snapshot is well-formed and on its ratchet, not that the current tree still
// produces that snapshot. Regenerate with `make reachability` and read the diff
// to check the tree itself; CI does exactly that, so a stale snapshot fails
// there rather than here.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// reachabilityUnreachedRatchet is the number of exported identifiers the
// committed inventory is allowed to report as reached by no test.
//
// It is an exact ratchet, checked in both directions on purpose. Growth has to
// be justified in the commit that raises it; a DROP has to be locked in by
// lowering it, so ground gained cannot be handed back later under a ceiling
// that was never tightened.
//
// Set to 11 by the first measurement on this module, at the tree where the
// analyzer landed. All eleven were verified by hand: no file in the module
// other than the declaring one so much as spells the name. Six functions
// (cluster/measurement.CelsiusToInt16, contract.DeviceTypeName,
// endpoint.ComposeNodeLabel, mdns.DeriveUniqueIDFromIdentity,
// schema.DeviceTypeAllowsServerCluster, secure/setup.IsValidSetupPIN), one
// type (cluster/core.SignVidVerificationResponse, named only inside two
// t.Error strings) and four package-level vars — three sentinel errors no code
// path returns (secure/channel.ErrCounterReplayed,
// secure/sigma.ErrResumptionMICInvalid, and schema.DeviceTypeServerClusters,
// read only by the untested DeviceTypeAllowsServerCluster) plus one unused test
// fixture key (secure/attestation.TestPAANoVIDPrivateKey).
//
// The number was NOT tuned to look good and nothing was annotated to bring it
// down: the module carries zero fabric:reachable annotations, so every one of
// the 11 is a measurement rather than an unrefuted assertion. Writing a test
// that reaches any of them lowers this constant by one.
const reachabilityUnreachedRatchet = 11

// inventoryWhitelistEntry mirrors one whitelist row of inventory.json.
type inventoryWhitelistEntry struct {
	Package    string `json:"package"`
	Identifier string `json:"identifier"`
	Reason     string `json:"reason"`
	File       string `json:"file"`
	Line       int    `json:"line"`
}

// inventoryUnreachableEntry mirrors one unreachable row of inventory.json.
type inventoryUnreachableEntry struct {
	Package    string `json:"package"`
	Identifier string `json:"identifier"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	Kind       string `json:"kind"`
}

// inventorySummary mirrors the summary object of inventory.json.
type inventorySummary struct {
	TotalExported int `json:"total_exported"`
	Reachable     int `json:"reachable"`
	Whitelisted   int `json:"whitelisted"`
	Unreachable   int `json:"unreachable"`
}

// inventoryPackageSummary mirrors one by_package row of inventory.json.
type inventoryPackageSummary struct {
	Package          string `json:"package"`
	UnreachableFuncs int    `json:"unreachable_funcs"`
	UnreachableTypes int    `json:"unreachable_types"`
	UnreachableOther int    `json:"unreachable_other"`
}

// reachabilityInventory is the committed inventory document.
type reachabilityInventory struct {
	Generated   string                      `json:"generated"`
	Head        string                      `json:"head"`
	RootSet     string                      `json:"root_set"`
	EntryPoints int                         `json:"entry_points"`
	EntryPkgs   []string                    `json:"entry_packages"`
	Summary     inventorySummary            `json:"summary"`
	ByPackage   []inventoryPackageSummary   `json:"by_package"`
	Unreachable []inventoryUnreachableEntry `json:"unreachable"`
	Whitelisted []inventoryWhitelistEntry   `json:"whitelisted"`
}

// moduleRootFromTestFile resolves the module root relative to this file.
func moduleRootFromTestFile(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// script/reachability/inventory_test.go -> ../../ is the module root.
	abs, err := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	if err != nil {
		t.Fatalf("resolve module root: %v", err)
	}
	return abs
}

// loadReachabilityInventory reads and parses the committed inventory.
func loadReachabilityInventory(t *testing.T) reachabilityInventory {
	t.Helper()
	path := filepath.Join(moduleRootFromTestFile(t), "script", "reachability", "inventory.json")

	data, err := os.ReadFile(path) //nolint:gosec // G304: fixed path under the module root
	if err != nil {
		t.Fatalf("inventory.json unreadable (%s): %v\nRun `make reachability` to produce it.", path, err)
	}
	var inv reachabilityInventory
	if err := json.Unmarshal(data, &inv); err != nil {
		t.Fatalf("inventory.json is not valid JSON: %v", err)
	}
	return inv
}

// TestReachabilityInventoryExists is the floor the rest of the file stands on:
// every other test loads the same document, so a missing or truncated inventory
// fails here rather than passing everything else vacuously.
func TestReachabilityInventoryExists(t *testing.T) {
	t.Parallel()
	inv := loadReachabilityInventory(t)

	if inv.Head == "" {
		t.Error("head is empty")
	}
	if inv.RootSet == "" {
		t.Error("root_set is empty — the inventory must say what it was measured against")
	}
	// The root set of a library module is its tests. Zero entry points would
	// mean RTA started from nothing and reported the whole exported surface as
	// unreached, which is a broken run wearing the shape of a finding.
	if inv.EntryPoints == 0 {
		t.Error("entry_points is 0 — the root set was empty, so the numbers below mean nothing")
	}
	if len(inv.EntryPkgs) == 0 {
		t.Error("entry_packages is empty")
	}
	t.Logf("head=%s entry_points=%d tested_packages=%d total_exported=%d unreached=%d whitelisted=%d",
		inv.Head, inv.EntryPoints, len(inv.EntryPkgs),
		inv.Summary.TotalExported, inv.Summary.Unreachable, inv.Summary.Whitelisted)
}

// TestReachabilityInventoryRowsAreWellFormed checks that every row carries the
// fields its readers use.
//
// This is a shape check on a generated artefact, not a statement about the
// tree: the analyzer writes these fields unconditionally, so no production edit
// can break it. What it catches is corruption of the committed file — a bad
// merge resolution, a truncated write.
func TestReachabilityInventoryRowsAreWellFormed(t *testing.T) {
	t.Parallel()
	inv := loadReachabilityInventory(t)

	validKinds := map[string]bool{"func": true, "type": true, "var": true, "unknown": true}
	for i, e := range inv.Unreachable {
		switch {
		case e.Package == "":
			t.Errorf("unreachable[%d]: package is empty (identifier=%q)", i, e.Identifier)
		case e.Identifier == "":
			t.Errorf("unreachable[%d]: identifier is empty (package=%q)", i, e.Package)
		case e.File == "":
			t.Errorf("unreachable[%d]: file is empty (%s.%s)", i, e.Package, e.Identifier)
		case e.Line <= 0:
			t.Errorf("unreachable[%d]: line must be > 0 (%s.%s)", i, e.Package, e.Identifier)
		case !validKinds[e.Kind]:
			t.Errorf("unreachable[%d]: invalid kind %q (%s.%s)", i, e.Kind, e.Package, e.Identifier)
		}
	}
	for i, e := range inv.Whitelisted {
		if e.Reason == "" {
			t.Errorf("whitelisted[%d]: reason is empty (%s.%s) — an exemption without a reason is not one",
				i, e.Package, e.Identifier)
		}
		if e.Identifier == "" || e.File == "" || e.Line <= 0 {
			t.Errorf("whitelisted[%d]: incomplete row (%s.%s file=%s line=%d)",
				i, e.Package, e.Identifier, e.File, e.Line)
		}
	}
}

// TestReachabilityInventoryByPackageMatchesRows re-implements the per-package
// aggregation rather than re-reading it, so a change to the analyzer's grouping
// does reach this test.
func TestReachabilityInventoryByPackageMatchesRows(t *testing.T) {
	t.Parallel()
	inv := loadReachabilityInventory(t)

	if inv.Summary.Unreachable != len(inv.Unreachable) {
		t.Errorf("summary.unreachable=%d does not match len(unreachable)=%d",
			inv.Summary.Unreachable, len(inv.Unreachable))
	}
	if inv.Summary.Whitelisted != len(inv.Whitelisted) {
		t.Errorf("summary.whitelisted=%d does not match len(whitelisted)=%d",
			inv.Summary.Whitelisted, len(inv.Whitelisted))
	}
	if len(inv.ByPackage) == 0 && len(inv.Unreachable) > 0 {
		t.Fatal("by_package is empty although unreachable has rows")
	}

	type counts struct{ funcs, types, other int }
	want := make(map[string]counts)
	for _, item := range inv.Unreachable {
		c := want[item.Package]
		switch item.Kind {
		case "func":
			c.funcs++
		case "type":
			c.types++
		default:
			c.other++
		}
		want[item.Package] = c
	}
	for _, ps := range inv.ByPackage {
		exp, ok := want[ps.Package]
		if !ok {
			t.Errorf("by_package lists %q, which has no unreachable rows", ps.Package)
			continue
		}
		if ps.UnreachableFuncs != exp.funcs || ps.UnreachableTypes != exp.types || ps.UnreachableOther != exp.other {
			t.Errorf("by_package[%s] = funcs:%d types:%d other:%d, counted funcs:%d types:%d other:%d",
				ps.Package, ps.UnreachableFuncs, ps.UnreachableTypes, ps.UnreachableOther,
				exp.funcs, exp.types, exp.other)
		}
	}
}

// TestReachabilityInventoryExcludesTestDeclarations checks that nothing declared
// in a _test.go file reaches the unreached list — the analyzer auto-whitelists
// those, since a test helper is not part of the module's exported API.
func TestReachabilityInventoryExcludesTestDeclarations(t *testing.T) {
	t.Parallel()
	inv := loadReachabilityInventory(t)

	for i, e := range inv.Unreachable {
		if strings.HasSuffix(e.File, "_test.go") {
			t.Errorf("unreachable[%d]: %s.%s is declared in %s — a _test.go declaration should have been auto-whitelisted",
				i, e.Package, e.Identifier, e.File)
		}
		if strings.HasPrefix(e.File, "script/") {
			t.Errorf("unreachable[%d]: %s.%s is build tooling (%s) — script/ is outside the analyzed population",
				i, e.Package, e.Identifier, e.File)
		}
	}
}

// TestReachabilityUnreachedCountIsOnItsRatchet is the one test in this file
// that says something about the tree rather than about the document's shape.
//
// It reads the committed snapshot, so it can only notice a change once somebody
// regenerates — `make reachability` and reading the diff remains the honest way
// to check the tree, and CI regenerates on every run so a stale snapshot fails
// there. What this removes is the step where the change is invisible even after
// regeneration.
//
// It fails in BOTH directions. Above the ratchet, an export lost its last test.
// Below it, an export gained one and the ratchet has to be tightened in the same
// commit, or the next regression slips back in under a ceiling nobody moved.
func TestReachabilityUnreachedCountIsOnItsRatchet(t *testing.T) {
	t.Parallel()
	inv := loadReachabilityInventory(t)

	got := inv.Summary.Unreachable
	switch {
	case got > reachabilityUnreachedRatchet:
		names := make([]string, 0, len(inv.Unreachable))
		for _, e := range inv.Unreachable {
			names = append(names, e.Package+"."+e.Identifier)
		}
		t.Errorf("the committed inventory reports %d exported identifiers reached by no test, "+
			"%d more than the ratchet of %d.\n\n"+
			"Either write a test that reaches them, or delete them, or — if a host consumer "+
			"genuinely calls something no test here can — annotate it with\n"+
			"  // fabric:reachable:reason=\"...\"\n"+
			"on the line DIRECTLY ABOVE the declaration (anywhere else is silently ignored), "+
			"and raise reachabilityUnreachedRatchet in the same commit saying why.\n\n"+
			"Current list: %s",
			got, got-reachabilityUnreachedRatchet, reachabilityUnreachedRatchet, strings.Join(names, ", "))
	case got < reachabilityUnreachedRatchet:
		t.Errorf("the committed inventory is down to %d unreached identifiers, below the ratchet of %d. "+
			"Lower reachabilityUnreachedRatchet to %d in this commit so the ground gained cannot be "+
			"given back unnoticed.", got, reachabilityUnreachedRatchet, got)
	}
}
