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
	"github.com/SukramJ/go-fabric/cluster/spec/descriptor"
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
			armed, live atomic.Bool
			mu          sync.Mutex
			order       []string
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
					Event:                     func(uint16, uint32, uint32, client.EventData, any) { record("event") },
					SubscriptionStatusChanged: func(active bool) { live.Store(active) },
					SubscriptionAlive:         func() { record("alive") },
				}
			}
		})
		n := sr.reg.Get("peer1")
		const id = 0x52
		sr.subscribe(t, n, sr.imageWithOpCreds("ours"), id)
		waitGoneLive(t, &live)
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
		waitUntil(t, "the alive hook", func() bool {
			mu.Lock()
			defer mu.Unlock()
			return slices.Contains(order, "alive")
		})
		// The StartUp event's registry reaction started before the alive;
		// it ends before the test does.
		client.WaitRegistry(sr.reg)
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
// after the drain (NetworkClient.ts:184-189 at b25ed960). Run after the
// stop, it latched the fabric-label reconciliation of a stopped node.
func TestSubscriptionAliveNotAfterStop(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		var armed, alive, live atomic.Bool
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
					SubscriptionStatusChanged: func(active bool) { live.Store(active) },
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
		waitGoneLive(t, &live)
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

// waitGoneLive waits until the subscription went live and its one alive —
// the going-live one, queued right after the status hook (network.go
// announceSubscriptionStatus) — has run: SubscriptionActive may read true
// before it is queued. Armed before it ran, a test took it for the report's
// alive (review II, phase 5: TestSubscriptionAliveNotAfterStop twice under
// `make race`). It must run in the bubble. The alive starts the registry's
// label reconciliation (Registry.hooksFor), so a client.WaitRegistry comes
// after it, never before.
func waitGoneLive(t *testing.T, live *atomic.Bool) {
	t.Helper()
	waitUntil(t, "the subscription's going-live announcement", live.Load)
	synctest.Wait()
}

// TestEstablishedAliveFollowsThePrimingReportsHooks: the alive of a
// subscription going live runs after the priming report's hooks — here the
// EndpointInstalled of an endpoint the priming report adds. matter.js goes
// live only once the priming report's updated drained
// (ClientInteraction.ts:1173-1174, SustainedSubscription.ts:224, :282 at
// db021da0) and then emits alive (NetworkClient.ts:224-234). The priming
// report's own alive has no subscription id yet, so the established alive
// is the batch end of the priming report, and the only alive.
func TestEstablishedAliveFollowsThePrimingReportsHooks(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		var (
			armed atomic.Bool
			mu    sync.Mutex
			order []string
		)
		record := func(s string) {
			mu.Lock()
			order = append(order, s)
			mu.Unlock()
		}
		entered := make(chan struct{}, 1)
		release := make(chan struct{})
		sr := newScriptedRegistry(t, seedCommissioned(3), func(c *client.RegistryConfig) {
			c.HooksFor = func(string) client.Hooks {
				return client.Hooks{
					EndpointInstalled: func(ep *client.Endpoint) {
						if !armed.Load() || ep.Number() != 1 {
							return
						}
						entered <- struct{}{}
						<-release
						record("endpoint 1")
					},
					SubscriptionAlive: func() {
						if armed.Load() {
							record("alive")
						}
					},
				}
			}
		})
		n := sr.reg.Get("peer1")
		// The bootstrap read knows the root only; the priming report adds
		// endpoint 1 and the root's PartsList naming it.
		var root, light []im.AttributeReport
		for _, r := range sr.imageWithOpCreds("ours") {
			switch {
			case r.Path.Endpoint == 1:
				light = append(light, r)
			case r.Path.Cluster == descriptor.ClusterID && r.Path.Attribute == descriptor.AttrPartsList:
				light = append(light, value(0, descriptor.ClusterID, descriptor.AttrPartsList, r.DataVersion+1, listN(1)))
				root = append(root, value(0, descriptor.ClusterID, descriptor.AttrPartsList, r.DataVersion, listN()))
			default:
				root = append(root, r)
			}
		}
		if err := client.SaveSettingsAutoSubscribe(n); err != nil {
			t.Fatal(err)
		}
		started := make(chan error, 1)
		go func() { started <- n.Start(context.Background()) }()
		boot := sr.next(t)
		readRequestOf(t, boot)
		sr.reply(t, boot, im.InteractionModelProtocolID, im.OpcodeReportData, dataReport(t, 0, root))
		if err := <-started; err != nil {
			t.Fatal(err)
		}
		armed.Store(true)
		const id = 0x54
		sub := sr.next(t)
		subscribeRequestOf(t, sub)
		sr.reply(t, sub, im.InteractionModelProtocolID, im.OpcodeReportData, dataReport(t, id, light))
		<-entered // a bubble that deadlocks fails the test
		sr.reply(t, sub, im.InteractionModelProtocolID, im.OpcodeSubscribeResponse, subscribeResponse(t, id))
		waitUntil(t, "the subscription", n.SubscriptionActive)
		mu.Lock()
		early := slices.Clone(order)
		mu.Unlock()
		close(release)
		if len(early) != 0 {
			t.Fatalf("hooks %v ran while the priming report's EndpointInstalled held the queue", early)
		}
		// The alive the release lets run starts the label reconciliation:
		// the bubble settles before the registry's work is waited for (see
		// client.WaitRegistry).
		synctest.Wait()
		client.WaitRegistry(sr.reg)
		waitUntil(t, "the alive hook", func() bool {
			mu.Lock()
			defer mu.Unlock()
			return slices.Contains(order, "alive")
		})
		mu.Lock()
		got := slices.Clone(order)
		mu.Unlock()
		if !slices.Equal(got, []string{"endpoint 1", "alive"}) {
			t.Fatalf("hooks %v, want the endpoint, then one alive", got)
		}
		if err := n.Stop(context.Background()); err != nil {
			t.Fatal(err)
		}
	})
}

// TestOneAlivePerSubscriptionGoingLive: a subscription going live emits
// exactly one SubscriptionAlive. matter.js gates a report's alive on the
// subscription id (NetworkClient.ts:179-191 at b25ed960) and assigns the id
// only after the priming report's updated returned (ClientInteraction.ts:1083
// before :1086, SustainedSubscription.ts:224 before :226 at db021da0), so
// the priming report emits none and the going-live emission
// (NetworkClient.ts:226-236) is the one. Deciding the priming report's alive
// when its job ran, after the SubscribeResponse had set the id, emitted two
// in 98.8 % of runs (review II, phase 5a); the loop makes one run that lets
// the priming alive through enough.
func TestOneAlivePerSubscriptionGoingLive(t *testing.T) {
	t.Parallel()
	const runs = 40
	for i := range runs {
		synctest.Test(t, func(t *testing.T) {
			var alives atomic.Int32
			var live atomic.Bool
			sr := newScriptedRegistry(t, seedCommissioned(3), func(c *client.RegistryConfig) {
				c.HooksFor = func(string) client.Hooks {
					return client.Hooks{
						SubscriptionStatusChanged: func(active bool) { live.Store(active) },
						SubscriptionAlive:         func() { alives.Add(1) },
					}
				}
			})
			n := sr.reg.Get("peer1")
			sr.subscribe(t, n, sr.imageWithOpCreds("ours"), 0x55)
			waitGoneLive(t, &live)
			client.WaitRegistry(sr.reg)
			if got := alives.Load(); got != 1 {
				t.Fatalf("run %d: %d SubscriptionAlive hooks as the subscription went live, want 1", i, got)
			}
			if err := n.Stop(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
