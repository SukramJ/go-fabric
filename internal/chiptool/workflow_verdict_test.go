// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package chiptool

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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
		"CODE": "true", "LONG_BASE": "skipped",
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
// stand-in gh: green only when a nightly or dispatched run on the base
// commit has a passed `certification families (long)` job.
func TestWorkflowLongOnBaseVerdict(t *testing.T) {
	script := runBlockAfter(t, chiptoolWorkflow(t), "  long-on-base:")
	for _, tc := range []struct {
		name       string
		runs, jobs string // what the stand-in prints for the runs and the jobs query
		want       int
	}{
		{"passed", "11", "success", 0},
		{"failed", "11", "failure", 1},
		{"no long job", "11", "", 1},
		{"no run", "", "", 1},
		{"second run passed", "11\n12", "success", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin := t.TempDir()
			stub := "#!/usr/bin/env bash\ncase \"$2\" in\n  */runs\\?*) printf '" + tc.runs + "\\n' ;;\n  *) printf '%s\\n' '" + tc.jobs + "' ;;\nesac\n"
			if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(stub), 0o700); err != nil { //nolint:gosec // a test stand-in
				t.Fatal(err)
			}
			env := []string{"PATH=" + bin + ":" + os.Getenv("PATH"), "REPO=o/r", "BASE=abc", "GH_TOKEN=x"}
			if got := runStepScript(t, script, env); got != tc.want {
				t.Errorf("exit %d, want %d", got, tc.want)
			}
		})
	}
}

// TestWorkflowFilterVerdict runs the `changes` job's filter step against a
// scratch repository. git diff reports a rename by its new path alone, so
// a Go file moved to a Markdown name counted as documentation only and
// the suites stayed off (DR5a-ci-10).
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
	git("init", "-q")
	write("bridge/x.go", "package bridge\n\n// A file long enough for git to see its move as a rename.\nconst x = 1\n")
	write("docs/index.md", "# docs\n")
	base := commit("base")
	write("docs/index.md", "# docs, edited\n")
	docsOnly := commit("docs only")
	git("mv", "bridge/x.go", "docs/x.md")
	renamed := commit("a Go file renamed into docs/")

	for _, tc := range []struct {
		name       string
		base, head string
		want       string
	}{
		{"documentation only", base, docsOnly, "code=false"},
		{"a Go file renamed to a Markdown name", docsOnly, renamed, "code=true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "output")
			s := "cd \"" + dir + "\"\n" + script
			if got := runStepScript(t, s, []string{"EVENT=pull_request", "BASE=" + tc.base, "HEAD=" + tc.head, "GITHUB_OUTPUT=" + out}); got != 0 {
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
