// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package fabric_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The chip-tool workflow runs the only check in this repository that drives a
// real controller against a real daemon. It found the reference bridge
// shipping un-pairable when nine other green checks did not, and until
// recently it started only on a label — which is a thing a human has to
// remember, on a module where nearly every change can move commissioning.
//
// Its `changes` job now decides from the diff. That decision is a list of
// patterns inside a YAML shell step, where nothing type-checks it and a
// pattern that stops matching produces no error: the job simply reports "no
// Go code touched" forever and the commissioner stops running. The failure
// mode is silence, which is why it is worth a test.
//
// This reads the patterns out of the workflow rather than restating them. A
// copy here would pass while the workflow said something else, which is the
// specific way a guard like this goes quietly wrong.
const chiptoolWorkflowPath = ".github/workflows/chiptool.yml"

func chiptoolTriggerPatterns(t *testing.T) []string {
	t.Helper()

	raw, err := os.ReadFile(chiptoolWorkflowPath)
	if err != nil {
		t.Fatalf("read %s: %v", chiptoolWorkflowPath, err)
	}
	found := regexp.MustCompile(`-e '([^']+)'`).FindAllStringSubmatch(string(raw), -1)
	if len(found) == 0 {
		t.Fatalf("%s carries no `-e '<pattern>'` arguments. Either the changes job lost its "+
			"filter or it was rewritten in a shape this guard cannot read — both are reasons to "+
			"look, because a filter matching nothing silently stops running the commissioner suite",
			chiptoolWorkflowPath)
	}
	out := make([]string, 0, len(found))
	for _, m := range found {
		out = append(out, m[1])
	}
	return out
}

// TestChiptoolTriggerClassifiesChangedFiles pins both directions. The quiet
// cases matter as much as the loud ones: a filter that fires on everything
// runs a slow arm64 suite with a 2.5 GiB image pull on a changelog typo, which
// gets it disabled — the same end state as not having it.
func TestChiptoolTriggerClassifiesChangedFiles(t *testing.T) {
	t.Parallel()
	pats := chiptoolTriggerPatterns(t)

	matches := func(path string) bool {
		for _, p := range pats {
			if regexp.MustCompile(p).MatchString(path) {
				return true
			}
		}
		return false
	}

	for _, tc := range []struct {
		path string
		want bool
		why  string
	}{
		{"bridge/subscribe.go", true, "the bridge is the commissioning path"},
		{"cluster/valve/valve_server.go", true, "a cluster server a controller reads"},
		{"secure/sigma/sigma.go", true, "CASE itself"},
		{"endpoint/materialize.go", true, "what a controller enumerates"},
		{"examples/reference-bridge/fleet.go", true, "the daemon under test"},
		{"internal/chiptool/commission_test.go", true, "the suite's own harness"},
		{"go.mod", true, "a dependency bump can move wire behaviour"},
		{"go.sum", true, "same reason as go.mod"},
		{".github/workflows/chiptool.yml", true, "the workflow itself, so a change to it is tested by it"},

		{"README.md", false, "documentation cannot move commissioning"},
		{"CHANGELOG.md", false, "same"},
		{"notes/audits/matter-threat-model.md", false, "the notes tree is not built"},
		{"CONTRIBUTING.md", false, "same"},
	} {
		if got := matches(tc.path); got != tc.want {
			verb := "did not match"
			if got {
				verb = "matched"
			}
			t.Errorf("%s %s the chiptool trigger, want the opposite (%s)", tc.path, verb, tc.why)
		}
	}
}

// TestChiptoolTriggerCoversEveryBuiltDirectory is the rot half, and it is
// stronger than checking the patterns against a fixed list: it walks the
// module's own top-level directories and asserts that a Go file in each one
// would start the suite. A directory added later is covered without anyone
// remembering to extend a list here.
func TestChiptoolTriggerCoversEveryBuiltDirectory(t *testing.T) {
	t.Parallel()
	pats := chiptoolTriggerPatterns(t)

	matches := func(path string) bool {
		for _, p := range pats {
			if regexp.MustCompile(p).MatchString(path) {
				return true
			}
		}
		return false
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read module root: %v", err)
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		switch e.Name() {
		case "notes", "parity", "testdata":
			continue // carried, not built
		}
		probe := e.Name() + "/probe.go"
		if !matches(probe) {
			t.Errorf("a Go file at %q would NOT start the commissioner suite. Every directory the "+
				"binary is built from has to, because this module is the Matter stack and almost "+
				"any change to it can move commissioning", probe)
		}
	}
}

// TestChiptoolTriggerFiresOnAFreshPullRequest pins the event list, which the
// pattern tests above cannot see.
//
// A pull request fires `opened` and nothing else when it is created. Without
// that type the workflow does not start at all on the one event every pull
// request begins with — so the changes job never runs, the suite never starts,
// and no pattern inside it matters. That is not hypothetical: the change that
// introduced the automatic trigger shipped without `opened` and failed to
// trigger itself, which is how it was found.
//
// `synchronize` alone would cover it eventually — the next push to the branch
// fires one — but "eventually" means the first review happens with no
// commissioner run behind it, and a pull request merged as opened gets no run
// at all.
//
// `reopened` is here for the same reason and is the one this guard used to
// miss while the workflow already declared it: reopening a closed pull request
// fires `reopened` alone, so without it a branch that was closed and reopened
// — routine when a rebase goes sideways — runs unguarded until someone pushes
// again. An event the workflow declares but no test names can be dropped in a
// tidy-up with nothing going red, which is how the whole list came to be
// missing `opened` in the first place.
//
// Read textually rather than through a YAML parser: this module has no YAML
// dependency and adding one to read four words would cost more than it
// explains.
func TestChiptoolTriggerFiresOnAFreshPullRequest(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(chiptoolWorkflowPath)
	if err != nil {
		t.Fatalf("read %s: %v", chiptoolWorkflowPath, err)
	}
	m := regexp.MustCompile(`(?m)^\s*types:\s*\[([^\]]*)\]`).FindSubmatch(raw)
	if m == nil {
		t.Fatalf("%s carries no `types: [...]` list under pull_request — either it stopped "+
			"triggering on pull requests, or it was rewritten in a shape this guard cannot read",
			chiptoolWorkflowPath)
	}
	have := strings.Split(strings.ReplaceAll(string(m[1]), " ", ""), ",")

	for _, want := range []string{"opened", "labeled", "synchronize", "reopened"} {
		found := false
		for _, h := range have {
			if h == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("pull_request types %v is missing %q — without it the workflow does not start "+
				"on that event, and every pattern inside it is moot", have, want)
		}
	}
}
