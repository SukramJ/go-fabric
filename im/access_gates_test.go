// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"context"
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/schema"
)

// TestWildcardInvokeTargetGates pins the access and fabric-scope checks of
// a wildcard-endpoint invoke's expansion (invokeGate.wildcardTarget, the
// gate of matter.js #wildcardTargetOf,
// packages/protocol/src/action/server/CommandInvokeResponse.ts:367-398 at
// db021da0, the fabric check :388). Each case runs or skips a command on
// endpoints 2 and 3 of a dispatcher that grants every ACL check it is
// asked, so only the gate decides:
//
//   - over PASE (no accessing fabric) a command that is not fabric-scoped
//     runs, and the ACL is never consulted — the subject ACL is a CASE
//     check, PASE holds the implicit Administer ([invokeGate] `pase`);
//   - over PASE a fabric-scoped command runs nowhere (no accessing
//     fabric);
//   - over CASE on fabric 1 a fabric-scoped command runs, after the ACL.
//
// Mutation pairs (R2-P5 fxcode 3): `g.hasACL && !g.pase` → `||` fails the
// first case (the ACL is consulted on fabric 0 and refuses); the fabric
// check's `&&` → `||` fails the first and the third.
func TestWildcardInvokeTargetGates(t *testing.T) {
	t.Parallel()
	const (
		onOff, toggle      uint32 = 0x0006, 0x02
		groups, addGroup   uint32 = 0x0004, 0x00
		caseSubject        uint64 = 0x77
		fabric, noFabric   uint8  = 1, 0
		wantRun, wantNoRun        = true, false
	)
	if schema.IsFabricScopedInvoke(onOff, toggle) || !schema.IsFabricScopedInvoke(groups, addGroup) {
		t.Fatal("precondition: OnOff.Toggle is not fabric-scoped and Groups.AddGroup is")
	}
	pase := WithAuthModePASE(WithFabricFilter(context.Background(), false, noFabric))
	caseSession := WithSubject(WithFabricFilter(context.Background(), false, fabric), caseSubject, nil)
	for _, c := range []struct {
		name       string
		ctx        context.Context
		cmd        CommandInvocation
		run        bool
		aclChecked bool
	}{
		{"PASE, not fabric-scoped", pase, wildcardInvoke(onOff, toggle), wantRun, false},
		{"PASE, fabric-scoped", pase, wildcardInvoke(groups, addGroup), wantNoRun, false},
		{"CASE, fabric-scoped", caseSession, wildcardInvoke(groups, addGroup), wantRun, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			d := &plainWildcardDispatcher{}
			ctx := WithTimedInteraction(c.ctx, false)
			HandleInvokeRequest(ctx, d, InvokeRequest{Invokes: []CommandInvocation{c.cmd}})
			eps := make([]uint16, 0, len(d.ran()))
			for _, p := range d.ran() {
				eps = append(eps, p.Endpoint)
			}
			want := []uint16(nil)
			if c.run {
				want = []uint16{2, 3}
			}
			if !slices.Equal(eps, want) {
				t.Errorf("ran on %v, want %v", eps, want)
			}
			if got := len(d.checked) > 0; got != c.aclChecked {
				t.Errorf("ACL consulted = %v (fabrics %v), want %v", got, d.checked, c.aclChecked)
			}
		})
	}
}

// TestGroupWriteNeedsGroupSubjectAndAuthorizingWriter pins
// HandleGroupWriteRequest's guard: a write without a Group subject in the
// context, or against a dispatcher that cannot authorize a write per
// endpoint, is answered SUCCESS without judging or writing anything —
// there is no group to map the path to and no gate to hold. The
// unsuppressed and timed checks before it still apply (controls in
// group_parity_matterjs_test.go). Mutation pair (R2-P5 fxcode 3): the
// guard `!ok || !canWrite` turned off fails both cases — the first judges
// the concrete path (InvalidAction), the second calls the absent writer
// (nil interface).
func TestGroupWriteNeedsGroupSubjectAndAuthorizingWriter(t *testing.T) {
	t.Parallel()
	group := ConcreteAttributePath{Cluster: 6, Attribute: 0x4001, HasCluster: true, HasAttribute: true}
	concrete := group
	concrete.Endpoint, concrete.HasEndpoint = 2, true
	write := func(p ConcreteAttributePath) WriteRequest {
		return WriteRequest{SuppressResponse: true, Writes: []AttributeWrite{{Path: p, Value: AttributeValue{Value: uint16(5)}}}}
	}
	t.Run("no group subject", func(t *testing.T) {
		t.Parallel()
		d := &groupFakeDispatcher{}
		ctx := WithFabricFilter(context.Background(), false, 7)
		if got := HandleGroupWriteRequest(ctx, d, write(concrete)); got != StatusSuccess {
			t.Errorf("status %v, want SUCCESS", got)
		}
		if len(d.written) != 0 || len(d.acl) != 0 {
			t.Errorf("wrote %v, checked %v; want nothing", d.written, d.acl)
		}
	})
	t.Run("no authorizing writer", func(t *testing.T) {
		t.Parallel()
		// plainDispatcher (group_parity_matterjs_test.go) is a Dispatcher
		// and nothing else.
		if got := HandleGroupWriteRequest(groupCtx(2, 3), plainDispatcher{}, write(group)); got != StatusSuccess {
			t.Errorf("status %v, want SUCCESS", got)
		}
	})
}
