// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cluster_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// writeChangedExempt are the types under cluster/ that declare MatterWrite
// and not MatterWriteChanged, each with its reason. OperationalCredentials
// refuses every write, so the dispatcher never asks it for a verdict.
var writeChangedExempt = map[string]string{
	"core.OperationalCredentials": "every write is refused (UNSUPPORTED_WRITE)",
}

// TestEveryMatterWriteSaysWhetherItChanged: every type under cluster/ that
// declares MatterWrite declares MatterWriteChanged beside it
// (contract.ChangeReportingWriter, review II owner decision 78). A type that
// embedded a server with the capability and declared only its own
// MatterWrite would inherit a MatterWriteChanged that applies the write
// without it; one that has neither advances its cluster's DataVersion after
// every successful write, an unchanged one included.
func TestEveryMatterWriteSaysWhetherItChanged(t *testing.T) {
	t.Parallel()
	// A type is keyed by its directory, not its package name: a
	// hand-written package and its generated definition under spec/ share
	// a name (levelcontrol, modeselect, onoff, servicearea), each with a
	// type Server, and must not stand in for each other.
	declared := map[string]map[string]bool{} // "dir.Type" → method names
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 {
				continue
			}
			typ := fn.Recv.List[0].Type
			if star, ok := typ.(*ast.StarExpr); ok {
				typ = star.X
			}
			ident, ok := typ.(*ast.Ident)
			if !ok {
				continue
			}
			key := filepath.ToSlash(filepath.Dir(path)) + "." + ident.Name
			if declared[key] == nil {
				declared[key] = map[string]bool{}
			}
			declared[key][fn.Name.Name] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	writers := 0
	for typ, methods := range declared {
		if !methods["MatterWrite"] {
			continue
		}
		writers++
		_, exempt := writeChangedExempt[typ]
		switch {
		case exempt && methods["MatterWriteChanged"]:
			t.Errorf("%s declares MatterWriteChanged: remove it from writeChangedExempt", typ)
		case !exempt && !methods["MatterWriteChanged"]:
			t.Errorf("%s declares MatterWrite and no MatterWriteChanged (contract.ChangeReportingWriter)", typ)
		}
	}
	for typ := range writeChangedExempt {
		if !declared[typ]["MatterWrite"] {
			t.Errorf("%s is exempt and declares no MatterWrite: remove the entry", typ)
		}
	}
	if writers < 80 {
		t.Fatalf("found %d types declaring MatterWrite under cluster/, expected the kit's servers", writers)
	}
}
