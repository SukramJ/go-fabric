// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package docscheck

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The published pages cite this repository by symbol, not by line
// (review II, R2-P6-doc-3001, -3083): a line range drifts with every edit
// above it, and of the 130 ranges the pages carried at dd17ab06, 91 no
// longer started or ended on the construct they named — TestCitationsResolve
// sees only that a range lies inside its file. The symbol form
// "`file.go` (`Name`, …)" drifts only on a rename, and
// TestCitedNamesOccurInTheirFile catches that.

// TestPublishedPagesCiteNoLineNumbers: a published page and an ADR cite a
// file of this repository as "`file.go` (`Name`, …)", never as
// "file.go:N" or "file.go:N-M". A citation pinned to a revision
// ("wiring.go:93-299 at b4ea1f96^") reads that revision's lines and stays
// allowed; a path that names no file here (a matter.js or chip source, a
// consumer's file) is another repository's and is not judged.
func TestPublishedPagesCiteNoLineNumbers(t *testing.T) {
	for _, page := range citingPages(t) {
		raw, err := os.ReadFile(filepath.Join(repoRoot, page))
		if err != nil {
			t.Fatal(err)
		}
		for n, line := range strings.Split(string(raw), "\n") {
			for _, idx := range citation.FindAllStringSubmatchIndex(line, -1) {
				path := line[idx[2]:idx[3]]
				if strings.HasPrefix(line[idx[1]:], " at ") {
					continue // pinned to a revision
				}
				if _, ok := resolve(page, path, false); !ok {
					continue
				}
				t.Errorf("%s:%d: %s cites %s by line; cite it by its symbols, `%s` (`Name`, …)",
					page, n+1, strings.TrimLeft(line[idx[0]:idx[1]], " `(,"), path, path)
			}
		}
	}
}

// symbolCitation is "`path.go` (…)" or "[`path.go`](link) (…)": a Go or
// SQL file of the repository and the parenthesis after it, which may span
// lines and hold one level of nested parentheses.
var symbolCitation = regexp.MustCompile("`((?:[\\w.-]+/)*[\\w.-]+\\.(?:go|sql))`(?:\\]\\([^)]*\\))?[ \\t\\n]*\\(([^()]*(?:\\([^()]*\\)[^()]*)*)\\)")

// citedName is a backticked token that names a Go or SQL symbol: Name,
// Type.Method, pkg.Name, *Type; a call's "()" is dropped first.
var citedName = regexp.MustCompile(`^[*&]?[A-Za-z_][\w]*(?:\.[A-Za-z_]\w*)*$`)

// TestCitedNamesOccurInTheirFile: in "`file.go` (`Name`, …)" on a
// published page or an ADR, every backticked name in the parenthesis
// occurs in that file — the last segment of a dotted name, as a word — and
// a TestX, BenchmarkX or FuzzX is a function of the package's tests. A
// renamed or removed symbol fails here instead of leaving the page citing
// what is gone (R2-P6-doc-3003, -3004). An ADR records the tree as it was
// decided; a name it keeps for that record is cited without the file, or
// under a dated note.
func TestCitedNamesOccurInTheirFile(t *testing.T) {
	checked := 0
	for _, page := range citingPages(t) {
		raw, err := os.ReadFile(filepath.Join(repoRoot, page))
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		for _, m := range symbolCitation.FindAllStringSubmatchIndex(text, -1) {
			path := text[m[2]:m[3]]
			target, ok := resolve(page, path, false)
			if !ok {
				continue
			}
			line := 1 + strings.Count(text[:m[0]], "\n")
			for _, tok := range backticked.FindAllStringSubmatch(text[m[4]:m[5]], -1) {
				name := strings.TrimSuffix(strings.TrimSpace(tok[1]), "()")
				if !citedName.MatchString(name) {
					continue
				}
				name = name[strings.LastIndex(name, ".")+1:]
				name = strings.TrimLeft(name, "*&")
				checked++
				if !nameInFile(t, target, name) {
					t.Errorf("%s:%d: %s names %s, which does not occur in it", page, line, target, tok[1])
				}
			}
		}
	}
	if checked < 300 {
		t.Fatalf("only %d cited names checked; the pattern or the walk is broken", checked)
	}
}

// nameInFile reports whether name occurs as a word in file, or — for a
// test, benchmark or fuzz function — is declared in a _test.go file of the
// file's package directory.
func nameInFile(t *testing.T, file, name string) bool {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, file))
	if err != nil {
		t.Fatal(err)
	}
	if regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`).Match(raw) {
		return true
	}
	for _, prefix := range []string{"Test", "Benchmark", "Fuzz"} {
		if strings.HasPrefix(name, prefix) && len(name) > len(prefix) {
			return testFuncInDir(t, filepath.Dir(file), name)
		}
	}
	return false
}

func testFuncInDir(t *testing.T, dir, name string) bool {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(repoRoot, dir, "*_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	decl := regexp.MustCompile(`(?m)^func ` + regexp.QuoteMeta(name) + `\(`)
	for _, m := range matches {
		raw, err := os.ReadFile(m)
		if err != nil {
			t.Fatal(err)
		}
		if decl.Match(raw) {
			return true
		}
	}
	return false
}

// citingPages are the pages the two rules above hold: the published pages
// (docs/ but the history and the unpublished concept papers mkdocs.yml
// excludes) and the ADRs, which the site publishes too.
func citingPages(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, p := range docPages(t) {
		if strings.HasPrefix(filepath.Base(p), "concept-v") {
			continue
		}
		out = append(out, p)
	}
	adrs, err := filepath.Glob(filepath.Join(repoRoot, "docs", "adr", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range adrs {
		rel, _ := filepath.Rel(repoRoot, a)
		out = append(out, filepath.ToSlash(rel))
	}
	sort.Strings(out)
	return out
}
