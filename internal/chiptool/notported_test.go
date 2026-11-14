// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package chiptool

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// No build tag: TestCertifiabilityDocument holds the "not ported yet" rows
// in every `go test ./...`.

// pendingRun is the evidence of a run-pending row: the unit test, in the
// package directory dir (relative to the repository root), that holds the
// answer the certification case expects. A test name stays true from one
// commit to the next, as long as the test is there; the guard reads it.
type pendingRun struct {
	dir, test string
}

// runPendingEvidence are the family cases whose fix is in the tree and whose
// certification run is pending (status "not ported yet", owner decision
// 82), case id → the unit test that holds the step's answer. The family run
// runs them; once one passes there, its row leaves the family's exclude.
var runPendingEvidence = map[string]pendingRun{
	"PAVST/2.3":      {"cluster/camera", "TestPushTransportCertificationSteps"},
	"PAVST/2.4":      {"cluster/camera", "TestPushTransportCertificationSteps"},
	"PAVST/2.6":      {"cluster/camera", "TestPushTransportCertificationSteps"},
	"PAVST/2.11":     {"cluster/camera", "TestPushTransportCertificationSteps"},
	"WASHERCTRL/2.2": {"cluster/appliance", "TestLaundryWasherControlsCertificationStep"},
}

// runPending is the exclude row of a family case whose fix is in the tree
// and whose certification run is pending: what is the step, the answer it
// expects and how the module now gives it; id names the case's
// runPendingEvidence, which the row names in turn.
func runPending(id, what string) gap {
	ev := runPendingEvidence[id]
	return gap{class: classNotPortedYet, reason: "run pending: " + what + ". `" + ev.test + "` (" + ev.dir +
		") holds that answer by unit test; the family run runs the case, and no run has measured it with the fix yet"}
}

// statusTables are the tables the "not ported yet" rows live in; the guard
// takes them as arguments so a test can hand it a row that breaks a rule.
type statusTables struct {
	notRun   map[string]gap // matterJSCasesNotRun
	role     map[string]gap // controllerRoleCases
	families []family       // chipFamilies
	pending  map[string]pendingRun
}

func currentStatusTables() statusTables {
	return statusTables{notRun: matterJSCasesNotRun, role: controllerRoleCases, families: chipFamilies, pending: runPendingEvidence}
}

// checkNotPortedYet holds the rows of status "not ported yet" (owner
// decision 82). A row of matterJSCasesNotRun names a matter.js
// certification case at the snapshot's pin (ts), and that case's TS file,
// and no registered Go-TH port runs it (ports, nil in a build without the
// chiptool tag, and the golden file's goCert). A row of a family's exclude
// is a run-pending one: one case, with runPendingEvidence naming a unit
// test that exists under root, names the case and is named by the row, and
// no counts of the case in the last run — a case that passed leaves the
// status. The status is no controller-role row and no recorded gap.
func checkNotPortedYet(tb testing.TB, g docGolden, ts matterJSCertCases, ports map[string]string, st statusTables, root string) {
	tb.Helper()
	files := map[string]string{}
	for _, f := range ts.Cases {
		if id, ok := tsCaseID(f); ok {
			files[id] = f
		}
	}
	for _, id := range slices.Sorted(maps.Keys(st.notRun)) {
		if st.notRun[id].class != classNotPortedYet {
			continue
		}
		file, atPin := files[id]
		if !atPin {
			tb.Errorf("%s is not ported yet, but matter.js has no such case at %s", id, ts.Commit)
			continue
		}
		if !strings.Contains(st.notRun[id].reason, "("+file+":") {
			tb.Errorf("%s is not ported yet, but its row does not name %s, the case's file at %s", id, file, ts.Commit)
		}
		if src, ok := ports[id]; ok {
			tb.Errorf("%s is not ported yet, but the registered Go-TH port %s runs it: the row leaves the status", id, src)
		} else if gc, ok := g.GoCert[id]; ok {
			tb.Errorf("%s is not ported yet, but testdata/%s's goCert runs it (%s): the row leaves the status", id, chipCasesGoldenFile, gc.Source)
		}
	}
	rows := map[string]bool{}
	for i := range st.families {
		fam := &st.families[i]
		prefix := fam.name
		if fam.cases != "" {
			prefix = fam.cases
		}
		for _, n := range slices.Sorted(maps.Keys(fam.exclude)) {
			gp := fam.exclude[n]
			if gp.class != classNotPortedYet {
				continue
			}
			id := fam.name + "/" + n
			rows[id] = true
			ev, ok := st.pending[id]
			if !ok {
				tb.Errorf("%s is not ported yet in the family table, but names no unit test (runPendingEvidence): "+
					"a family row of the status is a fix whose certification run is pending", id)
				continue
			}
			if !strings.Contains(gp.reason, ev.test) {
				tb.Errorf("%s: the row does not name %s, the unit test that holds its fix", id, ev.test)
			}
			tc := "TC-" + prefix + "-" + n
			switch found, names, err := unitTestNames(root, ev.dir, ev.test, tc); {
			case err != nil:
				tb.Errorf("%s: %v", id, err)
			case !found:
				tb.Errorf("%s: %s has no %s: name the unit test that holds the case's answer", id, ev.dir, ev.test)
			case !names:
				tb.Errorf("%s: %s (%s) does not name %s in its comment or body", id, ev.test, ev.dir, tc)
			}
			if passedIn(g, id) {
				tb.Errorf("%s passed in the last run: its run is no longer pending, so its row leaves the family's exclude", id)
			}
		}
		for _, n := range slices.Sorted(maps.Keys(fam.knownProblems)) {
			if fam.knownProblems[n].class == classNotPortedYet {
				tb.Errorf("%s/%s is a recorded gap of status not ported yet: a case that passes is measured", fam.name, n)
			}
		}
	}
	for _, id := range slices.Sorted(maps.Keys(st.pending)) {
		if !rows[id] {
			tb.Errorf("%s has run-pending evidence but no not-ported-yet row in its family's exclude", id)
		}
	}
	for _, id := range slices.Sorted(maps.Keys(st.role)) {
		if st.role[id].class == classNotPortedYet {
			tb.Errorf("%s is a controller-role row of status not ported yet: such a case is a row of matterJSCasesNotRun", id)
		}
	}
}

// unitTestNames reports whether a _test.go file of dir (relative to root)
// declares func test, and whether that test's doc comment or body names the
// certification case tc (TC-PAVST-2.1 is not named by TC-PAVST-2.11).
func unitTestNames(root, dir, test, tc string) (found, names bool, err error) {
	paths, err := filepath.Glob(filepath.Join(root, dir, "*_test.go"))
	if err != nil {
		return false, false, err
	}
	tcRe := regexp.MustCompile(regexp.QuoteMeta(tc) + `(?:$|[^0-9.]|\.[^0-9])`)
	for _, p := range paths {
		data, err := os.ReadFile(p) //nolint:gosec // a test file of the repository
		if err != nil {
			return false, false, err
		}
		text := string(data)
		i := strings.Index(text, "\nfunc "+test+"(")
		if i < 0 {
			continue
		}
		lines := strings.Split(text[:i], "\n")
		doc := ""
		for k := len(lines) - 1; k >= 0 && strings.HasPrefix(lines[k], "//"); k-- {
			doc = lines[k] + "\n" + doc
		}
		body := text[i+1:]
		if end := strings.Index(body, "\n}\n"); end >= 0 {
			body = body[:end]
		}
		return true, tcRe.MatchString(doc + body), nil
	}
	return false, false, nil
}

// TestNotPortedYetRowsAreHeld: a "not ported yet" row names a matter.js
// certification case at the snapshot's pin that no registered Go-TH port
// runs, and the TS file of exactly that case; a run-pending row is a family
// case whose unit test exists and names the case, and that has not passed in
// the last run; the status appears in no other table.
func TestNotPortedYetRowsAreHeld(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", chipCasesGoldenFile))
	if err != nil {
		t.Fatal(err)
	}
	var g docGolden
	if err := json.Unmarshal(data, &g); err != nil {
		t.Fatal(err)
	}
	ts := loadMatterJSCertCases(t)
	root := filepath.Join("..", "..")
	ports := map[string]string{}
	ported := ""
	for id, c := range g.GoCert {
		ports[id] = c.Source
		if strings.HasSuffix(c.Source, ".test.ts") && (ported == "" || id < ported) {
			ported = id
		}
	}
	if ported == "" {
		t.Fatal("the golden file's goCert names no matter.js case")
	}

	// A run-pending row of PAVST/2.3 with its evidence, set here so the
	// test does not depend on which rows the family table carries.
	pendingRow := gap{class: classNotPortedYet, reason: "run pending: TestPushTransportCertificationSteps (cluster/camera)"}
	base := func() statusTables {
		st := currentStatusTables()
		st.notRun = maps.Clone(st.notRun)
		st.role = maps.Clone(st.role)
		st.pending = maps.Clone(st.pending)
		st.pending["PAVST/2.3"] = pendingRun{"cluster/camera", "TestPushTransportCertificationSteps"}
		st.families = withExclude(t, st.families, "PAVST", "2.3", pendingRow)
		return st
	}
	check := func(g docGolden, st statusTables) []string {
		r := &errorRecorder{TB: t}
		checkNotPortedYet(r, g, ts, ports, st, root)
		return r.errs
	}
	if errs := check(g, base()); len(errs) != 0 {
		t.Fatalf("the tables as they are fail the guard: %q", errs)
	}

	cases := []struct {
		name string
		edit func(*docGolden, *statusTables)
		want string
	}{
		{"a case matter.js does not have", func(_ *docGolden, st *statusTables) {
			st.notRun["XX/9.9"] = portableNow("TC-XX-9.9.test.ts"+":1", "nothing")
		}, "XX/9.9 is not ported yet, but matter.js has no such case"},
		{"a case a registered port runs", func(_ *docGolden, st *statusTables) {
			st.notRun[ported] = portableNow(ports[ported]+":1", "nothing")
		}, ported + " is not ported yet, but the registered Go-TH port"},
		{"the TS file of another case", func(_ *docGolden, st *statusTables) {
			st.notRun["OO/3.2"] = portableNow("TC-WNCV-6.1.test.ts:25", "nothing")
		}, "OO/3.2 is not ported yet, but its row does not name TC-OO-3.2.test.ts"},
		{"a family row without its unit test", func(_ *docGolden, st *statusTables) {
			delete(st.pending, "PAVST/2.3")
		}, "PAVST/2.3 is not ported yet in the family table, but names no unit test"},
		{"a unit test that is not there", func(_ *docGolden, st *statusTables) {
			st.pending["PAVST/2.3"] = pendingRun{"cluster/camera", "TestNoSuchCertificationStep"}
		}, "PAVST/2.3: cluster/camera has no TestNoSuchCertificationStep"},
		{"a unit test of another case", func(_ *docGolden, st *statusTables) {
			st.pending["PAVST/2.3"] = pendingRun{"cluster/appliance", "TestLaundryWasherControlsCertificationStep"}
		}, "PAVST/2.3: TestLaundryWasherControlsCertificationStep (cluster/appliance) does not name TC-PAVST-2.3"},
		{"a row that does not name its unit test", func(_ *docGolden, st *statusTables) {
			st.families = withExclude(t, st.families, "PAVST", "2.3", gap{class: classNotPortedYet, reason: "run pending"})
		}, "PAVST/2.3: the row does not name TestPushTransportCertificationSteps"},
		{"a case that passed in the last run", func(g *docGolden, _ *statusTables) {
			g.Cases = maps.Clone(g.Cases)
			g.Cases["PAVST/2.3"] = json.RawMessage(`{"steps":1,"stepsSkipped":0}`)
		}, "PAVST/2.3 passed in the last run"},
		{"evidence without its row", func(_ *docGolden, st *statusTables) {
			st.pending["OO/2.1"] = pendingRun{"cluster/camera", "TestPushTransportCertificationSteps"}
		}, "OO/2.1 has run-pending evidence but no not-ported-yet row"},
		{"the status in another table", func(_ *docGolden, st *statusTables) {
			st.role["DA/1.9"] = gap{class: classNotPortedYet, reason: "TC-DA-1.9.test.ts:32"}
		}, "DA/1.9 is a controller-role row of status not ported yet"},
	}
	for _, c := range cases {
		g2, st := g, base()
		c.edit(&g2, &st)
		errs := check(g2, st)
		if !slices.ContainsFunc(errs, func(e string) bool { return strings.Contains(e, c.want) }) {
			t.Errorf("%s: the guard said %q, want %q", c.name, errs, c.want)
		}
	}
}

// withExclude is fams with family name's exclude row for case set to gp,
// the table itself left as it is.
func withExclude(t *testing.T, fams []family, name, c string, gp gap) []family {
	t.Helper()
	out := slices.Clone(fams)
	for i := range out {
		if out[i].name == name {
			out[i].exclude = maps.Clone(out[i].exclude)
			if out[i].exclude == nil {
				out[i].exclude = map[string]gap{}
			}
			out[i].exclude[c] = gp
			return out
		}
	}
	t.Fatalf("no family %s", name)
	return nil
}
