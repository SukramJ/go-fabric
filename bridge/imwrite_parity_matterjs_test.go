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
	"sync/atomic"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/tlv"
	"thingfabric.dev/go-fabric/transport/message"
	"thingfabric.dev/go-fabric/transport/mrp"
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

// sendReliable dispatches one IM message on exchange with the R flag set,
// as an initiator message or not, and returns dispatch's error.
func (p *imPeer) sendReliable(exchange uint16, opcode uint8, initiator bool, payload []byte) error {
	p.t.Helper()
	proto := message.ProtocolHeader{ProtocolID: im.InteractionModelProtocolID, Opcode: opcode, ExchangeID: exchange, Initiator: initiator, NeedsAck: true}.Marshal()
	return p.b.dispatch(context.Background(), secureTestDatagram(p.t, p.b, proto, payload), p.conn.LocalAddr())
}

// received collects what the bridge sends within wait: the number of
// standalone acknowledgements and the opcodes of every other message.
func (p *imPeer) received(wait time.Duration) (acks int, others []uint8) {
	p.t.Helper()
	deadline := time.Now().Add(wait)
	buf := make([]byte, 2048)
	for {
		_ = p.conn.SetReadDeadline(deadline)
		n, _, err := p.conn.ReadFromUDP(buf)
		if err != nil {
			return acks, others
		}
		got := openTestReplyFor(p.t, p.b, buf[:n])
		_, hdrLen, err := message.UnmarshalHeader(got)
		if err != nil {
			p.t.Fatalf("UnmarshalHeader: %v", err)
		}
		proto, _, err := message.UnmarshalProtocolHeader(got[hdrLen:])
		if err != nil {
			p.t.Fatalf("UnmarshalProtocolHeader: %v", err)
		}
		if proto.ProtocolID == mrp.SecureChannelProtocolID && proto.Opcode == mrp.StandaloneAckOpcode {
			acks++
			continue
		}
		others = append(others, proto.Opcode)
	}
}

// silent reports that the bridge sends no IM message within wait.
func (p *imPeer) silent(wait time.Duration) bool {
	p.t.Helper()
	_, _, ok := p.next(wait)
	return !ok
}

func timedRequestBytes(t *testing.T, ms uint16) []byte {
	t.Helper()
	enc := tlv.NewEncoder()
	im.TimedRequest{TimeoutMs: ms}.MarshalTLV(enc)
	b, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestSecondTimedRequestParity: a TimedRequest on an exchange whose timed
// window still runs is answered INVALID_ACTION and ends the window
// (packages/protocol/src/protocol/MessageExchange.ts:1066-1070 at db021da0;
// connectedhomeip src/app/TimedHandler.cpp:91-96 at 6170af84 answers anything but
// the Invoke or Write the window waits for InvalidAction), so a following
// timed Invoke mismatches.
func TestSecondTimedRequestParity(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	b.AttachRootClusters([]contract.ClusterServer{&noopCluster{id: 0x003C}})
	p := newIMPeer(t, b)
	const x = 0x4201
	p.send(x, im.OpcodeTimedRequest, timedRequestBytes(t, 30000))
	if got := p.status(); got != im.StatusSuccess {
		t.Fatalf("first TimedRequest: %v, want SUCCESS", got)
	}
	p.send(x, im.OpcodeTimedRequest, timedRequestBytes(t, 30000))
	if got := p.status(); got != im.StatusInvalidAction {
		t.Fatalf("second TimedRequest: %v, want INVALID_ACTION", got)
	}
	p.send(x, im.OpcodeInvokeRequest, encodeTimedTestInvokeRequest(t, true, im.ConcreteCommandPath{
		Endpoint: 0, Cluster: 0x003C, Command: 0x02, HasEndpoint: true, HasCluster: true, HasCommand: true,
	}))
	if got := p.status(); got != im.StatusTimedRequestMismatch {
		t.Fatalf("timed Invoke after the refused TimedRequest: %v, want TIMED_REQUEST_MISMATCH", got)
	}
}

// TestExpiredTimedWindowParity: a timed window that expired answers the
// Write or Invoke that follows it TIMEOUT whatever the request's Timed
// flag says (InteractionServer.ts:426-429 write, :994-997 invoke at
// db021da0, both before the mismatch check; connectedhomeip
// src/app/TimedHandler.cpp:61-72 at 6170af84).
func TestExpiredTimedWindowParity(t *testing.T) {
	t.Parallel()
	for _, flag := range []bool{false, true} {
		b := &Bridge{}
		b.routing.timedDeadlines.Store(timedKey{sessionID: 4, exchangeID: 21}, time.Now().Add(-time.Millisecond))
		status, gated := b.checkTimedGate(flag, 4, 21)
		if !gated || status != im.StatusTimeout {
			t.Errorf("timed flag %t: gated %t status %v, want TIMEOUT", flag, gated, status)
		}
		if _, ok := b.routing.timedDeadlines.Load(timedKey{sessionID: 4, exchangeID: 21}); ok {
			t.Errorf("timed flag %t: the expired window is still present", flag)
		}
	}
}

// TestChunkedWriteInTimedWindowParity: a chunked write inside a running
// timed window is INVALID_ACTION whatever its Timed flag says — matter.js
// tests the window, not the flag, and before the timed gate
// (InteractionServer.ts:417-424 at db021da0).
func TestChunkedWriteInTimedWindowParity(t *testing.T) {
	t.Parallel()
	b, srv := onTimeBridge(t)
	p := newIMPeer(t, b)
	const x = 0x4202
	p.send(x, im.OpcodeTimedRequest, timedRequestBytes(t, 30000))
	if got := p.status(); got != im.StatusSuccess {
		t.Fatalf("TimedRequest: %v", got)
	}
	p.send(x, im.OpcodeWriteRequest, writeRequestBytes(t, im.WriteRequest{MoreChunkedMessages: true, Writes: []im.AttributeWrite{
		{Path: onTimePath(), Value: im.AttributeValue{Value: uint16(7)}},
	}}))
	if got := p.status(); got != im.StatusInvalidAction {
		t.Fatalf("chunked write, Timed flag clear, in a running window: %v, want INVALID_ACTION", got)
	}
	if n := srv.writes(); n != 0 {
		t.Fatalf("%d writes, want 0", n)
	}
}

// TestWriteContinuationChunkParity: of a chunk that continues a chunked
// write only its writes, MoreChunkedMessages and SuppressResponse are read
// (InteractionServer.ts:582-584 at db021da0): its Timed flag is not judged
// again, and a SuppressResponse on it ends the interaction with
// INVALID_ACTION, itself suppressed, the chunk not applied (:594-599).
func TestWriteContinuationChunkParity(t *testing.T) {
	t.Parallel()
	chunk := func(v uint16, more, suppress, timed bool) im.WriteRequest {
		return im.WriteRequest{MoreChunkedMessages: more, SuppressResponse: suppress, TimedRequest: timed, Writes: []im.AttributeWrite{
			{Path: onTimePath(), Value: im.AttributeValue{Value: v}},
		}}
	}
	t.Run("SuppressResponse on the final chunk", func(t *testing.T) {
		t.Parallel()
		b, srv := onTimeBridge(t)
		p := newIMPeer(t, b)
		const x = 0x4203
		p.send(x, im.OpcodeWriteRequest, writeRequestBytes(t, chunk(5, true, false, false)))
		if proto, _, ok := p.next(2 * time.Second); !ok || proto.Opcode != im.OpcodeWriteResponse {
			t.Fatalf("chunk 1: reply %+v (ok %t), want a WriteResponse", proto, ok)
		}
		p.send(x, im.OpcodeWriteRequest, writeRequestBytes(t, chunk(9, false, true, false)))
		if !p.silent(300 * time.Millisecond) {
			t.Error("the bridge answered a suppressed continuation chunk")
		}
		if v, _ := srv.MatterRead(0x4001); fmt.Sprint(v) != "5" || srv.writes() != 1 {
			t.Fatalf("OnTime %v after %d writes, want 5 after one", v, srv.writes())
		}
	})
	t.Run("Timed flag on a continuation chunk", func(t *testing.T) {
		t.Parallel()
		b, srv := onTimeBridge(t)
		p := newIMPeer(t, b)
		const x = 0x4204
		p.send(x, im.OpcodeWriteRequest, writeRequestBytes(t, chunk(5, true, false, false)))
		if proto, _, ok := p.next(2 * time.Second); !ok || proto.Opcode != im.OpcodeWriteResponse {
			t.Fatalf("chunk 1: reply %+v (ok %t), want a WriteResponse", proto, ok)
		}
		p.send(x, im.OpcodeWriteRequest, writeRequestBytes(t, chunk(9, false, false, true)))
		if proto, _, ok := p.next(2 * time.Second); !ok || proto.Opcode != im.OpcodeWriteResponse {
			t.Fatalf("chunk 2: reply %+v (ok %t), want a WriteResponse", proto, ok)
		}
		if v, _ := srv.MatterRead(0x4001); fmt.Sprint(v) != "9" || srv.writes() != 2 {
			t.Fatalf("OnTime %v after %d writes, want 9 after two", v, srv.writes())
		}
	})
}

// TestNonInitiatorRequestIgnoredParity: an IM request that is not an
// initiator message, on an exchange this node did not open, is not served
// — matter.js opens an exchange for a protocol handler only for an
// initiator message and otherwise acknowledges and ignores it
// (packages/protocol/src/protocol/ExchangeManager.ts:462-467, :496-507 at
// db021da0; the standalone acknowledgement at :502, sent at once). The
// acknowledgement is the one message the bridge sends for it, and it goes
// out when the request is ignored, not after the pump's grace window.
func TestNonInitiatorRequestIgnoredParity(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	srv := &countingInvokeServer{id: 0x0006}
	b.AttachRootClusters([]contract.ClusterServer{srv})
	b.AttachAckTracker(mrp.NewAckTracker(time.Hour)) // attached after Start: no pump
	p := newIMPeer(t, b)
	payload := encodeTimedTestInvokeRequest(t, false, im.ConcreteCommandPath{Endpoint: 0, Cluster: 0x0006, Command: 0x01, HasEndpoint: true, HasCluster: true, HasCommand: true})
	if err := p.sendReliable(0x4205, im.OpcodeInvokeRequest, false, payload); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if acks, others := p.received(300 * time.Millisecond); acks != 1 || len(others) != 0 {
		t.Errorf("the bridge sent %d standalone acks and the messages %v, want one standalone ack and nothing else", acks, others)
	}
	if n := srv.invokes.Load(); n != 0 {
		t.Errorf("the command ran %d times, want 0", n)
	}
}

// countingInvokeServer serves cluster id and counts the commands it runs.
type countingInvokeServer struct {
	id      uint32
	invokes atomic.Int32
}

func (s *countingInvokeServer) MatterClusterID() uint32                        { return s.id }
func (s *countingInvokeServer) MatterRead(uint32) (any, bool)                  { return nil, false }
func (s *countingInvokeServer) MatterWrite(context.Context, uint32, any) error { return nil }
func (s *countingInvokeServer) MatterReportable() []uint32                     { return nil }

func (s *countingInvokeServer) MatterInvoke(context.Context, uint32, any) (any, error) {
	s.invokes.Add(1)
	return nil, nil
}
