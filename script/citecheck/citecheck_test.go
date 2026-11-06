// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// The ratchet tests in this file check the COMMITTED drift.json and
// summary.md: that they parse, agree with each other, and that the number of
// drifted citations sits exactly on its ratchet. Like the reachability
// inventory's tests they do not re-run the check — that needs the matter.js
// clone; `make cite-check` and the cite-check CI job regenerate the
// artefacts and fail when they moved. The other tests run the collector and
// the checker against testdata/sample.go.txt and an in-memory tree.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// citecheckDriftRatchet is the number of citations the committed drift.json
// is allowed to list as drifted.
//
// It is an exact ratchet, checked in both directions on purpose, as
// reachabilityUnreachedRatchet is: growth has to be justified in the commit
// that raises it, and a drop has to be locked in by lowering it.
//
// Set to 266 by the first measurement, at matter.js 862ad75f (brief BD,
// 2026-10-18), after the wave's scripted corrections: 3 716 citations in
// 643 Go files, 1 762 of them drift-checked (their sentence names a name the
// cited file contains), 266 of those with no such name within ±15 lines of
// the cited range. Before the corrections the same rules measured 284. The
// number was measured, not tuned: no citation was edited to leave the list
// other than by the brief's rules, and five derived moves the review found
// wrong were left in place (and on the list) rather than applied. The goal
// is 0 by the end of milestone 2; each owner correction lowers it.
//
// 265 after brief BH-1: the report assembler's InteractionMessenger.ts
// citation moved to the lines that name what it mirrors.
//
// 262 after brief BV-1: the NativeSwitcher doc moved into the generator
// (levelcontroldef) and its LevelControlServer.ts:529-550 citation now names
// transition there and #turnOnForLevel at :597.
//
// 251 after review 2026-11 W6-reg-13: the drifted citations of
// bridge/securechannel.go (ExchangeManager.ts #sendCloseSession),
// cluster/core/general_commissioning.go (the PASE auto-arm),
// controller/client/fabricops.go (CommissioningClient.ts) and im/client.go
// (InteractionMessenger.ts) re-read and re-cited at the pin.
//
// 254 after phase 31: four citations of the IM tranche drift (drift.json
// names them: bridge/interaction_protocol_parity_matterjs_test.go,
// bridge/subscribe_dispatch.go, bridge/suppress_response_parity_matterjs_test.go,
// im/write.go); each is to be re-read at the pin and re-cited.
//
// 253 after phase 33's TLV root: the TlvStringTest.ts citation of
// tlv/codec_parity_test.go re-cited at the pin.
//
// 418 after pin lift wave P2 (#431): the testSignerInfo citation of
// controller/attestation/store.go left with the built-in test CD signer.
const citecheckDriftRatchet = 418

func readDrift(t *testing.T) []driftEntry {
	t.Helper()
	data, err := os.ReadFile("drift.json")
	if err != nil {
		t.Fatalf("read the committed drift.json: %v (regenerate with make cite-report)", err)
	}
	var drift []driftEntry
	if err := json.Unmarshal(data, &drift); err != nil {
		t.Fatalf("drift.json does not parse: %v", err)
	}
	return drift
}

func TestCitecheckDriftOnRatchet(t *testing.T) {
	drift := readDrift(t)
	if got := len(drift); got != citecheckDriftRatchet {
		t.Errorf("drift.json lists %d drifted citations, the ratchet is %d: a rise needs its justification in the commit, a drop lowers citecheckDriftRatchet in the same commit", got, citecheckDriftRatchet)
	}
	sorted := sort.SliceIsSorted(drift, func(i, j int) bool {
		a, b := drift[i], drift[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Citation < b.Citation
	})
	if !sorted {
		t.Error("drift.json is not sorted by file, line and citation")
	}
	for _, d := range drift {
		if d.File == "" || d.Line < 1 || d.Citation == "" || d.Resolved == "" || d.Identifier == "" || d.Nearest < 1 {
			t.Errorf("incomplete drift entry %+v", d)
		}
	}
}

func TestCitecheckSummaryAgreesWithDrift(t *testing.T) {
	data, err := os.ReadFile("summary.md")
	if err != nil {
		t.Fatalf("read the committed summary.md: %v", err)
	}
	m := regexp.MustCompile(`\| \*\*Drift\*\* \| (\d+) \|`).FindSubmatch(data)
	if m == nil {
		t.Fatal("summary.md carries no Drift row")
	}
	n, _ := strconv.Atoi(string(m[1]))
	if got := len(readDrift(t)); n != got {
		t.Errorf("summary.md says %d drifted, drift.json lists %d", n, got)
	}
	pin, err := readPin(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "`"+pin+"`") {
		t.Errorf("summary.md was not measured at the pin %s", pin)
	}
}

// fakeTree is an in-memory matter.js tree.
type fakeTree map[string][]string

func (f fakeTree) files() []string {
	out := make([]string, 0, len(f))
	for p := range f {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func (f fakeTree) content(p string) ([]string, error) {
	lines, ok := f[p]
	if !ok {
		return nil, fmt.Errorf("%s: missing", p)
	}
	return lines, nil
}

// file returns n filler lines with the given lines set (1-based).
func file(n int, set map[int]string) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "  // filler"
	}
	for l, s := range set {
		out[l-1] = s
	}
	return out
}

func sampleTree() fakeTree {
	return fakeTree{
		"packages/core/src/Widget.ts":  file(70, map[int]string{1: "export class Widget {", 2: "  run() {}", 3: "  stop() {}", 60: "  runFast() {}"}),
		"packages/core/src/Gadget.ts":  file(40, map[int]string{1: "// knot", 2: "export class Gadget {", 30: "  #tie() {}"}),
		"packages/other/lib/Gadget.ts": file(5, nil),
		"packages/a/index.ts":          file(3, nil),
		"packages/b/index.ts":          file(3, nil),
	}
}

func sampleCitations(t *testing.T) []citation {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("testdata", "sample.go.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return collectFile("sample.go", string(src))
}

// expectation is testdata/expect.json: what sample.go.txt yields against
// sampleTree. The citation texts live there, not here, so the guard does
// not read them as the module's own.
type expectation struct {
	Citations []struct {
		Line     int      `json:"line"`
		Text     string   `json:"text"`
		Idents   []string `json:"idents"`
		Attached []string `json:"attached"`
		Shas     []string `json:"shas"`
	} `json:"citations"`
	Results map[string]string `json:"results"`
	Fixes   map[string]string `json:"fixes"`
	Owner   []string          `json:"owner"`
}

func readExpectation(t *testing.T) expectation {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "expect.json"))
	if err != nil {
		t.Fatal(err)
	}
	var e expectation
	if err := json.Unmarshal(data, &e); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestCollect(t *testing.T) {
	wants := readExpectation(t).Citations
	got := sampleCitations(t)
	if len(got) != len(wants) {
		for _, c := range got {
			t.Logf("%d %s %v", c.Line, c.Text, c.Idents)
		}
		t.Fatalf("collected %d citations, want %d", len(got), len(wants))
	}
	for i, w := range wants {
		c := got[i]
		if c.Line != w.Line || c.Text != w.Text || !slices.Equal(c.Idents, w.Idents) || !slices.Equal(c.Attached, w.Attached) || !slices.Equal(c.Shas, w.Shas) {
			t.Errorf("citation %d = line %d %q idents %v attached %v shas %v, want %+v", i, c.Line, c.Text, c.Idents, c.Attached, c.Shas, w)
		}
	}
}

func TestCheckAndDerive(t *testing.T) {
	e := readExpectation(t)
	tr := sampleTree()
	cites := sampleCitations(t)
	results := make([]result, 0, len(cites))
	for _, c := range cites {
		results = append(results, check(tr, c))
	}
	by := func(kind string) result {
		for _, r := range results {
			if fmt.Sprintf("%d %s", r.cite.Line, r.cite.Text) == e.Results[kind] {
				return r
			}
		}
		t.Fatalf("no result for %s (%s)", kind, e.Results[kind])
		return result{}
	}
	if r := by("basename"); r.rule != ruleBasename || r.drift == nil || r.drift.Nearest != 60 || r.drift.Occurrences != 1 || !r.attached {
		t.Errorf("basename: %+v, want attached drift to 60", r)
	}
	if r := by("exact"); r.rule != ruleExact || r.drift != nil || r.hard != "" || !r.checked {
		t.Errorf("exact: %+v", r)
	}
	if r := by("suffix"); r.rule != ruleSuffix || r.drift == nil || r.attached {
		t.Errorf("suffix: %+v, want unattached drift", r)
	}
	if r := by("ambiguous"); r.hard != errAmbiguous || len(r.candidates) != 2 {
		t.Errorf("ambiguous: %+v, want two candidates", r)
	}
	if r := by("beyond"); r.hard != errLine || r.length != 70 {
		t.Errorf("beyond: %+v, want beyond 70 lines", r)
	}
	if r := by("unnamed"); r.checked || r.hard != "" {
		t.Errorf("a citation without names is checked hard only: %+v", r)
	}

	sum := summarise("pin", results)
	if sum.citations != 11 || sum.files != 1 || len(sum.hard) != 4 || len(sum.drift) != 2 {
		t.Errorf("summary: %d citations, %d files, %d hard, %d drift; want 11, 1, 4, 2", sum.citations, sum.files, len(sum.hard), len(sum.drift))
	}
	dir := t.TempDir()
	if err := writeArtefacts(dir, sum); err != nil {
		t.Fatal(err)
	}
	md, _ := os.ReadFile(filepath.Join(dir, "summary.md"))
	if !strings.Contains(string(md), "| **Drift** | 2 |") || !strings.Contains(string(md), "## Hard errors") {
		t.Errorf("summary.md:\n%s", md)
	}

	fixes, owner := derive(tr, results)
	if len(fixes) != len(e.Fixes) {
		t.Errorf("fixes = %+v, want %v", fixes, e.Fixes)
	}
	for _, f := range fixes {
		if want := e.Fixes[fmt.Sprintf("%d %s", f.Line, f.Old)]; f.New != want {
			t.Errorf("fix %+v, want new %q", f, want)
		}
	}
	problems := make([]string, 0, len(owner))
	for _, o := range owner {
		problems = append(problems, fmt.Sprintf("%d %s %s", o.Line, o.Citation, o.Problem))
	}
	sort.Strings(problems)
	if !slices.Equal(problems, e.Owner) {
		t.Errorf("owner = %v, want %v", problems, e.Owner)
	}
	if err := writeFixes(filepath.Join(dir, "fixes.json"), tr, results); err != nil {
		t.Fatal(err)
	}
}

func TestResolveAndSuffix(t *testing.T) {
	tr := sampleTree()
	for _, tc := range []struct{ cited, resolved, rule string }{
		{"../matter.js/packages/core/src/Widget.ts", "packages/core/src/Widget.ts", ruleExact},
		{"matter.js/packages/core/src/Widget.ts", "packages/core/src/Widget.ts", ruleExact},
		{"core/src/Widget.ts", "packages/core/src/Widget.ts", ruleSuffix},
		{"Widget.ts", "packages/core/src/Widget.ts", ruleBasename},
		{"packages/core/Widget.ts", "", ruleExact},
		{"re/src/Widget.ts", "", ruleSuffix}, // suffixes end on a path element
	} {
		got, rule, _ := resolve(tr, tc.cited)
		if got != tc.resolved || rule != tc.rule {
			t.Errorf("resolve(%q) = %q, %s; want %q, %s", tc.cited, got, rule, tc.resolved, tc.rule)
		}
	}
	if got := uniqueSuffix(tr.files(), "packages/a/index.ts"); got != "a/index.ts" {
		t.Errorf("uniqueSuffix = %q", got)
	}
}

func TestOccurrencesLooseAndStrict(t *testing.T) {
	lines := []string{"interface BdxReceiveAccept {", "class ReceiveAcceptSchema {}", "  #tie = 1;", "  tie2"}
	if got := occurrences(lines, "ReceiveAccept", true); !slices.Equal(got, []int{1}) {
		t.Errorf("loose = %v, want the prefixed type", got)
	}
	if got := occurrences(lines, "ReceiveAccept", false); len(got) != 0 {
		t.Errorf("strict = %v, want none", got)
	}
	if got := occurrences(lines, "#tie", false); !slices.Equal(got, []int{3}) {
		t.Errorf("#tie = %v", got)
	}
}

func TestRunWithoutTheClone(t *testing.T) {
	code, err := run(filepath.Join("..", ".."), filepath.Join(t.TempDir(), "none"), filepath.Join(t.TempDir(), "none"), t.TempDir(), "")
	if code != 2 || err == nil {
		t.Errorf("run without a clone = %d, %v; want exit 2", code, err)
	}
	if code, _ := run(t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir(), ""); code != 2 {
		t.Errorf("run without parity/schema.json = %d, want 2", code)
	}
}

// gitInit makes a one-commit repository in a temporary directory holding
// files and returns it with the commit id.
func gitInit(t *testing.T, files map[string]string) (dir, sha string) {
	t.Helper()
	dir = t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	r := &gitRepo{dir: dir}
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "-A"},
		{"-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "fixture"},
	} {
		if _, err := r.git(args...); err != nil {
			t.Fatal(err)
		}
	}
	out, err := r.git("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	return dir, strings.TrimSpace(string(out))
}

// TestCitecheckSecondTree pins the routing of brief M9-module §1: a
// sentence naming matterjs-server is checked against the second clone at
// its own `at <sha>` (a file matter.js lacks resolves), never at the
// matter.js pin; one naming no commit of it is a hard error; and without
// the clone the run is exit 2, as without matter.js.
func TestCitecheckSecondTree(t *testing.T) {
	// The fixture's citations are spelt in pieces: written whole, this
	// file's literals would be citations of the module itself.
	const ts = ".t" + "s"
	body := strings.Repeat("// line\n", 20)
	matter, pin := gitInit(t, map[string]string{"packages/a/src/Bar" + ts: body})
	server, sha := gitInit(t, map[string]string{"packages/ws-controller/src/Foo" + ts: body})

	module := func(src string) string {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "parity"), 0o750); err != nil {
			t.Fatal(err)
		}
		schema := fmt.Sprintf(`{"matter": {"sourceCommit": %q}}`, pin)
		if err := os.WriteFile(filepath.Join(root, "parity", "schema.json"), []byte(schema), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "a.go"), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
		return root
	}
	good := fmt.Sprintf("package a\n\n// Mirrors matter.js Bar"+ts+":3.\n\n// After matterjs-server at %s, packages/ws-controller/src/Foo"+ts+":5.\n", sha[:8])
	root := module(good)
	out := t.TempDir()
	if code, err := run(root, matter, server, out, ""); code != 0 {
		t.Fatalf("run = %d, %v; want 0", code, err)
	}
	sum, err := os.ReadFile(filepath.Join(out, "summary.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"| Citations | 2 |", "| Checked against matterjs-server (`MATTERJS_SERVER_DIR`) at their `at <sha>` | 1 |", "| **Hard errors** | 0 |"} {
		if !strings.Contains(string(sum), want) {
			t.Errorf("summary.md lacks %q:\n%s", want, sum)
		}
	}

	if code, err := run(root, matter, filepath.Join(t.TempDir(), "none"), t.TempDir(), ""); code != 2 || err == nil {
		t.Errorf("run without the matterjs-server clone = %d, %v; want exit 2", code, err)
	}

	noSha := module("package a\n\n// After matterjs-server, packages/ws-controller/src/Foo" + ts + ":5.\n")
	out = t.TempDir()
	if code, _ := run(noSha, matter, server, out, ""); code != 1 {
		t.Errorf("run with a matterjs-server citation naming no commit = %d, want 1", code)
	}
	if sum, _ := os.ReadFile(filepath.Join(out, "summary.md")); !strings.Contains(string(sum), errNoServerCommit) {
		t.Errorf("summary.md does not name %q:\n%s", errNoServerCommit, sum)
	}

	wrongTree := module(fmt.Sprintf("package a\n\n// After matterjs-server at %s, packages/a/src/Bar"+ts+":3.\n", sha[:8]))
	if code, _ := run(wrongTree, matter, server, t.TempDir(), ""); code != 1 {
		t.Errorf("a matter.js path cited as matterjs-server's = %d, want 1 (not found in the second tree)", code)
	}
}
