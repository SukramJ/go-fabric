// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction_test

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/groups"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// servedCluster is the manufacturer-specific cluster the fake dispatcher
// serves: attribute n reads as a string of sizes[n] bytes, command n
// answers with a blob of sizes[n] bytes.
const servedCluster uint32 = 0xFFF1FC00

// oversized is a size no single message carries.
const oversized = 2000

// blob is a response payload of n bytes.
type blob int

func (b blob) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	enc.PutOctets(tlv.ContextTag(0), bytes.Repeat([]byte{0xA5}, int(b)))
	_ = enc.EndContainer()
}

// fakeDispatcher is an [im.Dispatcher] over servedCluster on endpoint 1,
// plus Groups' AddGroup, recording the timed flag of each write and invoke.
type fakeDispatcher struct {
	sizes map[uint32]int

	mu     sync.Mutex
	writes []bool
	timed  []bool
}

func (d *fakeDispatcher) Read(_ context.Context, p im.ConcreteAttributePath) []im.ReadResult {
	n, ok := d.sizes[p.Attribute]
	if !ok || p.Cluster != servedCluster {
		return []im.ReadResult{{Path: p, Status: im.StatusUnsupportedAttribute}}
	}
	return []im.ReadResult{{Path: p, Value: im.AttributeValue{Value: strings.Repeat("x", n)}, DataVersion: 7}}
}

func (d *fakeDispatcher) Write(ctx context.Context, p im.ConcreteAttributePath, _ im.AttributeValue) []im.WriteResult {
	d.mu.Lock()
	d.writes = append(d.writes, timedOf(ctx))
	d.mu.Unlock()
	return []im.WriteResult{{Path: p, Status: im.StatusSuccess}}
}

func (d *fakeDispatcher) Invoke(ctx context.Context, p im.ConcreteCommandPath, _ any) im.InvokeResult {
	d.mu.Lock()
	d.timed = append(d.timed, timedOf(ctx))
	d.mu.Unlock()
	if p.Cluster == groups.ClusterID {
		return im.InvokeResult{Path: p, Response: groups.AddGroupResponse{GroupId: 1}}
	}
	return im.InvokeResult{Path: p, Response: blob(d.sizes[p.Command])}
}

func timedOf(ctx context.Context) bool {
	timed, _ := im.TimedInteractionFromContext(ctx)
	return timed
}

// plainQualities marks no cluster Diagnostics and no attribute Fixed.
type plainQualities struct{}

func (plainQualities) DiagnosticsCluster(uint32) bool     { return false }
func (plainQualities) AttributeFixed(uint32, uint32) bool { return false }

// serverRig is handlerRig with a fake dispatcher served.
func serverRig(t *testing.T, maxPaths int) (*scripted, *interaction.Subscriptions, *fakeDispatcher) {
	t.Helper()
	s, reg, _, _ := handlerRig(t)
	d := &fakeDispatcher{sizes: map[uint32]int{0: 400, 1: 400, 2: 400, 3: 400, 9: oversized, 20: 10}}
	reg.Serve(interaction.ServerConfig{Dispatcher: d, MaxPathsPerInvoke: maxPaths, Qualities: plainQualities{}})
	return s, reg, d
}

func servedAttr(id uint32) im.ConcreteAttributePath {
	return im.ConcreteAttributePath{Endpoint: 1, Cluster: servedCluster, Attribute: id, HasEndpoint: true, HasCluster: true, HasAttribute: true}
}

func servedCmd(id uint32) im.ConcreteCommandPath {
	return im.ConcreteCommandPath{Endpoint: 1, Cluster: servedCluster, Command: id, HasEndpoint: true, HasCluster: true, HasCommand: true}
}

func readRequest(t *testing.T, attrs ...uint32) []byte {
	t.Helper()
	req := im.ReadRequest{}
	for _, a := range attrs {
		req.AttributeRequests = append(req.AttributeRequests, servedAttr(a))
	}
	return encode(t, func(e *tlv.Encoder) { req.MarshalTLV(e) })
}

func writeRequest(t *testing.T, timed, suppress bool) []byte {
	t.Helper()
	req := im.WriteRequest{TimedRequest: timed, SuppressResponse: suppress, Writes: []im.AttributeWrite{{Path: servedAttr(20), Value: im.AttributeValue{Value: uint8(4)}}}}
	return encode(t, func(e *tlv.Encoder) { req.MarshalTLV(e, wire.AttributeValueWriter) })
}

func invokeRequest(t *testing.T, timed, suppress bool, cmds ...uint32) []byte {
	t.Helper()
	req := im.InvokeRequest{TimedRequest: timed, SuppressResponse: suppress}
	for i, c := range cmds {
		req.Invokes = append(req.Invokes, im.CommandInvocation{Path: servedCmd(c), CommandRef: uint16(i), HasCommandRef: len(cmds) > 1})
	}
	return encode(t, func(e *tlv.Encoder) { im.MarshalInvokeRequestTLV(e, req, wire.CommandFieldsWriter) })
}

func timedRequest(t *testing.T, ms uint16) []byte {
	t.Helper()
	return encode(t, func(e *tlv.Encoder) { im.TimedRequest{TimeoutMs: ms}.MarshalTLV(e) })
}

// expectOpcode reads the next message and fails unless it is opcode.
func expectOpcode(t *testing.T, s *scripted, opcode uint8) msg {
	t.Helper()
	m := s.next(t)
	if m.proto.Opcode != opcode {
		t.Fatalf("answer opcode 0x%02X, want 0x%02X", m.proto.Opcode, opcode)
	}
	return m
}

func decodeReport(t *testing.T, b []byte) im.ReportData {
	t.Helper()
	rd, err := im.UnmarshalReportDataTLV(tlv.NewDecoder(b), wire.AttributeValueReader, nil)
	if err != nil {
		t.Fatal(err)
	}
	return rd
}

func decodeInvokeResponse(t *testing.T, b []byte) im.InvokeResponse {
	t.Helper()
	resp, err := im.UnmarshalInvokeResponseTLV(tlv.NewDecoder(b), wire.CommandFieldsReader)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// TestServeRead: a read that fits is one suppressed report; a read over
// the session's payload goes out in chunks, each answered by the peer's
// StatusResponse, with an attribute no message carries reported as
// ResourceExhausted.
func TestServeRead(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s, _, _ := serverRig(t, 0)

		s.push(t, 0x7201, im.OpcodeReadRequest, readRequest(t, 20))
		m := expectOpcode(t, s, im.OpcodeReportData)
		rd := decodeReport(t, m.payload)
		if !rd.SuppressResponse || rd.MoreChunkedMessages || len(rd.Reports) != 1 {
			t.Fatalf("single report %+v", rd)
		}
		s.ackPushed(t, m)

		s.push(t, 0x7202, im.OpcodeReadRequest, readRequest(t, 0, 1, 9, 2, 3))
		var reports []im.AttributeReport
		for chunk := 0; ; chunk++ {
			m := expectOpcode(t, s, im.OpcodeReportData)
			rd := decodeReport(t, m.payload)
			reports = append(reports, rd.Reports...)
			if !rd.MoreChunkedMessages {
				if !rd.SuppressResponse || chunk == 0 {
					t.Fatalf("last chunk %d: %+v", chunk, rd)
				}
				s.ackPushed(t, m)
				break
			}
			s.continueOn(t, m, im.OpcodeStatusResponse, statusResponse(t, im.StatusSuccess))
		}
		if len(reports) != 5 {
			t.Fatalf("%d reports over the chunks, want 5", len(reports))
		}
		if r := reports[2]; !r.IsStatus || r.Status.Status != im.StatusResourceExhausted || r.Path.Attribute != 9 {
			t.Fatalf("oversized attribute reported %+v, want ResourceExhausted", r)
		}
	})
}

// TestServeReadChunkRefused: a chunk the peer answers with a failure or
// with anything but a StatusResponse ends the report.
func TestServeReadChunkRefused(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s, _, _ := serverRig(t, 0)

		s.push(t, 0x7211, im.OpcodeReadRequest, readRequest(t, 0, 1, 2, 3))
		m := expectOpcode(t, s, im.OpcodeReportData)
		if !decodeReport(t, m.payload).MoreChunkedMessages {
			t.Fatal("the first chunk announces no more")
		}
		s.continueOn(t, m, im.OpcodeStatusResponse, statusResponse(t, im.StatusFailure))

		s.push(t, 0x7212, im.OpcodeReadRequest, readRequest(t, 0, 1, 2, 3))
		m = expectOpcode(t, s, im.OpcodeReportData)
		s.continueOn(t, m, im.OpcodeReadRequest, readRequest(t, 20))

		// The server goes on serving: the next exchange is answered.
		s.push(t, 0x7213, im.OpcodeReadRequest, readRequest(t, 20))
		if rd := decodeReport(t, expectOpcode(t, s, im.OpcodeReportData).payload); len(rd.Reports) != 1 || rd.MoreChunkedMessages {
			t.Fatalf("report after refused chunks %+v", rd)
		}
	})
}

// TestServeRefusals: an undecodable request and a read path with a
// wildcard cluster are InvalidAction; a SubscribeRequest stays with the
// report-only handler; a nil Dispatcher uninstalls the server.
func TestServeRefusals(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s, reg, _ := serverRig(t, 0)
		garbage := []byte{0x15, 0x24}
		for i, op := range []uint8{im.OpcodeReadRequest, im.OpcodeWriteRequest, im.OpcodeInvokeRequest, im.OpcodeTimedRequest} {
			s.push(t, 0x7220+uint16(i), op, garbage)
			s.ackPushed(t, expectStatus(t, s, im.StatusInvalidAction))
		}
		wild := im.ReadRequest{AttributeRequests: []im.ConcreteAttributePath{{Endpoint: 1, HasEndpoint: true, Attribute: 0, HasAttribute: true}}}
		s.push(t, 0x7228, im.OpcodeReadRequest, encode(t, func(e *tlv.Encoder) { wild.MarshalTLV(e) }))
		s.ackPushed(t, expectStatus(t, s, im.StatusInvalidAction))

		sub := im.SubscribeRequest{MaxIntervalCeiling: 10, AttributeRequests: []im.ConcreteAttributePath{servedAttr(20)}}
		s.push(t, 0x7229, im.OpcodeSubscribeRequest, encode(t, func(e *tlv.Encoder) { sub.MarshalTLV(e) }))
		s.ackPushed(t, expectStatus(t, s, im.StatusInvalidAction))

		reg.Serve(interaction.ServerConfig{})
		s.push(t, 0x722A, im.OpcodeReadRequest, readRequest(t, 20))
		s.ackPushed(t, expectStatus(t, s, im.StatusInvalidAction))
	})
}

// TestServeWrite: an untimed write is answered with a WriteResponse; a
// suppressed one with nothing; a write that claims a timed interaction
// the exchange did not open is TimedRequestMismatch.
func TestServeWrite(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s, _, d := serverRig(t, 0)

		s.push(t, 0x7231, im.OpcodeWriteRequest, writeRequest(t, false, false))
		m := expectOpcode(t, s, im.OpcodeWriteResponse)
		resp, err := im.UnmarshalWriteResponseTLV(tlv.NewDecoder(m.payload))
		if err != nil || len(resp.Responses) != 1 || resp.Responses[0].Status.Status != im.StatusSuccess {
			t.Fatalf("WriteResponse %+v, %v", resp, err)
		}
		s.ackPushed(t, m)

		s.push(t, 0x7232, im.OpcodeWriteRequest, writeRequest(t, false, true))
		s.push(t, 0x7233, im.OpcodeWriteRequest, writeRequest(t, true, false))
		s.ackPushed(t, expectStatus(t, s, im.StatusTimedRequestMismatch))
		synctest.Wait()

		d.mu.Lock()
		defer d.mu.Unlock()
		if len(d.writes) != 2 || d.writes[0] || d.writes[1] {
			t.Fatalf("writes reached the dispatcher as %v, want two untimed", d.writes)
		}
	})
}

// TestServeInvoke: the response names its own command; a batch above
// MaxPathsPerInvoke is InvalidAction; a suppressed invoke is answered with
// nothing; a timed flag without a timed interaction is
// TimedRequestMismatch.
func TestServeInvoke(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s, _, d := serverRig(t, 0)

		add := im.InvokeRequest{Invokes: []im.CommandInvocation{{
			Path:   im.ConcreteCommandPath{Endpoint: 1, Cluster: groups.ClusterID, Command: groups.CmdAddGroup, HasEndpoint: true, HasCluster: true, HasCommand: true},
			Fields: groups.AddGroupRequest{GroupId: 1, GroupName: "a"},
		}}}
		s.push(t, 0x7241, im.OpcodeInvokeRequest, encode(t, func(e *tlv.Encoder) { im.MarshalInvokeRequestTLV(e, add, wire.CommandFieldsWriter) }))
		m := expectOpcode(t, s, im.OpcodeInvokeResponse)
		resp := decodeInvokeResponse(t, m.payload)
		if len(resp.Responses) != 1 || resp.Responses[0].Path.Command != groups.CmdAddGroupResponse {
			t.Fatalf("AddGroup answered %+v, want AddGroupResponse", resp)
		}
		s.ackPushed(t, m)

		s.push(t, 0x7242, im.OpcodeInvokeRequest, invokeRequest(t, false, false, 0, 1))
		s.ackPushed(t, expectStatus(t, s, im.StatusInvalidAction))

		s.push(t, 0x7243, im.OpcodeInvokeRequest, invokeRequest(t, false, true, 20))
		s.push(t, 0x7244, im.OpcodeInvokeRequest, invokeRequest(t, true, false, 20))
		s.ackPushed(t, expectStatus(t, s, im.StatusTimedRequestMismatch))
		synctest.Wait()

		d.mu.Lock()
		defer d.mu.Unlock()
		if len(d.timed) != 2 {
			t.Fatalf("%d invokes reached the dispatcher, want 2", len(d.timed))
		}
	})
}

// blockingDispatcher is the fake dispatcher whose Invoke signals entered
// and returns once release is closed, whatever its context says.
type blockingDispatcher struct {
	*fakeDispatcher
	entered, release chan struct{}
}

func (d *blockingDispatcher) Invoke(ctx context.Context, p im.ConcreteCommandPath, f any) im.InvokeResult {
	close(d.entered)
	<-d.release
	return d.fakeDispatcher.Invoke(ctx, p, f)
}

// TestServeBlockedAndAwaited: once the registry blocks new activity a
// request for the served endpoints is not dispatched — matter.js's
// InteractionServer ignores anything newly incoming then — and
// BlockNewActivity waits for a served request already running, as it
// waits for a report (review II, R2-P1-net-04).
func TestServeBlockedAndAwaited(t *testing.T) {
	t.Parallel()
	t.Run("a request after BlockNewActivity is not dispatched", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			s, reg, d := serverRig(t, 0)
			if err := reg.BlockNewActivity(t.Context()); err != nil {
				t.Fatal(err)
			}
			s.push(t, 0x7251, im.OpcodeInvokeRequest, invokeRequest(t, false, false, 20))
			s.push(t, 0x7252, im.OpcodeReadRequest, readRequest(t, 20))
			synctest.Wait()
			d.mu.Lock()
			defer d.mu.Unlock()
			if len(d.timed) != 0 {
				t.Fatalf("%d invokes reached the dispatcher after BlockNewActivity, want 0", len(d.timed))
			}
		})
	})
	t.Run("BlockNewActivity waits for a served request in flight", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			s, reg, _, _ := handlerRig(t)
			d := &blockingDispatcher{
				fakeDispatcher: &fakeDispatcher{sizes: map[uint32]int{20: 10}},
				entered:        make(chan struct{}),
				release:        make(chan struct{}),
			}
			reg.Serve(interaction.ServerConfig{Dispatcher: d, MaxPathsPerInvoke: 1, Qualities: plainQualities{}})
			s.push(t, 0x7253, im.OpcodeInvokeRequest, invokeRequest(t, false, false, 20))
			<-d.entered
			blocked := make(chan error, 1)
			go func() { blocked <- reg.BlockNewActivity(t.Context()) }()
			synctest.Wait()
			select {
			case err := <-blocked:
				close(d.release)
				t.Fatalf("BlockNewActivity returned (%v) while a served invoke was running", err)
			default:
			}
			close(d.release)
			if err := <-blocked; err != nil {
				t.Fatal(err)
			}
		})
	})
}

// TestServeInvokeChunked: a batch whose responses exceed the session's
// payload goes out in chunks; a response no message carries becomes
// ResourceExhausted for its path; a refused chunk ends the response.
func TestServeInvokeChunked(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s, _, _ := serverRig(t, 8)

		s.push(t, 0x7251, im.OpcodeInvokeRequest, invokeRequest(t, false, false, 0, 1, 9, 2, 3))
		var entries []im.InvokeResponseEntry
		for chunk := 0; ; chunk++ {
			m := expectOpcode(t, s, im.OpcodeInvokeResponse)
			resp := decodeInvokeResponse(t, m.payload)
			entries = append(entries, resp.Responses...)
			if !resp.MoreChunkedMessages {
				if chunk == 0 {
					t.Fatal("one chunk")
				}
				s.ackPushed(t, m)
				break
			}
			s.continueOn(t, m, im.OpcodeStatusResponse, statusResponse(t, im.StatusSuccess))
		}
		if len(entries) != 5 {
			t.Fatalf("%d responses over the chunks, want 5", len(entries))
		}
		if e := entries[2]; !e.IsStatus || e.Status.Status != im.StatusResourceExhausted || e.Path.Command != 9 || e.CommandRef != 2 {
			t.Fatalf("oversized response %+v, want ResourceExhausted with its ref", e)
		}

		s.push(t, 0x7252, im.OpcodeInvokeRequest, invokeRequest(t, false, false, 0, 1, 2, 3))
		m := expectOpcode(t, s, im.OpcodeInvokeResponse)
		s.continueOn(t, m, im.OpcodeStatusResponse, statusResponse(t, im.StatusBusy))
		s.push(t, 0x7253, im.OpcodeInvokeRequest, invokeRequest(t, false, false, 20))
		if resp := decodeInvokeResponse(t, expectOpcode(t, s, im.OpcodeInvokeResponse).payload); len(resp.Responses) != 1 {
			t.Fatalf("response after a refused chunk %+v", resp)
		}
	})
}

// TestServeTimed: a TimedRequest is answered Success and the write or
// invoke it announces runs timed; anything else after it is
// InvalidAction; a window that closes without a request ends the
// exchange.
func TestServeTimed(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s, _, d := serverRig(t, 0)

		s.push(t, 0x7261, im.OpcodeTimedRequest, timedRequest(t, 500))
		st := expectStatus(t, s, im.StatusSuccess)
		s.continueOn(t, st, im.OpcodeWriteRequest, writeRequest(t, true, false))
		s.ackPushed(t, expectOpcode(t, s, im.OpcodeWriteResponse))

		s.push(t, 0x7262, im.OpcodeTimedRequest, timedRequest(t, 500))
		st = expectStatus(t, s, im.StatusSuccess)
		s.continueOn(t, st, im.OpcodeInvokeRequest, invokeRequest(t, true, false, 20))
		s.ackPushed(t, expectOpcode(t, s, im.OpcodeInvokeResponse))

		s.push(t, 0x7263, im.OpcodeTimedRequest, timedRequest(t, 500))
		st = expectStatus(t, s, im.StatusSuccess)
		s.continueOn(t, st, im.OpcodeWriteRequest, writeRequest(t, false, false))
		s.ackPushed(t, expectStatus(t, s, im.StatusTimedRequestMismatch))

		s.push(t, 0x7264, im.OpcodeTimedRequest, timedRequest(t, 500))
		st = expectStatus(t, s, im.StatusSuccess)
		s.continueOn(t, st, im.OpcodeReadRequest, readRequest(t, 20))
		s.ackPushed(t, expectStatus(t, s, im.StatusInvalidAction))

		s.push(t, 0x7265, im.OpcodeTimedRequest, timedRequest(t, 250))
		s.ackPushed(t, expectStatus(t, s, im.StatusSuccess))
		s.clock.waitTimer(t, 250*time.Millisecond)
		s.clock.Advance(250 * time.Millisecond)
		synctest.Wait()

		s.push(t, 0x7266, im.OpcodeReadRequest, readRequest(t, 20))
		s.ackPushed(t, expectOpcode(t, s, im.OpcodeReportData))

		d.mu.Lock()
		defer d.mu.Unlock()
		if len(d.writes) != 1 || !d.writes[0] || len(d.timed) != 1 || !d.timed[0] {
			t.Fatalf("writes %v, invokes %v; want one timed each", d.writes, d.timed)
		}
	})
}

// TestReportValues: a report's values of one cluster, by attribute id, a
// later value replacing an earlier one; a nil report has none.
func TestReportValues(t *testing.T) {
	t.Parallel()
	node := func(v uint64) spec.Node { return spec.Node{El: tlv.Element{Type: tlv.TypeUnsignedInt1, Uint: v}} }
	path := func(ep uint16, cl, attr uint32) im.ConcreteAttributePath {
		return im.ConcreteAttributePath{Endpoint: ep, Cluster: cl, Attribute: attr}
	}
	r := &interaction.Report{Attributes: []interaction.AttributeData{
		{Path: path(1, 6, 0), Value: node(1)},
		{Path: path(1, 8, 0), Value: node(2)},
		{Path: path(2, 6, 0), Value: node(3)},
		{Path: path(1, 6, 0), Value: node(4)},
		{Path: path(1, 6, 1), Value: node(5)},
	}}
	v := r.Values(1, 6)
	if len(v) != 2 || v[0].El.Uint != 4 || v[1].El.Uint != 5 {
		t.Fatalf("values %+v", v)
	}
	var none *interaction.Report
	if len(none.Values(1, 6)) != 0 || len(r.Values(3, 6)) != 0 {
		t.Fatal("values from no report")
	}
}
