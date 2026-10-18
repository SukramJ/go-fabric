// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"context"
	"testing"
)

// gateDispatcher serves OnOff (0x0006) on endpoints 1 and 2, the second
// denied to the subject; Groups.AddGroup (0x0004/0x00) needs Manage, the
// subject holds Operate.
type gateDispatcher struct{ invoked []uint16 }

func (*gateDispatcher) Read(context.Context, ConcreteAttributePath) []ReadResult { return nil }
func (*gateDispatcher) Write(context.Context, ConcreteAttributePath, AttributeValue) []WriteResult {
	return nil
}

func (d *gateDispatcher) Invoke(_ context.Context, p ConcreteCommandPath, _ any) InvokeResult {
	d.invoked = append(d.invoked, p.Endpoint)
	return InvokeResult{Path: p, Status: StatusSuccess}
}

func (*gateDispatcher) CommandPathStatus(p ConcreteCommandPath) StatusCode {
	if p.Cluster == 0x0006 && (p.Endpoint == 1 || p.Endpoint == 2) {
		return StatusSuccess
	}
	return StatusUnsupportedCluster
}

func (*gateDispatcher) CheckACL(_ context.Context, _ uint8, _ uint64, _ []uint32, endpoint uint16, _ uint32, privilege uint8) StatusCode {
	if endpoint == 2 || privilege > 3 {
		return StatusUnsupportedAccess
	}
	return StatusSuccess
}

func (*gateDispatcher) MinInvokePrivilege(_ uint16, cluster, _ uint32) uint8 {
	if cluster == 0x0004 {
		return 4
	}
	return 3
}

func (d *gateDispatcher) targets(p ConcreteCommandPath, authorize CommandAuthorizer) []uint16 {
	var out []uint16
	for _, ep := range []uint16{1, 2} {
		if p.Cluster == 0x0006 && authorize(ep, p.Cluster, p.Command).IsSuccess() {
			out = append(out, ep)
		}
	}
	return out
}

func (d *gateDispatcher) CommandTargets(p ConcreteCommandPath, authorize CommandAuthorizer) []uint16 {
	return d.targets(p, authorize)
}

func (d *gateDispatcher) InvokeAuthorized(ctx context.Context, p ConcreteCommandPath, fields any, authorize CommandAuthorizer) []InvokeResult {
	var out []InvokeResult
	for _, ep := range d.targets(p, authorize) {
		at := p
		at.Endpoint, at.HasEndpoint = ep, true
		out = append(out, d.Invoke(ctx, at, fields))
	}
	return out
}

func (*gateDispatcher) EventPathStatus(p ConcreteEventPath) StatusCode {
	if p.Endpoint != 0 {
		return StatusUnsupportedEndpoint
	}
	return StatusSuccess
}

// TestDispatchEndpoints is matter.js's dispatch-endpoint set
// (CommandInvokeResponseTest.ts "reports the endpoints it dispatches to
// before any command runs" at 862ad75f): a concrete path that passes its
// checks, the targets of a wildcard, nothing for a refused path; and the
// wildcard invoke runs on the targets only.
func TestDispatchEndpoints(t *testing.T) {
	t.Parallel()
	ctx := WithSubject(WithFabricFilter(context.Background(), false, 1), 0x55, nil)
	d := &gateDispatcher{}
	cmd := func(ep uint16, has bool, cluster, command uint32) CommandInvocation {
		return CommandInvocation{Path: ConcreteCommandPath{Endpoint: ep, Cluster: cluster, Command: command, HasEndpoint: has, HasCluster: true, HasCommand: true}}
	}
	got := DispatchEndpoints(ctx, d, InvokeRequest{Invokes: []CommandInvocation{
		cmd(1, true, 0x0006, 1),  // dispatched
		cmd(0, true, 0x0006, 1),  // no OnOff on 0
		cmd(1, true, 0x0004, 0),  // Manage needed
		cmd(0, false, 0x0006, 1), // wildcard: 1 (2 denied)
	}})
	if len(got) != 1 || !got[1] {
		t.Fatalf("dispatch endpoints %v, want {1}", got)
	}
	resp := HandleInvokeRequest(ctx, d, InvokeRequest{Invokes: []CommandInvocation{cmd(0, false, 0x0006, 1)}})
	if len(resp.Responses) != 1 || resp.Responses[0].Path.Endpoint != 1 || len(d.invoked) != 1 {
		t.Fatalf("wildcard invoke %+v, invoked %v", resp.Responses, d.invoked)
	}
	// A cluster status goes out under FAILURE.
	ent := resultEntry(cmd(1, true, 0x0006, 1), InvokeResult{Status: StatusConstraintError, ClusterStatus: 0x42, HasClusterStatus: true})
	if ent.Status.Status != StatusFailure || ent.Status.ClusterStatus != 0x42 {
		t.Fatalf("entry %+v", ent)
	}
}

// TestDeniedEventPathStatusesExistence: a concrete event path that does not
// exist answers its existence status (EventReadResponse.ts:158-224 at
// 862ad75f); a wildcard is skipped; Messages is fabric-sensitive.
func TestDeniedEventPathStatusesExistence(t *testing.T) {
	t.Parallel()
	d := &gateDispatcher{}
	auth := EventReadAuthorizer{Checker: d, FabricIndex: 1, SubjectNodeID: 0x55, Paths: d}
	got := DeniedEventPathStatuses(context.Background(), auth, []ConcreteEventPath{
		{Endpoint: 3, Cluster: 0x0028, Event: 0, HasEndpoint: true, HasCluster: true, HasEvent: true},
		{Endpoint: 0, Cluster: 0x0028, Event: 0, HasEndpoint: true, HasCluster: true, HasEvent: true},
		{Cluster: 0x0028, HasCluster: true},
	})
	if len(got) != 1 || got[0].Status.Status != StatusUnsupportedEndpoint || got[0].Path.Endpoint != 3 {
		t.Fatalf("statuses %+v", got)
	}
	if !isFabricSensitiveEventCluster(matterMessagesClusterID) || eventReadPrivilege(matterAccountLoginClusterID) != 5 {
		t.Fatal("Messages / AccountLogin not fabric-sensitive")
	}
}
