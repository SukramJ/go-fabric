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

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
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

// TestInvokeLargeMessageParity: a command with the Large Message quality
// runs only on a session whose transport carries large messages. On any
// other session a concrete path answers INVALID_TRANSPORT_TYPE after the
// existence and access checks and before the fabric-scope and timed ones
// (CommandInvokeResponse.ts:281-285 at db021da0), a wildcard path skips the
// endpoint (:384-386); connectedhomeip
// src/app/InteractionModelEngine.cpp:1878-1884 at 6170af84 (CheckCommandFlags).
// matter.js's session.largeMessage is the channel's supportsLargeMessages:
// TCP true (packages/protocol/src/transport/tcp/TcpChannel.ts:45), UDP and
// BLE false (packages/general/src/net/udp/UdpTransport.ts:69,
// packages/protocol/src/ble/Ble.ts:39).
func TestInvokeLargeMessageParity(t *testing.T) {
	t.Parallel()
	const (
		tlsCerts       = uint32(0x0801) // TlsCertificateManagement
		provisionRoot  = uint32(0x00)   // ProvisionRootCertificate, "L"
		findRoot       = uint32(0x02)   // FindRootCertificate, "L"
		onOff, onOffOn = uint32(0x0006), uint32(0x01)
	)
	large := im.WithLargeMessageSession
	for _, c := range []struct {
		name    string
		ctx     context.Context
		path    im.ConcreteCommandPath
		want    im.StatusCode
		invokes int
	}{
		{"concrete, UDP session", large(caseCtx(false), false), cmdAt(2, tlsCerts, provisionRoot), im.StatusInvalidTransportType, 0},
		{"concrete, TCP session", large(caseCtx(false), true), cmdAt(2, tlsCerts, provisionRoot), im.StatusSuccess, 1},
		{"concrete, no stamp: the caller's check", caseCtx(false), cmdAt(2, tlsCerts, provisionRoot), im.StatusSuccess, 1},
		{"concrete, UDP session, endpoint absent: existence first", large(caseCtx(false), false), cmdAt(42, tlsCerts, provisionRoot), im.StatusUnsupportedEndpoint, 0},
		{"concrete, UDP session, command without the quality", large(caseCtx(false), false), cmdAt(2, onOff, onOffOn), im.StatusSuccess, 0},
	} {
		srvTLS := &gateServer{id: tlsCerts, attrs: []uint32{0x0000}, vals: map[uint32]any{}}
		srvOnOff := &gateServer{id: onOff, attrs: []uint32{0x0000}, vals: map[uint32]any{}}
		d := gateDispatcher(t, store.PrivilegeAdminister, srvTLS, srvOnOff)
		if got := invokeStatus(c.ctx, t, d, c.path); got != c.want {
			t.Errorf("%s: status %v, want %v", c.name, got, c.want)
		}
		if srvTLS.invokes != c.invokes {
			t.Errorf("%s: TLS server ran %d commands, want %d", c.name, srvTLS.invokes, c.invokes)
		}
	}

	// A wildcard path skips the endpoint on a session without large
	// messages: no status, nothing runs.
	for _, on := range []bool{false, true} {
		srvTLS := &gateServer{id: tlsCerts, attrs: []uint32{0x0000}, vals: map[uint32]any{}}
		d := gateDispatcher(t, store.PrivilegeAdminister, srvTLS)
		wild := im.ConcreteCommandPath{Cluster: tlsCerts, Command: findRoot, HasCluster: true, HasCommand: true}
		resp := im.HandleInvokeRequest(large(caseCtx(false), on), d, im.InvokeRequest{Invokes: []im.CommandInvocation{{Path: wild}}})
		want := 0
		if on {
			want = 1
		}
		if len(resp.Responses) != want || srvTLS.invokes != want {
			t.Errorf("wildcard, large message session %t: responses %+v, invokes %d, want %d of each", on, resp.Responses, srvTLS.invokes, want)
		}
		if got := im.DispatchEndpoints(large(caseCtx(false), on), d, im.InvokeRequest{Invokes: []im.CommandInvocation{{Path: wild}}}); len(got) != want {
			t.Errorf("wildcard, large message session %t: dispatch endpoints %v, want %d", on, got, want)
		}
	}
}

// TestGroupInvokeLargeMessageParity: a group message travels UDP multicast,
// whose channel carries no large messages (UdpTransport.ts:69 at db021da0),
// so a group command with the Large Message quality is skipped on every
// member endpoint, the way #wildcardTargetOf skips it
// (CommandInvokeResponse.ts:384-386).
func TestGroupInvokeLargeMessageParity(t *testing.T) {
	t.Parallel()
	const tlsCerts, provisionRoot = uint32(0x0801), uint32(0x00)
	srv := &gateServer{id: tlsCerts, attrs: []uint32{0x0000}, vals: map[uint32]any{}}
	d := NewTopologyDispatcher(makeTopology(&Endpoint{ID: 2, Source: gateSource{srvs: []contract.ClusterServer{srv}}}))
	d.SetACLLister(fakeACLLister{entries: []store.ACLEntry{groupEntry(1, store.PrivilegeOperate, nil, nil)}})
	ctx := im.WithGroupSubject(im.WithFabricFilter(context.Background(), true, 1), im.GroupSubject{GroupID: 0x0101, HasValidMapping: true, Endpoints: []uint16{2}})
	wild := im.ConcreteCommandPath{Cluster: tlsCerts, Command: provisionRoot, HasCluster: true, HasCommand: true}
	report := im.HandleGroupInvoke(ctx, d, im.InvokeRequest{Invokes: []im.CommandInvocation{{Path: wild}}}, nil)
	if len(report.Dispatched) != 0 || srv.invokes != 0 {
		t.Fatalf("group Large Message command: dispatched %+v, invokes %d, want none", report.Dispatched, srv.invokes)
	}
	// Control: a command without the quality runs on the member endpoint.
	wild.Command = 0x01 // an id schema/large_message_gen.go does not list for 0x0801
	report = im.HandleGroupInvoke(ctx, d, im.InvokeRequest{Invokes: []im.CommandInvocation{{Path: wild}}}, nil)
	if len(report.Dispatched) != 1 || srv.invokes != 1 {
		t.Fatalf("group command without the quality: dispatched %+v, invokes %d, want one", report.Dispatched, srv.invokes)
	}
}
