// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"log/slog"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/contract"
	endpointpkg "thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/im/subscription"
	"thingfabric.dev/go-fabric/internal/valueenc"
	"thingfabric.dev/go-fabric/mdns"
	"thingfabric.dev/go-fabric/tlv"
	"thingfabric.dev/go-fabric/transport/message"
)

// failingValueW625 is a host value whose EncodeTLV fails the way kind
// names; the empty kind encodes a struct of one field.
type failingValueW625 struct{ kind string }

// failingKindsW625 are the ways a host encoder fails (#625): one or two
// containers left open, a panic, no element, two elements.
var failingKindsW625 = []string{"one open", "two open", "panic", "zero elements", "two elements"}

func (v failingValueW625) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	switch v.kind {
	case "":
		enc.StartStruct(tag)
		enc.PutUint(tlv.ContextTag(0), 1)
		_ = enc.EndContainer()
	case "one open":
		enc.StartStruct(tag)
	case "two open":
		enc.StartStruct(tag)
		enc.StartList(tlv.ContextTag(0))
	case "panic":
		const p = "host encoder bug"
		valueenc.ExpectPanic(p)
		panic(p)
	case "zero elements":
	case "two elements":
		enc.PutUint(tag, 1)
		enc.PutUint(tag, 2)
	}
}

// failingServerW625 is a root cluster server whose attribute 0 is a
// [failingValueW625] of the kind it holds and whose attribute 1 is true.
type failingServerW625 struct{ kind atomic.Pointer[string] }

func (*failingServerW625) MatterClusterID() uint32 { return 0x0006 }

func (s *failingServerW625) MatterRead(attrID uint32) (any, bool) {
	switch attrID {
	case 0:
		kind := ""
		if k := s.kind.Load(); k != nil {
			kind = *k
		}
		return failingValueW625{kind: kind}, true
	case 1:
		return true, true
	}
	return nil, false
}

func (s *failingServerW625) set(kind string) { s.kind.Store(&kind) }

func (*failingServerW625) MatterReportable() []uint32                             { return []uint32{0, 1} }
func (*failingServerW625) MatterWrite(context.Context, uint32, any) error         { return nil }
func (*failingServerW625) MatterInvoke(context.Context, uint32, any) (any, error) { return nil, nil }

// newLoggedBridgeW625 is newStartedBridge with its log captured, the
// server attached as a root cluster and the PASE test peer attached.
func newLoggedBridgeW625(t *testing.T, srv contract.ClusterServer) (*Bridge, *logSinkW2B10) {
	t.Helper()
	logs := &logSinkW2B10{}
	b, err := New(wbEmptySnapshotter, mdns.NewNoop(), Config{Listen: "127.0.0.1:0"},
		slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := b.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer stopCancel()
		_ = b.Stop(stopCtx)
	})
	if srv != nil {
		b.AttachRootClusters([]contract.ClusterServer{srv})
		if err := b.Reassemble(context.Background()); err != nil {
			t.Fatalf("Reassemble: %v", err)
		}
	}
	attachPASETestPeer(t, b)
	return b, logs
}

// sentW625 is one IM message the bridge sent the peer: its opcode and,
// for a ReportData, the report.
type sentW625 struct {
	opcode uint8
	report im.ReportData
}

// answerAndDecodeW625 is answerReportChunks that also decodes each
// ReportData it answers.
func answerAndDecodeW625(t *testing.T, b *Bridge, peerConn *net.UDPConn) <-chan sentW625 {
	t.Helper()
	out := make(chan sentW625, 16)
	go func() {
		defer close(out)
		for {
			_ = peerConn.SetReadDeadline(time.Now().Add(3 * time.Second))
			buf := make([]byte, 1500)
			n, _, err := peerConn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			got := openTestReplyFor(t, b, buf[:n])
			hdr, hdrLen, err := message.UnmarshalHeader(got)
			if err != nil {
				return
			}
			proto, protoLen, err := message.UnmarshalProtocolHeader(got[hdrLen:])
			if err != nil || proto.ProtocolID != im.InteractionModelProtocolID {
				continue
			}
			s := sentW625{opcode: proto.Opcode}
			if proto.Opcode == im.OpcodeReportData {
				if s.report, err = im.UnmarshalReportDataTLV(tlv.NewDecoder(got[hdrLen+protoLen:]), nil, nil); err != nil {
					return
				}
				b.signalStatusResponseRX(hdr.SessionID, proto.ExchangeID, proto.Initiator, im.StatusSuccess)
			}
			out <- s
		}
	}()
	return out
}

// nextSentW625 takes the next IM message of opcode the peer saw; the
// peer's read deadline bounds the wait (the channel closes).
func nextSentW625(t *testing.T, sent <-chan sentW625, opcode uint8) sentW625 {
	t.Helper()
	s, ok := <-sent
	if !ok {
		t.Fatal("peer saw no further message before its read deadline")
	}
	if s.opcode != opcode {
		t.Fatalf("message opcode 0x%02X, want 0x%02X", s.opcode, opcode)
	}
	return s
}

// checkFailedAttributeW625 holds a report's two attribute entries: the
// failing attribute 0 answered FAILURE on its bare path, attribute 1
// delivered as data.
func checkFailedAttributeW625(t *testing.T, what string, reports []im.AttributeReport) {
	t.Helper()
	var failed, delivered bool
	for _, r := range reports {
		switch {
		case r.Path.Attribute == 0 && r.IsStatus && r.Status.Status == im.StatusFailure && !r.Path.HasListIndex && !r.Path.ListAppend:
			failed = true
		case r.Path.Attribute == 1 && !r.IsStatus:
			delivered = true
		}
	}
	if !failed || !delivered {
		t.Fatalf("%s: reports %+v; want FAILURE for attribute 0 and attribute 1 delivered", what, reports)
	}
}

// waitLogW625 checks want is in the captured log. The failure is logged
// before the message that carries its status is sent, so the log holds it
// once the peer has seen that message.
func waitLogW625(t *testing.T, logs *logSinkW2B10, want string) {
	t.Helper()
	if !strings.Contains(logs.String(), want) {
		t.Fatalf("log lacks %q; log = %q", want, logs.String())
	}
}

const encodeFailedLogW625 = "level=ERROR msg=matter.attribute.encode_failed subsystem=matter.bridge endpoint=0 cluster=6 attribute=0"

// TestHostEncoderFailureCostsItsPathOnReadW625 (#625): a Read of a host
// attribute whose encoder fails — one or two containers left open, a
// panic, no element or two — is answered: FAILURE for that path, the
// other attribute delivered, an ERROR log naming the path. Red before
// #625: the chunker's encode failed (or panicked) and no report went out.
func TestHostEncoderFailureCostsItsPathOnReadW625(t *testing.T) {
	t.Parallel()
	for i, kind := range failingKindsW625 {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			srv := &failingServerW625{}
			srv.set(kind)
			b, logs := newLoggedBridgeW625(t, srv)
			peerConn, peerAddr := newSubscribeTestPeer(t)
			sent := answerAndDecodeW625(t, b, peerConn)
			hdr := &message.Header{SessionID: paseTestSessionID, MessageCounter: uint32(500 + i)}
			proto := message.ProtocolHeader{ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeReadRequest, ExchangeID: uint16(0x6250 + i), Initiator: true}
			req := im.ReadRequest{AttributeRequests: []im.ConcreteAttributePath{
				{Endpoint: 0, HasEndpoint: true, Cluster: 0x0006, HasCluster: true, Attribute: 0, HasAttribute: true},
				{Endpoint: 0, HasEndpoint: true, Cluster: 0x0006, HasCluster: true, Attribute: 1, HasAttribute: true},
			}}
			if err := b.dispatchReadRequest(context.Background(), peerAddr, hdr, proto, b.Dispatcher(), req); err != nil {
				t.Fatalf("dispatchReadRequest: %v", err)
			}
			rd := nextSentW625(t, sent, im.OpcodeReportData).report
			checkFailedAttributeW625(t, "read", rd.Reports)
			waitLogW625(t, logs, encodeFailedLogW625)
		})
	}
}

// TestHostEncoderFailureCostsItsPathOnSubscribeW625 (#625): over a
// subscription's life, a host attribute whose encoder fails costs its
// path alone — in the priming report (the SubscribeResponse still goes
// out and the subscription is established) and in an ongoing report (the
// subscription and the process live, the next good value is reported).
// Red before #625: the priming failed the Subscribe; the ongoing report
// panicked (dropped by the engine's backstop) or failed to encode.
func TestHostEncoderFailureCostsItsPathOnSubscribeW625(t *testing.T) {
	t.Parallel()
	for i, kind := range failingKindsW625 {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			srv := &failingServerW625{}
			srv.set(kind)
			b, logs := newLoggedBridgeW625(t, srv)
			mgr := subscription.NewManager(subscription.Config{}, nil, nil)
			b.AttachSubscriptionManager(mgr)
			path := im.ConcreteAttributePath{Endpoint: 0, HasEndpoint: true, Cluster: 0x0006, HasCluster: true, Attribute: 0, HasAttribute: true}
			other := path
			other.Attribute = 1
			peerConn, peerAddr := newSubscribeTestPeer(t)
			sent := answerAndDecodeW625(t, b, peerConn)
			hdr := &message.Header{SessionID: paseTestSessionID, MessageCounter: uint32(600 + i)}
			proto := message.ProtocolHeader{ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeSubscribeRequest, ExchangeID: uint16(0x6260 + i)}
			req := im.SubscribeRequest{AttributeRequests: []im.ConcreteAttributePath{path, other}, MaxIntervalCeiling: 60}
			if err := b.handleSubscribeRequest(context.Background(), peerAddr, hdr, proto, req); err != nil {
				t.Fatalf("handleSubscribeRequest: %v", err)
			}
			checkFailedAttributeW625(t, "priming", nextSentW625(t, sent, im.OpcodeReportData).report.Reports)
			nextSentW625(t, sent, im.OpcodeSubscribeResponse)
			if n := mgr.Active(); n != 1 {
				t.Fatalf("mgr.Active() = %d after the priming, want the subscription established", n)
			}
			waitLogW625(t, logs, encodeFailedLogW625)

			t0 := time.Now()
			srv.set("")
			mgr.OnAttributeChanged(path)
			mgr.Tick(context.Background(), t0.Add(2*time.Second))
			if r := nextSentW625(t, sent, im.OpcodeReportData).report.Reports; len(r) != 1 || r[0].IsStatus {
				t.Fatalf("report of the good value %+v, want its data", r)
			}

			srv.set(kind)
			mgr.OnAttributeChanged(path)
			mgr.Tick(context.Background(), t0.Add(4*time.Second))
			r := nextSentW625(t, sent, im.OpcodeReportData).report.Reports
			if len(r) != 1 || !r[0].IsStatus || r[0].Status.Status != im.StatusFailure || r[0].Path.Attribute != 0 {
				t.Fatalf("ongoing report %+v, want FAILURE for attribute 0", r)
			}
			if n := mgr.Active(); n != 1 {
				t.Fatalf("mgr.Active() = %d after the failing report, want the subscription kept", n)
			}

			srv.set("")
			mgr.OnAttributeChanged(path)
			mgr.Tick(context.Background(), t0.Add(6*time.Second))
			if r := nextSentW625(t, sent, im.OpcodeReportData).report.Reports; len(r) != 1 || r[0].IsStatus {
				t.Fatalf("report after the failing one %+v, want the good value's data", r)
			}
		})
	}
}

// failingInvokerW625 answers command 1 with a response whose encoder
// fails the way kind names and every other command with a good one; each
// response names its command + 0x80.
type failingInvokerW625 struct {
	im.Dispatcher
	kind string
}

func (d failingInvokerW625) Invoke(_ context.Context, p im.ConcreteCommandPath, _ any) im.InvokeResult {
	resp := p
	resp.Command = 0x80 + p.Command
	if p.Command == 1 {
		return im.InvokeResult{Path: resp, Status: im.StatusSuccess, Response: failingValueW625{kind: d.kind}}
	}
	return im.InvokeResult{Path: resp, Status: im.StatusSuccess, Response: failingValueW625{}}
}

// TestHostEncoderFailureCostsItsCommandW625 (#625): a batch invoke whose
// first command's response encoder fails is answered: that command a
// CommandStatusIB FAILURE on the request's path (command 1, not the
// response's 0x81) with its CommandRef, the other command its response;
// an ERROR log names the command. Red before #625: the response did not
// encode and no InvokeResponse went out (or the encode panicked).
func TestHostEncoderFailureCostsItsCommandW625(t *testing.T) {
	t.Parallel()
	for i, kind := range failingKindsW625 {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			b, logs := newLoggedBridgeW625(t, nil)
			peerConn, peerAddr := newSubscribeTestPeer(t)
			cmd := func(id uint32, ref uint16) im.CommandInvocation {
				return im.CommandInvocation{
					Path:       im.ConcreteCommandPath{Endpoint: 1, Cluster: 0xFFF1FC01, Command: id, HasEndpoint: true, HasCluster: true, HasCommand: true},
					CommandRef: ref, HasCommandRef: true,
				}
			}
			req := im.InvokeRequest{Invokes: []im.CommandInvocation{cmd(1, 7), cmd(2, 8)}}
			proto := message.ProtocolHeader{ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeInvokeRequest, ExchangeID: uint16(0x6270 + i), Initiator: true}
			hdr := &message.Header{MessageCounter: uint32(700 + i), HasSourceNodeID: true, SourceNodeID: 0x1234}
			if err := b.dispatchInvokeRequest(im.WithAuthModePASE(context.Background()), peerAddr, hdr, proto, failingInvokerW625{kind: kind}, req); err != nil {
				t.Fatalf("dispatchInvokeRequest: %v", err)
			}
			buf := make([]byte, 1500)
			for {
				_ = peerConn.SetReadDeadline(time.Now().Add(3 * time.Second))
				n, _, err := peerConn.ReadFromUDP(buf)
				if err != nil {
					t.Fatalf("no InvokeResponse: %v", err)
				}
				_, hl, err := message.UnmarshalHeader(buf[:n])
				if err != nil {
					t.Fatal(err)
				}
				p, pl, err := message.UnmarshalProtocolHeader(buf[hl:n])
				if err != nil || p.ProtocolID != im.InteractionModelProtocolID {
					continue
				}
				if p.Opcode != im.OpcodeInvokeResponse {
					t.Fatalf("IM opcode 0x%02X, want InvokeResponse", p.Opcode)
				}
				resp, err := im.UnmarshalInvokeResponseTLV(tlv.NewDecoder(buf[hl+pl:n]), nil)
				if err != nil {
					t.Fatal(err)
				}
				if len(resp.Responses) != 2 {
					t.Fatalf("%d responses, want 2: %+v", len(resp.Responses), resp.Responses)
				}
				bad, good := resp.Responses[0], resp.Responses[1]
				if !bad.IsStatus || bad.Status.Status != im.StatusFailure || bad.Path.Command != 1 || bad.Path.Endpoint != 1 || !bad.HasCommandRef || bad.CommandRef != 7 {
					t.Fatalf("failing command answered %+v, want FAILURE on command 1 with CommandRef 7", bad)
				}
				if good.IsStatus || good.Path.Command != 0x82 || good.CommandRef != 8 {
					t.Fatalf("good command answered %+v, want response 0x82 with CommandRef 8", good)
				}
				break
			}
			waitLogW625(t, logs, "level=ERROR msg=matter.command.encode_failed subsystem=matter.bridge endpoint=1 cluster=4294048769 command=1")
		})
	}
}

// TestChunkInvokeResponseRunsTheWriterOnce (#625): each command response
// is encoded once, however often the chunking measures and encodes the
// response — before #625 every trial encode ran the writer again.
func TestChunkInvokeResponseRunsTheWriterOnce(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	entries := make([]im.InvokeResponseEntry, 0, 3)
	for i := range 3 {
		entries = append(entries, im.InvokeResponseEntry{
			Path:     im.ConcreteCommandPath{Endpoint: 1, Cluster: 6, Command: uint32(i), HasEndpoint: true, HasCluster: true, HasCommand: true},
			Response: countingResponseW625{calls: &calls, n: 700}, HasResponse: true,
			CommandRef: uint16(i), HasCommandRef: true,
		})
	}
	chunks, err := chunkInvokeResponse(nil, im.InvokeRequest{}, im.InvokeResponse{Responses: entries}, reportChunkPayloadBudget, reportChunkHardCap)
	if err != nil || len(chunks) < 2 {
		t.Fatalf("%d chunks, %v; want the response split", len(chunks), err)
	}
	if n := calls.Load(); n != 3 {
		t.Fatalf("the response writer ran %d times for 3 responses, want once each", n)
	}
}

// countingResponseW625 is a response of n octets that counts its encodes.
type countingResponseW625 struct {
	calls *atomic.Int32
	n     int
}

func (r countingResponseW625) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	r.calls.Add(1)
	enc.StartStruct(tag)
	enc.PutOctets(tlv.ContextTag(0), make([]byte, r.n))
	_ = enc.EndContainer()
}

// TestEmitEventWithAPayloadThatCannotBeEncodedW625 (#625): an event whose
// payload encoder fails is refused when it is emitted — not recorded, no
// event number consumed, no panic into the emitter, an ERROR log naming
// it; the next good event takes the number the refused one would have.
// Red before #625: the event was recorded and failed every report that
// carried it.
func TestEmitEventWithAPayloadThatCannotBeEncodedW625(t *testing.T) {
	t.Parallel()
	for _, kind := range failingKindsW625 {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			b, logs := newLoggedBridgeW625(t, nil)
			var numbers []uint64
			b.SetOnEvent(func(rec im.EventRecord) { numbers = append(numbers, rec.Number) })
			b.EmitEvent(1, 0x003B, 1, failingValueW625{}, contract.EventPriorityInfo)
			b.EmitEvent(1, 0x003B, 2, failingValueW625{kind: kind}, contract.EventPriorityInfo)
			b.EmitEvent(1, 0x003B, 1, failingValueW625{}, contract.EventPriorityInfo)
			if len(numbers) != 2 || numbers[1] != numbers[0]+1 {
				t.Fatalf("event numbers %v, want two consecutive: the failing event consumed none", numbers)
			}
			if recs := b.EventLog().Query(1, 0x003B, 2, 0); len(recs) != 0 {
				t.Fatalf("the failing event was recorded: %+v", recs)
			}
			waitLogW625(t, logs, "level=ERROR msg=matter.event.encode_failed subsystem=matter.bridge endpoint=1 cluster=59 event=2 err=")
		})
	}
}

// laterFailingValueW625 encodes on its first call and fails as one open
// container on every later one: an event payload that passes the check
// at emit and fails when it is reported.
type laterFailingValueW625 struct{ calls *atomic.Int32 }

func (v laterFailingValueW625) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	if v.calls.Add(1) == 1 {
		failingValueW625{}.EncodeTLV(enc, tag)
		return
	}
	enc.StartStruct(tag)
}

// TestReadEventWhosePayloadFailsAtReportTimeW625 (#625): the report-time
// floor — an event payload that encodes when it is emitted but fails when
// a Read reports it is answered EventStatusIB FAILURE for its path; the
// other event is delivered, and the ERROR log names it. Red before #625:
// the chunker's encode failed and the Read went unanswered.
func TestReadEventWhosePayloadFailsAtReportTimeW625(t *testing.T) {
	t.Parallel()
	b, logs := newLoggedBridgeW625(t, nil)
	peerConn, peerAddr := newSubscribeTestPeer(t)
	sent := answerAndDecodeW625(t, b, peerConn)
	b.EmitEvent(1, 0x003B, 1, failingValueW625{}, contract.EventPriorityInfo)
	b.EmitEvent(1, 0x003B, 2, laterFailingValueW625{calls: &atomic.Int32{}}, contract.EventPriorityInfo)
	hdr := &message.Header{SessionID: paseTestSessionID, MessageCounter: 800}
	proto := message.ProtocolHeader{ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeReadRequest, ExchangeID: 0x6280, Initiator: true}
	req := im.ReadRequest{EventRequests: []im.ConcreteEventPath{{Endpoint: 1, HasEndpoint: true, Cluster: 0x003B, HasCluster: true}}}
	if err := b.dispatchReadRequest(context.Background(), peerAddr, hdr, proto, failingInvokerW625{}, req); err != nil {
		t.Fatalf("dispatchReadRequest: %v", err)
	}
	evs := nextSentW625(t, sent, im.OpcodeReportData).report.EventReports
	var failed, delivered bool
	for _, ev := range evs {
		switch {
		case ev.Path.Event == 2 && ev.IsStatus && ev.Status.Status == im.StatusFailure:
			failed = true
		case ev.Path.Event == 1 && !ev.IsStatus:
			delivered = true
		}
	}
	if !failed || !delivered {
		t.Fatalf("event reports %+v; want FAILURE for event 2 and event 1 delivered", evs)
	}
	waitLogW625(t, logs, "level=ERROR msg=matter.event.encode_failed subsystem=matter.bridge endpoint=1 cluster=59 event=2 status=Failure")
}

// TestAlwaysFailingValueIsNoChangeW625 (#625, review L4): a value whose
// encoder always fails compares equal to itself, so the change filter
// reports it once when it turns bad and not again on every check — no
// FAILURE report, and no ERROR line, per tick.
func TestAlwaysFailingValueIsNoChangeW625(t *testing.T) {
	t.Parallel()
	for _, kind := range failingKindsW625 {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			srv := &failingServerW625{}
			ep := &endpointpkg.Endpoint{ID: 5}
			ep.PublishClusterServers([]contract.ClusterServer{srv})
			path := im.ConcreteAttributePath{Endpoint: 5, Cluster: 0x0006, Attribute: 0, HasEndpoint: true, HasCluster: true, HasAttribute: true}
			v := newPathValues(ep, []im.ConcreteAttributePath{path})
			srv.set(kind)
			if got := v.changed(); len(got) != 1 {
				t.Fatalf("the value turned bad: %v, want it reported once", got)
			}
			for i := range 3 {
				if got := v.changed(); len(got) != 0 {
					t.Fatalf("check %d of the unchanged failing value: %v, want quiet", i, got)
				}
			}
			srv.set("")
			if got := v.changed(); len(got) != 1 {
				t.Fatalf("the value turned good: %v, want it reported", got)
			}
		})
	}
}

// TestFailingResponseInAChunkedBatchW625 (#625, review L4): a batch whose
// responses need several messages, one of them failing, is chunked with
// that command answered FAILURE on its request path with its CommandRef
// and the others' responses delivered.
func TestFailingResponseInAChunkedBatchW625(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	path := func(cmd uint32) im.ConcreteCommandPath {
		return im.ConcreteCommandPath{Endpoint: 1, Cluster: 6, Command: cmd, HasEndpoint: true, HasCluster: true, HasCommand: true}
	}
	req := im.InvokeRequest{Invokes: []im.CommandInvocation{
		{Path: path(1), CommandRef: 1, HasCommandRef: true},
		{Path: path(2), CommandRef: 2, HasCommandRef: true},
		{Path: path(3), CommandRef: 3, HasCommandRef: true},
	}}
	resp := im.InvokeResponse{Responses: []im.InvokeResponseEntry{
		{Path: path(0x81), Response: countingResponseW625{calls: &calls, n: 700}, HasResponse: true, CommandRef: 1, HasCommandRef: true},
		{Path: path(0x82), Response: failingValueW625{kind: "two open"}, HasResponse: true, CommandRef: 2, HasCommandRef: true},
		{Path: path(0x83), Response: countingResponseW625{calls: &calls, n: 700}, HasResponse: true, CommandRef: 3, HasCommandRef: true},
	}}
	chunks, err := chunkInvokeResponse(nil, req, resp, reportChunkPayloadBudget, reportChunkHardCap)
	if err != nil || len(chunks) < 2 {
		t.Fatalf("%d chunks, %v; want the batch chunked", len(chunks), err)
	}
	var got []im.InvokeResponseEntry
	for _, c := range chunks {
		r, err := im.UnmarshalInvokeResponseTLV(tlv.NewDecoder(c), nil)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, r.Responses...)
	}
	if len(got) != 3 {
		t.Fatalf("%d entries over the chunks, want 3: %+v", len(got), got)
	}
	for _, e := range got {
		failing := e.CommandRef == 2
		if failing != e.IsStatus || (failing && (e.Status.Status != im.StatusFailure || e.Path.Command != 2)) {
			t.Fatalf("entry %+v: want FAILURE on command 2 for CommandRef 2 and data otherwise", e)
		}
	}
}
