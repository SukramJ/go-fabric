// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package requiredchecks

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestNestedReleaseCheckUnreachableProxyIsExitTwo: a nested module whose
// requirements cannot be downloaded — here GOPROXY=off over an empty module
// cache, the state of a proxy outage — ends the release check with exit 2,
// "the environment", not 1 or a pass (N5-rwci-2; the mapping of
// script/nested-release-check.sh's go mod download step).
//
// The script runs in a scratch repository that holds the tag its fixture
// module requires: the check reads tags from the local clone before it
// downloads, and a CI checkout carries none, so a run against this
// repository's own modules would end at the tag check with exit 1 and never
// reach the step under test.
func TestNestedReleaseCheckUnreachableProxyIsExitTwo(t *testing.T) {
	bash := lookBash(t)
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "script", "nested-release-check.sh")

	repo := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(repo, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module github.com/SukramJ/go-fabric\n\ngo 1.21\n")
	write("nested/go.mod", "module github.com/SukramJ/go-fabric/nested\n\ngo 1.21\n\nrequire github.com/SukramJ/go-fabric v0.0.1\n")
	write("nested/nested.go", "package nested\n")
	git := func(args ...string) {
		t.Helper()
		argv := make([]string, 0, 10+len(args))
		argv = append(argv, "-c", "user.name=test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false", "-c", "core.hooksPath=/dev/null")
		argv = append(argv, args...)
		cmd := exec.CommandContext(t.Context(), "git", argv...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	git("add", ".")
	git("commit", "-q", "-m", "fixture")
	git("tag", "v0.0.1")

	cmd := exec.CommandContext(t.Context(), bash, script, "nested") //nolint:gosec // the repository's own script
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"GOPROXY=off", "GOMODCACHE="+t.TempDir(), "GOFLAGS=", "GOWORK=off", "GOTOOLCHAIN=local", "GONOSUMDB=", "GOSUMDB=off")
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 {
		t.Fatalf("nested-release-check with no proxy: %v, want exit 2\n%s", err, out)
	}
	if !strings.Contains(string(out), "cannot be downloaded") {
		t.Errorf("exit 2 for another reason than the download:\n%s", out)
	}
	if !strings.Contains(string(out), "(tag v0.0.1): resolved without the replace") {
		t.Errorf("the fixture's require did not pass the tag check first:\n%s", out)
	}
}

// nestedReleaseRepo writes files into a scratch git repository, commits
// them and makes the tags named; it returns the repository's directory.
func nestedReleaseRepo(t *testing.T, files map[string]string, tags ...string) string {
	t.Helper()
	repo := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(repo, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) {
		t.Helper()
		argv := make([]string, 0, 10+len(args))
		argv = append(argv, "-c", "user.name=test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false", "-c", "core.hooksPath=/dev/null")
		argv = append(argv, args...)
		cmd := exec.CommandContext(t.Context(), "git", argv...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	git("add", ".")
	git("commit", "-q", "-m", "fixture")
	for _, tag := range tags {
		git("tag", tag)
	}
	return repo
}

// runNestedReleaseCheck runs script/nested-release-check.sh in repo with
// args, offline (GOPROXY=off over an empty module cache), and returns its
// exit code and output.
func runNestedReleaseCheck(t *testing.T, repo string, args ...string) (code int, output string) {
	t.Helper()
	bash := lookBash(t)
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	argv := append([]string{filepath.Join(root, "script", "nested-release-check.sh")}, args...)
	cmd := exec.CommandContext(t.Context(), bash, argv...) //nolint:gosec // the repository's own script
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"GOPROXY=off", "GOMODCACHE="+t.TempDir(), "GOFLAGS=", "GOWORK=off", "GOTOOLCHAIN=local", "GONOSUMDB=", "GOSUMDB=off")
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0, string(out)
	case errors.As(err, &exit):
		return exit.ExitCode(), string(out)
	default:
		t.Fatalf("nested-release-check: %v\n%s", err, out)
		return -1, ""
	}
}

// siblingReleaseFixture is the shape of the nested release after v0.23.0
// (owner decision 266, PR #546): a nested library "lib" whose go.mod the
// release raises and whose tag lib/v0.0.2 is made at the merge, and an
// example that requires it at v0.0.2 through a replace to the directory
// beside it. Nothing is required from the module proxy, so the check runs
// offline.
var siblingReleaseFixture = map[string]string{
	"go.mod":     "module github.com/SukramJ/go-fabric\n\ngo 1.21\n",
	"lib/go.mod": "module github.com/SukramJ/go-fabric/lib\n\ngo 1.21\n",
	"lib/lib.go": "package lib\n\n// Answer is what the example uses.\nconst Answer = 42\n",
	"example/go.mod": "module github.com/SukramJ/go-fabric/example\n\ngo 1.21\n\n" +
		"require github.com/SukramJ/go-fabric/lib v0.0.2\n\n" +
		"replace github.com/SukramJ/go-fabric/lib => ../lib\n",
	"example/main.go": "package main\n\nimport \"github.com/SukramJ/go-fabric/lib\"\n\nfunc main() { println(lib.Answer) }\n",
}

// TestNestedReleaseCheckSiblingReleasedBySameChange: a require of a sibling
// at the version the same change releases (--released lib/v0.0.2, the tag
// made at its merge) passes when the sibling was checked before in the same
// run and is in release shape; the example builds against the sibling's
// checked copy (owner decision 266).
func TestNestedReleaseCheckSiblingReleasedBySameChange(t *testing.T) {
	repo := nestedReleaseRepo(t, siblingReleaseFixture, "lib/v0.0.1")
	code, out := runNestedReleaseCheck(t, repo, "--released", "lib/v0.0.2", "lib", "example")
	if code != 0 {
		t.Fatalf("a sibling released by the same change: exit %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "released by this change") {
		t.Errorf("the require was not accepted as released by this change:\n%s", out)
	}
}

// TestNestedReleaseCheckUnreleasedSiblingFails: a require of a sibling at a
// version that is neither tagged nor released by the change stays exit 1 —
// without --released, with --released naming another version, and with the
// sibling left out of the run or checked after the module that requires it.
func TestNestedReleaseCheckUnreleasedSiblingFails(t *testing.T) {
	repo := nestedReleaseRepo(t, siblingReleaseFixture, "lib/v0.0.1")
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"no release", []string{"lib", "example"}, "has no tag lib/v0.0.2"},
		{"another version", []string{"--released", "lib/v0.0.3", "lib", "example"}, "has no tag lib/v0.0.2"},
		{"sibling not checked", []string{"--released", "lib/v0.0.2", "example"}, "lib is not checked before example"},
		{"sibling checked after", []string{"--released", "lib/v0.0.2", "example", "lib"}, "lib is not checked before example"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out := runNestedReleaseCheck(t, repo, tc.args...)
			if code != 1 {
				t.Fatalf("exit %d, want 1\n%s", code, out)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("output lacks %q:\n%s", tc.want, out)
			}
		})
	}
}

// TestNestedReleaseCheckReleasedArgument: --released takes a tag of a
// nested module of this repository, <dir>/vX.Y.Z; anything else is the
// arguments, exit 2, and it is not combined with --tag.
func TestNestedReleaseCheckReleasedArgument(t *testing.T) {
	repo := nestedReleaseRepo(t, siblingReleaseFixture, "lib/v0.0.1")
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"missing", []string{"lib", "--released"}},
		{"no directory", []string{"--released", "v0.0.2", "lib"}},
		{"not a version", []string{"--released", "lib/next", "lib"}},
		{"no module", []string{"--released", "nothing/v0.0.2", "lib"}},
		{"with --tag", []string{"--tag", "lib/v0.0.1", "--released", "lib/v0.0.2"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if code, out := runNestedReleaseCheck(t, repo, tc.args...); code != 2 {
				t.Fatalf("exit %d, want 2\n%s", code, out)
			}
		})
	}
}
