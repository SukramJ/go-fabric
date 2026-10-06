// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The CSA certification cases, run by family the way matter.js runs them.
//
// Certification is not pursued by this project and nothing here is a
// certification result; certifiability is a goal
// (docs/adr/0011-certifiability-is-a-goal.md), and this leg is how it is
// held. The PICS each case runs with is CHIP's ci-pics-values, then the
// daemon's hand declarations (testdata/reference-bridge.pics), then the
// endpoint's slice generated from the device itself (testdata/pics,
// testdata/gen_pics.py) — matter.js composes ci-pics-values with its own
// overrides the same way (packages/testing/src/chip/config.ts defaultPics),
// and TC-IDM-10.4 holds the result against the device on every endpoint.
//
// The method is matter.js's (support/chip-testing/test/**/*.test.ts and
// packages/testing/src/chip/chip.ts): a family — every YAML and Python case
// of one cluster or core area, as the image's test descriptor lists them —
// runs in full unless a case is excluded with a written reason. A new case
// in a new image is run, not silently left out. Every exclusion is reported
// as a skip naming its reason.
//
// What makes a run meaningful is pinned per case in
// testdata/chip-cases.golden.json: how many test methods and steps ran and
// how many were skipped. A case whose steps are PICS-gated can pass by
// executing nothing — an over-broad PICS edit turns it green and silent — so
// a changed count fails the case until the golden file is regenerated with
// -update-chip-cases and the diff is read.
//
// The DUT is the reference daemon itself, bridged topology and all. A case
// about one cluster is pointed at the bridged endpoint that serves it
// (--endpoint), which is how the CHIP harness addresses a DUT whose cluster
// is not on endpoint 1.

var updateChipCases = flag.Bool("update-chip-cases", false,
	"rewrite internal/chiptool/testdata/chip-cases.golden.json with the counts of this run")

// chipFamilyEnv narrows the run to a comma-separated list of families.
const chipFamilyEnv = "GOFABRIC_CHIP_FAMILIES"

// chipCase is one runnable case of the image's test descriptor.
type chipCase struct {
	ID      string // "IDM/10.1"
	Family  string
	Name    string
	Kind    string // "yaml" or "py"
	Path    string
	Subpath string
	Args    []string // the descriptor's script-args
	Steps   []string
	// PICS is the descriptor's applicability expression ("" for none).
	PICS string
}

// descriptorNode mirrors /lib/test-descriptor.json (format 2), which the
// image generates from the CHIP tree (support/chip/support/
// generate-test-descriptor in matter.js).
type descriptorNode struct {
	PICS    string           `json:"pics"`
	Kind    string           `json:"kind"`
	Name    string           `json:"name"`
	Path    string           `json:"path"`
	Subpath string           `json:"subpath"`
	Config  map[string]any   `json:"config"`
	Members []descriptorNode `json:"members"`
}

// loadChipCases reads the descriptor out of the image and flattens it to
// family → cases. A Python suite with several runs (one per CHIP app) keeps
// run1: the runs differ in which CHIP app they drive, and there is one DUT
// here.
func loadChipCases(ctx context.Context, h *harness) (map[string][]chipCase, error) {
	raw, err := h.exec(ctx, "cat", "/lib/test-descriptor.json")
	if err != nil {
		return nil, fmt.Errorf("read the test descriptor: %w\n%s", err, raw)
	}
	var root descriptorNode
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		return nil, fmt.Errorf("decode the test descriptor: %w", err)
	}
	out := map[string][]chipCase{}
	var walk func(n descriptorNode, trail []string)
	walk = func(n descriptorNode, trail []string) {
		for _, m := range n.Members {
			switch m.Kind {
			case "suite":
				if m.Path != "" && len(trail) == 1 {
					// A multi-run Python case: take run1.
					for _, run := range m.Members {
						if run.Name == "run1" {
							run.Name = m.Name
							if run.PICS == "" {
								run.PICS = m.PICS
							}
							addCase(out, trail[0], run)
						}
					}
					continue
				}
				walk(m, append(slices.Clone(trail), m.Name))
			case "yaml", "py", "manual":
				if len(trail) == 1 {
					addCase(out, trail[0], m)
				}
			}
		}
	}
	walk(root, nil)
	return out, nil
}

func addCase(out map[string][]chipCase, fam string, n descriptorNode) {
	// The descriptor lists a Python file that holds several cases once per
	// case, and some entries twice; one case id is one case.
	for i := range out[fam] {
		if out[fam][i].Name == n.Name {
			return
		}
	}
	c := chipCase{
		ID: fam + "/" + n.Name, Family: fam, Name: n.Name, Kind: n.Kind,
		Path: n.Path, Subpath: n.Subpath, PICS: n.PICS,
	}
	switch a := n.Config["script-args"].(type) {
	case string:
		c.Args = strings.Fields(a)
	case []any:
		for _, s := range a {
			if str, ok := s.(string); ok {
				c.Args = append(c.Args, str)
			}
		}
	}
	for _, s := range n.Members {
		if s.Kind == "step" {
			c.Steps = append(c.Steps, s.Name)
		}
	}
	out[fam] = append(out[fam], c)
}

// caseCounts is what a run of one case did, pinned in the golden file.
type caseCounts struct {
	// Tests and TestsSkipped count Python test methods (mobly's summary);
	// zero for YAML.
	Tests        int `json:"tests,omitempty"`
	TestsSkipped int `json:"testsSkipped,omitempty"`
	// Steps and StepsSkipped count steps: "***** Test Step" / "**** Skipping"
	// lines for Python, the runner's own summary for YAML.
	Steps        int `json:"steps"`
	StepsSkipped int `json:"stepsSkipped"`
}

var (
	reYamlSummary = regexp.MustCompile(`Run finished in \d+ms with (\d+) steps runned and (\d+) steps skipped\.`)
	rePySummary   = regexp.MustCompile(`Test results: Error (\d+), Executed (\d+), Failed (\d+), Passed (\d+), Requested (\d+), Skipped (\d+)`)
)

// parsePython extracts counts and the verdict from a Python case's output.
// Pass is the framework's own last word: mobly's "Final result: PASS" or a
// "[Test] <name> PASS" line, as matter.js's python-test.ts reads it, and no
// failed or errored test in the summary.
func parsePython(out string) (caseCounts, bool) {
	var c caseCounts
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.Contains(line, " INFO ***** Test Step "):
			c.Steps++
		case strings.Contains(line, "**** Skipping: "):
			c.StepsSkipped++
		}
	}
	m := rePySummary.FindStringSubmatch(out)
	if m == nil {
		return c, false
	}
	errs, _ := strconv.Atoi(m[1])
	executed, _ := strconv.Atoi(m[2])
	failed, _ := strconv.Atoi(m[3])
	skipped, _ := strconv.Atoi(m[6])
	c.Tests, c.TestsSkipped = executed, skipped
	passed := strings.Contains(out, "Final result: PASS") || rePyTestPass.MatchString(out)
	return c, errs == 0 && failed == 0 && passed
}

// rePyTestPass is the new-format verdict matter.js's python-test.ts accepts.
var rePyTestPass = regexp.MustCompile(`(?m)\[Test\]\s+\S+\s+PASS\s*$`)

// --- the golden counts ------------------------------------------------------

type goldenFile struct {
	// Image is the harness the counts were taken with; a different image is
	// a different case set and its counts are not comparable.
	Image string                `json:"image"`
	Cases map[string]caseCounts `json:"cases"`
	// Families lists every case of each declared family in the image's
	// descriptor — run, excluded or not — so docs/certifiability.md can be
	// checked against the suite without the harness.
	Families map[string][]string `json:"families"`
	// NotApplicable maps a case to the descriptor PICS expression that is
	// false for the DUT: the case does not apply and is not run, as the CHIP
	// test harness and matter.js select cases (class (b)).
	NotApplicable map[string]string `json:"notApplicable,omitempty"`
	// HostSkipped maps a case to why it was not run — a manual case, or
	// one the host that took the counts cannot carry (no IPv6 multicast):
	// class (c).
	HostSkipped map[string]string `json:"hostSkipped,omitempty"`
}

var (
	goldenMu      sync.Mutex
	goldenLoaded  *goldenFile
	goldenUpdates = map[string]caseCounts{}
	familyUpdates = map[string][]string{}
	naUpdates     = map[string]string{}
	envUpdates    = map[string]string{}
)

func loadGolden(t *testing.T) *goldenFile {
	t.Helper()
	goldenMu.Lock()
	defer goldenMu.Unlock()
	if goldenLoaded != nil {
		return goldenLoaded
	}
	g := &goldenFile{Cases: map[string]caseCounts{}}
	data, err := os.ReadFile(filepath.Join("testdata", chipCasesGoldenFile))
	if err == nil {
		if err := json.Unmarshal(data, g); err != nil {
			t.Fatalf("decode testdata/%s: %v", chipCasesGoldenFile, err)
		}
	}
	goldenLoaded = g
	return g
}

// checkCounts holds a passing case's counts against the golden file, or
// records them under -update-chip-cases.
func checkCounts(t *testing.T, h *harness, id string, got caseCounts) {
	t.Helper()
	g := loadGolden(t)
	if *updateChipCases {
		goldenMu.Lock()
		goldenUpdates[id] = got
		goldenMu.Unlock()
		return
	}
	want, ok := g.Cases[id]
	if !ok {
		t.Fatalf("%s passed with %+v but has no entry in testdata/%s; run with -update-chip-cases, read the diff, commit it",
			id, got, chipCasesGoldenFile)
	}
	if g.Image != h.image {
		t.Logf("golden counts were taken with %s; this run uses %s", g.Image, h.image)
	}
	if got != want {
		t.Fatalf("%s ran %+v, golden file says %+v. The case or the PICS moved: read the step and skip lines in "+
			"the output, and regenerate with -update-chip-cases only once you know which steps changed and why", id, got, want)
	}
}

// writeGolden writes the updated golden file at the end of an
// -update-chip-cases run, merging into the existing entries.
func writeGolden(t *testing.T, h *harness) {
	t.Helper()
	if !*updateChipCases {
		return
	}
	g := loadGolden(t)
	goldenMu.Lock()
	defer goldenMu.Unlock()
	g.Image = h.image
	if g.Families == nil {
		g.Families = map[string][]string{}
	}
	// A family that ran is rewritten whole: a case that no longer passes
	// (now excluded, or failing) must not keep its old counts.
	if g.NotApplicable == nil {
		g.NotApplicable = map[string]string{}
	}
	if g.HostSkipped == nil {
		g.HostSkipped = map[string]string{}
	}
	// A case this host cannot run keeps the counts a capable host (CI)
	// recorded: host-skipping it here says nothing about what it executes.
	kept := map[string]caseCounts{}
	for id := range envUpdates {
		if c, ok := g.Cases[id]; ok {
			kept[id] = c
		}
	}
	for fam, names := range familyUpdates {
		for id := range g.HostSkipped {
			if strings.HasPrefix(id, fam+"/") {
				delete(g.HostSkipped, id)
			}
		}
		g.Families[fam] = names
		for id := range g.Cases {
			if strings.HasPrefix(id, fam+"/") {
				delete(g.Cases, id)
			}
		}
		for id := range g.NotApplicable {
			if strings.HasPrefix(id, fam+"/") {
				delete(g.NotApplicable, id)
			}
		}
	}
	for id, expr := range naUpdates {
		g.NotApplicable[id] = expr
	}
	for id, why := range envUpdates {
		if c, ok := kept[id]; ok {
			g.Cases[id] = c
			continue
		}
		g.HostSkipped[id] = why
	}
	for id, c := range goldenUpdates {
		g.Cases[id] = c
	}
	data, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("testdata", chipCasesGoldenFile), append(data, '\n'), 0o644); err != nil { //nolint:gosec // a committed fixture
		t.Fatal(err)
	}
}

// --- PICS --------------------------------------------------------------------

func readPICS(file string) (map[string]string, error) {
	f, err := os.Open(file) //nolint:gosec // a fixed testdata path
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("%s: malformed line %q", file, line)
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return out, sc.Err()
}

// --- the DUT -----------------------------------------------------------------

// dutPasscode / dutDiscriminator are the reference daemon's defaults
// (examples/reference-bridge/main.go), which every borrowed case commissions
// with.
const (
	dutPasscode      = 20202021
	dutDiscriminator = 3840
	// chipTestNodeID is the node id the CHIP harness assigns by default
	// (0x12344321, the YAML config's nodeId and matter_testing's dut_node_id).
	chipTestNodeID = 0x12344321
)

// errNotCommissioned marks a DUT commissioning failure — the case cannot say
// anything when the device does not pair.
var errNotCommissioned = errors.New("the Python harness could not commission the reference daemon")

// --- the family leg ------------------------------------------------------------

// newCaseWork creates the shared work directory of one case.
func newCaseWork(t *testing.T, h *harness) string {
	t.Helper()
	dir := filepath.Join(h.shared, "case-"+sanitizeTestName(t.Name())+"-"+strconv.FormatInt(time.Now().UnixNano(), 36))
	if err := os.MkdirAll(dir, 0o777); err != nil { //nolint:gosec // the container's root writes here
		t.Fatal(err)
	}
	_ = os.Chmod(dir, 0o777) //nolint:gosec // as above
	return dir
}

// reProblemBlock is one problem record of the CHIP checkers
// (matter/testing/problem_notices.py ProblemNotice.__str__): a severity,
// a location block, the problem text. A pattern is matched against the
// record flattened to one line, "<location> | <problem>", so it can name
// the cluster the problem is about.
var reProblemBlock = regexp.MustCompile(`(?s)Problem: (\w+)\n(.*?)\n\s*problem: ([^\n]+)`)

func onlyKnownProblems(t *testing.T, out string, known knownProblems) bool {
	t.Helper()
	blocks := reProblemBlock.FindAllStringSubmatch(out, -1)
	if len(blocks) == 0 {
		return false
	}
	hit := make([]bool, len(known.patterns))
	ok := true
	for _, b := range blocks {
		if b[1] != "ERROR" {
			continue // warnings do not fail a case
		}
		record := strings.Join(strings.Fields(b[2]), " ") + " | " + b[3]
		matched := false
		for i, re := range known.patterns {
			if re.MatchString(record) {
				hit[i], matched = true, true
			}
		}
		if !matched {
			t.Errorf("problem outside the recorded divergences: %s", record)
			ok = false
		}
	}
	for i, h := range hit {
		if !h {
			t.Errorf("recorded divergence %q no longer occurs: remove it", known.patterns[i])
			ok = false
		}
	}
	return ok
}

// --- commissioned snapshots ----------------------------------------------------
//
// matter.js commissions each subject once and restores a snapshot of the
// commissioned state before every case (packages/testing/src/chip/state.ts
// activateSubject: "Capture state snapshot" / "Restore state snapshot").
// This does the same: the daemon's database and the controller's storage
// right after commissioning are copied once, and every case starts from a
// copy — a commissioned device fresh from the factory, without paying for
// a commissioning per case.

type dutSnapshot struct {
	dir   string
	files []string
}

var (
	snapshotMu sync.Mutex
	snapshots  = map[string]*dutSnapshot{}
)

// commissionedSnapshot returns the snapshot for a controller kind: "py"
// (the Python harness's controller, admin_storage.json) or "yaml"
// (chip-tool's key-value store, kvs/).
func commissionedSnapshot(ctx context.Context, t *testing.T, h *harness, bin string, flags map[string]bool, kind string) *dutSnapshot {
	t.Helper()
	snapshotMu.Lock()
	defer snapshotMu.Unlock()
	if s, ok := snapshots[kind]; ok {
		return s
	}
	dir := filepath.Join(h.shared, "snapshot-"+kind)
	if err := os.MkdirAll(dir, 0o777); err != nil { //nolint:gosec // the container's root writes here
		t.Fatal(err)
	}
	_ = os.Chmod(dir, 0o777) //nolint:gosec // as above
	br := startBridgeWith(t, bin, flags, bridgeOptions{dbPath: filepath.Join(dir, "dut.db")})
	switch kind {
	case "py":
		cctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		out, err := h.execIn(cctx, dir, "python3", "/src/python_testing/hello_test.py", "--commission-only",
			"--commissioning-method", "on-network",
			"--discriminator", strconv.Itoa(dutDiscriminator), "--passcode", strconv.Itoa(dutPasscode))
		cancel()
		if err != nil || !strings.Contains(out, "Commissioning complete for node ID 0x0000000012344321: success") {
			br.dump(t)
			t.Fatalf("%v: %v\n%s", errNotCommissioned, err, tail(out, 60))
		}
	case "yaml":
		ctl := &controller{bin: h.chipTool, storageDir: filepath.Join(dir, "kvs"), nodeID: chipTestNodeID}
		if err := os.MkdirAll(ctl.storageDir, 0o777); err != nil { //nolint:gosec // the container's root writes here
			t.Fatal(err)
		}
		_ = os.Chmod(ctl.storageDir, 0o777) //nolint:gosec // as above
		out, err := ctl.pair(ctx, t, pairTargetHost, br.info.port, br.info.passcode)
		if err != nil || !commissioningComplete(out) {
			br.dump(t)
			t.Fatalf("chip-tool commissioning for the YAML snapshot stopped at %s: %v", handshakeStage(out), err)
		}
	}
	// Stop the daemon so its database is quiescent before it is copied.
	br.stop()
	br.wg.Wait()
	// The controller's storage is written by the container's root, and
	// chip-tool keeps its KVS mode 0600: make it readable for the copy.
	if out, err := h.exec(ctx, "chmod", "-R", "a+rwX", dir); err != nil {
		t.Fatalf("open the %s snapshot for copying: %v\n%s", kind, err, out)
	}
	s := &dutSnapshot{dir: dir}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		s.files = append(s.files, e.Name())
	}
	snapshots[kind] = s
	t.Logf("commissioned %s snapshot: %v", kind, s.files)
	return s
}

// restoreSnapshot copies a snapshot into a case's work directory. Files the
// container wrote as root are readable to us; the copies are ours.
func restoreSnapshot(t *testing.T, s *dutSnapshot, work string) {
	t.Helper()
	if err := copyTree(s.dir, work); err != nil {
		t.Fatalf("restore the commissioned snapshot: %v", err)
	}
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			if err := os.MkdirAll(target, 0o777); err != nil { //nolint:gosec // shared with the container's root
				return err
			}
			return os.Chmod(target, 0o777) //nolint:gosec // as above
		}
		if d.Type()&os.ModeNamedPipe != 0 {
			return nil
		}
		data, err := os.ReadFile(path) //nolint:gosec // our own snapshot
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o666) //nolint:gosec // shared with the container's root
	})
}

// scriptArgs adapts a case's descriptor arguments to the reference daemon,
// the way matter.js's python-test.ts createCommand does: the arguments are
// kept, except that the onboarding payloads the descriptor names for CHIP's
// app (--qr-code, --manual-code, --discriminator, --passcode) are replaced by
// the daemon's, and what the runner sets itself is dropped (storage, trace
// files, the app pipe, the PICS, the endpoint). --commissioning-method is
// dropped too: the case starts from a commissioned snapshot unless it is
// declared uncommissioned.
func scriptArgs(in []string, dut pairingInfo) []string {
	var out []string
	for i := 0; i < len(in); i++ {
		switch in[i] {
		case "--trace-to", "--storage-path", "--commissioning-method", "--endpoint", "--app-pipe", "--PICS":
			i++
			continue
		case "--qr-code":
			i++
			if dut.qrCode != "" {
				out = append(out, "--qr-code", dut.qrCode)
			}
			continue
		case "--manual-code":
			i++
			if dut.manualCode != "" {
				out = append(out, "--manual-code", dut.manualCode)
			}
			continue
		case "--discriminator":
			i++
			out = append(out, "--discriminator", strconv.Itoa(int(dut.discriminator)))
			continue
		case "--passcode":
			i++
			out = append(out, "--passcode", strconv.Itoa(int(dut.passcode)))
			continue
		case "--debug":
			continue
		case "--enable-spec-errata-ci-only-disallowed-for-certification":
			// Points the checkers at data_model/errata_future.yaml relative
			// to the working directory (matter/testing/runner.py), a file
			// the harness image does not carry; with it set every
			// conformance case records an ERROR about the missing overlay
			// rather than about the device.
			continue
		}
		out = append(out, in[i])
	}
	return out
}

// saveCaseLog keeps a case's full output under bin/chip-harness/logs —
// outside the per-run directory the harness removes — and returns its path.
func saveCaseLog(id, out string) string {
	dir := filepath.Join(moduleRoot(), "bin", "chip-harness", "logs")
	_ = os.MkdirAll(dir, 0o755) //nolint:gosec // a diagnostic directory
	file := filepath.Join(dir, strings.ReplaceAll(id, "/", "_")+".log")
	_ = os.WriteFile(file, []byte(out), 0o644) //nolint:gosec // a diagnostic artefact
	return file
}

// reVerdict picks the lines of a Python case's output that say why it
// failed: test verdicts, the checkers' problem records, assertion text and
// tracebacks, and the summary.
var reVerdict = regexp.MustCompile(`\[Test\]|[Pp]roblem|Assertion|assert|Traceback|Error:|failed|FAIL|Test results|Final result|location:|Endpoint:|Cluster:|Attribute:|Command:`)

func verdictLines(out string) string {
	var b strings.Builder
	n := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "Msg RX") || strings.Contains(line, "Msg TX") || strings.Contains(line, "Retransmission") {
			continue
		}
		if reVerdict.MatchString(line) {
			b.WriteString(line)
			b.WriteByte('\n')
			n++
			if n > 150 {
				b.WriteString("…\n")
				break
			}
		}
	}
	return b.String()
}

// tail returns the last n lines of s.
func tail(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// monitorRestartFlag implements the CHIP Python harness's restart protocol
// the way matter.js's restart-flag-monitor.ts does: a case that needs the DUT
// rebooted (or factory-reset) writes "restart" / "factory reset" into the
// file named by --restart-flag-file and waits for the file to disappear. The
// monitor restarts the daemon on the same database and port (a reboot) or on
// a fresh database (a factory reset), then deletes the flag. It returns the
// daemon currently running and a stop function.
func monitorRestartFlag(t *testing.T, br *bridgeProcess, flagFile string) (current func() *bridgeProcess, stop func()) {
	t.Helper()
	var mu sync.Mutex
	cur := br
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			select {
			case <-done:
				return
			case <-time.After(100 * time.Millisecond):
			}
			data, err := os.ReadFile(flagFile) //nolint:gosec // the case's own work file
			if err != nil {
				continue
			}
			content := strings.TrimSpace(string(data))
			if content == "" {
				continue
			}
			mu.Lock()
			switch content {
			case "restart":
				t.Logf("restart flag: rebooting the daemon on its database")
				cur = cur.restart(t)
			case "factory reset", "factory reset app only":
				t.Logf("restart flag: factory-resetting the daemon")
				cur = cur.factoryReset(t)
			default:
				t.Errorf("restart flag file holds %q, which the CHIP harness protocol does not define", content)
			}
			mu.Unlock()
			_ = os.Remove(flagFile)
		}
	}()
	return func() *bridgeProcess {
			mu.Lock()
			defer mu.Unlock()
			return cur
		}, func() {
			close(done)
			<-stopped
		}
}
