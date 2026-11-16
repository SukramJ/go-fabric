// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// TestKeyedSettingRootsAreForgotten: every settings root the package keys
// by an endpoint's stable key — a string constant that ends in "." — is in
// keyedSettingRoots, the list Aggregator.Forget erases, except the
// nonvolatile root, whose keys forget names attribute by attribute
// (nonVolatileCandidates), and the endpoint-number reservations, which
// Aggregator.erase releases first because they name the forgotten key's
// parts (persisted.release). A root added without joining the list would
// survive a Forget and come back on the next Add of the key (review II,
// R2-P5-port-02).
func TestKeyedSettingRootsAreForgotten(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var roots []string
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				for _, v := range spec.(*ast.ValueSpec).Values {
					lit, ok := v.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					s, err := strconv.Unquote(lit.Value)
					if err == nil && strings.HasSuffix(s, ".") {
						roots = append(roots, s)
					}
				}
			}
		}
	}
	if len(roots) < len(keyedSettingRoots) {
		t.Fatalf("found %d dot-terminated constants %v; want at least the %d keyed roots", len(roots), roots, len(keyedSettingRoots))
	}
	for _, root := range roots {
		if root == nonVolatileKeyRoot || root == endpointReservedKeyRoot {
			continue
		}
		if !slices.Contains(keyedSettingRoots, root) {
			t.Errorf("settings root %q is not in keyedSettingRoots: Forget leaves it", root)
		}
	}
}
