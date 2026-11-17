// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"context"
	"testing"
)

// noACLDispatcher serves everything and counts what it runs; it has no
// ACLChecker.
type noACLDispatcher struct{ reads, writes, invokes int }

func (d *noACLDispatcher) Read(_ context.Context, p ConcreteAttributePath) []ReadResult {
	d.reads++
	return []ReadResult{{Path: p, Status: StatusSuccess, Value: AttributeValue{Value: uint8(1)}}}
}

func (d *noACLDispatcher) Write(_ context.Context, p ConcreteAttributePath, _ AttributeValue) []WriteResult {
	d.writes++
	return []WriteResult{{Path: p, Status: StatusSuccess}}
}

func (d *noACLDispatcher) Invoke(_ context.Context, p ConcreteCommandPath, _ any) InvokeResult {
	d.invokes++
	return InvokeResult{Path: p, Status: StatusSuccess}
}

// TestDispatcherWithoutACLCheckerDenies (review II, R2-P6-sec-0560, owner
// decision 136): a Dispatcher without an [ACLChecker] denies a CASE
// subject's read, write and invoke — UNSUPPORTED_ACCESS, nothing run — as
// the group path and [AuthorityAt] already did; matter.js judges every
// peer's request by an access control that grants nothing without an
// entry (fable-6a-w3 H6). A PASE session keeps its implicit grant. "No
// ACL" is endpoint.UnenforcedACL's, a checker that grants.
func TestDispatcherWithoutACLCheckerDenies(t *testing.T) {
	t.Parallel()
	caseCtx := WithSubject(WithFabricFilter(context.Background(), true, 1), 0x1234, nil)
	paseCtx := WithAuthModePASE(WithFabricFilter(context.Background(), false, 0))
	attr := ConcreteAttributePath{Endpoint: 0, Cluster: 0x0028, Attribute: 0x0005, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	cmd := ConcreteCommandPath{Endpoint: 0, Cluster: 0x003E, Command: 0x0A, HasEndpoint: true, HasCluster: true, HasCommand: true}
	write := WriteRequest{Writes: []AttributeWrite{{Path: attr, Value: AttributeValue{Value: "x"}}}}
	invoke := InvokeRequest{Invokes: []CommandInvocation{{Path: cmd}}}
	read := ReadRequest{AttributeRequests: []ConcreteAttributePath{attr}}

	d := &noACLDispatcher{}
	wr := HandleWriteRequest(caseCtx, d, write)
	if len(wr.Responses) != 1 || wr.Responses[0].Status.Status != StatusUnsupportedAccess {
		t.Errorf("CASE write: %+v, want UNSUPPORTED_ACCESS", wr)
	}
	ir := HandleInvokeRequest(caseCtx, d, invoke)
	if len(ir.Responses) != 1 || !ir.Responses[0].IsStatus || ir.Responses[0].Status.Status != StatusUnsupportedAccess {
		t.Errorf("CASE invoke: %+v, want UNSUPPORTED_ACCESS", ir)
	}
	rd := HandleReadRequest(caseCtx, d, read)
	if len(rd.Reports) != 1 || !rd.Reports[0].IsStatus || rd.Reports[0].Status.Status != StatusUnsupportedAccess {
		t.Errorf("CASE read: %+v, want UNSUPPORTED_ACCESS", rd.Reports)
	}
	if d.writes != 0 || d.invokes != 0 || d.reads != 0 {
		t.Errorf("a denied subject ran %d writes, %d invokes, %d reads", d.writes, d.invokes, d.reads)
	}
	// Events: an authorizer without a Checker denies as the dispatcher
	// does (Fable's read of sub round 1, B-1); PASE keeps its grant.
	ev := []EventReport{{Path: ConcreteEventPath{Endpoint: 0, Cluster: 0x0028, Event: 0x00, HasEndpoint: true, HasCluster: true, HasEvent: true}, Number: 1}}
	noChecker := EventReadAuthorizer{FabricIndex: 1, SubjectNodeID: 0x1234}
	if noChecker.Allows(caseCtx, 0, 0x0028) {
		t.Error("CASE event read without a Checker: allowed, want denied")
	}
	if got := AuthorizeEventReports(caseCtx, noChecker, ev); len(got) != 0 {
		t.Errorf("CASE events without a Checker: %+v, want none", got)
	}
	if !noChecker.Allows(paseCtx, 0, 0x0028) || len(AuthorizeEventReports(paseCtx, noChecker, ev)) != 1 {
		t.Error("PASE events without a Checker: denied, want the implicit grant")
	}

	p := &noACLDispatcher{}
	if wr := HandleWriteRequest(paseCtx, p, write); len(wr.Responses) != 1 || wr.Responses[0].Status.Status != StatusSuccess {
		t.Errorf("PASE write: %+v, want SUCCESS", wr)
	}
	if ir := HandleInvokeRequest(paseCtx, p, invoke); len(ir.Responses) != 1 || ir.Responses[0].Status.Status != StatusSuccess {
		t.Errorf("PASE invoke: %+v, want SUCCESS", ir)
	}
}

// noResponseDispatcher answers every command with [NoResponse].
type noResponseDispatcher struct{ noACLDispatcher }

func (*noResponseDispatcher) Invoke(_ context.Context, p ConcreteCommandPath, _ any) InvokeResult {
	return InvokeResult{Path: p, Status: StatusSuccess, Response: NoResponse{}}
}

// TestNoResponseLeavesTheCommandOut (review II, R2-P6-fsm-1001): a command
// whose handler answers [NoResponse] has no entry in the InvokeResponse.
func TestNoResponseLeavesTheCommandOut(t *testing.T) {
	t.Parallel()
	cmd := ConcreteCommandPath{Endpoint: 0, Cluster: 0x0031, Command: 0x06, HasEndpoint: true, HasCluster: true, HasCommand: true}
	ir := HandleInvokeRequest(unchecked(), &noResponseDispatcher{}, InvokeRequest{Invokes: []CommandInvocation{{Path: cmd}}})
	if len(ir.Responses) != 0 {
		t.Fatalf("responses = %+v, want none", ir.Responses)
	}
}
