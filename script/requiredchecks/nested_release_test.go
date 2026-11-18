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
func TestNestedReleaseCheckUnreachableProxyIsExitTwo(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go mod download")
	}
	for _, tool := range []string{"git", "go", "jq"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found: the script would exit 2 for that reason instead", tool)
		}
	}
	bash := lookBash(t)
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), bash, filepath.Join(root, "script", "nested-release-check.sh"), "ble/esphome") //nolint:gosec // the repository's own script
	cmd.Dir = root
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
}
