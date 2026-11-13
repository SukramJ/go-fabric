// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// The BLE cases leg (milestone 7): connectedhomeip's Python certification
// cases whose TH commissions the DUT over Bluetooth LE onto Wi-Fi inside
// the case (--commissioning-method ble-wifi), run against
// examples/ble-wifi-light under chip's BlueZ and wpa_supplicant mocks in a
// privileged container of the harness image, case by case, each in a
// fresh environment (testdata/blecases_driver.py). The TH is the image's
// BLE-capable Python controller, /opt/matter-ble: the image's own Python
// controller is built without BLE (matter.js support/chip/support/
// install-chip-python, chip_config_network_layer_ble=false,
// chip_enable_ble=false at 862ad75f), so the image carries a second one
// beside it (internal/chiptool/image/Dockerfile). A case that does not
// pass on ours runs once more against chip's all-clusters-app-ble under
// the same mocks, the control: the control red too is the environment
// (the mocks or the TH), not the node.
//
// The PICS are the light's own: gen_pics.py runs first, commissioning the
// light over BLE as the cases do, and its root slice is every case's PICS
// (the cases' clusters live on the root).
//
// GOFABRIC_CHIP_BLECASES=1 runs it; =require — what `make
// chiptool-ble-cases` sets — turns a missing Docker into a failure. An
// image without /opt/matter-ble (every image before the one this leg came
// with) is a skip under both, naming why: the leg cannot fail before its
// image exists. Without the variable it skips.

const bleCasesEnv = "GOFABRIC_CHIP_BLECASES"

// bleCasesProbeMissing is the probe's exit when the image lacks the BLE
// Python controller, the -ble app or bluezoo.
const bleCasesProbeMissing = 3

// bleCase is one Python case of the leg and what it does on ours.
type bleCase struct {
	id     string   // TC-CNET-4.1
	script string   // TC_CNET_4_1.py under /src/python_testing
	args   []string // beyond the commissioning, network and PICS arguments
	want   string   // bleCasePass, bleCaseSkips, bleCaseHarness or bleCaseDefect
	// reason is the evidence of a case that does not pass, as
	// docs/certifiability.md carries it.
	reason string
}

const (
	// bleCasePass: the case runs every step and passes.
	bleCasePass = "pass"
	// bleCaseSkips: (b) not supported — the case runs and skips itself.
	bleCaseSkips = "(b) not supported, skips itself"
	// bleCaseHarness: (c) harness — the case cannot pass under the mocks;
	// the control fails the same way.
	bleCaseHarness = "(c) harness"
	// bleCaseDefect: (a) defect — an open finding names it.
	bleCaseDefect = "(a) defect"
)

// bleCases are the Python cases at the harness pin whose TH commissions
// over BLE-Wi-Fi in the test: the Wi-Fi NetworkCommissioning cases of
// chip's src/python_testing (CNET.S.F00; the Thread cases, CNET.S.F01, and
// the Ethernet one, F02, do not apply to a Wi-Fi node). No other case at
// the pin commissions over BLE: the DD and SC cases that discover over BLE
// are manual YAML cases, and the Python DD cases read NFC or QR payloads.
var bleCases = []bleCase{
	{id: "TC-CNET-1.4", script: "TC_CNET_1_4.py", want: bleCasePass},
	{id: "TC-CNET-4.1", script: "TC_CNET_4_1.py", want: bleCasePass},
	{
		id: "TC-CNET-4.4", script: "TC_CNET_4_4.py", want: bleCaseDefect,
		reason: "step 4's ScanNetworks is answered only after the wpa_supplicant driver's 30 s scan timeout (wifi/wpasupplicant DefaultScanTimeout), past the TH's command timeout: chip's mock ends a scan at once (matter/testing/linux/wifi.py:403-412 at 6170af84); open finding in notes/parity/matter_behaviour_findings.md (suspected: a ScanDone between the Scan call's reply and the arming of the wait is dropped)",
	},
	{id: "TC-CNET-4.9", script: "TC_CNET_4_9.py", want: bleCasePass},
	{
		id: "TC-CNET-4.11", script: "TC_CNET_4_11.py", want: bleCaseHarness,
		reason: "TC-CNET-4.11 moves the DUT to a second access point (PIXIT.CNET.WIFI_2ND_ACCESSPOINT_SSID, TC_CNET_4_11.py:1107-1112) and switches the TH host's Wi-Fi with it; chip's wpa_supplicant mock serves one network (WpaSupplicantMock.__init__, matter/testing/linux/wifi.py:735-736) and the TH host has no Wi-Fi; all-clusters-app-ble fails the same assertion",
	},
	{id: "TC-CNET-4.15", script: "TC_CNET_4_15.py", want: bleCasePass},
	{
		id: "TC-CNET-4.23", script: "TC_CNET_4_23.py", want: bleCaseHarness,
		reason: "step 3 expects ConnectNetwork to a wrong SSID to fail; chip's wpa_supplicant mock ignores the network's arguments (AddNetwork returns its one network) and associates whatever is selected (SelectNetwork, matter/testing/linux/wifi.py:415-440 at 6170af84), so the join succeeds; all-clusters-app-ble fails at the same step",
	},
	{id: "TC-CNET-4.25", script: "TC_CNET_4_25.py", want: bleCaseSkips, reason: "no per-device credentials (NetworkCommissioning PDC)"},
	{id: "TC-CNET-4.26", script: "TC_CNET_4_26.py", want: bleCaseSkips, reason: "no per-device credentials (NetworkCommissioning PDC)"},
	{id: "TC-CNET-4.27", script: "TC_CNET_4_27.py", want: bleCaseSkips, reason: "no per-device credentials (NetworkCommissioning PDC)"},
	{id: "TC-CNET-4.29", script: "TC_CNET_4_29.py", want: bleCaseSkips, reason: "no per-device credentials (NetworkCommissioning PDC)"},
}

func TestChipToolBLECases(t *testing.T) {
	mode := os.Getenv(bleCasesEnv)
	if mode != "1" && mode != "require" {
		t.Skipf("the BLE cases leg runs with %s=1 or =require: make chiptool-ble-cases", bleCasesEnv)
	}
	if _, err := exec.LookPath("docker"); err != nil {
		if mode == "require" {
			t.Fatalf("required leg cannot run: docker not on PATH: %v", err)
		}
		t.Skipf("docker not on PATH: %v", err)
	}
	image, err := pinnedChipImage()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Minute)
	defer cancel()
	if err := exec.CommandContext(ctx, "docker", "image", "inspect", image).Run(); err != nil { //nolint:gosec // the pinned harness image
		if out, err := exec.CommandContext(ctx, "docker", "pull", image).CombinedOutput(); err != nil { //nolint:gosec // the pinned harness image
			t.Fatalf("docker pull %s: %v\n%s", image, err, out)
		}
	}
	probe := fmt.Sprintf("test -x /opt/matter-ble/bin/python3 && test -x /bin/all-clusters-app-ble && command -v bluezoo >/dev/null || exit %d", bleCasesProbeMissing)
	if out, err := exec.CommandContext(ctx, "docker", "run", "--rm", "--entrypoint", "sh", image, "-c", probe).CombinedOutput(); err != nil { //nolint:gosec // fixed argv
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == bleCasesProbeMissing {
			t.Skipf("the harness image %s has no BLE-capable Python controller (/opt/matter-ble), /bin/all-clusters-app-ble or bluezoo; the image built from internal/chiptool/image/Dockerfile since the BLE cases leg has them", image)
		}
		t.Fatalf("probing %s: %v\n%s", image, err, out)
	}

	base := filepath.Join(moduleRoot(), "bin", "chip-blecases")
	if err := os.MkdirAll(base, 0o755); err != nil { //nolint:gosec // shared with the container's root
		t.Fatal(err)
	}
	shared, err := os.MkdirTemp(base, "run-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = exec.Command("docker", "run", "--rm", "-v", shared+":/x", "--entrypoint", "sh", image, "-c", "rm -rf /x/*").Run() //nolint:gosec,noctx // our own scratch dir
		_ = os.RemoveAll(shared)
	})
	if err := os.Chmod(shared, 0o777); err != nil { //nolint:gosec // shared with the container's root
		t.Fatal(err)
	}
	for _, f := range []string{"blecases_driver.py", "gen_pics.py"} {
		data, err := os.ReadFile(filepath.Join("testdata", f)) //nolint:gosec // our own testdata
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(shared, f), data, 0o644); err != nil { //nolint:gosec // read by the container
			t.Fatal(err)
		}
	}
	events, err := deviceTypeEventsJSON()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shared, "device_events.json"), events, 0o644); err != nil { //nolint:gosec // read by the container
		t.Fatal(err)
	}
	example := filepath.Join(shared, "ble-wifi-light")
	build := exec.CommandContext(ctx, "go", "build", "-o", example, ".") //nolint:gosec // fixed argv
	build.Dir = filepath.Join(moduleRoot(), "examples", "ble-wifi-light")
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+imageArch(ctx, t, image))
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build examples/ble-wifi-light: %v\n%s", err, out)
	}

	// dut is the light's argv on a fresh store per run; control chip's app.
	dut := func(dir string) ([]string, string) {
		return []string{example, "--adapter", "hci0", "--wifi-interface", "wlx-app-0", "--storage", filepath.Join(dir, "light.db"), "--log-level", "debug"}, "matter.ble.serving"
	}
	control := func(dir string) ([]string, string) {
		return []string{"/bin/all-clusters-app-ble", "--ble-controller", "0", "--wifi", "--KVS", filepath.Join(dir, "acl.kvs")}, "APP STATUS: Starting event loop"
	}

	// run runs script with args (the commissioning arguments added) in a
	// fresh environment and returns the case's log; ok is false when the
	// app did not come up.
	run := func(t *testing.T, leg, script string, args []string, app func(string) ([]string, string)) (log, appLog string, ready bool) {
		t.Helper()
		out := filepath.Join(shared, leg)
		if err := os.MkdirAll(out, 0o777); err != nil { //nolint:gosec // shared with the container's root
			t.Fatal(err)
		}
		_ = os.Chmod(out, 0o777) //nolint:gosec // shared with the container's root
		name := fmt.Sprintf("gofabric-blecases-%d-%d", os.Getpid(), time.Now().UnixNano())
		t.Cleanup(func() {
			_ = exec.Command("docker", "rm", "-f", name).Run() //nolint:gosec,noctx // our own container
		})
		argv, readyLine := app(out)
		caseArgs := slices.Concat([]string{
			"--commissioning-method", "ble-wifi", "--ble-controller", "1",
			"--discriminator", "3840", "--passcode", "20202021",
			"--wifi-ssid", "MatterAP", "--wifi-passphrase", bleWiFiPassphrase,
			"--paa-trust-store-path", chipPAATrustStore,
			"--storage-path", filepath.Join(out, "admin_storage.json"),
		}, args)
		js, err := json.Marshal(caseArgs)
		if err != nil {
			t.Fatal(err)
		}
		dargs := slices.Concat([]string{
			"run", "--rm", "--privileged", "--name", name,
			"-v", shared + ":" + shared, "-e", "PICS_USER_PROMPT=0", "--entrypoint", "python3", image,
			filepath.Join(shared, "blecases_driver.py"), "--out", out, "--ready", readyLine,
			"--script", script, "--script-args", string(js), "--",
		}, argv)
		cmdOut, err := exec.CommandContext(ctx, "docker", dargs...).CombinedOutput() //nolint:gosec // fixed argv
		if err != nil {
			t.Fatalf("the driver: %v\n%s", err, tail(string(cmdOut), 40))
		}
		data, err := os.ReadFile(filepath.Join(out, "results.json"))
		if err != nil {
			t.Fatalf("no results: %v\n%s", err, tail(string(cmdOut), 40))
		}
		var steps []bleWiFiStep
		if err := json.Unmarshal(data, &steps); err != nil {
			t.Fatal(err)
		}
		for _, s := range steps {
			t.Logf("%s %s: exit %d after %.1f s", leg, s.Step, s.Exit, s.Seconds)
			if s.Step == "app-ready" {
				ready = s.Exit == 0
			}
		}
		read := func(f string) string {
			b, _ := os.ReadFile(filepath.Join(out, f)) //nolint:gosec // our own scratch dir
			return string(b)
		}
		return read("case.log"), read("app.log"), ready
	}

	// The PICS: the light's own root slice, generated over BLE.
	picsDir := filepath.Join(shared, "pics")
	log, appLog, ready := run(t, "pics", filepath.Join(shared, "gen_pics.py"), []string{
		"--string-arg", "out_dir:" + picsDir, "--string-arg", "device_events:" + filepath.Join(shared, "device_events.json"),
	}, dut)
	if !ready {
		t.Fatalf("the light did not come up on the BLE adapter:\n%s", tail(appLog, 30))
	}
	if _, pass := parsePython(log); !pass {
		t.Fatalf("gen_pics.py over BLE-Wi-Fi commissioning did not pass:\n%s", tail(log, 60))
	}
	pics := filepath.Join(picsDir, "ep0.txt")
	if _, err := os.Stat(pics); err != nil {
		t.Fatalf("no root PICS slice: %v", err)
	}

	for _, c := range bleCases {
		t.Run(c.id, func(t *testing.T) {
			args := slices.Concat([]string{"--PICS", pics, "--endpoint", "0"}, c.args)
			script := "/src/python_testing/" + c.script
			log, appLog, ready := run(t, c.script+"-dut", script, args, dut)
			if !ready {
				t.Fatalf("the light did not come up on the BLE adapter:\n%s", tail(appLog, 30))
			}
			counts, pass := parsePython(log)
			t.Logf("ours: pass %v, %d tests (%d skipped), %d step lines (%d skipped)", pass, counts.Tests, counts.TestsSkipped, counts.Steps, counts.StepsSkipped)
			skipped := pass && counts.TestsSkipped > 0
			switch c.want {
			case bleCasePass:
				if !pass || skipped {
					// The control tells ours from the environment.
					clog, _, cready := run(t, c.script+"-control", script, args, control)
					_, cpass := parsePython(clog)
					t.Errorf("%s does not pass on examples/ble-wifi-light (skipped itself: %v; control all-clusters-app-ble: ready %v, pass %v):\n%s\n--- the light ---\n%s",
						c.id, skipped, cready, cpass, tail(log, 60), tail(appLog, 30))
				}
			case bleCaseSkips:
				if !skipped {
					t.Errorf("%s is classified %s (%s) but did not skip itself (pass %v):\n%s", c.id, c.want, c.reason, pass, tail(log, 60))
				}
			case bleCaseHarness:
				if pass {
					t.Errorf("%s passes on examples/ble-wifi-light but is classified %s (%s): reclassify it", c.id, c.want, c.reason)
					return
				}
				clog, _, cready := run(t, c.script+"-control", script, args, control)
				if _, cpass := parsePython(clog); cpass || !cready {
					t.Errorf("%s is classified %s, but the control all-clusters-app-ble (ready %v) passes: the gap is ours (%s)", c.id, c.want, cready, c.reason)
					return
				}
				t.Logf("%s: %s, the control fails as well: %s", c.id, c.want, c.reason)
			case bleCaseDefect:
				if pass {
					t.Errorf("%s passes on examples/ble-wifi-light but is classified %s (%s): close the finding", c.id, c.want, c.reason)
					return
				}
				t.Logf("%s: %s: %s", c.id, c.want, c.reason)
			default:
				t.Fatalf("%s: unknown class %q", c.id, c.want)
			}
		})
	}
}
