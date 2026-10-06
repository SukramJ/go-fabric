// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package im

import (
	"context"
	"testing"
)

// authorityProbe is a dispatcher whose one command answers with what
// AuthorityAt says about Administer on (0, 0x0065).
type authorityProbe struct {
	granted map[uint8]bool // privilege → CheckACL grants
}

func (d *authorityProbe) Read(context.Context, ConcreteAttributePath) []ReadResult { return nil }
func (d *authorityProbe) Write(context.Context, ConcreteAttributePath, AttributeValue) []WriteResult {
	return nil
}

func (d *authorityProbe) Invoke(ctx context.Context, p ConcreteCommandPath, _ any) InvokeResult {
	return InvokeResult{Path: p, Status: AuthorityAt(ctx, 0, 0x0065, 5)}
}

// authorityProbeACL wraps the probe so it implements ACLChecker.
type authorityProbeACL struct{ *authorityProbe }

func (d authorityProbeACL) CheckACL(_ context.Context, fabric uint8, node uint64, _ []uint32, endpoint uint16, cluster uint32, privilege uint8) StatusCode {
	if fabric != 3 || node != 0x77 || endpoint != 0 || cluster != 0x0065 || !d.granted[privilege] {
		return StatusUnsupportedAccess
	}
	return StatusSuccess
}

// probeInvoke invokes Groupcast command 0x02, which is not fabric-scoped,
// so the probe measures AuthorityAt alone — a fabric-scoped command would
// stop at the accessing-fabric gate first (TestFabricScopedInvokeNeedsAFabric).
func probeInvoke(ctx context.Context, d Dispatcher) StatusCode {
	resp := HandleInvokeRequest(ctx, d, InvokeRequest{Invokes: []CommandInvocation{{
		Path: ConcreteCommandPath{Endpoint: 0, Cluster: 0x0065, Command: 0x02, HasEndpoint: true, HasCluster: true, HasCommand: true},
	}}})
	return resp.Responses[0].Status.Status
}

// TestFabricScopedInvokeNeedsAFabric pins matter.js
// CommandInvokeResponse.ts:287: a fabric-scoped command (GroupKeyManagement
// KeySetRead, "F A") on a session without an accessing fabric answers
// UnsupportedAccess and never reaches the server; the same command with a
// fabric, and a non-fabric-scoped command without one, are dispatched.
// Found by the CHIP Python harness (TC-IDM-1.2 step 5).
func TestFabricScopedInvokeNeedsAFabric(t *testing.T) {
	t.Parallel()
	invoke := func(ctx context.Context, cluster, command uint32) StatusCode {
		d := &authorityProbe{}
		resp := HandleInvokeRequest(ctx, d, InvokeRequest{Invokes: []CommandInvocation{{
			Path: ConcreteCommandPath{Endpoint: 0, Cluster: cluster, Command: command, HasEndpoint: true, HasCluster: true, HasCommand: true},
		}}})
		return resp.Responses[0].Status.Status
	}
	noFabric := WithAuthModePASE(WithFabricFilter(context.Background(), false, 0))
	if got := invoke(noFabric, 0x003F, 0x01); got != StatusUnsupportedAccess {
		t.Errorf("KeySetRead without a fabric: %v, want UnsupportedAccess", got)
	}
	if got := invoke(WithAuthModePASE(WithFabricFilter(context.Background(), false, 2)), 0x003F, 0x01); got == StatusUnsupportedAccess {
		t.Errorf("KeySetRead with a fabric was refused")
	}
	if got := invoke(noFabric, 0x0030, 0x00); got == StatusUnsupportedAccess {
		t.Errorf("ArmFailSafe (not fabric-scoped) without a fabric was refused")
	}
}

// TestAuthorityAtParityMatterJS pins the in-handler access check a command
// consults for a privilege beyond its own (matter.js session.authorityAt,
// as GroupcastServer #requireAdmin uses it): the CASE subject holds what
// the ACL grants, PASE the implicit Administer, and nothing holds anything
// without an ACL source or outside a command.
func TestAuthorityAtParityMatterJS(t *testing.T) {
	t.Parallel()
	caseCtx := WithSubject(WithFabricFilter(context.Background(), false, 3), 0x77, nil)

	probe := &authorityProbe{granted: map[uint8]bool{3: true, 5: true}}
	if got := probeInvoke(caseCtx, authorityProbeACL{probe}); got != StatusSuccess {
		t.Errorf("CASE subject the ACL grants Administer: %v", got)
	}
	probe.granted[5] = false
	if got := probeInvoke(caseCtx, authorityProbeACL{probe}); got != StatusUnsupportedAccess {
		t.Errorf("CASE subject without Administer: %v", got)
	}
	if got := probeInvoke(WithAuthModePASE(caseCtx), authorityProbeACL{probe}); got != StatusSuccess {
		t.Errorf("PASE: %v, want the implicit Administer", got)
	}
	if got := probeInvoke(WithFabricFilter(context.Background(), false, 0), &authorityProbe{}); got != StatusSuccess {
		t.Errorf("pre-AddNOC PASE: %v", got)
	}
	if got := probeInvoke(caseCtx, &authorityProbe{}); got != StatusUnsupportedAccess {
		t.Errorf("no ACL source: %v, want a denial", got)
	}
	if got := AuthorityAt(context.Background(), 0, 0x0065, 1); got != StatusUnsupportedAccess {
		t.Errorf("outside a command: %v, want a denial", got)
	}
	stamped := WithAuthority(context.Background(), func(uint16, uint32, uint8) StatusCode { return StatusBusy })
	if got := AuthorityAt(stamped, 0, 0, 0); got != StatusBusy {
		t.Errorf("stamped check: %v", got)
	}
}

// TestGroupInvokeReport pins what a group invoke reports for Groupcast
// testing: nothing for a request that ends before dispatch, the requested
// path when nothing ran, and each endpoint the command ran on.
func TestGroupInvokeReport(t *testing.T) {
	t.Parallel()
	toggle := wildcardInvoke(6, 2)
	concrete := toggle
	concrete.Path.Endpoint, concrete.Path.HasEndpoint = 2, true

	d := &groupFakeDispatcher{deny: map[uint16]bool{3: true}}
	r := HandleGroupInvoke(groupCtx(2, 3), d, InvokeRequest{Invokes: []CommandInvocation{toggle}}, nil)
	if !r.Processed || len(r.Requested) != 1 || len(r.Dispatched) != 1 || r.Status != StatusSuccess {
		t.Errorf("one member granted, one denied: %+v", r)
	}
	r = HandleGroupInvoke(groupCtx(), d, InvokeRequest{Invokes: []CommandInvocation{toggle}}, nil)
	if !r.Processed || len(r.Dispatched) != 0 || len(r.Requested) != 1 {
		t.Errorf("no member endpoints: %+v", r)
	}
	for name, req := range map[string]InvokeRequest{
		"timed":    {TimedRequest: true, Invokes: []CommandInvocation{toggle}},
		"concrete": {Invokes: []CommandInvocation{concrete}},
		"two":      {Invokes: []CommandInvocation{toggle, toggle}},
	} {
		if r := HandleGroupInvoke(groupCtx(2), d, req, nil); r.Processed || r.Status.IsSuccess() {
			t.Errorf("%s: %+v, want an unprocessed failure", name, r)
		}
	}
	bad := toggle
	bad.DecodeStatus = StatusConstraintError
	if r := HandleGroupInvoke(groupCtx(2), d, InvokeRequest{Invokes: []CommandInvocation{bad}}, nil); !r.Processed || r.Status != StatusConstraintError || len(r.Dispatched) != 0 {
		t.Errorf("rejected fields: %+v", r)
	}
}

// TestGroupcastEventsAreAdministerAndFabricSensitive: GroupcastTesting
// carries access "S A" (groupcast.element.ts).
func TestGroupcastEventsAreAdministerAndFabricSensitive(t *testing.T) {
	t.Parallel()
	if eventReadPrivilege(0x0065) != 5 || !isFabricSensitiveEventCluster(0x0065) {
		t.Error("Groupcast events are not gated like AccessControl's")
	}
	if eventReadPrivilege(0x0006) != 1 || isFabricSensitiveEventCluster(0x0006) {
		t.Error("an ordinary cluster's events are gated")
	}
}
