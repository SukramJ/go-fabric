// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cluster_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// commandFlagExempt are the files under cluster/ that still mark a running
// command with a flag, each with its owner.
var commandFlagExempt = map[string]string{
	"energy/devicemanagement.go": "unit appinfra (R2-P6-conc-1509's class, not this unit's file)",
}

// commandFlag is a command's origin kept in one flag per server: set when
// a command starts, cleared when it ends.
var commandFlag = regexp.MustCompile(`\.Store\(u?int32\(contract\.OriginCommand\)\)`)

// TestNoServerMarksACommandWithAFlag (R2-P6-conc-1509): a server whose
// host setters report OriginCommand while a command's handler runs counts
// the running commands; a flag that each command sets and clears is
// cleared by the first command to end while another still runs.
func TestNoServerMarksACommandWithAFlag(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(path)
		if !commandFlag.Match(b) {
			return nil
		}
		seen[rel] = true
		if _, ok := commandFlagExempt[rel]; !ok {
			t.Errorf("%s marks a running command with a flag: count the commands (an atomic.Int32 Add(1) / Add(-1))", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for f := range commandFlagExempt {
		if !seen[f] {
			t.Errorf("%s is exempt but no longer marks a command with a flag: delete the entry", f)
		}
	}
}
