// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package chiptool

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// No build tag: the coverage check runs in every `go test ./...` and reads
// the connectedhomeip checkout at the harness commit; without one it skips,
// and fails instead under GOFABRIC_CHIP_REQUIRED, as internal/chipdm's
// cross-check does (crosscheck_test.go chipRoot / chipModel).
//
// Why it exists (decision 642, the run-5 handover's second lesson): the
// families' case lists come from the harness image's descriptor
// (/lib/test-descriptor.json), not from chip's file tree, and the image's
// generator skips a case file whose import raises — so a case missing there
// is silently not run. This check holds the golden file's families lists to
// chip's own Python cases at the commit the image was built from.

var (
	// rePyTestMethod is a test method of a Python case file. The generator
	// lists what mobly's get_existing_test_names() returns on an instance
	// of each loaded MatterBaseTest subclass (matter.js
	// support/chip/support/generate-test-descriptor:344-345 at 862ad75f,
	// byte-identical to the image's /bin/generate-test-descriptor); read
	// statically, that is each indented `def test_…` of the file. The static
	// read can only list more than mobly does — a nested function, a method
	// of a class that is no MatterBaseTest, a method no CI run names — never
	// fewer, so a surplus shows as a problem to classify, never as a pass.
	rePyTestMethod = regexp.MustCompile(`(?m)^[ \t]+(?:async[ \t]+)?def[ \t]+(test_[A-Za-z0-9_]+)[ \t]*\(`)
	// reSemanticName is the generator's semantic_name_pattern
	// (generate-test-descriptor:494): group 1 is the family ("parent"),
	// "<2>.<3>[.<4>]" the case name (:495-501).
	reSemanticName = regexp.MustCompile(`^(.*)_(\d+)_(\d+)(?:[_\\-](.*))?`)
)

// chipPyCaseID derives a Python case's family and case name from its test
// method name by the generator's own rule: the method name without "test_"
// and then "TC_" (generate-test-descriptor:401), split by
// semantic_name_pattern (:494-501). ok is false for a name the pattern does
// not match — the descriptor files such a test under no family.
func chipPyCaseID(method string) (fam, name string, ok bool) {
	n := strings.TrimPrefix(strings.TrimPrefix(method, "test_"), "TC_")
	m := reSemanticName.FindStringSubmatch(n)
	if m == nil {
		return "", "", false
	}
	name = m[2] + "." + m[3]
	if m[4] != "" {
		name += "." + m[4]
	}
	return m[1], name, true
}

// chipCoverageRoot is the connectedhomeip checkout: GOFABRIC_CHIP_ROOT, else
// the sibling of the main checkout (found through git's common directory,
// so a linked worktree finds it too), else the sibling of this tree — the
// lookup of internal/chipdm crosscheck_test.go chipRoot.
func chipCoverageRoot() string {
	if root := os.Getenv("GOFABRIC_CHIP_ROOT"); root != "" {
		return root
	}
	repo := filepath.Join("..", "..")
	if out, err := exec.Command("git", "-C", repo, "rev-parse", "--path-format=absolute", "--git-common-dir").Output(); err == nil {
		candidate := filepath.Join(filepath.Dir(strings.TrimSpace(string(out))), "..", "connectedhomeip")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return filepath.Join(repo, "..", "connectedhomeip")
}

// chipPythonCases reads every src/python_testing/*.py at commit (the
// generator globs PYTHON_DIR/*.py, generate-test-descriptor:290) from the
// object store — the checkout's working tree may stand elsewhere or be
// sparse — and returns family → case name → the files defining it.
func chipPythonCases(root, commit string) (map[string]map[string][]string, error) {
	out, err := exec.Command("git", "-C", root, "ls-tree", "--name-only", commit, "src/python_testing/").Output() //nolint:gosec // a fixed git invocation
	if err != nil {
		return nil, fmt.Errorf("git ls-tree %s src/python_testing/ in %s: %w", commit, root, err)
	}
	var files []string
	for _, f := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.HasSuffix(f, ".py") {
			files = append(files, f)
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no src/python_testing/*.py at %s in %s", commit, root)
	}
	var in bytes.Buffer
	for _, f := range files {
		in.WriteString(commit + ":" + f + "\n")
	}
	cmd := exec.Command("git", "-C", root, "cat-file", "--batch") //nolint:gosec // a fixed git invocation
	cmd.Stdin = &in
	raw, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git cat-file --batch in %s: %w", root, err)
	}
	cases := map[string]map[string][]string{}
	r := bufio.NewReader(bytes.NewReader(raw))
	for _, f := range files {
		header, err := r.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", f, err)
		}
		fields := strings.Fields(header)
		if len(fields) != 3 || fields[1] != "blob" {
			return nil, fmt.Errorf("read %s at %s: %q", f, commit, strings.TrimSpace(header))
		}
		size, err := strconv.Atoi(fields[2])
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", f, err)
		}
		body := make([]byte, size+1) // the blob and its trailing newline
		if _, err := io.ReadFull(r, body); err != nil {
			return nil, fmt.Errorf("read %s: %w", f, err)
		}
		for _, m := range rePyTestMethod.FindAllSubmatch(body[:size], -1) {
			fam, name, ok := chipPyCaseID(string(m[1]))
			if !ok {
				continue
			}
			if cases[fam] == nil {
				cases[fam] = map[string][]string{}
			}
			base := path.Base(f)
			if !slices.Contains(cases[fam][name], base) {
				cases[fam][name] = append(cases[fam][name], base)
			}
		}
	}
	return cases, nil
}

// TestChipPythonCasesCovered holds every family of the table to chip's
// Python cases at the harness commit: each case is in the golden file's
// families list for the row (the image's descriptor filed it, so the family
// run reaches it) or in the row's absent map with its class and reason. An
// absent entry that names no Python case at the commit, or a case the
// golden list has, fails too.
func TestChipPythonCasesCovered(t *testing.T) {
	commit := harnessChipCommit(t)
	root := chipCoverageRoot()
	skip := ""
	var chip map[string]map[string][]string
	if _, err := exec.LookPath("git"); err != nil {
		skip = "git is not installed"
	} else if err := exec.Command("git", "-C", root, "cat-file", "-e", commit+"^{commit}").Run(); err != nil { //nolint:gosec // a fixed git invocation
		skip = "no connectedhomeip checkout with the harness commit " + commit + " at " + root +
			" (set GOFABRIC_CHIP_ROOT, or run make chiptool-setup)"
	} else if chip, err = chipPythonCases(root, commit); err != nil {
		t.Fatal(err)
	}
	if skip != "" {
		if os.Getenv("GOFABRIC_CHIP_REQUIRED") != "" {
			t.Fatalf("GOFABRIC_CHIP_REQUIRED is set, but %s", skip)
		}
		t.Skip(skip)
	}

	data, err := os.ReadFile(filepath.Join("testdata", chipCasesGoldenFile))
	if err != nil {
		t.Fatal(err)
	}
	var g docGolden
	if err := json.Unmarshal(data, &g); err != nil {
		t.Fatal(err)
	}
	if problems := checkChipPythonCoverage(chip, g.Families, chipFamilies); len(problems) > 0 {
		t.Errorf("%d problems against chip's Python cases at %s:\n%s", len(problems), commit, strings.Join(problems, "\n"))
	}
}

// checkChipPythonCoverage is the check of TestChipPythonCasesCovered, apart
// so the seeded cases can drive it without a checkout.
func checkChipPythonCoverage(chip map[string]map[string][]string, golden map[string][]string, fams []family) []string {
	var problems []string
	for i := range fams {
		f := &fams[i]
		descFam := f.name
		if f.cases != "" {
			descFam = f.cases
		}
		listed := map[string]bool{}
		for _, n := range golden[f.name] {
			listed[n] = true
		}
		names := make([]string, 0, len(chip[descFam]))
		for n := range chip[descFam] {
			names = append(names, n)
		}
		sort.Strings(names)
		var missing []string
		for _, n := range names {
			if listed[n] {
				continue
			}
			if _, ok := f.absent[n]; ok {
				continue
			}
			missing = append(missing, n+" ("+strings.Join(chip[descFam][n], ", ")+")")
		}
		if len(missing) > 0 {
			problems = append(problems, fmt.Sprintf("%s: %d Python cases of %s at the harness commit are neither in the golden families list nor absent: %s",
				f.name, len(missing), descFam, strings.Join(missing, "; ")))
		}
		absent := make([]string, 0, len(f.absent))
		for n := range f.absent {
			absent = append(absent, n)
		}
		sort.Strings(absent)
		for _, n := range absent {
			a := f.absent[n]
			switch {
			case chip[descFam][n] == nil:
				problems = append(problems, fmt.Sprintf("%s: absent %s names no Python case of %s at the harness commit", f.name, n, descFam))
			case listed[n]:
				problems = append(problems, fmt.Sprintf("%s: absent %s is in the golden families list — the descriptor files it now; drop the absent entry", f.name, n))
			case a.class == "" || a.reason == "":
				problems = append(problems, fmt.Sprintf("%s: absent %s has no class or no reason", f.name, n))
			}
		}
	}
	return problems
}

// TestChipPythonCoverageSeeded drives the check with a seeded chip tree, so
// it is seen to react without a checkout.
func TestChipPythonCoverageSeeded(t *testing.T) {
	chip := map[string]map[string][]string{
		"X": {"1.1": {"TC_X_1_1.py"}, "1.2": {"TC_X_1_2.py"}},
	}
	for _, tc := range []struct {
		name   string
		golden []string
		absent map[string]gap
		want   string
	}{
		{"covered", []string{"1.1", "1.2"}, nil, ""},
		{"absent with a reason", []string{"1.1"}, map[string]gap{"1.2": {class: classHarness, reason: "r"}}, ""},
		{"a case nowhere", []string{"1.1"}, nil, "neither in the golden families list nor absent: 1.2 (TC_X_1_2.py)"},
		{"absent without a reason", []string{"1.1"}, map[string]gap{"1.2": {class: classHarness}}, "absent 1.2 has no class or no reason"},
		{"absent and listed", []string{"1.1", "1.2"}, map[string]gap{"1.2": {class: classHarness, reason: "r"}}, "absent 1.2 is in the golden families list"},
		{"absent and not at the commit", []string{"1.1", "1.2"}, map[string]gap{"1.3": {class: classHarness, reason: "r"}}, "absent 1.3 names no Python case"},
	} {
		got := checkChipPythonCoverage(chip, map[string][]string{"X": tc.golden}, []family{{name: "X", absent: tc.absent}})
		switch {
		case tc.want == "" && len(got) > 0:
			t.Errorf("%s: want no problem, got %q", tc.name, got)
		case tc.want != "" && (len(got) != 1 || !strings.Contains(got[0], tc.want)):
			t.Errorf("%s: want one problem with %q, got %q", tc.name, tc.want, got)
		}
	}
	// A second run of a family (cases) reads that family's Python cases.
	if got := checkChipPythonCoverage(chip, map[string][]string{"XWIFI": {"1.1"}}, []family{{name: "XWIFI", cases: "X"}}); len(got) != 1 || !strings.Contains(got[0], "1.2") {
		t.Errorf("a row with cases: want the missing 1.2 of X, got %q", got)
	}
}

func TestChipPyCaseID(t *testing.T) {
	for _, tc := range []struct{ method, fam, name string }{
		{"test_TC_WEBRTCP_2_12", "WEBRTCP", "2.12"},
		{"test_TC_WebRTCP_2_1", "WebRTCP", "2.1"},
		{"test_TC_IDM_10_7", "IDM", "10.7"},
		{"test_TC_SC_3_6_extra", "SC", "3.6.extra"},
		{"test_TC_FOO_BAR_1_2", "FOO_BAR", "1.2"},
	} {
		fam, name, ok := chipPyCaseID(tc.method)
		if !ok || fam != tc.fam || name != tc.name {
			t.Errorf("chipPyCaseID(%q) = %q, %q, %v; want %q, %q", tc.method, fam, name, ok, tc.fam, tc.name)
		}
	}
	if _, _, ok := chipPyCaseID("test_hello"); ok {
		t.Error("chipPyCaseID(test_hello) matched; the descriptor files it under no family")
	}
}
