// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package main implements the per-package statement-coverage floor for
// go-fabric.
//
// # What it is for
//
// This module is heavily tested in absolute terms, so the risk it carries is
// not "too little coverage" — it is coverage quietly draining out of one
// package while the aggregate stays respectable. A single number over the
// whole module hides that: a package can lose fifteen points and the total
// moves by a fraction, because every other package's statements dilute it.
// The floors below are therefore per package, and each one is set at or just
// under what that package measured when it was written. They ratchet: a
// change may raise a floor, and lowering one is a deliberate edit somebody
// has to justify in review rather than a number that drifts on its own.
//
// It is a regression detector, not a coverage target. A floor says "this
// package was covered this well and must not silently get worse"; it says
// nothing about whether that level is good enough, and raising a low one is
// a decision about what deserves testing, which belongs in a change of its
// own.
//
// # How it measures
//
// It parses the summary lines of `go test -cover ./...` — the same numbers a
// contributor sees — rather than a merged coverage profile. That keeps the
// gate's input identical to what a human reads back when it fails, and it
// keeps the per-package attribution honest: the percentage go prints for a
// package counts only that package's OWN test binary, so a package whose
// statements happen to be executed by a neighbour's tests does not get
// credit for it here.
//
// The report is passed in with -report (or on stdin) rather than produced by
// this program shelling out to `go test`, so this package's own test can
// exercise the parser without recursively invoking the suite it is part of.
//
// # Failure modes it reports
//
//   - a package measured below its floor — the regression this exists for;
//   - a package in the report with no floor entry — a new package must get
//     a floor, otherwise the gate silently stops covering it;
//   - a floor entry with no measurement — a stale entry left behind by a
//     renamed or deleted package, which would otherwise sit there forever
//     guarding nothing;
//   - a FAIL line — a report from a failed run is not a measurement, and
//     ratcheting against it would compare against whatever partial coverage
//     the failure produced.
//
// Run it through `make cover-check`.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// modulePath is the import-path prefix every package of this module shares.
// Report lines are recognised by it, so unrelated build chatter that happens
// to carry a percentage cannot be mistaken for a measurement.
const modulePath = "github.com/SukramJ/go-fabric"

// staleFloorSlack is how far a measurement may sit above its floor before
// the tool advises raising it. It is advice, never a failure: a package that
// gained coverage must not turn the gate red, but a floor that trails the
// truth by a wide margin has stopped detecting anything and should be
// pulled up with the next change that touches the package.
const staleFloorSlack = 5.0

// coverageRe matches the percentage in a `go test -cover` summary line.
var coverageRe = regexp.MustCompile(`coverage: (\d+(?:\.\d+)?)% of statements`)

// errFloorViolated is returned when at least one package is below its floor
// or the floor table no longer matches the module's package set.
var errFloorViolated = errors.New("coverage floor violated")

// measurement is one package's line in the report.
type measurement struct {
	pkg string
	// pct is the measured statement coverage. Meaningful only when
	// measurable is true.
	pct float64
	// measurable is false for a package go reported as having no test files
	// or no statements at all — there is no percentage to compare.
	measurable bool
	// note carries go's own words for an unmeasurable package, so the
	// printed table says why a row has no number.
	note string
}

func main() {
	// run returns the exit code instead of calling os.Exit itself: the
	// report file is closed by a defer, and os.Exit skips defers.
	os.Exit(run())
}

func run() int {
	reportPath := flag.String("report", "", "path to a `go test -cover ./...` report; empty reads stdin")
	flag.Parse()

	in := io.Reader(os.Stdin)
	if *reportPath != "" {
		f, err := os.Open(*reportPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "coverfloor: %v\n", err)
			return 2
		}
		defer f.Close() //nolint:errcheck // read-only handle
		in = f
	}

	measurements, err := parseReport(in)
	if err != nil {
		fmt.Fprintf(os.Stderr, "coverfloor: %v\n", err)
		return 2
	}
	if len(measurements) == 0 {
		fmt.Fprintln(os.Stderr, "coverfloor: the report contains no package lines — was it produced by `go test -cover ./...`?")
		return 2
	}

	report := check(measurements, floors)
	fmt.Print(report.table())
	for _, line := range report.advisories {
		fmt.Println("advice:", line)
	}
	if len(report.problems) == 0 {
		fmt.Printf("coverfloor: %d package(s) at or above their floor\n", len(measurements))
		return 0
	}
	fmt.Fprintln(os.Stderr)
	for _, p := range report.problems {
		fmt.Fprintln(os.Stderr, "FAIL:", p)
	}
	fmt.Fprintf(os.Stderr, "\ncoverfloor: %v\n", errFloorViolated)
	return 1
}

// parseReport reads `go test -cover` summary lines. Lines that name no
// package of this module are ignored, so the caller may pass the raw test
// output including build chatter.
func parseReport(r io.Reader) ([]measurement, error) {
	var (
		out    []measurement
		failed []string
		seen   = map[string]bool{}
	)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		pkg := packageOf(line)
		if pkg == "" {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "FAIL") {
			failed = append(failed, pkg)
			continue
		}
		if seen[pkg] {
			continue
		}
		seen[pkg] = true
		switch {
		case strings.Contains(line, "[no test files]"):
			out = append(out, measurement{pkg: pkg, note: "no test files"})
		case strings.Contains(line, "[no statements]"):
			out = append(out, measurement{pkg: pkg, note: "no statements"})
		default:
			m := coverageRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			pct, err := strconv.ParseFloat(m[1], 64)
			if err != nil {
				return nil, fmt.Errorf("parsing %q: %w", line, err)
			}
			out = append(out, measurement{pkg: pkg, pct: pct, measurable: true})
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading report: %w", err)
	}
	if len(failed) > 0 {
		sort.Strings(failed)
		return nil, fmt.Errorf("the report contains failing package(s) — a failed run is not a measurement: %s",
			strings.Join(failed, ", "))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].pkg < out[j].pkg })
	return out, nil
}

// packageOf returns the module package named on a report line, or "" when
// the line names none.
func packageOf(line string) string {
	for _, f := range strings.Fields(line) {
		if f == modulePath || strings.HasPrefix(f, modulePath+"/") {
			return f
		}
	}
	return ""
}

// result is what check produced: the rows to print, the failures, and the
// non-fatal advice.
type result struct {
	rows       []row
	problems   []string
	advisories []string
}

// row is one line of the printed table.
type row struct {
	pkg      string
	measured string
	floor    string
	status   string
}

// check compares every measurement against the floor table, in both
// directions: a package with no floor and a floor with no package are both
// reported, because either one leaves the gate quietly measuring less than
// it claims to.
func check(measurements []measurement, table []packageFloor) result {
	byPkg := make(map[string]packageFloor, len(table))
	for _, f := range table {
		byPkg[f.pkg] = f
	}
	var res result
	measured := map[string]bool{}
	for _, m := range measurements {
		measured[m.pkg] = true
		f, ok := byPkg[m.pkg]
		if !ok {
			res.problems = append(res.problems,
				m.pkg+" has no floor entry — add one to script/coverfloor/floors.go at or just below its measured coverage")
			res.rows = append(res.rows, row{pkg: m.pkg, measured: pctString(m), floor: "—", status: "no floor entry"})
			continue
		}
		switch {
		case !m.measurable:
			res.rows = append(res.rows, row{pkg: m.pkg, measured: m.note, floor: floorString(f.min), status: "exempt — " + f.why})
			if f.min > 0 {
				res.problems = append(res.problems, fmt.Sprintf(
					"%s reports %q but carries a floor of %.1f%% — the floor can never be met", m.pkg, m.note, f.min,
				))
			}
		case m.pct+1e-9 < f.min:
			res.rows = append(res.rows, row{pkg: m.pkg, measured: pctString(m), floor: floorString(f.min), status: "BELOW FLOOR"})
			res.problems = append(res.problems, fmt.Sprintf(
				"%s: %.1f%% < floor %.1f%% — restore the coverage, or lower the floor deliberately and say why",
				m.pkg, m.pct, f.min,
			))
		default:
			res.rows = append(res.rows, row{pkg: m.pkg, measured: pctString(m), floor: floorString(f.min), status: "ok"})
			if f.min > 0 && m.pct-f.min >= staleFloorSlack {
				res.advisories = append(res.advisories, fmt.Sprintf(
					"%s is %.1f points above its floor — raise it to %.1f%% to keep the ratchet biting",
					m.pkg, m.pct-f.min, math.Floor(m.pct),
				))
			}
		}
	}
	for _, f := range table {
		if !measured[f.pkg] {
			res.problems = append(res.problems,
				f.pkg+" has a floor but appears in no report line — delete the stale entry, or run the check over the whole module")
		}
	}
	return res
}

// pctString renders a measurement for the table.
func pctString(m measurement) string {
	if !m.measurable {
		return m.note
	}
	return fmt.Sprintf("%.1f%%", m.pct)
}

// floorString renders a floor for the table.
func floorString(floor float64) string {
	if floor == 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f%%", floor)
}

// table renders the per-package result, shortest-path names first so the
// output reads like the `go test` output it came from.
func (r result) table() string {
	var sb strings.Builder
	width := len("package")
	for _, row := range r.rows {
		if n := len(short(row.pkg)); n > width {
			width = n
		}
	}
	fmt.Fprintf(&sb, "%-*s  %10s  %8s  %s\n", width, "package", "measured", "floor", "status")
	for _, row := range r.rows {
		fmt.Fprintf(&sb, "%-*s  %10s  %8s  %s\n", width, short(row.pkg), row.measured, row.floor, row.status)
	}
	return sb.String()
}

// short trims the module prefix so the table reads as package paths.
func short(pkg string) string {
	if pkg == modulePath {
		return "."
	}
	return strings.TrimPrefix(pkg, modulePath+"/")
}
