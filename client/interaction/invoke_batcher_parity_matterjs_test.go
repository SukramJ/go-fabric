// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction_test

// Ports of matter.js at 862ad75f, by meaning, of the cases about the
// batcher of single commands (ClientInteraction.#invokeWithBatching,
// packages/protocol/src/action/client/ClientInteraction.ts:744-1005):
// packages/node/test/node/ClientInvokeTest.ts (the batcher, its bypasses,
// per-request options, lanes per network, close and abort) and
// packages/protocol/test/action/client/ClientInteractionInvokeTest.ts (the
// close cases). The scripted rig stands in for the peer: its session
// reports a MaxPathsPerInvoke, the batch timer runs on the fake clock, so
// a test queues its commands, sees them queued and advances the clock
// past BatchDuration — matter.js's MockTime.resolve.

import (
	"context"
	"errors"
	"net"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
)

const ciToggle = 2

// batchWindow is the BatchDuration the cases queue their commands with.
const batchWindow = time.Second

// batchingPair is ClientInvokeTest.ts's batchingPair: a peer of
// MaxPathsPerInvoke maxPaths and the interaction over its session, its
// timers on the rig's clock.
func batchingPair(t *testing.T, maxPaths uint16) (*scripted, *interaction.Interaction) {
	t.Helper()
	s := newScriptedWith(t, &sessionparams.Parameters{MaxPathsPerInvoke: maxPaths})
	c := interaction.Bind(interaction.SessionProvider(s.mgr, s.session), interaction.Config{Clock: s.clock})
	t.Cleanup(func() { c.Close(nil) })
	return s, c
}

// queued lets the bubble settle and fails unless n commands wait in c's
// batch lanes. Every case runs in a synctest bubble: the batch timer is
// the scripted pair's fake clock, so a settled bubble is the batcher's
// final word until the test advances it.
func queued(t *testing.T, c *interaction.Interaction, n int) {
	t.Helper()
	synctest.Wait()
	if got := interaction.QueuedInvokes(c); got != n {
		t.Fatalf("%d commands queued, want %d", got, n)
	}
}

// batched queues the command for the batch window.
func batched() interaction.InvokeOptions {
	return interaction.InvokeOptions{BatchDuration: batchWindow}
}

// answerByRef answers req with a Success status per command, echoing each
// command's CommandRef — in reverse order, so the demultiplexing is by
// reference and not by position.
func answerByRef(t *testing.T, s *scripted, m msg) im.InvokeRequest {
	t.Helper()
	req := ciRequest(t, m)
	entries := make([]im.InvokeResponseEntry, 0, len(req.Invokes))
	for i := len(req.Invokes) - 1; i >= 0; i-- {
		inv := req.Invokes[i]
		entries = append(entries, im.InvokeResponseEntry{
			Path: inv.Path, IsStatus: true, Status: im.StatusIB{Status: im.StatusSuccess},
			CommandRef: inv.CommandRef, HasCommandRef: inv.HasCommandRef,
		})
	}
	s.reply(t, m, im.InteractionModelProtocolID, im.OpcodeInvokeResponse, invokeResponse(t, false, entries...))
	return req
}

func commandsOf(req im.InvokeRequest) []uint32 {
	out := make([]uint32, len(req.Invokes))
	for i, inv := range req.Invokes {
		out[i] = inv.Path.Command
	}
	return out
}

func wantSuccess(t *testing.T, chs ...<-chan invokeResult) {
	t.Helper()
	for i, ch := range chs {
		if r := await(t, ch); r.err != nil || r.res.Status != im.StatusSuccess {
			t.Fatalf("command %d: %+v (%v)", i, r.res, r.err)
		}
	}
}

// TestInvokeBatcherParity covers ClientInvokeTest.ts's batcher cases.
func TestInvokeBatcherParity(t *testing.T) {
	t.Parallel()
	t.Run("commands without options share a batch (characterization)", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			// ClientInvokeTest.ts:734-751, and "executes commands via the
			// batcher" (:168-195): two commands of one tick go as one
			// InvokeRequest, each numbered by its CommandRef, and each caller
			// gets its own answer.
			s, c := batchingPair(t, 10)
			on := ciInvoke(context.Background(), c, ciPath(1, ciOn), batched())
			queued(t, c, 1)
			toggle := ciInvoke(context.Background(), c, ciPath(1, ciToggle), batched())
			queued(t, c, 2)
			ciNothingSent(t, s)
			s.clock.Advance(batchWindow)
			req := answerByRef(t, s, s.next(t))
			if got := commandsOf(req); len(got) != 2 || got[0] != ciOn || got[1] != ciToggle {
				t.Fatalf("commands %v, want [On Toggle]", got)
			}
			if !req.Invokes[0].HasCommandRef || !req.Invokes[1].HasCommandRef || req.Invokes[0].CommandRef == req.Invokes[1].CommandRef {
				t.Fatalf("command refs %+v", req.Invokes)
			}
			wantSuccess(t, on, toggle)
		})
	})
	t.Run("requires MockTime.resolve for batched commands (non-root endpoints)", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			// :197-231: a batched command waits for the batch timer.
			s, c := batchingPair(t, 10)
			done := ciInvoke(context.Background(), c, ciPath(1, ciToggle), batched())
			queued(t, c, 1)
			select {
			case r := <-done:
				t.Fatalf("resolved before the timer: %+v", r)
			default:
			}
			s.clock.Advance(batchWindow)
			req := ciRefLess(t, s, s.next(t))
			if len(req.Invokes) != 1 || req.Invokes[0].HasCommandRef {
				t.Fatalf("a batch of one sends no CommandRef: %+v", req.Invokes)
			}
			wantSuccess(t, done)
		})
	})
	t.Run("bypasses batching when maxPathsPerInvoke is 1", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			// :233-247: the command goes at once, no timer.
			s, c := batchingPair(t, 1)
			done := ciInvoke(context.Background(), c, ciPath(1, ciToggle), batched())
			ciRefLess(t, s, s.next(t))
			wantSuccess(t, done)
			if n := interaction.QueuedInvokes(c); n != 0 {
				t.Fatalf("%d queued", n)
			}
		})
	})
	t.Run("batches different commands together while splitting duplicates", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			// :382-415 and "splits duplicate command paths into separate
			// batches" (:973-1003): on, toggle, on — the second on goes into a
			// batch of its own, after the first.
			s, c := batchingPair(t, 10)
			chs := make([]<-chan invokeResult, 0, 3)
			for i, cmd := range []uint32{ciOn, ciToggle, ciOn} {
				chs = append(chs, ciInvoke(context.Background(), c, ciPath(1, cmd), batched()))
				queued(t, c, i+1)
			}
			s.clock.Advance(batchWindow)
			first := answerByRef(t, s, s.next(t))
			second := ciRefLess(t, s, s.next(t))
			if got := commandsOf(first); len(got) != 2 || got[0] != ciOn || got[1] != ciToggle {
				t.Fatalf("first batch %v", got)
			}
			if got := commandsOf(second); len(got) != 1 || got[0] != ciOn || second.Invokes[0].HasCommandRef {
				t.Fatalf("second batch %+v", second.Invokes)
			}
			wantSuccess(t, chs...)
		})
	})
	t.Run("correctly splits different-path commands when maxPathsPerInvoke is 2", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			// :935-971 with a limit the batcher can use: three commands of one
			// batch go as a part of two (numbered) and a part of one
			// (ref-less), each request with only its own commands.
			s, c := batchingPair(t, 2)
			chs := make([]<-chan invokeResult, 0, 3)
			for i, ep := range []uint16{1, 2, 3} {
				chs = append(chs, ciInvoke(context.Background(), c, ciPath(ep, ciToggle), batched()))
				queued(t, c, i+1)
			}
			s.clock.Advance(batchWindow)
			a, b := s.next(t), s.next(t)
			sizes := map[int]bool{}
			for _, m := range []msg{a, b} {
				req := answerByRef(t, s, m)
				sizes[len(req.Invokes)] = true
				if len(req.Invokes) == 1 && req.Invokes[0].HasCommandRef {
					t.Fatal("a part of one carries a CommandRef")
				}
			}
			if !sizes[1] || !sizes[2] {
				t.Fatalf("part sizes %v, want 2 and 1", sizes)
			}
			wantSuccess(t, chs...)
		})
	})
	t.Run("rejects pending commands when batcher is closed", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			// :314-345: Close rejects the queued commands with the close
			// reason, nothing is sent.
			s, c := batchingPair(t, 10)
			a := ciInvoke(context.Background(), c, ciPath(1, ciToggle), batched())
			queued(t, c, 1)
			b := ciInvoke(context.Background(), c, ciPath(2, ciToggle), batched())
			queued(t, c, 2)
			c.Close(nil)
			for _, ch := range []<-chan invokeResult{a, b} {
				if r := await(t, ch); !errors.Is(r.err, interaction.ErrClosed) {
					t.Fatalf("err %v, want ErrClosed", r.err)
				}
			}
			s.clock.Advance(batchWindow)
			ciNothingSent(t, s)
			if _, err := c.Invoke(context.Background(), ciPath(1, ciOn), nil, batched()); !errors.Is(err, interaction.ErrClosed) {
				t.Fatalf("invoke after close: %v", err)
			}
		})
	})
	t.Run("bypasses batching for timed invoke requests", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			// :347-380 and "timed command goes out alone after a TimedRequest"
			// (:578-594).
			s, c := batchingPair(t, 10)
			plain := ciInvoke(context.Background(), c, ciPath(1, ciOn), batched())
			queued(t, c, 1)
			opts := batched()
			opts.Timed = true
			timed := ciInvoke(context.Background(), c, ciPath(1, ciToggle), opts)
			tr := s.next(t)
			if tr.proto.Opcode != im.OpcodeTimedRequest {
				t.Fatalf("opcode 0x%02X, want TimedRequest", tr.proto.Opcode)
			}
			s.reply(t, tr, im.InteractionModelProtocolID, im.OpcodeStatusResponse, statusResponse(t, im.StatusSuccess))
			req := ciRefLess(t, s, s.next(t))
			if !req.TimedRequest || len(req.Invokes) != 1 {
				t.Fatalf("timed request %+v", req)
			}
			wantSuccess(t, timed)
			s.clock.Advance(batchWindow)
			ciRefLess(t, s, s.next(t))
			wantSuccess(t, plain)
		})
	})
	t.Run("infers a timed invoke from the model and keeps it out of the batch", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			// W4-ctlim-02: DoorLock LockDoor (endpoint 1) is timed in the
			// model; with zero options it goes alone after a TimedRequest, as
			// matter.js Invoke.ts:136-139 infers and ClientInteraction.ts:200
			// keeps it out of the batch.
			s, c := batchingPair(t, 10)
			lock := im.ConcreteCommandPath{Endpoint: 1, Cluster: 0x0101, Command: 0x00}
			done := ciInvoke(context.Background(), c, lock, batched())
			tr := s.next(t)
			if tr.proto.Opcode != im.OpcodeTimedRequest {
				t.Fatalf("opcode 0x%02X, want TimedRequest", tr.proto.Opcode)
			}
			s.reply(t, tr, im.InteractionModelProtocolID, im.OpcodeStatusResponse, statusResponse(t, im.StatusSuccess))
			req := ciRefLess(t, s, s.next(t))
			if !req.TimedRequest || len(req.Invokes) != 1 || req.Invokes[0].Path.Cluster != 0x0101 {
				t.Fatalf("timed request %+v", req)
			}
			wantSuccess(t, done)
			if n := interaction.QueuedInvokes(c); n != 0 {
				t.Fatalf("%d commands queued", n)
			}
		})
	})
	t.Run("respects TimedOff for a timed command", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			// matter.js `options.timed === false` is respected "even if we know
			// it better" (Invoke.ts:136-139): no TimedRequest. The command goes
			// on its own, as the batch request would infer timed again.
			s, c := batchingPair(t, 10)
			lock := im.ConcreteCommandPath{Endpoint: 1, Cluster: 0x0101, Command: 0x00}
			opts := batched()
			opts.TimedOff = true
			done := ciInvoke(context.Background(), c, lock, opts)
			req := ciRefLess(t, s, s.next(t))
			if req.TimedRequest {
				t.Fatalf("request %+v: want untimed", req)
			}
			wantSuccess(t, done)
		})
	})
	t.Run("per-request options never join a batch", func(t *testing.T) {
		t.Parallel()
		// :543-576 (sendDirect), "command with suppressResponse goes out
		// alone" (:596-610), "command with its own MRP delay keeps it"
		// (:698-713), "direct invoke keeps its expected processing time"
		// (:867-882) and "does not batch L commands" of
		// InvokeLargeMessageTest.ts: a command that sets an option the
		// batch request does not rebuild goes at once and on its own
		// (isBatchable, ClientInteraction.ts:185-214), and the option
		// reaches its InvokeRequest. The interactionModelRevision row is
		// matter.js's fallback − 1 (:549-552): one below this module's
		// default; the delayReportData row is :557. The beforeDispatch row
		// (:554) is a register left: a server-only field (Invoke.ts:52-55).
		delay := 3 * time.Second
		large := true
		rows := []struct {
			name string
			opts interaction.InvokeOptions
		}{
			{"expectedProcessingTime", interaction.InvokeOptions{ExpectedProcessingTime: 90 * time.Second}},
			{"useExtendedFailSafeMessageResponseTimeout", interaction.InvokeOptions{ExtendedFailSafeTimeout: true}},
			{"additionalMrpDelay", interaction.InvokeOptions{ClientRequest: interaction.ClientRequest{AdditionalMrpDelay: &delay}}},
			{"timeout", interaction.InvokeOptions{TimedTimeout: 5 * time.Second}},
			{"suppressResponse", interaction.InvokeOptions{SuppressResponse: true}},
			{"largeMessage", interaction.InvokeOptions{ClientRequest: interaction.ClientRequest{LargeMessage: &large}}},
			{"batchDuration false", interaction.InvokeOptions{NoBatch: true, ExpectedProcessingTime: 90 * time.Second}},
			{"interactionModelRevision", interaction.InvokeOptions{InteractionModelRevision: im.MatterInteractionModelRevision - 1}},
			{"delayReportData", interaction.InvokeOptions{DelayReportData: &im.DelayReportData{}}},
			// Go only: ResponseArrived has no matter.js counterpart (issue 464).
			{"responseArrived", interaction.InvokeOptions{ResponseArrived: func(interaction.Result) {}}},
		}
		for _, row := range rows {
			t.Run(row.name, func(t *testing.T) {
				t.Parallel()
				synctest.Test(t, func(t *testing.T) {
					s, c := batchingPair(t, 10)
					plain := ciInvoke(context.Background(), c, ciPath(1, ciOn), batched())
					queued(t, c, 1)
					opts := row.opts
					opts.BatchDuration = batchWindow
					direct := ciInvoke(context.Background(), c, ciPath(1, ciToggle), opts)
					m := s.next(t)
					if m.proto.Opcode == im.OpcodeTimedRequest {
						s.reply(t, m, im.InteractionModelProtocolID, im.OpcodeStatusResponse, statusResponse(t, im.StatusSuccess))
						m = s.next(t)
					}
					req := ciRequest(t, m)
					if len(req.Invokes) != 1 || req.Invokes[0].Path.Command != ciToggle {
						t.Fatalf("direct request %+v", req.Invokes)
					}
					if want := row.opts.InteractionModelRevision; want != 0 && req.InteractionModelRevision != want {
						t.Fatalf("revision %d on the wire, want %d", req.InteractionModelRevision, want)
					}
					if want := row.opts.DelayReportData; want != nil && (req.DelayReportData == nil || *req.DelayReportData != *want) {
						t.Fatalf("DelayReportData %+v on the wire, want %+v", req.DelayReportData, want)
					}
					if req.SuppressResponse {
						s.ack(t, m)
					} else {
						ciRefLess(t, s, m)
					}
					if r := await(t, direct); r.err != nil {
						t.Fatalf("direct: %v", r.err)
					}
					if n := interaction.QueuedInvokes(c); n != 1 {
						t.Fatalf("%d queued, want the plain command alone", n)
					}
					s.clock.Advance(batchWindow)
					if req := ciRefLess(t, s, s.next(t)); len(req.Invokes) != 1 || req.Invokes[0].Path.Command != ciOn {
						t.Fatalf("batch %+v", req.Invokes)
					}
					wantSuccess(t, plain)
				})
			})
		}
	})
	t.Run("command without an endpoint never joins a batch", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			// :632-650 by meaning: a Go invoke names a concrete path, and the
			// one isBatchable keeps out with it is endpoint 0, the root
			// endpoint (ClientInteraction.ts:186, :188).
			s, c := batchingPair(t, 10)
			done := ciInvoke(context.Background(), c, ciPath(0, ciToggle), batched())
			ciRefLess(t, s, s.next(t))
			wantSuccess(t, done)
		})
	})
	t.Run("abort and network do not keep a command out of the batch", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			// :652-670: a cancelable context and a network join the batch of
			// that network's lane.
			s, c := batchingPair(t, 10)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			opts := batched()
			opts.Network = "unlimited"
			a := ciInvoke(ctx, c, ciPath(1, ciOn), opts)
			queued(t, c, 1)
			b := ciInvoke(ctx, c, ciPath(1, ciToggle), opts)
			queued(t, c, 2)
			s.clock.Advance(batchWindow)
			if req := answerByRef(t, s, s.next(t)); len(req.Invokes) != 2 {
				t.Fatalf("%d commands, want 2", len(req.Invokes))
			}
			wantSuccess(t, a, b)
		})
	})
	t.Run("command with options goes out alone and ahead of the batch invoked before it", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			// :753-771.
			s, c := batchingPair(t, 10)
			on := ciInvoke(context.Background(), c, ciPath(1, ciOn), batched())
			queued(t, c, 1)
			off := ciInvoke(context.Background(), c, ciPath(1, ciOff), interaction.InvokeOptions{ExpectedProcessingTime: 90 * time.Second})
			first := ciRefLess(t, s, s.next(t))
			toggle := ciInvoke(context.Background(), c, ciPath(1, ciToggle), batched())
			queued(t, c, 2)
			s.clock.Advance(batchWindow)
			second := answerByRef(t, s, s.next(t))
			if got := commandsOf(first); len(got) != 1 || got[0] != ciOff {
				t.Fatalf("first %v, want [Off]", got)
			}
			if got := commandsOf(second); len(got) != 2 || got[0] != ciOn || got[1] != ciToggle {
				t.Fatalf("second %v, want [On Toggle]", got)
			}
			wantSuccess(t, on, off, toggle)
		})
	})
	t.Run("commands for different networks go into separate batches", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			// :773-788, and "batches for different networks do not wait on
			// each other" (:790-806): the lane of one network flushes at its
			// own deadline.
			s, c := batchingPair(t, 10)
			a := ciInvoke(context.Background(), c, ciPath(1, ciOn), batched())
			queued(t, c, 1)
			opts := interaction.InvokeOptions{BatchDuration: 10 * time.Second, ClientRequest: interaction.ClientRequest{Network: "icdLit"}}
			b := ciInvoke(context.Background(), c, ciPath(1, ciToggle), opts)
			queued(t, c, 2)
			s.clock.Advance(batchWindow)
			if req := ciRefLess(t, s, s.next(t)); len(req.Invokes) != 1 || req.Invokes[0].Path.Command != ciOn {
				t.Fatalf("default lane %+v", req.Invokes)
			}
			wantSuccess(t, a)
			if n := interaction.QueuedInvokes(c); n != 1 {
				t.Fatalf("%d queued, want the icdLit command", n)
			}
			s.clock.Advance(9 * time.Second)
			if req := ciRefLess(t, s, s.next(t)); len(req.Invokes) != 1 || req.Invokes[0].Path.Command != ciToggle {
				t.Fatalf("icdLit lane %+v", req.Invokes)
			}
			wantSuccess(t, b)
		})
	})
	t.Run("a command that needs a faster flush restarts the timer", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			// ClientInteraction.ts:798-806: the running timer restarts with the
			// shorter duration and the batch takes both commands.
			s, c := batchingPair(t, 10)
			a := ciInvoke(context.Background(), c, ciPath(1, ciOn), interaction.InvokeOptions{BatchDuration: 10 * time.Second})
			queued(t, c, 1)
			b := ciInvoke(context.Background(), c, ciPath(1, ciToggle), batched())
			queued(t, c, 2)
			s.clock.Advance(batchWindow)
			if req := answerByRef(t, s, s.next(t)); len(req.Invokes) != 2 {
				t.Fatalf("%d commands, want 2", len(req.Invokes))
			}
			wantSuccess(t, a, b)
		})
	})
	t.Run("stops the batch exchange of a command whose acting context aborts", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			// :884-933: the batch of one abandoned command stops its exchange.
			s, c := batchingPair(t, 10)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := ciInvoke(ctx, c, ciPath(1, ciToggle), batched())
			queued(t, c, 1)
			s.clock.Advance(batchWindow)
			ciRequest(t, s.next(t))
			cancel()
			if r := await(t, done); !errors.Is(r.err, context.Canceled) {
				t.Fatalf("err %v, want context.Canceled", r.err)
			}
			// The command returns at once; its batch's exchange winds down on
			// the flush's goroutine.
			synctest.Wait()
			if n := interaction.FlushingBatches(c); n != 0 {
				t.Fatalf("%d batches still flushing after the bubble settled", n)
			}
			ciNoRetransmission(t, s)
		})
	})
	t.Run("an abandoned command leaves the batch to the others", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			// ClientInteraction.ts:780-792, :917-922: a command aborted before
			// the flush is dropped from it; the other goes alone.
			s, c := batchingPair(t, 10)
			ctx, cancel := context.WithCancel(context.Background())
			a := ciInvoke(ctx, c, ciPath(1, ciOn), batched())
			queued(t, c, 1)
			b := ciInvoke(context.Background(), c, ciPath(1, ciToggle), batched())
			queued(t, c, 2)
			cancel()
			if r := await(t, a); !errors.Is(r.err, context.Canceled) {
				t.Fatalf("err %v, want context.Canceled", r.err)
			}
			queued(t, c, 1)
			s.clock.Advance(batchWindow)
			if req := ciRefLess(t, s, s.next(t)); len(req.Invokes) != 1 || req.Invokes[0].Path.Command != ciToggle {
				t.Fatalf("batch %+v", req.Invokes)
			}
			wantSuccess(t, b)
		})
	})
	t.Run("a command left without an answer is NoResponse", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			// ClientInteraction.ts:990-995 with Invoke's contract: the peer
			// answers one command of two.
			s, c := batchingPair(t, 10)
			a := ciInvoke(context.Background(), c, ciPath(1, ciOn), batched())
			queued(t, c, 1)
			b := ciInvoke(context.Background(), c, ciPath(1, ciToggle), batched())
			queued(t, c, 2)
			s.clock.Advance(batchWindow)
			m := s.next(t)
			req := ciRequest(t, m)
			inv := req.Invokes[0]
			s.reply(t, m, im.InteractionModelProtocolID, im.OpcodeInvokeResponse, invokeResponse(t, false, im.InvokeResponseEntry{
				Path: inv.Path, IsStatus: true, Status: im.StatusIB{Status: im.StatusSuccess}, CommandRef: inv.CommandRef, HasCommandRef: true,
			}))
			wantSuccess(t, a)
			if r := await(t, b); !errors.Is(r.err, interaction.ErrNoResponse) {
				t.Fatalf("err %v, want ErrNoResponse", r.err)
			}
		})
	})
	t.Run("command queued while the interaction closes is rejected with the close reason", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			// ClientInvokeTest.ts:832-865: the connect the batcher awaits
			// before it queues the command is held, the interaction closes
			// meanwhile and the connect is released — the command is
			// rejected with the close reason and no invoke exchange opens.
			s := newScriptedWith(t, &sessionparams.Parameters{MaxPathsPerInvoke: 10})
			p, connecting, release := interaction.HoldConnect(interaction.SessionProvider(s.mgr, s.session))
			c := interaction.Bind(p, interaction.Config{Clock: s.clock})
			done := ciInvoke(context.Background(), c, ciPath(1, ciOn), batched())
			<-connecting
			reason := errors.New("closed for test")
			c.Close(reason)
			release()
			if r := await(t, done); !errors.Is(r.err, reason) {
				t.Fatalf("err %v, want the close reason", r.err)
			}
			s.clock.Advance(batchWindow)
			synctest.Wait()
			if n := interaction.QueuedInvokes(c); n != 0 {
				t.Fatalf("%d commands queued after the close", n)
			}
			ciNothingSent(t, s)
		})
	})
	t.Run("only a non-default interaction model revision keeps a command out of the batch", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			// :612-630: two commands naming the default revision share a
			// batch, the one naming default − 1 goes on its own — isBatchable
			// blocks a revision only when it is set and not the default
			// (ClientInteraction.ts:196-198). matter.js's default is
			// SessionParameters.fallbacks (Invoke.ts:102); this module's is
			// im.MatterInteractionModelRevision, the revision its batch
			// request carries.
			s, c := batchingPair(t, 10)
			opts := batched()
			opts.InteractionModelRevision = im.MatterInteractionModelRevision
			on := ciInvoke(context.Background(), c, ciPath(1, ciOn), opts)
			queued(t, c, 1)
			toggle := ciInvoke(context.Background(), c, ciPath(1, ciToggle), opts)
			queued(t, c, 2)
			opts.InteractionModelRevision = im.MatterInteractionModelRevision - 1
			off := ciInvoke(context.Background(), c, ciPath(1, ciOff), opts)
			direct := ciRefLess(t, s, s.next(t))
			s.clock.Advance(batchWindow)
			batch := answerByRef(t, s, s.next(t))
			counts := []int{len(direct.Invokes), len(batch.Invokes)}
			slices.Sort(counts)
			if !slices.Equal(counts, []int{1, 2}) {
				t.Fatalf("command counts %v, want [1 2]", counts)
			}
			if direct.Invokes[0].Path.Command != ciOff || direct.InteractionModelRevision != im.MatterInteractionModelRevision-1 {
				t.Fatalf("direct request %+v", direct)
			}
			if batch.InteractionModelRevision != im.MatterInteractionModelRevision {
				t.Fatalf("batch revision %d", batch.InteractionModelRevision)
			}
			wantSuccess(t, on, toggle, off)
		})
	})
	t.Run("command with an address override keeps it", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			// :715-732: a command with an address override goes on its own
			// (ClientInteraction.ts:209) and its exchange carries the
			// override (:1254, ClientRequest.ts:34-40); the batched command
			// goes to the session's address. matter.js overrides with the
			// peer's own address; another port here makes the override
			// observable on the datagram.
			s, c := batchingPair(t, 10)
			override := &net.UDPAddr{IP: peerAddr.IP, Port: peerAddr.Port + 1}
			toggle := ciInvoke(context.Background(), c, ciPath(1, ciToggle), batched())
			queued(t, c, 1)
			opts := batched()
			opts.AddressOverride = override
			on := ciInvoke(context.Background(), c, ciPath(1, ciOn), opts)
			m := s.next(t)
			direct := ciRefLess(t, s, m)
			if len(direct.Invokes) != 1 || direct.Invokes[0].Path.Command != ciOn || !m.to.IP.Equal(override.IP) || m.to.Port != override.Port {
				t.Fatalf("direct request %+v to %v, want On to %v", direct.Invokes, m.to, override)
			}
			s.clock.Advance(batchWindow)
			m = s.next(t)
			batch := ciRefLess(t, s, m)
			if len(batch.Invokes) != 1 || batch.Invokes[0].Path.Command != ciToggle || m.to.Port != peerAddr.Port {
				t.Fatalf("batch %+v to %v, want Toggle to the session's address", batch.Invokes, m.to)
			}
			wantSuccess(t, toggle, on)
		})
	})
	t.Run("command without a network and one naming the default network share a batch", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			// :811-830: an interaction constructed with network "unlimited"
			// puts a command without one in the lane of that network
			// (`request.network ?? this.#network`, ClientInteraction.ts:765),
			// and its exchange takes the network (:1250).
			s := newScriptedWith(t, &sessionparams.Parameters{MaxPathsPerInvoke: 10})
			p, networks := interaction.RecordNetworks(interaction.SessionProvider(s.mgr, s.session))
			c := interaction.Bind(p, interaction.Config{Clock: s.clock, Network: "unlimited"})
			t.Cleanup(func() { c.Close(nil) })
			on := ciInvoke(context.Background(), c, ciPath(1, ciOn), batched())
			queued(t, c, 1)
			opts := batched()
			opts.Network = "unlimited"
			toggle := ciInvoke(context.Background(), c, ciPath(1, ciToggle), opts)
			queued(t, c, 2)
			s.clock.Advance(batchWindow)
			if req := answerByRef(t, s, s.next(t)); len(req.Invokes) != 2 {
				t.Fatalf("%d commands, want one batch of 2", len(req.Invokes))
			}
			wantSuccess(t, on, toggle)
			if got := networks(); !slices.Equal(got, []string{"unlimited"}) {
				t.Fatalf("exchange networks %q, want [unlimited]", got)
			}
		})
	})
}

// TestInvokeBatcherCloseParity covers ClientInteractionInvokeTest.ts's
// close cases.
func TestInvokeBatcherCloseParity(t *testing.T) {
	t.Parallel()
	t.Run("aborts an in-flight batch on close instead of awaiting its response", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			// ClientInteractionInvokeTest.ts:416-434.
			s, c := batchingPair(t, 10)
			done := ciInvoke(context.Background(), c, ciPath(1, ciToggle), batched())
			queued(t, c, 1)
			s.clock.Advance(batchWindow)
			ciRequest(t, s.next(t))
			reason := errors.New("closed for test")
			c.Close(reason)
			if r := await(t, done); !errors.Is(r.err, reason) {
				t.Fatalf("err %v, want the close reason", r.err)
			}
		})
	})
	t.Run("rejects commands queued behind an in-flight batch with the close reason", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			// :609-631: the second batch (the duplicate path) is rejected with
			// the close reason without being sent.
			s, c := batchingPair(t, 10)
			first := ciInvoke(context.Background(), c, ciPath(1, ciToggle), batched())
			queued(t, c, 1)
			second := ciInvoke(context.Background(), c, ciPath(1, ciToggle), batched())
			queued(t, c, 2)
			s.clock.Advance(batchWindow)
			ciRequest(t, s.next(t))
			reason := errors.New("closed for test")
			c.Close(reason)
			for _, ch := range []<-chan invokeResult{first, second} {
				if r := await(t, ch); !errors.Is(r.err, reason) {
					t.Fatalf("err %v, want the close reason", r.err)
				}
			}
			ciNothingSent(t, s)
		})
	})
	t.Run("throws when invoked with an already-aborted signal, before sending anything", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			// :557-582 on the batched path (ClientInteraction.ts:749-751).
			s, c := batchingPair(t, 10)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if _, err := c.Invoke(ctx, ciPath(1, ciToggle), nil, batched()); !errors.Is(err, context.Canceled) {
				t.Fatalf("err %v, want context.Canceled", err)
			}
			if n := interaction.QueuedInvokes(c); n != 0 {
				t.Fatalf("%d queued", n)
			}
			ciNothingSent(t, s)
		})
	})
}

// TestInvokeNetworkParity covers ClientInvokeTest.ts's network propagation
// cases (:417-541 at 862ad75f): the network of an invoke reaches the
// exchange on the direct, the batched and the split path. matter.js spies
// NetworkProfiles.select (:421-436); the provider here records the network
// of every exchange it initiates (interaction.RecordNetworks), the
// options a peer provider hands NetworkProfiles.Select
// (provider.go peerProvider.InitiateExchange). matter.js's "unlimited"
// comes from ClientNodeInteraction.invoke, which names it for a request
// without a network (ClientNodeInteraction.ts:213-216); here the request
// names it.
func TestInvokeNetworkParity(t *testing.T) {
	t.Parallel()
	pair := func(t *testing.T, maxPaths uint16) (*scripted, *interaction.Interaction, func() []string) {
		t.Helper()
		s := newScriptedWith(t, &sessionparams.Parameters{MaxPathsPerInvoke: maxPaths})
		p, networks := interaction.RecordNetworks(interaction.SessionProvider(s.mgr, s.session))
		c := interaction.Bind(p, interaction.Config{Clock: s.clock})
		t.Cleanup(func() { c.Close(nil) })
		return s, c, networks
	}
	unlimited := func(o interaction.InvokeOptions) interaction.InvokeOptions {
		o.Network = "unlimited"
		return o
	}
	wantNetworks := func(t *testing.T, networks func() []string, n int) {
		t.Helper()
		got := networks()
		if len(got) != n {
			t.Fatalf("%d exchanges (%q), want %d", len(got), got, n)
		}
		for _, id := range got {
			if id != "unlimited" {
				t.Fatalf("exchange networks %q, want unlimited on each", got)
			}
		}
	}
	t.Run("direct invoke (#invokeSingle) propagates network to exchange", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			// :438-466: two concurrent invokes with batching off go on two
			// exchanges, each on the request's network.
			s, c, networks := pair(t, 10)
			on := ciInvoke(context.Background(), c, ciPath(1, ciOn), unlimited(interaction.InvokeOptions{NoBatch: true}))
			toggle := ciInvoke(context.Background(), c, ciPath(1, ciToggle), unlimited(interaction.InvokeOptions{NoBatch: true}))
			for range 2 {
				if req := ciRefLess(t, s, s.next(t)); len(req.Invokes) != 1 {
					t.Fatalf("%d commands, want 1", len(req.Invokes))
				}
			}
			wantSuccess(t, on, toggle)
			wantNetworks(t, networks, 2)
		})
	})
	t.Run("batched invoke (#invokeWithBatching) propagates network to exchange", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			// :468-492: on and toggle share one batch exchange, on the
			// network of their lane (ClientInteraction.ts:765, :934-958).
			s, c, networks := pair(t, 10)
			on := ciInvoke(context.Background(), c, ciPath(1, ciOn), unlimited(batched()))
			queued(t, c, 1)
			toggle := ciInvoke(context.Background(), c, ciPath(1, ciToggle), unlimited(batched()))
			queued(t, c, 2)
			s.clock.Advance(batchWindow)
			if req := answerByRef(t, s, s.next(t)); len(req.Invokes) != 2 {
				t.Fatalf("%d commands, want 2", len(req.Invokes))
			}
			wantSuccess(t, on, toggle)
			wantNetworks(t, networks, 1)
		})
	})
	t.Run("split invoke (#invokeWithSplitting) propagates network to each exchange", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			// :494-540: two commands of one request against MaxPathsPerInvoke
			// 1 go on two exchanges (ClientInteraction.ts:654-700), each on
			// the request's network.
			s, c, networks := pair(t, 1)
			done := make(chan batchResult, 1)
			go func() {
				res, err := c.InvokeBatch(context.Background(), []interaction.CommandRequest{{Path: ciPath(1, ciOn)}, {Path: ciPath(1, ciOff)}}, 1, unlimited(interaction.InvokeOptions{}))
				done <- batchResult{res, err}
			}()
			for range 2 {
				ciRefLess(t, s, s.next(t))
			}
			if r := await(t, done); r.err != nil || len(r.res) != 2 {
				t.Fatalf("results %+v (%v)", r.res, r.err)
			}
			wantNetworks(t, networks, 2)
		})
	})
}
