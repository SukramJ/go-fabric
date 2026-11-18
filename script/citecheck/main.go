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
//     at a '.', '!' or '?' before white space. A commit the sentence names
//     checks the citation at that commit instead of the pin — the one
//     nearest the citation that is a matter.js commit, since a sentence may
//     name the chip harness commit as well; a sentence naming none is
//     checked at the pin. A sentence names a commit as `at <sha>` (or `at`
//     before a backticked sha), as a sha opening a parenthesis or as
//     "matter.js <sha>" — the last two only with a letter in the sha, since
//     they frame plain numbers too (R2-P6-test-3202). A bare line part
//     (":12-14") whose nearest reference before it is a .ts file named
//     without a line, or a part already bound, is bound to that file and
//     checked as its citation; it is counted on its own (below).
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
//  5. Off the pin. The line citations checked at a matter.js commit other
//     than the pin are counted per commit in summary.md, and
//     citecheckOffPinRatchet holds their number, exactly, in both
//     directions (R2-P6-test-3201, decision 123): each is a citation that
//     still has to be re-read at the pin. Beside it summary.md counts, and
//     no ratchet holds, the .ts files named without a line (and those whose
//     sentence names another matter.js commit), the bound bare parts (with
//     their commits and hard errors, which are not hard errors of the run),
//     the bare parts after a citation or after no file, which are not read,
//     and the chip citations — a chip source in a sentence naming chip or
//     connectedhomeip — whose nearest non-matter.js commit is not the
//     Makefile's CHIP_TEST_IMAGE_COMMIT.
//
// All git access is plumbing on MATTERJS_DIR (default ../matter.js):
// rev-parse, ls-tree and one cat-file --batch. The working tree is never
// read, so the clone may stand at any commit as long as it has the pin.
//
// # The second tree: matterjs-server
//
// A citation whose sentence names matterjs-server
// (github.com/matter-js/matterjs-server, the behaviour source of the
// controller's network topology, brief M9-module §1) is checked against
// MATTERJS_SERVER_DIR (default ../matterjs-server) instead, at the
// sentence's `at <sha>` — the first that is a commit of that clone. It has
// no pin to fall back to: a sentence naming none is the hard error "no
// matterjs-server commit". The clone is opened on the first such citation;
// missing, it is exit 2 like a missing matter.js clone. Drift rows of it
// carry "tree": "matterjs-server".
//
// # The parity registers
//
// The *.md files directly under notes/parity — by_design.md, the findings
// register, pin-lifts.md — cite matter.js the same way and were read by
// nothing (review 2026-11 W6-reg-12; review II found 169 lines of
// by_design.md at the pin before last). Their citations are collected by
// the same rules, a Markdown paragraph, list item or table row standing
// for the comment paragraph, and checked hard only: the cited file exists
// at the commit the sentence is checked at — its own `at <sha>`, else the
// pin — and has the cited lines. The failures go to registers.json (file,
// line, citation, the commit, the error), which citecheck_test.go holds at
// citecheckRegisterRatchet, exactly. They do not count as hard errors of
// the run and are not in summary.md: a register's text is corrected by its
// owner, not by the commit that touches the code. A citation that resolves
// says nothing about whether the register's sentence is still true at
// that commit; that is the re-read of a pin lift.
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
// `make cite-report` regenerates drift.json, summary.md and
// registers.json. `make cite-check` regenerates them and fails when one
// moved or a hard error exists. When a change legitimately moves the drift count, regenerate,
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
	"regexp"
	"sort"
	"strings"
)

// driftEntry is one row of drift.json.
type driftEntry struct {
	File        string `json:"file"`
	Tree        string `json:"tree,omitempty"` // "matterjs-server" for the second tree; empty for matter.js
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
		server  = flag.String("server", "", "matterjs-server clone (default $MATTERJS_SERVER_DIR, else ../matterjs-server)")
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
	serverDir := *server
	if serverDir == "" {
		serverDir = os.Getenv("MATTERJS_SERVER_DIR")
	}
	if serverDir == "" {
		serverDir = filepath.Join(*root, "..", "matterjs-server")
	}
	code, err := run(*root, dir, serverDir, filepath.Join(*root, *out), *fixFile)
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

// openRepo opens the clone at dir, or fails with errEnv (exit 2).
func openRepo(dir, name, env string) (*gitRepo, error) {
	if st, err := os.Stat(dir); err != nil || !st.IsDir() { //nolint:gosec // G703: the clone the caller names
		return nil, fmt.Errorf("%w: %s clone %s missing (set %s)", errEnv, name, dir, env)
	}
	repo := &gitRepo{dir: dir}
	if _, err := repo.git("rev-parse", "--git-dir"); err != nil {
		return nil, fmt.Errorf("%w: %s is no git repository: %w", errEnv, dir, err)
	}
	return repo, nil
}

func run(root, matterDir, serverDir, outDir, fixFile string) (int, error) {
	pin, err := readPin(root)
	if err != nil {
		return 2, err
	}
	repo, err := openRepo(matterDir, "matter.js", "MATTERJS_DIR")
	if err != nil {
		return 2, err
	}
	defer repo.close()
	var server *gitRepo // opened on the first matterjs-server citation
	defer func() {
		if server != nil {
			server.close()
		}
	}()
	pinTree, err := repo.at(pin)
	if err != nil {
		return 2, fmt.Errorf("%w: the pin %s: %w", errEnv, pin, err)
	}
	checkAll := func(cites []citation) ([]result, error) {
		results := make([]result, 0, len(cites))
		for i := range cites {
			c := cites[i]
			if c.Server {
				if server == nil {
					if server, err = openRepo(serverDir, serverName, "MATTERJS_SERVER_DIR"); err != nil {
						return nil, err
					}
				}
				r := checkServer(server, c)
				if r.env != nil {
					return nil, r.env
				}
				results = append(results, r)
				continue
			}
			r := checkAt(repo, pinTree, c)
			if r.env != nil {
				return nil, r.env
			}
			results = append(results, r)
		}
		return results, nil
	}
	cites, ments, err := collect(root)
	if err != nil {
		return 2, err
	}
	results, err := checkAll(cites)
	if err != nil {
		return 2, err
	}
	sum := summarise(pin, results)
	commits := map[string]string{} // sha as written → full matter.js commit, "" for none
	matterCommit := func(sha string) string {
		full, ok := commits[sha]
		if !ok {
			full, _ = repo.commit(sha)
			commits[sha] = full
		}
		return full
	}
	sum.counted = countMentions(ments, matterCommit, pin, readHarness(root))
	if err := writeArtefacts(outDir, sum); err != nil {
		return 2, err
	}
	// The parity registers: their own artefact and ratchet, outside the
	// Go citations' counts and exit status (registers.go).
	if err := writeRegisters(root, outDir, checkAll); err != nil {
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

// checkServer checks a matterjs-server citation at the first of its own
// commits the clone has; without one it is errNoServerCommit.
func checkServer(server *gitRepo, c citation) result {
	for _, sha := range c.Shas {
		if own, err := server.at(sha); err == nil {
			c.Sha = sha
			return check(own, c)
		}
	}
	return result{cite: c, hard: errNoServerCommit}
}

// checkAt checks a matter.js citation at the first of its own commits the
// clone has, else at the pin.
func checkAt(repo *gitRepo, pinTree tree, c citation) result {
	for _, sha := range c.Shas {
		if own, err := repo.at(sha); err == nil {
			c.Sha = sha
			r := check(own, c)
			r.commit = own.sha
			return r
		}
	}
	return check(pinTree, c)
}

// harnessRE is the Makefile's harness CHIP commit, the commit the chip-tool
// suite and the CSA cases run at.
var harnessRE = regexp.MustCompile(`(?m)^CHIP_TEST_IMAGE_COMMIT\s*\??=\s*([0-9a-f]{7,40})\s*$`)

// readHarness returns the harness CHIP commit the module's Makefile names,
// or "" when it names none (the chip count is then not measured).
func readHarness(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "Makefile")) //nolint:gosec // G304: fixed path under the module root
	if err != nil {
		return ""
	}
	if m := harnessRE.FindSubmatch(data); m != nil {
		return string(m[1])
	}
	return ""
}

// counted are the references the summary prints but no ratchet holds
// (decision 123): the .ts files named without a line, the bare line parts
// and the chip citations.
type counted struct {
	fileOnly, fileOnlyOff int
	bareUnbound           int
	harness               string // "" when the Makefile names none
	chip, chipNamed       int
	chipOff               map[string]int // commit as written → chip citations
}

// countMentions counts ments. matterCommit resolves a sha to its full
// matter.js commit ("" when it is none); a file mention is off the pin
// when its nearest such commit is not pin. A chip citation's commit is
// the nearest sha of its sentence that is no matter.js commit, off the
// harness when it is not a prefix of harness.
func countMentions(ments []mention, matterCommit func(string) string, pin, harness string) counted {
	n := counted{harness: harness, chipOff: map[string]int{}}
	for _, m := range ments {
		switch m.Kind {
		case mentionFileOnly:
			if m.Server {
				continue
			}
			n.fileOnly++
			for _, sha := range m.Shas {
				if full := matterCommit(sha); full != "" {
					if full != pin {
						n.fileOnlyOff++
					}
					break
				}
			}
		case mentionBare:
			if !m.Server {
				n.bareUnbound++
			}
		case mentionChip:
			n.chip++
			for _, sha := range m.Shas {
				if matterCommit(sha) != "" {
					continue
				}
				n.chipNamed++
				if harness != "" && !strings.HasPrefix(harness, sha) {
					n.chipOff[sha]++
				}
				break
			}
		}
	}
	return n
}

type summary struct {
	pin                    string
	citations, files       int
	ownShaPin              int            // checked at their own `at <sha>`, which is the pin
	offPin                 map[string]int // full commit → line citations checked there (citecheckOffPinRatchet)
	server                 int            // checked against matterjs-server
	byRule                 map[string]int
	driftChecked, hardOnly int
	drift                  []driftEntry
	hard                   []string
	hardByKind             map[string]int
	// The bare line parts bound to a .ts file named without a line:
	// checked, counted, outside the citations, drift and hard errors.
	bare, bareHard int
	bareOff        map[string]int // full commit → bound bare parts checked there
	counted        counted
}

// offPinCount is the number of line citations checked at a matter.js
// commit other than the pin.
func (s *summary) offPinCount() int {
	n := 0
	for _, v := range s.offPin {
		n += v
	}
	return n
}

func summarise(pin string, results []result) summary {
	s := summary{pin: pin, byRule: map[string]int{}, hardByKind: map[string]int{}, offPin: map[string]int{}, bareOff: map[string]int{}}
	files := map[string]bool{}
	for i := range results {
		r := &results[i]
		if r.cite.Bare {
			if r.cite.Server {
				continue
			}
			s.bare++
			if r.hard != "" {
				s.bareHard++
			}
			if r.commit != "" && r.commit != pin {
				s.bareOff[r.commit]++
			}
			continue
		}
		s.citations++
		files[r.cite.File] = true
		switch {
		case r.cite.Server:
			s.server++
		case r.commit != "" && r.commit != pin:
			s.offPin[r.commit]++
		case r.cite.Sha != "":
			s.ownShaPin++
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
	fmt.Fprintf(&b, "Second tree: a sentence naming matterjs-server is checked against `MATTERJS_SERVER_DIR` (default `../matterjs-server`) at its own `at <sha>`; it has no pin.\n\n")
	fmt.Fprintf(&b, "| Metric | Count |\n|---|---|\n")
	row := func(k string, v int) { fmt.Fprintf(&b, "| %s | %d |\n", k, v) }
	row("Citations", s.citations)
	row("Go files citing", s.files)
	row("Resolved: exact path from a tree root (packages/, support/, examples/)", s.byRule[ruleExact])
	row("Resolved: unique suffix", s.byRule[ruleSuffix])
	row("Resolved: unique basename", s.byRule[ruleBasename])
	row("Checked at their own `at <sha>`, the pin", s.ownShaPin)
	row(offPinRow, s.offPinCount())
	row("Checked against matterjs-server (`MATTERJS_SERVER_DIR`) at their `at <sha>`", s.server)
	row("Drift-checked (the sentence names a name the file contains)", s.driftChecked)
	row("Hard-checked only", s.hardOnly)
	row("**Hard errors**", len(s.hard))
	row("**Drift**", len(s.drift))
	writeOffPin(&b, s)
	writeCounted(&b, s)
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

// offPinRow is the summary row citecheckOffPinRatchet holds.
const offPinRow = "**Checked at another matter.js commit** (their own `at <sha>`, not the pin)"

// short is a commit id cut to eight characters.
func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

// sortedKeys returns m's keys, sorted.
func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// writeOffPin writes the line citations checked off the pin, by commit
// (R2-P6-test-3201): the rows citecheckOffPinRatchet counts.
func writeOffPin(b *strings.Builder, s summary) {
	fmt.Fprintf(b, "\n## Off the pin, by commit\n\n")
	fmt.Fprintf(b, "Line citations whose sentence names a matter.js commit other than the pin are checked at that commit; their count is held by `citecheckOffPinRatchet`, exactly. The bound bare parts are counted beside them, not ratcheted.\n\n")
	fmt.Fprintf(b, "| Commit | Line citations | Bare parts bound to a file |\n|---|---|---|\n")
	all := map[string]int{}
	for k, v := range s.offPin {
		all[k] += v
	}
	for k := range s.bareOff {
		all[k] += 0
	}
	for _, k := range sortedKeys(all) {
		fmt.Fprintf(b, "| `%s` | %d | %d |\n", short(k), s.offPin[k], s.bareOff[k])
	}
}

// writeCounted writes the references counted and printed, not ratcheted
// (decision 123): .ts files named without a line, bare line parts, chip
// citations.
func writeCounted(b *strings.Builder, s summary) {
	n := s.counted
	bareOff := 0
	for _, v := range s.bareOff {
		bareOff += v
	}
	fmt.Fprintf(b, "\n## Counted, not ratcheted\n\n| Reference | Count |\n|---|---|\n")
	row := func(k string, v int) { fmt.Fprintf(b, "| %s | %d |\n", k, v) }
	row("A .ts file named without a line part", n.fileOnly)
	row("… whose sentence names another matter.js commit", n.fileOnlyOff)
	row("A bare line part bound to the .ts file named before it (checked; not a citation, drift or hard error of the run)", s.bare)
	row("… checked at another matter.js commit", bareOff)
	row("… with a hard error at the commit it was checked at", s.bareHard)
	row("A bare line part after a .ts citation or after no file (not checked)", n.bareUnbound)
	row("A chip citation (a chip source in a sentence naming chip or connectedhomeip)", n.chip)
	row("… naming a commit that is no matter.js commit", n.chipNamed)
	if n.harness == "" {
		fmt.Fprintf(b, "| … at a commit other than the harness commit | not measured: the Makefile names no CHIP_TEST_IMAGE_COMMIT |\n")
		return
	}
	off := 0
	for _, v := range n.chipOff {
		off += v
	}
	row(fmt.Sprintf("… at a commit other than the harness commit %#q (Makefile CHIP_TEST_IMAGE_COMMIT)", short(n.harness)), off)
	if off == 0 {
		return
	}
	fmt.Fprintf(b, "\n| chip commit | chip citations |\n|---|---|\n")
	for _, k := range sortedKeys(n.chipOff) {
		fmt.Fprintf(b, "| `%s` | %d |\n", k, n.chipOff[k])
	}
}
