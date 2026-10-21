// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"go/constant"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"testing"
)

// root is the module root, two levels above this package.
const root = "../.."

// TestForwardFilesAreCurrent regenerates every forward file and fails when a
// committed one differs: a new exported identifier in client/<name> needs
// "go run ./script/bx4-forward".
func TestForwardFilesAreCurrent(t *testing.T) {
	files, err := generate(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range moves {
		got, err := os.ReadFile(filepath.Join(root, m.old, forwardFile))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, files[m.old]) {
			t.Errorf("%s/%s is stale; run go run ./script/bx4-forward", m.old, forwardFile)
		}
	}
}

// TestEveryExportHasItsForward type-checks the old and the new package of
// every move and holds the old one to exactly the new one's exported
// identifiers, each of the same kind and the identical type: a type is an
// alias of the new type, a constant has the same value, a variable or a
// function is a variable of the identical type.
func TestEveryExportHasItsForward(t *testing.T) {
	paths := make([]string, 0, 2*len(moves))
	for _, m := range moves {
		paths = append(paths, "./"+m.old, "./"+m.new)
	}
	pkgs, err := load(root, paths)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range moves {
		oldPkg := pkgs[modulePath+"/"+m.old].Types
		newPkg := pkgs[modulePath+"/"+m.new].Types
		want := exported(newPkg)
		if len(want) == 0 {
			t.Errorf("%s exports nothing", m.new)
		}
		for _, n := range want {
			o := oldPkg.Scope().Lookup(n.Name())
			if o == nil {
				t.Errorf("%s.%s has no forward in %s", m.new, n.Name(), m.old)
				continue
			}
			if !types.Identical(o.Type(), n.Type()) {
				t.Errorf("%s.%s: forward type %s, want %s", m.old, n.Name(), o.Type(), n.Type())
			}
			checkKind(t, m, o, n)
		}
		if got := len(exported(oldPkg)); got != len(want) {
			t.Errorf("%s exports %d identifiers, %s %d", m.old, got, m.new, len(want))
		}
	}
}

func checkKind(t *testing.T, m move, o, n types.Object) {
	t.Helper()
	switch nv := n.(type) {
	case *types.TypeName:
		if ot, ok := o.(*types.TypeName); !ok || !ot.IsAlias() {
			t.Errorf("%s.%s is not a type alias", m.old, n.Name())
		}
	case *types.Const:
		oc, ok := o.(*types.Const)
		if !ok || !constant.Compare(oc.Val(), token.EQL, nv.Val()) {
			t.Errorf("%s.%s is not the constant %s", m.old, n.Name(), nv.Val())
		}
	case *types.Var, *types.Func:
		if _, ok := o.(*types.Var); !ok {
			t.Errorf("%s.%s is not a variable", m.old, n.Name())
		}
	}
}
