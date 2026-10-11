// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package chiptool

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// This file carries no build tag: what the SU cases are handed is fixed
// here and held by TestOtaPlumbing in every `go test ./...`; the
// chiptool-tagged runner (familyrun_test.go prepareOta) only executes it
// in the container.

// The staged OTA image a software-update requestor case serves names the
// daemon's own identity and the next software version: the requestor
// accepts only an image for its vendor and product with a newer version.
// TestOtaPlumbing reads each from the daemon's source.
const (
	// otaVendorID and otaProductID are the reference daemon's CSA test
	// identity (examples/reference-bridge/main.go testVendorID,
	// testProductID).
	otaVendorID  = 0xFFF1
	otaProductID = 0x8001
	// otaSoftwareVersion is the daemon's BasicInformation SoftwareVersion
	// (examples/reference-bridge/wiring.go buildRootClusters) plus one.
	otaSoftwareVersion = 2
)

// What the harness image carries for the SU cases, per matter.js HEAD's
// support/chip/Dockerfile: chip's OTA image tool (:234 copies it to this
// path) and the provider app (:159 builds it), which the runner locates
// with `which`. Whether the pinned image digest already carries both is
// what prepareOta finds out in the container.
const (
	otaImageTool   = "/src/app/ota_image_tool.py"
	otaProviderApp = "chip-ota-provider-app"
)

// otaImageCommand is the argv that builds the staged image, as chip's own
// caller does (scripts/build/build_ota_images.sh:106 at the harness pin:
// `ota_image_tool.py create -v VID -p PID -vn N -vs "N.0" -da sha256
// <payload> <image>`).
func otaImageCommand(payload, image string) []string {
	return []string{
		"python3", otaImageTool, "create",
		"-v", "0x" + strconv.FormatUint(otaVendorID, 16),
		"-p", "0x" + strconv.FormatUint(otaProductID, 16),
		"-vn", strconv.Itoa(otaSoftwareVersion),
		"-vs", strconv.Itoa(otaSoftwareVersion) + ".0",
		"-da", "sha256",
		payload, image,
	}
}

// The --string-arg values of the SU cases that name the provider app and
// the staged image (TC_SU_2_2.py:39-40, TC_SU_2_4.py:39,42,
// TC_SU_2_5.py:40-41, TC_SU_2_7.py:41,44 at the harness pin). Their
// descriptor values are chip CI's environment variables
// (${OTA_PROVIDER_APP}, ${SU_OTA_REQUESTOR_V2}), which name nothing here.
const (
	otaArgProvider = "provider_app_path:"
	otaArgImage    = "ota_image:"
)

// needsOta reports whether a case's descriptor arguments name the OTA
// provider app or a staged image.
func needsOta(args []string) bool {
	return slices.ContainsFunc(args, func(a string) bool {
		return strings.HasPrefix(a, otaArgProvider) || strings.HasPrefix(a, otaArgImage)
	})
}

// otaArgs replaces the provider app and image values of a case's
// arguments with the ones prepared in the container; every other
// argument, ota_provider_port and the provider pipes included, is kept as
// the case's header gives it.
func otaArgs(args []string, provider, image string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		switch {
		case strings.HasPrefix(a, otaArgProvider):
			a = otaArgProvider + provider
		case strings.HasPrefix(a, otaArgImage):
			a = otaArgImage + image
		}
		out[i] = a
	}
	return out
}

// otaCaseMargin is what the runner adds to an SU case's own --timeout so
// the case's own budget, not the runner's kill, ends a slow run.
const otaCaseMargin = 5 * time.Minute

// otaCaseTimeout is the runner budget of an SU case: at least the
// --timeout its descriptor arguments give (TC_SU_2_2.py:42 and
// TC_SU_2_4.py:44 at the harness pin: `--timeout 2100`) plus
// otaCaseMargin. The family default (10 min) cut TC-SU-2.2 off in CI
// families run 38136443914 at 600 s of a case that budgets 2100 s for its
// six steps (two 120 s guards, a 180 s delay and a full download among
// them).
func otaCaseTimeout(args []string, base time.Duration) time.Duration {
	for i, a := range args {
		if a != "--timeout" || i+1 >= len(args) {
			continue
		}
		if s, err := strconv.Atoi(args[i+1]); err == nil {
			if d := time.Duration(s)*time.Second + otaCaseMargin; d > base {
				return d
			}
		}
	}
	return base
}

// otaPayloadSize sizes the staged image so that a download is observable.
// TC-SU-2.2 step 3 waits for UpdateState=Downloading on its subscription
// and terminates the provider at that moment (TC_SU_2_2.py:146-156 and
// :858-870 at the harness pin); with a 44-byte payload the BDX transfer
// was one block, the requestor went Querying → Downloading → Applying →
// Idle within 200 ms and the subscription reported only Idle (run
// 38146455037, SU_2.2-daemon.log 14:34:08.017-.222). One MiB is 1024 BDX
// blocks of 1024 bytes, each its own round trip, which keeps Downloading
// visible for seconds and stays far inside TC-SU-2.4's 360 s download
// budget (TC_SU_2_4.py:69).
const otaPayloadSize = 1 << 20

// otaPayload is the staged image's payload: opaque to the provider and the
// transfer (the image header names the identity and version the requestor
// checks), deterministic so a rebuilt image is byte-identical.
func otaPayload() []byte {
	marker := []byte("go-fabric reference-bridge OTA test payload\n")
	return bytes.Repeat(marker, otaPayloadSize/len(marker)+1)[:otaPayloadSize]
}

// otaProviderKVS is the provider's persistent store every SU case names
// (TC_SU_2_2.py:41 KVS_PATH, TC_SU_2_4.py:107 provider_kvs_path default:
// /tmp/chip_kvs_provider); the cases remove it by prefix in their
// teardown (TC_SUTestBase.py:405-423 clear_kvs).
const otaProviderKVS = "/tmp/chip_kvs_provider"

// otaCleanupCommand stops the provider app an SU case started and removes
// its store, in that order. The case starts the provider as its own child
// (TC_SUTestBase.py start_provider → OTAProviderSubprocess) and stops it
// in its teardown; a case the runner kills never gets there, and killing
// `docker exec` does not reach the processes inside the container (the
// same reason runYamlCase pkills the chip-tool server). The provider left
// behind by TC-SU-2.2 would hold UDP 5541 and the store, which every SU
// case's provider uses (TC_SU_2_2.py:41 and :435, TC_SU_2_4.py:106-107).
//
// The store goes after the process is gone, not before: TC-SU-2.2's own
// teardown removes the store first and terminates the provider second
// (TC_SU_2_2.py:186-190), and chip's Linux store writes its whole content
// back on every commit — a temp file renamed over the path
// (src/platform/Linux/CHIPLinuxStorageIni.cpp:92-105 CommitConfig at the
// harness pin) — so the provider's shutdown recreated the store with the
// fabric TC-SU-2.2 had commissioned it into. TC-SU-2.4's provider then
// came up operational on that fabric and advertised no commissionable
// service (its log in run 38142202247: "Advertise operational node
// 5F228F8CF154C939-0000000012344322"), and the case's CommissionOnNetwork
// on discriminator 321 timed out. The bracket in the pattern keeps pkill
// and pgrep from matching this shell's own command line.
func otaCleanupCommand() []string {
	const pattern = "chip-ota-provider-ap[p]"
	return []string{
		"sh", "-c",
		"pkill -f '" + pattern + "'; " +
			"for i in 1 2 3 4 5 6 7 8 9 10; do pgrep -f '" + pattern + "' >/dev/null || break; sleep 0.5; done; " +
			"rm -rf " + otaProviderKVS + "*",
	}
}

// otaProviderLogsScript lists, one path per line, the provider logs an SU
// case left in the container: the fixed /tmp/provider_*.log a case names
// (TC_SU_2_4.py:108: provider_log_path default
// /tmp/provider_2_4.log) and the per-start logs TC-SU-2.2 writes under its
// Mobly output directory (TC_SU_2_2.py:431: <output_path>/provider_logs).
// class is the case's test class, TC_SU_2_2.
func otaProviderLogsScript(class string) string {
	return "for f in /tmp/provider_*.log /tmp/matter_testing/logs/*/*/" + class + "/*/provider_logs/*.log; " +
		`do [ -f "$f" ] && echo "$f"; done; true`
}

// otaCaseClass is the Python test class of an SU case: family SU, case
// 2.2 → TC_SU_2_2.
func otaCaseClass(family, name string) string {
	return "TC_" + family + "_" + strings.ReplaceAll(name, ".", "_")
}

// otaProviderLogName is the name a collected provider log is saved under
// next to the case's own log: SU/2.4 and /tmp/provider_2_4.log →
// SU/2.4-provider-provider_2_4 (saveCaseLog turns "/" into "_").
func otaProviderLogName(id, path string) string {
	return id + "-provider-" + strings.TrimSuffix(filepath.Base(path), ".log")
}

// TestOtaPlumbing holds the SU plumbing to its sources: the image identity
// to the daemon's, and the argument rewrite to TC_SU_2_2's header.
func TestOtaPlumbing(t *testing.T) {
	root := filepath.Join("..", "..", "examples", "reference-bridge")
	read := func(name string) string {
		data, err := os.ReadFile(filepath.Join(root, name)) //nolint:gosec // a module source file
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	main, wiring := read("main.go"), read("wiring.go")
	for _, c := range []struct {
		src, pattern string
		want         uint64
	}{
		{main, `testVendorID\s+uint16 = 0x([0-9A-Fa-f]+)`, otaVendorID},
		{main, `testProductID\s+uint16 = 0x([0-9A-Fa-f]+)`, otaProductID},
		{wiring, `SoftwareVersion:\s+(\d+),`, otaSoftwareVersion - 1},
	} {
		m := regexp.MustCompile(c.pattern).FindStringSubmatch(c.src)
		if m == nil {
			t.Errorf("examples/reference-bridge: %q not found; the staged OTA image must name the daemon's identity", c.pattern)
			continue
		}
		base := 16
		if strings.Contains(c.pattern, `(\d+)`) {
			base = 10
		}
		if v, _ := strconv.ParseUint(m[1], base, 32); v != c.want {
			t.Errorf("examples/reference-bridge %s is %s, the OTA plumbing assumes %d", c.pattern, m[1], c.want)
		}
	}

	// TC_SU_2_2.py:30-42's script-args, as the descriptor carries them.
	header := []string{
		"--endpoint", "0",
		"--string-arg", "provider_app_path:${OTA_PROVIDER_APP}",
		"--string-arg", "ota_image:${SU_OTA_REQUESTOR_V2}",
		"--int-arg", "ota_provider_port:5541",
		"--timeout", "2100",
	}
	if !needsOta(header) || needsOta([]string{"--int-arg", "ota_provider_port:5541"}) {
		t.Errorf("needsOta misjudges TC_SU_2_2's arguments")
	}
	got := otaArgs(header, "/bin/chip-ota-provider-app", "/run/su.ota")
	want := []string{
		"--endpoint", "0",
		"--string-arg", "provider_app_path:/bin/chip-ota-provider-app",
		"--string-arg", "ota_image:/run/su.ota",
		"--int-arg", "ota_provider_port:5541",
		"--timeout", "2100",
	}
	if !slices.Equal(got, want) {
		t.Errorf("otaArgs = %q, want %q", got, want)
	}
	if got, want := strings.Join(otaImageCommand("p.bin", "i.ota"), " "),
		"python3 /src/app/ota_image_tool.py create -v 0xfff1 -p 0x8001 -vn 2 -vs 2.0 -da sha256 p.bin i.ota"; got != want {
		t.Errorf("otaImageCommand = %q, want %q", got, want)
	}

	if got := otaCaseTimeout(header, 10*time.Minute); got != 2100*time.Second+otaCaseMargin {
		t.Errorf("otaCaseTimeout(TC_SU_2_2) = %v", got)
	}
	if got := otaCaseTimeout([]string{"--endpoint", "0"}, 10*time.Minute); got != 10*time.Minute {
		t.Errorf("otaCaseTimeout without --timeout = %v", got)
	}
	if got := otaCaseTimeout([]string{"--timeout", "60"}, 10*time.Minute); got != 10*time.Minute {
		t.Errorf("otaCaseTimeout lowered the base: %v", got)
	}
	if got := otaCaseClass("SU", "2.2"); got != "TC_SU_2_2" {
		t.Errorf("otaCaseClass = %q", got)
	}
	if got := otaProviderLogName("SU/2.4", "/tmp/provider_2_4.log"); got != "SU/2.4-provider-provider_2_4" {
		t.Errorf("otaProviderLogName = %q", got)
	}
	if s := otaProviderLogsScript("TC_SU_2_2"); !strings.Contains(s, "/tmp/provider_*.log") ||
		!strings.Contains(s, "/TC_SU_2_2/*/provider_logs/*.log") {
		t.Errorf("otaProviderLogsScript = %q", s)
	}
	if got := len(otaPayload()); got != otaPayloadSize || got < 1<<20 {
		t.Errorf("otaPayload() is %d bytes, want %d (at least a MiB so a download is observable)", got, otaPayloadSize)
	}
	cleanup := otaCleanupCommand()
	if len(cleanup) != 3 || cleanup[0] != "sh" || cleanup[1] != "-c" ||
		!strings.Contains(cleanup[2], "pkill -f 'chip-ota-provider-ap[p]'") ||
		!strings.Contains(cleanup[2], "pgrep -f 'chip-ota-provider-ap[p]'") ||
		!strings.HasSuffix(cleanup[2], "rm -rf /tmp/chip_kvs_provider*") ||
		strings.Index(cleanup[2], "pkill") > strings.Index(cleanup[2], "rm -rf") {
		t.Errorf("otaCleanupCommand = %q: want the provider stopped and waited for before its store is removed", cleanup)
	}
}
