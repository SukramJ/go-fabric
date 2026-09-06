// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

// Tests for the floor checker itself.
//
// They run on a report string held here rather than on a real `go test`
// run: this package is part of the module the gate measures, so a test that
// produced its input by invoking the suite would invoke itself. The parser
// and the comparison are the whole of the logic; the report format is
// pinned by the sample below, which carries every line shape `go test
// -cover ./...` emits.

import (
	"strings"
	"testing"
)

// sampleReport carries one line of each shape go emits: a covered package,
// a package with no statements, a package with no test files, and a package
// that compiled but ran nothing.
const sampleReport = `ok  	github.com/SukramJ/go-fabric	0.666s	coverage: [no statements]
ok  	github.com/SukramJ/go-fabric/tlv	3.908s	coverage: 91.9% of statements
	github.com/SukramJ/go-fabric/endpoint/endpointtest		coverage: 0.0% of statements
?   	github.com/SukramJ/go-fabric/internal/bridgeseam	[no test files]
ok  	github.com/SukramJ/go-fabric/transport/message	4.400s	coverage: 100.0% of statements
`

// TestParseReportReadsEveryLineShape pins the parse of the four summary
// shapes. A shape the parser silently skips would drop that package out of
// the comparison, and the gate would then pass by not looking.
func TestParseReportReadsEveryLineShape(t *testing.T) {
	t.Parallel()
	got, err := parseReport(strings.NewReader(sampleReport))
	if err != nil {
		t.Fatalf("parseReport: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("parsed %d measurements, want 5: %+v", len(got), got)
	}
	byPkg := map[string]measurement{}
	for _, m := range got {
		byPkg[m.pkg] = m
	}
	if m := byPkg["github.com/SukramJ/go-fabric/tlv"]; !m.measurable || m.pct != 91.9 {
		t.Errorf("tlv = %+v, want measurable 91.9%%", m)
	}
	if m := byPkg["github.com/SukramJ/go-fabric/transport/message"]; !m.measurable || m.pct != 100 {
		t.Errorf("transport/message = %+v, want measurable 100%%", m)
	}
	if m := byPkg["github.com/SukramJ/go-fabric/endpoint/endpointtest"]; !m.measurable || m.pct != 0 {
		t.Errorf("endpointtest = %+v, want a measurable 0.0%%", m)
	}
	if m := byPkg["github.com/SukramJ/go-fabric"]; m.measurable || m.note != "no statements" {
		t.Errorf("root = %+v, want unmeasurable with note %q", m, "no statements")
	}
	if m := byPkg["github.com/SukramJ/go-fabric/internal/bridgeseam"]; m.measurable || m.note != "no test files" {
		t.Errorf("bridgeseam = %+v, want unmeasurable with note %q", m, "no test files")
	}
}

// TestParseReportRejectsAFailedRun verifies that a report containing a FAIL
// line is refused rather than measured. Coverage from a failed run is
// whatever the run got to before it stopped, and ratcheting against it would
// bake a partial number into the floors.
func TestParseReportRejectsAFailedRun(t *testing.T) {
	t.Parallel()
	const report = `ok  	github.com/SukramJ/go-fabric/tlv	3.9s	coverage: 91.9% of statements
FAIL	github.com/SukramJ/go-fabric/bridge	2.8s
`
	if _, err := parseReport(strings.NewReader(report)); err == nil {
		t.Fatal("parseReport accepted a report with a FAIL line; want an error")
	}
}

// TestCheckPassesAtAndAboveTheFloor is the green half of the gate: a
// measurement exactly on its floor passes, and so does one above it.
func TestCheckPassesAtAndAboveTheFloor(t *testing.T) {
	t.Parallel()
	res := check(
		[]measurement{
			{pkg: "a", pct: 80, measurable: true},
			{pkg: "b", pct: 81.4, measurable: true},
		},
		[]packageFloor{{pkg: "a", min: 80}, {pkg: "b", min: 81}},
	)
	if len(res.problems) != 0 {
		t.Fatalf("problems = %v, want none", res.problems)
	}
}

// TestCheckFailsBelowTheFloor is the bite: one tenth of a point under the
// floor has to be reported, or the ratchet is decorative.
func TestCheckFailsBelowTheFloor(t *testing.T) {
	t.Parallel()
	res := check(
		[]measurement{{pkg: "a", pct: 79.9, measurable: true}},
		[]packageFloor{{pkg: "a", min: 80}},
	)
	if len(res.problems) != 1 {
		t.Fatalf("problems = %v, want exactly one", res.problems)
	}
	if !strings.Contains(res.problems[0], "79.9") || !strings.Contains(res.problems[0], "80.0") {
		t.Errorf("problem %q names neither the measurement nor the floor", res.problems[0])
	}
}

// TestCheckFailsOnAPackageWithNoFloor covers the way a gate like this
// usually rots: a package is added, nobody adds a floor, and the check keeps
// reporting success over a shrinking share of the module.
func TestCheckFailsOnAPackageWithNoFloor(t *testing.T) {
	t.Parallel()
	res := check(
		[]measurement{{pkg: "newpkg", pct: 90, measurable: true}},
		nil,
	)
	if len(res.problems) != 1 || !strings.Contains(res.problems[0], "newpkg") {
		t.Fatalf("problems = %v, want one naming newpkg", res.problems)
	}
}

// TestCheckFailsOnAStaleFloor covers the other direction: a floor left
// behind by a deleted or renamed package guards nothing, and hides that the
// package it names is gone.
func TestCheckFailsOnAStaleFloor(t *testing.T) {
	t.Parallel()
	res := check(nil, []packageFloor{{pkg: "gone", min: 50}})
	if len(res.problems) != 1 || !strings.Contains(res.problems[0], "gone") {
		t.Fatalf("problems = %v, want one naming gone", res.problems)
	}
}

// TestCheckFailsOnAnUnreachableFloor guards against a floor above zero on a
// package go reports as having no statements or no test files: nothing can
// ever satisfy it, so it would fail forever rather than measure anything.
func TestCheckFailsOnAnUnreachableFloor(t *testing.T) {
	t.Parallel()
	res := check(
		[]measurement{{pkg: "seam", note: "no test files"}},
		[]packageFloor{{pkg: "seam", min: 10}},
	)
	if len(res.problems) != 1 || !strings.Contains(res.problems[0], "seam") {
		t.Fatalf("problems = %v, want one naming seam", res.problems)
	}
}

// TestCheckAdvisesRaisingAStaleFloor pins the non-fatal half: coverage well
// above its floor is reported as advice and never as a failure, so a change
// that improves a package cannot turn the gate red.
func TestCheckAdvisesRaisingAStaleFloor(t *testing.T) {
	t.Parallel()
	res := check(
		[]measurement{{pkg: "a", pct: 92.5, measurable: true}},
		[]packageFloor{{pkg: "a", min: 80}},
	)
	if len(res.problems) != 0 {
		t.Fatalf("problems = %v, want none — improved coverage must not fail the gate", res.problems)
	}
	if len(res.advisories) != 1 || !strings.Contains(res.advisories[0], "92.0") {
		t.Fatalf("advisories = %v, want one suggesting 92.0%%", res.advisories)
	}
}

// TestFloorsTableIsWellFormed checks the committed table itself: no
// duplicate package, every path inside this module, no floor outside 0..100,
// and a stated reason for every zero floor. A duplicate entry would make the
// effective floor depend on map iteration order at load time, and a zero
// floor without a reason is indistinguishable from an unfinished edit.
func TestFloorsTableIsWellFormed(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for _, f := range floors {
		if seen[f.pkg] {
			t.Errorf("%s appears twice in floors", f.pkg)
		}
		seen[f.pkg] = true
		if f.pkg != modulePath && !strings.HasPrefix(f.pkg, modulePath+"/") {
			t.Errorf("%s is not a package of this module", f.pkg)
		}
		if f.min < 0 || f.min > 100 {
			t.Errorf("%s: floor %.1f is outside 0..100", f.pkg, f.min)
		}
		if f.min == 0 && strings.TrimSpace(f.why) == "" {
			t.Errorf("%s: a zero floor needs a why — say what makes the package unmeasurable", f.pkg)
		}
	}
}
