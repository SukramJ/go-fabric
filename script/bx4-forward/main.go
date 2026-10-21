// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Command bx4-forward writes the forwarding packages that keep the old
// controller/ import paths of the client machinery compiling for the
// deprecation window (brief BX4, README §API stability).
//
// Brief BX4 moved the client machinery out of controller/ into client/. For
// each pair in [moves] the command loads the new package and writes
// <old>/forward.go: the package doc with its Deprecated: paragraph, then one
// forward per exported identifier of the new package — a type alias for a
// type, a constant for a constant, a variable for a variable and for a
// function. None of the moved functions or types has type parameters; a
// generic one would need a wrapper, and the run fails on it instead of
// guessing. Methods travel with the alias and need nothing.
//
// Run it from the module root:
//
//	go run ./script/bx4-forward
//
// The package's test regenerates every file in memory, fails when a
// committed one is stale, and checks through go/types that every exported
// identifier of a new package has a forward of the same kind and the
// identical type in the old one. The forwards and this command go with the
// window (removable in v0.12.0).
package main

import (
	"bytes"
	"fmt"
	"go/format"
	"go/types"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

const modulePath = "github.com/SukramJ/go-fabric"

// move is one package of brief BX4: the old module-relative directory and
// the new one.
type move struct {
	old string
	new string
}

// moves lists the packages brief BX4 moved.
var moves = []move{
	{"controller/discovery", "client/discovery"},
	{"controller/exchange", "client/exchange"},
	{"controller/handshake", "client/handshake"},
	{"controller/interaction", "client/interaction"},
	{"controller/peer", "client/peer"},
}

// forwardFile is the name of the generated file in each old directory.
const forwardFile = "forward.go"

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run("."); err != nil {
		logger.Error("bx4forward.failed", "err", err)
		os.Exit(1)
	}
}

func run(root string) error {
	files, err := generate(root)
	if err != nil {
		return err
	}
	for _, m := range moves {
		path := filepath.Join(root, m.old, forwardFile)
		if err := os.WriteFile(path, files[m.old], 0o600); err != nil {
			return fmt.Errorf("bx4-forward: %w", err)
		}
	}
	return nil
}

// generate returns the forward file of every move, keyed by the old
// directory.
func generate(root string) (map[string][]byte, error) {
	paths := make([]string, len(moves))
	for i, m := range moves {
		paths[i] = "./" + m.new
	}
	pkgs, err := load(root, paths)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]byte, len(moves))
	for _, m := range moves {
		p, ok := pkgs[modulePath+"/"+m.new]
		if !ok {
			return nil, fmt.Errorf("bx4-forward: %s not loaded", m.new)
		}
		src, err := render(m, p.Types)
		if err != nil {
			return nil, err
		}
		out[m.old] = src
	}
	return out, nil
}

// load type-checks the named packages and keys them by import path.
func load(root string, patterns []string) (map[string]*packages.Package, error) {
	cfg := &packages.Config{Mode: packages.NeedName | packages.NeedTypes, Dir: root}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, fmt.Errorf("bx4-forward: load: %w", err)
	}
	out := make(map[string]*packages.Package, len(pkgs))
	for _, p := range pkgs {
		if len(p.Errors) > 0 {
			return nil, fmt.Errorf("bx4-forward: %s: %w", p.PkgPath, p.Errors[0])
		}
		out[p.PkgPath] = p
	}
	return out, nil
}

// exported returns the exported package-level objects of pkg, by name.
func exported(pkg *types.Package) []types.Object {
	scope := pkg.Scope()
	var objs []types.Object
	for _, name := range scope.Names() {
		if obj := scope.Lookup(name); obj.Exported() {
			objs = append(objs, obj)
		}
	}
	sort.Slice(objs, func(i, j int) bool { return objs[i].Name() < objs[j].Name() })
	return objs
}

func render(m move, pkg *types.Package) ([]byte, error) {
	name := pkg.Name()
	var typs, consts, vars, funcs []string
	for _, obj := range exported(pkg) {
		id := obj.Name()
		line := fmt.Sprintf("%s = %s.%s", id, name, id)
		switch o := obj.(type) {
		case *types.TypeName:
			if n, ok := o.Type().(*types.Named); ok && n.TypeParams().Len() > 0 {
				return nil, fmt.Errorf("bx4-forward: %s.%s has type parameters", m.new, id)
			}
			typs = append(typs, line)
		case *types.Const:
			consts = append(consts, line)
		case *types.Var:
			vars = append(vars, line)
		case *types.Func:
			if sig, ok := o.Type().(*types.Signature); ok && sig.TypeParams().Len() > 0 {
				return nil, fmt.Errorf("bx4-forward: %s.%s has type parameters", m.new, id)
			}
			funcs = append(funcs, line)
		default:
			return nil, fmt.Errorf("bx4-forward: %s.%s: unexpected %T", m.new, id, obj)
		}
	}

	var b bytes.Buffer
	b.WriteString("// SPDX-License-Identifier: Apache-2.0\n// Copyright (C) 2026 SukramJ.\n\n")
	b.WriteString("// Code generated by script/bx4-forward; DO NOT EDIT.\n\n")
	fmt.Fprintf(&b, "// Package %s forwards to %s/%s,\n", name, modulePath, m.new)
	b.WriteString("// where brief BX4 moved the client machinery that the device side and the\n")
	b.WriteString("// controller role share (ADR 0026 principle 12). Every exported identifier\n")
	b.WriteString("// of the new package has its forward here; methods travel with the type\n")
	b.WriteString("// aliases, and a forwarded variable is the same value at init (errors.Is\n// holds).\n//\n")
	fmt.Fprintf(&b, "// Deprecated: moved to %s/%s (brief BX4, v0.10.0); removable in v0.12.0.\n", modulePath, m.new)
	fmt.Fprintf(&b, "package %s\n\nimport %q\n", name, modulePath+"/"+m.new)
	// The reachability marker sits directly above each value block: a test
	// reaches the identifier through its new path, never through the forward
	// (script/reachability whitelists type aliases by itself).
	reason := fmt.Sprintf("// fabric:reachable:reason=\"BX4 forward of %s; tests reach it there\"", m.new)
	block(&b, nil, "type", typs)
	block(&b, []string{reason}, "const", consts)
	block(&b, []string{reason}, "var", vars)
	block(&b, []string{"// Functions forward as variables of the same function type.", reason}, "var", funcs)
	src, err := format.Source(b.Bytes())
	if err != nil {
		return nil, fmt.Errorf("bx4-forward: format %s: %w", m.old, err)
	}
	return src, nil
}

func block(b *bytes.Buffer, comments []string, kw string, lines []string) {
	if len(lines) == 0 {
		return
	}
	b.WriteString("\n")
	for _, c := range comments {
		b.WriteString(c + "\n")
	}
	fmt.Fprintf(b, "%s (\n\t%s\n)\n", kw, strings.Join(lines, "\n\t"))
}
