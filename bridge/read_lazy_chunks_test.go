// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

// White-box tests for decision 44 of review II: a Read's response and a
// Subscribe's priming report are generated chunk by chunk, as matter.js
// does (packages/protocol/src/action/server/AttributeReadResponse.ts
// *process: one producer per path, a chunk emitted lazily when the
// endpoint changes), not materialised whole and then cut. What the
// interaction holds while it waits for the peer's answer to a chunk is
// the cursor and the chunk in flight — measured here as the attribute
// results the dispatcher has produced but no chunk has carried yet.

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/udp"
)

// wideCountingDispatcher is a node of endpoints × attrs attributes (one
// cluster per endpoint) that counts every result it hands out.
type wideCountingDispatcher struct {
	endpoints, attrs int
	produced         atomic.Int64
}

func (d *wideCountingDispatcher) endpointResults(ep uint16, path im.ConcreteAttributePath) []im.ReadResult {
	const cluster = 0x0006
	if path.HasCluster && path.Cluster != cluster {
		return nil
	}
	out := make([]im.ReadResult, 0, d.attrs)
	for a := range d.attrs {
		if path.HasAttribute && path.Attribute != uint32(a) {
			continue
		}
		out = append(out, im.ReadResult{
			Path: im.ConcreteAttributePath{
				Endpoint: ep, Cluster: cluster, Attribute: uint32(a),
				HasEndpoint: true, HasCluster: true, HasAttribute: true,
			},
			Value:       im.AttributeValue{Value: uint32(a)},
			Status:      im.StatusSuccess,
			DataVersion: 7,
		})
	}
	d.produced.Add(int64(len(out)))
	return out
}

func (d *wideCountingDispatcher) Read(_ context.Context, path im.ConcreteAttributePath) []im.ReadResult {
	if path.HasEndpoint {
		if int(path.Endpoint) >= d.endpoints {
			return []im.ReadResult{{Path: path, Status: im.StatusUnsupportedEndpoint}}
		}
		return d.endpointResults(path.Endpoint, path)
	}
	var out []im.ReadResult
	for ep := range d.endpoints {
		out = append(out, d.endpointResults(uint16(ep), path)...)
	}
	return out
}

func (d *wideCountingDispatcher) ExpansionEndpoints(im.ConcreteAttributePath) []uint16 {
	out := make([]uint16, d.endpoints)
	for i := range out {
		out[i] = uint16(i)
	}
	return out
}

func (d *wideCountingDispatcher) ReadAtEndpoint(_ context.Context, path im.ConcreteAttributePath, ep uint16) []im.ReadResult {
	return d.endpointResults(ep, path)
}

func (d *wideCountingDispatcher) Write(context.Context, im.ConcreteAttributePath, im.AttributeValue) []im.WriteResult {
	return nil
}

func (d *wideCountingDispatcher) Invoke(context.Context, im.ConcreteCommandPath, any) im.InvokeResult {
	return im.InvokeResult{}
}

// repeatedWildcardPaths are n full-wildcard paths that differ only in a
// field the expansion ignores (WildcardFilterConfigurationVersion without
// WildcardPathFlags), so no repeat of an identical path is dropped.
func repeatedWildcardPaths(n int) []im.ConcreteAttributePath {
	out := make([]im.ConcreteAttributePath, n)
	for k := range out {
		out[k] = im.ConcreteAttributePath{
			WildcardFilterConfigurationVersion:    uint32(k),
			HasWildcardFilterConfigurationVersion: true,
		}
	}
	return out
}

// readReportChunk reads the next ReportData chunk the bridge sent to the
// peer and returns its header, protocol header and attribute report count.
func readReportChunk(t *testing.T, peerConn *net.UDPConn, peerSess *channel.Session) (message.Header, message.ProtocolHeader, int, bool) {
	t.Helper()
	buf := make([]byte, udp.MaxDatagramSize)
	for {
		if err := peerConn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
			t.Fatalf("SetReadDeadline: %v", err)
		}
		n, from, err := peerConn.ReadFromUDP(buf)
		if err != nil {
			t.Fatalf("no chunk: %v", err)
		}
		if port, ok := chunkPeerBridgePort.Load(peerConn); ok && from.Port != port.(int) {
			continue // another test's traffic
		}
		hdr, hdrLen, err := message.UnmarshalHeader(buf[:n])
		if err != nil {
			t.Fatalf("UnmarshalHeader: %v", err)
		}
		plain, _, err := peerSess.Decrypt(&hdr, securityFlagsByte(&hdr), buf[hdrLen:n])
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}
		proto, protoLen, err := message.UnmarshalProtocolHeader(plain)
		if err != nil {
			t.Fatalf("UnmarshalProtocolHeader: %v", err)
		}
		if proto.Opcode != im.OpcodeReportData {
			continue // a standalone ack
		}
		rd, err := im.UnmarshalReportDataTLV(tlv.NewDecoder(plain[protoLen:]), nil, nil)
		if err != nil {
			t.Fatalf("UnmarshalReportDataTLV: %v", err)
		}
		return hdr, proto, len(rd.Reports), rd.MoreChunkedMessages
	}
}

// assertChunksStreamed answers the first chunks of an interaction and,
// while the bridge waits on each, asserts that the results the dispatcher
// has produced exceed what the chunks carried by no more than one chunk's
// worth plus one endpoint's attributes (the cursor's batch). It then
// refuses a chunk, which ends the interaction.
func assertChunksStreamed(t *testing.T, b *Bridge, d *wideCountingDispatcher, peerConn *net.UDPConn, peerSess *channel.Session) {
	t.Helper()
	const checked = 4
	sent := int64(0)
	for k := range checked {
		hdr, proto, reports, more := readReportChunk(t, peerConn, peerSess)
		if !more {
			t.Fatalf("chunk %d is the last; the report should span many chunks", k)
		}
		sent += int64(reports)
		// The bridge is blocked on this chunk's StatusResponse: nothing
		// more is produced until the answer below.
		held := d.produced.Load() - sent
		if bound := int64(reports + d.attrs); held > bound {
			t.Fatalf("chunk %d: %d results produced but not yet sent while the chunk waits; want at most %d (one chunk of %d reports plus one endpoint of %d attributes) — the response was materialised before it was cut",
				k, held, bound, reports, d.attrs)
		}
		status := im.StatusSuccess
		if k == checked-1 {
			status = im.StatusFailure
		}
		b.signalStatusResponseRX(hdr.SessionID, proto.ExchangeID, proto.Initiator, status)
	}
}

// TestReadResponseIsGeneratedChunkByChunk: a Read of 200 wildcard paths
// that differ only in an ignored field, against a node of 500 attributes,
// holds no more than one chunk plus one endpoint's results across a chunk
// wait (review II, R2-P2-alloc-04).
func TestReadResponseIsGeneratedChunkByChunk(t *testing.T) {
	t.Parallel()
	const sessionID uint16 = 91
	b := newStartedBridge(t)
	peerConn, peerSess, peerAddr := chunkTestPeer(t, b, sessionID)
	markPASE(b, sessionID)
	d := &wideCountingDispatcher{endpoints: 20, attrs: 25}

	req := im.ReadRequest{AttributeRequests: repeatedWildcardPaths(200)}
	hdr := &message.Header{SessionID: sessionID, MessageCounter: 5000}
	proto := message.ProtocolHeader{ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeReadRequest, ExchangeID: 0x2201, Initiator: true}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- b.dispatchReadRequest(ctx, peerAddr, hdr, proto, d, req) }()

	assertChunksStreamed(t, b, d, peerConn, peerSess)
	if err := <-done; err == nil {
		t.Error("dispatchReadRequest returned nil after the peer refused a chunk")
	}
}

// TestPrimingReportIsGeneratedChunkByChunk is the Subscribe twin: the
// priming report of the same request streams under the same bound.
func TestPrimingReportIsGeneratedChunkByChunk(t *testing.T) {
	t.Parallel()
	const sessionID uint16 = 92
	b := newStartedBridge(t)
	b.AttachSubscriptionManager(subscription.NewManager(subscription.Config{}, nil, nil))
	peerConn, peerSess, peerAddr := chunkTestPeer(t, b, sessionID)
	markPASE(b, sessionID)
	d := &wideCountingDispatcher{endpoints: 20, attrs: 25}

	req := im.SubscribeRequest{AttributeRequests: repeatedWildcardPaths(200), MaxIntervalCeiling: 60}
	hdr := &message.Header{SessionID: sessionID, MessageCounter: 6000}
	proto := message.ProtocolHeader{ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeSubscribeRequest, ExchangeID: 0x2202, Initiator: true}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- b.subscribeWith(ctx, d, peerAddr, hdr, proto, req) }()

	assertChunksStreamed(t, b, d, peerConn, peerSess)
	if err := <-done; err == nil {
		t.Error("subscribeWith returned nil after the peer refused a chunk")
	}
}
