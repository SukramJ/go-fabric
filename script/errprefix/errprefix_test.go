// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package errprefix guards the error-text convention of ADR 0026 P11: an error
// built with errors.New or fmt.Errorf from a string literal starts with
// "<prefix>:" where prefix is the name of the package the file belongs to (or
// of its directory), so a message read in a log names the layer it came from.
//
// The test parses every non-test Go file of the module with go/parser (no type
// checking) outside examples/, script/, internal/, notes/ and .claude/.
// The exceptions are rules, not a list of files:
//
//   - a fmt.Errorf whose format has a %w verb wraps; the wrapped error carries
//     its own prefix, so the format is not judged;
//   - text after the prefix may name a sub-area ("node: ota: ...");
//   - package main is exempt (a program's messages are not a library layer's);
//   - generated files ("Code generated ... DO NOT EDIT.") are judged too and
//     reported apart, so the fix lands in the generator;
//   - the security paths (secure/, commissioning/ and the four cluster/core
//     servers ADR 0013 skips) change only by the owner's review (CLAUDE.md,
//     "Which model does which work"): their violations are counted against
//     a ratchet, [securityRatchet], listed in the log, and a new one fails.
//
// Status texts built by spec.Errorf or im.UnsupportedCommandf are not
// errors.New / fmt.Errorf and are out of scope.
package errprefix

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// minChecked is the floor on judged literals: fewer means the walk or the
// call matching broke and the guard is silently checking nothing.
const minChecked = 300

// securityRatchet is the number of violations the security paths still
// carry (BY-2, listed for the owner). A new one fails; a fixed one fails
// too until the constant is lowered in the same commit.
const securityRatchet = 5

// securityPath reports whether rel is one of the owner-reviewed paths.
func securityPath(rel string) bool {
	rel = filepath.ToSlash(rel)
	if strings.HasPrefix(rel, "secure/") || strings.HasPrefix(rel, "commissioning/") {
		return true
	}
	for _, f := range []string{"cluster/core/access_control", "cluster/core/operational_credentials.go", "cluster/core/group_key_management.go", "cluster/core/general_commissioning.go"} {
		if strings.HasPrefix(rel, f) {
			return true
		}
	}
	return false
}

// skippedTop are the top-level directories the guard does not judge.
var skippedTop = map[string]bool{
	"examples": true,
	"script":   true,
	"internal": true,
	"notes":    true,
	".claude":  true,
	".git":     true,
	"docs":     true,
}

// wrapVerb matches a %w verb, tolerating flags/width between % and w.
var wrapVerb = regexp.MustCompile(`%[-+# 0-9.]*w`)

type violation struct {
	pos     string
	literal string
	want    []string
}

func TestErrorTextsStartWithPackagePrefix(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	var (
		checked   int
		handwrit  []violation
		generated []violation
		security  []violation
	)
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() {
			if rel != "." && (skippedTop[strings.SplitN(rel, string(filepath.Separator), 2)[0]] || d.Name() == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		n, vs, isGenerated, perr := checkFile(path, rel)
		if perr != nil {
			return perr
		}
		checked += n
		switch {
		case isGenerated:
			generated = append(generated, vs...)
		case securityPath(rel):
			security = append(security, vs...)
		default:
			handwrit = append(handwrit, vs...)
		}
		return nil
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}
	if checked < minChecked {
		t.Fatalf("only %d error literals checked, want at least %d: the walk or the call matching is broken", checked, minChecked)
	}
	for _, v := range handwrit {
		t.Errorf("%s: %s should start with one of %q", v.pos, v.literal, v.want)
	}
	for _, v := range generated {
		t.Errorf("GENERATED %s: %s should start with one of %q (fix the generator)", v.pos, v.literal, v.want)
	}
	for _, v := range security {
		t.Logf("security path (owner): %s: %s should start with one of %q", v.pos, v.literal, v.want)
	}
	if len(security) != securityRatchet {
		t.Errorf("%d violations on the security paths, the ratchet is %d: a new one is fixed before it lands, a fixed one lowers securityRatchet", len(security), securityRatchet)
	}
	t.Logf("checked %d error literals", checked)
}

// checkFile judges one file and returns how many literals it checked.
func checkFile(path, rel string) (int, []violation, bool, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return 0, nil, false, fmt.Errorf("errprefix: parse %s: %w", rel, err)
	}
	if f.Name.Name == "main" {
		return 0, nil, false, nil
	}
	generated := false
	for _, cg := range f.Comments {
		if cg.Pos() > f.Package {
			break
		}
		if generatedHeaderLine(cg) {
			generated = true
		}
	}
	prefixes := []string{f.Name.Name + ":"}
	if base := filepath.Base(filepath.Dir(path)); base != f.Name.Name {
		prefixes = append(prefixes, base+":")
	}
	var (
		n  int
		vs []violation
	)
	ast.Inspect(f, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		isNew := pkg.Name == "errors" && sel.Sel.Name == "New"
		isErrorf := pkg.Name == "fmt" && sel.Sel.Name == "Errorf"
		if !isNew && !isErrorf {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		text, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		if isErrorf && wrapVerb.MatchString(text) {
			return true
		}
		n++
		for _, p := range prefixes {
			if strings.HasPrefix(text, p) {
				return true
			}
		}
		vs = append(vs, violation{
			pos:     fmt.Sprintf("%s:%d", rel, fset.Position(lit.Pos()).Line),
			literal: lit.Value,
			want:    prefixes,
		})
		return true
	})
	return n, vs, generated, nil
}

// generatedHeaderLine reports whether any line of the comment group is the
// generated-file marker.
func generatedHeaderLine(cg *ast.CommentGroup) bool {
	for _, c := range cg.List {
		if strings.HasPrefix(c.Text, "// Code generated ") && strings.HasSuffix(c.Text, " DO NOT EDIT.") {
			return true
		}
	}
	return false
}
