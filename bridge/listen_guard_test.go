// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net"
	"path/filepath"
	"strconv"
	"testing"
)

// TestBridgeTestsBindNoWildcardEphemeralPort is the standing guard of the
// wildcard-port flake class (N5-tflake-4): on macOS a UDP bind on the
// dual-stack wildcard with an OS-assigned port can get a port another
// socket holds on 127.0.0.1, and a datagram to 127.0.0.1:<port> then
// reaches that socket, not the bridge (owned_socket_test.go). No test of
// this package may configure a Listen string literal whose host is a
// wildcard and whose port is 0 — bind "127.0.0.1:0". A Listen value that
// is not a literal is not checked.
func TestBridgeTestsBindNoWildcardEphemeralPort(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 100 {
		t.Fatalf("read %d test files, expected the package's", len(files))
	}
	fset := token.NewFileSet()
	literals := 0
	for _, name := range files {
		f, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			kv, ok := n.(*ast.KeyValueExpr)
			if !ok {
				return true
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok || key.Name != "Listen" {
				return true
			}
			lit, ok := kv.Value.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			v, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			literals++
			host, port, err := net.SplitHostPort(v)
			if err != nil || port != "0" {
				return true
			}
			if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
				t.Errorf("%s: Listen %q binds the wildcard on an OS-assigned port; bind \"127.0.0.1:0\"", fset.Position(lit.Pos()), v)
			}
			return true
		})
	}
	if literals < 50 {
		t.Fatalf("found %d Listen literals, expected the package's", literals)
	}
}
