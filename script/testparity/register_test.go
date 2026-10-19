// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// The tests in this file are the guard on the COMMITTED register: the
// snapshot is the pin's, every snapshot path has exactly one entry, every Go
// test an entry names exists, a ported or partial mapping was checked against
// the blob now pinned (an exclusion too), the page is what rendering produces, and the number of
// planned files sits exactly on its ratchet. None of them needs a matter.js
// clone; regenerating the snapshot (make testparity-snapshot) is a pin lift's
// job, and its diff is what tells the next mapping check where to look.
package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testparityPlannedRatchet is the number of matter.js test files the
// committed register lists as planned: neither ported, nor in part, nor
// excluded with a class.
//
// It is an exact ratchet, checked in both directions on purpose, like
// reachabilityUnreachedRatchet. Growth has to be justified in the commit that
// raises it — a pin lift that brings new upstream test files does, and says
// so; a DROP has to be locked in by lowering it, so a file ported once cannot
// slide back to planned under a ceiling that was never tightened.
//
// Set to 134 by the initial fill (brief BE) at matter.js 862ad75f: 324 test
// files, every one decided from the seed read and then checked against its
// describe/it names and the Go tests' Test and t.Run names. Phase 16 brings
// the protocol/test and node/test/node share of it to zero, milestone 3 the
// node/test/behaviors share.
const testparityPlannedRatchet = 53

// moduleRoot is where the committed artefacts live, seen from this package.
const moduleRoot = "../.."

func mustLoad(t *testing.T) (*Snapshot, *Register) {
	t.Helper()
	snap, err := loadSnapshot(filepath.Join(moduleRoot, snapshotFile))
	if err != nil {
		t.Fatal(err)
	}
	reg, err := loadRegister(filepath.Join(moduleRoot, registerFile))
	if err != nil {
		t.Fatal(err)
	}
	return snap, reg
}

func TestTestparitySnapshotIsThePin(t *testing.T) {
	snap, _ := mustLoad(t)
	commit, err := pinnedCommit(filepath.Join(moduleRoot, schemaFile))
	if err != nil {
		t.Fatal(err)
	}
	if snap.Commit != commit {
		t.Fatalf("snapshot is of matter.js %s, parity/schema.json pins %s — run make testparity-snapshot and re-check every changed file's mapping", snap.Commit, commit)
	}
	if strings.Join(snap.Roots, ",") != strings.Join(snapshotRoots, ",") {
		t.Errorf("snapshot roots %v, want %v", snap.Roots, snapshotRoots)
	}
	for i, f := range snap.Files {
		if i > 0 && snap.Files[i-1].Path >= f.Path {
			t.Errorf("snapshot not sorted or duplicate at %s", f.Path)
		}
		if !shaPattern.MatchString(f.SHA) {
			t.Errorf("%s: sha %q", f.Path, f.SHA)
		}
		if !strings.HasSuffix(f.Path, "Test.ts") || (!strings.HasPrefix(f.Path, snapshotRoots[0]+"/") && !strings.HasPrefix(f.Path, snapshotRoots[1]+"/")) {
			t.Errorf("%s: not a *Test.ts under the snapshot roots", f.Path)
		}
		if f.Lines <= 0 {
			t.Errorf("%s: %d lines", f.Path, f.Lines)
		}
	}
}

func TestTestparityRegisterCoversSnapshot(t *testing.T) {
	snap, reg := mustLoad(t)
	if err := reg.check(snap); err != nil {
		t.Fatal(err)
	}
}

func TestTestparityGoReferencesResolve(t *testing.T) {
	_, reg := mustLoad(t)
	funcs := map[string]map[string]bool{} // package dir -> Test function names
	for _, e := range reg.Entries {
		for _, ref := range e.Go {
			dir, name, _ := strings.Cut(ref, ":")
			if _, ok := funcs[dir]; !ok {
				funcs[dir] = testFuncs(t, filepath.Join(moduleRoot, filepath.FromSlash(dir)))
			}
			if !funcs[dir][name] {
				t.Errorf("%s: %s — no func %s in %s/*_test.go", e.Path, ref, name, dir)
			}
		}
	}
}

// testFuncs parses a package directory's _test.go files and returns its
// top-level Test functions.
func testFuncs(t *testing.T, dir string) map[string]bool {
	t.Helper()
	names := map[string]bool{}
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Errorf("%s: %v", dir, err)
		return names
	}
	fset := token.NewFileSet()
	for _, ent := range ents {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, ent.Name()), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Errorf("%s: %v", ent.Name(), err)
			continue
		}
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "Test") {
				names[fn.Name.Name] = true
			}
		}
	}
	return names
}

func TestTestparityPageIsCurrent(t *testing.T) {
	snap, reg := mustLoad(t)
	if err := reg.check(snap); err != nil {
		t.Skip("register does not match the snapshot; TestTestparityRegisterCoversSnapshot reports it")
	}
	want := renderPage(snap, reg)
	got, err := os.ReadFile(filepath.Join(moduleRoot, pageFile))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s is stale: run make testparity and commit the result", pageFile)
	}
}

func TestTestparityPlannedRatchet(t *testing.T) {
	_, reg := mustLoad(t)
	got := reg.count()[StatusPlanned]
	switch {
	case got > testparityPlannedRatchet:
		t.Fatalf("%d planned files, ratchet %d: a file went back to planned, or new upstream test files arrived — justify it and raise testparityPlannedRatchet in the same commit", got, testparityPlannedRatchet)
	case got < testparityPlannedRatchet:
		t.Fatalf("%d planned files, ratchet %d: lock the gain in by lowering testparityPlannedRatchet to %d", got, testparityPlannedRatchet, got)
	}
}

func TestTestparityEntryValidation(t *testing.T) {
	sha := strings.Repeat("a", 40)
	ok := []Entry{
		{Path: "p", Status: StatusPorted, Go: []string{"im:TestX"}, Reason: "r", SnapshotSHA: sha},
		{Path: "p", Status: StatusPartial, Go: []string{"secure/channel:TestY"}, Missing: []string{"m"}, Reason: "r", SnapshotSHA: sha},
		{Path: "p", Status: StatusPlanned, Reason: "r", SnapshotSHA: sha},
		{Path: "p", Status: StatusPlanned, Until: "M5 phase 23", Reason: "r", SnapshotSHA: sha},
		{Path: "p", Status: StatusExcluded, Class: ClassControllerStage, Until: "M4", Reason: "r", SnapshotSHA: sha},
		{Path: "p", Status: StatusExcluded, Class: ClassDeviceBLEThread, Until: "M7", Reason: "r", SnapshotSHA: sha},
		{Path: "p", Status: StatusExcluded, Class: ClassJSRuntimeOnly, Reason: "r", SnapshotSHA: sha},
	}
	for i, e := range ok {
		if err := e.validate(); err != nil {
			t.Errorf("ok[%d]: %v", i, err)
		}
	}
	bad := map[string]Entry{
		"unknown status":        {Path: "p", Status: "done", Reason: "r", SnapshotSHA: sha},
		"unknown class":         {Path: "p", Status: StatusExcluded, Class: "later", Reason: "r", SnapshotSHA: sha},
		"go on excluded":        {Path: "p", Status: StatusExcluded, Class: ClassJointFabric, Go: []string{"im:TestX"}, Reason: "r", SnapshotSHA: sha},
		"no reason":             {Path: "p", Status: StatusPlanned, SnapshotSHA: sha},
		"no sha":                {Path: "p", Status: StatusPlanned, Reason: "r"},
		"no path":               {Status: StatusPlanned, Reason: "r", SnapshotSHA: sha},
		"ported without go":     {Path: "p", Status: StatusPorted, Reason: "r", SnapshotSHA: sha},
		"ported with missing":   {Path: "p", Status: StatusPorted, Go: []string{"im:TestX"}, Missing: []string{"m"}, Reason: "r", SnapshotSHA: sha},
		"partial without miss":  {Path: "p", Status: StatusPartial, Go: []string{"im:TestX"}, Reason: "r", SnapshotSHA: sha},
		"malformed go ref":      {Path: "p", Status: StatusPorted, Go: []string{"im.TestX"}, Reason: "r", SnapshotSHA: sha},
		"class on ported":       {Path: "p", Status: StatusPorted, Go: []string{"im:TestX"}, Class: ClassJSRuntimeOnly, Reason: "r", SnapshotSHA: sha},
		"planned with go":       {Path: "p", Status: StatusPlanned, Go: []string{"im:TestX"}, Reason: "r", SnapshotSHA: sha},
		"stage without until":   {Path: "p", Status: StatusExcluded, Class: ClassControllerStage, Reason: "r", SnapshotSHA: sha},
		"until on js-runtime":   {Path: "p", Status: StatusExcluded, Class: ClassJSRuntimeOnly, Until: "M9", Reason: "r", SnapshotSHA: sha},
		"malformed until":       {Path: "p", Status: StatusPlanned, Until: "later", Reason: "r", SnapshotSHA: sha},
		"until past M7":         {Path: "p", Status: StatusExcluded, Class: ClassControllerStage, Until: "M9", Reason: "r", SnapshotSHA: sha},
		"missing on excluded":   {Path: "p", Status: StatusExcluded, Class: ClassJointFabric, Missing: []string{"m"}, Reason: "r", SnapshotSHA: sha},
		"blank reason (spaces)": {Path: "p", Status: StatusPlanned, Reason: "  ", SnapshotSHA: sha},
	}
	for name, e := range bad {
		if err := e.validate(); err == nil {
			t.Errorf("%s: validate accepted %+v", name, e)
		}
	}
	unsorted := Register{Entries: []Entry{ok[2], ok[2]}}
	if err := unsorted.validate(); err == nil {
		t.Error("validate accepted a duplicate path")
	}
}

func TestTestparityCheckReportsDrift(t *testing.T) {
	sha, other := strings.Repeat("a", 40), strings.Repeat("b", 40)
	snap := &Snapshot{Files: []SnapshotFile{{Path: "a", SHA: sha}, {Path: "b", SHA: sha}, {Path: "d", SHA: sha}, {Path: "e", SHA: sha}}}
	reg := &Register{Entries: []Entry{
		{Path: "a", Status: StatusPorted, Go: []string{"im:TestX"}, Reason: "r", SnapshotSHA: other},
		{Path: "b", Status: StatusExcluded, Class: ClassJSRuntimeOnly, Reason: "r", SnapshotSHA: other},
		{Path: "c", Status: StatusPlanned, Reason: "r", SnapshotSHA: sha},
		{Path: "d", Status: StatusPlanned, Reason: "r", SnapshotSHA: other},
	}}
	err := reg.check(snap)
	if err == nil {
		t.Fatal("check accepted a register that does not match the snapshot")
	}
	for _, want := range []string{"a: ported against blob", "re-check the mapping", "b: excluded against blob", "re-check the exclusion", "c: in the register but not in the snapshot", "e: in the snapshot but not in the register"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("check error lacks %q:\n%v", want, err)
		}
	}
	if strings.Contains(err.Error(), "d: ") {
		t.Errorf("check reported a planned entry's changed blob:\n%v", err)
	}
}

func TestTestparitySnapshotParsing(t *testing.T) {
	ls := "100644 blob " + strings.Repeat("1", 40) + "\tpackages/node/test/b/ZTest.ts\n" +
		"100644 blob " + strings.Repeat("2", 40) + "\tpackages/node/test/b/helper.ts\n" +
		"040000 tree " + strings.Repeat("3", 40) + "\tpackages/node/test/b\n" +
		"100644 blob " + strings.Repeat("4", 40) + "\tpackages/node/test/a/ATest.ts\n"
	files, err := parseLsTree([]byte(ls))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Path != "packages/node/test/a/ATest.ts" || files[1].Path != "packages/node/test/b/ZTest.ts" {
		t.Fatalf("parseLsTree = %+v", files)
	}
	if _, err := parseLsTree([]byte("garbage\n")); err == nil {
		t.Error("parseLsTree accepted a malformed line")
	}

	src := "import x from \"y\";\n" +
		"describe(\"Top one\", () => {\n" +
		"    describe(\"nested\", () => {\n" +
		"        it(\"does\", () => {});\n" +
		"    });\n" +
		"});\n" +
		"describe('single \\'quoted\\'', () => {});\n" +
		"describe.skip(`template`, () => {});\n" +
		"describe(Foo.name, () => {});\n" +
		"describe(\n    \"next line\", () => {});"
	got := topLevelDescribes([]byte(src))
	want := []string{"Top one", `single \'quoted\'`, "template", "Foo.name", "next line"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("topLevelDescribes = %q, want %q", got, want)
	}
	for in, n := range map[string]int{"": 0, "a": 1, "a\n": 1, "a\nb": 2, "a\nb\n": 2} {
		if got := countLines([]byte(in)); got != n {
			t.Errorf("countLines(%q) = %d, want %d", in, got, n)
		}
	}
}
