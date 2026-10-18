// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package main implements the matter.js citation guard (concept v4 §A.2,
// `make cite-check`).
//
// The Go code cites matter.js by file and line — a bare file name, a
// partial path or a full packages/… path, a colon, a line or a range — and
// those lines move with every pin lift without anything noticing. This
// program reads every citation in the module's *.go files (tests included;
// every directory starting with "." — .git, .claude and its worktrees — and
// vendor excluded) and checks it against the matter.js tree at the pin,
// parity/schema.json → matter.sourceCommit.
//
// # What it checks
//
//  1. Collect. A citation is `<path>.ts:<line>[-<line>]`. A full-line
//     comment paragraph is read as one text (a path broken after a '/' is
//     rejoined), every other line on its own; the citation's sentence ends
//     at a '.', '!' or '?' before white space. An `at <sha>` (or an `at`
//     before a backticked sha) in the sentence checks the citation at that
//     commit instead of the pin — the one nearest the citation that is a
//     matter.js commit, since a sentence may name the chip harness commit
//     as well; a sentence naming none is checked at the pin.
//  2. Resolve. A packages/… path exactly; a partial path as a unique suffix
//     of the tree; a bare file name as a unique basename of the tree
//     (`git ls-tree -r --name-only <sha> -- examples packages support`:
//     the Go code cites the codegen and chip-testing sources under support/
//     and the bridge example as well). No candidate, or more than one, is a
//     hard error naming them.
//  3. Check. The file exists at the commit and the cited lines are within
//     its length: hard. Drift, soft: the sentence names an identifier — a
//     CamelCase word, a `#name` or a backticked name — that occurs in the
//     cited file, and none of the names it gives occurs within ±15 lines of
//     the cited range. A sentence naming nothing the file contains is
//     checked hard only. The cited file's own basename (its class) does not
//     count: it is declared once at the head of the file.
//  4. Ratchet. The drifted citations go to drift.json (sorted, stable: Go
//     file and line, citation, the resolved path, the name, the line in the
//     cited file nearest the cited one where the name stands, and how often
//     it stands there); the counts to summary.md. citecheck_test.go holds
//     len(drift.json) at citecheckDriftRatchet, exactly, in both directions.
//
// All git access is plumbing on MATTERJS_DIR (default ../matter.js):
// rev-parse, ls-tree and one cat-file --batch. The working tree is never
// read, so the clone may stand at any commit as long as it has the pin.
//
// # Exit status
//
// 0 when the artefacts were written and no citation has a hard error; 1
// when one has (the artefacts are still written, the errors go to stderr
// and summary.md) — with -report, 0 then too; 2 when MATTERJS_DIR is no
// git repository or lacks the pin. A check that did not run is not green.
//
// # Regenerating
//
// `make cite-report` regenerates drift.json and summary.md. `make
// cite-check` regenerates them and fails when either moved or a hard error
// exists. When a change legitimately moves the drift count, regenerate,
// read the diff and commit it with citecheckDriftRatchet adjusted in the
// same commit. Neither target is in `make ci` (they need the matter.js
// clone); the cite-check job in ci.yml runs it on a sparse clone at the pin.
//
// -fixes <file> additionally writes the corrections the brief-BD rules
// derive (a bare or partial path lengthened to its unique suffix; a hard
// error or a drift moved to the one line its name stands on) and the
// citations those rules cannot settle, for a scripted rewrite.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// driftEntry is one row of drift.json.
type driftEntry struct {
	File        string `json:"file"`
	Line        int    `json:"line"`
	Citation    string `json:"citation"`
	Resolved    string `json:"resolved"`
	At          string `json:"at,omitempty"`
	Identifier  string `json:"identifier"`
	Nearest     int    `json:"nearest"`
	Occurrences int    `json:"occurrences"`
}

func main() {
	var (
		root    = flag.String("root", ".", "module root")
		matter  = flag.String("matterjs", "", "matter.js clone (default $MATTERJS_DIR, else ../matter.js)")
		out     = flag.String("out", "script/citecheck", "directory for drift.json and summary.md, relative to -root")
		report  = flag.Bool("report", false, "exit 0 despite hard errors (make cite-report)")
		fixFile = flag.String("fixes", "", "also write the derived corrections to this file")
	)
	flag.Parse()
	dir := *matter
	if dir == "" {
		dir = os.Getenv("MATTERJS_DIR")
	}
	if dir == "" {
		dir = filepath.Join(*root, "..", "matter.js")
	}
	code, err := run(*root, dir, filepath.Join(*root, *out), *fixFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "citecheck:", err)
	}
	if code == 1 && *report {
		code = 0
	}
	os.Exit(code)
}

// errEnv marks a missing clone or pin: exit 2.
var errEnv = errors.New("environment")

func readPin(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "parity", "schema.json")) //nolint:gosec // G304: fixed path under the module root
	if err != nil {
		return "", err
	}
	var doc struct {
		Matter struct {
			SourceCommit string `json:"sourceCommit"`
		} `json:"matter"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return "", err
	}
	if doc.Matter.SourceCommit == "" {
		return "", errors.New("parity/schema.json carries no matter.sourceCommit")
	}
	return doc.Matter.SourceCommit, nil
}

func run(root, matterDir, outDir, fixFile string) (int, error) {
	pin, err := readPin(root)
	if err != nil {
		return 2, err
	}
	if st, err := os.Stat(matterDir); err != nil || !st.IsDir() { //nolint:gosec // G703: the clone the caller names
		return 2, fmt.Errorf("%w: matter.js clone %s missing (set MATTERJS_DIR)", errEnv, matterDir)
	}
	repo := &gitRepo{dir: matterDir}
	defer repo.close()
	if _, err := repo.git("rev-parse", "--git-dir"); err != nil {
		return 2, fmt.Errorf("%w: %s is no git repository: %w", errEnv, matterDir, err)
	}
	pinTree, err := repo.at(pin)
	if err != nil {
		return 2, fmt.Errorf("%w: the pin %s: %w", errEnv, pin, err)
	}
	cites, err := collect(root)
	if err != nil {
		return 2, err
	}
	results := make([]result, 0, len(cites))
	for i := range cites {
		c := cites[i]
		t := pinTree
		for _, sha := range c.Shas {
			if own, err := repo.at(sha); err == nil {
				c.Sha, t = sha, own
				break
			}
		}
		results = append(results, check(t, c))
	}
	sum := summarise(pin, results)
	if err := writeArtefacts(outDir, sum); err != nil {
		return 2, err
	}
	if fixFile != "" {
		if err := writeFixes(fixFile, pinTree, results); err != nil {
			return 2, err
		}
	}
	for _, h := range sum.hard {
		fmt.Fprintln(os.Stderr, h)
	}
	if len(sum.hard) > 0 {
		return 1, fmt.Errorf("%d hard errors", len(sum.hard))
	}
	return 0, nil
}

type summary struct {
	pin                      string
	citations, files, ownSha int
	byRule                   map[string]int
	driftChecked, hardOnly   int
	drift                    []driftEntry
	hard                     []string
	hardByKind               map[string]int
}

func summarise(pin string, results []result) summary {
	s := summary{pin: pin, byRule: map[string]int{}, hardByKind: map[string]int{}}
	files := map[string]bool{}
	for i := range results {
		r := &results[i]
		s.citations++
		files[r.cite.File] = true
		if r.cite.Sha != "" {
			s.ownSha++
		}
		if r.hard != "" {
			s.hardByKind[r.hard]++
			line := fmt.Sprintf("%s:%d: %s: %s", r.cite.File, r.cite.Line, r.cite.Text, r.hard)
			switch {
			case len(r.candidates) > 0:
				line += " (" + strings.Join(r.candidates, ", ") + ")"
			case r.hard == errLine:
				line += fmt.Sprintf(" (%s has %d lines)", r.resolved, r.length)
			}
			s.hard = append(s.hard, line)
			if r.resolved != "" {
				s.byRule[r.rule]++
			}
			continue
		}
		s.byRule[r.rule]++
		if r.checked {
			s.driftChecked++
		} else {
			s.hardOnly++
		}
		if r.drift != nil {
			s.drift = append(s.drift, *r.drift)
		}
	}
	s.files = len(files)
	sort.Slice(s.drift, func(i, j int) bool {
		a, b := s.drift[i], s.drift[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Citation < b.Citation
	})
	sort.Strings(s.hard)
	return s
}

func writeArtefacts(dir string, s summary) error {
	drift := s.drift
	if drift == nil {
		drift = []driftEntry{}
	}
	data, err := json.MarshalIndent(drift, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "drift.json"), append(data, '\n'), 0o644); err != nil { //nolint:gosec // G306: a committed artefact, readable like every file of the repository
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# matter.js citation check\n\n")
	fmt.Fprintf(&b, "Pin: `%s` (parity/schema.json matter.sourceCommit). Regenerate with `make cite-report`; `make cite-check` fails when this file or drift.json moves.\n\n", s.pin)
	fmt.Fprintf(&b, "| Metric | Count |\n|---|---|\n")
	row := func(k string, v int) { fmt.Fprintf(&b, "| %s | %d |\n", k, v) }
	row("Citations", s.citations)
	row("Go files citing", s.files)
	row("Resolved: exact path from a tree root (packages/, support/, examples/)", s.byRule[ruleExact])
	row("Resolved: unique suffix", s.byRule[ruleSuffix])
	row("Resolved: unique basename", s.byRule[ruleBasename])
	row("Checked at their own `at <sha>`", s.ownSha)
	row("Drift-checked (the sentence names a name the file contains)", s.driftChecked)
	row("Hard-checked only", s.hardOnly)
	row("**Hard errors**", len(s.hard))
	row("**Drift**", len(s.drift))
	if len(s.hard) > 0 {
		kinds := make([]string, 0, len(s.hardByKind))
		for k := range s.hardByKind {
			kinds = append(kinds, k)
		}
		sort.Strings(kinds)
		fmt.Fprintf(&b, "\n## Hard errors\n\n| Kind | Count |\n|---|---|\n")
		for _, k := range kinds {
			row(k, s.hardByKind[k])
		}
		b.WriteString("\n")
		for _, h := range s.hard {
			fmt.Fprintf(&b, "- `%s`\n", h)
		}
	}
	return os.WriteFile(filepath.Join(dir, "summary.md"), []byte(b.String()), 0o644) //nolint:gosec // G306: a committed artefact
}
