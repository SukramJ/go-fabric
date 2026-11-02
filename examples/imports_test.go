// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package examples_test holds the guard of the three small examples: they
// teach the facade, so they import nothing below it (W5-ex-02).
package examples_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// module is the module path the examples import from.
const module = "github.com/SukramJ/go-fabric/"

// allowed are the module packages each small example may import: the
// facade (node, device, contract), the controller role and its client
// view, and the generated cluster definitions (cluster/spec/<name>).
var allowed = map[string][]string{
	"quickstart-device":     {"node", "device", "contract", "cluster/spec/*"},
	"host-view":             {"node", "device", "contract", "cluster/spec/*"},
	"quickstart-controller": {"controller", "controller/client", "controller/attestation", "cluster/spec/*"},
}

func permitted(pkg string, list []string) bool {
	return slices.ContainsFunc(list, func(p string) bool {
		if prefix, ok := strings.CutSuffix(p, "/*"); ok {
			return strings.HasPrefix(pkg, prefix+"/") && !strings.Contains(strings.TrimPrefix(pkg, prefix+"/"), "/")
		}
		return pkg == p
	})
}

// TestSmallExamplesImportTheFacadeOnly parses every non-test file of the
// three small examples and refuses a module import outside its list —
// as the embed guide's daemon is held to the facade.
func TestSmallExamplesImportTheFacadeOnly(t *testing.T) {
	t.Parallel()
	for dir, list := range allowed {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil || len(files) == 0 {
			t.Fatalf("%s: no Go files (%v)", dir, err)
		}
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			parsed, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range parsed.Imports {
				path, _ := strconv.Unquote(imp.Path.Value)
				pkg, ok := strings.CutPrefix(path, module)
				if !ok {
					continue
				}
				if !permitted(pkg, list) {
					t.Errorf("%s imports %s, which is below the facade", f, path)
				}
			}
		}
	}
}
