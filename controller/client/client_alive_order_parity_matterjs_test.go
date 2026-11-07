// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"context"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/spec"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
)

// TestSubscriptionAliveFollowsTheReportsHooks: SubscriptionAlive of a
// subscription report runs after the report's hooks — here its Changed
// and its StartUp event — on the node's job queue, as matter.js's updated
// iterates the whole mutate (drained queue, structural announcements,
// delayed events) before it emits subscriptionAlive (NetworkClient.ts:179-190,
// ClientStructure.ts:482-497 at db021da0). A consumer's batch end — the
// camera service's sweep — relies on it (brief FP2 §2). The report's
// Changed hook holds the queue; inline, the alive hook ran before the
// report's StatusResponse while the report's hooks still waited. In a
// synctest bubble: a hook that never runs deadlocks the bubble.
func TestSubscriptionAliveFollowsTheReportsHooks(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		var (
			armed atomic.Bool
			mu    sync.Mutex
			order []string
		)
		record := func(s string) {
			if armed.Load() {
				mu.Lock()
				order = append(order, s)
				mu.Unlock()
			}
		}
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
						default:
						}
						record("changed")
					},
					Event:             func(uint16, uint32, uint32, client.EventData, any) { record("event") },
					SubscriptionAlive: func() { record("alive") },
				}
			}
		})
		n := sr.reg.Get("peer1")
		const id = 0x52
		sr.subscribe(t, n, sr.imageWithOpCreds("ours"), id)
		client.WaitRegistry(sr.reg)
		armed.Store(true)

		sr.send(t, message.ProtocolHeader{Initiator: true, NeedsAck: true, Opcode: im.OpcodeReportData, ExchangeID: 0x7102, ProtocolID: im.InteractionModelProtocolID},
			encode(t, func(e *tlv.Encoder) {
				im.ReportData{
					SubscriptionID: id, HasSubscription: true,
					Reports:      []im.AttributeReport{value(0, basicinfo.ClusterID, 2, 9, tlvNode(uintNode(0xFFF3)))},
					EventReports: []im.EventReport{startUp(11)},
				}.MarshalTLV(e, func(e *tlv.Encoder, tag tlv.Tag, v im.AttributeValue) {
					interaction.Raw(v.Value.(spec.Node)).EncodeTLV(e, tag)
				})
			}))
		status := sr.next(t)
		sr.send(t, message.ProtocolHeader{Initiator: true, HasAck: true, AckCounter: status.hdr.MessageCounter, Opcode: 0x10, ExchangeID: 0x7102}, nil)
		<-entered // a bubble that deadlocks fails the test
		mu.Lock()
		early := slices.Clone(order)
		mu.Unlock()
		close(release)
		if slices.Contains(early, "alive") {
			t.Fatalf("the alive hook ran while the report's Changed hook held the queue: %v", early)
		}
		client.WaitRegistry(sr.reg)
		waitUntil(t, "the alive hook", func() bool {
			mu.Lock()
			defer mu.Unlock()
			return slices.Contains(order, "alive")
		})
		mu.Lock()
		got := slices.Clone(order)
		mu.Unlock()
		if len(got) != 3 || got[2] != "alive" || !slices.Contains(got, "changed") || !slices.Contains(got, "event") {
			t.Fatalf("hooks %v, want the report's Changed and Event, then alive", got)
		}
		if err := n.Stop(context.Background()); err != nil {
			t.Fatal(err)
		}
	})
}

// TestSubscriptionAliveNotAfterStop: an alive hook queued behind a report
// does not run once the node stopped meanwhile — matter.js asks isCurrent
// after the drain (NetworkClient.ts:184-189 at db021da0). Run after the
// stop, it latched the fabric-label reconciliation of a stopped node.
func TestSubscriptionAliveNotAfterStop(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		var armed, alive atomic.Bool
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
						default:
						}
					},
					SubscriptionAlive: func() {
						if armed.Load() {
							alive.Store(true)
						}
					},
				}
			}
		})
		n := sr.reg.Get("peer1")
		const id = 0x53
		sr.subscribe(t, n, sr.imageWithOpCreds("ours"), id)
		client.WaitRegistry(sr.reg)
		armed.Store(true)

		sr.send(t, message.ProtocolHeader{Initiator: true, NeedsAck: true, Opcode: im.OpcodeReportData, ExchangeID: 0x7103, ProtocolID: im.InteractionModelProtocolID},
			encode(t, func(e *tlv.Encoder) {
				im.ReportData{SubscriptionID: id, HasSubscription: true, Reports: []im.AttributeReport{value(0, basicinfo.ClusterID, 2, 10, tlvNode(uintNode(0xFFF4)))}}.MarshalTLV(e,
					func(e *tlv.Encoder, tag tlv.Tag, v im.AttributeValue) {
						interaction.Raw(v.Value.(spec.Node)).EncodeTLV(e, tag)
					})
			}))
		status := sr.next(t)
		sr.send(t, message.ProtocolHeader{Initiator: true, HasAck: true, AckCounter: status.hdr.MessageCounter, Opcode: 0x10, ExchangeID: 0x7103}, nil)
		<-entered // a bubble that deadlocks fails the test
		// Stop clears the subscription at once and then waits for it, and for
		// the node's jobs, to end; the held job is released once the
		// subscription is gone.
		stopped := make(chan error, 1)
		go func() { stopped <- n.Stop(context.Background()) }()
		waitUntil(t, "the stop", func() bool { return !client.HoldsDefaultSubscription(n) })
		close(release)
		if err := <-stopped; err != nil {
			t.Fatal(err)
		}
		client.WaitRegistry(sr.reg)
		if alive.Load() {
			t.Fatal("the alive hook queued behind the report ran after the node stopped")
		}
	})
}
