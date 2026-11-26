// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction_test

import (
	"context"
	"testing"
	"testing/synctest"

	"thingfabric.dev/go-fabric/client/interaction"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/tlv"
)

// openStructValue is a value whose encoder leaves its structure open.
type openStructValue struct{}

func (openStructValue) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) { enc.StartStruct(tag) }

// failingValueDispatcher is the fake dispatcher with attribute 30 and
// command 30 of the served cluster answered by a value whose encoder
// fails.
type failingValueDispatcher struct{ *fakeDispatcher }

func (d failingValueDispatcher) Read(ctx context.Context, p im.ConcreteAttributePath) []im.ReadResult {
	if p.Cluster == servedCluster && p.Attribute == 30 {
		return []im.ReadResult{{Path: p, Value: im.AttributeValue{Value: openStructValue{}}, DataVersion: 7}}
	}
	return d.fakeDispatcher.Read(ctx, p)
}

func (d failingValueDispatcher) Invoke(ctx context.Context, p im.ConcreteCommandPath, f any) im.InvokeResult {
	if p.Cluster == servedCluster && p.Command == 30 {
		resp := p
		resp.Command = 0x80
		return im.InvokeResult{Path: resp, Response: openStructValue{}}
	}
	return d.fakeDispatcher.Invoke(ctx, p, f)
}

// TestServeValueThatCannotBeEncoded (#625): the client stack's served
// read answers an attribute whose encoder fails with FAILURE for its path
// and delivers the other; a batch invoke answers the command whose
// response fails with a CommandStatusIB FAILURE on the request's path
// with its CommandRef and delivers the other's response. Red before
// #625: the read's chunker failed and the read was answered with a
// status alone; the invoke's encode failed and nothing was sent.
func TestServeValueThatCannotBeEncoded(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s, reg, _, _ := handlerRig(t)
		d := failingValueDispatcher{&fakeDispatcher{sizes: map[uint32]int{20: 10}}}
		reg.Serve(interaction.ServerConfig{Dispatcher: d, MaxPathsPerInvoke: 2, Qualities: plainQualities{}})

		s.push(t, 0x7301, im.OpcodeReadRequest, readRequest(t, 30, 20))
		m := expectOpcode(t, s, im.OpcodeReportData)
		rd := decodeReport(t, m.payload)
		if len(rd.Reports) != 2 {
			t.Fatalf("%d reports, want 2: %+v", len(rd.Reports), rd.Reports)
		}
		if r := rd.Reports[0]; !r.IsStatus || r.Status.Status != im.StatusFailure || r.Path.Attribute != 30 {
			t.Fatalf("failing attribute reported %+v, want FAILURE for attribute 30", r)
		}
		if r := rd.Reports[1]; r.IsStatus || r.Path.Attribute != 20 {
			t.Fatalf("good attribute reported %+v, want its data", r)
		}
		s.ackPushed(t, m)

		s.push(t, 0x7302, im.OpcodeInvokeRequest, invokeRequest(t, false, false, 30, 20))
		m = expectOpcode(t, s, im.OpcodeInvokeResponse)
		resp := decodeInvokeResponse(t, m.payload)
		if len(resp.Responses) != 2 {
			t.Fatalf("%d responses, want 2: %+v", len(resp.Responses), resp.Responses)
		}
		bad, good := resp.Responses[0], resp.Responses[1]
		if !bad.IsStatus || bad.Status.Status != im.StatusFailure || bad.Path.Command != 30 || !bad.HasCommandRef || bad.CommandRef != 0 {
			t.Fatalf("failing response answered %+v, want FAILURE on command 30 with CommandRef 0", bad)
		}
		if good.IsStatus || !good.HasResponse || good.CommandRef != 1 {
			t.Fatalf("good response answered %+v, want its data with CommandRef 1", good)
		}
		s.ackPushed(t, m)
	})
}
