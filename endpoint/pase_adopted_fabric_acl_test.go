// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
)

// privAdminister5 is the Matter Administer privilege level (§9.10.5.3).
const privAdminister5 uint8 = 5

// TestCheckACL_AdoptedPASESessionKeepsAdminister pins the auth-mode keying
// of the implicit commissioning grant. After AddNOC the daemon adopts the
// PASE session onto the new fabric, so CheckACL sees fabricIndex=N with
// subject node-id 0 — and the default `[CaseAdminSubject]` ACE never covers
// node-id 0. matter.js grants Administer off the session's auth mode
// instead: packages/protocol/src/interaction/FabricAccessControl.ts:189-191.
func TestCheckACL_AdoptedPASESessionKeepsAdminister(t *testing.T) {
	d := NewTopologyDispatcher(nil)
	// The post-AddNOC ACL table: one CASE ACE for a real admin subject.
	d.SetACLLister(fakeACLLister{entries: []store.ACLEntry{
		caseEntryWithSubjects(2, store.PrivilegeAdminister, []uint64{0x1122334455667788}, nil),
	}})

	paseCtx := im.WithAuthModePASE(context.Background())
	if got := d.CheckACL(paseCtx, 2, 0, nil, 0, 0x001F, privAdminister5); !got.IsSuccess() {
		t.Fatalf("CheckACL(adopted PASE, fabric 2) = %v, want Success", got)
	}

	// Negative control: the same fabric and the same node-id 0, but a CASE
	// session. The bypass must not widen to non-PASE sessions.
	if got := d.CheckACL(context.Background(), 2, 0, nil, 0, 0x001F, privAdminister5); got.IsSuccess() {
		t.Fatalf("CheckACL(CASE, node-id 0, fabric 2) = %v, want UnsupportedAccess", got)
	}
}
