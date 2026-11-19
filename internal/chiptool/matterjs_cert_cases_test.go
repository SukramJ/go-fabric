// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package chiptool

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/parity"
)

// No build tag: TestCertifiabilityDocument holds the Go-TH leg's case set
// to matter.js's certification cases in every `go test ./...`.

var updateMatterJSCertCases = flag.Bool("update-matterjs-cert-cases", false,
	"rewrite testdata/"+matterJSCertCasesFile+" from a matter.js git checkout ($MATTERJS_DIR, default the Makefile's ../matter.js) "+
		"at parity/schema.json's matter.sourceCommit")

const (
	// matterJSCertCasesFile is the list of matter.js's certification case
	// files at the snapshot's pin, under testdata/.
	matterJSCertCasesFile = "matterjs-cert-cases.json"
	// matterJSCertDir is where matter.js keeps them.
	matterJSCertDir = "support/chip-testing/test/cert"
)

// matterJSCertCases is testdata/matterjs-cert-cases.json: every
// TC-*.test.ts file of matter.js's support/chip-testing/test/cert at one
// commit, as `git ls-tree` lists it there (writeMatterJSCertCases).
type matterJSCertCases struct {
	Source string   `json:"source"`
	Commit string   `json:"commit"`
	Cases  []string `json:"cases"`
}

// tsCaseFile is a matter.js certification case file name; the case id is
// "<prefix>/<number>", as goCertID derives it from the TC id.
var tsCaseFile = regexp.MustCompile(`^TC-(.+)-(\d+(?:\.\d+)*)\.test\.ts$`)

// tsCaseID is "TC-APPLAUNCHER-3.7.1.test.ts" → "APPLAUNCHER/3.7.1".
func tsCaseID(file string) (string, bool) {
	m := tsCaseFile.FindStringSubmatch(file)
	if m == nil {
		return "", false
	}
	return m[1] + "/" + m[2], true
}

// schemaPin is parity/schema.json's matter.sourceCommit, the matter.js pin.
func schemaPin(tb testing.TB) string {
	tb.Helper()
	var s struct {
		Matter struct {
			SourceCommit string `json:"sourceCommit"`
		} `json:"matter"`
	}
	if err := json.Unmarshal(parity.SchemaJSON(), &s); err != nil {
		tb.Fatal(err)
	}
	if s.Matter.SourceCommit == "" {
		tb.Fatal("parity/schema.json carries no matter.sourceCommit")
	}
	return s.Matter.SourceCommit
}

// loadMatterJSCertCases reads testdata/matterjs-cert-cases.json, after
// rewriting it under -update-matterjs-cert-cases.
func loadMatterJSCertCases(t *testing.T) matterJSCertCases {
	t.Helper()
	file := filepath.Join("testdata", matterJSCertCasesFile)
	if *updateMatterJSCertCases {
		writeMatterJSCertCases(t, file, schemaPin(t))
	}
	data, err := os.ReadFile(file) //nolint:gosec // a fixed fixture path
	if err != nil {
		t.Fatal(err)
	}
	var ts matterJSCertCases
	if err := json.Unmarshal(data, &ts); err != nil {
		t.Fatalf("decode %s: %v", file, err)
	}
	return ts
}

// writeMatterJSCertCases lists matter.js's certification case files at
// commit from the git objects of the checkout at $MATTERJS_DIR, so the list
// is the commit's whatever the checkout's working tree holds.
func writeMatterJSCertCases(t *testing.T, file, commit string) {
	t.Helper()
	dir := os.Getenv("MATTERJS_DIR")
	if dir == "" {
		// The Makefile's MATTERJS_DIR default, relative to the repository root.
		dir = filepath.Join("..", "..", "..", "matter.js")
	}
	out, err := exec.CommandContext(t.Context(), "git", "-C", dir, "ls-tree", "--name-only", commit, matterJSCertDir+"/").Output() //nolint:gosec // a git read of the pinned commit
	if err != nil {
		t.Fatalf("list %s at %s in %s (a matter.js git checkout that has the commit; set MATTERJS_DIR): %v", matterJSCertDir, commit, dir, err)
	}
	var cases []string
	for _, p := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if name := path.Base(p); tsCaseFile.MatchString(name) {
			cases = append(cases, name)
		}
	}
	if len(cases) == 0 {
		t.Fatalf("%s at %s holds no TC-*.test.ts file", matterJSCertDir, commit)
	}
	slices.Sort(cases)
	data, err := json.MarshalIndent(matterJSCertCases{
		Source: "matter.js " + matterJSCertDir + "/TC-*.test.ts, listed by `git ls-tree` at commit; " +
			"regenerate: MATTERJS_DIR=<matter.js checkout> go test ./internal/chiptool/ -run TestCertifiabilityDocument -args -update-matterjs-cert-cases",
		Commit: commit,
		Cases:  cases,
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, append(data, '\n'), 0o644); err != nil { //nolint:gosec // a committed fixture
		t.Fatal(err)
	}
}

// goCertRegistry lists the Go-TH leg's registered ports, case id → the
// matter.js file it mirrors (goCertCase.source). gocert_test.go sets it
// under the chiptool build tag, where the registry (goCertCases) is
// compiled in; a build without the tag leaves it nil.
var goCertRegistry func() map[string]string

// registeredGoCertPorts is goCertRegistry's list, nil in a build without
// the chiptool tag.
func registeredGoCertPorts() map[string]string {
	if goCertRegistry == nil {
		return nil
	}
	return goCertRegistry()
}

// checkMatterJSCertCases holds the Go-TH leg to matter.js's certification
// cases at the snapshot's pin: each is run by the leg (goCert in the golden
// file, by its source file) or classified, as a controller-role case
// (controllerRoleCases) or as a case not ported yet (matterJSCasesNotRun),
// exactly once; no classification names a case the pin does not have, and
// the list is the pin's, so a pin lift fails here until the list is
// regenerated and every case it adds is run or classified.
//
// ports is the leg's registry (registeredGoCertPorts), nil in a build
// without the chiptool tag. With it, a case counts as run only when a
// registered port runs it, and the golden file's goCert names exactly the
// registered ports with their sources: a port deleted or renamed fails
// here although its golden entry stays.
func checkMatterJSCertCases(tb testing.TB, g docGolden, ts matterJSCertCases, ports map[string]string) {
	tb.Helper()
	if pin := schemaPin(tb); ts.Commit != pin {
		tb.Errorf("testdata/%s lists matter.js's certification cases at %s, the snapshot pins %s: "+
			"regenerate with -update-matterjs-cert-cases and run or classify every case it adds", matterJSCertCasesFile, ts.Commit, pin)
	}
	run := map[string]bool{}
	for id, c := range g.GoCert {
		if ports != nil {
			src, registered := ports[id]
			if !registered {
				tb.Errorf("testdata/%s: goCert names %s, which no registered Go-TH port runs (gocert_*.go): "+
					"drop the entry or register the port", chipCasesGoldenFile, id)
				continue
			}
			if src != c.Source {
				tb.Errorf("testdata/%s: goCert %s names source %s, its registered port mirrors %s", chipCasesGoldenFile, id, c.Source, src)
				continue
			}
		}
		if strings.HasSuffix(c.Source, ".test.ts") {
			run[c.Source] = true
		}
	}
	for id := range ports {
		if _, ok := g.GoCert[id]; !ok {
			tb.Errorf("Go-TH port %s has no goCert entry in testdata/%s: run it with -update-chip-cases, read the diff, commit it",
				id, chipCasesGoldenFile)
		}
	}
	atPin := map[string]bool{}
	for _, file := range ts.Cases {
		id, ok := tsCaseID(file)
		if !ok {
			tb.Errorf("testdata/%s: %q is no TC-<prefix>-<number>.test.ts file", matterJSCertCasesFile, file)
			continue
		}
		if atPin[id] {
			tb.Errorf("testdata/%s lists %s twice", matterJSCertCasesFile, id)
		}
		atPin[id] = true
		_, role := controllerRoleCases[id]
		_, notRun := matterJSCasesNotRun[id]
		switch n := btoi(run[file]) + btoi(role) + btoi(notRun); {
		case n == 0:
			tb.Errorf("%s (matter.js %s at %s) is neither run by the Go-TH leg nor classified: port it (gocert_*.go), "+
				"or give it its class and reason in matterJSCasesNotRun", id, file, ts.Commit)
		case n > 1:
			tb.Errorf("%s: run by the Go-TH leg %v, a controller-role row %v, a not-run row %v — exactly one of them", id, run[file], role, notRun)
		}
	}
	for src := range run {
		if !slices.Contains(ts.Cases, src) {
			tb.Errorf("the Go-TH leg runs %s, which matter.js does not have at %s", src, ts.Commit)
		}
	}
	for _, m := range []map[string]gap{controllerRoleCases, matterJSCasesNotRun} {
		for id := range m {
			if !atPin[id] {
				tb.Errorf("%s is classified as a matter.js certification case, but matter.js has no such case at %s", id, ts.Commit)
			}
		}
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// matterJSCasesNotRun are matter.js's certification cases at the snapshot's
// pin that the Go-TH leg does not run yet, each with its class and what a
// port needs; the module is unmeasured against them. Each cites the TS file
// and the line of its certTest (or case-builder) call at the pin, and the
// PICS codes are the daemon's generated ones (testdata/pics/).
var matterJSCasesNotRun = map[string]gap{
	// A media client case (support file tc-tv-client-support.ts): the DUT
	// sends commands to the tv TH, which answers success.
	"CHANNEL/5.8": notPorted(classNotSupported, "TC-CHANNEL-5.8.test.ts:24", "needs Channel's ElectronicGuide and RecordProgram "+
		"features (GetProgramGuide, RecordProgram, CancelRecordProgram): the module serves them to a host port that implements "+
		"ChannelGuide and ChannelRecorder (`cluster/media/channel.go`: `ChannelGuide`, `ChannelRecorder`), the daemon's video player implements neither "+
		"(CHANNEL.S.F02=0, CHANNEL.S.F03=0)"),

	// Client cases of a device cluster: the DUT invokes the TH's server,
	// read mirrored as TC-IDM-1.1's and TC-LVL-8.1's ports are.
	"DGETH/3.2": notPorted(classNotSupported, "TC-DGETH-3.2.test.ts:19", "needs ResetCounts, which the module's "+
		"EthernetNetworkDiagnostics does not offer, with neither PacketCounts nor ErrorCounts "+
		"(`EthernetNetworkDiagnosticsConfig.Attributes` in `cluster/core/ethernet_network_diagnostics.go`; DGETH.S.C00.Rsp=0): a port would record its only step skipped"),
	"DGSW/3.2": notPorted(classNotSupported, "TC-DGSW-3.2.test.ts:19", "needs ResetWatermarks: the module serves the Watermarks "+
		"feature to a host that supplies a WatermarkReporter (`WatermarkReporter` in `cluster/core/software_diagnostics.go`), the reference daemon "+
		"supplies none (DGSW.S.F00=0, DGSW.S.C00.Rsp=0)"),
	"DGTHREAD/3.4": notPorted(classHarness, "TC-DGTHREAD-3.4.test.ts:27", "needs the daemon's Thread variant (`--otbr` against "+
		"a live OTBR REST API, the DGTHREAD family's composition, run by otbr-virtual.yml), which the Go-TH leg does not start"),
	"CNET/4.21": notPorted(classHarness, "TC-CNET-4.21.test.ts:9", "needs a daemon composition with Thread "+
		"NetworkCommissioning (`core.ThreadNetworkCommissioning`, ADR 0027), which the reference daemon does not mount"),

	// Commissioner cases: in matter.js's own direction (the Go controller as
	// the DUT, the daemon as the TH device), as the DD and ICD DUT ports run.
	"DA/1.3": notPorted(classHarness, "TC-DA-1.3.test.ts:101", "needs, with the Go controller as DUT commissioning the daemon "+
		"twice, each AttestationNonce observable: matter.js reads it from its TH's log; the daemon logs none, and the "+
		"controller's nonce reaches only controller/attestation's validation input (validate.go:40-41)"),
	"OPCREDS/3.3": notPorted(classHarness, "TC-OPCREDS-3.3.test.ts:455", "needs, with the Go controller as DUT, the controller's UpdateNOC for a "+
		"node it commissioned (step 11, :703-758, controller capability `noc-update`; the controller rotates only its own fabric's key, "+
		"`DefaultFabric` in `controller/commissioner/fabricauthority.go`) and the CSRResponse and AddNOC fields observable on the TH "+
		"(steps 5b and 7b, :510, :543): matter.js reads them from its TH's log; the daemon logs the command names only "+
		"(`matter.opcreds.cmd` in `cluster/core/operational_credentials.go`)"),
	"DA/1.4": daDUT("TC-DA-1.4.test.ts:458", ":279"),
	"DA/1.8": daDUT("TC-DA-1.8.test.ts:200", ":105"),
}

// notPorted is the row of a matter.js case the Go-TH leg does not run; at
// is the TS file and line, why what a port needs.
func notPorted(class gapClass, at, why string) gap {
	return gap{class: class, reason: "matter.js automates it at the snapshot's pin (" + at + "); the Go-TH leg does not run it, " +
		"so the module is unmeasured here — " + why}
}

// portableNow is a case whose clusters and harness support exist: status
// "not ported yet" (owner decision 82), held by checkNotPortedYet.
func portableNow(at, how string) gap { return notPorted(classNotPortedYet, at, "portable now: "+how) }

// daDUT is a device attestation case whose TH starts from each of chip's
// DAC-provider test vectors; args is the line that passes them.
func daDUT(at, args string) gap {
	return notPorted(classHarness, at, "needs, with the Go controller as DUT, a daemon started from each of chip's "+
		"credentials/development/commissioner_dut vectors with the vector's product id (`--dac_provider`, `--product-id`, "+args+
		"): the daemon reads one attestation chain from files (`--attestation-dir` in `examples/reference-bridge/main.go`) "+
		"and serves a fixed product id (`testProductID`)")
}

// errorRecorder is a testing.TB whose Errorf records the message instead
// of failing the test; everything else is the test's own.
type errorRecorder struct {
	testing.TB
	errs []string
}

func (r *errorRecorder) Errorf(format string, args ...any) {
	r.errs = append(r.errs, fmt.Sprintf(format, args...))
}

// TestMatterJSCertCasesCountOnlyRegisteredPorts: with the Go-TH leg's
// registry at hand, a golden goCert entry counts a matter.js case as run
// only while a registered port runs it — a port deleted or renamed fails
// the guard although its golden entry stays — and a registered port
// without a golden entry fails it too.
func TestMatterJSCertCasesCountOnlyRegisteredPorts(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", chipCasesGoldenFile))
	if err != nil {
		t.Fatal(err)
	}
	var g docGolden
	if err := json.Unmarshal(data, &g); err != nil {
		t.Fatal(err)
	}
	ts := loadMatterJSCertCases(t)
	var gone string
	ports := map[string]string{}
	for id, c := range g.GoCert {
		ports[id] = c.Source
		if strings.HasSuffix(c.Source, ".test.ts") && (gone == "" || id < gone) {
			gone = id
		}
	}
	if gone == "" {
		t.Fatal("the golden file's goCert names no matter.js case")
	}

	r := &errorRecorder{TB: t}
	checkMatterJSCertCases(r, g, ts, ports)
	if len(r.errs) != 0 {
		t.Fatalf("the registry the golden file names fails the guard: %q", r.errs)
	}

	delete(ports, gone)
	r = &errorRecorder{TB: t}
	checkMatterJSCertCases(r, g, ts, ports)
	if !slices.ContainsFunc(r.errs, func(e string) bool { return strings.Contains(e, "goCert names "+gone+", which no registered") }) ||
		!slices.ContainsFunc(r.errs, func(e string) bool { return strings.HasPrefix(e, gone+" (matter.js") }) {
		t.Errorf("a golden entry for %s without a registered port: the guard said %q, want it to name the entry and the case as not run", gone, r.errs)
	}

	ports["IDM/0.0"] = "TC-IDM-0.0.test.ts"
	r = &errorRecorder{TB: t}
	checkMatterJSCertCases(r, g, ts, ports)
	if !slices.ContainsFunc(r.errs, func(e string) bool { return strings.HasPrefix(e, "Go-TH port IDM/0.0 has no goCert entry") }) {
		t.Errorf("a registered port without a golden entry: the guard said %q, want it named", r.errs)
	}
}
