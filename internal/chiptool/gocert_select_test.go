// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package chiptool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

// selectGoCert reads a GOFABRIC_GOCERT value against the Go-TH leg's case
// ids ("IDM/1.1"): a comma-separated list whose entries are a family (the
// id's part before the slash, "IDM") or a case id ("SC/4.2"), each of
// them an exclusion with a leading "-". Without an inclusion every id is
// a candidate; the exclusions are taken out of the candidates. selected
// is sorted; unmatched lists, as written, every entry that names no id of
// ids — an empty entry included — so a typo fails instead of running
// nothing (review II, R2-P5-sup-10).
func selectGoCert(ids []string, spec string) (selected, unmatched []string) {
	matches := func(entry, id string) bool {
		fam, _, _ := strings.Cut(id, "/")
		return entry == fam || entry == id
	}
	var include, exclude []string
	narrowed := false // an inclusion was written, matched or not
	for _, raw := range strings.Split(spec, ",") {
		entry := strings.TrimSpace(raw)
		name, excluded := strings.CutPrefix(entry, "-")
		narrowed = narrowed || (name != "" && !excluded)
		if name == "" || !slices.ContainsFunc(ids, func(id string) bool { return matches(name, id) }) {
			unmatched = append(unmatched, raw)
			continue
		}
		if excluded {
			exclude = append(exclude, name)
		} else {
			include = append(include, name)
		}
	}
	for _, id := range ids {
		in := !narrowed || slices.ContainsFunc(include, func(e string) bool { return matches(e, id) })
		out := slices.ContainsFunc(exclude, func(e string) bool { return matches(e, id) })
		if in && !out {
			selected = append(selected, id)
		}
	}
	sort.Strings(selected)
	return selected, unmatched
}

// TestSelectGoCert holds the reader of GOFABRIC_GOCERT: families, ids,
// exclusions, and an entry that names nothing reported rather than
// selecting nothing.
func TestSelectGoCert(t *testing.T) {
	ids := []string{"CADMIN/1.1", "CADMIN/1.2", "CAMERA-GO/1.1", "DD/1.1", "IDM/1.1", "SC/4.2"}
	for _, tc := range []struct {
		spec, selected string
		unmatched      []string
	}{
		{"IDM", "IDM/1.1", nil},
		{"IDM,SC/4.2", "IDM/1.1 SC/4.2", nil},
		{"CAMERA-GO", "CAMERA-GO/1.1", nil},
		{"-CADMIN,-DD", "CAMERA-GO/1.1 IDM/1.1 SC/4.2", nil},
		{"CADMIN,-CADMIN/1.2", "CADMIN/1.1", nil},
		{"CADMN", "", []string{"CADMN"}},
		{"IDM,-NOPE", "IDM/1.1", []string{"-NOPE"}},
		{"IDM,,SC", "IDM/1.1 SC/4.2", []string{""}},
		{"IDM, ", "IDM/1.1", []string{" "}},
		{"-", "CADMIN/1.1 CADMIN/1.2 CAMERA-GO/1.1 DD/1.1 IDM/1.1 SC/4.2", []string{"-"}},
		{"1.1", "", []string{"1.1"}},
	} {
		selected, unmatched := selectGoCert(ids, tc.spec)
		if got := strings.Join(selected, " "); got != tc.selected {
			t.Errorf("%q: selected %q, want %q", tc.spec, got, tc.selected)
		}
		if !slices.Equal(unmatched, tc.unmatched) {
			t.Errorf("%q: unmatched %q, want %q", tc.spec, unmatched, tc.unmatched)
		}
	}
}

// gocertGroup is one group of chiptool.yml's gocert matrix.
var gocertGroup = regexp.MustCompile(`(?m)^ +- group: (gocert-[a-z-]+)\n +families: "([^"]+)"$`)

// TestWorkflowGoCertGroups holds the Go-TH leg's split in chiptool.yml
// (job gocert) to the golden file's Go-TH cases: every group's entries
// name a family or case of the golden file, and the groups partition its
// cases — each case in exactly one group, so none runs twice and none is
// left out. The suite's job leaves the leg to the gocert job, and the
// required gate reads the gocert job's result.
func TestWorkflowGoCertGroups(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "chiptool.yml"))
	if err != nil {
		t.Fatal(err)
	}
	wf := string(data)
	raw, err := os.ReadFile(filepath.Join("testdata", chipCasesGoldenFile))
	if err != nil {
		t.Fatal(err)
	}
	var g docGolden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(g.GoCert))
	for id := range g.GoCert {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		t.Fatal("the golden file holds no Go-TH case")
	}
	groups := gocertGroup.FindAllStringSubmatch(wf, -1)
	if len(groups) < 2 {
		t.Fatalf("chiptool.yml's gocert matrix has %d groups (want the `- group: gocert-…` / `families: \"…\"` pairs)", len(groups))
	}
	in := map[string][]string{}
	for _, m := range groups {
		selected, unmatched := selectGoCert(ids, m[2])
		if len(unmatched) > 0 {
			t.Errorf("group %s: %s names no Go-TH case or family of the golden file", m[1], strings.Join(unmatched, ", "))
		}
		if len(selected) == 0 {
			t.Errorf("group %s (%s) selects no case", m[1], m[2])
		}
		for _, id := range selected {
			in[id] = append(in[id], m[1])
		}
	}
	for _, id := range ids {
		if n := len(in[id]); n != 1 {
			t.Errorf("Go-TH case %s runs in %d groups (%s), want exactly 1", id, n, strings.Join(in[id], ", "))
		}
	}
	for _, want := range []string{
		"GOFABRIC_GOCERT: ${{ matrix.families }}",
		"make chiptool-gocert ",
		"make chiptool-test CHIPTOOL_TEST_SKIP_GOCERT=1 ",
	} {
		if !strings.Contains(wf, want) {
			t.Errorf("chiptool.yml lacks %q: the gocert job runs the groups, the suite's job leaves the leg to it", want)
		}
	}
	if !regexp.MustCompile(`(?m)^ {4}needs: \[[^\]]*\bgocert\b[^\]]*\]$`).MatchString(wf) {
		t.Error("no job of chiptool.yml needs gocert: the required gate must read the Go-TH groups' result")
	}
}

// TestWorkflowCodeFilter holds the `code touched` rule of chiptool.yml:
// the suites run unless every changed path is documentation. The Makefile
// (the harness image's digest, the timeouts, the skip lists) and every
// file a package of the module embeds are inputs of the daemon or of the
// harness, and a pull request that changes only one of them must run the
// suites (review II, R2-P5-sup-03). A go.mod is code here; whether a
// release pull request's go.mod changes are the release shape (issue 632)
// is read from the diff line by line, and TestWorkflowFilterVerdict holds
// that class and its boundaries.
func TestWorkflowCodeFilter(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "chiptool.yml"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^ +docs_only='([^']+)'$`).FindStringSubmatch(string(data))
	if m == nil {
		t.Fatal("chiptool.yml's filter step has no docs_only='…' pattern")
	}
	docsOnly, err := regexp.Compile(m[1])
	if err != nil {
		t.Fatalf("docs_only is no pattern this test can read (%v); keep it to the ERE subset RE2 shares", err)
	}
	for _, p := range []string{"docs/index.md", "docs/requirements.txt", "notes/parity/by_design.md", "CHANGELOG.md", "README.md", "mkdocs.yml", ".github/dependabot.yml"} {
		if !docsOnly.MatchString(p) {
			t.Errorf("%s is documentation, but the filter would start the suites for it", p)
		}
	}
	embedded := embeddedFiles(t)
	code := make([]string, 0, 9+len(embedded))
	code = append(code, "Makefile", "go.mod", "go.sum", "bridge/bridge.go", ".github/workflows/chiptool.yml", ".github/workflows/ci.yml",
		"internal/chiptool/image/Dockerfile", "internal/chiptool/testdata/chip-cases.golden.json", "script/nested-release-check.sh")
	code = append(code, embedded...)
	for _, p := range code {
		if docsOnly.MatchString(p) {
			t.Errorf("%s is an input of the daemon or the harness, but the filter counts it as documentation", p)
		}
	}
}

// embeddedFiles lists every file a //go:embed directive of a non-test
// file of the module (nested modules excluded) names, relative to the
// module root.
func embeddedFiles(t *testing.T) []string {
	t.Helper()
	root := filepath.Join("..", "..")
	directive := regexp.MustCompile(`(?m)^//go:embed (.+)$`)
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "bin") {
				return filepath.SkipDir
			}
			if path != root {
				if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path) //nolint:gosec // a file of this module
		if err != nil {
			return err
		}
		for _, m := range directive.FindAllStringSubmatch(string(src), -1) {
			for _, pattern := range strings.Fields(m[1]) {
				matched, err := filepath.Glob(filepath.Join(filepath.Dir(path), pattern))
				if err != nil {
					return err
				}
				for _, f := range matched {
					_ = filepath.WalkDir(f, func(p string, e os.DirEntry, _ error) error {
						if e != nil && !e.IsDir() {
							rel, _ := filepath.Rel(root, p)
							files = append(files, filepath.ToSlash(rel))
						}
						return nil
					})
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 8 {
		t.Fatalf("found %d embedded files; the walk is broken", len(files))
	}
	return files
}
