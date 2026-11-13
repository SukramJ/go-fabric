// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package docscheck

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// registerPath is a backticked path of at least two segments that ends in
// a file extension, with whatever follows a colon (a line, a range, a
// function): `mdns/responder.go`, `store/schema.sql:250-256`.
var registerPath = regexp.MustCompile("`((?:[\\w.-]+/)+[\\w.-]+\\.(?:go|md|json|yml|yaml|sh|sql|txt))(?::[^`]*)?`")

// registerPathsLeft is the number of paths, per parity register, that
// start with a top-level directory of this repository and name no file in
// it, measured on 2026-11-13 at 95e03096 (review II, W6-reg-10 and
// W6-reg-12): entries that prove a fix with a file since renamed or
// removed (`mdns/zeroconf.go`), and entries that name a host's file as if
// it were this module's (`internal/model/custom/light/matter.go`). Exact
// in both directions; the goal is no entry. A register's text is its
// owner's: an entry is corrected there — the path that holds the code
// today, or the sentence reworded so that it no longer points into the
// tree — and the number lowered in the same commit.
var registerPathsLeft = map[string]int{
	"notes/parity/by_design.md":                        22,
	"notes/parity/matter_behaviour_findings_closed.md": 10,
	"notes/parity/pin-lifts.md":                        4,
}

// TestRegisterPathsExist: a path into this repository that a parity
// register (notes/parity/*.md) writes in backticks names a file that
// exists. The registers are cited as fact (CLAUDE.md, Workflow), and an
// entry whose proof is a file that is not there cannot be checked by its
// reader. Lines are not judged here: TestCitationsResolve's rule would,
// and a register describes the tree at the time of its entry. matter.js
// and chip paths start with no directory of this repository and are the
// citation check's (script/citecheck, registers.json).
func TestRegisterPathsExist(t *testing.T) {
	tops := topDirs(t)
	files, err := filepath.Glob(filepath.Join(repoRoot, "notes", "parity", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	checked := 0
	seen := map[string]bool{}
	for _, f := range files {
		rel, _ := filepath.Rel(repoRoot, f)
		rel = filepath.ToSlash(rel)
		seen[rel] = true
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var missing []string
		for n, line := range strings.Split(string(raw), "\n") {
			for _, m := range registerPath.FindAllStringSubmatch(line, -1) {
				if first, _, _ := strings.Cut(m[1], "/"); !tops[first] {
					continue
				}
				checked++
				if st, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(m[1]))); err != nil || st.IsDir() {
					missing = append(missing, fmt.Sprintf("%s:%d: %s", rel, n+1, m[1]))
				}
			}
		}
		switch want := registerPathsLeft[rel]; {
		case len(missing) > want:
			t.Errorf("%s: %d paths name no file of the repository, registerPathsLeft holds %d — a new one: name the file that holds the code today\n\t%s",
				rel, len(missing), want, strings.Join(missing, "\n\t"))
		case len(missing) < want:
			t.Errorf("%s: %d paths name no file of the repository, registerPathsLeft holds %d: lower it to %d in the same commit", rel, len(missing), want, len(missing))
		}
	}
	for rel := range registerPathsLeft {
		if !seen[rel] {
			t.Errorf("registerPathsLeft names %s, which is not a register under notes/parity; delete the entry", rel)
		}
	}
	if checked < 300 {
		t.Fatalf("only %d paths checked; the pattern or the walk is broken", checked)
	}
}
