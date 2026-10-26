// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction_test

// A port of matter.js packages/protocol/test/action/client/ClientInteractionInvokeTest.ts at
// 862ad75f: the controller's single-command invoke on the wire (no
// commandRef, abort before and during the exchange, suppressResponse, no
// DelayReportData, a response without InteractionModelRevision) and the
// origin of its error statuses.
//
// The Go Invoke sends one command on its own exchange and never batches;
// its context stands for both the request's and the session's abort
// signal. A case that needs a multi-command invoke, batching or
// ClientInteraction.close stays missing.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// The OnOff commands of ClientInteractionInvokeTest.ts:46-48.
const (
	ciOnOffCluster = 0x6
	ciOn           = 1
	ciOff          = 0
)

func ciPath(ep uint16, cmd uint32) im.ConcreteCommandPath {
	return im.ConcreteCommandPath{Endpoint: ep, Cluster: ciOnOffCluster, Command: cmd, HasEndpoint: true, HasCluster: true, HasCommand: true}
}

func ciInvoke(ctx context.Context, c *interaction.Interaction, path im.ConcreteCommandPath, opts interaction.InvokeOptions) <-chan invokeResult {
	ch := make(chan invokeResult, 1)
	go func() {
		res, err := c.Invoke(ctx, path, nil, opts)
		ch <- invokeResult{res, err}
	}()
	return ch
}

// ciRequest decodes the InvokeRequest the manager sent.
func ciRequest(t *testing.T, m msg) im.InvokeRequest {
	t.Helper()
	if m.proto.Opcode != im.OpcodeInvokeRequest {
		t.Fatalf("opcode 0x%02X, want InvokeRequest", m.proto.Opcode)
	}
	req, err := im.UnmarshalInvokeRequestTLV(tlv.NewDecoder(m.payload), nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

// ciRefLess answers like RefLessDeviceExchangeProvider
// (ClientInteractionInvokeTest.ts:221-254): a ref-less Success per command.
func ciRefLess(t *testing.T, s *scripted, m msg) im.InvokeRequest {
	t.Helper()
	req := ciRequest(t, m)
	entries := make([]im.InvokeResponseEntry, 0, len(req.Invokes))
	for _, inv := range req.Invokes {
		entries = append(entries, im.InvokeResponseEntry{Path: inv.Path, IsStatus: true, Status: im.StatusIB{Status: im.StatusSuccess}})
	}
	s.reply(t, m, im.InteractionModelProtocolID, im.OpcodeInvokeResponse, invokeResponse(t, false, entries...))
	return req
}

func ciNothingSent(t *testing.T, s *scripted) {
	t.Helper()
	if n := len(s.conn.ch); n != 0 {
		t.Fatalf("%d datagrams sent", n)
	}
}

// ciNoRetransmission advances past the MRP backoff of an abandoned
// request: the exchange no longer sends anything.
func ciNoRetransmission(t *testing.T, s *scripted) {
	t.Helper()
	for range 10 {
		s.clock.Advance(5 * time.Second)
	}
	ciNothingSent(t, s)
}

func TestClientInteractionInvokeCommandRefWireHandling(t *testing.T) {
	t.Parallel()
	t.Run("sends a single-command invoke without commandRef and matches the ref-less response", func(t *testing.T) {
		t.Parallel()
		// The Go Invoke takes no commandRef from the caller; the single
		// command goes without one and the ref-less answer is its result.
		s := newScripted(t)
		c := interaction.New(s.mgr, s.session, nil)
		done := ciInvoke(context.Background(), c, ciPath(1, ciOff), interaction.InvokeOptions{})
		req := ciRefLess(t, s, s.next(t))
		if len(req.Invokes) != 1 || req.Invokes[0].HasCommandRef || req.Invokes[0].Path != ciPath(1, ciOff) {
			t.Fatalf("request %+v", req)
		}
		r := await(t, done)
		if r.err != nil || r.res.Status != im.StatusSuccess || r.res.HasFields || r.res.ClusterStatus != nil {
			t.Fatalf("result %+v (%v)", r.res, r.err)
		}
	})
	t.Run("abandons an in-flight command when the caller's own request signal aborts", func(t *testing.T) {
		t.Parallel()
		s := newScripted(t)
		c := interaction.New(s.mgr, s.session, nil)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := ciInvoke(ctx, c, ciPath(1, ciOff), interaction.InvokeOptions{})
		s.ack(t, s.next(t)) // the request left; the peer never answers
		cancel()
		if r := await(t, done); !errors.Is(r.err, context.Canceled) {
			t.Fatalf("err %v, want context.Canceled", r.err)
		}
	})
	t.Run("stops a batch's own exchange once every command in it has been abandoned", func(t *testing.T) {
		t.Parallel()
		// The exchange the request went out on is what keeps
		// retransmitting (ClientInteractionInvokeTest.ts:487-488): once
		// the command is abandoned it sends nothing more.
		s := newScripted(t)
		c := interaction.New(s.mgr, s.session, nil)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := ciInvoke(ctx, c, ciPath(1, ciOff), interaction.InvokeOptions{})
		ciRequest(t, s.next(t))
		cancel()
		if r := await(t, done); !errors.Is(r.err, context.Canceled) {
			t.Fatalf("err %v, want context.Canceled", r.err)
		}
		ciNoRetransmission(t, s)
	})
	t.Run("stops a batch whose two commands share one abort signal", func(t *testing.T) {
		t.Parallel()
		s := newScripted(t)
		c := interaction.New(s.mgr, s.session, nil)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		one := ciInvoke(ctx, c, ciPath(1, ciOff), interaction.InvokeOptions{})
		two := ciInvoke(ctx, c, ciPath(2, ciOn), interaction.InvokeOptions{})
		ciRequest(t, s.next(t))
		ciRequest(t, s.next(t))
		cancel()
		for _, done := range []<-chan invokeResult{one, two} {
			if r := await(t, done); !errors.Is(r.err, context.Canceled) {
				t.Fatalf("err %v, want context.Canceled", r.err)
			}
		}
		ciNoRetransmission(t, s)
	})
	t.Run("abandons a read whose request signal aborts, not only an invoke", func(t *testing.T) {
		t.Parallel()
		s := newScripted(t)
		c := interaction.New(s.mgr, s.session, nil)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() {
			_, err := c.ReadWith(ctx, interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{{Endpoint: 1, Cluster: ciOnOffCluster, HasEndpoint: true, HasCluster: true}}})
			done <- err
		}()
		if m := s.next(t); m.proto.Opcode != im.OpcodeReadRequest {
			t.Fatalf("opcode 0x%02X, want ReadRequest", m.proto.Opcode)
		}
		cancel()
		if err := await(t, done); !errors.Is(err, context.Canceled) {
			t.Fatalf("err %v, want context.Canceled", err)
		}
	})
	t.Run("throws when a request carries an already-aborted signal, before sending anything", func(t *testing.T) {
		t.Parallel()
		s := newScripted(t)
		c := interaction.New(s.mgr, s.session, nil)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := c.Invoke(ctx, ciPath(1, ciOff), nil, interaction.InvokeOptions{}); !errors.Is(err, context.Canceled) {
			t.Fatalf("err %v, want context.Canceled", err)
		}
		ciNothingSent(t, s)
	})
	t.Run("throws when invoked with an already-aborted session signal", func(t *testing.T) {
		t.Parallel()
		// The session's signal is the same context in Go: one that ended
		// before the call — here by its deadline — refuses it unsent.
		s := newScripted(t)
		c := interaction.New(s.mgr, s.session, nil)
		ctx, cancel := context.WithDeadline(context.Background(), time.Unix(0, 0))
		defer cancel()
		if _, err := c.Invoke(ctx, ciPath(1, ciOff), nil, interaction.InvokeOptions{}); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err %v, want context.DeadlineExceeded", err)
		}
		ciNothingSent(t, s)
	})
	t.Run("honors suppressResponse for a single command instead of batching it", func(t *testing.T) {
		t.Parallel()
		s := newScripted(t)
		c := interaction.New(s.mgr, s.session, nil)
		done := ciInvoke(context.Background(), c, ciPath(1, ciOff), interaction.InvokeOptions{SuppressResponse: true})
		m := s.next(t)
		if req := ciRequest(t, m); !req.SuppressResponse || len(req.Invokes) != 1 {
			t.Fatalf("request %+v", req)
		}
		s.ack(t, m)
		if r := await(t, done); r.err != nil || r.res.HasFields || r.res.Command != 0 || r.res.Status != im.StatusSuccess || r.res.ClusterStatus != nil {
			t.Fatalf("result %+v (%v), want none", r.res, r.err)
		}
		ciNothingSent(t, s)
	})
	t.Run("invokes concurrent single commands directly and ref-less when peer maxPathsPerInvoke is 1", func(t *testing.T) {
		t.Parallel()
		s := newScripted(t)
		c := interaction.New(s.mgr, s.session, nil)
		first := ciInvoke(context.Background(), c, ciPath(1, ciOff), interaction.InvokeOptions{})
		second := ciInvoke(context.Background(), c, ciPath(2, ciOff), interaction.InvokeOptions{})
		for range 2 {
			req := ciRefLess(t, s, s.next(t))
			if len(req.Invokes) != 1 || req.Invokes[0].HasCommandRef {
				t.Fatalf("request %+v", req)
			}
		}
		for _, done := range []<-chan invokeResult{first, second} {
			if r := await(t, done); r.err != nil || r.res.Status != im.StatusSuccess {
				t.Fatalf("result %+v (%v)", r.res, r.err)
			}
		}
	})
}

// TestClientInteractionInvokeLargeMessageInference ports the inference case
// (ClientInteractionInvokeTest.ts:633-661 at 862ad75f): an invoke of
// WebRtcTransportProvider.ProvideOffer, a command with the "L" quality,
// infers LargeMessage on a copy (ClientInteraction.ts:716-719) — the
// caller's options keep their nil. The session-bound rig ignores the
// required transport (ClientRequest.ts:81-84), so the request goes out.
func TestClientInteractionInvokeLargeMessageInference(t *testing.T) {
	t.Parallel()
	t.Run("does not mutate the caller's request when inferring largeMessage", func(t *testing.T) {
		t.Parallel()
		s := newScripted(t)
		c := interaction.New(s.mgr, s.session, nil)
		provideOffer := im.ConcreteCommandPath{Endpoint: 2, Cluster: 0x0553, Command: 0x02, HasEndpoint: true, HasCluster: true, HasCommand: true}
		opts := interaction.InvokeOptions{}
		done := ciInvoke(context.Background(), c, provideOffer, opts)
		req := ciRefLess(t, s, s.next(t))
		if len(req.Invokes) != 1 || req.Invokes[0].Path != provideOffer {
			t.Fatalf("request %+v", req)
		}
		if r := await(t, done); r.err != nil {
			t.Fatal(r.err)
		}
		if opts.LargeMessage != nil {
			t.Errorf("caller's LargeMessage %v, want nil", *opts.LargeMessage)
		}
	})
}

func TestClientInteractionInvokeDelayReportData(t *testing.T) {
	t.Parallel()
	t.Run("sends no DelayReportData", func(t *testing.T) {
		t.Parallel()
		s := newScripted(t)
		c := interaction.New(s.mgr, s.session, nil)
		done := ciInvoke(context.Background(), c, ciPath(1, ciOff), interaction.InvokeOptions{})
		if req := ciRefLess(t, s, s.next(t)); req.DelayReportData != nil {
			t.Fatalf("DelayReportData %+v", req.DelayReportData)
		}
		await(t, done)
	})
}

func TestClientInteractionInvokeResponseWithoutInteractionModelRevision(t *testing.T) {
	t.Parallel()
	t.Run("accepts the response", func(t *testing.T) {
		t.Parallel()
		s := newScripted(t)
		c := interaction.New(s.mgr, s.session, nil)
		done := ciInvoke(context.Background(), c, ciPath(1, ciOn), interaction.InvokeOptions{})
		m := s.next(t)
		ciRequest(t, m)
		// successStatus(ON, 1) without the InteractionModelRevision
		// (ClientInteractionInvokeTest.ts:812-818).
		s.reply(t, m, im.InteractionModelProtocolID, im.OpcodeInvokeResponse, encode(t, func(e *tlv.Encoder) {
			e.StartStruct(tlv.AnonymousTag())
			e.PutBool(tlv.ContextTag(0), false)
			e.StartArray(tlv.ContextTag(1))
			e.StartStruct(tlv.AnonymousTag())
			e.StartStruct(tlv.ContextTag(1))
			e.StartList(tlv.ContextTag(0))
			e.PutUint(tlv.ContextTag(0), 1)
			e.PutUint(tlv.ContextTag(1), ciOnOffCluster)
			e.PutUint(tlv.ContextTag(2), ciOn)
			_ = e.EndContainer()
			e.StartStruct(tlv.ContextTag(1))
			e.PutUint(tlv.ContextTag(0), uint64(im.StatusSuccess))
			_ = e.EndContainer()
			e.PutUint(tlv.ContextTag(2), 1)
			_ = e.EndContainer()
			_ = e.EndContainer()
			_ = e.EndContainer()
			_ = e.EndContainer()
		}))
		if r := await(t, done); r.err != nil || r.res.Status != im.StatusSuccess {
			t.Fatalf("result %+v (%v)", r.res, r.err)
		}
	})
}

func TestClientInteractionInvokeStatusOrigin(t *testing.T) {
	t.Parallel()
	t.Run("turns a status the peer sent into a received error", func(t *testing.T) {
		t.Parallel()
		s := newScripted(t)
		c := interaction.New(s.mgr, s.session, nil)
		done := ciInvoke(context.Background(), c, ciPath(1, ciOn), interaction.InvokeOptions{})
		m := s.next(t)
		ciRequest(t, m)
		s.reply(t, m, im.InteractionModelProtocolID, im.OpcodeInvokeResponse, invokeResponse(t, false, im.InvokeResponseEntry{
			Path: ciPath(1, ciOn), IsStatus: true, Status: im.StatusIB{Status: im.StatusFailure}, CommandRef: 1, HasCommandRef: true,
		}))
		var se *interaction.StatusError
		if r := await(t, done); !errors.As(r.err, &se) || se.Status != im.StatusFailure {
			t.Fatalf("err %v, want the received Failure", r.err)
		}
	})
	t.Run("turns a synthesized NoCommandResponse into a local error", func(t *testing.T) {
		t.Parallel()
		// The single command's response carries no entry for it: the
		// local ErrNoResponse, not a status the peer sent.
		s := newScripted(t)
		c := interaction.New(s.mgr, s.session, nil)
		done := ciInvoke(context.Background(), c, ciPath(1, ciOff), interaction.InvokeOptions{})
		m := s.next(t)
		ciRequest(t, m)
		s.reply(t, m, im.InteractionModelProtocolID, im.OpcodeInvokeResponse, invokeResponse(t, false))
		r := await(t, done)
		var se *interaction.StatusError
		if !errors.Is(r.err, interaction.ErrNoResponse) || errors.As(r.err, &se) {
			t.Fatalf("err %v, want the local ErrNoResponse", r.err)
		}
	})
	t.Run("throws a received error when the peer answers with a StatusResponse message", func(t *testing.T) {
		t.Parallel()
		s := newScripted(t)
		c := interaction.New(s.mgr, s.session, nil)
		done := ciInvoke(context.Background(), c, ciPath(1, ciOn), interaction.InvokeOptions{})
		m := s.next(t)
		ciRequest(t, m)
		s.reply(t, m, im.InteractionModelProtocolID, im.OpcodeStatusResponse, statusResponse(t, im.StatusUnsupportedAccess))
		var se *interaction.StatusError
		if r := await(t, done); !errors.As(r.err, &se) || se.Status != im.StatusUnsupportedAccess {
			t.Fatalf("err %v, want the received UnsupportedAccess", r.err)
		}
	})
}
