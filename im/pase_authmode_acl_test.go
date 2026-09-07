// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package im

import (
	"context"
	"testing"
)

// paseAdoptedACLDispatcher denies every operational request whose
// fabricIndex is non-zero, exactly the way the real
// [endpoint.TopologyDispatcher] answers a subject that matches no ACE:
// after AddNOC the only stored entry is the default
// `[CaseAdminSubject]` ACE, which never covers subject node-id 0.
type paseAdoptedACLDispatcher struct {
	fakeDispatcher
}

func (d *paseAdoptedACLDispatcher) CheckACL(_ context.Context, fabricIndex uint8, _ uint64, _ []uint32, _ uint16, _ uint32, _ uint8) StatusCode {
	if fabricIndex == 0 {
		return StatusSuccess
	}
	return StatusUnsupportedAccess
}

var (
	_ Dispatcher = (*paseAdoptedACLDispatcher)(nil)
	_ ACLChecker = (*paseAdoptedACLDispatcher)(nil)
)

const (
	aclClusterID  uint32 = 0x001F
	aclAttributeI uint32 = 0x0000
)

// paseAdoptedCtx is the context a commissioner's request carries after
// AddNOC: the PASE session has been adopted onto the new fabric
// (operational.Manager.AdoptFabricIndex), so fabricIndex is N and the
// subject is still node-id 0 — but the session's auth mode is PASE.
func paseAdoptedCtx(fabricIndex uint8) context.Context {
	ctx := WithFabricFilter(context.Background(), false, fabricIndex)
	ctx = WithSubject(ctx, 0, nil)
	return WithAuthModePASE(ctx)
}

// caseFabricCtx is the negative control: a CASE session on the same
// fabric with subject node-id 0. It carries no PASE auth mode, so the
// implicit Administer grant must NOT apply.
func caseFabricCtx(fabricIndex uint8) context.Context {
	ctx := WithFabricFilter(context.Background(), false, fabricIndex)
	return WithSubject(ctx, 0, nil)
}

// TestPASEAdoptedSessionKeepsAdministerOnRead pins the matter.js rule that
// the implicit Administer grant is keyed on the session's auth mode, not on
// the fabric: FabricAccessControl.ts:189-191
// (`subjectDesc.authMode === Pase && subjectDesc.isCommissioning`), while
// OperationalCredentialsServer.ts:266-270 associates the new fabric with the
// very same PASE session.
func TestPASEAdoptedSessionKeepsAdministerOnRead(t *testing.T) {
	d := &paseAdoptedACLDispatcher{fakeDispatcher{readVal: AttributeValue{Value: true}, readStat: StatusSuccess}}
	req := ReadRequest{AttributeRequests: []ConcreteAttributePath{
		{Endpoint: 0, HasEndpoint: true, Cluster: aclClusterID, HasCluster: true, Attribute: aclAttributeI, HasAttribute: true},
	}}

	rd := HandleReadRequest(paseAdoptedCtx(2), d, req)
	if len(rd.Reports) != 1 {
		t.Fatalf("reports=%d, want 1", len(rd.Reports))
	}
	if rd.Reports[0].IsStatus {
		t.Fatalf("adopted PASE read denied with status %v, want data", rd.Reports[0].Status.Status)
	}

	// Negative control: same fabric, same node-id 0, but CASE.
	rd = HandleReadRequest(caseFabricCtx(2), d, req)
	if len(rd.Reports) != 1 || !rd.Reports[0].IsStatus || rd.Reports[0].Status.Status != StatusUnsupportedAccess {
		t.Fatalf("CASE node-id 0 read = %+v, want UnsupportedAccess status", rd.Reports)
	}
}

// TestPASEAdoptedSessionKeepsAdministerOnWrite covers the ACL write a
// commissioner sends over the adopted PASE session (AccessControl.ACL).
func TestPASEAdoptedSessionKeepsAdministerOnWrite(t *testing.T) {
	d := &paseAdoptedACLDispatcher{fakeDispatcher{writeStat: StatusSuccess}}
	req := WriteRequest{Writes: []AttributeWrite{{
		Path:  ConcreteAttributePath{Endpoint: 0, HasEndpoint: true, Cluster: aclClusterID, HasCluster: true, Attribute: aclAttributeI, HasAttribute: true},
		Value: AttributeValue{Value: uint32(1)},
	}}}

	wr := HandleWriteRequest(paseAdoptedCtx(2), d, req)
	if len(wr.Responses) != 1 {
		t.Fatalf("responses=%d, want 1", len(wr.Responses))
	}
	if wr.Responses[0].Status.Status != StatusSuccess {
		t.Fatalf("adopted PASE write = %v, want Success", wr.Responses[0].Status.Status)
	}

	wr = HandleWriteRequest(caseFabricCtx(2), d, req)
	if len(wr.Responses) != 1 || wr.Responses[0].Status.Status != StatusUnsupportedAccess {
		t.Fatalf("CASE node-id 0 write = %+v, want UnsupportedAccess", wr.Responses)
	}
}

// TestPASEAdoptedSessionKeepsAdministerOnInvoke covers an Administer-level
// command (e.g. GroupKeyManagement.KeySetWrite) over the adopted PASE session.
func TestPASEAdoptedSessionKeepsAdministerOnInvoke(t *testing.T) {
	d := &paseAdoptedACLDispatcher{fakeDispatcher{invokeStat: StatusSuccess}}
	req := InvokeRequest{Invokes: []CommandInvocation{{
		Path: ConcreteCommandPath{Endpoint: 0, HasEndpoint: true, Cluster: 0x003F, HasCluster: true, Command: 0x0000, HasCommand: true},
	}}}

	ir := HandleInvokeRequest(paseAdoptedCtx(2), d, req)
	if len(ir.Responses) != 1 {
		t.Fatalf("responses=%d, want 1", len(ir.Responses))
	}
	if ir.Responses[0].IsStatus && ir.Responses[0].Status.Status != StatusSuccess {
		t.Fatalf("adopted PASE invoke = %v, want Success", ir.Responses[0].Status.Status)
	}

	ir = HandleInvokeRequest(caseFabricCtx(2), d, req)
	if len(ir.Responses) != 1 || !ir.Responses[0].IsStatus || ir.Responses[0].Status.Status != StatusUnsupportedAccess {
		t.Fatalf("CASE node-id 0 invoke = %+v, want UnsupportedAccess", ir.Responses)
	}
}

// TestIsPASEFromContextDefaultsFalse pins the safe default: a context with
// no auth-mode stamp is not PASE, so the implicit grant never applies to a
// request whose session type could not be resolved.
func TestIsPASEFromContextDefaultsFalse(t *testing.T) {
	if IsPASEFromContext(context.Background()) {
		t.Fatal("IsPASEFromContext(bare ctx) = true, want false")
	}
	if !IsPASEFromContext(WithAuthModePASE(context.Background())) {
		t.Fatal("IsPASEFromContext(stamped ctx) = false, want true")
	}
}
