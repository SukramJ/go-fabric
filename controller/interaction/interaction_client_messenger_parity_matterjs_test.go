// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction_test

// A port of matter.js packages/protocol/test/interaction/InteractionClientMessengerTest.ts
// at 862ad75f: the client messenger's read, subscribe and write
// exchanges, driven through Interaction against a scripted peer. The
// trimming of DataVersionFilters is im's TestMessengerParity_*; the
// abandonSubscription group and write chunking have no Go counterpart and
// stay in the register's missing list.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// icmTopTags returns the context tags of a message's top-level fields.
func icmTopTags(t *testing.T, payload []byte) []uint32 {
	t.Helper()
	dec := tlv.NewDecoder(payload)
	if _, err := dec.Next(); err != nil {
		t.Fatal(err)
	}
	var tags []uint32
	for {
		el, err := dec.Next()
		if err != nil {
			t.Fatal(err)
		}
		if el.IsEndContainer {
			return tags
		}
		tags = append(tags, el.Tag.Number)
		if el.IsContainer {
			if _, err := spec.ReadContainer(dec, el); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func icmReadAsync(c *interaction.Interaction, paths ...im.ConcreteAttributePath) <-chan error {
	ch := make(chan error, 1)
	go func() {
		_, err := c.Read(context.Background(), paths...)
		ch <- err
	}()
	return ch
}

// TestInteractionClientMessengerRead ports "reads attributes" and "throws
// a received error when the peer answers a read with a StatusResponse
// ${status}" (InteractionClientMessengerTest.ts:93-162). The 0x7777
// variant has no Go form: a status is a uint8 (im.StatusCode).
func TestInteractionClientMessengerRead(t *testing.T) {
	t.Parallel()

	t.Run("reads attributes", func(t *testing.T) {
		t.Parallel()
		s := newScripted(t)
		c := interaction.New(s.mgr, s.session, nil)
		done := icmReadAsync(c, im.ConcreteAttributePath{})
		req := s.next(t)
		if req.proto.Opcode != im.OpcodeReadRequest || len(req.payload) >= exchange.MaxPayloadSize {
			t.Fatalf("opcode 0x%02X, %d bytes", req.proto.Opcode, len(req.payload))
		}
		// {interactionModelRevision, isFabricFiltered: true, attributeRequests: [{}]}:
		// AttributeRequests (0), IsFabricFiltered (3), the revision (0xFF).
		if tags := icmTopTags(t, req.payload); !slices.Equal(tags, []uint32{0, 3, 0xFF}) {
			t.Fatalf("ReadRequest fields %v", tags)
		}
		rr, err := im.UnmarshalReadRequestTLV(tlv.NewDecoder(req.payload))
		if err != nil || !rr.FabricFiltered || len(rr.AttributeRequests) != 1 || rr.AttributeRequests[0] != (im.ConcreteAttributePath{}) {
			t.Fatalf("ReadRequest %+v (%v)", rr, err)
		}
		s.reply(t, req, im.InteractionModelProtocolID, im.OpcodeReportData, reportChunk(t, false, true))
		if err := await(t, done); err != nil {
			t.Fatal(err)
		}
	})

	for _, status := range []im.StatusCode{im.StatusUnsupportedAccess, im.StatusSuccess} {
		t.Run(fmt.Sprintf("throws a received error when the peer answers a read with a StatusResponse %d", status), func(t *testing.T) {
			t.Parallel()
			s := newScripted(t)
			c := interaction.New(s.mgr, s.session, nil)
			done := icmReadAsync(c, im.ConcreteAttributePath{})
			s.reply(t, s.next(t), im.InteractionModelProtocolID, im.OpcodeStatusResponse, statusResponse(t, status))
			var se *interaction.StatusError
			if err := await(t, done); !errors.As(err, &se) || se.Status != status {
				t.Fatalf("read answered with %v: %v, want a *StatusError with it", status, err)
			}
		})
	}
}

// TestInteractionClientMessengerSubscribe ports "subscribes attributes"
// (InteractionClientMessengerTest.ts:199-237): the request, the priming
// report answered with Success, the subscribe response finalising it.
func TestInteractionClientMessengerSubscribe(t *testing.T) {
	t.Parallel()
	t.Run("subscribes attributes", func(t *testing.T) {
		t.Parallel()
		s := newScripted(t)
		reg := interaction.NewSubscriptions(interaction.SubscriptionsConfig{Clock: s.clock})
		c := interaction.Bind(interaction.SessionProvider(s.mgr, s.session), interaction.Config{Subscriptions: reg})
		done := subscribeAsync(c, interaction.SubscribeRequest{
			Attributes:       []im.ConcreteAttributePath{{}},
			MinIntervalFloor: interaction.Interval(0), MaxIntervalCeiling: interaction.Interval(time.Second),
		})
		req := s.next(t)
		if req.proto.Opcode != im.OpcodeSubscribeRequest || len(req.payload) >= exchange.MaxPayloadSize {
			t.Fatalf("opcode 0x%02X, %d bytes", req.proto.Opcode, len(req.payload))
		}
		// KeepSubscriptions (0), the floor (1), the ceiling (2),
		// AttributeRequests (3), IsFabricFiltered (7), the revision.
		if tags := icmTopTags(t, req.payload); !slices.Equal(tags, []uint32{0, 1, 2, 3, 7, 0xFF}) {
			t.Fatalf("SubscribeRequest fields %v", tags)
		}
		sr, err := im.UnmarshalSubscribeRequestTLV(tlv.NewDecoder(req.payload))
		if err != nil || !sr.KeepSubscriptions || sr.MinIntervalFloor != 0 || sr.MaxIntervalCeiling != 1 || !sr.FabricFiltered ||
			len(sr.AttributeRequests) != 1 || sr.AttributeRequests[0] != (im.ConcreteAttributePath{}) {
			t.Fatalf("SubscribeRequest %+v (%v)", sr, err)
		}
		priming := encode(t, func(e *tlv.Encoder) {
			im.ReportData{HasSubscription: true, SubscriptionID: 1}.MarshalTLV(e, func(*tlv.Encoder, tlv.Tag, im.AttributeValue) {})
		})
		s.reply(t, req, im.InteractionModelProtocolID, im.OpcodeReportData, priming)
		status := s.next(t)
		if status.proto.Opcode != im.OpcodeStatusResponse || decodeStatus(t, status.payload) != im.StatusSuccess {
			t.Fatalf("priming report answered with opcode 0x%02X", status.proto.Opcode)
		}
		s.reply(t, status, im.InteractionModelProtocolID, im.OpcodeSubscribeResponse, subscribeResponse(t, 1, 10))
		res := await(t, done)
		if res.err != nil {
			t.Fatal(res.err)
		}
		if res.sub.ID() != 1 || res.sub.MaxInterval() != 10*time.Second {
			t.Fatalf("subscription %d, %v", res.sub.ID(), res.sub.MaxInterval())
		}
		res.sub.Close()
	})
}

// icmEntry is ENTRY of InteractionClientMessengerTest.ts:533, 300 bytes.
var icmEntry = bytes.Repeat([]byte{0xab}, 300)

func icmOctets(b []byte) spec.Node {
	return spec.Node{El: tlv.Element{Type: tlv.TypeOctetStr2, Octets: b}}
}

func icmOctetList(n int) spec.Node {
	l := spec.Node{El: tlv.Element{Type: tlv.TypeArray, IsContainer: true}}
	for range n {
		l.Children = append(l.Children, icmOctets(icmEntry))
	}
	return l
}

// icmWrite is attributeData of InteractionClientMessengerTest.ts:535-544:
// endpoint 1, cluster 0x1234.
func icmWrite(attribute uint32, value spec.Node) interaction.WriteRequest {
	return interaction.WriteRequest{Path: attrPath(1, 0x1234, attribute), Value: interaction.Raw(value)}
}

// icmRefused checks that a write is refused with nothing sent.
func icmRefused(t *testing.T, s *scripted, done <-chan writeResult) {
	t.Helper()
	if res := await(t, done); !errors.Is(res.err, interaction.ErrRequestTooLarge) {
		t.Fatalf("write: %v, want ErrRequestTooLarge", res.err)
	}
	if n := len(s.conn.ch); n != 0 {
		t.Fatalf("%d messages sent, want none", n)
	}
}

// TestInteractionClientMessengerWrites ports the "writes" group
// (InteractionClientMessengerTest.ts:532-802) as far as a write that fits
// one message goes; Go refuses every write that does not (ErrRequestTooLarge)
// where matter.js chunks it, so the chunking cases stay missing.
func TestInteractionClientMessengerWrites(t *testing.T) {
	t.Parallel()

	t.Run("sends a write that fits one message unchanged, with MoreChunkedMessages encoded as false", func(t *testing.T) {
		t.Parallel()
		s := newScripted(t)
		c := interaction.New(s.mgr, s.session, nil)
		done := writeAsync(c, []interaction.WriteRequest{icmWrite(1, icmOctets(icmEntry)), icmWrite(2, icmOctets(icmEntry))}, interaction.WriteOptions{})
		req := s.next(t)
		// TimedRequest false (1), WriteRequests (2), MoreChunkedMessages
		// false (3), the revision; no SuppressResponse.
		if tags := icmTopTags(t, req.payload); !slices.Equal(tags, []uint32{1, 2, 3, 0xFF}) {
			t.Fatalf("WriteRequest fields %v", tags)
		}
		wr, err := im.UnmarshalWriteRequestTLV(tlv.NewDecoder(req.payload), nodeReader)
		if err != nil || wr.TimedRequest || wr.SuppressResponse || wr.MoreChunkedMessages || len(wr.Writes) != 2 {
			t.Fatalf("WriteRequest %+v (%v)", wr, err)
		}
		for i, w := range wr.Writes {
			if w.Path != attrPath(1, 0x1234, uint32(i+1)) || w.HasDataVersion || !bytes.Equal(w.Value.Value.(spec.Node).El.Octets, icmEntry) {
				t.Fatalf("write %d: %+v", i, w.Path)
			}
		}
		s.reply(t, req, im.InteractionModelProtocolID, im.OpcodeWriteResponse, writeResponse(t,
			im.AttributeStatus{Path: attrPath(1, 0x1234, 1)}, im.AttributeStatus{Path: attrPath(1, 0x1234, 2)}))
		if res := await(t, done); res.err != nil || len(res.res) != 2 {
			t.Fatalf("statuses %+v (%v)", res.res, res.err)
		}
	})

	t.Run("refuses a timed write whose statuses would not fit one WriteResponse", func(t *testing.T) {
		t.Parallel()
		// smallValuesFitting (:613-622): as many boolean writes as fit one
		// request; their worst statuses do not fit one WriteResponse.
		small := func(n int) []im.AttributeWrite {
			out := make([]im.AttributeWrite, 0, n)
			for i := range n {
				out = append(out, im.AttributeWrite{Path: attrPath(1, 0x1234, uint32(i)), Value: im.AttributeValue{Value: true}})
			}
			return out
		}
		size := func(f func(*tlv.Encoder)) int { return len(encode(t, f)) }
		requestSize := func(n int) int {
			return size(func(e *tlv.Encoder) {
				im.WriteRequest{Writes: small(n)}.MarshalTLV(e, func(e *tlv.Encoder, tag tlv.Tag, v im.AttributeValue) { e.PutBool(tag, true) })
			})
		}
		count := 1
		for requestSize(count+1) <= exchange.MaxPayloadSize {
			count++
		}
		worst := im.WriteResponse{}
		for _, w := range small(count) {
			worst.Responses = append(worst.Responses, im.AttributeStatus{Path: w.Path, Status: im.StatusIB{Status: im.StatusFailure, ClusterStatus: 1, HasClusterStatus: true}})
		}
		if size(worst.MarshalTLV) <= exchange.MaxPayloadSize {
			t.Fatalf("the worst WriteResponse of %d writes fits one message", count)
		}
		writes := make([]interaction.WriteRequest, 0, count)
		for i := range count {
			writes = append(writes, interaction.WriteRequest{Path: attrPath(1, 0x1234, uint32(i)), Value: interaction.Raw(spec.Node{El: tlv.Element{Type: tlv.TypeBoolTrue, Bool: true}})})
		}
		s := newScripted(t)
		c := interaction.New(s.mgr, s.session, nil)
		icmRefused(t, s, writeAsync(c, writes, interaction.WriteOptions{Timed: true}))
	})

	for _, flag := range []string{"timedRequest", "suppressResponse"} {
		t.Run("refuses to chunk a write with "+flag, func(t *testing.T) {
			t.Parallel()
			s := newScripted(t)
			c := interaction.New(s.mgr, s.session, nil)
			writes := make([]interaction.WriteRequest, 0, 10)
			for i := range 10 {
				writes = append(writes, icmWrite(uint32(i), icmOctets(icmEntry)))
			}
			opts := interaction.WriteOptions{Timed: flag == "timedRequest", SuppressResponse: flag == "suppressResponse"}
			icmRefused(t, s, writeAsync(c, writes, opts))
		})
	}

	t.Run("refuses a whole list at a wildcard path that does not fit one message", func(t *testing.T) {
		t.Parallel()
		s := newScripted(t)
		c := interaction.New(s.mgr, s.session, nil)
		w := icmWrite(1, icmOctetList(8))
		w.Path.HasEndpoint = false
		icmRefused(t, s, writeAsync(c, []interaction.WriteRequest{w}, interaction.WriteOptions{}))
	})

	t.Run("refuses a single value that does not fit one message", func(t *testing.T) {
		t.Parallel()
		s := newScripted(t)
		c := interaction.New(s.mgr, s.session, nil)
		large := icmWrite(1, icmOctets(bytes.Repeat([]byte{0xab}, 1300)))
		icmRefused(t, s, writeAsync(c, []interaction.WriteRequest{large}, interaction.WriteOptions{}))
	})

	t.Run("sends a timed versioned list write that fits one message unchanged", func(t *testing.T) {
		t.Parallel()
		// versionedList(2) (:733-741): a REPLACE with the empty list and two
		// ADDs, every IB at DataVersion 7; Go builds it from the list value.
		s := newScripted(t)
		c := interaction.New(s.mgr, s.session, nil)
		w := icmWrite(1, icmOctetList(2))
		version := uint32(7)
		w.DataVersion = &version
		done := writeAsync(c, []interaction.WriteRequest{w}, interaction.WriteOptions{Timed: true})
		timed := s.next(t)
		if timed.proto.Opcode != im.OpcodeTimedRequest {
			t.Fatalf("opcode 0x%02X, want TimedRequest", timed.proto.Opcode)
		}
		s.reply(t, timed, im.InteractionModelProtocolID, im.OpcodeStatusResponse, statusResponse(t, im.StatusSuccess))
		req := s.next(t)
		wr, err := im.UnmarshalWriteRequestTLV(tlv.NewDecoder(req.payload), nodeReader)
		if err != nil || !wr.TimedRequest || wr.MoreChunkedMessages || len(wr.Writes) != 3 {
			t.Fatalf("WriteRequest %+v (%v)", wr, err)
		}
		for i, a := range wr.Writes {
			n := a.Value.Value.(spec.Node)
			if !a.HasDataVersion || a.DataVersion != 7 || a.Path.Endpoint != 1 || a.Path.Cluster != 0x1234 || a.Path.Attribute != 1 {
				t.Fatalf("IB %d: %+v", i, a)
			}
			switch {
			case i == 0 && (a.Path.HasListIndex || n.El.Type != tlv.TypeArray || len(n.Children) != 0):
				t.Fatalf("REPLACE %+v", a.Path)
			case i > 0 && (!a.Path.ListAppend || !bytes.Equal(n.El.Octets, icmEntry)):
				t.Fatalf("ADD %d %+v", i, a.Path)
			}
		}
		s.reply(t, req, im.InteractionModelProtocolID, im.OpcodeWriteResponse, writeResponse(t))
		if res := await(t, done); res.err != nil {
			t.Fatal(res.err)
		}
	})
}
