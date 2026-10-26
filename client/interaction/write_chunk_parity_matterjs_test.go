// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// The write-chunking cases of matter.js's InteractionClientMessengerTest
// "writes" group (packages/protocol/test/interaction/InteractionClientMessengerTest.ts:593-801
// at 862ad75f) through Interaction.Write against the scripted peer:
// sendWriteCommand and #chunkWriteRequest (InteractionMessenger.ts:1269-1450).

// chunkedWrite is one WriteRequest message the peer received.
type chunkedWrite struct {
	size int
	req  im.WriteRequest
}

// respondChunks answers every WriteRequest chunk of a write with a
// WriteResponse — status decides each status, Success when nil — until
// the last; errorAt, when positive, answers that chunk (1-based) with a
// StatusResponse Failure instead and stops.
func respondChunks(t *testing.T, s *scripted, status func(im.AttributeWrite) im.StatusCode, errorAt int) []chunkedWrite {
	t.Helper()
	var received []chunkedWrite
	for {
		m := s.next(t)
		if m.proto.Opcode != im.OpcodeWriteRequest {
			t.Fatalf("opcode 0x%02X, want WriteRequest", m.proto.Opcode)
		}
		wr, err := im.UnmarshalWriteRequestTLV(tlv.NewDecoder(m.payload), nodeReader)
		if err != nil {
			t.Fatal(err)
		}
		received = append(received, chunkedWrite{len(m.payload), wr})
		if errorAt > 0 && len(received) == errorAt {
			s.reply(t, m, im.InteractionModelProtocolID, im.OpcodeStatusResponse, statusResponse(t, im.StatusFailure))
			return received
		}
		statuses := make([]im.AttributeStatus, 0, len(wr.Writes))
		for _, w := range wr.Writes {
			st := im.StatusSuccess
			if status != nil {
				st = status(w)
			}
			statuses = append(statuses, im.AttributeStatus{Path: w.Path, Status: im.StatusIB{Status: st}})
		}
		s.reply(t, m, im.InteractionModelProtocolID, im.OpcodeWriteResponse, writeResponse(t, statuses...))
		if !wr.MoreChunkedMessages {
			return received
		}
	}
}

func writesOf(received []chunkedWrite) []im.AttributeWrite {
	var out []im.AttributeWrite
	for _, r := range received {
		out = append(out, r.req.Writes...)
	}
	return out
}

func versioned(w interaction.WriteRequest, v uint32) interaction.WriteRequest {
	w.DataVersion = &v
	return w
}

func TestInteractionClientMessengerWriteChunking(t *testing.T) {
	t.Parallel()

	t.Run("chunks a write that does not fit one message and merges the statuses of all chunks", func(t *testing.T) {
		t.Parallel()
		s := newScripted(t)
		c := interaction.New(s.mgr, s.session, nil)
		writes := make([]interaction.WriteRequest, 0, 10)
		for i := range 10 {
			writes = append(writes, icmWrite(uint32(i), icmOctets(icmEntry)))
		}
		done := writeAsync(c, writes, interaction.WriteOptions{})
		received := respondChunks(t, s, nil, 0)
		res := await(t, done)
		if res.err != nil || len(received) < 2 {
			t.Fatalf("%d chunks, %v", len(received), res.err)
		}
		for i, r := range received {
			if r.size > exchange.MaxPayloadSize || r.req.MoreChunkedMessages != (i < len(received)-1) {
				t.Fatalf("chunk %d: %d bytes, more %v", i, r.size, r.req.MoreChunkedMessages)
			}
		}
		sent := writesOf(received)
		if len(sent) != 10 || len(res.res) != 10 {
			t.Fatalf("%d writes sent, %d statuses", len(sent), len(res.res))
		}
		for i := range 10 {
			if sent[i].Path.Attribute != uint32(i) || res.res[i].Path.Attribute != uint32(i) {
				t.Fatalf("order at %d: sent %d, status %d", i, sent[i].Path.Attribute, res.res[i].Path.Attribute)
			}
		}
	})

	t.Run("chunks a write whose statuses would not fit one WriteResponse although the request does", func(t *testing.T) {
		t.Parallel()
		s := newScripted(t)
		c := interaction.New(s.mgr, s.session, nil)
		boolean := spec.Node{El: tlv.Element{Type: tlv.TypeBoolTrue, Bool: true}}
		var writes []interaction.WriteRequest
		for i := 0; ; i++ {
			next := append(append([]interaction.WriteRequest(nil), writes...), icmWrite(uint32(i), boolean))
			if requestSize(t, next) > exchange.MaxPayloadSize {
				break
			}
			writes = next
		}
		if worstResponseSize(t, writes) <= exchange.MaxPayloadSize {
			t.Fatal("the statuses fit one WriteResponse")
		}
		done := writeAsync(c, writes, interaction.WriteOptions{})
		received := respondChunks(t, s, nil, 0)
		if res := await(t, done); res.err != nil || len(received) < 2 {
			t.Fatalf("%d chunks, %v", len(received), res.err)
		}
		for i, r := range received {
			if worstResponseOf(t, r.req.Writes) > exchange.MaxPayloadSize {
				t.Fatalf("chunk %d's statuses do not fit one WriteResponse", i)
			}
		}
		if len(writesOf(received)) != len(writes) {
			t.Fatal("writes lost")
		}
	})

	t.Run("sends a whole list that does not fit one message as an empty REPLACE followed by ADDs", func(t *testing.T) {
		t.Parallel()
		s := newScripted(t)
		c := interaction.New(s.mgr, s.session, nil)
		done := writeAsync(c, []interaction.WriteRequest{icmWrite(1, icmOctets(icmEntry)), icmWrite(2, icmOctetList(8))}, interaction.WriteOptions{DisableListChunking: true})
		received := respondChunks(t, s, nil, 0)
		if res := await(t, done); res.err != nil {
			t.Fatal(res.err)
		}
		first, replaced, adds := sentList(t, writesOf(received))
		if first.Path.Attribute != 1 || len(replaced) != 0 || len(adds) != 8 {
			t.Fatalf("first %d, replace %d entries, %d ADDs", first.Path.Attribute, len(replaced), len(adds))
		}
	})

	t.Run("sends a whole ACL that does not fit one message as a filled REPLACE followed by ADDs", func(t *testing.T) {
		t.Parallel()
		s := newScripted(t)
		c := interaction.New(s.mgr, s.session, nil)
		acl := interaction.WriteRequest{Path: attrPath(0, 0x001F, 0x0000), Value: interaction.Raw(icmOctetList(8))}
		done := writeAsync(c, []interaction.WriteRequest{icmWrite(1, icmOctets(icmEntry)), acl}, interaction.WriteOptions{})
		received := respondChunks(t, s, nil, 0)
		if res := await(t, done); res.err != nil {
			t.Fatal(res.err)
		}
		if len(received[0].req.Writes) != 1 {
			t.Fatalf("first chunk %d writes, want the small one alone", len(received[0].req.Writes))
		}
		_, replaced, adds := sentList(t, writesOf(received))
		if len(replaced) == 0 || len(replaced)+len(adds) != 8 {
			t.Fatalf("replace %d entries, %d ADDs", len(replaced), len(adds))
		}
	})

	t.Run("sends the ADDs of a versioned list write without DataVersion once its REPLACE succeeded", func(t *testing.T) {
		t.Parallel()
		s := newScripted(t)
		c := interaction.New(s.mgr, s.session, nil)
		done := writeAsync(c, []interaction.WriteRequest{icmWrite(2, icmOctets(icmEntry)), versioned(icmWrite(1, icmOctetList(2)), 7)}, interaction.WriteOptions{})
		received := respondChunks(t, s, nil, 0)
		if res := await(t, done); res.err != nil {
			t.Fatal(res.err)
		}
		if len(received) != 2 || len(received[0].req.Writes) != 2 || len(received[1].req.Writes) != 2 ||
			!received[0].req.MoreChunkedMessages || received[1].req.MoreChunkedMessages {
			t.Fatalf("chunks %+v", received)
		}
		if !received[0].req.Writes[1].HasDataVersion {
			t.Fatal("the REPLACE lost its DataVersion")
		}
		for _, a := range received[1].req.Writes {
			if !a.Path.ListAppend || a.HasDataVersion {
				t.Fatalf("ADD %+v", a)
			}
		}
	})

	t.Run("keeps the DataVersion on the ADDs of a list write whose REPLACE failed", func(t *testing.T) {
		t.Parallel()
		s := newScripted(t)
		c := interaction.New(s.mgr, s.session, nil)
		done := writeAsync(c, []interaction.WriteRequest{versioned(icmWrite(1, icmOctetList(2)), 7)}, interaction.WriteOptions{})
		received := respondChunks(t, s, func(w im.AttributeWrite) im.StatusCode {
			if !w.Path.HasListIndex {
				return im.StatusDataVersionMismatch
			}
			return im.StatusSuccess
		}, 0)
		await(t, done)
		if len(received) != 2 || len(received[0].req.Writes) != 1 || len(received[1].req.Writes) != 2 {
			t.Fatalf("chunks %+v", received)
		}
		for _, a := range received[1].req.Writes {
			if !a.HasDataVersion {
				t.Fatalf("ADD lost its DataVersion: %+v", a)
			}
		}
	})

	t.Run("keeps the DataVersion on the ADDs of a repeated list write whose second REPLACE failed", func(t *testing.T) {
		t.Parallel()
		s := newScripted(t)
		c := interaction.New(s.mgr, s.session, nil)
		replaces := 0
		done := writeAsync(c, []interaction.WriteRequest{
			versioned(icmWrite(1, icmOctetList(1)), 7), versioned(icmWrite(1, icmOctetList(1)), 7),
		}, interaction.WriteOptions{})
		received := respondChunks(t, s, func(w im.AttributeWrite) im.StatusCode {
			if !w.Path.HasListIndex {
				replaces++
				if replaces > 1 {
					return im.StatusDataVersionMismatch
				}
			}
			return im.StatusSuccess
		}, 0)
		await(t, done)
		if len(received) != 3 {
			t.Fatalf("%d chunks, want 3", len(received))
		}
		c1, c2, c3 := received[0].req.Writes, received[1].req.Writes, received[2].req.Writes
		if len(c1) != 1 || len(c2) != 2 || len(c3) != 1 || c2[0].HasDataVersion || !c2[1].HasDataVersion || !c3[0].HasDataVersion {
			t.Fatalf("chunks %+v / %+v / %+v", c1, c2, c3)
		}
	})

	t.Run("stops sending chunks when the peer answers a chunk with an error status", func(t *testing.T) {
		t.Parallel()
		s := newScripted(t)
		c := interaction.New(s.mgr, s.session, nil)
		writes := make([]interaction.WriteRequest, 0, 10)
		for i := range 10 {
			writes = append(writes, icmWrite(uint32(i), icmOctets(icmEntry)))
		}
		done := writeAsync(c, writes, interaction.WriteOptions{})
		received := respondChunks(t, s, nil, 2)
		var se *interaction.StatusError
		if res := await(t, done); !errors.As(res.err, &se) || len(received) != 2 {
			t.Fatalf("%d chunks, %v", len(received), res.err)
		}
	})
}

// sentList is sentList of InteractionClientMessengerTest.ts:675-684: the
// first write, the REPLACE's entries and the ADDs, each ADD of ENTRY on
// the REPLACE's path.
func sentList(t *testing.T, writes []im.AttributeWrite) (first im.AttributeWrite, replaced []spec.Node, adds []im.AttributeWrite) {
	t.Helper()
	if len(writes) < 2 {
		t.Fatalf("%d writes", len(writes))
	}
	first, replace, adds := writes[0], writes[1], writes[2:]
	if replace.Path.HasListIndex {
		t.Fatalf("REPLACE with a list index: %+v", replace.Path)
	}
	for _, a := range adds {
		n := a.Value.Value.(spec.Node)
		if !a.Path.ListAppend || !sameAttr(a.Path, replace.Path) || !bytes.Equal(n.El.Octets, icmEntry) {
			t.Fatalf("ADD %+v", a.Path)
		}
	}
	return first, replace.Value.Value.(spec.Node).Children, adds
}

func sameAttr(a, b im.ConcreteAttributePath) bool {
	return a.Endpoint == b.Endpoint && a.Cluster == b.Cluster && a.Attribute == b.Attribute
}

// requestSize is the size of the one WriteRequest of writes.
func requestSize(t *testing.T, writes []interaction.WriteRequest) int {
	t.Helper()
	req := im.WriteRequest{}
	for _, w := range writes {
		req.Writes = append(req.Writes, im.AttributeWrite{Path: w.Path, Value: im.AttributeValue{Value: w.Value}})
	}
	return len(encode(t, func(e *tlv.Encoder) {
		req.MarshalTLV(e, func(e *tlv.Encoder, tag tlv.Tag, v im.AttributeValue) { v.Value.(spec.Encodable).EncodeTLV(e, tag) })
	}))
}

// worstResponseSize and worstResponseOf are worstResponseSize of
// InteractionClientMessengerTest.ts:621-629: the WriteResponse with every
// status Failure with a cluster status.
func worstResponseSize(t *testing.T, writes []interaction.WriteRequest) int {
	t.Helper()
	paths := make([]im.AttributeWrite, 0, len(writes))
	for _, w := range writes {
		paths = append(paths, im.AttributeWrite{Path: w.Path})
	}
	return worstResponseOf(t, paths)
}

func worstResponseOf(t *testing.T, writes []im.AttributeWrite) int {
	t.Helper()
	resp := im.WriteResponse{}
	for _, w := range writes {
		resp.Responses = append(resp.Responses, im.AttributeStatus{Path: w.Path, Status: im.StatusIB{Status: im.StatusFailure, ClusterStatus: uint8(im.StatusFailure), HasClusterStatus: true}})
	}
	return len(encode(t, resp.MarshalTLV))
}
