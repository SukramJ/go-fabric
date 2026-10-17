// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spectest_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
)

// unhandled is a server that lists a command it has no handler for.
type unhandled struct{ *fake }

func (unhandled) Handles(uint32) bool { return false }

// TestCheckServerReportsAnUnhandledCommand: an accepted command needs an
// implementation (ValidatedElements.ts:341-343 at 0ceb202d); CheckServer
// says so where the server reports it.
func TestCheckServerReportsAnUnhandledCommand(t *testing.T) {
	t.Parallel()
	inst, err := spec.New(serverDef, spec.Options{Features: 1})
	if err != nil {
		t.Fatal(err)
	}
	errs := run(t, func(tb *recorder) { spectest.CheckServer(tb, unhandled{&fake{Instance: inst}}, serverDef, 1) })
	wantReport(t, "unhandled", errs, "has no handler")
}
