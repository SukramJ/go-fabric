// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package docscheck

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// limitsPage is the page whose values TestLimitsPage holds to the code.
const limitsPage = "docs/guides/limits.md"

// limitsMinChecked is a floor on the assertions the page yields: a pattern
// or layout change that silently matched nothing would pass otherwise.
const limitsMinChecked = 30

var (
	// limitsCellConst is one `Name = value` code span of a Value cell.
	limitsCellConst = regexp.MustCompile("`([A-Za-z_][A-Za-z0-9_]*) = ([^`]+)`")
	// limitsCellFile is the repository file a Value cell links.
	limitsCellFile = regexp.MustCompile(`\]\(https://github\.com/SukramJ/go-fabric/blob/main/([^)#]+\.go)\)`)
)

// TestLimitsPage: every `Name = value` in the Value column of the limits
// page is a constant or variable declared in the Go file the same cell
// links, with that value as its declaration spells it. A constant that
// changes its value, moves to another file or is renamed fails here until
// the page follows; a row without a checked value fails too. The page is
// read rather than the constants imported because most of them are
// unexported, and because a value checked against the import would not
// notice the page naming the wrong file.
func TestLimitsPage(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot, limitsPage))
	if err != nil {
		t.Fatal(err)
	}
	decls := map[string]map[string]string{}
	checked := 0
	for n, line := range strings.Split(string(raw), "\n") {
		cells := strings.Split(line, " | ")
		if !strings.HasPrefix(line, "| ") || len(cells) < 3 || strings.HasPrefix(line, "| Limit ") || strings.HasPrefix(line, "| ---") {
			continue
		}
		value := cells[1]
		files := limitsCellFile.FindAllStringSubmatch(value, -1)
		consts := limitsCellConst.FindAllStringSubmatch(value, -1)
		if len(files) != 1 || len(consts) == 0 {
			t.Errorf("%s:%d: the Value cell needs one linked .go file and at least one `Name = value`, has %d and %d", limitsPage, n+1, len(files), len(consts))
			continue
		}
		file := files[0][1]
		if decls[file] == nil {
			decls[file] = goDecls(t, file)
		}
		for _, c := range consts {
			got, ok := decls[file][c[1]]
			switch {
			case !ok:
				t.Errorf("%s:%d: %s is not declared in %s", limitsPage, n+1, c[1], file)
			case got != c[2]:
				t.Errorf("%s:%d: the page says %s = %s, %s declares %s", limitsPage, n+1, c[1], c[2], file, got)
			}
			checked++
		}
	}
	if checked < limitsMinChecked {
		t.Fatalf("only %d values checked on %s, want at least %d; the pattern or the page layout is broken", checked, limitsPage, limitsMinChecked)
	}
}

// goDecls maps every package-level constant and variable of the file to
// its value expression as go/types prints it.
func goDecls(t *testing.T, file string) map[string]string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(repoRoot, file), nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	out := map[string]string{}
	for _, d := range f.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || (g.Tok != token.CONST && g.Tok != token.VAR) {
			continue
		}
		for _, s := range g.Specs {
			vs := s.(*ast.ValueSpec)
			for i, name := range vs.Names {
				if i < len(vs.Values) {
					out[name.Name] = types.ExprString(vs.Values[i])
				}
			}
		}
	}
	return out
}
