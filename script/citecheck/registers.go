// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// registerDir holds the parity registers: by_design.md, the findings
// register, pin-lifts.md and their siblings. Their citations are read as
// the Go files' are and checked hard — the file exists at the commit the
// sentence is checked at and has the cited lines — and each failure is a
// row of registers.json, on its own exact ratchet
// (citecheckRegisterRatchet). They are kept out of the Go citations'
// artefacts and exit status: a register is prose about the code, corrected
// by whoever owns its text, and the drift measure (a name near the cited
// lines) is not applied to it.
const registerDir = "notes/parity"

// registerEntry is one row of registers.json: a citation of a register
// that does not resolve.
type registerEntry struct {
	File     string `json:"file"`
	Tree     string `json:"tree,omitempty"` // "matterjs-server" for the second tree; empty for matter.js
	Line     int    `json:"line"`
	Citation string `json:"citation"`
	At       string `json:"at,omitempty"` // the commit it was checked at; empty for the pin
	Error    string `json:"error"`
}

// Markdown line kinds that open a segment of their own.
var (
	mdListItemRE = regexp.MustCompile(`^\s*(?:[-*+]|\d+[.)])\s`)
	mdFenceRE    = regexp.MustCompile("^\\s*(?:```|~~~)")
)

// markdownSegments cuts a Markdown file into [segment]s, the units a
// sentence cannot cross: a paragraph (consecutive lines, ended by an empty
// one), a list item with the lines that continue it, a table row, a
// heading, and each line of a fenced block. A path broken after a '/', or
// between ".ts" and its ":<line>", is rejoined without a space, as in a Go
// comment.
func markdownSegments(src string) []segment {
	var out []segment
	var cur *segment
	flush := func() {
		if cur != nil && strings.TrimSpace(cur.text) != "" {
			out = append(out, *cur)
		}
		cur = nil
	}
	single := func(text string, line int) {
		flush()
		out = append(out, segment{})
		out[len(out)-1].add(text, line, "")
	}
	fenced := false
	for i, raw := range strings.Split(src, "\n") {
		line := i + 1
		trimmed := strings.TrimSpace(raw)
		switch {
		case mdFenceRE.MatchString(raw):
			flush()
			fenced = !fenced
		case fenced:
			single(trimmed, line)
		case trimmed == "":
			flush()
		case strings.HasPrefix(trimmed, "|"), strings.HasPrefix(trimmed, "#"):
			single(trimmed, line)
		case mdListItemRE.MatchString(raw) || cur == nil:
			flush()
			cur = &segment{}
			cur.add(trimmed, line, "")
		default:
			sep := " "
			if strings.HasSuffix(cur.text, "/") || strings.HasSuffix(cur.text, ".ts") && lineStartRE.MatchString(trimmed) {
				sep = ""
			}
			cur.add(trimmed, line, sep)
		}
	}
	flush()
	return out
}

// collectRegisterFile returns the citations in one register's source.
func collectRegisterFile(file, src string) []citation {
	var out []citation
	for _, seg := range markdownSegments(src) {
		matches := citeRE.FindAllStringSubmatchIndex(seg.text, -1)
		for i := 0; i < len(matches); {
			s, e := sentenceBounds(seg.text, matches[i][0], matches[i][1])
			j := i
			for j < len(matches) && matches[j][0] < e {
				j++
			}
			out = append(out, collectSentence(file, seg, s, e, matches[i:j])...)
			i = j
		}
	}
	return out
}

// collectRegisters returns the citations of the *.md files directly under
// [registerDir], in file order. A module without the directory has none.
func collectRegisters(root string) ([]citation, error) {
	files, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(registerDir), "*.md"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	var out []citation
	for _, p := range files {
		src, err := os.ReadFile(p) //nolint:gosec // G304: a register of the walked module
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil, err
		}
		out = append(out, collectRegisterFile(filepath.ToSlash(rel), string(src))...)
	}
	return out, nil
}

// registerEntries are the hard failures among the registers' results,
// sorted by file, line and citation.
func registerEntries(results []result) []registerEntry {
	out := []registerEntry{}
	for _, r := range results {
		if r.hard == "" {
			continue
		}
		e := registerEntry{File: r.cite.File, Line: r.cite.Line, Citation: r.cite.Text, At: r.cite.Sha, Error: r.hard}
		if r.cite.Server {
			e.Tree = serverName
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Citation < b.Citation
	})
	return out
}

// writeRegisters writes registers.json.
func writeRegisters(dir string, entries []registerEntry) error {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "registers.json"), append(data, '\n'), 0o644) //nolint:gosec // G306: a committed artefact, readable like every file of the repository
}
