// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package requiredchecks

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// runScript is the text of one `run:` value of a workflow, with the line
// its key is on.
type runScript struct {
	file string
	line int
	text string
}

var runKey = regexp.MustCompile(`^(\s*)(- )?run:\s*(.*)$`)

// runScripts returns every `run:` value of a workflow file: a one-line
// value, or the lines of a block scalar (`|`, `>` and their modifiers)
// indented past the key.
func runScripts(file, src string) []runScript {
	lines := strings.Split(src, "\n")
	var out []runScript
	for i := 0; i < len(lines); i++ {
		m := runKey.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		col := len(m[1]) + len(m[2])
		value := strings.TrimSpace(m[3])
		if value == "" || (value[0] != '|' && value[0] != '>') {
			out = append(out, runScript{file, i + 1, value})
			continue
		}
		var body []string
		j := i + 1
		for ; j < len(lines); j++ {
			l := lines[j]
			if strings.TrimSpace(l) != "" && len(l)-len(strings.TrimLeft(l, " ")) <= col {
				break
			}
			body = append(body, l)
		}
		out = append(out, runScript{file, i + 1, strings.Join(body, "\n")})
		i = j - 1
	}
	return out
}

// workflowFiles returns every workflow file's name and text.
func workflowFiles(t *testing.T) map[string]string {
	t.Helper()
	dir := filepath.Join(repoRoot, ".github", "workflows")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, e := range entries {
		if ext := filepath.Ext(e.Name()); e.IsDir() || (ext != ".yml" && ext != ".yaml") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out[e.Name()] = string(raw)
	}
	return out
}

// TestNoExpressionInRunScripts: no `${{ … }}` inside a run script of any
// workflow. An expression there is pasted into the script before the
// shell reads it, so a value from a pull request (a branch name, a title,
// a file of its tree) becomes code; a value reaches a script through
// `env:` instead (review II, R2-P5-sup-15: chiptool.yml checked out a
// commit read from the pull request's own parity/schema.json this way).
// Held for every expression, trusted or not, so the rule needs no list of
// which contexts are safe.
func TestNoExpressionInRunScripts(t *testing.T) {
	n := 0
	for file, src := range workflowFiles(t) {
		for _, rs := range runScripts(file, src) {
			n++
			if strings.Contains(rs.text, "${{") {
				t.Errorf("%s:%d: a run script holds a ${{ }} expression; pass the value through env: and quote it", rs.file, rs.line)
			}
		}
	}
	if n < 50 {
		t.Fatalf("read %d run scripts; the reader is broken", n)
	}
	for _, tc := range []struct {
		src  string
		want []string
	}{
		{"      - run: make x\n", []string{"make x"}},
		{"        run: |\n          a ${{ b }}\n          c\n      - name: d\n", []string{"          a ${{ b }}\n          c"}},
		{"      - name: x\n        run: >-\n          y\n\n          z\n        env:\n", []string{"          y\n\n          z"}},
	} {
		scripts := runScripts("x.yml", tc.src)
		got := make([]string, 0, len(scripts))
		for _, rs := range scripts {
			got = append(got, rs.text)
		}
		if strings.Join(got, "\x00") != strings.Join(tc.want, "\x00") {
			t.Errorf("runScripts(%q) = %q, want %q", tc.src, got, tc.want)
		}
	}
}

// ciNotInMakeCI lists the make targets ci.yml runs that `make ci` and
// `make ci-net` deliberately leave out, each with its reason (the comment
// over the ci target in the Makefile).
var ciNotInMakeCI = map[string]string{
	"test-32bit":    "needs a Linux host: it runs linux/386 test binaries, and darwin has no 386 port",
	"workflow-lint": "needs zizmor, a Python tool `make setup` does not install",
}

// makeCall is `make <target>` in a run script.
var makeCall = regexp.MustCompile(`\bmake ([a-z0-9][a-z0-9-]*)`)

// TestMakeCIRunsWhatCIRuns: every make target ci.yml runs is a
// prerequisite of `make ci` (the offline guards) or `make ci-net` (the two
// that read a pinned clone), or is listed in [ciNotInMakeCI] with its
// reason. The Makefile and CLAUDE.md call `make ci` what the pipeline
// runs; it had left out race, reachability-check, bench-smoke,
// testparity-check, cite-check and chipdm-check (review II, R2-P5-sup-09).
func TestMakeCIRunsWhatCIRuns(t *testing.T) {
	src := workflowFiles(t)["ci.yml"]
	inCI := map[string]bool{}
	for _, rs := range runScripts("ci.yml", src) {
		for _, m := range makeCall.FindAllStringSubmatch(rs.text, -1) {
			inCI[m[1]] = true
		}
	}
	if len(inCI) < 10 {
		t.Fatalf("found %d make targets in ci.yml; the reader is broken", len(inCI))
	}
	raw, err := os.ReadFile(filepath.Join(repoRoot, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	local := map[string]bool{}
	for _, target := range []string{"ci", "ci-net"} {
		m := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(target) + `:([^#\n]*)`).FindStringSubmatch(string(raw))
		if m == nil {
			t.Fatalf("the Makefile has no %s target", target)
		}
		for _, p := range strings.Fields(m[1]) {
			local[p] = true
		}
	}
	targets := make([]string, 0, len(inCI))
	for target := range inCI {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	for _, target := range targets {
		_, excused := ciNotInMakeCI[target]
		switch {
		case local[target] && excused:
			t.Errorf("make %s is in `make ci` and in ciNotInMakeCI; delete the row", target)
		case !local[target] && !excused:
			t.Errorf("ci.yml runs `make %s`, which neither `make ci` nor `make ci-net` runs; add it, or a row to ciNotInMakeCI with the reason", target)
		}
	}
	for target := range ciNotInMakeCI {
		if !inCI[target] {
			t.Errorf("ciNotInMakeCI names make %s, which ci.yml no longer runs; delete the row", target)
		}
	}
}

// TestEveryModuleIsScanned: the Makefile's GO_MODULES, which `make vuln`
// scans one by one, names every Go module of the tree. govulncheck at the
// root does not reach a nested module's own dependencies (flynn/noise and
// golang.org/x/crypto in ble/esphome, godbus in ble/bluez and
// wifi/wpasupplicant; review II, R2-P5-sup-05).
func TestEveryModuleIsScanned(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^GO_MODULES\s*\?=\s*(.*)$`).FindStringSubmatch(string(raw))
	if m == nil {
		t.Fatal("the Makefile has no GO_MODULES list")
	}
	listed := strings.Fields(m[1])
	sort.Strings(listed)
	var found []string
	err = filepath.WalkDir(repoRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != repoRoot && (strings.HasPrefix(d.Name(), ".") || d.Name() == "testdata" || d.Name() == "node_modules" || d.Name() == "bin") {
			return filepath.SkipDir
		}
		if !d.IsDir() && d.Name() == "go.mod" {
			rel, err := filepath.Rel(repoRoot, filepath.Dir(path))
			if err != nil {
				return err
			}
			found = append(found, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(found)
	if strings.Join(listed, " ") != strings.Join(found, " ") {
		t.Errorf("GO_MODULES is %q, the tree's modules are %q", listed, found)
	}
}

// lintToolPins are the tool versions the Makefile and ci.yml both pin; the
// comments over both say "the same versions".
var lintToolPins = []string{"GOFUMPT_VERSION", "GOLANGCI_LINT_VERSION", "ACTIONLINT_VERSION", "ZIZMOR_VERSION"}

// TestLintToolPinsMatch: each of [lintToolPins] has one value in the
// Makefile (`NAME ?= v`) and the same value in ci.yml's top-level env
// (`NAME: "v"`). A drift passed every test, so the local `make lint` and
// the pipeline could judge one tree differently (review II, DR5a-ci-09).
func TestLintToolPinsMatch(t *testing.T) {
	makefile, err := os.ReadFile(filepath.Join(repoRoot, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	ci := workflowFiles(t)["ci.yml"]
	if ci == "" {
		t.Fatal("no .github/workflows/ci.yml")
	}
	for _, name := range lintToolPins {
		mk := regexp.MustCompile(`(?m)^`+name+`\s*\?=\s*(\S+)\s*$`).FindAllStringSubmatch(string(makefile), -1)
		wf := regexp.MustCompile(`(?m)^  `+name+`: "([^"]+)"\s*$`).FindAllStringSubmatch(ci, -1)
		if len(mk) != 1 || len(wf) != 1 {
			t.Errorf("%s: %d pins in the Makefile, %d in ci.yml's env; want one each", name, len(mk), len(wf))
			continue
		}
		if mk[0][1] != wf[0][1] {
			t.Errorf("%s: the Makefile pins %s, ci.yml %s", name, mk[0][1], wf[0][1])
		}
	}
}

// quietGrepAfterPipe is a pipe (not `||`) into a grep whose flags make it
// exit at its first match: -q, --quiet or --silent.
var quietGrepAfterPipe = regexp.MustCompile(`(^|[^|])\|\s*grep\s(\s*-[^\s|;&]*)*?\s*(-[A-Za-z]*q[A-Za-z]*|--quiet|--silent)(\s|$)`)

// headAfterPipe is a pipe (not `||`) into head, which exits once it has
// printed its lines, whatever its flags.
var headAfterPipe = regexp.MustCompile(`(^|[^|])\|\s*head(\s|$|\))`)

// quietGrepPipes returns the lines of src, from line first on, where a
// pipe feeds a quiet grep; comment lines are skipped.
func quietGrepPipes(src string, first int) []int { return pipeSites(quietGrepAfterPipe, src, first) }

// headPipes returns the lines of src, from line first on, where a pipe
// feeds head; comment lines are skipped.
func headPipes(src string, first int) []int { return pipeSites(headAfterPipe, src, first) }

// pipeSites returns the lines of src, from line first on, that re
// matches; comment lines are skipped.
func pipeSites(re *regexp.Regexp, src string, first int) []int {
	var out []int
	for i, l := range strings.Split(src, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "#") {
			continue
		}
		if re.MatchString(l) {
			out = append(out, first+i)
		}
	}
	return out
}

// pipefailSites returns every site, as path:line, that sites finds in a
// script that sets pipefail: a tracked *.sh of the repository or a
// workflow's run script.
func pipefailSites(t *testing.T, sites func(src string, first int) []int) []string {
	t.Helper()
	var bad []string
	err := filepath.WalkDir(repoRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "worktrees":
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".sh" {
			return nil
		}
		raw, err := os.ReadFile(path) //nolint:gosec // a file of the repository
		if err != nil {
			return err
		}
		if !strings.Contains(string(raw), "pipefail") {
			return nil
		}
		for _, n := range sites(string(raw), 1) {
			bad = append(bad, path+":"+strconv.Itoa(n))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for file, src := range workflowFiles(t) {
		for _, rs := range runScripts(file, src) {
			if !strings.Contains(rs.text, "pipefail") {
				continue
			}
			for _, n := range sites(rs.text, rs.line+1) {
				bad = append(bad, ".github/workflows/"+file+":"+strconv.Itoa(n))
			}
		}
	}
	sort.Strings(bad)
	return bad
}

// TestNoQuietGrepAfterPipeUnderPipefail: no script that sets pipefail —
// a tracked *.sh of the repository or a workflow's run script — pipes
// into `grep -q`. grep -q exits at its first match; bash's printf and echo
// write a multi-line value one line per write(2) (strace, bash 5.2), so a
// line written after grep has gone is SIGPIPE, the writer's status is 141
// and pipefail turns the match into a miss. release-provenance.sh dropped
// "bench (smoke)" from its required checks this way in ci run 44447266854
// (TestProvenanceRequiredCI, review II phase 7, fl-ci); a here-string
// (`grep -q … <<<"$v"`) has no writer to lose.
func TestNoQuietGrepAfterPipeUnderPipefail(t *testing.T) {
	for _, b := range pipefailSites(t, quietGrepPipes) {
		t.Errorf("%s: a pipe into a quiet grep under pipefail; the writer's SIGPIPE turns a match into a miss — read a here-string instead", b)
	}
}

// TestNoHeadAfterPipeUnderPipefail: the same race with head. head exits
// once it has printed its lines; a writer with more to write gets
// SIGPIPE, its status is 141, and pipefail fails the pipeline although
// the line was read — a step under `set -e` ends, and a `|| fallback`
// appends its fallback to the value. `sed -n 1p` reads its whole input
// and has no writer to lose (review II phase 7 backlog).
func TestNoHeadAfterPipeUnderPipefail(t *testing.T) {
	for _, b := range pipefailSites(t, headPipes) {
		t.Errorf("%s: a pipe into head under pipefail; the writer's SIGPIPE fails the pipeline — pipe into `sed -n 1p` instead", b)
	}
}

func TestHeadAfterPipe(t *testing.T) {
	for _, tc := range []struct {
		line string
		want bool
	}{
		{`PIN=$(sed -n 's/x/y/p' f | head -1)`, true},
		{`x | head -n1`, true},
		{`x |head`, true},
		{`$(x | head)`, true},
		{`x | sed -n 1p`, false},
		{`a || head -1 f`, false},
		{`head -1 f`, false},
		{`x | header`, false},
	} {
		if got := len(headPipes(tc.line, 1)) > 0; got != tc.want {
			t.Errorf("%q: got %v, want %v", tc.line, got, tc.want)
		}
	}
}

func TestQuietGrepAfterPipe(t *testing.T) {
	for _, tc := range []struct {
		line string
		want bool
	}{
		{`if printf '%s\n' "$names" | grep -Fxq -- "$ctx"; then`, true},
		{`echo "$changed" | grep -Evq "$docs_only"`, true},
		{`printf '%s\n' "$changed" | grep -qE '^.+/go\.mod$'`, true},
		{`x | grep --quiet y`, true},
		{`x | grep -E -q y`, true},
		{`if grep -Fxq -- "$ctx" <<<"$names"; then`, false},
		{`a || grep -q b c`, false},
		{`x | grep -v y | wc -l`, false},
		{`x | grep -c y`, false},
	} {
		if got := len(quietGrepPipes(tc.line, 1)) > 0; got != tc.want {
			t.Errorf("%q: got %v, want %v", tc.line, got, tc.want)
		}
	}
}
