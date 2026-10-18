// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction_test

// A port of matter.js packages/protocol/test/action/request/InvokeTest.ts at
// 862ad75f: the InvokeRequest a client formulates — paths, fields,
// commandRefs, suppress and timed flags, revision, processing time.
//
// matter.js tests the Invoke() request object; the Go controller
// formulates the request inside Interaction.Invoke, so each case reads the
// InvokeRequest off the wire of a scripted peer. The multi-command and
// suppressResponse cases use im.InvokeRequest with
// im.MarshalInvokeRequestTLV, the request encoder, because
// Interaction.Invoke sends one command without a suppress option.

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/identify"
	"github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

var invokeToggle = im.ConcreteCommandPath{Endpoint: 1, Cluster: onoff.ClusterID, Command: onoff.CmdToggle}

// invokeFieldsNode keeps a command's fields as a node tree.
func invokeFieldsNode(_ im.ConcreteCommandPath, dec *tlv.Decoder, el tlv.Element) (any, error) {
	return spec.ReadContainer(dec, el)
}

// invokeCaptured runs Interaction.Invoke against a scripted peer, answers a
// TimedRequest with Success and the invoke with a Success status, and
// returns the InvokeRequest as sent with its payload.
func invokeCaptured(t *testing.T, path im.ConcreteCommandPath, fields spec.Encodable, opts interaction.InvokeOptions) (parsed im.InvokeRequest, payload []byte) {
	t.Helper()
	s := newScripted(t)
	c := interaction.New(s.mgr, s.session, nil)
	done := invokeAsync(c, path, fields, opts)
	req := s.next(t)
	if req.proto.Opcode == im.OpcodeTimedRequest {
		s.reply(t, req, im.InteractionModelProtocolID, im.OpcodeStatusResponse, statusResponse(t, im.StatusSuccess))
		req = s.next(t)
	}
	if req.proto.Opcode != im.OpcodeInvokeRequest {
		t.Fatalf("opcode 0x%02X", req.proto.Opcode)
	}
	parsed, err := im.UnmarshalInvokeRequestTLV(tlv.NewDecoder(req.payload), invokeFieldsNode)
	if err != nil {
		t.Fatal(err)
	}
	s.reply(t, req, im.InteractionModelProtocolID, im.OpcodeInvokeResponse, invokeResponse(t, false,
		im.InvokeResponseEntry{Path: path, IsStatus: true, Status: im.StatusIB{Status: im.StatusSuccess}}))
	if r := await(t, done); r.err != nil {
		t.Fatal(r.err)
	}
	return parsed, req.payload
}

// encodedInvoke encodes req with the request encoder and decodes it again.
func encodedInvoke(t *testing.T, req im.InvokeRequest) im.InvokeRequest {
	t.Helper()
	b := encode(t, func(e *tlv.Encoder) {
		im.MarshalInvokeRequestTLV(e, req, func(e *tlv.Encoder, tag tlv.Tag, _ any) {
			e.StartStruct(tag)
			_ = e.EndContainer()
		})
	})
	out, err := im.UnmarshalInvokeRequestTLV(tlv.NewDecoder(b), nil)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// interactionModelRevision returns the request's InteractionModelRevision
// (context tag 0xFF of the message struct).
func interactionModelRevision(t *testing.T, payload []byte) (uint64, bool) {
	t.Helper()
	n, err := spec.ReadElement(tlv.NewDecoder(payload))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range n.Children {
		if c.El.Tag.Kind == tlv.TagKindContext && c.El.Tag.Number == 0xFF {
			return c.El.Uint, true
		}
	}
	return 0, false
}

func TestInvokeRequestBuildBasic(t *testing.T) {
	t.Parallel()
	t.Run("creates invoke request for single command", func(t *testing.T) {
		t.Parallel()
		req, _ := invokeCaptured(t, invokeToggle, nil, interaction.InvokeOptions{})
		if len(req.Invokes) != 1 {
			t.Fatalf("invokes %d, want 1", len(req.Invokes))
		}
		p := req.Invokes[0].Path
		if p.Endpoint != 1 || p.Cluster != onoff.ClusterID || p.Command != onoff.CmdToggle {
			t.Fatalf("path %+v", p) // InvokeTest.ts:27-30
		}
	})
	t.Run("encodes command arguments into command fields", func(t *testing.T) {
		t.Parallel()
		path := im.ConcreteCommandPath{Endpoint: 1, Cluster: identify.ClusterID, Command: identify.CmdIdentify}
		req10, wire10 := invokeCaptured(t, path, identify.IdentifyRequest{IdentifyTime: 10}, interaction.InvokeOptions{})
		_, wire20 := invokeCaptured(t, path, identify.IdentifyRequest{IdentifyTime: 20}, interaction.InvokeOptions{})
		if len(req10.Invokes) != 1 || req10.Invokes[0].Path.Cluster != identify.ClusterID {
			t.Fatalf("request %+v", req10)
		}
		fields, ok := req10.Invokes[0].Fields.(spec.Node)
		var got identify.IdentifyRequest
		if !ok || got.DecodeTLV(fields) != nil || got.IdentifyTime != 10 {
			t.Fatalf("fields %+v (%v)", req10.Invokes[0].Fields, got)
		}
		if bytes.Equal(wire10, wire20) {
			t.Fatal("identifyTime 10 and 20 encode the same fields") // InvokeTest.ts:54
		}
	})
	t.Run("accepts a bare command request without an options wrapper", func(t *testing.T) {
		t.Parallel()
		// The zero InvokeOptions is the call without options.
		if req, _ := invokeCaptured(t, invokeToggle, nil, interaction.InvokeOptions{}); len(req.Invokes) != 1 {
			t.Fatalf("invokes %d, want 1", len(req.Invokes))
		}
	})
}

func TestInvokeRequestBuildMultipleCommands(t *testing.T) {
	t.Parallel()
	t.Run("creates request for multiple commands with refs", func(t *testing.T) {
		t.Parallel()
		on := im.ConcreteCommandPath{Endpoint: 2, Cluster: onoff.ClusterID, Command: onoff.CmdOn, HasEndpoint: true, HasCluster: true, HasCommand: true}
		toggle := invokeToggle
		toggle.HasEndpoint, toggle.HasCluster, toggle.HasCommand = true, true, true
		req := encodedInvoke(t, im.InvokeRequest{Invokes: []im.CommandInvocation{
			{Path: toggle, CommandRef: 1, HasCommandRef: true},
			{Path: on, CommandRef: 2, HasCommandRef: true},
		}})
		if len(req.Invokes) != 2 || req.Invokes[0].CommandRef != 1 || req.Invokes[1].CommandRef != 2 ||
			!req.Invokes[0].HasCommandRef || !req.Invokes[1].HasCommandRef {
			t.Fatalf("request %+v", req) // InvokeTest.ts:89-91
		}
	})
}

func TestInvokeRequestBuildOptions(t *testing.T) {
	t.Parallel()
	single := []im.CommandInvocation{{Path: im.ConcreteCommandPath{Endpoint: 1, Cluster: onoff.ClusterID, Command: onoff.CmdToggle, HasEndpoint: true, HasCluster: true, HasCommand: true}}}
	t.Run("does not suppress response by default", func(t *testing.T) {
		t.Parallel()
		if req, _ := invokeCaptured(t, invokeToggle, nil, interaction.InvokeOptions{}); req.SuppressResponse {
			t.Fatal("suppressResponse set") // InvokeTest.ts:142
		}
		if encodedInvoke(t, im.InvokeRequest{Invokes: single}).SuppressResponse {
			t.Fatal("suppressResponse set by the encoder")
		}
	})
	t.Run("suppresses response when requested", func(t *testing.T) {
		t.Parallel()
		if !encodedInvoke(t, im.InvokeRequest{SuppressResponse: true, Invokes: single}).SuppressResponse {
			t.Fatal("suppressResponse lost") // InvokeTest.ts:146
		}
	})
	t.Run("marks request as timed when timeout provided", func(t *testing.T) {
		t.Parallel()
		if req, _ := invokeCaptured(t, invokeToggle, nil, interaction.InvokeOptions{TimedTimeout: time.Second}); !req.TimedRequest {
			t.Fatal("timedRequest not set") // InvokeTest.ts:150-152
		}
	})
	t.Run("marks request as timed when timed flag set", func(t *testing.T) {
		t.Parallel()
		if req, _ := invokeCaptured(t, invokeToggle, nil, interaction.InvokeOptions{Timed: true}); !req.TimedRequest {
			t.Fatal("timedRequest not set") // InvokeTest.ts:157
		}
	})
	t.Run("uses default interaction model revision", func(t *testing.T) {
		t.Parallel()
		_, wire := invokeCaptured(t, invokeToggle, nil, interaction.InvokeOptions{})
		if _, ok := interactionModelRevision(t, wire); !ok {
			t.Fatal("no InteractionModelRevision") // InvokeTest.ts:161
		}
	})
	t.Run("retains expected processing time", func(t *testing.T) {
		t.Parallel()
		// The processing time is not a field of the wire request; Go keeps
		// it as the wait for the response, the use matter.js retains it for.
		ept := 5 * time.Second // InvokeTest.ts:169
		s := newScripted(t)
		c := interaction.New(s.mgr, s.session, nil)
		want := c.MaximumPeerResponseTime(ept, false)
		done := invokeAsync(c, invokeToggle, nil, interaction.InvokeOptions{ExpectedProcessingTime: ept})
		s.ack(t, s.next(t))
		s.clock.waitTimer(t, want)
		s.clock.Advance(want)
		var missing *exchange.PeerMessageMissingError
		if r := await(t, done); !errors.As(r.err, &missing) || missing.Timeout != want {
			t.Fatalf("%v, want a PeerMessageMissingError after %v", r.err, want)
		}
	})
}
