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
