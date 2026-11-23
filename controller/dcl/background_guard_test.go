// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestBackgroundContextsAreNamed (review II, R2-P6-conc-1906,
// R2-P6-conc-1907) is the standing guard of the class "work a Close cannot
// cancel": every context.Background() in controller/dcl,
// controller/softwareupdate and dcl (where the ledger client moved, review
// II, decision 553) is listed here by file and function, with the
// reason it is not a caller's or the service's life. Periodic work runs
// under the context its Close cancels; a new Background joins the list
// with its reason or takes such a context.
func TestBackgroundContextsAreNamed(t *testing.T) {
	t.Parallel()
	allowed := map[string]string{
		"dcl/certstore.go OpenCertificateStore":             "the root of the periodic context Close cancels",
		"dcl/certstore.go Close":                            "Close waits for an update in flight after it cancelled the periodic one",
		"dcl/certstore.go attestationContext":               "attestation.PAAStore (PAA, CDSigner) takes no context; bounded by attestationLookupTimeout, the wait for a running update included",
		"dcl/certstore.go CDSigner":                         "the store write of a fetched CD signer, bounded by the Store",
		"dcl/revocation.go IsRevoked":                       "attestation.Revocations takes no context; bounded by revocationLookupTimeout",
		"ledger/vendorinfo.go NewVendorInfoService":         "the root of the periodic context Close cancels",
		"softwareupdate/manager.go New":                     "the root of the manager's life Close cancels",
		"softwareupdate/announcements.go processQueueEntry": "an announcement, bounded by the node's own timeouts; announcements.close stops the queue",
	}
	seen := map[string]bool{}
	fset := token.NewFileSet()
	for _, dir := range []string{".", "../softwareupdate", "../../dcl"} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		// "ledger" is package dcl at the module root, "dcl" this one.
		pkg := map[string]string{".": "dcl", "../softwareupdate": "softwareupdate", "../../dcl": "ledger"}[dir]
		for _, name := range files {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(fset, name, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range f.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || sel.Sel.Name != "Background" {
						return true
					}
					if id, ok := sel.X.(*ast.Ident); !ok || id.Name != "context" {
						return true
					}
					key := pkg + "/" + filepath.Base(name) + " " + fn.Name.Name
					seen[key] = true
					if _, ok := allowed[key]; !ok {
						t.Errorf("%s: context.Background() in %s is not listed: periodic work runs under the context Close cancels", fset.Position(call.Pos()), fn.Name.Name)
					}
					return true
				})
			}
		}
	}
	for key := range allowed {
		if !seen[key] {
			t.Errorf("%s is listed but has no context.Background() any more: drop it from the list", key)
		}
	}
}
