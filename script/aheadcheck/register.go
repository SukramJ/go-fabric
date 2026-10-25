// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// registerHeading opens the register section in by_design.md.
const registerHeading = "## Ahead of matter.js — register"

// notRecorded is the cell text of a row without a marker.
const notRecorded = "not recorded"

// marker is one file and the text that holds in it while the gap exists.
type marker struct {
	file, text string
}

// row is one register row.
type row struct {
	entry   string
	line    int // 1-based line in the register file
	markers []marker
	// noMarker is set when both marker cells read "not recorded".
	noMarker bool
	// problem is a malformed marker cell, reported as a finding.
	problem string
}

var (
	codeSpan   = regexp.MustCompile("`([^`]+)`")
	quotedSpan = regexp.MustCompile("`(\"(?:[^\"\\\\]|\\\\.)*\")`")
)

// parseRegister reads the register table out of by_design.md.
func parseRegister(doc string) ([]row, error) {
	lines := strings.Split(doc, "\n")
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == registerHeading {
			start = i
			break
		}
	}
	if start < 0 {
		return nil, fmt.Errorf("no heading %q", registerHeading)
	}
	var rows []row
	colEntry, colFile, colText := -1, -1, -1
	inTable := false
	for i := start + 1; i < len(lines); i++ {
		l := strings.TrimSpace(lines[i])
		if strings.HasPrefix(l, "## ") || (inTable && !strings.HasPrefix(l, "|")) {
			break
		}
		if !strings.HasPrefix(l, "|") {
			continue
		}
		cells := splitRow(l)
		if !inTable {
			for c, name := range cells {
				switch strings.ToLower(name) {
				case "entry":
					colEntry = c
				case "marker file":
					colFile = c
				case "marker text":
					colText = c
				}
			}
			if colEntry < 0 || colFile < 0 || colText < 0 {
				return nil, fmt.Errorf("line %d: the register table names no %q, %q and %q columns", i+1, "Entry", "marker file", "marker text")
			}
			inTable = true
			continue
		}
		if isSeparator(cells) {
			continue
		}
		if len(cells) <= max(colEntry, colFile, colText) {
			return nil, fmt.Errorf("line %d: %d cells, want at least %d", i+1, len(cells), max(colEntry, colFile, colText)+1)
		}
		rows = append(rows, parseRow(cells[colEntry], cells[colFile], cells[colText], i+1))
	}
	if !inTable {
		return nil, errors.New("no register table after " + registerHeading)
	}
	if len(rows) == 0 {
		return nil, errors.New("the register table has no rows")
	}
	return rows, nil
}

func parseRow(entry, fileCell, textCell string, line int) row {
	r := row{entry: strings.Trim(entry, "` "), line: line}
	if strings.EqualFold(fileCell, notRecorded) && strings.EqualFold(textCell, notRecorded) {
		r.noMarker = true
		return r
	}
	var files []string
	for _, m := range codeSpan.FindAllStringSubmatch(fileCell, -1) {
		files = append(files, m[1])
	}
	var texts []string
	for _, m := range quotedSpan.FindAllStringSubmatch(textCell, -1) {
		t, err := strconv.Unquote(m[1])
		if err != nil {
			r.problem = fmt.Sprintf("marker text %s is no Go-quoted string: %v", m[1], err)
			return r
		}
		texts = append(texts, t)
	}
	switch {
	case len(files) == 0 || len(texts) == 0:
		r.problem = "marker file or marker text is neither \"not recorded\" nor a backticked path and a backticked Go-quoted string"
	case len(files) != len(texts):
		r.problem = fmt.Sprintf("%d marker files but %d marker texts", len(files), len(texts))
	}
	if r.problem != "" {
		return r
	}
	for i := range files {
		if strings.TrimSpace(texts[i]) == "" {
			r.problem = "an empty marker text holds in every file"
			return r
		}
		r.markers = append(r.markers, marker{file: files[i], text: texts[i]})
	}
	return r
}

// splitRow splits a Markdown table row into trimmed cells. A '|' inside a
// code span is not escaped by Markdown either, so the column contract
// forbids it in a marker text.
func splitRow(l string) []string {
	l = strings.TrimSpace(l)
	l = strings.TrimPrefix(l, "|")
	l = strings.TrimSuffix(l, "|")
	parts := strings.Split(l, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func isSeparator(cells []string) bool {
	for _, c := range cells {
		if strings.Trim(c, "-: ") != "" {
			return false
		}
	}
	return true
}

// revision is a commit of the matter.js clone with the name it is
// reported under.
type revision struct{ name, sha string }

// showFunc returns a file's content at a commit, wrapping errNoFile when
// the path does not exist there.
type showFunc func(sha, path string) (string, error)

// check returns one line per finding, in register order, then the
// allowance's own findings sorted.
func check(rows []row, allow map[string]bool, show showFunc, pin, ref revision) []string {
	var out []string
	seen := map[string]bool{}
	for _, r := range rows {
		seen[r.entry] = true
		at := fmt.Sprintf("aheadcheck: %s (by_design.md:%d)", r.entry, r.line)
		switch {
		case r.problem != "":
			out = append(out, fmt.Sprintf("%s: %s", at, r.problem))
			continue
		case r.noMarker && !allow[r.entry]:
			out = append(out, at+": no marker, and the row is not in todo-marker.txt — read it from the tree at the pin and record it")
			continue
		case r.noMarker:
			continue
		case allow[r.entry]:
			out = append(out, at+": has a marker but is still in todo-marker.txt — remove its line")
		}
		for _, m := range r.markers {
			if f := holds(show, pin, m); f != "" {
				out = append(out, fmt.Sprintf("%s: the row is wrong: %s", at, f))
				continue
			}
			if ref.sha == pin.sha {
				continue
			}
			if f := holds(show, ref, m); f != "" {
				out = append(out, fmt.Sprintf("%s: caught up? %s", at, f))
			}
		}
	}
	var stale []string
	for name := range allow {
		if !seen[name] {
			stale = append(stale, "aheadcheck: todo-marker.txt names "+name+", which is no row of the register — remove its line")
		}
	}
	sort.Strings(stale)
	return append(out, stale...)
}

// holds reports why m does not hold at rev, or "" when it does.
func holds(show showFunc, rev revision, m marker) string {
	content, err := show(rev.sha, m.file)
	if errors.Is(err, errNoFile) {
		return fmt.Sprintf("%s does not exist at %s (%s)", m.file, rev.name, short(rev.sha))
	}
	if err != nil {
		return fmt.Sprintf("%s at %s (%s): %v", m.file, rev.name, short(rev.sha), err)
	}
	if !strings.Contains(content, m.text) {
		return fmt.Sprintf("the marker text %s is not in %s at %s (%s)", strconv.Quote(firstLine(m.text)), m.file, rev.name, short(rev.sha))
	}
	return ""
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " …"
	}
	return s
}
