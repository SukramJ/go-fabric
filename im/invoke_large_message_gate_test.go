// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

// The Large Message gate of the invoke path (review II, R2-P4-imwrite-01 =
// F-db021da0-29): a command with the Large Message quality ("L") on a
// session that carries no large messages answers InvalidTransportType for
// its concrete path and is left out of a wildcard path's targets — matter.js
// packages/protocol/src/action/server/CommandInvokeResponse.ts:281-285 and
// :378-386 at db021da0, connectedhomeip CheckCommandFlags
// (src/app/InteractionModelEngine.cpp:1875-1884 at 6170af84). No matter.js
// unit test of the server gate exists at the pin; the table is this
// module's.

import (
	"context"
	"testing"

	"thingfabric.dev/go-fabric/schema"
)

const (
	lmProviderCluster uint32 = 0x0553 // WebRtcTransportProvider
	lmProvideOffer    uint32 = 0x02   // ProvideOffer, "L"
	lmOnOffCluster    uint32 = 0x0006
	lmToggle          uint32 = 0x02
)

// lmGateDispatcher grants or refuses each gate on demand: the access check
// (denyACL), the path's existence (pathStatus) and the command's timed
// quality from its model (timed). Wildcard paths expand to endpoints 2 and 3.
type lmGateDispatcher struct {
	grantingDispatcher
	denyACL    bool
	pathStatus StatusCode
	timed      bool
}

func (d *lmGateDispatcher) CheckACL(context.Context, uint8, uint64, []uint32, uint16, uint32, uint8) StatusCode {
	if d.denyACL {
		return StatusUnsupportedAccess
	}
	return StatusSuccess
}

func (d *lmGateDispatcher) CommandPathStatus(ConcreteCommandPath) StatusCode {
	if d.pathStatus == 0 {
		return StatusSuccess
	}
	return d.pathStatus
}

func (d *lmGateDispatcher) IsTimedCommand(uint16, uint32, uint32) bool { return d.timed }

func (d *lmGateDispatcher) CommandTargets(p ConcreteCommandPath, authorize CommandAuthorizer) []uint16 {
	var out []uint16
	for _, ep := range []uint16{2, 3} {
		if authorize(ep, p.Cluster, p.Command).IsSuccess() {
			out = append(out, ep)
		}
	}
	return out
}

func lmConcreteInvoke(cluster, cmd uint32) CommandInvocation {
	inv := wildcardInvoke(cluster, cmd)
	inv.Path.Endpoint, inv.Path.HasEndpoint = 2, true
	return inv
}

// TestInvokeLargeMessageGate is the gate table: a Large Message command over
// UDP, BLE (both stated as no large messages) and TCP, on a concrete and on
// a wildcard path; a command without the quality is never refused; a
// session that stated nothing is not gated (the caller's check).
func TestInvokeLargeMessageGate(t *testing.T) {
	t.Parallel()
	if !schema.CommandLargeMessage(lmProviderCluster, lmProvideOffer) {
		t.Fatal("precondition: WebRtcTransportProvider.ProvideOffer has no Large Message quality in the schema")
	}
	if schema.CommandLargeMessage(lmOnOffCluster, lmToggle) {
		t.Fatal("precondition: OnOff.Toggle has the Large Message quality")
	}
	base := WithTimedInteraction(WithSubject(WithFabricFilter(context.Background(), false, 1), 0x77, nil), false)
	sessions := []struct {
		name  string
		ctx   context.Context
		large bool // the session carries large messages (or stated nothing)
	}{
		{"udp", WithLargeMessageSession(base, false), false},
		{"ble", WithLargeMessageSession(base, false), false},
		{"tcp", WithLargeMessageSession(base, true), true},
		{"unstated", base, true},
	}
	for _, s := range sessions {
		t.Run(s.name+"/concrete", func(t *testing.T) {
			t.Parallel()
			d := &lmGateDispatcher{}
			resp := HandleInvokeRequest(s.ctx, d, InvokeRequest{Invokes: []CommandInvocation{lmConcreteInvoke(lmProviderCluster, lmProvideOffer)}})
			if len(resp.Responses) != 1 {
				t.Fatalf("responses = %+v, want one", resp.Responses)
			}
			got := resp.Responses[0]
			if s.large {
				if len(d.invoked) != 1 {
					t.Errorf("the command did not run on a session that carries large messages: %+v", got)
				}
				return
			}
			if !got.IsStatus || got.Status.Status != StatusInvalidTransportType || len(d.invoked) != 0 {
				t.Errorf("answered %+v, ran %v; want InvalidTransportType and nothing run", got, d.invoked)
			}
		})
		t.Run(s.name+"/wildcard", func(t *testing.T) {
			t.Parallel()
			d := &lmGateDispatcher{}
			req := InvokeRequest{Invokes: []CommandInvocation{wildcardInvoke(lmProviderCluster, lmProvideOffer)}}
			eps := DispatchEndpoints(s.ctx, d, req)
			resp := HandleInvokeRequest(s.ctx, d, req)
			if s.large {
				if len(d.invoked) != 2 || len(eps) != 2 {
					t.Errorf("ran on %v (dispatch set %v), want endpoints 2 and 3", d.invoked, eps)
				}
				return
			}
			if len(resp.Responses) != 0 || len(d.invoked) != 0 || len(eps) != 0 {
				t.Errorf("answered %+v, ran %v, dispatch set %v; want the command left out silently", resp.Responses, d.invoked, eps)
			}
		})
		t.Run(s.name+"/plain command", func(t *testing.T) {
			t.Parallel()
			d := &lmGateDispatcher{}
			HandleInvokeRequest(s.ctx, d, InvokeRequest{Invokes: []CommandInvocation{lmConcreteInvoke(lmOnOffCluster, lmToggle), wildcardInvoke(lmOnOffCluster, lmToggle)}})
			if len(d.invoked) != 3 {
				t.Errorf("a command without the quality ran on %v, want the concrete and both wildcard endpoints", d.invoked)
			}
		})
	}
}

// TestInvokeLargeMessageGateOrder pins where the gate sits among the other
// checks of a concrete path: after the existence check (UnsupportedCommand)
// and the command's privilege (UnsupportedAccess), before the timed quality
// (NeedsTimedInteraction) — CommandInvokeResponse.ts:263-295 at db021da0.
// The fabric-scope check sits between the gate and the timed one there
// (:287-290); no command of the snapshot is both Large Message and
// fabric-scoped, so that pair is not observable and not tested.
func TestInvokeLargeMessageGateOrder(t *testing.T) {
	t.Parallel()
	udp := WithLargeMessageSession(WithTimedInteraction(WithSubject(WithFabricFilter(context.Background(), false, 1), 0x77, nil), false), false)
	cases := []struct {
		name string
		d    *lmGateDispatcher
		want StatusCode
	}{
		{"existence first", &lmGateDispatcher{pathStatus: StatusUnsupportedCommand}, StatusUnsupportedCommand},
		{"privilege first", &lmGateDispatcher{denyACL: true}, StatusUnsupportedAccess},
		{"before timed", &lmGateDispatcher{timed: true}, StatusInvalidTransportType},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resp := HandleInvokeRequest(udp, tc.d, InvokeRequest{Invokes: []CommandInvocation{lmConcreteInvoke(lmProviderCluster, lmProvideOffer)}})
			if len(resp.Responses) != 1 || resp.Responses[0].Status.Status != tc.want {
				t.Errorf("answered %+v, want %s", resp.Responses, tc.want)
			}
		})
	}
	// The wildcard path: a refused authority leaves the endpoint out before
	// the gate is asked, and so does the gate itself — no entry either way.
	d := &lmGateDispatcher{denyACL: true}
	if resp := HandleInvokeRequest(udp, d, InvokeRequest{Invokes: []CommandInvocation{wildcardInvoke(lmProviderCluster, lmProvideOffer)}}); len(resp.Responses) != 0 {
		t.Errorf("wildcard with access refused answered %+v, want nothing", resp.Responses)
	}
}

// TestGroupInvokeLeavesALargeMessageCommandOut: a group session carries no
// large messages, so a group invoke of a Large Message command runs on no
// member endpoint, after the authority check; a command without the
// quality runs on every member (review II, Fable P-1; matter.js runs a
// group invoke through the wildcard expansion, CommandInvokeResponse.ts
// :136-160 at db021da0, and #wildcardTargetOf leaves it out, :384-386).
func TestGroupInvokeLeavesALargeMessageCommandOut(t *testing.T) {
	t.Parallel()
	d := &lmGateDispatcher{}
	HandleGroupInvoke(groupCtx(2, 3), d, InvokeRequest{Invokes: []CommandInvocation{wildcardInvoke(lmProviderCluster, lmProvideOffer)}}, nil)
	if len(d.invoked) != 0 {
		t.Fatalf("a Large Message command ran on %v over a group session", d.invoked)
	}
	HandleGroupInvoke(groupCtx(2, 3), d, InvokeRequest{Invokes: []CommandInvocation{wildcardInvoke(lmOnOffCluster, lmToggle)}}, nil)
	if len(d.invoked) != 2 {
		t.Fatalf("a plain command ran on %v, want both members", d.invoked)
	}
}
