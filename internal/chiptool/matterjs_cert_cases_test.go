// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package chiptool

import (
	"encoding/json"
	"flag"
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
func schemaPin(t *testing.T) string {
	t.Helper()
	var s struct {
		Matter struct {
			SourceCommit string `json:"sourceCommit"`
		} `json:"matter"`
	}
	if err := json.Unmarshal(parity.SchemaJSON(), &s); err != nil {
		t.Fatal(err)
	}
	if s.Matter.SourceCommit == "" {
		t.Fatal("parity/schema.json carries no matter.sourceCommit")
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

// checkMatterJSCertCases holds the Go-TH leg to matter.js's certification
// cases at the snapshot's pin: each is run by the leg (goCert in the golden
// file, by its source file) or classified, as a controller-role case
// (controllerRoleCases) or as a case not ported yet (matterJSCasesNotRun),
// exactly once; no classification names a case the pin does not have, and
// the list is the pin's, so a pin lift fails here until the list is
// regenerated and every case it adds is run or classified.
func checkMatterJSCertCases(t *testing.T, g docGolden, ts matterJSCertCases) {
	t.Helper()
	if pin := schemaPin(t); ts.Commit != pin {
		t.Errorf("testdata/%s lists matter.js's certification cases at %s, the snapshot pins %s: "+
			"regenerate with -update-matterjs-cert-cases and run or classify every case it adds", matterJSCertCasesFile, ts.Commit, pin)
	}
	run := map[string]bool{}
	for _, c := range g.GoCert {
		if strings.HasSuffix(c.Source, ".test.ts") {
			run[c.Source] = true
		}
	}
	atPin := map[string]bool{}
	for _, file := range ts.Cases {
		id, ok := tsCaseID(file)
		if !ok {
			t.Errorf("testdata/%s: %q is no TC-<prefix>-<number>.test.ts file", matterJSCertCasesFile, file)
			continue
		}
		if atPin[id] {
			t.Errorf("testdata/%s lists %s twice", matterJSCertCasesFile, id)
		}
		atPin[id] = true
		_, role := controllerRoleCases[id]
		_, notRun := matterJSCasesNotRun[id]
		switch n := btoi(run[file]) + btoi(role) + btoi(notRun); {
		case n == 0:
			t.Errorf("%s (matter.js %s at %s) is neither run by the Go-TH leg nor classified: port it (gocert_*.go), "+
				"or give it its class and reason in matterJSCasesNotRun", id, file, ts.Commit)
		case n > 1:
			t.Errorf("%s: run by the Go-TH leg %v, a controller-role row %v, a not-run row %v — exactly one of them", id, run[file], role, notRun)
		}
	}
	for src := range run {
		if !slices.Contains(ts.Cases, src) {
			t.Errorf("the Go-TH leg runs %s, which matter.js does not have at %s", src, ts.Commit)
		}
	}
	for _, m := range []map[string]gap{controllerRoleCases, matterJSCasesNotRun} {
		for id := range m {
			if !atPin[id] {
				t.Errorf("%s is classified as a matter.js certification case, but matter.js has no such case at %s", id, ts.Commit)
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
	// Media client cases (support file tc-tv-client-support.ts): the DUT
	// sends one command to the tv TH, which answers success.
	"ALOGIN/12.2":          tvClient("TC-ALOGIN-12.2.test.ts:28", "GetSetupPIN, Login and Logout, timed", "content app", "ALOGIN.S.C00.Rsp, C02.Rsp, C03.Rsp"),
	"APPLAUNCHER/3.7.1":    tvClient("TC-APPLAUNCHER-3.7.1.test.ts:17", "LaunchApp", "video player", "APPLAUNCHER.S.C00.Rsp"),
	"APPLAUNCHER/3.8.1":    tvClient("TC-APPLAUNCHER-3.8.1.test.ts:17", "StopApp", "video player", "APPLAUNCHER.S.C01.Rsp"),
	"APPLAUNCHER/3.9.1":    tvClient("TC-APPLAUNCHER-3.9.1.test.ts:17", "HideApp", "video player", "APPLAUNCHER.S.C02.Rsp"),
	"AUDIOOUTPUT/7.3":      tvClient("TC-AUDIOOUTPUT-7.3.test.ts:20", "SelectOutput", "video player", "AUDIOOUTPUT.S.C00.Rsp"),
	"AUDIOOUTPUT/7.4":      tvClient("TC-AUDIOOUTPUT-7.4.test.ts:21", "RenameOutput", "video player", "AUDIOOUTPUT.S.F00, C01.Rsp"),
	"CHANNEL/5.4":          tvClient("TC-CHANNEL-5.4.test.ts:23", "ChangeChannel", "video player", "CHANNEL.S.C00.Rsp"),
	"CHANNEL/5.5":          tvClient("TC-CHANNEL-5.5.test.ts:24", "ChangeChannelByNumber", "video player", "CHANNEL.S.C02.Rsp"),
	"CHANNEL/5.6":          tvClient("TC-CHANNEL-5.6.test.ts:20", "SkipChannel", "video player", "CHANNEL.S.C03.Rsp"),
	"CONTENTLAUNCHER/10.4": tvClient("TC-CONTENTLAUNCHER-10.4.test.ts:24", "LaunchContent", "video player", "CONTENTLAUNCHER.S.F00, C00.Rsp"),
	"CONTENTLAUNCHER/10.6": tvClient("TC-CONTENTLAUNCHER-10.6.test.ts:27", "LaunchURL", "video player", "CONTENTLAUNCHER.S.F01, C01.Rsp"),
	"KEYPADINPUT/3.1":      tvClient("TC-KEYPADINPUT-3.1.test.ts:20", "SendKey", "video player", "KEYPADINPUT.S.C00.Rsp"),
	"LOWPOWER/2.2":         tvClient("TC-LOWPOWER-2.2.test.ts:19", "Sleep", "video player", "LOWPOWER.S.C00.Rsp"),
	"MEDIAINPUT/3.14":      tvClient("TC-MEDIAINPUT-3.14.test.ts:20", "SelectInput", "video player", "MEDIAINPUT.S.C00.Rsp"),
	"MEDIAINPUT/3.15":      tvClient("TC-MEDIAINPUT-3.15.test.ts:17", "ShowInputStatus", "video player", "MEDIAINPUT.S.C01.Rsp"),
	"MEDIAINPUT/3.16":      tvClient("TC-MEDIAINPUT-3.16.test.ts:17", "HideInputStatus", "video player", "MEDIAINPUT.S.C02.Rsp"),
	"MEDIAINPUT/3.17":      tvClient("TC-MEDIAINPUT-3.17.test.ts:21", "RenameInput", "video player", "MEDIAINPUT.S.F00, C03.Rsp"),
	"MEDIAPLAYBACK/6.5":    tvClient("TC-MEDIAPLAYBACK-6.5.test.ts:17", "Play, Pause and Stop", "video player", "MEDIAPLAYBACK.S.C00.Rsp to C02.Rsp"),
	"MEDIAPLAYBACK/6.7":    tvClient("TC-MEDIAPLAYBACK-6.7.test.ts:20", "Play, StartOver, Next, Previous, SkipForward and SkipBackward", "video player", "MEDIAPLAYBACK.S.C00.Rsp, C03.Rsp to C05.Rsp, C08.Rsp, C09.Rsp"),
	"MEDIAPLAYBACK/6.10":   tvClient("TC-MEDIAPLAYBACK-6.10.test.ts:25", "ActivateAudioTrack, ActivateTextTrack and DeactivateTextTrack", "video player", "MEDIAPLAYBACK.S.F02 TextTracks, F03 AudioTracks"),
	"TGTNAV/8.2":           tvClient("TC-TGTNAV-8.2.test.ts:20", "NavigateTarget", "video player", "TGTNAV.S.C00.Rsp"),
	"CHANNEL/5.8": notPorted(classNotSupported, "TC-CHANNEL-5.8.test.ts:24", "needs Channel's ElectronicGuide and RecordProgram "+
		"features (GetProgramGuide, RecordProgram, CancelRecordProgram): the module serves them to a host port that implements "+
		"ChannelGuide and ChannelRecorder (cluster/media/channel.go:42-51), the daemon's video player implements neither "+
		"(CHANNEL.S.F02=0, CHANNEL.S.F03=0)"),

	// Client cases of a device cluster: the DUT invokes the TH's server,
	// read mirrored as TC-IDM-1.1's and TC-LVL-8.1's ports are.
	"OO/3.2": portableNow("TC-OO-3.2.test.ts:60", "the Go TH sends Off, On, Toggle, OffWithEffect, OnWithRecallGlobalScene "+
		"and OnWithTimedOff to a daemon light with the Lighting feature (OO.S.F00, OO.S.C40.Rsp to C42.Rsp)"),
	"I/3.2":      portableNow("TC-I-3.2.test.ts:55", "the Go TH sends Identify and TriggerEffect to a daemon endpoint (I.S.C00.Rsp, I.S.C40.Rsp)"),
	"MOD/2.2":    portableNow("TC-MOD-2.2.test.ts:47", "the Go TH reads SupportedModes and sends ChangeToMode to the daemon's ModeSelect (MOD.S.A0002, MOD.S.C00.Rsp)"),
	"TSTAT/3.2":  portableNow("TC-TSTAT-3.2.test.ts:49", "the Go TH sends SetpointRaiseLower to a daemon thermostat (TSTAT.S.C00.Rsp)"),
	"WNCV/6.1":   portableNow("TC-WNCV-6.1.test.ts:25", "the Go TH sends UpOrOpen, DownOrClose and StopMotion to the daemon's window covering (WNCV.S.C00.Rsp to C02.Rsp)"),
	"WNCV/7.1":   portableNow("TC-WNCV-7.1.test.ts:37", "the Go TH sends GoToLiftPercentage (WNCV.S.C05.Rsp); the GoToTiltPercentage step's mirrored PICS WNCV.S.C08.Rsp is 0, so it is recorded skipped"),
	"DRLK/3.2":   portableNow("TC-DRLK-3.2.test.ts:80", "the Go TH sends the case's door lock commands — door, schedule and user commands — to the daemon's lock (DRLK.S.F04, F08 and every command's .Rsp are 1)"),
	"DGWIFI/3.2": portableNow("TC-DGWIFI-3.2.test.ts:19", "the Go TH sends ResetCounts to the root's WiFiNetworkDiagnostics, which every harness daemon serves with `--wifi` (DGWIFI.S.C00.Rsp)"),
	"CC/3.4":     colorClient("TC-CC-3.4.test.ts:13", "HueSaturation (CC.S.F00)"),
	"CC/4.5":     colorClient("TC-CC-4.5.test.ts:13", "HueSaturation (CC.S.F00)"),
	"CC/5.4":     colorClient("TC-CC-5.4.test.ts:13", "XY (CC.S.F03)"),
	"CC/6.4":     colorClient("TC-CC-6.4.test.ts:37", "ColorTemperature (CC.S.F04)"),
	"CC/7.5":     colorClient("TC-CC-7.5.test.ts:13", "EnhancedHue (CC.S.F01)"),
	"CC/9.4":     colorClient("TC-CC-9.4.test.ts:33", "ColorLoop and EnhancedHue (CC.S.F02, F01)"),
	"DGETH/3.2": notPorted(classNotSupported, "TC-DGETH-3.2.test.ts:19", "needs ResetCounts, which the module's "+
		"EthernetNetworkDiagnostics does not offer, with neither PacketCounts nor ErrorCounts "+
		"(cluster/core/ethernet_network_diagnostics.go:39-42; DGETH.S.C00.Rsp=0): a port would record its only step skipped"),
	"DGSW/3.2": notPorted(classNotSupported, "TC-DGSW-3.2.test.ts:19", "needs ResetWatermarks: the module serves the Watermarks "+
		"feature to a host that supplies a WatermarkReporter (cluster/core/software_diagnostics.go:29-37), the reference daemon "+
		"supplies none (DGSW.S.F00=0, DGSW.S.C00.Rsp=0)"),
	"DGTHREAD/3.4": notPorted(classHarness, "TC-DGTHREAD-3.4.test.ts:27", "needs the daemon's Thread variant (`--otbr` against "+
		"a live OTBR REST API, the DGTHREAD family's composition, run by otbr-virtual.yml), which the Go-TH leg does not start"),
	"CNET/4.20": portableNow("TC-CNET-4.20.test.ts:9", "the Go TH sends ScanNetworks, ArmFailSafe, AddOrUpdateWiFiNetwork, "+
		"ConnectNetwork, ReorderNetwork and RemoveNetwork (tc-cnet-support.ts) to the daemon with `--wifi-sim`, the CNETWIFI row's "+
		"composition (CNET.S.F00=1)"),
	"CNET/4.21": notPorted(classHarness, "TC-CNET-4.21.test.ts:9", "needs a daemon composition with Thread "+
		"NetworkCommissioning (`core.ThreadNetworkCommissioning`, ADR 0027), which the reference daemon does not mount"),

	// Administrator commissioning: three controllers and the TH device
	// (tc-cadmin-support.ts, tc-cadmin-window-support.ts), read mirrored
	// as the TC-CADMIN-1.17 port is.
	"CADMIN/1.1":  cadminClient("TC-CADMIN-1.1.test.ts:9", "administrator behaviour", "an enhanced window (CADMIN.S.C00.Rsp)"),
	"CADMIN/1.2":  cadminClient("TC-CADMIN-1.2.test.ts:9", "administrator behaviour", "a basic window (CADMIN.S.F00, C01.Rsp)"),
	"CADMIN/1.7":  cadminClient("TC-CADMIN-1.7.test.ts:9", "window handling", "an enhanced window (CADMIN.S.C00.Rsp)"),
	"CADMIN/1.8":  cadminClient("TC-CADMIN-1.8.test.ts:9", "window handling", "a basic window (CADMIN.S.F00, C01.Rsp)"),
	"CADMIN/1.18": cadminClient("TC-CADMIN-1.18.test.ts:9", "fabric removal", "a basic window (CADMIN.S.F00, C01.Rsp)"),

	// Commissioner cases: in matter.js's own direction (the Go controller as
	// the DUT, the daemon as the TH device), as the DD and ICD DUT ports run.
	"DD/1.9": portableNow("TC-DD-1.9.test.ts:23", "the Go controller as DUT commissions the daemon from its Manual Pairing "+
		"Code and gives up on a code no device advertises, with the DD DUT machinery of DD/3.15 to 3.17 (gocert_dddut_test.go)"),
	"DD/2.2": portableNow("TC-DD-2.2.test.ts:25", "the Go controller as DUT finds the daemon over DNS-SD, the case's IP leg; "+
		"matter.js runs its BLE and Wi-Fi PAF legs as not applicable (:9-24)"),
	"DD/3.5": portableNow("TC-DD-3.5.test.ts:152", "the Go controller as DUT commissions the daemon; each stage's evidence is a "+
		"daemon log line or a state read, where matter.js reads its TH's log"),
	"DA/1.3": notPorted(classHarness, "TC-DA-1.3.test.ts:101", "needs, with the Go controller as DUT commissioning the daemon "+
		"twice, each AttestationNonce observable: matter.js reads it from its TH's log; the daemon logs none, and the "+
		"controller's nonce reaches only controller/attestation's validation input (validate.go:40-41)"),
	"DA/1.4": daDUT("TC-DA-1.4.test.ts:458", ":279"),
	"DA/1.8": daDUT("TC-DA-1.8.test.ts:200", ":105"),
}

// notPorted is the row of a matter.js case the Go-TH leg does not run; at
// is the TS file and line, why what a port needs.
func notPorted(class gapClass, at, why string) gap {
	return gap{class: class, reason: "matter.js automates it at the snapshot's pin (" + at + "); the Go-TH leg does not run it, " +
		"so the module is unmeasured here — " + why}
}

// portableNow is a case whose clusters and harness support exist: the
// missing port is the harness's gap (class (c)).
func portableNow(at, how string) gap { return notPorted(classHarness, at, "portable now: "+how) }

// tvClient is a media client case against the daemon's `--media` pair.
func tvClient(at, commands, where, pics string) gap {
	return portableNow(at, "the Go TH sends "+commands+" to the daemon's `--media` "+where+" ("+pics+
		"), and the TS file's TH-side check, the command's success, is the daemon's")
}

// colorClient is a ColorControl client case against the daemon's extended
// color light (tc-cc-support.ts: commands sent, attributes read back).
func colorClient(at, feature string) gap {
	return portableNow(at, "the Go TH sends the case's commands of the "+feature+" feature to the daemon's extended color "+
		"light and reads the attributes it names (the light declares every ColorControl feature)")
}

// cadminClient is a multi-controller administrator commissioning case.
func cadminClient(at, what, window string) gap {
	return portableNow(at, "the Go TH plays the case's three controllers ("+what+") against the daemon with "+window+
		", with the multi-controller machinery of the TC-CADMIN-1.17 port (gocert_app_test.go)")
}

// daDUT is a device attestation case whose TH starts from each of chip's
// DAC-provider test vectors; args is the line that passes them.
func daDUT(at, args string) gap {
	return notPorted(classHarness, at, "needs, with the Go controller as DUT, a daemon started from each of chip's "+
		"credentials/development/commissioner_dut vectors with the vector's product id (`--dac_provider`, `--product-id`, "+args+
		"): the daemon reads one attestation chain from files (`--attestation-dir`, examples/reference-bridge/main.go:134) "+
		"and serves a fixed product id (main.go:71, :503)")
}
