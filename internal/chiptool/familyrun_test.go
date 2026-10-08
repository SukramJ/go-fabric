// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"net"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// chipCasesEnv narrows the families further to a comma-separated list of
// case ids ("FAN/3.1,DGGEN/2.3"); chipRepeatEnv runs the selection that many
// times over one PICS derivation. Both are for chasing one case: a narrowed
// run cannot rewrite the golden file, which holds whole families.
const (
	chipCasesEnv  = "GOFABRIC_CHIP_CASES"
	chipRepeatEnv = "GOFABRIC_CHIP_REPEAT"
)

// caseFilter is chipCasesEnv split, nil when unset.
var caseFilter []string

// TestChipCertificationFamilies runs chipFamilies in the CHIP harness image.
func TestChipCertificationFamilies(t *testing.T) {
	if v := os.Getenv(chipCasesEnv); v != "" {
		caseFilter = strings.Split(v, ",")
		if *updateChipCases {
			t.Fatalf("%s narrows a family to some of its cases; -update-chip-cases rewrites whole families", chipCasesEnv)
		}
	}
	tool := resolveChipTool(t)
	if tool.harness == nil {
		t.Skipf("the certification families run inside the CHIP harness image; this run uses a host chip-tool (%s). "+
			"Unset %s / set %s=image to run them", tool.describe, chipToolBinEnv, chipHarnessEnv)
	}
	h := tool.harness
	bin := requireBridgeBinary(t)
	flags := requireBridgeFlags(t, bin, "db", "listen")
	defaultListen = fmt.Sprintf(":%d", matterPort)
	defer func() { defaultListen = ":0" }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Hour)
	defer cancel()
	cases, err := loadChipCases(ctx, h)
	if err != nil {
		t.Fatal(err)
	}
	pics := devicePICS(ctx, t, h, bin, flags)
	activePICS = pics
	var only []string
	if v := os.Getenv(chipFamilyEnv); v != "" {
		only = strings.Split(v, ",")
	}
	repeat := 1
	if v := os.Getenv(chipRepeatEnv); v != "" {
		if repeat, err = strconv.Atoi(v); err != nil || repeat < 1 {
			t.Fatalf("%s=%q: want a positive count", chipRepeatEnv, v)
		}
	}
	for range repeat {
		for _, fam := range chipFamilies {
			if only != nil && !slices.Contains(only, fam.name) {
				continue
			}
			t.Run(fam.name, func(t *testing.T) {
				runFamily(ctx, t, h, fam, cases[fam.name], pics, bin, flags)
			})
		}
	}
	writeGolden(t, h)
}

// --- PICS ----------------------------------------------------------------------

// picsSet is the PICS the families run with: one composed file per endpoint
// of the daemon (ci-pics-values, then testdata/reference-bridge.pics, then
// the endpoint's device-generated slice), the same with PICS_SDK_CI_ONLY=0
// for the TC-IDM-10.4 checker, and the device-type → endpoint map read off
// the daemon.
type picsSet struct {
	byEP     map[uint16]string
	values   map[uint16]map[string]string
	honest   map[uint16]string
	epOf     map[uint32]uint16
	sortedEP []uint16
}

func (p *picsSet) endpoint(t *testing.T, deviceType uint32) uint16 {
	t.Helper()
	if deviceType == 0 {
		return 0
	}
	ep, ok := p.epOf[deviceType]
	if !ok {
		t.Fatalf("no endpoint of the reference daemon advertises device type 0x%04X", deviceType)
	}
	return ep
}

// reGeneratedCode is a code the device-generated slices decide: a
// cluster-server element, or a derivable MCORE code.
var reGeneratedCode = regexp.MustCompile(`^[A-Za-z0-9]+\.S(\.[AFC][0-9A-Fa-f]+(\.(Rsp|Tx))?)?$|^MCORE\.(ROLE\.COMMISSIONEE|IDM\.S|BRIDGE|OTA\.Requestor|OTA\.Provider|G\.MULTIENDPOINT)$`)

// reEventCode is a server-side event code. The slices set the events the
// spec makes mandatory to 1 and every other event of a present cluster to 0;
// which optional events the daemon emits cannot be read off the device
// (EventList is not served), so testdata/reference-bridge.pics declares
// them — it may switch an event on, never off.
var reEventCode = regexp.MustCompile(`^[A-Za-z0-9]+\.S\.E[0-9A-Fa-f]+$`)

// devicePICS generates the per-endpoint PICS slices from a commissioned
// daemon with testdata/gen_pics.py (CHIP's own derivation helpers), holds
// them against testdata/pics/ (rewritten under -update-chip-cases), and
// composes the files the cases run with.
func devicePICS(ctx context.Context, t *testing.T, h *harness, bin string, flags map[string]bool) *picsSet {
	t.Helper()
	work := newCaseWork(t, h)
	snap := commissionedSnapshot(ctx, t, h, bin, flags, "py")
	restoreSnapshot(t, snap, work)
	br := startBridgeWith(t, bin, flags, bridgeOptions{dbPath: filepath.Join(work, "dut.db")})
	defer br.stop()

	script, err := os.ReadFile(filepath.Join("testdata", "gen_pics.py"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "gen_pics.py"), script, 0o644); err != nil { //nolint:gosec // read by the container
		t.Fatal(err)
	}
	out := filepath.Join(work, "pics")
	gctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	log, err := h.execIn(gctx, work, "python3", "gen_pics.py", "--string-arg", "out_dir:"+out)
	cancel()
	if _, pass := parsePython(log); err != nil || !pass {
		t.Fatalf("generate the PICS slices: %v\n%s", err, saveCaseLog("PICS/generate", log))
	}

	set := &picsSet{byEP: map[uint16]string{}, values: map[uint16]map[string]string{}, honest: map[uint16]string{}, epOf: map[uint32]uint16{}}
	for _, dt := range []uint32{
		dtOnOffLight, dtColorTempLight, dtExtColorLight, dtSpeaker, dtTempSensor, dtWaterValve, dtModeSelect, dtFan,
		dtSmokeCOAlarm, dtPump, dtFlowSensor, dtLaundryWasher, dtRVC, dtThermostat, dtWindowCovering,
		dtDoorLock, dtHumiditySensor, dtOccupancySensor, dtContactSensor, dtGenericSwitch, dtAirPurifier, dtClosure,
	} {
		set.epOf[dt] = br.endpointFor(t, dt)
	}

	overrides, err := readPICS(filepath.Join("testdata", "reference-bridge.pics"))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range overrides {
		if reGeneratedCode.MatchString(k) {
			t.Errorf("testdata/reference-bridge.pics sets %s, a code the device-generated slices decide; remove the line", k)
		}
		if reEventCode.MatchString(k) && v != "1" {
			t.Errorf("testdata/reference-bridge.pics sets %s=%s; it may only declare an optional event the daemon emits (=1)", k, v)
		}
	}
	base, err := h.exec(ctx, "cat", "/src/app/tests/suites/certification/ci-pics-values")
	if err != nil {
		t.Fatalf("read ci-pics-values: %v", err)
	}

	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	committed := filepath.Join("testdata", "pics")
	if *updateChipCases {
		_ = os.RemoveAll(committed)
		if err := os.MkdirAll(committed, 0o755); err != nil { //nolint:gosec // a committed fixture directory
			t.Fatal(err)
		}
	}
	var drift []string
	slicesByEP := map[uint16]map[string]string{}
	for _, e := range entries {
		var ep int
		if _, err := fmt.Sscanf(e.Name(), "ep%d.txt", &ep); err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(out, e.Name())) //nolint:gosec // the generator's output
		if err != nil {
			t.Fatal(err)
		}
		if *updateChipCases {
			if err := os.WriteFile(filepath.Join(committed, e.Name()), data, 0o644); err != nil { //nolint:gosec // a committed fixture
				t.Fatal(err)
			}
		} else if want, err := os.ReadFile(filepath.Join(committed, e.Name())); err != nil || !bytes.Equal(want, data) { //nolint:gosec // a committed fixture
			drift = append(drift, e.Name())
		}
		slice, err := readPICS(filepath.Join(out, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		// An optional event the daemon emits is declared in the overrides;
		// it applies on an endpoint whose slice knows the event (the
		// cluster is present there).
		for k, v := range overrides {
			if reEventCode.MatchString(k) {
				server := k[:strings.Index(k, ".S.")+2]
				if slice[server] == "1" {
					slice[k] = v
				}
			}
		}
		slicesByEP[uint16(ep)] = slice
		honest := maps.Clone(slice)
		honest["PICS_SDK_CI_ONLY"] = "0"
		set.honest[uint16(ep)] = writeComposedPICS(t, h, fmt.Sprintf("pics-honest-ep%d.txt", ep), base, overrides, honest)
		set.sortedEP = append(set.sortedEP, uint16(ep))
	}
	slices.Sort(set.sortedEP)
	// A case aimed at a bridged endpoint still reaches the node's root
	// clusters on endpoint 0 (ACL, BasicInformation, the commissioning
	// clusters), so its PICS is the union of the two slices: a code is set
	// when either endpoint has the element.
	root := slicesByEP[0]
	for ep, slice := range slicesByEP {
		union := maps.Clone(slice)
		for k, v := range root {
			if v == "1" {
				union[k] = "1"
			}
		}
		set.byEP[ep] = writeComposedPICS(t, h, fmt.Sprintf("pics-ep%d.txt", ep), base, overrides, union)
		vals := lenientPICS(base)
		maps.Copy(vals, overrides)
		maps.Copy(vals, union)
		set.values[ep] = vals
	}
	if len(drift) > 0 {
		t.Fatalf("the device no longer matches its committed PICS slices (%s): the daemon's composition changed. "+
			"Regenerate with -update-chip-cases and read the diff of internal/chiptool/testdata/pics/", strings.Join(drift, ", "))
	}
	return set
}

// writeComposedPICS writes base with each layer applied over it in order.
func writeComposedPICS(t *testing.T, h *harness, name, base string, layers ...map[string]string) string {
	t.Helper()
	merged := map[string]string{}
	for _, l := range layers {
		maps.Copy(merged, l)
	}
	var b strings.Builder
	seen := map[string]bool{}
	for _, line := range strings.Split(base, "\n") {
		key, _, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && !strings.HasPrefix(key, "#") {
			if v, over := merged[key]; over {
				fmt.Fprintf(&b, "%s=%s\n", key, v)
				seen[key] = true
				continue
			}
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	b.WriteString("\n# --- go-fabric reference daemon (testdata/reference-bridge.pics + testdata/pics) ---\n")
	keys := slices.Collect(maps.Keys(merged))
	sort.Strings(keys)
	for _, k := range keys {
		if !seen[k] {
			fmt.Fprintf(&b, "%s=%s\n", k, merged[k])
		}
	}
	file := filepath.Join(h.shared, name)
	if err := os.WriteFile(file, []byte(b.String()), 0o644); err != nil { //nolint:gosec // read by the container
		t.Fatal(err)
	}
	return file
}

// --- the family leg ------------------------------------------------------------

// runFamily runs every case of one family against fresh daemons.
func runFamily(ctx context.Context, t *testing.T, h *harness, fam family, cases []chipCase, pics *picsSet, bin string, flags map[string]bool) {
	t.Helper()
	if len(cases) == 0 {
		t.Fatalf("family %s has no cases in this image's descriptor", fam.name)
	}
	names := make([]string, 0, len(cases))
	for i := range cases {
		names = append(names, cases[i].Name)
	}
	goldenMu.Lock()
	familyUpdates[fam.name] = names
	goldenMu.Unlock()
	for pattern := range fam.exclude {
		if !slices.ContainsFunc(names, func(n string) bool { ok, _ := path.Match(pattern, n); return ok }) {
			t.Errorf("exclusion %q matches no case of %s in this image: remove it", pattern, fam.name)
		}
	}
	for i := range cases {
		c := cases[i]
		if caseFilter != nil && !slices.Contains(caseFilter, c.ID) {
			continue
		}
		if e, ok := fam.picsEdits[c.Name]; ok {
			if !strings.Contains(c.PICS, e.old) {
				t.Errorf("PICS edit of %s (%s): %q is not in the case's expression %q", c.ID, e.reason, e.old, c.PICS)
			}
			c.PICS = strings.Replace(c.PICS, e.old, e.new, 1)
		}
		t.Run(c.Name, func(t *testing.T) {
			if c.Kind == "manual" {
				goldenMu.Lock()
				envUpdates[c.ID] = "a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate"
				goldenMu.Unlock()
				t.Skipf("skipped, %s: a manual case — the CHIP test plan has a test-lab operator perform it (%s)", classHarness, c.Path)
			}
			g, excluded := fam.gapFor(c.Name)
			if excluded && !g.selfSkip {
				t.Skipf("excluded, %s: %s", g.class, g.reason)
			}
			if fam.multicast[c.Name] {
				requireIPv6(t, c.ID, "sends Matter group messages (IPv6 multicast)")
			}
			applyEdits(ctx, t, h, c, fam.edits[c.Name], pics.epOf)
			start := time.Now()
			switch {
			case fam.perEndpoint[c.Name]:
				for _, ep := range pics.sortedEP {
					t.Run(fmt.Sprintf("ep%d", ep), func(t *testing.T) {
						runPythonCase(ctx, t, h, fam, c, c.ID+fmt.Sprintf("@ep%d", ep), pics.honest[ep], ep, true, pics.epOf, bin, flags)
					})
				}
			case c.Kind == "py":
				dt := fam.deviceTypeOf(c.Name)
				ep, passEP := pics.endpoint(t, dt), dt != 0
				if desc, ok := descriptorEndpoint(c.Args); ok && dt == 0 {
					// A core case names an endpoint of the CHIP all-clusters
					// app it was written against: 0 is the root node here
					// too; its application endpoint (1) maps to the daemon's
					// first bridged light, which serves the clusters a core
					// case exercises there (OnOff, Groups, Identify,
					// Scenes) — the daemon's endpoint 1 is the aggregator.
					// Without one the case uses its own default.
					ep, passEP = 0, true
					if desc != 0 {
						ep = pics.endpoint(t, dtOnOffLight)
					}
				}
				runPythonCase(ctx, t, h, fam, c, c.ID, pics.byEP[ep], ep, passEP, pics.epOf, bin, flags)
			case c.Kind == "yaml":
				dt := fam.deviceTypeOf(c.Name)
				ep := pics.endpoint(t, dt)
				runYamlCase(ctx, t, h, fam, c, pics.byEP[ep], ep, dt != 0 && !fam.ownEndpoints[c.Name], pics.epOf, bin, flags)
			}
			t.Logf("%s: %s", c.ID, time.Since(start).Round(100*time.Millisecond))
		})
	}
}

// applyEdits patches a case file in the container (matter.js
// chip.testFor(...).edit). An edit whose old text is gone but whose new text
// is present was applied by an earlier run in this container.
func applyEdits(ctx context.Context, t *testing.T, h *harness, c chipCase, edits []edit, epOf map[uint32]uint16) {
	t.Helper()
	for _, e := range edits {
		// The replacement may name the daemon's endpoints by device type
		// ({ep:0xNNNN}), as the family arguments do.
		e.new = expandEndpointArgs(t, []string{e.new}, epOf)[0]
		file := c.Path
		if c.Kind == "yaml" {
			file = c.Path
		}
		out, err := h.exec(ctx, "python3", "-c", `
import sys
p, old, new = sys.argv[1:4]
s = open(p).read()
if old in s:
    open(p, "w").write(s.replace(old, new))
elif new not in s:
    sys.exit("edit target not found")
`, file, e.old, e.new)
		if err != nil {
			t.Fatalf("edit %s (%s): %v\n%s", c.ID, e.reason, err, out)
		}
		t.Logf("edited %s: %s", c.ID, e.reason)
	}
}

func runPythonCase(ctx context.Context, t *testing.T, h *harness, fam family, c chipCase, id, pics string, ep uint16, passEP bool, epOf map[uint32]uint16, bin string, flags map[string]bool) {
	t.Helper()
	skipNotApplicable(t, c, id, ep)
	work := newCaseWork(t, h)
	if !fam.uncommissioned[c.Name] {
		restoreSnapshot(t, commissionedSnapshot(ctx, t, h, bin, flags, "py"), work)
	}
	br := startBridgeWith(t, bin, flags, bridgeOptions{
		dbPath:    filepath.Join(work, "dut.db"),
		appPipe:   filepath.Join(work, "app.fifo"),
		enableKey: chipTestEnableKey,
	})

	args := []string{
		"python3", c.Path, "--PICS", pics,
		"--app-pipe", filepath.Join(work, "app.fifo"),
		"--restart-flag-file", filepath.Join(work, "restart.flag"),
	}
	for _, a := range scriptArgs(c.Args, br.info) {
		// PIXIT.ACE.APPENDPOINT names the all-clusters app's application
		// endpoint (1); here that is the first bridged light, or the
		// case's own endpoint when it targets an application cluster.
		if strings.HasPrefix(a, "PIXIT.ACE.APPENDPOINT:") && activePICS != nil {
			app := activePICS.epOf[dtOnOffLight]
			if passEP && ep != 0 {
				app = ep
			}
			a = fmt.Sprintf("PIXIT.ACE.APPENDPOINT:%d", app)
		}
		args = append(args, a)
	}
	// A factory-fresh case whose descriptor supplies the onboarding code
	// (TC-SC-7.1) commissions in its own steps; one without (TC-ACL-2.6)
	// has the runner commission it in its commissioning step.
	if fam.uncommissioned[c.Name] && !slices.Contains(args, "--qr-code") && !slices.Contains(args, "--manual-code") {
		args = append(args, "--commissioning-method", "on-network", "--qr-code", br.info.qrCode)
	}
	if c.Subpath != "" {
		args = append(args, "--tests", c.Subpath)
	}
	args = append(args, expandEndpointArgs(t, dutArgs(fam.args["*"], br.info), epOf)...)
	args = append(args, expandEndpointArgs(t, dutArgs(fam.args[c.Name], br.info), epOf)...)
	if passEP {
		args = append(args, "--endpoint", strconv.Itoa(int(ep)))
	}
	rctx, rcancel := context.WithTimeout(ctx, fam.caseTimeout(c.Name))
	defer rcancel()
	current, stopMonitor := monitorRestartFlag(t, br, filepath.Join(work, "restart.flag"))
	out, err := h.execIn(rctx, work, args...)
	stopMonitor()
	counts, pass := parsePython(out)
	logFile := saveCaseLog(id, out)
	if g, ok := fam.gapFor(c.Name); ok && g.selfSkip {
		if counts.Tests != 0 || counts.TestsSkipped == 0 {
			t.Fatalf("%s should skip itself (%s: %s) but ran %+v; full output: %s", id, g.class, g.reason, counts, logFile)
		}
		t.Skipf("skipped itself as declared, %s: %s", g.class, g.reason)
	}
	if !pass {
		if known, ok := fam.knownProblems[c.Name]; ok && onlyKnownProblems(t, out, known) {
			t.Logf("%s failed only on its recorded gap, %s: %s; every other check of the case passed", id, known.class, known.reason)
			checkCounts(t, h, id, counts)
			return
		}
		t.Logf("daemon log: %s (whole log: %s)", tail(current().snapshotStderr(), 40), saveCaseLog(id+"-daemon", current().snapshotStderr()))
		t.Fatalf("%s failed (%v): %+v\nfull output: %s\n--- verdict lines ---\n%s", id, err, counts, logFile, verdictLines(out))
	}
	if known, ok := fam.knownProblems[c.Name]; ok {
		t.Fatalf("%s passed, but it carries the recorded gap %q, which no longer occurs: "+
			"remove it from the family table (and its record) now that it is fixed", id, known.reason)
	}
	checkCounts(t, h, id, counts)
	t.Logf("%s: %d tests (%d skipped), %d steps (%d skipped)", id, counts.Tests, counts.TestsSkipped, counts.Steps, counts.StepsSkipped)
}

func runYamlCase(ctx context.Context, t *testing.T, h *harness, fam family, c chipCase, pics string, ep uint16, passEP bool, epOf map[uint32]uint16, bin string, flags map[string]bool) {
	t.Helper()
	skipNotApplicable(t, c, c.ID, ep)
	if fam.uncommissioned[c.Name] {
		t.Fatalf("%s: uncommissioned YAML cases are not supported by this runner", c.ID)
	}
	work := newCaseWork(t, h)
	restoreSnapshot(t, commissionedSnapshot(ctx, t, h, bin, flags, "yaml"), work)
	br := startBridgeWith(t, bin, flags, bridgeOptions{
		dbPath:    filepath.Join(work, "dut.db"),
		appPipe:   filepath.Join(work, "app.fifo"),
		enableKey: chipTestEnableKey,
	})
	kvs := filepath.Join(work, "kvs")
	args := []string{
		"python3", "/scripts/tests/chipyaml/chiptool.py", "tests", strings.TrimSuffix(path.Base(c.Path), ".yaml"),
		"--server_path", "/bin/chip-tool",
		"--server_arguments", "interactive server --storage-directory " + kvs,
		"--PICS", pics,
		"--PIXIT.CADMIN.CwDuration", "3",
		"--waitAfterCommissioning", "500",
	}
	if passEP {
		args = append(args, "--endpoint", strconv.Itoa(int(ep)))
	}
	args = append(args, expandEndpointArgs(t, fam.args["*"], epOf)...)
	args = append(args, expandEndpointArgs(t, fam.args[c.Name], epOf)...)
	rctx, cancel := context.WithTimeout(ctx, fam.caseTimeout(c.Name))
	defer cancel()
	// A case cut short leaves its chip-tool interactive server running in
	// the container (killing `docker exec` does not reach it), and the
	// next YAML case's server then fails to bind its WebSocket port.
	defer func() {
		_, _ = h.exec(context.Background(), "pkill", "-f", "chip-tool interactive server")
	}()
	current := attachAccessory(t, h, br)
	out, err := h.execIn(rctx, "/", args...)
	h.accessory.detach()
	logFile := saveCaseLog(c.ID, out)
	m := reYamlSummary.FindStringSubmatch(out)
	if err != nil || m == nil || !regexp.MustCompile(`Test finished.+ 0 errors`).MatchString(out) {
		t.Logf("daemon log: %s (whole log: %s)", tail(current().snapshotStderr(), 40), saveCaseLog(c.ID+"-daemon", current().snapshotStderr()))
		t.Fatalf("%s failed (%v)\nfull output: %s\n--- runner output (tail) ---\n%s", c.ID, err, logFile, tail(out, 80))
	}
	run, _ := strconv.Atoi(m[1])
	skipped, _ := strconv.Atoi(m[2])
	counts := caseCounts{Steps: run, StepsSkipped: skipped}
	checkCounts(t, h, c.ID, counts)
	t.Logf("%s: %d steps run, %d skipped", c.ID, run, skipped)
}

// reEndpointArg is a family argument placeholder naming the endpoint of a
// device type: {ep:0x0100} is the daemon's OnOffLight endpoint. A YAML
// case's endpoint config variables (Groups.Endpoint1 in TC-G-2.4) name
// endpoints the test plan leaves to the PIXIT.
var reEndpointArg = regexp.MustCompile(`\{ep:0x([0-9A-Fa-f]{4})\}`)

// expandEndpointArgs resolves {ep:0xNNNN} placeholders against the
// daemon's topology.
func expandEndpointArgs(t *testing.T, args []string, epOf map[uint32]uint16) []string {
	t.Helper()
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = reEndpointArg.ReplaceAllStringFunc(a, func(m string) string {
			v, _ := strconv.ParseUint(reEndpointArg.FindStringSubmatch(m)[1], 16, 32)
			ep, ok := epOf[uint32(v)]
			if !ok {
				t.Fatalf("argument %q names device type 0x%04X, which the family table does not resolve", a, v)
			}
			return strconv.Itoa(int(ep))
		})
	}
	return out
}

// descriptorEndpoint returns the --endpoint the image's descriptor gives a
// case, if any.
func descriptorEndpoint(args []string) (uint16, bool) {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--endpoint" {
			v, err := strconv.ParseUint(args[i+1], 10, 16)
			return uint16(v), err == nil
		}
	}
	return 0, false
}

// lenientPICS reads the KEY=VALUE lines of a PICS text, skipping comments
// and anything else.
func lenientPICS(text string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}

// applicable evaluates the case's descriptor PICS expression against the
// PICS of the endpoint it runs on. A case whose expression is false does
// not apply to the DUT; matter.js leaves it out of the run the same way
// (packages/testing/src/test-descriptor.ts filter).
func (p *picsSet) applicable(t *testing.T, c chipCase, ep uint16) bool {
	t.Helper()
	if c.PICS == "" {
		return true
	}
	ast, err := parsePICSExpr(c.PICS)
	if err != nil {
		t.Fatalf("%s: %v", c.ID, err)
	}
	return ast.evaluate(p.values[ep])
}

// skipNotApplicable skips a case whose descriptor PICS expression is false
// for the DUT — class (b), with the expression as the record — and notes it
// for the golden file and docs/certifiability.md. The per-endpoint PICS
// checker runs regardless: it is what holds the PICS honest.
func skipNotApplicable(t *testing.T, c chipCase, id string, ep uint16) {
	t.Helper()
	if activePICS == nil || activePICS.applicable(t, c, ep) || strings.Contains(id, "@") {
		return
	}
	goldenMu.Lock()
	naUpdates[c.ID] = c.PICS
	goldenMu.Unlock()
	t.Skipf("not applicable, %s: PICS %q is false for this DUT", classNotSupported, c.PICS)
}

// activePICS is the run's PICS set, for skipNotApplicable.
var activePICS *picsSet

// requireIPv6 skips a case that needs IPv6 on the host — group messages
// (Matter groups are IPv6 multicast, ff05::fa, ff35:…) or AAAA records —
// when no up, multicast-capable, non-loopback interface has an IPv6
// address; why says what the case needs it for.
// The skip names the interface and the exact command; the suite never
// changes host network settings itself.
func requireIPv6(t *testing.T, id, why string) {
	t.Helper()
	has, candidates := lanIPv6()
	if has {
		return
	}
	cmd := "an IPv6-capable network interface"
	if len(candidates) > 0 {
		var parts []string
		for _, n := range candidates {
			parts = append(parts, "sudo sysctl -w net.ipv6.conf."+n+".disable_ipv6=0")
		}
		cmd = strings.Join(parts, " && ")
	}
	goldenMu.Lock()
	envUpdates[id] = "not run on the host that took the counts: the case " + why + " and the host's interfaces have no IPv6; enable with `" + cmd + "`"
	goldenMu.Unlock()
	t.Skipf("skipped, %s: the case %s, and no up multicast interface "+
		"of this host has IPv6 (%v). Enable it with: %s", classHarness, why, candidates, cmd)
}

// lanIPv6 reports whether an up, multicast-capable, non-loopback LAN
// interface of this host has an IPv6 address; docker, bridge and veth
// interfaces do not count. candidates names the LAN interfaces without one.
func lanIPv6() (has bool, candidates []string) {
	ifs, err := net.Interfaces()
	if err != nil {
		return false, nil
	}
	for _, ifi := range ifs {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagMulticast == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		if strings.HasPrefix(ifi.Name, "docker") || strings.HasPrefix(ifi.Name, "br-") || strings.HasPrefix(ifi.Name, "veth") {
			continue
		}
		addrs, _ := ifi.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() == nil {
				return true, nil
			}
		}
		candidates = append(candidates, ifi.Name)
	}
	return false, candidates
}

// dutArgs substitutes the daemon's onboarding values into family arguments:
// {passcode}, {discriminator}, {qr}, {manual}.
func dutArgs(in []string, dut pairingInfo) []string {
	out := make([]string, 0, len(in))
	r := strings.NewReplacer("{passcode}", strconv.Itoa(int(dut.passcode)),
		"{discriminator}", strconv.Itoa(int(dut.discriminator)), "{qr}", dut.qrCode, "{manual}", dut.manualCode)
	for _, a := range in {
		out = append(out, r.Replace(a))
	}
	return out
}

// defaultCaseTimeout bounds one case's runner process.
const defaultCaseTimeout = 10 * time.Minute

// caseTimeout is the budget of one case.
func (f family) caseTimeout(name string) time.Duration {
	if d, ok := f.timeout[name]; ok {
		return d
	}
	return defaultCaseTimeout
}

// deviceTypeOf is the device type whose endpoint a case runs against.
func (f family) deviceTypeOf(name string) uint32 {
	if dt, ok := f.caseDeviceType[name]; ok {
		return dt
	}
	return f.deviceType
}

// matterPort is the IANA Matter port (transport/udp.MatterPort).
const matterPort = 5540
