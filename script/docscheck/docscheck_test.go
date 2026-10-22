// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package docscheck holds the guards of the developer documentation that a
// reader on GitHub, on the published site and in `go doc` relies on: the
// programs a page walks through are shown inline and equal the files the
// build compiles, and every in-repository file:line citation resolves.
package docscheck

import (
	"bufio"
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// update rewrites the inline program blocks from their files:
//
//	go test ./script/docscheck -run TestInlinePrograms -update
var update = flag.Bool("update", false, "rewrite the inline program blocks of docs/ from their files")

// repoRoot is the module root, two levels above this package.
const repoRoot = "../.."

// programMarker names the file an inline block shows: the HTML comment is
// invisible on GitHub and on the site, the fenced block after it is the
// file's whole content.
var programMarker = regexp.MustCompile(`^<!-- program: (\S+) -->$`)

// TestInlinePrograms: every page's `<!-- program: path -->` block equals
// the file it names, so the program a page shows is the one the tests
// build. The pages show it inline rather than by a snippet include, which
// GitHub and `go doc` readers never see (gate 2, G2-3).
func TestInlinePrograms(t *testing.T) {
	pages := docPages(t)
	found := 0
	for _, page := range pages {
		raw, err := os.ReadFile(filepath.Join(repoRoot, page))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte("--8<--")) {
			t.Errorf("%s: a snippet include (--8<--) renders only on the site; show the program inline", page)
		}
		lines := strings.Split(string(raw), "\n")
		out := make([]string, 0, len(lines))
		changed := false
		for i := 0; i < len(lines); i++ {
			out = append(out, lines[i])
			m := programMarker.FindStringSubmatch(lines[i])
			if m == nil {
				continue
			}
			found++
			src, err := os.ReadFile(filepath.Join(repoRoot, m[1]))
			if err != nil {
				t.Errorf("%s:%d: %v", page, i+1, err)
				continue
			}
			want := strings.TrimRight(string(src), "\n")
			if i+1 >= len(lines) || !strings.HasPrefix(lines[i+1], "```") {
				t.Errorf("%s:%d: the marker is not followed by a fenced block", page, i+1)
				continue
			}
			end := i + 2
			for end < len(lines) && lines[end] != "```" {
				end++
			}
			if end == len(lines) {
				t.Errorf("%s:%d: the fenced block is not closed", page, i+1)
				continue
			}
			got := strings.Join(lines[i+2:end], "\n")
			if got != want {
				if !*update {
					t.Errorf("%s:%d: the block differs from %s; run go test ./script/docscheck -run TestInlinePrograms -update", page, i+1, m[1])
				}
				changed = true
			}
			out = append(out, lines[i+1], want, "```")
			i = end
		}
		if changed && *update {
			if err := os.WriteFile(filepath.Join(repoRoot, page), []byte(strings.Join(out, "\n")), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if found == 0 {
		t.Fatal("no inline program block found; the marker or the walk is broken")
	}
}

// citation is a path:line or path:first-last reference.
var citation = regexp.MustCompile(`(?:^|[^\w/.-])((?:[\w.-]+/)*[\w.-]+\.(?:go|md|json|yml|yaml|py|sh)):(\d+)(?:-(\d+))?`)

// TestCitationsResolve: every file:line citation in the published docs
// (docs/ but its ADRs and history, which record the tree as it was when
// they were written) and in the package documentation of node, device and
// fabrictest that names a file of this repository resolves to that file
// and to lines within it. A path that names no file here — a matter.js or
// chip source, a consumer's file — is another repository's and is not
// judged — but a path into a directory of this repository, or a bare .go
// name in a Go file, must name a file. A
// citation pinned to a revision ("wiring.go:93-299 at b4ea1f96^", the
// tree a provenance comment describes) is read at that revision and not
// judged here. Regenerate a moved citation from the commit that wrote it
// (the BY-2 report names the script); this test keeps it from pointing
// past the file.
func TestCitationsResolve(t *testing.T) {
	files := docPages(t)
	for _, pkg := range []string{"node", "device", "fabrictest"} {
		matches, err := filepath.Glob(filepath.Join(repoRoot, pkg, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range matches {
			if !strings.HasSuffix(m, "_test.go") {
				rel, _ := filepath.Rel(repoRoot, m)
				files = append(files, filepath.ToSlash(rel))
			}
		}
	}
	checked := 0
	for _, f := range files {
		checked += checkCitations(t, f)
	}
	if checked < 100 {
		t.Fatalf("only %d citations checked; the pattern or the walk is broken", checked)
	}
}

// checkCitations judges the citations of one file and returns how many
// named a file of the repository.
func checkCitations(t *testing.T, file string) int {
	t.Helper()
	fh, err := os.Open(filepath.Join(repoRoot, file))
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	isGo := strings.HasSuffix(file, ".go")
	tops := topDirs(t)
	checked := 0
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if isGo && !strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		for _, idx := range citation.FindAllStringSubmatchIndex(line, -1) {
			m := make([]string, 4)
			for k := range 4 {
				if idx[2*k] >= 0 {
					m[k] = line[idx[2*k]:idx[2*k+1]]
				}
			}
			if strings.HasPrefix(line[idx[1]:], " at ") {
				continue // pinned to a revision
			}
			target, ok := resolve(file, m[1], isGo)
			if !ok {
				first, _, nested := strings.Cut(m[1], "/")
				_, dirErr := os.Stat(filepath.Join(repoRoot, filepath.Dir(m[1])))
				if (nested && tops[first] && dirErr == nil) || (isGo && !nested && strings.HasSuffix(m[1], ".go")) {
					t.Errorf("%s:%d: %s names no file of the repository", file, n, m[1])
				}
				continue
			}
			checked++
			first, _ := strconv.Atoi(m[2])
			last := first
			if m[3] != "" {
				last, _ = strconv.Atoi(m[3])
			}
			count := lineCount(t, target)
			if first < 1 || last < first || last > count {
				t.Errorf("%s:%d: %s:%s cites lines outside %s (%d lines)", file, n, m[1], lineRange(m), target, count)
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return checked
}

func lineRange(m []string) string {
	if m[3] == "" {
		return m[2]
	}
	return m[2] + "-" + m[3]
}

// resolve finds the repository file a citation names: the path from the
// module root, or — in a Go file — from the citing file's directory (a
// bare name is a file of the same package).
func resolve(citing, path string, isGo bool) (string, bool) {
	cands := []string{path}
	if isGo {
		cands = append(cands, filepath.ToSlash(filepath.Join(filepath.Dir(citing), path)))
	}
	for _, c := range cands {
		if st, err := os.Stat(filepath.Join(repoRoot, c)); err == nil && !st.IsDir() {
			return c, true
		}
	}
	return "", false
}

// topDirs are the repository's top-level directories.
func topDirs(t *testing.T) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			out[e.Name()] = true
		}
	}
	return out
}

var lineCounts = map[string]int{}

func lineCount(t *testing.T, path string) int {
	t.Helper()
	if n, ok := lineCounts[path]; ok {
		return n
	}
	raw, err := os.ReadFile(filepath.Join(repoRoot, path))
	if err != nil {
		t.Fatal(err)
	}
	n := bytes.Count(raw, []byte("\n"))
	if len(raw) > 0 && raw[len(raw)-1] != '\n' {
		n++
	}
	lineCounts[path] = n
	return n
}

// docPages returns the published pages: docs/**/*.md but the ADRs and the
// history.
func docPages(t *testing.T) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(filepath.Join(repoRoot, "docs"), func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(repoRoot, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() && (rel == "docs/adr" || rel == "docs/history") {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(p, ".md") {
			out = append(out, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	if len(out) == 0 {
		t.Fatal("no page under docs/")
	}
	return out
}
