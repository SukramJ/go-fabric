// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestImportsAreWhatTheDocSays holds the package doc's sentence on its
// imports to the code: mdns, clock and the standard library only (review
// II, R2-P6-doc-2157).
func TestImportsAreWhatTheDocSays(t *testing.T) {
	t.Parallel()
	allowed := map[string]bool{
		"thingfabric.dev/go-fabric/mdns":  true,
		"thingfabric.dev/go-fabric/clock": true,
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if first, _, _ := strings.Cut(path, "/"); strings.Contains(first, ".") && !allowed[path] {
				t.Errorf("%s imports %s; doc.go says the package imports mdns, clock and the standard library only", name, path)
			}
		}
	}
}
