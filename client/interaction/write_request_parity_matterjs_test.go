// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction_test

// A port of matter.js packages/protocol/test/action/request/WriteTest.ts at
// 862ad75f: the WriteRequest a client formulates — paths, encoded values,
// timed flag and timeout, data versions and their concrete-endpoint rule,
// chunking defaults, the empty write, the revision.
//
// matter.js tests the Write() request object; the Go controller
// formulates the request inside Interaction.Write, so each case reads the
// WriteRequest off the wire of a scripted peer, or takes the
// ErrMalformedRequest Write returns before sending. Go has one numeric
// WriteRequest surface: matter.js's name-based Write.Attribute and its raw
// writes both map to it.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// writeCaptured runs Interaction.Write against a scripted peer, answers a
// TimedRequest with Success and the write with an empty WriteResponse,
// and returns the WriteRequest as sent, its payload and the TimedRequest's
// timeout in milliseconds (0 without one).
func writeCaptured(t *testing.T, writes []interaction.WriteRequest, opts interaction.WriteOptions) (parsed im.WriteRequest, payload []byte, timedMs uint32) {
	t.Helper()
	s := newScripted(t)
	c := interaction.New(s.mgr, s.session, nil)
	done := writeAsync(c, writes, opts)
	req := s.next(t)
	if req.proto.Opcode == im.OpcodeTimedRequest {
		tr, err := im.UnmarshalTimedRequestTLV(tlv.NewDecoder(req.payload))
		if err != nil {
			t.Fatal(err)
		}
		timedMs = uint32(tr.TimeoutMs)
		s.reply(t, req, im.InteractionModelProtocolID, im.OpcodeStatusResponse, statusResponse(t, im.StatusSuccess))
		req = s.next(t)
	}
	if req.proto.Opcode != im.OpcodeWriteRequest {
		t.Fatalf("opcode 0x%02X", req.proto.Opcode)
	}
	parsed, err := im.UnmarshalWriteRequestTLV(tlv.NewDecoder(req.payload), nodeReader)
	if err != nil {
		t.Fatal(err)
	}
	s.reply(t, req, im.InteractionModelProtocolID, im.OpcodeWriteResponse, writeResponse(t))
	if r := await(t, done); r.err != nil {
		t.Fatal(r.err)
	}
	return parsed, req.payload, timedMs
}

func writeUint8(v uint64) spec.Encodable {
	return interaction.Raw(spec.Node{El: tlv.Element{Type: tlv.TypeUnsignedInt1, Uint: v}})
}

func writeString(v string) spec.Encodable {
	return interaction.Raw(spec.Node{El: tlv.Element{Type: tlv.TypeUTF8Str1, String: v}})
}

var writeOnOff = []interaction.WriteRequest{{Path: attrPath(1, 6, 0), Value: writeUint8(1)}}

func TestWriteRequestBuildBasic(t *testing.T) {
	t.Parallel()
	t.Run("creates write request with numeric IDs", func(t *testing.T) {
		t.Parallel()
		req, _, _ := writeCaptured(t, writeOnOff, interaction.WriteOptions{})
		if len(req.Writes) != 1 {
			t.Fatalf("writes %d, want 1", len(req.Writes))
		}
		if p := req.Writes[0].Path; p.Endpoint != 1 || p.Cluster != 6 || p.Attribute != 0 {
			t.Fatalf("path %+v", p) // WriteTest.ts:29-32
		}
	})
	t.Run("creates write request for multiple attributes", func(t *testing.T) {
		t.Parallel()
		req, _, _ := writeCaptured(t, []interaction.WriteRequest{
			{Path: attrPath(1, 6, 0), Value: writeUint8(1)},
			{Path: attrPath(1, 0x28, 5), Value: writeString("test")},
		}, interaction.WriteOptions{})
		if len(req.Writes) != 2 || req.Writes[0].Path.Attribute != 0 || req.Writes[1].Path.Attribute != 5 {
			t.Fatalf("writes %+v", req.Writes) // WriteTest.ts:57-59
		}
	})
	t.Run("encodes data with correct schema", func(t *testing.T) {
		t.Parallel()
		req, _, _ := writeCaptured(t, []interaction.WriteRequest{{Path: attrPath(0, 0x28, 3), Value: writeString("TestProduct")}}, interaction.WriteOptions{})
		if len(req.Writes) != 1 {
			t.Fatalf("writes %d, want 1", len(req.Writes))
		}
		n, ok := req.Writes[0].Value.Value.(spec.Node)
		if s, err := spec.DecodeString(n); !ok || err != nil || s != "TestProduct" {
			t.Fatalf("value %+v (%v)", req.Writes[0].Value, err) // WriteTest.ts:77-79
		}
	})
}

// TestWriteRequestTimedFromModel (W4-ctlim-02): a write of an attribute
// with the timed access quality is timed without the option — matter.js
// Write.ts:188. No attribute of the snapshot at the pin carries it, so the
// test names one; sequential, because it swaps the package's lookup.
func TestWriteRequestTimedFromModel(t *testing.T) {
	restore := interaction.SetTimedWrite(writeOnOff[0].Path.Cluster, writeOnOff[0].Path.Attribute)
	defer restore()
	req, _, timedMs := writeCaptured(t, writeOnOff, interaction.WriteOptions{})
	if !req.TimedRequest || timedMs != uint32(interaction.DefaultTimedRequestTimeout/time.Millisecond) {
		t.Fatalf("timedRequest %v, TimedRequest timeout %d ms", req.TimedRequest, timedMs)
	}
}

func TestWriteRequestBuildTimed(t *testing.T) {
	t.Parallel()
	t.Run("creates timed write request", func(t *testing.T) {
		t.Parallel()
		if req, _, _ := writeCaptured(t, writeOnOff, interaction.WriteOptions{Timed: true}); !req.TimedRequest {
			t.Fatal("timedRequest not set") // WriteTest.ts:99
		}
	})
	t.Run("creates timed write with timeout", func(t *testing.T) {
		t.Parallel()
		req, _, timedMs := writeCaptured(t, writeOnOff, interaction.WriteOptions{TimedTimeout: 5 * time.Second})
		if !req.TimedRequest || timedMs != 5000 {
			t.Fatalf("timedRequest %v, TimedRequest timeout %d ms, want 5000", req.TimedRequest, timedMs) // WriteTest.ts:117-118
		}
	})
	t.Run("enables timedRequest when timeout is set", func(t *testing.T) {
		t.Parallel()
		if req, _, _ := writeCaptured(t, writeOnOff, interaction.WriteOptions{TimedTimeout: 10 * time.Second}); !req.TimedRequest {
			t.Fatal("timedRequest not set") // WriteTest.ts:136
		}
	})
	t.Run("defaults timedRequest to false when not specified", func(t *testing.T) {
		t.Parallel()
		req, _, timedMs := writeCaptured(t, writeOnOff, interaction.WriteOptions{})
		if req.TimedRequest || timedMs != 0 {
			t.Fatal("timed by default") // WriteTest.ts:153
		}
	})
}

func TestWriteRequestBuildDataVersions(t *testing.T) {
	t.Parallel()
	version := func(v uint32) *uint32 { return &v }
	t.Run("includes data version when provided", func(t *testing.T) {
		t.Parallel()
		req, _, _ := writeCaptured(t, []interaction.WriteRequest{{Path: attrPath(1, 6, 0), Value: writeUint8(1), DataVersion: version(12345)}}, interaction.WriteOptions{})
		if len(req.Writes) != 1 || !req.Writes[0].HasDataVersion || req.Writes[0].DataVersion != 12345 {
			t.Fatalf("writes %+v", req.Writes) // WriteTest.ts:173
		}
	})
	t.Run("handles writes with different data versions", func(t *testing.T) {
		t.Parallel()
		req, _, _ := writeCaptured(t, []interaction.WriteRequest{
			{Path: attrPath(1, 6, 0), Value: writeUint8(1), DataVersion: version(100)},
			{Path: attrPath(1, 8, 0), Value: writeUint8(2), DataVersion: version(200)},
		}, interaction.WriteOptions{})
		if len(req.Writes) != 2 || req.Writes[0].DataVersion != 100 || req.Writes[1].DataVersion != 200 {
			t.Fatalf("writes %+v", req.Writes) // WriteTest.ts:200-201
		}
	})
	t.Run("keeps data version on a concrete reified write path", func(t *testing.T) {
		t.Parallel()
		// Write.Attribute({endpoint 1, OnOff, "onOff", true, version 7}).
		on := interaction.Raw(spec.Node{El: tlv.Element{Type: tlv.TypeBoolTrue, Bool: true}})
		req, _, _ := writeCaptured(t, []interaction.WriteRequest{{Path: attrPath(1, 6, 0), Value: on, DataVersion: version(7)}}, interaction.WriteOptions{})
		if len(req.Writes) != 1 || req.Writes[0].Path.Endpoint != 1 || req.Writes[0].DataVersion != 7 {
			t.Fatalf("writes %+v", req.Writes) // WriteTest.ts:215-216
		}
	})
	wildcard := im.ConcreteAttributePath{Cluster: 6, Attribute: 0, HasCluster: true, HasAttribute: true}
	for _, name := range []string{
		"rejects a data version on a wildcard-endpoint write path (§8.9.2.8.1)",
		"rejects a data version on a wildcard-endpoint raw write request (§8.9.2.8.1)",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := newScripted(t)
			c := interaction.New(s.mgr, s.session, nil)
			_, err := c.Write(context.Background(), []interaction.WriteRequest{{Path: wildcard, Value: writeUint8(1), DataVersion: version(7)}}, interaction.WriteOptions{})
			if !errors.Is(err, interaction.ErrMalformedRequest) || !strings.Contains(err.Error(), "must target a concrete endpoint") {
				t.Fatalf("%v, want a malformed request", err) // WriteTest.ts:219-230
			}
		})
	}
}

func TestWriteRequestBuildChunked(t *testing.T) {
	t.Parallel()
	t.Run("initializes moreChunkedMessages to false", func(t *testing.T) {
		t.Parallel()
		if req, _, _ := writeCaptured(t, writeOnOff, interaction.WriteOptions{}); req.MoreChunkedMessages {
			t.Fatal("moreChunkedMessages set") // WriteTest.ts:255
		}
	})
	t.Run("supports chunk lists option", func(t *testing.T) {
		t.Parallel()
		// chunkLists true is the default (DisableListChunking false); a
		// value that is no list stays one write.
		if req, _, _ := writeCaptured(t, writeOnOff, interaction.WriteOptions{DisableListChunking: false}); len(req.Writes) != 1 {
			t.Fatalf("writes %d, want 1", len(req.Writes)) // WriteTest.ts:273
		}
	})
}

func TestWriteRequestBuildValidation(t *testing.T) {
	t.Parallel()
	t.Run("throws when no writes provided", func(t *testing.T) {
		t.Parallel()
		s := newScripted(t)
		c := interaction.New(s.mgr, s.session, nil)
		_, err := c.Write(context.Background(), []interaction.WriteRequest{}, interaction.WriteOptions{})
		if !errors.Is(err, interaction.ErrMalformedRequest) || !strings.Contains(err.Error(), "contains no attributes") {
			t.Fatalf("%v, want a malformed request", err) // WriteTest.ts:279
		}
	})
}

func TestWriteRequestBuildInteractionModelRevision(t *testing.T) {
	t.Parallel()
	t.Run("uses default interaction model revision", func(t *testing.T) {
		t.Parallel()
		_, wire, _ := writeCaptured(t, writeOnOff, interaction.WriteOptions{})
		if !readFieldPresent(t, wire, 0xFF) {
			t.Fatal("no InteractionModelRevision") // WriteTest.ts:306
		}
	})
}
