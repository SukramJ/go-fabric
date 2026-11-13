// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package requiredchecks holds the workflows' job list against the list of
// status checks a pull request needs before it merges, so a guard job
// cannot be added without a decision about it.
//
// A job that is not a required check can be red on a pull request that
// merges anyway. That happened twice with a job written to be a guard:
// ble/bluez on 2026-11-01 (review 2026-11, W6-test-01) and
// wifi/wpasupplicant on 2026-11-12 (review II, R2-P0-test-D01). Each time
// the job was added to a workflow and nothing asked whether the branch
// protection should name it.
//
// # The two lists
//
// required.txt is a copy of the branch protection of main, one check
// context per line, in the order the API returns them. The test cannot ask
// GitHub (it runs offline and without a token that may read the
// protection), so the file is only as true as its last refresh:
//
//	gh api repos/SukramJ/go-fabric/branches/main/protection \
//	    --jq '.required_status_checks.contexts[]' > script/requiredchecks/required.txt
//
// Last refreshed 2026-11-13: 20 contexts (wifi/wpasupplicant added that
// day, owner decision 12 of review II), strict, and no ruleset
// (`gh api repos/SukramJ/go-fabric/rulesets` answers []). Refresh it in the
// commit that follows a change of the protection; a context is never added
// to the file by hand ahead of the protection.
//
// [notRequired] and [offPullRequest] in this file are the other list: every
// job of a workflow a pull request starts that is deliberately not a
// required check, with its reason and where the reason is recorded, and
// every workflow no pull request starts.
//
// # What the test holds
//
// [TestEveryJobIsDecided] reads .github/workflows/*.yml and fails when
//
//   - a job a pull request starts is in neither list (decide: required, or
//     a row with its reason) or in both (the protection now names it:
//     delete the row);
//   - a required context names no job of a workflow a pull request starts
//     (the job was renamed or removed, and the protection waits for a check
//     that never reports);
//   - a required context names a matrix job (its check names are the
//     expanded ones);
//   - two jobs carry one required name and no row says so;
//   - a row names a job or a workflow that no longer exists;
//   - a workflow listed as started by no pull request has gained a
//     pull-request event.
//
// A job is known by its `name:`, which is what the branch protection's
// contexts were read to equal on 2026-11-13 (all 19 are `name:` values of
// jobs in ci.yml, codeql.yml and chiptool.yml); a job without one is keyed
// by its id.
//
// # Changing the lists
//
// Changing the branch protection is the owner's. A row's reason is quoted
// from where it is recorded — a workflow comment, the CHANGELOG, a review —
// and a job whose reason is recorded nowhere says exactly that
// ([unrecorded]) rather than carrying one written for the table.
package requiredchecks

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
)

// repoRoot is the module root, two levels above this package.
const repoRoot = "../.."

// class says why a job a pull request starts is not a required check.
type class int

const (
	// pending: the job is a guard and belongs in the branch protection;
	// adding it is an owner action that has not happened yet.
	pending class = iota + 1
	// advisory: not required by a recorded decision; the reason names its
	// source.
	advisory
	// sharesName: the job carries the name of a required context that
	// another job is required under.
	sharesName
	// unrecorded: not required, and no reason is recorded anywhere this
	// table could quote. Listed so the owner sees it.
	unrecorded
)

// decision is one job that is deliberately not in required.txt.
type decision struct {
	workflow string // file under .github/workflows
	job      string // the job's name:, or its id when it has none
	class    class
	reason   string
}

// notRequired lists every job of a pull-request workflow that is not a
// required check.
var notRequired = []decision{
	{
		"chiptool.yml", "code touched", advisory,
		"not a guard: it decides which of the workflow's jobs an event runs (the comment over the job in chiptool.yml); " +
			"the required gate job needs it and reads its result.",
	},
	{
		"chiptool.yml", "certification cases commissioning over BLE onto Wi-Fi (chip's mocks)", advisory,
		"a recorded decision: \"Not a required check; cheap […], so it runs where the BLE-Wi-Fi leg runs and a release " +
			"pull request gates on it\" (the header of chiptool.yml; CHANGELOG, the `make chiptool-ble-cases` entry: \"not a required check, part of the release gate\"). The required gate job reads " +
			"its result on a release pull request.",
	},
	{
		"chiptool.yml", "certification families (${{ matrix.group }})", advisory,
		"a matrix job cannot serve as a required check: \"its check names depend on the groups an event runs, and a " +
			"skipped matrix job reports under its unexpanded name\" (the comment over chip-families-gate in chiptool.yml); " +
			"the gate job reports for it under one required name.",
	},
	{
		"pages.yml", "build", sharesName,
		"no entry of its own in the branch protection: the job (mkdocs build --strict, path-filtered to the " +
			"documentation) shares the name `build` with ci.yml's job. What GitHub does with two check " +
			"runs of one required name is unverified (review II, phase 0, W6 §4.1).",
	},
	{
		"pages.yml", "deploy", advisory,
		"does not run on a pull request: its condition is `github.event_name != 'pull_request' && github.ref == " +
			"'refs/heads/main'` (pages.yml).",
	},
	{
		"dependabot-auto-merge.yml", "auto-merge", unrecorded,
		"no recorded reason. The workflow starts on pull_request_target; its header records what the job does (it " +
			"queues Dependabot's minor and patch pull requests for auto-merge), not a decision about the required list.",
	},
}

// offPullRequest lists the workflows no pull request starts, each with the
// events its `on:` names. None of their jobs can report on a pull request,
// so none is decided job by job; the test fails when a listed workflow's
// events are no longer these.
var offPullRequest = map[string]string{
	"ble-virtual.yml":  "schedule,workflow_dispatch",
	"chip-image.yml":   "workflow_dispatch",
	"lock.yml":         "schedule,workflow_dispatch",
	"matterjs-pin.yml": "schedule,workflow_dispatch",
	"nightly.yml":      "schedule,workflow_dispatch",
	"otbr-virtual.yml": "schedule,workflow_dispatch",
	"release.yml":      "push",
}

// pullRequestEvents are the events under which a job reports on a pull
// request (or on its merge-queue entry).
var pullRequestEvents = map[string]bool{
	"pull_request":        true,
	"pull_request_target": true,
	"merge_group":         true,
}

// minJobs is the floor on jobs judged: fewer means the reader or the walk
// broke and the guard is checking nothing.
const minJobs = 30

// TestEveryJobIsDecided: every job a pull request starts is a required
// check or a row of [notRequired], and both lists name only what exists.
func TestEveryJobIsDecided(t *testing.T) {
	for _, problem := range judge(loadWorkflows(t), readRequired(t), notRequired, offPullRequest, minJobs) {
		t.Error(problem)
	}
}

// readRequired returns the contexts of required.txt.
func readRequired(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile("required.txt")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if line != strings.TrimSpace(line) || line == "" {
			t.Fatalf("required.txt: %q is not a context; the file is the command's output, unedited", line)
		}
		out = append(out, line)
	}
	return out
}

// judge compares the workflows with the two lists and returns every
// disagreement as one line.
func judge(workflows []workflow, required []string, rows []decision, off map[string]string, floor int) []string {
	var problems []string
	fail := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	isRequired := map[string]bool{}
	for _, c := range required {
		if isRequired[c] {
			fail("required.txt names %q twice", c)
		}
		isRequired[c] = true
	}
	type rowKey struct{ workflow, job string }
	rowOf := map[rowKey]decision{}
	for _, r := range rows {
		k := rowKey{r.workflow, r.job}
		if _, dup := rowOf[k]; dup {
			fail("notRequired names %s job %q twice", r.workflow, r.job)
		}
		if r.class < pending || r.class > unrecorded || strings.TrimSpace(r.reason) == "" {
			fail("notRequired: %s job %q has no class or no reason", r.workflow, r.job)
		}
		if _, both := off[r.workflow]; both {
			fail("%s is listed in offPullRequest and has a row in notRequired (job %q)", r.workflow, r.job)
		}
		rowOf[k] = r
	}

	holders := map[string][]string{} // required context -> jobs required under it
	seenRow := map[rowKey]bool{}
	seenFile := map[string]bool{}
	judged := 0
	for _, wf := range workflows {
		seenFile[wf.file] = true
		onPR := false
		for _, e := range wf.events {
			onPR = onPR || pullRequestEvents[e]
		}
		if events, listed := off[wf.file]; listed {
			if got := strings.Join(wf.events, ","); got != events {
				fail("%s: offPullRequest records the events %q, the workflow's on: names %q — decide its jobs again", wf.file, events, got)
			}
			if onPR {
				fail("%s is listed in offPullRequest and a pull request starts it; its jobs need a decision each", wf.file)
			}
			judged += len(wf.jobs)
			continue
		}
		if !onPR {
			fail("%s: no pull request starts this workflow (on: %s) and offPullRequest does not list it", wf.file, strings.Join(wf.events, ","))
			continue
		}
		inFile := map[string]bool{}
		for _, j := range wf.jobs {
			judged++
			name := j.key()
			if inFile[name] {
				fail("%s:%d: two jobs are named %q", wf.file, j.line, name)
			}
			inFile[name] = true
			k := rowKey{wf.file, name}
			row, hasRow := rowOf[k]
			if hasRow {
				seenRow[k] = true
			}
			switch {
			case isRequired[name] && hasRow && row.class == sharesName:
				// explained below, once the other holder is known
			case isRequired[name] && hasRow:
				fail("%s:%d: job %q is in required.txt and in notRequired; the protection names it now, delete the row", wf.file, j.line, name)
			case isRequired[name]:
				if strings.Contains(name, "${{") {
					fail("%s:%d: the required context %q is a matrix job's template; its checks report under the expanded names", wf.file, j.line, name)
				}
				holders[name] = append(holders[name], wf.file)
			case hasRow && row.class == sharesName:
				fail("%s:%d: job %q is listed as sharing a required name, and required.txt has no such context", wf.file, j.line, name)
			case !hasRow:
				fail("%s:%d: job %q is not a required check (required.txt) and has no row in notRequired — "+
					"have the owner add it to the branch protection and refresh required.txt, or add a row with its reason", wf.file, j.line, name)
			}
		}
	}

	for _, c := range required {
		switch h := holders[c]; len(h) {
		case 0:
			fail("required.txt: no job of a workflow a pull request starts is named %q; the protection waits for a check that never reports", c)
		case 1:
		default:
			sort.Strings(h)
			fail("the required context %q is the name of %d jobs (%s) and no sharesName row says which is meant", c, len(h), strings.Join(h, ", "))
		}
	}
	for _, r := range rows {
		if !seenRow[rowKey{r.workflow, r.job}] {
			fail("notRequired: %s has no job %q any more; delete the row", r.workflow, r.job)
		}
	}
	for file := range off {
		if !seenFile[file] {
			fail("offPullRequest: .github/workflows/%s does not exist any more; delete the entry", file)
		}
	}
	if judged < floor {
		fail("only %d jobs judged; the workflow reader or the walk is broken", judged)
	}
	sort.Strings(problems)
	return problems
}

// TestJudge holds the comparison itself: each disagreement the package
// documentation names is reported, and the agreeing case reports nothing.
func TestJudge(t *testing.T) {
	ci := workflow{file: "ci.yml", events: []string{"pull_request", "push"}, jobs: []job{{id: "build", name: "build"}, {id: "extra", name: "extra"}}}
	nightly := workflow{file: "nightly.yml", events: []string{"schedule"}, jobs: []job{{id: "fuzz"}}}
	required := []string{"build"}
	rows := []decision{{"ci.yml", "extra", advisory, "a reason"}}
	off := map[string]string{"nightly.yml": "schedule"}
	if got := judge([]workflow{ci, nightly}, required, rows, off, 3); len(got) != 0 {
		t.Fatalf("the agreeing case reports %q", got)
	}
	with := func(wf workflow, jobs ...job) workflow {
		wf.jobs = append(append([]job(nil), wf.jobs...), jobs...)
		return wf
	}
	for name, tc := range map[string]struct {
		workflows []workflow
		required  []string
		rows      []decision
		off       map[string]string
		want      string
	}{
		"a new job":                        {[]workflow{with(ci, job{id: "guard", name: "new guard"}), nightly}, required, rows, off, `job "new guard" is not a required check`},
		"a job without a name":             {[]workflow{with(ci, job{id: "unnamed"}), nightly}, required, rows, off, `job "unnamed" is not a required check`},
		"required and listed":              {[]workflow{ci, nightly}, []string{"build", "extra"}, rows, off, `job "extra" is in required.txt and in notRequired`},
		"a required job is gone":           {[]workflow{ci, nightly}, []string{"build", "vet"}, rows, off, `no job of a workflow a pull request starts is named "vet"`},
		"a required job off pull requests": {[]workflow{ci, nightly}, []string{"build", "fuzz"}, rows, off, `no job of a workflow a pull request starts is named "fuzz"`},
		"a stale row":                      {[]workflow{ci, nightly}, required, append([]decision{{"ci.yml", "gone", advisory, "a reason"}}, rows...), off, `ci.yml has no job "gone" any more`},
		"a row without a reason":           {[]workflow{ci, nightly}, required, []decision{{"ci.yml", "extra", advisory, " "}}, off, `has no class or no reason`},
		"two jobs of one required name": {
			[]workflow{ci, {file: "pages.yml", events: []string{"pull_request"}, jobs: []job{{id: "build", name: "build"}}}, nightly},
			required, rows, off, `the required context "build" is the name of 2 jobs (ci.yml, pages.yml)`,
		},
		"a shared name without its context": {[]workflow{ci, nightly}, required, []decision{{"ci.yml", "extra", sharesName, "a reason"}}, off, `required.txt has no such context`},
		"a required matrix template": {
			[]workflow{with(ci, job{id: "m", name: "m (${{ matrix.x }})"}), nightly},
			[]string{"build", "m (${{ matrix.x }})"},
			rows, off, `is a matrix job's template`,
		},
		"an unlisted workflow off pull requests": {[]workflow{ci, nightly}, required, rows, map[string]string{}, `no pull request starts this workflow (on: schedule)`},
		"a listed workflow gains a pull-request event": {
			[]workflow{ci, {file: "nightly.yml", events: []string{"pull_request", "schedule"}, jobs: nightly.jobs}},
			required, rows, off, `nightly.yml is listed in offPullRequest and a pull request starts it`,
		},
		"a listed workflow is gone": {[]workflow{ci}, required, rows, off, `.github/workflows/nightly.yml does not exist any more`},
		"too few jobs":              {[]workflow{ci, nightly}, required, rows, off, ``},
	} {
		floor := 3
		if name == "too few jobs" {
			floor, tc.want = 4, "only 3 jobs judged"
		}
		got := strings.Join(judge(tc.workflows, tc.required, tc.rows, tc.off, floor), "\n")
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s: want a line with %q, got %q", name, tc.want, got)
		}
	}
}
