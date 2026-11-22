// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

// The per-path gates of a concrete and a wildcard invoke through
// im.HandleInvokeRequest and the TopologyDispatcher, in the order of
// matter.js packages/protocol/src/action/server/CommandInvokeResponse.ts at
// db021da0 (#processConcrete :240-296, #wildcardTargetOf :367-401): the
// Operate pass, the existence statuses (:263-271), the command privilege
// (:273-279), then the command's Large Message, fabric-scope and timed
// qualities (:281-295).

import (
	"context"
	"testing"

	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/store"
)

// gateServer keeps one value per attribute and counts the commands it runs.
type gateServer struct {
	id      uint32
	attrs   []uint32
	vals    map[uint32]any
	writes  int
	invokes int
}

func (s *gateServer) MatterClusterID() uint32 { return s.id }
func (s *gateServer) MatterRead(a uint32) (any, bool) {
	v, ok := s.vals[a]
	return v, ok
}
func (s *gateServer) MatterReportable() []uint32 { return s.attrs }
func (s *gateServer) MatterAttributes() []uint32 { return s.attrs }
func (s *gateServer) MatterWrite(_ context.Context, a uint32, v any) error {
	s.writes++
	s.vals[a] = v
	return nil
}

func (s *gateServer) MatterInvoke(context.Context, uint32, any) (any, error) {
	s.invokes++
	return nil, nil
}

type gateSource struct{ srvs []contract.ClusterServer }

func (s gateSource) MatterDeviceType() uint16                       { return 0x010A }
func (s gateSource) MatterClusterServers() []contract.ClusterServer { return s.srvs }

// gateDispatcher is a topology with endpoint 2 serving srvs, behind an ACL
// that grants fabric 1's CASE subjects priv.
func gateDispatcher(t *testing.T, priv store.Privilege, srvs ...contract.ClusterServer) *TopologyDispatcher {
	t.Helper()
	d := NewTopologyDispatcher(makeTopology(&Endpoint{ID: 2, Source: gateSource{srvs: srvs}}))
	d.SetACLLister(fakeACLLister{entries: []store.ACLEntry{caseEntry(1, priv, nil)}})
	return d
}

// caseCtx is a CASE request on fabric 1, timed or not.
func caseCtx(timed bool) context.Context {
	ctx := im.WithFabricFilter(context.Background(), false, 1)
	ctx = im.WithSubject(ctx, 0x1111, nil)
	return im.WithTimedInteraction(ctx, timed)
}

// paseCtx is a PASE request before AddNOC: no fabric.
func paseCtx() context.Context {
	return im.WithAuthModePASE(im.WithTimedInteraction(im.WithFabricFilter(context.Background(), false, 0), false))
}

func cmdAt(endpoint uint16, cluster, command uint32) im.ConcreteCommandPath {
	return im.ConcreteCommandPath{Endpoint: endpoint, Cluster: cluster, Command: command, HasEndpoint: true, HasCluster: true, HasCommand: true}
}

func invokeStatus(ctx context.Context, t *testing.T, d im.Dispatcher, p im.ConcreteCommandPath) im.StatusCode {
	t.Helper()
	resp := im.HandleInvokeRequest(ctx, d, im.InvokeRequest{Invokes: []im.CommandInvocation{{Path: p}}})
	if len(resp.Responses) != 1 || !resp.Responses[0].IsStatus {
		t.Fatalf("responses %+v, want one status", resp.Responses)
	}
	return resp.Responses[0].Status.Status
}

// TestInvokeExistenceBeforeFlagGatesParity: a command at a path that does
// not exist answers the existence status before its fabric-scope and timed
// qualities are looked at — CommandInvokeResponse.ts:263-271 ahead of
// :287-295 at db021da0; connectedhomeip
// src/app/InteractionModelEngine.cpp:1846-1851 at 6170af84.
func TestInvokeExistenceBeforeFlagGatesParity(t *testing.T) {
	t.Parallel()
	const (
		onOff            = uint32(0x0006)
		adminCommission  = uint32(0x003C) // OpenCommissioningWindow 0x00 is timed
		groupKeyMgmt     = uint32(0x003F) // KeySetRead 0x01 is fabric-scoped
		openCommissionWd = uint32(0x00)
		keySetRead       = uint32(0x01)
	)
	srv := &gateServer{id: onOff, attrs: []uint32{0x0000}, vals: map[uint32]any{}}
	d := gateDispatcher(t, store.PrivilegeAdminister, srv)
	for _, c := range []struct {
		name string
		ctx  context.Context
		path im.ConcreteCommandPath
		want im.StatusCode
	}{
		{"timed command untimed, endpoint absent", caseCtx(false), cmdAt(42, adminCommission, openCommissionWd), im.StatusUnsupportedEndpoint},
		{"timed command untimed, cluster absent", caseCtx(false), cmdAt(2, adminCommission, openCommissionWd), im.StatusUnsupportedCluster},
		{"timed command in a timed interaction, cluster absent", caseCtx(true), cmdAt(2, adminCommission, openCommissionWd), im.StatusUnsupportedCluster},
		{"fabric-scoped command without a fabric, endpoint absent", paseCtx(), cmdAt(42, groupKeyMgmt, keySetRead), im.StatusUnsupportedEndpoint},
		{"fabric-scoped command without a fabric, cluster absent", paseCtx(), cmdAt(2, groupKeyMgmt, keySetRead), im.StatusUnsupportedCluster},
	} {
		if got := invokeStatus(c.ctx, t, d, c.path); got != c.want {
			t.Errorf("%s: status %v, want %v", c.name, got, c.want)
		}
	}
	if srv.invokes != 0 {
		t.Errorf("server ran %d commands, want 0", srv.invokes)
	}
}

// TestWildcardInvokeUndecodableFieldsParity: a wildcard-endpoint command
// whose fields do not decode answers the decode status once per endpoint it
// targets, and runs nowhere. matter.js selects the endpoints first
// (#processEndpointForWildcard → #wildcardTargetOf,
// CommandInvokeResponse.ts:330-361 at db021da0) and decodes per endpoint in
// #invokeCommand (:484-487, the failure a status for that endpoint
// :509-546); no target, no status.
func TestWildcardInvokeUndecodableFieldsParity(t *testing.T) {
	t.Parallel()
	const onOff, off = uint32(0x0006), uint32(0x00)
	wild := im.ConcreteCommandPath{Cluster: onOff, Command: off, HasCluster: true, HasCommand: true}
	inv := im.CommandInvocation{Path: wild, DecodeStatus: im.StatusInvalidCommand, CommandRef: 4, HasCommandRef: true}

	srv := &gateServer{id: onOff, attrs: []uint32{0x0000}, vals: map[uint32]any{}}
	d := gateDispatcher(t, store.PrivilegeAdminister, srv)
	resp := im.HandleInvokeRequest(caseCtx(false), d, im.InvokeRequest{Invokes: []im.CommandInvocation{inv}})
	if len(resp.Responses) != 1 {
		t.Fatalf("responses %+v, want one", resp.Responses)
	}
	r := resp.Responses[0]
	if !r.IsStatus || r.Status.Status != im.StatusInvalidCommand || r.Path != cmdAt(2, onOff, off) || r.CommandRef != 4 || !r.HasCommandRef {
		t.Errorf("response %+v, want INVALID_COMMAND at endpoint 2 with CommandRef 4", r)
	}
	if srv.invokes != 0 {
		t.Errorf("server ran %d commands, want 0", srv.invokes)
	}

	// No endpoint the session may invoke the command on: no status at all.
	denied := gateDispatcher(t, store.PrivilegeView, &gateServer{id: onOff, attrs: []uint32{0x0000}, vals: map[uint32]any{}})
	if resp := im.HandleInvokeRequest(caseCtx(false), denied, im.InvokeRequest{Invokes: []im.CommandInvocation{inv}}); len(resp.Responses) != 0 {
		t.Errorf("denied everywhere: responses %+v, want none", resp.Responses)
	}
}
