// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package chiptool

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

// No build tag: the counter runs in every `go test ./...` from the cached
// listing in testdata/chip-prefixes.json; only -update-chip-prefixes needs a
// connectedhomeip checkout.

var updateChipPrefixes = flag.Bool("update-chip-prefixes", false,
	"rewrite testdata/chip-prefixes.json from the connectedhomeip checkout ($GOFABRIC_CHIP_SRC, default ../connectedhomeip) at the harness commit")

// unfamiliedRatchet is the number of chip case prefixes with at least
// minPrefixCases cases at the harness commit that are neither a chipFamilies
// row nor a code in the first column of docs/certifiability.md's "Families
// this module does not run yet" table (TestUnfamiliedPrefixes).
//
// It is an exact ratchet, checked in both directions like the testparity
// ratchets (script/testparity/register_test.go): growth — a new image
// bringing a family nobody has looked at — is raised in the commit that
// says so; a drop is locked in by lowering it, so a family that once ran or
// was classified cannot slide back to unseen.
//
// Phase 34 (2026-11-04) took it from 31 (the roadmap's count, which
// excluded only chipFamilies rows) — 26 with the not-run table counted —
// to 0: the camera, TLS, Joint Fabric, Thread and Messages families got
// rows, and the rest a not-run row with its class.
const unfamiliedRatchet = 0

// minPrefixCases is the case count from which a prefix counts as a family.
const minPrefixCases = 3

// prefixNotFamilies are case prefixes that name no device-side family, with
// the reason.
var prefixNotFamilies = map[string]string{
	// Test_TC_BR_1..5: chip's bridge cases are manual or controller-side
	// (BR_1..BR_4 are "manual" in the harness image's descriptor; TC-BR-4 runs
	// in the Go-TH leg, gocert_br_test.go).
	"BR": "the bridge cases are manual in the harness image, and TC-BR-4 runs in the Go-TH leg (gocert_br_test.go)",
	// Test_TC_MCORE_FS_1_*: the Fabric Synchronization cases, a
	// controller-role pairing of fabric-admin and fabric-bridge apps.
	"MCORE": "a PICS area, not a cluster family: the MCORE_FS cases drive chip's fabric-sync apps, a controller role",
}

// chipPrefixListing is testdata/chip-prefixes.json: every case file prefix
// of the harness commit and its case count.
type chipPrefixListing struct {
	Commit   string         `json:"commit"`
	Source   string         `json:"source"`
	Prefixes map[string]int `json:"prefixes"`
}

const chipPrefixesFile = "chip-prefixes.json"

var (
	// reCaseFile is a certification case file: TC_<P>_….py under
	// src/python_testing, Test_TC_<P>_….yaml under the certification suite.
	reCaseFile = regexp.MustCompile(`^(?:Test_)?TC_([A-Z0-9]+)_(.+)\.(?:py|yaml)$`)
	// reSupportFile is a base or helper module, no case.
	reSupportFile = regexp.MustCompile(`(?:^|_)(?:TestBase|Base|Utils|common)$`)
)

// TestUnfamiliedPrefixes counts the chip case prefixes the status page does
// not account for and holds the count to unfamiliedRatchet.
func TestUnfamiliedPrefixes(t *testing.T) {
	file := filepath.Join("testdata", chipPrefixesFile)
	if *updateChipPrefixes {
		writeChipPrefixes(t, file)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var listing chipPrefixListing
	if err := json.Unmarshal(data, &listing); err != nil {
		t.Fatal(err)
	}
	if listing.Commit != harnessChipCommit(t) {
		t.Fatalf("%s lists commit %s, the Makefile's CHIP_TEST_IMAGE_COMMIT is %s: regenerate with -update-chip-prefixes", file, listing.Commit, harnessChipCommit(t))
	}
	known := map[string]bool{}
	for _, f := range chipFamilies {
		known[f.name] = true
		if f.cases != "" {
			known[f.cases] = true
		}
	}
	for _, code := range notRunFamilies(t) {
		known[code] = true
	}
	var unseen []string
	for p, n := range listing.Prefixes {
		if n < minPrefixCases || known[p] {
			continue
		}
		if _, ok := prefixNotFamilies[p]; ok {
			continue
		}
		unseen = append(unseen, p)
	}
	sort.Strings(unseen)
	got := len(unseen)
	t.Logf("%d chip case prefixes with ≥ %d cases have no family row and no not-run row: %v", got, minPrefixCases, unseen)
	if got > unfamiliedRatchet {
		t.Fatalf("%d unaccounted chip case prefixes, ratchet %d: %v — run them (chipFamilies) or classify them in %s's \"Families this module does not run yet\" table; raise unfamiliedRatchet only for a new image, and say so", got, unfamiliedRatchet, unseen, certDocPath)
	}
	if got < unfamiliedRatchet {
		t.Fatalf("%d unaccounted chip case prefixes, ratchet %d: lock the gain in by lowering unfamiliedRatchet to %d", got, unfamiliedRatchet, got)
	}
}

// harnessChipCommit is the Makefile's CHIP_TEST_IMAGE_COMMIT.
func harnessChipCommit(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^CHIP_TEST_IMAGE_COMMIT\s*\?=\s*([0-9a-f]{40})\s*$`).FindSubmatch(data)
	if m == nil {
		t.Fatal("the Makefile sets no CHIP_TEST_IMAGE_COMMIT")
	}
	return string(m[1])
}

// writeChipPrefixes lists the case files at the harness commit with git
// ls-tree (the working tree of the checkout may be elsewhere) and writes the
// prefix counts.
func writeChipPrefixes(t *testing.T, file string) {
	t.Helper()
	src := os.Getenv("GOFABRIC_CHIP_SRC")
	if src == "" {
		src = filepath.Join("..", "..", "..", "connectedhomeip")
	}
	commit := harnessChipCommit(t)
	out, err := exec.Command("git", "-C", src, "ls-tree", "-r", "--name-only", commit, //nolint:gosec // a fixed git invocation
		"src/python_testing", "src/app/tests/suites/certification").Output()
	if err != nil {
		t.Fatalf("list the case files of %s at %s: %v", src, commit, err)
	}
	listing := chipPrefixListing{
		Commit:   commit,
		Source:   "git ls-tree -r --name-only <commit> src/python_testing src/app/tests/suites/certification; TC_<P>_*.py and Test_TC_<P>_*.yaml, base and helper modules (" + reSupportFile.String() + ") excluded",
		Prefixes: map[string]int{},
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		m := reCaseFile.FindStringSubmatch(filepath.Base(line))
		if len(m) < 3 || reSupportFile.MatchString(m[2]) {
			continue
		}
		dir := filepath.Dir(line)
		py := strings.HasSuffix(m[0], ".py") && dir == "src/python_testing"
		yaml := strings.HasSuffix(m[0], ".yaml") && dir == "src/app/tests/suites/certification"
		if !py && !yaml {
			continue
		}
		listing.Prefixes[m[1]]++
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(listing); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, buf.Bytes(), 0o644); err != nil { //nolint:gosec // a committed fixture
		t.Fatal(err)
	}
	keys := make([]string, 0, len(listing.Prefixes))
	for k := range listing.Prefixes {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	t.Logf("wrote %s: %d prefixes at %s", file, len(keys), commit)
}
