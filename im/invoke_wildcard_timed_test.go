// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"context"
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/schema"
)

// timedWildcardDispatcher serves every command on endpoints 2 and 3, grants
// every access check, and — when timedCmd is set — reports that one command
// as timed from its model ([TimedCommandProvider]). Only the timed gate of
// the wildcard expansion can keep a command from running on it.
type timedWildcardDispatcher struct {
	grantingDispatcher
	timedCmd *ConcreteCommandPath
}

func (d *timedWildcardDispatcher) IsTimedCommand(_ uint16, cluster, cmd uint32) bool {
	return d.timedCmd != nil && d.timedCmd.Cluster == cluster && d.timedCmd.Command == cmd
}

func (d *timedWildcardDispatcher) CommandTargets(p ConcreteCommandPath, authorize CommandAuthorizer) []uint16 {
	var out []uint16
	for _, ep := range []uint16{2, 3} {
		if authorize(ep, p.Cluster, p.Command).IsSuccess() {
			out = append(out, ep)
		}
	}
	return out
}

// plainWildcardDispatcher is timedWildcardDispatcher without the
// TimedCommandProvider: the timed quality then comes from the schema.
type plainWildcardDispatcher struct{ grantingDispatcher }

func (d *plainWildcardDispatcher) CommandTargets(p ConcreteCommandPath, authorize CommandAuthorizer) []uint16 {
	var out []uint16
	for _, ep := range []uint16{2, 3} {
		if authorize(ep, p.Cluster, p.Command).IsSuccess() {
			out = append(out, ep)
		}
	}
	return out
}

// TestWildcardInvokeSkipsTimedCommandWithoutTimedRequest pins the timed
// check of a wildcard-endpoint invoke's expansion (invokeGate.wildcardTarget,
// matter.js #wildcardTargetOf in
// packages/protocol/src/action/server/CommandInvokeResponse.ts): a command
// with the timed quality, sent outside a timed interaction, runs on no
// endpoint and answers nothing — the endpoints are skipped, not reported —
// and the dispatch-endpoint set is empty. Inside a timed interaction it
// runs on every endpoint. Without the check a wildcard path would run a
// timed-only command (AdministratorCommissioning, DoorLock) untimed on
// every endpoint (R2-P5-tmut-02). The group path has its own authorizer
// and is covered in group_parity_matterjs_test.go.
func TestWildcardInvokeSkipsTimedCommandWithoutTimedRequest(t *testing.T) {
	t.Parallel()
	const (
		adminCommissioning      uint32 = 0x003C
		openCommissioningWindow uint32 = 0x00
		onOff                   uint32 = 0x0006
		toggle                  uint32 = 0x02
	)
	if !schema.IsTimedInvoke(adminCommissioning, openCommissioningWindow) {
		t.Fatal("precondition: AdministratorCommissioning.OpenCommissioningWindow is not timed in the schema")
	}
	type dispatcher interface {
		Dispatcher
		AuthorizingInvoker
		CommandTargeter
		ran() []ConcreteCommandPath
	}
	cases := []struct {
		name string
		cmd  CommandInvocation
		d    func() dispatcher
	}{
		{"timed in the schema", wildcardInvoke(adminCommissioning, openCommissioningWindow), func() dispatcher { return &plainWildcardDispatcher{} }},
		{"timed in the server's model", wildcardInvoke(onOff, toggle), func() dispatcher {
			return &timedWildcardDispatcher{timedCmd: &ConcreteCommandPath{Cluster: onOff, Command: toggle}}
		}},
	}
	session := WithSubject(WithFabricFilter(context.Background(), false, 1), 0x77, nil)
	for _, tc := range cases {
		t.Run(tc.name+"/untimed", func(t *testing.T) {
			t.Parallel()
			ctx := WithTimedInteraction(session, false)
			d := tc.d()
			req := InvokeRequest{Invokes: []CommandInvocation{tc.cmd}}
			if got := DispatchEndpoints(ctx, d, req); len(got) != 0 {
				t.Errorf("DispatchEndpoints = %v, want none outside a timed interaction", got)
			}
			resp := HandleInvokeRequest(ctx, d, req)
			if len(resp.Responses) != 0 || len(d.ran()) != 0 {
				t.Errorf("answered %+v and ran on %v, want no entry and no endpoint", resp.Responses, d.ran())
			}
		})
		t.Run(tc.name+"/timed", func(t *testing.T) {
			t.Parallel()
			ctx := WithTimedInteraction(session, true)
			d := tc.d()
			req := InvokeRequest{Invokes: []CommandInvocation{tc.cmd}}
			if got := DispatchEndpoints(ctx, d, req); len(got) != 2 || !got[2] || !got[3] {
				t.Errorf("DispatchEndpoints = %v, want {2, 3}", got)
			}
			HandleInvokeRequest(ctx, d, req)
			var eps []uint16
			for _, p := range d.ran() {
				eps = append(eps, p.Endpoint)
			}
			if !slices.Equal(eps, []uint16{2, 3}) {
				t.Errorf("ran on %v, want [2 3]", eps)
			}
		})
	}
}

func (d *timedWildcardDispatcher) ran() []ConcreteCommandPath { return d.invoked }
func (d *plainWildcardDispatcher) ran() []ConcreteCommandPath { return d.invoked }
