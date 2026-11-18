// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// This file is the text search of the two manual tools beside the analyzer,
// crosscheck.go and whitelist_audit.go (both //go:build ignore, run as
// `go run <tool>.go mentions.go`), kept here so that a test holds it
// (R2-P6-test-2807).

// skipWalkDir reports whether a walk of the module skips a directory: every
// dot-directory (.git, and .claude, whose worktrees hold copies of the module
// that would read as callers), vendor and node_modules. The walk's root
// itself is never skipped.
func skipWalkDir(name string) bool {
	return (strings.HasPrefix(name, ".") && name != "." && name != "..") ||
		name == "vendor" || name == "node_modules"
}

// mentioningFiles lists the tracked .go files of the repository at root that
// name identifier as a word, other than declFile and the files under script/
// (the analyzer's own sources quote identifiers in their docs). It searches
// with git grep, so an untracked file or a worktree copy under .claude/ is never
// a caller. A git grep that fails — not a repository, a broken index — is an
// error, not "no mention".
func mentioningFiles(root, identifier, declFile string) ([]string, error) {
	cmd := exec.CommandContext(context.Background(), "git", "grep", "-l", "-w", "-F", "-e", identifier, "--", "*.go") //nolint:gosec // G204: identifier comes from the module's own source or inventory
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 && len(exit.Stderr) == 0 {
			return nil, nil // git grep's "no match"
		}
		return nil, fmt.Errorf("git grep %s: %w", identifier, err)
	}
	declFile = filepath.Clean(declFile)
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		clean := filepath.Clean(line)
		if clean == declFile || strings.HasPrefix(clean, "script"+string(filepath.Separator)) {
			continue
		}
		files = append(files, clean)
	}
	return files, nil
}
