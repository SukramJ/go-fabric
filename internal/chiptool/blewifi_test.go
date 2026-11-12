// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// The BLE-Wi-Fi leg (milestone 7, wave 28c): the CSA's controller commissions
// examples/ble-wifi-light over Bluetooth LE onto Wi-Fi, under
// connectedhomeip's own BlueZ mock (bluezoo) and wpa_supplicant mock, in a
// privileged container of the harness image. The orchestration is
// testdata/blewifi_driver.py, which imports chip's matter.testing.linux
// classes from the image unchanged, as chip's runner sets them up for
// --commissioning-method ble-wifi (scripts/tests/chiptest/linux.py:99-116
// at 6170af84). The commissioner is the image's chip-tool-ble, the control
// the image's all-clusters-app-ble under the same mocks — what chip's own
// suite runs (test_definition.py:523-525, :571) — so a red leg is ours
// or the environment's by inspection: the control red too is the
// environment.
//
// `make chiptool-ble-wifi` runs it (GOFABRIC_CHIP_BLEWIFI=1); without the
// variable, or on an image without the -ble binaries or bluezoo, it skips
// naming why.

const (
	bleWiFiEnv        = "GOFABRIC_CHIP_BLEWIFI"
	bleWiFiPassphrase = "MatterAPPassword" //nolint:gosec // G101: the mock's test network, chip's own constant
	// bleWiFiSSIDHex is "MatterAP" as chip-tool prints a NetworkID.
	bleWiFiSSIDHex = "4D61747465724150"
)

// bleWiFiStep is one line of the driver's results.json.
type bleWiFiStep struct {
	Step    string  `json:"step"`
	Exit    int     `json:"exit"`
	Seconds float64 `json:"seconds"`
}

func TestChipToolBLEWiFi(t *testing.T) {
	if os.Getenv(bleWiFiEnv) != "1" {
		t.Skipf("the BLE-Wi-Fi leg runs with %s=1: make chiptool-ble-wifi", bleWiFiEnv)
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skipf("docker not on PATH: %v", err)
	}
	image, err := pinnedChipImage()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	if err := exec.CommandContext(ctx, "docker", "image", "inspect", image).Run(); err != nil { //nolint:gosec // the pinned harness image
		if out, err := exec.CommandContext(ctx, "docker", "pull", image).CombinedOutput(); err != nil { //nolint:gosec // the pinned harness image
			t.Fatalf("docker pull %s: %v\n%s", image, err, out)
		}
	}
	probe := "test -x /bin/chip-tool-ble && test -x /bin/all-clusters-app-ble && command -v bluezoo >/dev/null"
	if out, err := exec.CommandContext(ctx, "docker", "run", "--rm", "--entrypoint", "sh", image, "-c", probe).CombinedOutput(); err != nil { //nolint:gosec // fixed argv
		t.Skipf("the harness image %s has no /bin/chip-tool-ble, /bin/all-clusters-app-ble or bluezoo (built since wave 28c): %v %s", image, err, out)
	}

	// Under the module's bin/, as launchHarness: a snap Docker sees no host /tmp.
	base := filepath.Join(moduleRoot(), "bin", "chip-blewifi")
	if err := os.MkdirAll(base, 0o755); err != nil { //nolint:gosec // shared with the container's root
		t.Fatal(err)
	}
	shared, err := os.MkdirTemp(base, "run-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// The container wrote as root: remove from inside.
		_ = exec.Command("docker", "run", "--rm", "-v", shared+":/x", "--entrypoint", "sh", image, "-c", "rm -rf /x/*").Run() //nolint:gosec,noctx // our own scratch dir
		_ = os.RemoveAll(shared)
	})
	if err := os.Chmod(shared, 0o777); err != nil { //nolint:gosec // shared with the container's root
		t.Fatal(err)
	}
	driver, err := os.ReadFile(filepath.Join("testdata", "blewifi_driver.py"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shared, "driver.py"), driver, 0o644); err != nil { //nolint:gosec // read by the container
		t.Fatal(err)
	}
	example := filepath.Join(shared, "ble-wifi-light")
	build := exec.CommandContext(ctx, "go", "build", "-o", example, ".") //nolint:gosec // fixed argv
	build.Dir = filepath.Join(moduleRoot(), "examples", "ble-wifi-light")
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+imageArch(ctx, t, image))
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build examples/ble-wifi-light: %v\n%s", err, out)
	}

	run := func(t *testing.T, mode, app, ready, endpoint string) (string, map[string]int) {
		t.Helper()
		out := filepath.Join(shared, mode)
		name := fmt.Sprintf("gofabric-blewifi-%s-%d", mode, os.Getpid())
		args := []string{
			"run", "--rm", "--privileged", "--name", name,
			"-v", shared + ":" + shared, "--entrypoint", "python3", image,
			filepath.Join(shared, "driver.py"), "--out", out, "--app", app, "--ready", ready, "--endpoint", endpoint,
		}
		cmdOut, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput() //nolint:gosec // fixed argv
		if ctx.Err() != nil {
			_ = exec.Command("docker", "rm", "-f", name).Run() //nolint:gosec,noctx // our own container
		}
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
		exits := map[string]int{}
		for _, s := range steps {
			exits[s.Step] = s.Exit
			t.Logf("%s: exit %d after %.1f s", s.Step, s.Exit, s.Seconds)
		}
		return out, exits
	}
	readLog := func(t *testing.T, dir, step string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(dir, step+".log"))
		if err != nil {
			t.Fatalf("%s: %v", step, err)
		}
		return string(data)
	}
	// proof holds the operational network: the Wi-Fi feature, the mock's
	// network connected, Success as the last status, and the light switched.
	//
	// connected false relaxes the Networks check to the NetworkID for the
	// control: chip's app reads the current network's ssid property as a
	// quoted string and drops its first and last byte
	// (src/platform/Linux/WpaSupplicantClient.cpp:416-421 at 6170af84), and
	// chip's mock serves it unquoted
	// (matter/testing/linux/wifi.py:335, :706-707), so the app compares
	// "atterA" with "MatterAP" and reports Connected FALSE
	// (NetworkCommissioningWiFiDriver.cpp:301-308) — chip's app under chip's
	// mock, not the leg.
	proof := func(t *testing.T, dir string, exits map[string]int, connected bool) {
		t.Helper()
		if exits["app-ready"] != 0 {
			t.Fatalf("the app did not come up on the BLE adapter:\n%s", tail(readLog(t, dir, "app"), 30))
		}
		if exits["pairing"] != 0 {
			t.Fatalf("chip-tool-ble pairing ble-wifi failed (exit %d):\n%s", exits["pairing"], tail(readLog(t, dir, "pairing"), 40))
		}
		for line := range strings.SplitSeq(readLog(t, dir, "pairing"), "\n") {
			if strings.Contains(line, "commissioning completed with success") || strings.Contains(line, "Commissioning complete for node ID") {
				t.Logf("pairing: %s", strings.TrimSpace(line))
			}
		}
		checks := []bleWiFiCheck{
			{"feature-map", `FeatureMap: 1\b`, "NetworkCommissioning's FeatureMap is Wi-Fi (1)"},
			networksCheck(connected),
			{"last-networking-status", `LastNetworkingStatus: 0\b`, "LastNetworkingStatus is Success"},
			{"on-off", `OnOff: TRUE`, "the toggle switched the light on"},
		}
		for _, c := range checks {
			if exits[c.step] != 0 {
				t.Errorf("%s: exit %d:\n%s", c.step, exits[c.step], tail(readLog(t, dir, c.step), 20))
				continue
			}
			log := readLog(t, dir, c.step)
			m := regexp.MustCompile(c.pattern).FindString(log)
			if m == "" {
				t.Errorf("%s: %s does not hold; its [TOO] lines:\n%s", c.step, c.what, toolLines(log))
				continue
			}
			t.Logf("%s: %q", c.step, strings.Join(strings.Fields(m), " "))
		}
		if exits["toggle"] != 0 {
			t.Errorf("toggle: exit %d", exits["toggle"])
		}
	}

	t.Run("ble-wifi-light", func(t *testing.T) {
		app := "/" + strings.TrimPrefix(example, "/") + " --adapter hci0 --wifi-interface wlx-app-0 --storage " + filepath.Join(shared, "light.db") + " --log-level debug"
		dir, exits := run(t, "dut", app, "matter.ble.serving", "2")
		proof(t, dir, exits, true)
		log := readLog(t, dir, "app")
		if strings.Contains(log, bleWiFiPassphrase) {
			t.Error("the example's log names the Wi-Fi passphrase")
		}
		// The node leaves the radio once CommissioningComplete committed
		// its fail-safe (wave 28a): the stop comes after the command.
		complete := strings.Index(log, "command_name=CommissioningComplete")
		stopped := strings.LastIndex(log, "matter.ble.stopped")
		switch {
		case complete < 0:
			t.Error("the example's log shows no CommissioningComplete")
		case stopped < complete:
			t.Errorf("the example did not leave the radio after CommissioningComplete:\n%s", tail(log, 30))
		}
		if !strings.Contains(log, "wpasupplicant: connected") {
			t.Error("the example's log shows no join over wpa_supplicant")
		}
	})
	t.Run("control: all-clusters-app-ble", func(t *testing.T) {
		app := "/bin/all-clusters-app-ble --ble-controller 0 --wifi --KVS " + filepath.Join(shared, "acl.kvs")
		dir, exits := run(t, "control", app, "APP STATUS: Starting event loop", "1")
		proof(t, dir, exits, false)
	})
}

// imageArch is the image's architecture as GOARCH names it.
func imageArch(ctx context.Context, t *testing.T, image string) string {
	t.Helper()
	out, err := exec.CommandContext(ctx, "docker", "image", "inspect", "--format", "{{.Architecture}}", image).Output() //nolint:gosec // the pinned image
	if err != nil {
		t.Fatalf("docker image inspect %s: %v", image, err)
	}
	return strings.TrimSpace(string(out))
}

// toolLines are chip-tool's [TOO] lines of a log, the attribute it printed.
func toolLines(log string) string {
	var out []string
	for line := range strings.SplitSeq(log, "\n") {
		if strings.Contains(line, "[TOO]") {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// bleWiFiCheck is one read of the operational proof: the step's log must
// match pattern.
type bleWiFiCheck struct{ step, pattern, what string }

// networksCheck is the Networks read: MatterAP and, when connected,
// Connected TRUE.
func networksCheck(connected bool) bleWiFiCheck {
	if connected {
		return bleWiFiCheck{"networks", `(?s)NetworkID: ` + bleWiFiSSIDHex + `.*Connected: TRUE`, "Networks names MatterAP connected"}
	}
	return bleWiFiCheck{"networks", `NetworkID: ` + bleWiFiSSIDHex, "Networks names MatterAP"}
}
