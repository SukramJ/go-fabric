// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package docscheck

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// An ADR is the record of a decision and is not rewritten; a later
// reversal is a dated note in its status line naming what superseded it
// (ADRs 0001, 0008, 0016, 0026). The review of phase 6b found decisions
// reversed without that note and an index that showed them as current,
// left out three ADRs and gave one the wrong status (review II,
// R2-P6-doc-3011, -3076 … -3086). The two tests below hold the index and
// the back-references.

// adrFile is an ADR's file name: four digits, a dash, a slug.
var adrFile = regexp.MustCompile(`^(\d{4})-.+\.md$`)

// adrStatusWord is the first word of an ADR's "- **Status**:" line.
var adrStatusWord = regexp.MustCompile(`(?m)^- \*\*Status(?:\*\*:|:\*\*)\s*([A-Za-z]+)`)

// adrIndexRow is one row of docs/adr/README.md's table: the linked
// number, the decision, the status.
var adrIndexRow = regexp.MustCompile(`(?m)^\| \[(\d{4})\]\(\./([^)]+)\) \|.*\| ([^|]+) \|$`)

// adrs returns the ADR files by number.
func adrs(t *testing.T) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(repoRoot, "docs", "adr"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, e := range entries {
		if m := adrFile.FindStringSubmatch(e.Name()); m != nil {
			out[m[1]] = e.Name()
		}
	}
	if len(out) < 30 {
		t.Fatalf("only %d ADRs found; the walk is broken", len(out))
	}
	return out
}

// adrHead is an ADR's text before its first "## " heading: the title, the
// provenance and the status block.
func adrHead(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, "docs", "adr", name))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if i := strings.Index(text, "\n## "); i >= 0 {
		return text[:i]
	}
	return text
}

// TestADRIndexListsEveryADR: docs/adr/README.md has exactly one row per
// ADR file, linking that file, and the row's status starts with the word
// the ADR's own status line starts with (accepted, rejected, proposed).
func TestADRIndexListsEveryADR(t *testing.T) {
	files := adrs(t)
	raw, err := os.ReadFile(filepath.Join(repoRoot, "docs", "adr", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]int{}
	for _, m := range adrIndexRow.FindAllStringSubmatch(string(raw), -1) {
		num, link, status := m[1], m[2], strings.TrimSpace(m[3])
		rows[num]++
		name, ok := files[num]
		if !ok {
			t.Errorf("README.md: row %s names no ADR file", num)
			continue
		}
		if link != name {
			t.Errorf("README.md: row %s links %s, the file is %s", num, link, name)
		}
		w := adrStatusWord.FindStringSubmatch(adrHead(t, name))
		if w == nil {
			t.Errorf("%s: no status line", name)
			continue
		}
		if !strings.HasPrefix(strings.ToLower(status), strings.ToLower(w[1])) {
			t.Errorf("README.md: row %s's status %q does not start with the ADR's own %q", num, status, w[1])
		}
	}
	nums := make([]string, 0, len(files))
	for num := range files {
		nums = append(nums, num)
	}
	sort.Strings(nums)
	for _, num := range nums {
		if rows[num] != 1 {
			t.Errorf("README.md: ADR %s has %d index rows, want 1", num, rows[num])
		}
	}
}

// supersedesBlock is an ADR's "- **Supersedes…**" bullet with its
// continuation lines: "Supersedes" and "Supersedes in part".
var supersedesBlock = regexp.MustCompile(`(?m)^- \*\*Supersedes[^*]*\*\*:?(.*(?:\n  .*)*)`)

// adrLink is a relative link to another ADR file.
var adrLink = regexp.MustCompile(`\]\(\./(\d{4})-[^)]+\.md\)`)

// TestSupersededADRNamesItsSuccessor: an ADR that another ADR's
// Supersedes line links carries, in its own head (title, provenance,
// status block), the number of the ADR that superseded it — so a reader
// who opens the older decision learns in its first lines that it no longer
// stands as written.
func TestSupersededADRNamesItsSuccessor(t *testing.T) {
	files := adrs(t)
	links := 0
	for num, name := range files {
		head := adrHead(t, name)
		for _, block := range supersedesBlock.FindAllStringSubmatch(head, -1) {
			for _, l := range adrLink.FindAllStringSubmatch(block[1], -1) {
				links++
				older, ok := files[l[1]]
				if !ok {
					t.Errorf("%s: Supersedes links ADR %s, which does not exist", name, l[1])
					continue
				}
				if !strings.Contains(adrHead(t, older), num) {
					t.Errorf("%s supersedes %s, whose status block does not name ADR %s", name, older, num)
				}
			}
		}
	}
	if links < 4 {
		t.Fatalf("only %d Supersedes links found; the pattern is broken", links)
	}
}
