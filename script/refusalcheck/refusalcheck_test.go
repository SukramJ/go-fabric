// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package refusalcheck guards how a test asserts that a cluster server
// refuses a write or a command: by the status it answers, not by "some
// error".
//
// A controller reads the status. CONSTRAINT_ERROR, UNSUPPORTED_ACCESS and
// FAILURE are three different answers, matter.js states which one each
// refusal carries, and a test that accepts any error keeps passing when the
// server starts answering the wrong one (review 2026-11, W6-test-17; phase
// 35 moved 87 such assertions to their status, and nothing kept the next
// one out).
//
// # The rule
//
// The test parses every *_test.go under cluster/ and endpoint/ with
// go/parser (no type checking) and counts two shapes, each around a direct
// call of a method named MatterWrite or MatterInvoke:
//
//	if err := srv.MatterWrite(ctx, id, v); err == nil { … }       // no else
//
//	_, err := srv.MatterInvoke(ctx, id, req)
//	if err == nil { … }                                            // no else
//	// … and err is not read again in the block before it is assigned anew
//
// In both the test learns that the call failed and never looks at the
// error. An assertion of the status reads it (errors.As, a status helper,
// errors.Is against a sentinel), which is either an else branch, a
// condition other than "err == nil", or a later use of err — none of which
// is counted.
//
// What the rule does not see: a refusal reached through a helper of the
// test (writeACL(ac, entry)), an error checked with a function
// (require.Error), and a call whose error is discarded. It is a floor under
// the habit, not a proof that every refusal names its status.
//
// # The numbers
//
// On the security paths — the four cluster/core servers the owner's review
// covers (CLAUDE.md, "Which model does which work") — the sites left are
// counted per file in [securityLeft], exactly: AccessControl's tests have
// none and are not in it, so one there fails. Everywhere else the count is
// the exact ratchet [bareRefusalRatchet]. In both a new site fails, and a
// fixed one fails until the number is lowered in the same commit. The goal
// of both is zero; a site is fixed by reading which status matter.js
// answers and asserting that one, not the one the server happens to
// answer today.
package refusalcheck

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// repoRoot is the module root, two levels above this package.
const repoRoot = "../.."

// trees are the directories whose tests are judged.
var trees = []string{"cluster", "endpoint"}

// bareRefusalRatchet is the number of refusals outside the security paths
// that are asserted as "an error" only, measured by the rule of the
// package documentation on 2026-11-13 (review II, W6-test-17: 101 at
// 95e03096, 100 after cluster/alarm's read-only write names
// UNSUPPORTED_WRITE). Exact in both directions.
const bareRefusalRatchet = 100

// securityLeft are the sites left on the security paths, per test file,
// measured on 2026-11-13: 22 at 95e03096, 15 after AccessControl's seven
// name their status (three of the seven had never reached the validation
// they are named after — the write was refused for want of an accessing
// fabric).
var securityLeft = map[string]int{
	"cluster/core/general_commissioning_tc_chip_test.go": 3,
	"cluster/core/group_key_management_failure_test.go":  6,
	"cluster/core/group_key_management_test.go":          2,
	"cluster/core/operational_credentials_test.go":       4,
}

// minCalls is the floor on MatterWrite and MatterInvoke calls seen in the
// judged test files: fewer means the walk or the match broke.
const minCalls = 1000

// securityPath reports whether a test file belongs to one of the four
// cluster/core servers on the security paths.
func securityPath(rel string) bool {
	for _, prefix := range []string{
		"cluster/core/access_control",
		"cluster/core/operational_credentials",
		"cluster/core/group_key_management",
		"cluster/core/general_commissioning",
	} {
		if strings.HasPrefix(rel, prefix) {
			return true
		}
	}
	return false
}

// TestRefusalsNameTheirStatus holds the two numbers.
func TestRefusalsNameTheirStatus(t *testing.T) {
	security := map[string][]string{}
	var other []string
	calls := 0
	fset := token.NewFileSet()
	for _, tree := range trees {
		err := filepath.WalkDir(filepath.Join(repoRoot, tree), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(repoRoot, path)
			rel = filepath.ToSlash(rel)
			n, sites := bareRefusals(fset, f)
			calls += n
			for _, line := range sites {
				site := fmt.Sprintf("%s:%d", rel, line)
				if securityPath(rel) {
					security[rel] = append(security[rel], site)
				} else {
					other = append(other, site)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if calls < minCalls {
		t.Fatalf("only %d MatterWrite/MatterInvoke calls seen in the tests of %v; the walk or the match is broken", calls, trees)
	}
	sort.Strings(other)
	for file, sites := range security {
		switch want := securityLeft[file]; {
		case len(sites) > want:
			t.Errorf("%s: %d refusals on a security path are asserted as \"an error\" only, securityLeft holds %d: a new one — assert the status matter.js answers:\n\t%s",
				file, len(sites), want, strings.Join(sites, "\n\t"))
		case len(sites) < want:
			t.Errorf("%s: %d refusals are asserted as \"an error\" only, securityLeft holds %d: lower it to %d in the same commit", file, len(sites), want, len(sites))
		}
	}
	for file, want := range securityLeft {
		if len(security[file]) == 0 {
			t.Errorf("%s has no refusal asserted as \"an error\" only any more, securityLeft holds %d: delete the entry", file, want)
		}
	}
	switch {
	case len(other) > bareRefusalRatchet:
		t.Errorf("%d refusals are asserted as \"an error\" only, ratchet bareRefusalRatchet = %d: a new one — assert the status matter.js answers:\n\t%s",
			len(other), bareRefusalRatchet, strings.Join(other, "\n\t"))
	case len(other) < bareRefusalRatchet:
		t.Errorf("%d refusals are asserted as \"an error\" only, ratchet bareRefusalRatchet = %d: lower the ratchet to %d in the same commit",
			len(other), bareRefusalRatchet, len(other))
	}
}

// bareRefusals returns how many MatterWrite and MatterInvoke calls the file
// has and the lines of the ones asserted as "an error" only.
func bareRefusals(fset *token.FileSet, f *ast.File) (calls int, lines []int) {
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CallExpr:
			if isServerCall(n) {
				calls++
			}
		case *ast.IfStmt:
			if assign, ok := n.Init.(*ast.AssignStmt); ok && n.Else == nil {
				if errName, ok := refusalAssign(assign); ok && isNilCheck(n.Cond, errName) {
					lines = append(lines, fset.Position(n.Pos()).Line)
				}
			}
		case *ast.BlockStmt:
			lines = append(lines, bareInBlock(fset, n.List)...)
		case *ast.CaseClause:
			lines = append(lines, bareInBlock(fset, n.Body)...)
		case *ast.CommClause:
			lines = append(lines, bareInBlock(fset, n.Body)...)
		}
		return true
	})
	return calls, lines
}

// bareInBlock finds the two-statement shape in one statement list.
func bareInBlock(fset *token.FileSet, list []ast.Stmt) (lines []int) {
	for i := 0; i+1 < len(list); i++ {
		assign, ok := list[i].(*ast.AssignStmt)
		if !ok {
			continue
		}
		errName, ok := refusalAssign(assign)
		if !ok {
			continue
		}
		check, ok := list[i+1].(*ast.IfStmt)
		if !ok || check.Init != nil || check.Else != nil || !isNilCheck(check.Cond, errName) {
			continue
		}
		if !readLater(list[i+2:], errName) {
			lines = append(lines, fset.Position(assign.Pos()).Line)
		}
	}
	return lines
}

// refusalAssign reports whether the statement assigns the results of one
// MatterWrite or MatterInvoke call, and the name its error lands in.
func refusalAssign(a *ast.AssignStmt) (string, bool) {
	if len(a.Rhs) != 1 || len(a.Lhs) == 0 {
		return "", false
	}
	call, ok := a.Rhs[0].(*ast.CallExpr)
	if !ok || !isServerCall(call) {
		return "", false
	}
	id, ok := a.Lhs[len(a.Lhs)-1].(*ast.Ident)
	if !ok || id.Name == "_" {
		return "", false
	}
	return id.Name, true
}

// isServerCall reports a call of a method named MatterWrite or
// MatterInvoke.
func isServerCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && (sel.Sel.Name == "MatterWrite" || sel.Sel.Name == "MatterInvoke")
}

// isNilCheck reports the condition "name == nil".
func isNilCheck(cond ast.Expr, name string) bool {
	b, ok := cond.(*ast.BinaryExpr)
	if !ok || b.Op != token.EQL {
		return false
	}
	x, okX := b.X.(*ast.Ident)
	y, okY := b.Y.(*ast.Ident)
	return okX && okY && x.Name == name && y.Name == "nil"
}

// readLater reports whether the statements read name before assigning it
// anew: the first statement that mentions it decides.
func readLater(rest []ast.Stmt, name string) bool {
	for _, st := range rest {
		if a, ok := st.(*ast.AssignStmt); ok {
			assigned := false
			for _, l := range a.Lhs {
				if id, ok := l.(*ast.Ident); ok && id.Name == name {
					assigned = true
				}
			}
			if assigned {
				for _, r := range a.Rhs {
					if mentions(r, name) {
						return true
					}
				}
				return false
			}
		}
		if mentions(st, name) {
			return true
		}
	}
	return false
}

// mentions reports whether the node contains the identifier name.
func mentions(n ast.Node, name string) bool {
	found := false
	ast.Inspect(n, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == name {
			found = true
		}
		return !found
	})
	return found
}
