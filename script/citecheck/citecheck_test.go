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
	"bufio"
	"encoding/json"
	"errors"
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
// is 0; each correction lowers it.
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
// 419 after the pin lift to db021da0 (#434, f24d83f1), up from 253: the
// bare citations resolve at the new pin and 166 of them drifted with it —
// a citation whose line moved without changing what it says drifts, and a
// lift moves many at once (notes/parity/pin-lifts.md). They are worked
// off as the files are touched.
//
// 418 after pin lift wave P2 (#431): the testSignerInfo citation of
// controller/attestation/store.go left with the built-in test CD signer.
//
// 416 after pin lift wave P4 (#433): the retransmissionRestartSaving and
// #recomputeConnectionState citations re-cited at db021da0.
//
// 413 after review II, phase 0: four citations re-cited at the pin with
// their fixes (bridge/im_gate.go, endpoint/dispatcher.go, node/node.go and
// its parity test) and one new one left on the ratchet.
//
// 410 after review II, phase 1: the citations the phase's fixes touched
// were re-read and re-cited at the pin (41 comments in 29 files).
//
// 404 after review II, phase 2, and 403 after phase 3: the citations each
// phase's fixes touched were re-read and re-cited at the pin.
//
// 406 after review II, phase 4: by file, citation and identifier, 40 rows
// of the committed list are gone with the phase's fixes and its five
// citation passes and 43 are new. Each of the 43 was read at the pin and
// its citation holds: the sentence names a word the cited range does not
// carry — the construct of a neighbouring citation, the function the range
// lies in, or a model name the source writes in another case.
//
// 380 after review II, phase 6a (unit secure): the #evictExcessSessionsFor
// citation of secure/operational/manager.go re-cited at the pin
// (R2-P6-doc-0116).
//
// 377 after review II, phase 6a wave 1: the drifting citation of
// groups/decode.go was rewritten with net's groups fixes, and the two of
// schema/fabric_scoped.go went with the hand-written tables the generated
// fabric-scope tables replaced.
//
// 374 after review II, phase 6b wave 1, two rows new and none gone. The
// rewritten addNOCRequest sentence of controller/commissioner/wire.go
// names AddNoc, which its cited line does not carry at the pin: that line
// is the addTrustedRootCertificate invoke, the addNoc invoke eleven lines
// below it (an off-pin citation, open for its re-cite). The cap comment of
// controller/dcl/certstore.go names GitHub for its second range; its
// first, the root loop, holds. The lines are in the raising commit.
//
// 343 after review II, phase 6b wave 3: a citation whose sentence spells a
// name in Go's form (AddNoc, IcacValue, OnOff) is kept when the cited file
// spells it in matter.js's lower camel case ("addNoc", icacValue, onOff)
// next to the cited line (lowerCamel). 31 rows go, among them
// controller/commissioner/wire.go's re-cited AddNoc sentence, whose line
// 1466 is the icacValue field of the addNoc invoke; none is new.
//
// 340 after review II, phase 6b wave 4, unit sec: a .ts file a sentence
// names without a line now takes the names nearest to it, as a bare part or
// another language's citation does (R2-P6-test-3202), so a name given for
// it no longer drifts the citation beside it. Three rows go —
// bridge/bridge.go (CloseSession, beside Session.ts),
// bridge/subscribe_dispatch.go (AttributeReadResponse, its own file named
// with its lines broken off) and cluster/levelcontrol/native.go
// (targetValue, beside Transitions.ts); none is new. Rows whose sentence
// now names its commit carry it in "at".
//
// 305 after the Fable wave-4 read applied (review II, phase 6b): the rows
// it corrected go, two heuristic rows come (client/interaction/server.go,
// cluster/spec/decode_value.go: a name the sentence gives for another
// part); none is a moved citation.
//
// 441 at the pin lift db021da0 → b25ed960: the pin moved, and every
// citation whose sentence names no commit is now read at b25ed960, where
// 448 drifted before the lift's re-cites (17 hard errors, the stale
// comments of the lift's P0 list); 441 after them. The remaining rows are
// citations into files the lift moved that their reconcile units (P1–P5)
// re-read, not new citations. 429 after wave P2b (E, A, B, K merged);
// 433 after its units I and F: four new rows whose sentence names the
// construct the cited range sits in, read at b25ed960 — writeValue
// (AttributeWriteResponse.ts:419) around :446-460, expectChunkedWrite
// (tc-idm-3.1-support.ts:119) around :158-162, listLongOctetString beside
// LONG_OCTET_STRING_ENTRY (unit-testing-cluster.ts:66-70), the describe
// LocalizationConfigurationServer of its test file; none is a moved line.
// 435 after P2b-F's follow-up: two rows naming writeValue
// (AttributeWriteResponse.ts:419) around its catch block :463-470, read.
// 434 after wave P3: one row fewer after P3-A's citation corrections in
// cluster/thermo (measured by make cite-report).
// 432 after wave P3b: the pinned re-reads of presets.go's
// setActivePresetRequest (:551-553) and subscription.go's activate()
// (:415/:433) leave the drift list (measured by make cite-report).
// 426 after wave P4-S: the DAV: lines of controller/attestation
// renumbered to DeviceAttestationValidator.ts at b25ed960 leave six rows
// (measured by make cite-report). 423 after P4b-C. 422 after P4b-REG:
// script.go's scripted applyDelay re-cited to InProcessControllerAdapter.ts
// :775-789 at b25ed960 leaves the list (measured by make cite-report).
// 420 after P5-C: the CloseSession cites of
// securechannel_close_parity_test.go and fabric_session_close_test.go
// re-read at b25ed960 leave two rows (measured by make cite-report).
// 415 after P5-A: controller/softwareupdate's cites re-pointed at b25ed960.
// 412 after P5-S: client/interaction/interaction.go's #network, #interactions and ClosedError cites re-read at b25ed960.
// 413 after R3-err-3053: cluster/core/vid_verification.go's cite of
// OperationalCredentialsServer.ts:485-488 at b25ed960, confirmed by
// fable-diff-g1 (statement 2), is drift only because its sentence names
// ConstraintError and the collector finds that name nearest at :453
// (measured by make cite-report).
// 411 after the run-4 fix PR's REG: thermostat_server.go's two cites
// re-read at b25ed960 (R4-doc-4280, verify-r4-pass3.md) leave the drift
// rows; bridge/bridge.go's #589 announce cite of ProtocolService.ts:290-296
// at b25ed960, confirmed by fable-diff-h11 (statement 4), is drift only
// because the collector finds addCluster nearest at :261 (measured by make
// cite-report).
// 410 after phase 8 wave 1's REG: client/peer/probe.go's InteractionMessenger
// cite re-read at b25ed960 (fable-read-a6-a8 A6 (b), :1019-1054) leaves the
// drift rows.
// 411 after phase 8's gate repair: client/peer/probe.go's "tracked, not
// awaited" cite re-pinned to InteractionMessenger.ts:1035-1048 at b25ed960
// (fable-read-reg-w1 stale pin (b)) is drift only because its sentence
// names StatusResponse and the collector finds that name nearest at :999
// (measured by make cite-report).
const citecheckDriftRatchet = 410

// citecheckOffPinRatchet is the number of line citations checked at a
// matter.js commit other than the pin: their sentence names that commit
// (`at <sha>`, a sha opening or closing a parenthesis, "matter.js <sha>",
// "pin <sha>", "HEAD <sha>"), so they are
// measured there and not at the pin (R2-P6-test-3201, decision 123). The
// committed summary.md carries the count and the rows per commit; exact in
// both directions, as citecheckDriftRatchet: a new off-pin citation needs
// its justification in the commit that raises it, and a citation re-read
// and re-labelled at the pin lowers it in the same commit. The goal is 0.
// File-only mentions, bare line parts and chip citations at a commit other
// than the harness commit are counted in summary.md beside it, not held.
//
// 1216 at the first measurement, review II phase 6b wave 4, unit sec: 1327
// with the forms the collector learnt in the same change ("(0ceb202d, …",
// "matter.js 0ceb202d …"; 115 citations that read as the pin's before),
// less the unit's own rows re-read and re-labelled at db021da0. The cite
// unit counted 1211 at dd17ab06 in the old forms. 151 after the five
// wave-4 units; 122 after the Fable wave-4 read applied, with the forms
// of F3 (a sha closing a parenthesis after a comma, "pin <sha>",
// "HEAD <sha>") counted.
//
// 3946 at the pin lift db021da0 → b25ed960: the 3822 line citations
// labelled "at db021da0", checked at the pin until now, are checked at a
// commit other than the pin from this lift on, with no text changed; the
// other 124 are the earlier rows at older commits. Re-reading them at
// b25ed960 lowers the ratchet, unit by unit.
//
// 2889 after the scripted relabel of the same lift, by the whole-file
// rule: an "at db021da0" became "at b25ed960" only where every citation,
// bound bare part and file-only mention the collector binds to that one
// label (those whose commit moves when that label alone is rewritten)
// names a matter.js file byte-identical between the two commits (git diff
// --quiet) that resolves to the same path at b25ed960 — no range-identity
// shortcut, as a claim of absence reads the whole file. 1057 line
// citations relabelled (1001 labels in 499 Go files; 238 more in the
// registers); 2765 remain at db021da0: 2067 cite a file that changed
// upstream (130 files, most of them the Datasource, MessageExchange,
// ServerSubscription and ExchangeManager sources), 693 cite an identical
// file but share their label with a changed one or with a bare line part
// after a citation, whose file the collector does not carry (313 labels
// held by such a part alone), and 5 no longer resolve at b25ed960. A label
// binding no .ts file stays as written. The registers' 308 citations moved
// with the same rule; their ratchet did not move. P1-B (pin lift
// b25ed960, #5012) re-read and relabelled 37 more in the request-check
// files it rewrote (2728 at db021da0 remain). Wave P2b relabelled 10
// more at b25ed960 in the files its units touched (E, A, B); P2b-L2 one
// more (client/exchange/manager.go). The P2b register commit relabelled
// one more (controller/commissioner/fabric.go's MaxLabelLength). Wave P3
// relabelled 15 more at b25ed960 in the files its units touched (A 10, C 2,
// D 3). Wave P3c relabelled 27 more (units A, E, F and the register pass).
// Wave P4-S relabelled 12 more at b25ed960: bridge/conn.go's eight
// ExchangeManager.ts citations and four in controller/attestation
// (validate.go 3, policy.go 1). Wave P5-C relabelled 3 more at b25ed960
// (bridge/securechannel.go's CloseSession sender), its fix round 8 more
// (securechannel.go #deleteSession, bridge.go's two ExchangeManager.ts
// cites, the CloseSession tests, commissioning_window.go's
// PaseSessionCloser). Wave P5-B relabelled 3 more (controller/client
// fabricops.go, client/interaction/interaction.go, controller/client/node.go). Wave P5-A re-pointed controller/softwareupdate's
// cites at b25ed960. Wave P5-S re-pointed images.go's and interaction.go's remaining cites at b25ed960 (2675; images.go's checkForUpdate cite labelled at db021da0, its selection not yet the pin's).
// Wave P5-A2 moved checkForUpdate onto the pin's findApplicable selection and its cite to b25ed960 (2674).
// Wave P6-B relabelled im/subscription/manager.go's flush cites at b25ed960 (2668).
// Wave P6-C relabelled bridge cites at b25ed960 (2658).
// Review II run-3 fix close: node's Add/Replace/Forget Endpoint.ts cites
// re-pinned at b25ed960 per fable-diff-g5c/g5d/g5e (2655).
// Review II run-4 fix PR's REG: the cites re-read at b25ed960 by
// verify-r4-pass3.md and fable-diff-h11 note 6 (2653).
// Review II phase 8 wave 1's REG: the wave re-cited lines at b25ed960
// (measured by make cite-report: 2653 → 2650; which cites moved is not
// itemised) (2650).
// Phase 8's gate repair: the stale pins of fable-read-reg-w1 and
// fable-diff-a1 N7 re-cited at b25ed960 (measured by make cite-report:
// 2650 → 2641) (2641).
const citecheckOffPinRatchet = 2638

// offPinRowRE reads the off-pin row of summary.md.
var offPinRowRE = regexp.MustCompile(`(?m)^\| ` + regexp.QuoteMeta(offPinRow) + ` \| (\d+) \|$`)

func TestCitecheckOffPinOnRatchet(t *testing.T) {
	data, err := os.ReadFile("summary.md")
	if err != nil {
		t.Fatalf("read the committed summary.md: %v", err)
	}
	m := offPinRowRE.FindSubmatch(data)
	if m == nil {
		t.Fatal("summary.md carries no off-pin row (regenerate with make cite-report)")
	}
	n, _ := strconv.Atoi(string(m[1]))
	if n != citecheckOffPinRatchet {
		t.Errorf("summary.md counts %d line citations checked at a matter.js commit other than the pin, the ratchet is %d: a rise needs its justification in the commit, a drop lowers citecheckOffPinRatchet in the same commit", n, citecheckOffPinRatchet)
	}
	// The rows per commit add up to the count.
	sum := 0
	for _, row := range regexp.MustCompile("(?m)^\\| `([0-9a-f]+)` \\| (\\d+) \\| (\\d+) \\|$").FindAllSubmatch(data, -1) {
		k, _ := strconv.Atoi(string(row[2]))
		sum += k
	}
	if sum != n {
		t.Errorf("the off-pin rows per commit add up to %d, the count is %d", sum, n)
	}
}

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
		"packages/protocol/src/certificate/DeviceAttestationValidator.ts": file(40, map[int]string{30: "function idHex() {}"}),
	}
}

func sampleCitations(t *testing.T) []citation {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("testdata", "sample.go.txt"))
	if err != nil {
		t.Fatal(err)
	}
	cites, _ := collectFile("sample.go", string(src))
	return cites
}

// expectation is testdata/expect.json: what sample.go.txt yields against
// sampleTree. The citation texts live there, not here, so the guard does
// not read them as the module's own.
type expectation struct {
	Citations []struct {
		Line     int      `json:"line"`
		Text     string   `json:"text"`
		Path     string   `json:"path"` // compared when set
		Bare     bool     `json:"bare"`
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
		if c.Line != w.Line || c.Text != w.Text || c.Bare != w.Bare || w.Path != "" && c.Path != w.Path ||
			!slices.Equal(c.Idents, w.Idents) || !slices.Equal(c.Attached, w.Attached) || !slices.Equal(c.Shas, w.Shas) {
			t.Errorf("citation %d = line %d %q path %q bare %v idents %v attached %v shas %v, want %+v", i, c.Line, c.Text, c.Path, c.Bare, c.Idents, c.Attached, c.Shas, w)
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
	// An alias resolves to the file it stands for, and its fix keeps the
	// alias (expect.json, "fixes").
	if r := by("alias"); r.rule != ruleExact || r.drift == nil || r.drift.Nearest != 30 || !r.attached {
		t.Errorf("alias: %+v, want an exact path with attached drift to 30", r)
	}

	sum := summarise("pin", results)
	if sum.citations != 21 || sum.files != 1 || len(sum.hard) != 4 || len(sum.drift) != 3 {
		t.Errorf("summary: %d citations, %d files, %d hard, %d drift; want 21, 1, 4, 3", sum.citations, sum.files, len(sum.hard), len(sum.drift))
	}
	// The two bare parts are checked and counted on their own: never a
	// citation, a hard error or a drift of the run.
	if sum.bare != 2 || sum.bareHard != 0 {
		t.Errorf("summary: %d bare parts, %d with a hard error; want 2, 0", sum.bare, sum.bareHard)
	}
	dir := t.TempDir()
	if err := writeArtefacts(dir, sum); err != nil {
		t.Fatal(err)
	}
	md, _ := os.ReadFile(filepath.Join(dir, "summary.md"))
	if !strings.Contains(string(md), "| **Drift** | 3 |") || !strings.Contains(string(md), "## Hard errors") {
		t.Errorf("summary.md:\n%s", md)
	}

	fixes, owner, err := derive(tr, results)
	if err != nil {
		t.Fatal(err)
	}
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
	// Go's exported name is matter.js's lower-camel one: lowerCamel finds
	// "addNoc" and icacValue for AddNoc and IcacValue
	// (controller/commissioner/wire.go's citation of the addNoc invoke);
	// occurrences, which decides whether a citation is drift-checked, does not.
	invoke := []string{`command: "addNoc",`, "icacValue: this.ca.icacCert,", "readdNocx"}
	if got := lowerCamel(invoke, "AddNoc"); !slices.Equal(got, []int{1}) {
		t.Errorf("lowerCamel AddNoc = %v, want the invoke", got)
	}
	if got := lowerCamel(invoke, "IcacValue"); !slices.Equal(got, []int{2}) {
		t.Errorf("lowerCamel IcacValue = %v", got)
	}
	if got := occurrences(invoke, "AddNoc", true); len(got) != 0 {
		t.Errorf("loose AddNoc = %v, want none", got)
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

// TestCitecheckOffPinCounts runs the tool over a module whose citations
// name a matter.js commit other than the pin in each form decision 123
// counts (R2-P6-test-3201, -3202): the line citation at the older commit
// is the one off-pin row of summary.md, under its commit; one naming the
// pin counts at the pin; the bare parts after a file named without a line
// are checked at that commit and counted beside it — one beyond the end of
// the file is no hard error of the run; and a chip citation at a commit
// other than the Makefile's harness commit is counted under its commit.
func TestCitecheckOffPinCounts(t *testing.T) {
	// Spelt in pieces, as in TestCitecheckSecondTree.
	const ts = ".t" + "s"
	matter, old := gitInit(t, map[string]string{"packages/a/src/Bar" + ts: strings.Repeat("// line\n", 20)})
	r := &gitRepo{dir: matter}
	if err := os.WriteFile(filepath.Join(matter, "packages/a/src/Bar"+ts), []byte(strings.Repeat("// line\n", 21)), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", "-A"},
		{"-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "pin"},
	} {
		if _, err := r.git(args...); err != nil {
			t.Fatal(err)
		}
	}
	out, err := r.git("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	pin := strings.TrimSpace(string(out))
	const harness = "6170af8461b10b1766044122ac83332c6d00ab20"

	module := func(makefile bool) string {
		root := t.TempDir()
		files := map[string]string{
			"parity/schema.json": fmt.Sprintf(`{"matter": {"sourceCommit": %q}}`, pin),
			"a.go": "package a\n\n" +
				"// At the pin: Bar" + ts + ":3.\n\n" +
				"// Named: Bar" + ts + ":5 at " + pin[:8] + ".\n\n" +
				"// Older: Bar" + ts + ":4 at " + old[:8] + ".\n\n" +
				"// Parts: Bar" + ts + " at " + old[:8] + ": :6, :7 and :99.\n\n" +
				"// chip at 645b267a: Foo.cpp:12. chip at 6170af84: Foo.cpp:13.\n",
		}
		if makefile {
			files["Makefile"] = "CHIP_TEST_IMAGE_COMMIT ?= " + harness + "\n"
		}
		for name, body := range files {
			p := filepath.Join(root, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return root
	}
	dir := t.TempDir()
	if code, err := run(module(true), matter, filepath.Join(t.TempDir(), "none"), dir, ""); code != 0 {
		t.Fatalf("run = %d, %v; want 0: a bare part beyond the file is no hard error", code, err)
	}
	sum, err := os.ReadFile(filepath.Join(dir, "summary.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"| Citations | 3 |",
		"| Checked at their own `at <sha>`, the pin | 1 |",
		"| " + offPinRow + " | 1 |",
		"| `" + old[:8] + "` | 1 | 3 |",
		"| A .ts file named without a line part | 1 |",
		"| … whose sentence names another matter.js commit | 1 |",
		"| A bare line part bound to the .ts file named before it (checked; not a citation, drift or hard error of the run) | 3 |",
		"| … checked at another matter.js commit | 3 |",
		"| … with a hard error at the commit it was checked at | 1 |",
		"| A chip citation (a chip source in a sentence naming chip or connectedhomeip) | 2 |",
		"| … at a commit other than the harness commit `6170af84` (Makefile CHIP_TEST_IMAGE_COMMIT) | 1 |",
		"| `645b267a` | 1 |",
		"| **Hard errors** | 0 |",
	} {
		if !strings.Contains(string(sum), want) {
			t.Errorf("summary.md lacks %q:\n%s", want, sum)
		}
	}
	if m := offPinRowRE.FindSubmatch(sum); len(m) < 2 || string(m[1]) != "1" {
		t.Errorf("offPinRowRE does not read the off-pin row of a written summary.md: %q", m)
	}

	dir = t.TempDir()
	if code, err := run(module(false), matter, filepath.Join(t.TempDir(), "none"), dir, ""); code != 0 {
		t.Fatalf("run without a Makefile = %d, %v", code, err)
	}
	if sum, _ := os.ReadFile(filepath.Join(dir, "summary.md")); !strings.Contains(string(sum), "not measured: the Makefile names no CHIP_TEST_IMAGE_COMMIT") {
		t.Errorf("summary.md without a harness commit:\n%s", sum)
	}
}

// citecheckRegisterRatchet is the number of citations in the parity
// registers (notes/parity/*.md) that do not resolve: the cited file is not
// in the tree at the commit the sentence is checked at, or lacks the cited
// lines (registers.json, written by `make cite-report`; review II,
// W6-reg-12). Exact in both directions, as citecheckDriftRatchet is: a new
// one fails, and a corrected one fails until the constant is lowered in
// the same commit. The goal is 0.
//
// 20 at the first measurement, at matter.js db021da0 (2026-11-13): 17 in
// by_design.md and 3 in the findings register; 8 "file not found", 7
// "ambiguous", 4 "line beyond end of file" and 1 "no matterjs-server
// commit"; 7 of them cite matterjs-server.
//
// 16 after review II, phase 4: four citations of by_design.md corrected
// with the phase's re-read of the register (13 left there, 3 in the
// findings register; 7 "file not found", 5 "ambiguous", 3 "line beyond end
// of file" and 1 "no matterjs-server commit").
//
// 15 after review II, phase 6b wave 4, unit sec: the collector reads a sha
// opening a parenthesis, so by_design.md's controller-diagnostics row
// ("matterjs-server (`0bd06f2`)") names its matterjs-server commit and its
// WebSocketControllerHandler citation resolves.
const citecheckRegisterRatchet = 15

func TestCitecheckRegistersOnRatchet(t *testing.T) {
	data, err := os.ReadFile("registers.json")
	if err != nil {
		t.Fatalf("read the committed registers.json: %v (regenerate with make cite-report)", err)
	}
	var entries []registerEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("registers.json does not parse: %v", err)
	}
	if got := len(entries); got != citecheckRegisterRatchet {
		t.Errorf("registers.json lists %d citations of the parity registers that do not resolve, the ratchet is %d: a new one is corrected in the register, a drop lowers citecheckRegisterRatchet in the same commit", got, citecheckRegisterRatchet)
	}
	sorted := sort.SliceIsSorted(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Citation < b.Citation
	})
	if !sorted {
		t.Error("registers.json is not sorted by file, line and citation")
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.File, registerDir+"/") || e.Line < 1 || e.Citation == "" || e.Error == "" {
			t.Errorf("incomplete register entry %+v", e)
		}
	}
}

// TestCitecheckRegisters runs the tool over a module with one register:
// a citation that resolves leaves no row, one beyond the file's end and
// one of a file the tree lacks are rows of registers.json — in a
// paragraph that wraps, a table row, a list item and a fenced block —
// and none of them is a hard error of the run or a citation of summary.md.
func TestCitecheckRegisters(t *testing.T) {
	// Spelt in pieces, as in TestCitecheckSecondTree.
	const ts = ".t" + "s"
	matter, pin := gitInit(t, map[string]string{"packages/a/src/Bar" + ts: strings.Repeat("// line\n", 20)})
	root := t.TempDir()
	write := func(name, body string) {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("parity/schema.json", fmt.Sprintf(`{"matter": {"sourceCommit": %q}}`, pin))
	write("a.go", "package a\n")
	write(registerDir+"/reg.md", strings.Join([]string{
		"# Register",
		"",
		"A paragraph that cites Bar" + ts + ":3 and wraps packages/a/",
		"src/Bar" + ts + ":4 over two lines.",
		"",
		"| ID | where |",
		"| --- | --- |",
		"| X | `Bar" + ts + ":99` |",
		"",
		"- an item citing Gone" + ts + ":1",
		"  and continuing",
		"",
		"```",
		"Bar" + ts + ":21",
		"```",
		"",
	}, "\n"))
	write("notes/other/unread.md", "Gone"+ts+":7 is outside the registers.\n")

	out := t.TempDir()
	if code, err := run(root, matter, filepath.Join(t.TempDir(), "none"), out, ""); code != 0 {
		t.Fatalf("run = %d, %v; want 0: a register's failure is not a hard error of the run", code, err)
	}
	data, err := os.ReadFile(filepath.Join(out, "registers.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got []registerEntry
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	want := []registerEntry{
		{File: registerDir + "/reg.md", Line: 8, Citation: "Bar" + ts + ":99", Error: errLine},
		{File: registerDir + "/reg.md", Line: 10, Citation: "Gone" + ts + ":1", Error: errNotFound},
		{File: registerDir + "/reg.md", Line: 14, Citation: "Bar" + ts + ":21", Error: errLine},
	}
	if !slices.Equal(got, want) {
		t.Errorf("registers.json = %+v\nwant %+v", got, want)
	}
	sum, err := os.ReadFile(filepath.Join(out, "summary.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sum), "| Citations | 0 |") {
		t.Errorf("the register's citations are counted in summary.md:\n%s", sum)
	}
}

// unreadableTree lists a.ts but cannot read it: a lazy blob fetch failed.
type unreadableTree struct{}

func (unreadableTree) files() []string { return []string{"packages/x/src/a.ts"} }

func (unreadableTree) content(p string) ([]string, error) {
	return nil, fmt.Errorf("%w: %s: fetch failed", errEnv, p)
}

// TestUnreadableBlobIsNoVerdict: a listed file that cannot be read gives no
// hard error ("file not found") but the run's exit 2 (R2-P6-err-2812).
func TestUnreadableBlobIsNoVerdict(t *testing.T) {
	// The text is split so that citecheck does not collect it as a citation.
	r := check(unreadableTree{}, citation{File: "x.go", Line: 1, Text: "a" + ".ts" + ":3", Path: "a.ts", Lo: 3, Hi: 3})
	if r.hard != "" || !errors.Is(r.env, errEnv) {
		t.Errorf("hard %q, env %v; want no hard error and an environment error", r.hard, r.env)
	}
}

// TestCatFileOutOfStepStaysDead: after a read that left the batch pipe out
// of step, every later read fails with that error instead of taking the
// next object's bytes as its own; "missing" keeps the pipe in step
// (R2-P6-err-2812).
func TestCatFileOutOfStepStaysDead(t *testing.T) {
	c := &catFile{in: nopWriteCloser{}, out: bufio.NewReader(strings.NewReader(
		"a missing\nb blob 10\nsho"))} // the stream ends inside b: a failed fetch
	if _, err := c.show("a"); !errors.Is(err, errMissing) || c.dead != nil {
		t.Fatalf("missing: %v, dead %v", err, c.dead)
	}
	if _, err := c.show("b"); err == nil || c.dead == nil {
		t.Fatalf("short read: %v, dead %v", err, c.dead)
	}
	if data, err := c.show("c"); err == nil {
		t.Errorf("read after the pipe went out of step: %q", data)
	}
}

type nopWriteCloser struct{}

func (nopWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (nopWriteCloser) Close() error                { return nil }
