// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package chiptool

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
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
}
