// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/onoff"
)

// This file runs cases from the CSA's own certification test suite — the
// `Test_TC_*` YAML files in connectedhomeip — against the reference daemon.
//
// CERTIFICATION IS A NON-GOAL. Nothing here produces, or is a step towards,
// a certification result: these are somebody else's carefully written
// regression cases and this module borrows them as a regression harness. The
// PICS file next to this test (testdata/reference-bridge.pics) is written
// only as far as the chosen cases reference it, and says so; it is not a
// declaration of conformance.
//
// Why a Python runner, when the rest of the suite drives chip-tool directly:
// chip-tool used to carry the YAML cases compiled in and run them as
// `chip-tool tests <name>`. At the pinned chip-cert-bins commit that is gone —
// examples/chip-tool/main.cpp registers no `tests` command set, and
// zzz_generated/chip-tool has no generated test/Commands.h. The remaining
// supported path is scripts/tests/chipyaml/chiptool.py, which parses the YAML
// in Python and drives the SAME chip-tool binary this suite already extracts,
// over its `interactive server` websocket. So the case is still executed by
// the CSA reference commissioner; only the script that sequences the steps is
// Python.

const (
	// chipRootEnv points at a connectedhomeip checkout at the pin the
	// Makefile names. Unset skips: the checkout is a CI-provided input, the
	// way chip-tool itself is.
	chipRootEnv = "GOFABRIC_CHIP_ROOT"

	// chipYamlPythonEnv names the interpreter that has matter-yamltests and
	// matter-idl installed (both from the same checkout, so the runner and
	// the cases cannot drift apart).
	chipYamlPythonEnv = "GOFABRIC_CHIPYAML_PYTHON"
)

// conformanceCase is one borrowed certification case plus the two numbers
// that make a run meaningful.
//
// wantRun / wantSkipped are the point of the table. A case whose steps are
// PICS-gated can pass by executing nothing at all — an over-broad edit to the
// PICS file, or a renamed attribute, turns the case green and silent. Pinning
// both counts means the harness fails when the shape of the run changes, not
// only when a step's value is wrong.
type conformanceCase struct {
	name string

	// onOffEndpoint routes the case at the bridged light rather than at the
	// endpoint the YAML's own config block names (endpoint 1, which on this
	// daemon is the aggregator). The number is discovered from the daemon at
	// run time, so this flag only says "this case needs it".
	onOffEndpoint bool

	wantRun     int
	wantSkipped int

	why string
}

// conformanceCases is the set this module runs. It starts at one on purpose:
// the value of the first case is the harness, and the second costs a table
// row.
//
// Test_TC_OO_2_2 was chosen over the other honest candidates:
//
//   - Test_TC_OO_2_1 reads the OnOff attribute set. Every step past the first
//     is gated on the LT (Lighting) feature or its attributes, and the
//     reference daemon's OnOff server answers FeatureMap 0 with OnOff as its
//     only attribute (examples/reference-bridge/fleet.go, onOffServer.
//     MatterRead / MatterAttributes). Honest PICS reduce that case to a
//     single boolean type check.
//   - Test_TC_DESC_2_1 would cover the Descriptor surface, but every step in
//     it carries `disabled: true` and a `verification:` block for a human to
//     read. There is nothing in it for a machine to run.
//   - Test_TC_OO_2_2 drives Off, On and Toggle and reads the attribute back
//     after each, including the two idempotency cases (On on an already-on
//     device, Off on an already-off one) that the commissioning guard in this
//     package does not cover. Every one of its automated steps maps onto
//     something the daemon implements; only the four manual "operate the
//     device by hand" steps are skipped.
var conformanceCases = []conformanceCase{
	{
		name:          "Test_TC_OO_2_2",
		onOffEndpoint: true,
		// 22 automated steps; the 4 skipped are Step 6a-6d, which are gated
		// on PICS_USER_PROMPT / OO.M.ManuallyControlled — a human physically
		// operating the device, which no daemon can satisfy.
		wantRun:     22,
		wantSkipped: 4,
		why:         "Off/On/Toggle plus the idempotency cases, read back over the operational session",
	},
}

// TestChipToolRunsChipConformanceCases commissions the reference daemon and
// then hands it to the CSA's own YAML runner.
func TestChipToolRunsChipConformanceCases(t *testing.T) {
	chipBin := requireChipTool(t)
	chipRoot := requireChipRoot(t)
	python := requireChipYamlPython(t, chipRoot)

	bridgeBin := requireBridgeBinary(t)
	flags := requireBridgeFlags(t, bridgeBin, "db", "listen")
	br := startBridge(t, bridgeBin, flags)

	ctl := newController(t, chipBin, controllerNodeID)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	// The YAML's first step is DelayCommands/WaitForCommissionee, which waits
	// for a node chip-tool has ALREADY commissioned — it does no pairing of
	// its own. So the fabric is established here, with the same
	// `already-discovered` flow the commissioning guard uses, into the very
	// storage directory the websocket server will be started against.
	out, err := ctl.pair(ctx, t, pairTargetHost, br.info.port, br.info.passcode)
	if err != nil {
		br.dump(t)
		t.Fatalf("commissioning the reference daemon stopped at: %s\n%v", handshakeStage(out), err)
	}
	if !commissioningComplete(out) {
		br.dump(t)
		t.Fatalf("chip-tool exited 0 but reported no completed commissioning; furthest stage: %s\n%s",
			handshakeStage(out), out)
	}

	// Registered before the endpoint is known, so a failure to find it still
	// gives the fabric back. The closure reads the endpoint when it runs.
	var lightEndpoint uint16
	var lightKnown bool
	t.Cleanup(func() {
		teardownCtx, teardownCancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer teardownCancel()
		// Left off explicitly, never by a toggle whose parity depends on
		// where the last case stopped.
		if lightKnown {
			if _, err := ctl.invoke(teardownCtx, t, "onoff", "off", lightEndpoint); err != nil {
				t.Logf("teardown: final off failed: %v", err)
			}
		}
		if _, err := ctl.unpair(teardownCtx, t); err != nil {
			t.Logf("teardown: unpair failed: %v", err)
		}
	})

	lightEndpoint, lightKnown = discoverOnOffEndpoint(ctx, t, ctl)
	if !lightKnown {
		br.dump(t)
		t.Fatal("no OnOff endpoint on the reference daemon — every case below would measure nothing")
	}

	for _, tc := range conformanceCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s", tc.name, tc.why)
			endpoint := uint16(0)
			if tc.onOffEndpoint {
				endpoint = lightEndpoint
			}
			runConformanceCase(ctx, t, br, ctl, chipRoot, python, tc, endpoint)
		})
	}
}

// requireChipRoot resolves the connectedhomeip checkout and refuses one that
// is not at the pin.
//
// The pin is read from the Makefile's CHIP_CERT_BINS_IMAGE line, which is
// already this module's single source for the chip-tool build (see the
// comment above it and both jobs in .github/workflows/chiptool.yml). The
// cases, the cluster definitions the runner parses them against, and the
// commissioner that executes them then all come from one commit — a checkout
// at a different commit can disagree with the binary about what an attribute
// is called, and the resulting failure would read like a daemon defect.
func requireChipRoot(t *testing.T) string {
	t.Helper()

	root := os.Getenv(chipRootEnv)
	if root == "" {
		t.Skipf("%s is not set; the chip YAML cases need a connectedhomeip checkout at the Makefile pin", chipRootEnv)
	}
	yaml := filepath.Join(root, "src", "app", "tests", "suites", "certification")
	if _, err := os.Stat(yaml); err != nil {
		t.Fatalf("%s=%q does not contain %s: %v", chipRootEnv, root, yaml, err)
	}

	pin := makefileChipPin(t)
	head, err := gitHead(root)
	if err != nil {
		// A checkout without git metadata (an archive, a container layer) is
		// usable; it just cannot be verified. Say which of the two happened.
		t.Logf("cannot verify %s against the Makefile pin %s: %v", chipRootEnv, pin, err)
		return root
	}
	if head != pin {
		t.Fatalf("%s=%q is at %s but the Makefile pins chip-cert-bins to %s.\n"+
			"The YAML cases, the cluster definitions and the chip-tool binary must come from one commit.",
			chipRootEnv, root, head, pin)
	}
	return root
}

// reMakefilePin matches the same line .github/workflows/chiptool.yml seds out
// of the Makefile, so the test and the workflow cannot read different pins.
var reMakefilePin = regexp.MustCompile(`(?m)^CHIP_CERT_BINS_IMAGE[^=]*=\s*connectedhomeip/chip-cert-bins:([0-9a-f]{40})`)

// makefileChipPin returns the pinned connectedhomeip commit.
func makefileChipPin(t *testing.T) string {
	t.Helper()
	path := filepath.Join(moduleRoot(), "Makefile")
	data, err := os.ReadFile(path) //nolint:gosec // a fixed path inside this module
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	m := reMakefilePin.FindSubmatch(data)
	if m == nil {
		t.Fatalf("no CHIP_CERT_BINS_IMAGE pin in %s", path)
	}
	return string(m[1])
}

// gitHead returns the commit a checkout is at.
func gitHead(dir string) (string, error) {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// requireChipYamlPython resolves the interpreter and proves it can import the
// runner's packages before anything is commissioned, so a missing dependency
// is a one-line message rather than a stack trace twenty minutes in.
func requireChipYamlPython(t *testing.T, chipRoot string) string {
	t.Helper()

	python := os.Getenv(chipYamlPythonEnv)
	if python == "" {
		python = "python3"
	}
	resolved, err := exec.LookPath(python)
	if err != nil {
		t.Skipf("%s=%q not executable: %v", chipYamlPythonEnv, python, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, resolved, "-c", "import matter.yamltests, matter.idl, click, diskcache") //nolint:gosec // resolved by LookPath
	cmd.Env = append(os.Environ(), "PYTHONPATH="+chipYamlPythonPath(chipRoot))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s cannot import the chip YAML runner's packages: %v\n%s\n"+
			"install them from the pinned checkout: pip install %s/scripts/py_matter_idl %s/scripts/py_matter_yamltests",
			resolved, err, out, chipRoot, chipRoot)
	}
	return resolved
}

// chipYamlPythonPath is the PYTHONPATH the runner is invoked with.
//
// Order is load-bearing. testdata/pythonpath comes FIRST so its `chiptest`
// stand-in shadows the SDK's own, which cannot be imported outside a pigweed
// bootstrap; the stub's docstring carries the import chain. <chipRoot>/scripts/
// tests comes second because chiptool.py imports the `chipyaml` package from
// there — and it normally reaches that path through relative_importer, which
// appends it only when `import chiptest` fails. With the stub in place that
// import succeeds, so the path has to be supplied here instead.
func chipYamlPythonPath(chipRoot string) string {
	return filepath.Join(moduleRoot(), "internal", "chiptool", "testdata", "pythonpath") +
		string(os.PathListSeparator) +
		filepath.Join(chipRoot, "scripts", "tests")
}

// moduleRoot returns this module's root directory.
func moduleRoot() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
}

// reRunSummary matches the runner's own end-of-run line. The format string is
// RunnerStrings.stop in scripts/tests/chipyaml/tests_logger.py at the pinned
// commit: 'Run finished in {duration}ms with {runned} steps runned and
// {skipped} steps skipped.' It is printed in both of the runner's log
// formats, so it survives the use_test_harness_log_format that chiptool.py
// turns on.
var reRunSummary = regexp.MustCompile(`Run finished in \d+ms with (\d+) steps runned and (\d+) steps skipped\.`)

// runConformanceCase invokes the chip YAML runner for one case.
func runConformanceCase(
	parent context.Context,
	t *testing.T,
	br *bridgeProcess,
	ctl *controller,
	chipRoot, python string,
	tc conformanceCase,
	endpoint uint16,
) {
	t.Helper()

	ctx, cancel := context.WithTimeout(parent, 10*time.Minute)
	defer cancel()

	// --server_arguments is one string that matter_yamltests splits on spaces
	// (websocket_runner.py, _make_server_startup_command), so a storage path
	// containing one would silently become two arguments and chip-tool would
	// come up on the wrong store. Say so here rather than debugging a
	// WaitForCommissionee that never resolves.
	if strings.ContainsAny(ctl.storageDir, " \t") {
		t.Fatalf("chip-tool storage directory %q contains whitespace; the YAML runner cannot pass it through "+
			"--server_arguments. Set %s to a path without spaces.", ctl.storageDir, kvsBaseEnv)
	}

	args := []string{
		filepath.Join("scripts", "tests", "chipyaml", "chiptool.py"),
		"tests", tc.name,
		"--server_path", ctl.bin,
		// chip-tool reads argv[1] as its command set, so the storage flag
		// goes AFTER `interactive server` — the same rule controller.run
		// follows. matter_yamltests appends `--port <n>` after this string
		// (websocket_runner.py, _make_server_startup_command), which is
		// consistent with that. The storage directory is the one the pairing
		// above wrote the fabric into; without it the websocket server comes
		// up on an empty store and WaitForCommissionee never resolves.
		"--server_arguments", "interactive server --storage-directory " + ctl.storageDir,
		"--PICS", filepath.Join(moduleRoot(), "internal", "chiptool", "testdata", "reference-bridge.pics"),
		// The two config overrides. chiptool.py declares `commands` with
		// nargs=-1 and ignore_unknown_options, and pairs everything after the
		// test name into the parser's config_override (tests_tool.py,
		// send_yaml_command), which is how a YAML `config:` value is
		// replaced from the command line.
		"--nodeId", fmt.Sprintf("0x%X", ctl.nodeID),
	}
	if tc.onOffEndpoint {
		args = append(args, "--endpoint", strconv.FormatUint(uint64(endpoint), 10))
	}

	cmd := exec.CommandContext(ctx, python, args...) //nolint:gosec // python is resolved by LookPath
	// The runner resolves the cluster definition XMLs and the case files by
	// paths relative to the checkout root, so it has to run from there.
	cmd.Dir = chipRoot
	cmd.Env = append(
		os.Environ(),
		"PYTHONPATH="+chipYamlPythonPath(chipRoot),
		"TERM=dumb", "NO_COLOR=1",
	)

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	runErr := cmd.Run()
	out := stripANSI(buf.String())

	if runErr != nil {
		br.dump(t)
		t.Fatalf("%s failed: %v\n--- runner output ---\n%s", tc.name, runErr, out)
	}

	m := reRunSummary.FindStringSubmatch(out)
	if m == nil {
		br.dump(t)
		t.Fatalf("%s exited 0 but printed no run summary; the runner may not have executed the case\n"+
			"--- runner output ---\n%s", tc.name, out)
	}
	got, _ := strconv.Atoi(m[1])
	skipped, _ := strconv.Atoi(m[2])
	if got != tc.wantRun || skipped != tc.wantSkipped {
		t.Fatalf("%s ran %d steps and skipped %d, want %d and %d.\n"+
			"The case or the PICS moved: read the skip lines in the output below and update the table "+
			"only once you know which steps changed and why.\n--- runner output ---\n%s",
			tc.name, got, skipped, tc.wantRun, tc.wantSkipped, out)
	}
	t.Logf("%s: %d steps run, %d skipped", tc.name, got, skipped)
}

// discoverOnOffEndpoint walks the aggregator's PartsList for an endpoint that
// hosts OnOff.
//
// The walk is repeated here rather than shared with the commissioning guard
// on purpose: the two top-level tests each own a daemon with its own database
// and its own endpoint numbering, and a helper that carried state between
// them would make each depend on the other having run first.
func discoverOnOffEndpoint(ctx context.Context, t *testing.T, ctl *controller) (uint16, bool) {
	t.Helper()

	out, err := ctl.readAttr(ctx, t, "descriptor", "parts-list", aggregatorEndpoint)
	if err != nil {
		t.Errorf("read aggregator PartsList: %v", err)
		return 0, false
	}
	parts := listAfter(out, "PartsList:")
	if len(parts) == 0 {
		t.Errorf("aggregator PartsList is empty — the daemon bridged no devices:\n%s", out)
		return 0, false
	}

	for _, ep := range parts {
		epID := uint16(ep)
		srvOut, err := ctl.readAttr(ctx, t, "descriptor", "server-list", epID)
		if err != nil {
			t.Errorf("read ServerList on endpoint %d: %v", epID, err)
			return 0, false
		}
		for _, id := range listAfter(srvOut, "ServerList:") {
			if id == onoff.ClusterID {
				t.Logf("OnOff (0x%04X) found on endpoint %d", onoff.ClusterID, epID)
				return epID, true
			}
		}
	}
	t.Errorf("no endpoint in %v hosts OnOff", parts)
	return 0, false
}
