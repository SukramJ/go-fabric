// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

// The Write, Invoke and Timed interactions as matter.js answers them on the
// wire, driven through Bridge.dispatch over the PASE test session: matter.js
// packages/node/src/node/server/InteractionServer.ts (handleWriteRequest,
// handleInvokeRequest), packages/protocol/src/protocol/MessageExchange.ts
// (the timed window) and packages/protocol/src/action/server/
// AttributeWriteResponse.ts at db021da0.

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// imPeer is a controller on the PASE test session of a started bridge.
type imPeer struct {
	t    *testing.T
	b    *Bridge
	conn *net.UDPConn
}

func newIMPeer(t *testing.T, b *Bridge) *imPeer {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &imPeer{t: t, b: b, conn: conn}
}

// send dispatches one IM message on exchange as its initiator.
func (p *imPeer) send(exchange uint16, opcode uint8, payload []byte) {
	p.t.Helper()
	proto := message.ProtocolHeader{ProtocolID: im.InteractionModelProtocolID, Opcode: opcode, ExchangeID: exchange, Initiator: true}.Marshal()
	if err := p.b.dispatch(context.Background(), secureTestDatagram(p.t, p.b, proto, payload), p.conn.LocalAddr()); err != nil {
		p.t.Fatalf("dispatch: %v", err)
	}
}

// next returns the next IM message the bridge sent, skipping standalone
// acknowledgements; ok is false when none arrives within wait.
func (p *imPeer) next(wait time.Duration) (message.ProtocolHeader, []byte, bool) {
	p.t.Helper()
	deadline := time.Now().Add(wait)
	buf := make([]byte, 2048)
	for {
		_ = p.conn.SetReadDeadline(deadline)
		n, _, err := p.conn.ReadFromUDP(buf)
		if err != nil {
			return message.ProtocolHeader{}, nil, false
		}
		got := openTestReplyFor(p.t, p.b, buf[:n])
		_, hdrLen, err := message.UnmarshalHeader(got)
		if err != nil {
			p.t.Fatalf("UnmarshalHeader: %v", err)
		}
		proto, protoLen, err := message.UnmarshalProtocolHeader(got[hdrLen:])
		if err != nil {
			p.t.Fatalf("UnmarshalProtocolHeader: %v", err)
		}
		if proto.ProtocolID == mrp.SecureChannelProtocolID && proto.Opcode == mrp.StandaloneAckOpcode {
			continue
		}
		return proto, got[hdrLen+protoLen:], true
	}
}

// status returns the status of the StatusResponse the bridge answers next.
func (p *imPeer) status() im.StatusCode {
	p.t.Helper()
	proto, body, ok := p.next(2 * time.Second)
	if !ok {
		p.t.Fatal("no reply")
	}
	if proto.Opcode != im.OpcodeStatusResponse {
		p.t.Fatalf("reply opcode 0x%02X, want StatusResponse", proto.Opcode)
	}
	return decodeStatusResponseCode(p.t, body)
}

// writeRequestBytes encodes req as a client sends it.
func writeRequestBytes(t *testing.T, req im.WriteRequest) []byte {
	t.Helper()
	enc := tlv.NewEncoder()
	req.MarshalTLV(enc, defaultAttributeValueWriter)
	b, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// storeServer serves one cluster and keeps what is written to it.
type storeServer struct {
	id    uint32
	mu    sync.Mutex
	vals  map[uint32]any
	write int
}

func (s *storeServer) MatterClusterID() uint32 { return s.id }
func (s *storeServer) MatterRead(a uint32) (any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.vals[a]
	return v, ok
}

func (s *storeServer) MatterWrite(_ context.Context, a uint32, v any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.write++
	s.vals[a] = v
	return nil
}

func (s *storeServer) MatterInvoke(context.Context, uint32, any) (any, error) { return nil, nil }
func (s *storeServer) MatterReportable() []uint32                             { return nil }

func (s *storeServer) writes() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.write
}

// onTimeBridge is a started bridge whose root serves OnOff (OnTime 0x4001
// writable) from a storeServer.
func onTimeBridge(t *testing.T) (*Bridge, *storeServer) {
	t.Helper()
	b := newStartedBridge(t)
	srv := &storeServer{id: 0x0006, vals: map[uint32]any{0x4001: uint16(0)}}
	b.AttachRootClusters([]contract.ClusterServer{srv})
	return b, srv
}

func onTimePath() im.ConcreteAttributePath {
	return im.ConcreteAttributePath{Endpoint: 0, Cluster: 0x0006, Attribute: 0x4001, HasEndpoint: true, HasCluster: true, HasAttribute: true}
}

// TestWriteWildcardPathFlagsEndsTheInteractionParity: a path with
// WildcardPathFlags ends the write with StatusResponse(INVALID_ACTION) when
// it is reached — the check is the first step of each element of
// AttributeWriteResponse.process (:59-66 at db021da0), so the elements before
// it have been written.
func TestWriteWildcardPathFlagsEndsTheInteractionParity(t *testing.T) {
	t.Parallel()
	b, srv := onTimeBridge(t)
	p := newIMPeer(t, b)
	flagged := onTimePath()
	flagged.WildcardPathFlags = 1
	p.send(0x4101, im.OpcodeWriteRequest, writeRequestBytes(t, im.WriteRequest{Writes: []im.AttributeWrite{
		{Path: onTimePath(), Value: im.AttributeValue{Value: uint16(7)}},
		{Path: flagged, Value: im.AttributeValue{Value: uint16(9)}},
	}}))
	if got := p.status(); got != im.StatusInvalidAction {
		t.Fatalf("status %v, want INVALID_ACTION", got)
	}
	if v, _ := srv.MatterRead(0x4001); fmt.Sprint(v) != "7" || srv.writes() != 1 {
		t.Fatalf("OnTime %v after %d writes, want 7 after one", v, srv.writes())
	}
}

// TestWriteNumericListIndexEndsTheInteractionParity: a write path with a
// numeric ListIndex is answered StatusResponse(INVALID_ACTION) and not
// written (AttributeWriteResponse.ts:419-424 at db021da0).
func TestWriteNumericListIndexEndsTheInteractionParity(t *testing.T) {
	t.Parallel()
	b, srv := onTimeBridge(t)
	p := newIMPeer(t, b)
	indexed := onTimePath()
	indexed.HasListIndex, indexed.ListIndex = true, 3
	p.send(0x4102, im.OpcodeWriteRequest, writeRequestBytes(t, im.WriteRequest{Writes: []im.AttributeWrite{
		{Path: indexed, Value: im.AttributeValue{Value: uint16(7)}},
	}}))
	if got := p.status(); got != im.StatusInvalidAction {
		t.Fatalf("status %v, want INVALID_ACTION", got)
	}
	if n := srv.writes(); n != 0 {
		t.Fatalf("%d writes, want 0", n)
	}
}

// TestWriteOtherNodeParity: a concrete write path naming a node other than
// this node's id on the session's fabric (none over PASE before AddNOC:
// UNSPECIFIED_NODE_ID) answers UNSUPPORTED_NODE and is not written
// (AttributeWriteResponse.ts:171-173, DataResponse.ts:54-63 at db021da0).
func TestWriteOtherNodeParity(t *testing.T) {
	t.Parallel()
	b, srv := onTimeBridge(t)
	p := newIMPeer(t, b)
	other := onTimePath()
	other.Node, other.HasNode = 0xDEADBEEF, true
	p.send(0x4103, im.OpcodeWriteRequest, writeRequestBytes(t, im.WriteRequest{Writes: []im.AttributeWrite{
		{Path: other, Value: im.AttributeValue{Value: uint16(7)}},
	}}))
	proto, body, ok := p.next(2 * time.Second)
	if !ok || proto.Opcode != im.OpcodeWriteResponse {
		t.Fatalf("reply %+v (ok %t), want a WriteResponse", proto, ok)
	}
	resp, err := im.UnmarshalWriteResponseTLV(tlv.NewDecoder(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Responses) != 1 || resp.Responses[0].Status.Status != im.StatusUnsupportedNode {
		t.Fatalf("responses %+v, want one UNSUPPORTED_NODE", resp.Responses)
	}
	if n := srv.writes(); n != 0 {
		t.Fatalf("%d writes, want 0", n)
	}
}
