// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
)

// fix is one derived correction: on Go line Line of File, the citation
// tail Old (file name and line part) becomes New.
type fix struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Old    string `json:"old"`
	New    string `json:"new"`
	Reason string `json:"reason"`
}

// ownerCitation is a citation the rules cannot settle: the owner moves it.
type ownerCitation struct {
	File       string   `json:"file"`
	Line       int      `json:"line"`
	Citation   string   `json:"citation"`
	At         string   `json:"at,omitempty"`
	Problem    string   `json:"problem"`
	Identifier string   `json:"identifier,omitempty"`
	Candidates []string `json:"candidates,omitempty"`
}

// derive applies the brief-BD rules to the results checked at the pin; a
// citation with its own `at <sha>` is reported, never rewritten.
//
//   - An ambiguous path is lengthened to the shortest unique suffix of the
//     one candidate on which a name of the sentence stands within the drift
//     window of the cited lines — or, failing that, of the one candidate
//     that contains a name of the sentence at all, or of the one
//     candidate the same Go file cites elsewhere by a resolving path.
//   - A hard line error or a drift whose name stands exactly once in the
//     cited file moves to that line (a range keeps its span, clipped to
//     the file) — when the sentence ties the name to the citation by its
//     parentheses ([citation.Attached]); a name merely nearby is not
//     enough to move a citation, the sample of 2026-10-18 found half of
//     those moves wrong.
//   - Everything else goes to the owner, with the candidate lines or
//     files.
func derive(pin tree, results []result) (fixes []fix, owner []ownerCitation) {
	cited := map[string]map[string]bool{} // Go file → the matter.js files it cites resolvably
	for i := range results {
		r := &results[i]
		if r.resolved != "" {
			if cited[r.cite.File] == nil {
				cited[r.cite.File] = map[string]bool{}
			}
			cited[r.cite.File][r.resolved] = true
		}
	}
	for i := range results {
		r := &results[i]
		c := r.cite
		base := path.Base(c.Path)
		tail := base + c.Text[len(c.Path):]
		switch {
		case r.hard == "" && r.drift == nil:
			continue
		case c.Sha != "":
			owner = append(owner, ownerCitation{File: c.File, Line: c.Line, Citation: c.Text, At: c.Sha, Problem: problem(r), Identifier: r.ident, Candidates: lineCands(r)})
		case r.hard == errAmbiguous:
			if pick := pickCandidate(pin, c, r.candidates, cited[c.File]); pick != "" {
				suffix := uniqueSuffix(pin.files(), pick)
				newPath := suffix
				fixes = append(fixes, fix{File: c.File, Line: c.Line, Old: c.Text, New: newPath + c.Text[len(c.Path):], Reason: "ambiguous: lengthened to " + pick})
				continue
			}
			owner = append(owner, ownerCitation{File: c.File, Line: c.Line, Citation: c.Text, Problem: errAmbiguous, Candidates: r.candidates})
		case (r.hard == errLine || r.drift != nil) && len(r.occ) == 1 && r.attached:
			lo := r.occ[0]
			hi := min(lo+(c.Hi-c.Lo), r.length)
			lines := strconv.Itoa(lo)
			if hi > lo {
				lines = fmt.Sprintf("%d-%d", lo, hi)
			}
			reason := "drift"
			if r.hard != "" {
				reason = r.hard
			}
			fixes = append(fixes, fix{File: c.File, Line: c.Line, Old: tail, New: base + ":" + lines, Reason: fmt.Sprintf("%s: %s stands once, at %d", reason, r.ident, lo)})
		default:
			owner = append(owner, ownerCitation{File: c.File, Line: c.Line, Citation: c.Text, Problem: problem(r), Identifier: r.ident, Candidates: lineCands(r)})
		}
	}
	return fixes, owner
}

func problem(r *result) string {
	if r.hard != "" {
		return r.hard
	}
	return "drift"
}

func lineCands(r *result) []string {
	if len(r.candidates) > 0 {
		return r.candidates
	}
	out := make([]string, 0, len(r.occ))
	for _, l := range r.occ {
		out = append(out, strconv.Itoa(l))
	}
	return out
}

// pickCandidate chooses the file an ambiguous citation means, or "".
func pickCandidate(t tree, c citation, candidates []string, sameFile map[string]bool) string {
	var near, anywhere []string
	for _, f := range candidates {
		lines, err := t.content(f)
		if err != nil {
			continue
		}
		stem := strings.TrimSuffix(path.Base(f), ".ts")
		in, at := false, false
		for _, id := range c.Idents {
			if id == stem {
				continue
			}
			for _, l := range occurrences(lines, id, true) {
				at = true
				if c.Hi <= len(lines) && l >= c.Lo-driftWindow && l <= c.Hi+driftWindow {
					in = true
				}
			}
		}
		if in {
			near = append(near, f)
		}
		if at {
			anywhere = append(anywhere, f)
		}
	}
	switch {
	case len(near) == 1:
		return near[0]
	case len(near) == 0 && len(anywhere) == 1:
		return anywhere[0]
	case len(near) == 0:
		pool := anywhere
		if len(pool) == 0 {
			pool = candidates
		}
		var known []string
		for _, f := range pool {
			if sameFile[f] {
				known = append(known, f)
			}
		}
		if len(known) == 1 {
			return known[0]
		}
	}
	return ""
}

// uniqueSuffix is the shortest trailing run of f's path elements, two at
// least, that no other file of the tree ends with.
func uniqueSuffix(files []string, f string) string {
	parts := strings.Split(f, "/")
	for k := 2; k <= len(parts); k++ {
		suffix := strings.Join(parts[len(parts)-k:], "/")
		if suffix == f {
			return suffix
		}
		n := 0
		for _, g := range files {
			if g == suffix || strings.HasSuffix(g, "/"+suffix) {
				n++
			}
		}
		if n == 1 {
			return suffix
		}
	}
	return f
}

func writeFixes(file string, pin tree, results []result) error {
	fixes, owner := derive(pin, results)
	sort.SliceStable(fixes, func(i, j int) bool {
		if fixes[i].File != fixes[j].File {
			return fixes[i].File < fixes[j].File
		}
		return fixes[i].Line < fixes[j].Line
	})
	data, err := json.MarshalIndent(struct {
		Fixes []fix           `json:"fixes"`
		Owner []ownerCitation `json:"owner"`
	}{fixes, owner}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(file, append(data, '\n'), 0o600)
}
