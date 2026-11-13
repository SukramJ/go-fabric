// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/spec"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
)

// TestStopReturnsWithItsContextWhileAHookBlocks (review II,
// R2-P0-conc-C03): a report's hooks run on the node's job queue and may
// block (Hooks); Stop drains that queue, and a hook that blocks past
// Stop's context — on the host's own I/O, or on a lock the goroutine
// calling Stop holds — no longer holds Stop: it returns the context's
// error and leaves the queue to finish. The stop is then not complete,
// and a second Stop, once the hook returned, completes it.
func TestStopReturnsWithItsContextWhileAHookBlocks(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		var armed, returned atomic.Bool
		entered := make(chan struct{}, 1)
		release := make(chan struct{})
		sr := newScriptedRegistry(t, seedCommissioned(3), func(c *client.RegistryConfig) {
			c.HooksFor = func(string) client.Hooks {
				return client.Hooks{
					Changed: func() {
						if !armed.Load() {
							return
						}
						select {
						case entered <- struct{}{}:
							<-release
							returned.Store(true)
						default:
						}
					},
				}
			}
		})
		n := sr.reg.Get("peer1")
		const id = 0x54
		sr.subscribe(t, n, sr.imageWithOpCreds("ours"), id)
		client.WaitRegistry(sr.reg)
		armed.Store(true)

		sr.send(t, message.ProtocolHeader{Initiator: true, NeedsAck: true, Opcode: im.OpcodeReportData, ExchangeID: 0x7104, ProtocolID: im.InteractionModelProtocolID},
			encode(t, func(e *tlv.Encoder) {
				im.ReportData{SubscriptionID: id, HasSubscription: true, Reports: []im.AttributeReport{value(0, basicinfo.ClusterID, 2, 10, tlvNode(uintNode(0xFFF4)))}}.MarshalTLV(e,
					func(e *tlv.Encoder, tag tlv.Tag, v im.AttributeValue) {
						interaction.Raw(v.Value.(spec.Node)).EncodeTLV(e, tag)
					})
			}))
		status := sr.next(t)
		sr.send(t, message.ProtocolHeader{Initiator: true, HasAck: true, AckCounter: status.hdr.MessageCounter, Opcode: 0x10, ExchangeID: 0x7104}, nil)
		<-entered // the hook holds the node's job queue

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		stopped := make(chan error, 1)
		go func() { stopped <- n.Stop(ctx) }()
		// Bubble time: well past the context's 50 ms.
		time.Sleep(time.Second)
		select {
		case err := <-stopped:
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("Stop = %v, want context.DeadlineExceeded", err)
			}
		default:
			close(release)
			<-stopped
			t.Fatal("Stop still waited for the node's jobs a second after its 50 ms context ended")
		}
		if returned.Load() {
			t.Fatal("the hook returned: Stop was not held by it")
		}

		// The queue finishes by itself; a second Stop completes the stop.
		close(release)
		if err := n.Stop(context.Background()); err != nil {
			t.Fatal(err)
		}
		if !returned.Load() {
			t.Fatal("the second Stop returned before the queued hook did")
		}
		client.WaitRegistry(sr.reg)
	})
}
