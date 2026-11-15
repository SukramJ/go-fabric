// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package requiredchecks

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// provenanceScript is the release workflow's tag check.
var provenanceScript = filepath.Join(repoRoot, "script", "release-provenance.sh")

// TestProvenanceRequiredCI: the checks release-provenance.sh requires of a
// ci run are exactly the contexts of required.txt that are jobs of ci.yml,
// read by the same parser as the rest of this package. A job ci.yml runs
// but the protection does not require is not among them, so a pending
// guard cannot block a release before it blocks a merge (review II,
// DR5a-ci-04).
func TestProvenanceRequiredCI(t *testing.T) {
	bash := lookBash(t)
	out, err := exec.Command(bash, provenanceScript, "--required-ci").Output() //nolint:gosec // the repository's own script
	if err != nil {
		t.Fatalf("release-provenance.sh --required-ci: %v", err)
	}
	got := strings.Split(strings.TrimRight(string(out), "\n"), "\n")

	var ciJobs []string
	for _, wf := range loadWorkflows(t) {
		if wf.file == "ci.yml" {
			for _, j := range wf.jobs {
				ciJobs = append(ciJobs, j.key())
			}
		}
	}
	if len(ciJobs) == 0 {
		t.Fatal("no jobs read from ci.yml")
	}
	var want []string
	for _, ctx := range readRequired(t) {
		if slices.Contains(ciJobs, ctx) {
			want = append(want, ctx)
		}
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("release-provenance.sh requires %q of a ci run, want required.txt ∩ ci.yml's jobs %q", got, want)
	}
	for _, j := range ciJobs {
		if !slices.Contains(want, j) && slices.Contains(got, j) {
			t.Errorf("release-provenance.sh requires %q, which is not a required check", j)
		}
	}
}

// TestProvenanceVerdict runs release-provenance.sh against a scratch
// repository (a tag on a commit that origin/main contains) with a stub gh
// that answers each API path with the text the script's --jq would
// produce. The jq expressions themselves are proven against the real API
// (fix notes, tags v0.22.1 and v0.22.2); this holds what the script makes
// of the answers: a pending non-required job does not block (DR5a-ci-04),
// and an answer that is no count fails closed (DR5a-ci-05).
func TestProvenanceVerdict(t *testing.T) {
	bash := lookBash(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found")
	}
	out, err := exec.Command(bash, provenanceScript, "--required-ci").Output() //nolint:gosec // the repository's own script
	if err != nil {
		t.Fatal(err)
	}
	required := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	allPass := func(extra ...string) string {
		lines := []string{"17"}
		for _, r := range required {
			lines = append(lines, "success\t"+r)
		}
		return strings.Join(append(lines, extra...), "\n") + "\n"
	}
	without := func(drop, concl string) string {
		lines := []string{"17"}
		for _, r := range required {
			switch {
			case r != drop:
				lines = append(lines, "success\t"+r)
			case concl != "":
				lines = append(lines, concl+"\t"+r)
			}
		}
		return strings.Join(lines, "\n") + "\n"
	}
	pending := []string{"failure\ttest (linux/386)", "failure\tworkflow lint"}

	repo := scratchRepo(t)
	for _, tc := range []struct {
		name  string
		files map[string]string // runs, jobs-<id>, fail
		want  int
	}{
		{"every required check passed", map[string]string{"runs": "1\n7\n", "jobs-7": allPass()}, 0},
		{"pending non-required jobs failed", map[string]string{"runs": "1\n7\n", "jobs-7": allPass(pending...)}, 0},
		{"a required check failed", map[string]string{"runs": "1\n7\n", "jobs-7": without("test (linux, race)", "failure")}, 1},
		{"a required check still running", map[string]string{"runs": "1\n7\n", "jobs-7": without("lint", "none")}, 1},
		{"a required check missing from the run", map[string]string{"runs": "1\n7\n", "jobs-7": without("build", "")}, 1},
		{"no run", map[string]string{"runs": "0\n"}, 1},
		{"the newest run failed, an older one passed", map[string]string{"runs": "2\n9\n7\n", "jobs-9": without("vet", "cancelled"), "jobs-7": allPass()}, 0},
		{"total_count null", map[string]string{"runs": "null\n7\n", "jobs-7": allPass()}, 2},
		{"total_count missing", map[string]string{"runs": ""}, 2},
		{"listing shorter than total_count", map[string]string{"runs": "2\n7\n", "jobs-7": allPass()}, 2},
		{"run id not a number", map[string]string{"runs": "1\nnull\n"}, 2},
		{"jobs total_count null", map[string]string{"runs": "1\n7\n", "jobs-7": "null\n" + strings.Join(required, "\n")}, 2},
		{"gh fails", map[string]string{"runs": "1\n7\n", "fail": ""}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stub := t.TempDir()
			for name, body := range tc.files {
				if err := os.WriteFile(filepath.Join(stub, name), []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			bin := t.TempDir()
			gh := "#!/usr/bin/env bash\n" +
				"[ -e \"$STUB/fail\" ] && { echo 'gh: HTTP 502' >&2; exit 1; }\n" +
				"case \"$2\" in\n" +
				"  */actions/workflows/ci.yml/runs\\?*) cat \"$STUB/runs\" ;;\n" +
				"  */actions/runs/*/jobs\\?*) id=${2#*/actions/runs/}; cat \"$STUB/jobs-${id%%/*}\" ;;\n" +
				"  *) echo \"stub gh: unexpected $*\" >&2; exit 1 ;;\n" +
				"esac\n"
			if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(gh), 0o700); err != nil { //nolint:gosec // an executable stub
				t.Fatal(err)
			}
			abs, err := filepath.Abs(provenanceScript)
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(bash, abs, "v9.9.9") //nolint:gosec // the repository's own script
			cmd.Dir = repo
			cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
				"STUB="+stub, "GITHUB_REPOSITORY=o/r")
			out, err := cmd.CombinedOutput()
			got := 0
			var ee *exec.ExitError
			switch {
			case errors.As(err, &ee):
				got = ee.ExitCode()
			case err != nil:
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("exit %d, want %d:\n%s", got, tc.want, out)
			}
		})
	}
}

// scratchRepo is a git repository with one commit, tagged v9.9.9, that
// refs/remotes/origin/main points at.
func scratchRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-q", "--allow-empty", "-m", "x"},
		{"tag", "v9.9.9"},
		{"update-ref", "refs/remotes/origin/main", "HEAD"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func lookBash(t *testing.T) string {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not found")
	}
	return bash
}
