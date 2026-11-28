// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package chiptool

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The verdict scripts of chiptool.yml, run here against sample inputs. A
// test that only finds a string in the workflow holds its shape; these
// hold what the script decides (review II, DR5a-ci-03 and DR5a-ci-09).

// chiptoolWorkflow returns .github/workflows/chiptool.yml.
func chiptoolWorkflow(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "chiptool.yml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// runBlockAfter returns the first `run: |` block of wf after the first
// line that contains anchor, dedented: the lines indented past the run
// key, up to the first non-blank line that is not.
func runBlockAfter(t *testing.T, wf, anchor string) string {
	t.Helper()
	lines := strings.Split(wf, "\n")
	start := -1
	for i, l := range lines {
		if strings.Contains(l, anchor) {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("chiptool.yml has no line with %q", anchor)
	}
	for i := start; i < len(lines); i++ {
		trimmed := strings.TrimLeft(lines[i], " ")
		if trimmed != "run: |" && trimmed != "- run: |" {
			continue
		}
		col := len(lines[i]) - len(trimmed)
		var body []string
		indent := -1
		for _, l := range lines[i+1:] {
			if strings.TrimSpace(l) == "" {
				body = append(body, "")
				continue
			}
			n := len(l) - len(strings.TrimLeft(l, " "))
			if n <= col {
				break
			}
			if indent < 0 {
				indent = n
			}
			if n < indent {
				t.Fatalf("chiptool.yml: the run block after %q dedents below its first line", anchor)
			}
			body = append(body, l[indent:])
		}
		return strings.Join(body, "\n") + "\n"
	}
	t.Fatalf("chiptool.yml has no `run: |` after %q", anchor)
	return ""
}

// runStepScript runs script as GitHub runs a step without `shell:` on
// Linux (`bash -e {0}`) with env added, and reports its exit code.
func runStepScript(t *testing.T, script string, env []string) int {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatalf("bash, which runs the workflow's steps, is not on PATH: %v", err)
	}
	f := filepath.Join(t.TempDir(), "step.sh")
	if err := os.WriteFile(f, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bash, "--noprofile", "--norc", "-e", f) //nolint:gosec // the workflow's own script
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		t.Logf("exit %d: %s", ee.ExitCode(), strings.TrimSpace(string(out)))
		return ee.ExitCode()
	}
	t.Fatalf("bash: %v", err)
	return -1
}

// TestWorkflowGoCertStepVerdict runs the gocert job's "Go-TH cases" step
// with the make call replaced by a sample log. A Go-TH group was green
// with two multicast cases skipped: the step matched only the leg's own
// skip line (DR5a-ci-03). The samples are `go test -v` output as Go
// prints it (a case's SKIP line is indented under the leg's PASS line;
// a -run that matches nothing prints no PASS line for the leg). make's
// exit status is kept: the stand-in prints the sample and exits with the
// row's status inside the step's own pipeline, so a go test that fails
// after the leg's PASS line (a panic in a goroutine the leg left, TestMain)
// fails the step through `set -o pipefail` (RR5a-02).
func TestWorkflowGoCertStepVerdict(t *testing.T) {
	const makeLine = "make chiptool-gocert CHIPTOOL_TIMEOUT=40m 2>&1 | tee /tmp/gocert.log"
	script := runBlockAfter(t, chiptoolWorkflow(t), "- name: Go-TH cases")
	if strings.Count(script, makeLine) != 1 {
		t.Fatalf("the Go-TH step does not run %q once:\n%s", makeLine, script)
	}
	passed := "=== RUN   TestGoCertificationCases\n" +
		"--- PASS: TestGoCertificationCases (586.12s)\n" +
		"    --- PASS: TestGoCertificationCases/BIND/2.3 (12.01s)\n" +
		"    --- PASS: TestGoCertificationCases/SC/5.3 (9.40s)\nPASS\n"
	for _, tc := range []struct {
		name   string
		log    string
		makeRC int
		want   int
	}{
		{"every case passed", passed, 0, 0},
		{"every case passed, make exits 2", passed, 2, 2},
		{"a case skipped", "=== RUN   TestGoCertificationCases\n" +
			"--- PASS: TestGoCertificationCases (586.12s)\n" +
			"    --- SKIP: TestGoCertificationCases/BIND/2.3 (0.00s)\n" +
			"    --- PASS: TestGoCertificationCases/SC/5.3 (9.40s)\nPASS\n", 0, 1},
		{"the leg skipped", "=== RUN   TestGoCertificationCases\n" +
			"    gocert_test.go:328: no multicast here: x\n" +
			"--- SKIP: TestGoCertificationCases (0.01s)\nPASS\n", 0, 1},
		{"no test ran", "testing: warning: no tests to run\nPASS\n", 0, 1},
		{"a case's log names the leg", "=== RUN   TestGoCertificationCases\n" +
			"    x_test.go:1: --- PASS: TestGoCertificationCases (quoted)\n" +
			"--- FAIL: TestGoCertificationCases (1.00s)\nFAIL\n", 0, 1}, // make's status 0: the row holds the anchored grep alone
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			sample := filepath.Join(dir, "sample.log")
			if err := os.WriteFile(sample, []byte(tc.log), 0o600); err != nil {
				t.Fatal(err)
			}
			log := filepath.Join(dir, "gocert.log")
			stand := `{ cat "$SAMPLE"; exit "$MAKE_RC"; } 2>&1 | tee /tmp/gocert.log`
			s := strings.Replace(script, makeLine, stand, 1)
			s = strings.ReplaceAll(s, "/tmp/gocert.log", `"$LOG"`)
			env := []string{"SAMPLE=" + sample, "LOG=" + log, "MAKE_RC=" + strconv.Itoa(tc.makeRC)}
			if got := runStepScript(t, s, env); got != tc.want {
				t.Errorf("exit %d, want %d", got, tc.want)
			}
		})
	}
}

// TestWorkflowGateVerdict runs the required gate's script (job
// chip-families-gate) over its inputs. The structural guard only asked
// that some job needs gocert; deleting $GOCERT from the accepted pattern
// passed it (DR5a-ci-09).
func TestWorkflowGateVerdict(t *testing.T) {
	script := runBlockAfter(t, chiptoolWorkflow(t), "  chip-families-gate:")
	if !strings.Contains(script, `"$CHANGES/$FAMILIES/$GOCERT"`) {
		t.Fatalf("the gate's script does not judge $CHANGES/$FAMILIES/$GOCERT:\n%s", script)
	}
	allPass := map[string]string{
		"CHANGES": "success", "FAMILIES": "success", "GOCERT": "success", "RELEASE": "false",
		"SUITE": "success", "CONTROL": "success", "BLE_WIFI": "success", "BLE_CASES": "success", "NESTED": "success",
		"CODE": "true", "LONG_BASE": "skipped", "SHAPE": "false",
	}
	// A release pull request in release shape (issue 632): the `code
	// touched` job reports code, every chip leg is skipped, nested-release
	// runs and long-on-base reads the legs on the base commit.
	shape := map[string]string{
		"RELEASE": "true", "SHAPE": "true", "SUITE": "skipped", "GOCERT": "skipped", "CONTROL": "skipped",
		"BLE_WIFI": "skipped", "BLE_CASES": "skipped", "FAMILIES": "skipped", "LONG_BASE": "success",
	}
	shapeWith := func(k, v string) map[string]string {
		m := map[string]string{k: v}
		for sk, sv := range shape {
			if sk != k {
				m[sk] = sv
			}
		}
		return m
	}
	for _, tc := range []struct {
		name string
		set  map[string]string
		want int
	}{
		{"all passed", nil, 0},
		{"undue", map[string]string{"FAMILIES": "skipped", "GOCERT": "skipped"}, 0},
		{"Go-TH failed", map[string]string{"GOCERT": "failure"}, 1},
		{"Go-TH cancelled", map[string]string{"GOCERT": "cancelled"}, 1},
		{"Go-TH skipped alone", map[string]string{"GOCERT": "skipped"}, 1},
		{"families skipped alone", map[string]string{"FAMILIES": "skipped"}, 1},
		{"families failed", map[string]string{"FAMILIES": "failure"}, 1},
		{"changes failed", map[string]string{"CHANGES": "failure", "FAMILIES": "skipped", "GOCERT": "skipped"}, 1},
		{"changes skipped", map[string]string{"CHANGES": "skipped", "FAMILIES": "skipped", "GOCERT": "skipped"}, 1},
		{"release, all passed", map[string]string{"RELEASE": "true"}, 0},
		{"release, Go-TH skipped", map[string]string{"RELEASE": "true", "GOCERT": "skipped"}, 1},
		{"release, both undue", map[string]string{"RELEASE": "true", "GOCERT": "skipped", "FAMILIES": "skipped"}, 1},
		{"release, suite skipped", map[string]string{"RELEASE": "true", "SUITE": "skipped"}, 1},
		{"release, nested failed", map[string]string{"RELEASE": "true", "NESTED": "failure"}, 1},
		{"release, control cancelled", map[string]string{"RELEASE": "true", "CONTROL": "cancelled"}, 1},
		{"release, BLE-Wi-Fi skipped", map[string]string{"RELEASE": "true", "BLE_WIFI": "skipped"}, 1},
		{"release, BLE cases failed", map[string]string{"RELEASE": "true", "BLE_CASES": "failure"}, 1},
		{"release, docs only, long on base passed", map[string]string{"RELEASE": "true", "CODE": "false", "LONG_BASE": "success"}, 0},
		{"release, docs only, long on base failed", map[string]string{"RELEASE": "true", "CODE": "false", "LONG_BASE": "failure"}, 1},
		{"release, docs only, long on base skipped", map[string]string{"RELEASE": "true", "CODE": "false", "LONG_BASE": "skipped"}, 1},
		{"release, code, long on base not read", map[string]string{"RELEASE": "true", "CODE": "true", "LONG_BASE": "failure"}, 0},
		{"release shape, verdicts on base passed", shape, 0},
		{"release shape, verdicts on base failed", shapeWith("LONG_BASE", "failure"), 1},
		{"release shape, verdicts on base skipped", shapeWith("LONG_BASE", "skipped"), 1},
		{"release shape, verdicts on base cancelled", shapeWith("LONG_BASE", "cancelled"), 1},
		{"release shape, nested failed", shapeWith("NESTED", "failure"), 1},
		{"release shape, nested skipped", shapeWith("NESTED", "skipped"), 1},
		{"release shape, changes failed", shapeWith("CHANGES", "failure"), 1},
		{"release, code, legs on base not accepted", map[string]string{
			"RELEASE": "true", "SUITE": "skipped", "GOCERT": "skipped", "CONTROL": "skipped",
			"BLE_WIFI": "skipped", "BLE_CASES": "skipped", "FAMILIES": "skipped", "LONG_BASE": "success",
		}, 1},
		{"release, docs only, legs on base not accepted", map[string]string{
			"RELEASE": "true", "CODE": "false", "SUITE": "skipped", "GOCERT": "skipped", "CONTROL": "skipped",
			"BLE_WIFI": "skipped", "BLE_CASES": "skipped", "FAMILIES": "skipped", "LONG_BASE": "success",
		}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := make([]string, 0, len(allPass))
			for k, v := range allPass {
				if o, ok := tc.set[k]; ok {
					v = o
				}
				env = append(env, k+"="+v)
			}
			if got := runStepScript(t, script, env); got != tc.want {
				t.Errorf("exit %d, want %d", got, tc.want)
			}
		})
	}
}

// TestWorkflowGroupsSelection runs the `changes` job's groups step over
// the events it distinguishes: `long` runs on the nightly run, a dispatch
// and a release pull request that changes code, never on a release pull
// request that changes documentation only (long-on-base reads it on the
// base commit, owner 2026-11-24); both commissioning groups run on every
// pull request.
func TestWorkflowGroupsSelection(t *testing.T) {
	script := runBlockAfter(t, chiptoolWorkflow(t), "- id: groups")
	for _, tc := range []struct {
		name                     string
		event, full, release, co string
		long, cadmin, lightsCC   bool
	}{
		{"schedule", "schedule", "false", "false", "false", true, true, true},
		{"dispatch", "workflow_dispatch", "false", "false", "false", true, true, true},
		{"pull request", "pull_request", "false", "false", "true", false, true, false},
		{"labelled pull request", "pull_request", "true", "false", "true", false, true, true},
		{"release, code", "pull_request", "true", "true", "true", true, true, true},
		{"release, docs only", "pull_request", "true", "true", "false", false, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out")
			env := []string{"EVENT=" + tc.event, "FULL=" + tc.full, "RELEASE=" + tc.release, "CODE=" + tc.co, "FAMILIES_INPUT=", "GITHUB_OUTPUT=" + out}
			if got := runStepScript(t, script, env); got != 0 {
				t.Fatalf("exit %d", got)
			}
			data, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			m := string(data)
			for group, want := range map[string]bool{"long": tc.long, "commissioning-cadmin": tc.cadmin, "app-lights-cc": tc.lightsCC} {
				if got := strings.Contains(m, `"group":"`+group+`"`); got != want {
					t.Errorf("group %s runs: %v, want %v\n%s", group, got, want, m)
				}
			}
		})
	}
}

// TestWorkflowLongOnBaseVerdict runs the long-on-base job's step with a
// stand-in gh that applies the step's own --jq filters (with jq) to
// sample API answers: green only when one nightly or dispatched run on the
// base commit passed every job the pull request takes from there —
// `certification families (long)` for a documentation-only release pull
// request, every chip-tool leg, Go-TH group and family group of the
// release matrix for one in release shape (issue 632). A dispatched run
// with update_fixtures relaxes the family groups' drift checks and is no
// such verdict (P10-sup-7001): the event `workflow_dispatch+update` stands
// for it, its display title rendered from the workflow's run-name.
func TestWorkflowLongOnBaseVerdict(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Fatalf("jq, which gh's --jq and the step run, is not on PATH: %v", err)
	}
	wf := chiptoolWorkflow(t)
	script := runBlockAfter(t, wf, "  long-on-base:")
	const families = `{"include":[{"group":"core-su","families":"SU"},{"group":"long","families":"DGGEN"}]}`
	legs := []string{
		"chip-tool commissions the reference daemon",
		"chip-tool control leg (CHIP app, no go-fabric)",
		"chip-tool commissions over BLE onto Wi-Fi (chip's mocks)",
		"certification cases commissioning over BLE onto Wi-Fi (chip's mocks)",
		"Go-TH certification cases (gocert-cadmin)",
		"Go-TH certification cases (gocert-dd)",
		"Go-TH certification cases (gocert-rest)",
		"certification families (core-su)",
		"certification families (long)",
	}
	type run struct {
		id, event string
		passed    []string // the jobs that concluded success
		failed    []string
	}
	without := func(name string) []string {
		var out []string
		for _, l := range legs {
			if l != name {
				out = append(out, l)
			}
		}
		return out
	}
	long := []string{"certification families (long)"}
	for _, tc := range []struct {
		name  string
		shape string
		runs  []run
		want  int
	}{
		{"docs only, passed", "false", []run{{"11", "schedule", long, nil}}, 0},
		{"docs only, failed", "false", []run{{"11", "schedule", nil, long}}, 1},
		{"docs only, no long job", "false", []run{{"11", "schedule", []string{"certification families (core-su)"}, nil}}, 1},
		{"docs only, no run", "false", nil, 1},
		{"docs only, second run passed", "false", []run{{"11", "schedule", nil, long}, {"12", "workflow_dispatch", long, nil}}, 0},
		{"docs only, a pull request's run", "false", []run{{"11", "pull_request", long, nil}}, 1},
		{"release shape, every leg passed", "true", []run{{"11", "schedule", legs, nil}}, 0},
		{"release shape, dispatched", "true", []run{{"11", "workflow_dispatch", legs, nil}}, 0},
		{"release shape, long alone", "true", []run{{"11", "schedule", long, nil}}, 1},
		{"release shape, a Go-TH group failed", "true", []run{{"11", "schedule", without("Go-TH certification cases (gocert-dd)"), []string{"Go-TH certification cases (gocert-dd)"}}}, 1},
		{"release shape, a family group missing", "true", []run{{"11", "schedule", without("certification families (core-su)"), nil}}, 1},
		{"release shape, the control leg missing", "true", []run{{"11", "schedule", without("chip-tool control leg (CHIP app, no go-fabric)"), nil}}, 1},
		{"release shape, split over two runs", "true", []run{{"11", "schedule", legs[:4], nil}, {"12", "schedule", legs[4:], nil}}, 1},
		{"release shape, second run passed", "true", []run{{"11", "schedule", legs[1:], nil}, {"12", "workflow_dispatch", legs, nil}}, 0},
		{"release shape, a pull request's run", "true", []run{{"11", "pull_request", legs, nil}}, 1},
		{"docs only, a dispatched fixture update", "false", []run{{"11", "workflow_dispatch+update", long, nil}}, 1},
		{"release shape, a dispatched fixture update", "true", []run{{"11", "workflow_dispatch+update", legs, nil}}, 1},
		{"release shape, a fixture update, then a full dispatch", "true", []run{{"11", "workflow_dispatch+update", legs, nil}, {"12", "workflow_dispatch", legs, nil}}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			bin := filepath.Join(dir, "bin")
			if err := os.Mkdir(bin, 0o750); err != nil {
				t.Fatal(err)
			}
			var runsJSON []string
			for _, r := range tc.runs {
				event, update := strings.CutSuffix(r.event, "+update")
				title := dispatchTitle(t, wf, event, update)
				runsJSON = append(runsJSON, `{"id":`+r.id+`,"event":"`+event+`","display_title":`+strconv.Quote(title)+`}`)
				var jobs []string
				for _, n := range r.passed {
					jobs = append(jobs, `{"name":`+strconv.Quote(n)+`,"conclusion":"success"}`)
				}
				for _, n := range r.failed {
					jobs = append(jobs, `{"name":`+strconv.Quote(n)+`,"conclusion":"failure"}`)
				}
				jobs = append(jobs, `{"name":"code touched","conclusion":"success"}`)
				if err := os.WriteFile(filepath.Join(dir, r.id+".json"), []byte(`{"jobs":[`+strings.Join(jobs, ",")+`]}`), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(dir, "runs.json"), []byte(`{"workflow_runs":[`+strings.Join(runsJSON, ",")+`]}`), 0o600); err != nil {
				t.Fatal(err)
			}
			// gh api <path> --jq <filter>: the runs query or a run's jobs.
			stub := "#!/usr/bin/env bash\nset -euo pipefail\n" +
				"[ \"$1\" = api ] && [ \"$3\" = --jq ] || { echo \"unexpected gh call: $*\" >&2; exit 2; }\n" +
				"case \"$2\" in\n" +
				"  repos/o/r/actions/workflows/chiptool.yml/runs\\?head_sha=abc\\&*) f=\"$FIXTURES/runs.json\" ;;\n" +
				"  repos/o/r/actions/runs/*/jobs\\?*) id=${2#repos/o/r/actions/runs/}; f=\"$FIXTURES/${id%%/*}.json\" ;;\n" +
				"  *) echo \"unexpected gh path: $2\" >&2; exit 2 ;;\n" +
				"esac\njq -r \"$4\" \"$f\"\n"
			if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(stub), 0o700); err != nil { //nolint:gosec // a test stand-in
				t.Fatal(err)
			}
			env := []string{
				"PATH=" + bin + ":" + os.Getenv("PATH"), "FIXTURES=" + dir, "REPO=o/r", "BASE=abc", "GH_TOKEN=x",
				"SHAPE=" + tc.shape, "FAMILIES=" + families,
			}
			if got := runStepScript(t, script, env); got != tc.want {
				t.Errorf("exit %d, want %d", got, tc.want)
			}
		})
	}
}

// dispatchTitle renders chiptool.yml's run-name for a run of event, with
// update_fixtures set or not and no families: the display title the runs
// API reports for it. Without a run-name it is "" (the default title is
// GitHub's and not modelled here).
func dispatchTitle(t *testing.T, wf, event string, update bool) string {
	t.Helper()
	m := regexp.MustCompile(`(?m)^run-name: (.*)$`).FindStringSubmatch(wf)
	if m == nil {
		return ""
	}
	f := regexp.MustCompile(`^\$\{\{ github\.event_name == 'workflow_dispatch' && format\('([^']*)', github\.event\.inputs\.update_fixtures, github\.event\.inputs\.families\) \|\| '' \}\}$`).FindStringSubmatch(m[1])
	if f == nil {
		t.Fatalf("chiptool.yml's run-name %q is not the form this test renders", m[1])
	}
	if event != "workflow_dispatch" {
		return ""
	}
	return strings.NewReplacer("{0}", strconv.FormatBool(update), "{1}", "").Replace(f[1])
}

// TestWorkflowLongOnBaseNamesEveryLeg holds the release-shape path to the
// gate: every job the gate reads on a release pull request but the nested
// modules' and long-on-base itself steps aside in release shape, and
// long-on-base names it by the name it reports under on the base commit —
// a leg added to the gate without both would be neither run nor read.
func TestWorkflowLongOnBaseNamesEveryLeg(t *testing.T) {
	wf := chiptoolWorkflow(t)
	script := runBlockAfter(t, wf, "  long-on-base:")
	needs := regexp.MustCompile(`(?m)^  chip-families-gate:\n(?:    .*\n)*?    needs: \[([^\]]+)\]$`).FindStringSubmatch(wf)
	if needs == nil {
		t.Fatal("chiptool.yml's chip-families-gate has no `needs: [...]` list")
	}
	legs := 0
	for _, id := range strings.Split(needs[1], ",") {
		id = strings.TrimSpace(id)
		if id == "changes" || id == "nested-release" || id == "long-on-base" {
			continue
		}
		legs++
		job := regexp.MustCompile(`(?m)^  ` + regexp.QuoteMeta(id) + `:\n((?:(?:    .*)?\n)*?)    if: \|\n      (.*)\n`).FindStringSubmatch(wf)
		if job == nil {
			t.Errorf("job %s has no `if: |` block", id)
			continue
		}
		if job[2] != "needs.changes.outputs.release_shape != 'true' && (" {
			t.Errorf("job %s runs in release shape (its condition opens with %q): long-on-base reads its verdict on the base commit instead", id, job[2])
		}
		name := regexp.MustCompile(`(?m)^ {4}name: (.+)$`).FindStringSubmatch(job[1])
		if name == nil {
			t.Errorf("job %s has no name", id)
			continue
		}
		switch id {
		case "gocert":
			groups := gocertGroup.FindAllStringSubmatch(wf, -1)
			if len(groups) == 0 {
				t.Error("chiptool.yml's gocert matrix has no group")
			}
			for _, g := range groups {
				if want := `"Go-TH certification cases (` + g[1] + `)"`; !strings.Contains(script, want) {
					t.Errorf("long-on-base does not read %s on the base commit", want)
				}
			}
		case "chip-families":
			if !strings.Contains(script, `required+=("certification families ($group)")`) || !strings.Contains(script, `jq -r '.include[].group' <<<"$FAMILIES"`) {
				t.Error("long-on-base does not read every family group of the release matrix on the base commit")
			}
		default:
			if !strings.Contains(script, `"`+name[1]+`"`) {
				t.Errorf("long-on-base does not read %q (job %s) on the base commit", name[1], id)
			}
		}
	}
	if legs != 6 {
		t.Errorf("the gate reads %d legs besides changes, nested-release and long-on-base; this test was written for 6 — check the new one", legs)
	}
}

// TestWorkflowFilterVerdict runs the `changes` job's filter step against a
// scratch repository. git diff reports a rename by its new path alone, so
// a Go file moved to a Markdown name counted as documentation only and
// the suites stayed off (DR5a-ci-10). The step also decides the release
// shape (issue 632): a release pull request whose only paths that are not
// documentation are go.mod files below the root, each changed only in
// require lines of thingfabric.dev/go-fabric… modules it replaces with a
// local path. Its boundaries are code: a go.sum, a replace, a require of
// another module, any other go.mod line, the root go.mod, a go.mod that
// does not replace the module it raises (a versioned replace applies to
// its version alone, P10-sup-7004), any other file — and a pull request
// that is not a release one.
func TestWorkflowFilterVerdict(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatalf("git, which the filter step runs, is not on PATH: %v", err)
	}
	script := runBlockAfter(t, chiptoolWorkflow(t), "- id: filter")
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	commit := func(msg string) string {
		t.Helper()
		git("add", "-A")
		git("-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-q", "-m", msg)
		return git("rev-parse", "HEAD")
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(name)), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// The shapes of the repository's own go.mod files: a nested module
	// (ble/bluez: a require block, a single replace line), an example
	// (examples/ble-wifi-light: three thingfabric.dev requires, a replace
	// block) and a go.mod that replaces nothing.
	nested := func(fabric, dbus, replace string) string {
		return "module thingfabric.dev/go-fabric/ble/bluez\n\ngo 1.27.4\n\nrequire (\n" +
			"\tgithub.com/godbus/dbus/v5 " + dbus + "\n\tthingfabric.dev/go-fabric " + fabric + "\n)\n\n" +
			"require golang.org/x/sys v0.49.0 // indirect\n\nreplace thingfabric.dev/go-fabric => " + replace + "\n"
	}
	example := func(v string) string {
		return "module thingfabric.dev/go-fabric/examples/ble-wifi-light\n\ngo 1.27.4\n\nrequire (\n" +
			"\tthingfabric.dev/go-fabric " + v + "\n\tthingfabric.dev/go-fabric/ble/bluez " + v + "\n" +
			"\tthingfabric.dev/go-fabric/wifi/wpasupplicant " + v + "\n)\n\n" +
			"// From the clone.\nreplace (\n\tthingfabric.dev/go-fabric => ../..\n" +
			"\tthingfabric.dev/go-fabric/ble/bluez => ../../ble/bluez\n" +
			"\tthingfabric.dev/go-fabric/wifi/wpasupplicant => ../../wifi/wpasupplicant\n)\n"
	}
	unreplaced := func(v string) string {
		return "module example.invalid/fixture\n\ngo 1.27.4\n\nrequire thingfabric.dev/go-fabric " + v + "\n"
	}
	versioned := func(v string) string {
		return "module example.invalid/versioned\n\ngo 1.27.4\n\nrequire thingfabric.dev/go-fabric " + v +
			"\n\nreplace thingfabric.dev/go-fabric v0.26.0 => ../..\n"
	}
	git("init", "-q")
	write("bridge/x.go", "package bridge\n\n// A file long enough for git to see its move as a rename.\nconst x = 1\n")
	write("docs/index.md", "# docs\n")
	write("CHANGELOG.md", "# Changelog\n")
	write("go.mod", "module thingfabric.dev/go-fabric\n\ngo 1.27.4\n\nrequire golang.org/x/sys v0.49.0\n")
	write("ble/bluez/go.mod", nested("v0.26.0", "v5.2.4", "../.."))
	write("ble/bluez/go.sum", "github.com/godbus/dbus/v5 v5.2.4 h1:x=\n")
	write("examples/ble-wifi-light/go.mod", example("v0.26.0"))
	write("fixture/go.mod", unreplaced("v0.26.0"))
	write("versioned/go.mod", versioned("v0.26.0"))
	base := commit("base")
	// branch commits one change set on top of base and returns to base.
	branch := func(msg string, files map[string]string) string {
		t.Helper()
		git("checkout", "-q", "--detach", base)
		for name, body := range files {
			if body == "" {
				if err := os.Remove(filepath.Join(dir, name)); err != nil {
					t.Fatal(err)
				}
				continue
			}
			write(name, body)
		}
		head := commit(msg)
		git("checkout", "-q", "--detach", base)
		return head
	}
	release := map[string]string{
		"CHANGELOG.md":                   "# Changelog\n\n- v0.27.0\n",
		"ble/bluez/go.mod":               nested("v0.27.0", "v5.2.4", "../.."),
		"examples/ble-wifi-light/go.mod": example("v0.27.0"),
	}
	with := func(extra map[string]string) map[string]string {
		m := map[string]string{}
		for k, v := range release {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	docsOnly := branch("docs only", map[string]string{"docs/index.md": "# docs, edited\n"})
	shape := branch("release shape", release)
	requiresOnly := branch("require lines alone", map[string]string{"ble/bluez/go.mod": nested("v0.27.0", "v5.2.4", "../..")})
	goSum := branch("and a go.sum", with(map[string]string{"ble/bluez/go.sum": "github.com/godbus/dbus/v5 v5.2.4 h1:y=\n"}))
	replace := branch("and a replace", with(map[string]string{"ble/bluez/go.mod": nested("v0.27.0", "v5.2.4", "../../")}))
	otherRequire := branch("and another module's require", with(map[string]string{"ble/bluez/go.mod": nested("v0.27.0", "v5.2.5", "../..")}))
	otherLine := branch("and the go line", with(map[string]string{
		"ble/bluez/go.mod": strings.Replace(nested("v0.27.0", "v5.2.4", "../.."), "go 1.27.4", "go 1.27.5", 1),
	}))
	rootGoMod := branch("and the root go.mod", with(map[string]string{"go.mod": "module thingfabric.dev/go-fabric\n\ngo 1.27.4\n\nrequire golang.org/x/sys v0.50.0\n"}))
	notReplaced := branch("a go.mod that does not replace", with(map[string]string{"fixture/go.mod": unreplaced("v0.27.0")}))
	versionReplaced := branch("a go.mod that replaces another version", with(map[string]string{"versioned/go.mod": versioned("v0.27.0")}))
	goFile := branch("and a Go file", with(map[string]string{"bridge/x.go": "package bridge\n\nconst x = 2\n"}))
	binary := branch("and another file", with(map[string]string{".gitignore": "/examples/ble-wifi-light/ble-wifi-light\n"}))
	deleted := branch("a nested go.mod deleted", map[string]string{"CHANGELOG.md": "# Changelog\n\n- x\n", "fixture/go.mod": ""})
	git("checkout", "-q", "--detach", docsOnly)
	git("mv", "bridge/x.go", "docs/x.md")
	renamed := commit("a Go file renamed into docs/")

	for _, tc := range []struct {
		name          string
		base, head    string
		release, want string
	}{
		{"documentation only", base, docsOnly, "true", "code=false\nrelease_shape=false"},
		{"a Go file renamed to a Markdown name", docsOnly, renamed, "true", "code=true\nrelease_shape=false"},
		{"release shape", base, shape, "true", "code=true\nrelease_shape=true"},
		{"release shape, require lines alone", base, requiresOnly, "true", "code=true\nrelease_shape=true"},
		{"release shape, not a release pull request", base, shape, "false", "code=true\nrelease_shape=false"},
		{"a go.sum changed", base, goSum, "true", "code=true\nrelease_shape=false"},
		{"a replace changed", base, replace, "true", "code=true\nrelease_shape=false"},
		{"a require of another module", base, otherRequire, "true", "code=true\nrelease_shape=false"},
		{"another go.mod line", base, otherLine, "true", "code=true\nrelease_shape=false"},
		{"the root go.mod", base, rootGoMod, "true", "code=true\nrelease_shape=false"},
		{"a require the go.mod does not replace", base, notReplaced, "true", "code=true\nrelease_shape=false"},
		{"a require past a versioned replace", base, versionReplaced, "true", "code=true\nrelease_shape=false"},
		{"a Go file", base, goFile, "true", "code=true\nrelease_shape=false"},
		{"a file that is neither", base, binary, "true", "code=true\nrelease_shape=false"},
		{"a go.mod deleted", base, deleted, "true", "code=true\nrelease_shape=false"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "output")
			s := "cd \"" + dir + "\"\n" + script
			env := []string{"EVENT=pull_request", "RELEASE=" + tc.release, "BASE=" + tc.base, "HEAD=" + tc.head, "GITHUB_OUTPUT=" + out}
			if got := runStepScript(t, s, env); got != 0 {
				t.Fatalf("the filter step exited %d", got)
			}
			raw, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(string(raw)); got != tc.want {
				t.Errorf("GITHUB_OUTPUT %q, want %q", got, tc.want)
			}
		})
	}
}

// TestMutateWorkflowWideInput runs mutate.yml's input checks (the plan
// step up to the shards check) on -wide values: script/mutate hands each
// element to go test as an argument, so an element that opens with `-`
// is a flag and an empty one is the module root (P10-sup-7003).
func TestMutateWorkflowWideInput(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "mutate.yml"))
	if err != nil {
		t.Fatal(err)
	}
	script := runBlockAfter(t, string(data), "- name: list and shard the files")
	cut := strings.Index(script, `if [[ ! "$SHARDS"`)
	if cut < 0 {
		t.Fatal("mutate.yml's plan step has no shards check")
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src", "bridge"), 0o750); err != nil {
		t.Fatal(err)
	}
	script = "cd \"" + dir + "\"\n" + script[:cut] + "exit 0\n"
	for _, tc := range []struct {
		wide string
		want int
	}{
		{"", 0},
		{"./node/", 0},
		{"./node/,./im/,bridge", 0},
		{"-exec", 1},
		{"./node/,-v", 1},
		{"a,,b", 1},
		{"./node/,", 1},
		{",./node/", 1},
	} {
		t.Run(tc.wide, func(t *testing.T) {
			if got := runStepScript(t, script, []string{"PKG=bridge", "WIDE=" + tc.wide}); got != tc.want {
				t.Errorf("wide %q: exit %d, want %d", tc.wide, got, tc.want)
			}
		})
	}
}

// TestWorkflowNoGoCacheOverACheckedOutRef holds every workflow: a job
// that checks out a ref of its own at the workspace root (not the
// event's tree) builds and tests that ref there, so its setup-go steps
// set `cache: false` — the post step would otherwise save the caches
// that ref's run left (P10-sup-7002, mutate.yml's shard job). A ref
// checked out under a `path:` is not built at the root and is not held.
func TestWorkflowNoGoCacheOverACheckedOutRef(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", ".github", "workflows", "*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	jobHeader := regexp.MustCompile(`(?m)^  [A-Za-z0-9_-]+:$`)
	stepHeader := regexp.MustCompile(`(?m)^      - `)
	ref := regexp.MustCompile(`(?m)^ +ref: `)
	path := regexp.MustCompile(`(?m)^ +path: `)
	cache := regexp.MustCompile(`(?m)^ +cache: (\S+)$`)
	held := 0
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		wf := string(data)
		at := strings.Index(wf, "\njobs:\n")
		if at < 0 {
			continue
		}
		jobs := wf[at:]
		heads := jobHeader.FindAllStringIndex(jobs, -1)
		for i, h := range heads {
			end := len(jobs)
			if i+1 < len(heads) {
				end = heads[i+1][0]
			}
			job := jobs[h[0]:end]
			starts := stepHeader.FindAllStringIndex(job, -1)
			foreign := false
			var caches []string
			for j, s := range starts {
				stop := len(job)
				if j+1 < len(starts) {
					stop = starts[j+1][0]
				}
				step := job[s[0]:stop]
				switch {
				case strings.Contains(step, "uses: actions/checkout@"):
					if ref.MatchString(step) && !path.MatchString(step) {
						foreign = true
					}
				case strings.Contains(step, "uses: actions/setup-go@"):
					c := "(unset)"
					if m := cache.FindStringSubmatch(step); m != nil {
						c = m[1]
					}
					caches = append(caches, c)
				}
			}
			if !foreign {
				continue
			}
			held++
			for _, c := range caches {
				if c != "false" {
					t.Errorf("%s job %s checks out a ref of its own at the root and runs setup-go with cache %s; want cache: false", filepath.Base(f), strings.TrimSpace(jobs[h[0]:h[1]]), c)
				}
			}
		}
	}
	if held == 0 {
		t.Fatal("no workflow job checks out a ref of its own at the root; mutate.yml's shard job did — the parser no longer sees it")
	}
}
