// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
)

// TestReadAuthorizedResults_AdoptedPASEKeepsAdminister covers the subscribe
// half of the auth-mode keying. After AddNOC the commissioner's PASE session
// carries fabric N, so the fabricIndex==0 bypass no longer fires and the
// default `[CaseAdminSubject]` ACE does not cover subject node-id 0 — the
// initial ReportData would silently drop AccessControl.ACL. matter.js grants
// Administer off the auth mode instead
// (packages/protocol/src/interaction/FabricAccessControl.ts:189-191).
func TestReadAuthorizedResults_AdoptedPASEKeepsAdminister(t *testing.T) {
	t.Parallel()
	fake := &aclStoreFake{entries: []store.ACLEntry{
		{FabricIndex: 2, Privilege: store.PrivilegeAdminister, AuthMode: store.AuthModeCASE, Subjects: []uint64{0x1122334455667788}},
	}}
	b := newACLTestBridge(t, fake)

	ctx := im.WithFabricFilter(context.Background(), true, 2)
	ctx = im.WithSubject(ctx, 0, nil)
	ctx = im.WithAuthModePASE(ctx)

	got := b.readAuthorizedResults(ctx, b.Dispatcher(), accessControlACLPath())
	if len(got) != 1 {
		t.Fatalf("adopted PASE subscribe read: want 1 result, got %d: %+v", len(got), got)
	}

	// Negative control: the same fabric and node-id 0 over CASE stays denied.
	caseCtx := im.WithFabricFilter(context.Background(), true, 2)
	caseCtx = im.WithSubject(caseCtx, 0, nil)
	if got := b.readAuthorizedResults(caseCtx, b.Dispatcher(), accessControlACLPath()); len(got) != 0 {
		t.Fatalf("CASE node-id 0 subscribe read: want 0 results, got %d: %+v", len(got), got)
	}
}
