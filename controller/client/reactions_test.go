// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"bytes"
	"context"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"thingfabric.dev/go-fabric/client/exchange"
	basicinfo "thingfabric.dev/go-fabric/cluster/spec/basicinformation"
	"thingfabric.dev/go-fabric/controller/client"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/secure/channel"
	"thingfabric.dev/go-fabric/tlv"
	"thingfabric.dev/go-fabric/transport/message"
)

// msgLog records log messages.
type msgLog struct {
	mu   sync.Mutex
	msgs []string
}

func (h *msgLog) Enabled(context.Context, slog.Level) bool { return true }
func (h *msgLog) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h *msgLog) WithGroup(string) slog.Handler            { return h }
func (h *msgLog) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	h.msgs = append(h.msgs, r.Message)
	h.mu.Unlock()
	return nil
}

func (h *msgLog) has(msg string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Contains(h.msgs, msg)
}

// pushEvents delivers events over the subscription id, on the scripted
// session.
func (sr *scriptedRegistry) pushEvents(t *testing.T, id uint32, events ...im.EventReport) {
	t.Helper()
	sr.send(t, message.ProtocolHeader{Initiator: true, NeedsAck: true, Opcode: im.OpcodeReportData, ExchangeID: uint16(0x7000 + sr.counter), ProtocolID: im.InteractionModelProtocolID},
		dataReport(t, id, nil, events...))
}

func eventReport(event, number uint64) im.EventReport {
	return im.EventReport{
		Path:   im.ConcreteEventPath{Endpoint: 0, Cluster: basicinfo.ClusterID, Event: uint32(event), HasEndpoint: true, HasCluster: true, HasEvent: true},
		Number: number, Priority: im.EventPriorityCritical, Timestamp: 1,
		Data: im.AttributeValue{Value: tlvNode(func(e *tlv.Encoder, tag tlv.Tag) { e.StartStruct(tag); _ = e.EndContainer() })},
	}
}

// TestReactionsNeedSubscription: a leave, shutDown or startUp delivered
// without an active default subscription may be stale and is ignored
// (Peers.ts:869-881, :899-908, :929-938 at b25ed960). The events come from the
// newly-commissioned read, which emits them (NetworkClient.ts:304-309 at b25ed960)
// with no default subscription active; a host read would not emit them
// at all (W4-ctlim-01).
func TestReactionsNeedSubscription(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		logs := &msgLog{}
		sr := newScriptedRegistry(t, seedCommissioned(3), func(c *client.RegistryConfig) { c.Logger = slog.New(logs) })
		n := sr.reg.Get("peer1")
		client.SetNewlyCommissioned(n)
		read := make(chan error, 1)
		go func() { read <- n.Start(context.Background()) }() // no autoSubscribe: started, unsubscribed
		m := sr.next(t)
		reports := sr.imageWithOpCreds("ours")
		sr.reply(t, m, im.InteractionModelProtocolID, im.OpcodeReportData, dataReport(t, 0, reports,
			eventReport(uint64(basicinfo.EventShutDown), 1), eventReport(uint64(basicinfo.EventStartUp), 2), leaveEvent(3, 3)))
		if err := <-read; err != nil {
			t.Fatal(err)
		}
		client.WaitRegistry(sr.reg)
		for _, want := range []string{"leave_ignored", "shutdown_ignored", "startup_ignored"} {
			if !logs.has("matter.controller.client.registry." + want) {
				t.Errorf("no %s", want)
			}
		}
		if sr.reg.Size() != 1 || sr.session.IsPeerLost() {
			t.Fatal("an ignored reaction acted")
		}
	})
}

// TestReactionsOfReportBeforeSubscriptionHeldLate: a ShutDown, StartUp
// and Leave of the newly-commissioned read are ignored also when their
// reactions run only after the default subscription became active — the
// interleaving of issue #628. matter.js's #onShutdown and #onStartUp run
// inside the report's awaited emit and see the subscription inactive by
// construction; the Go reactions run on goroutines of their own, so each
// needs the report's live flag. The gate holds every reaction until the
// subscription is active.
func TestReactionsOfReportBeforeSubscriptionHeldLate(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		logs := &msgLog{}
		sr := newScriptedRegistry(t, seedCommissioned(3), func(c *client.RegistryConfig) { c.Logger = slog.New(logs) })
		n := sr.reg.Get("peer1")
		client.SetNewlyCommissioned(n)
		release := make(chan struct{})
		client.SetReactionGate(sr.reg, func(*client.Node, uint32) { <-release })
		if err := client.SaveSettingsAutoSubscribe(n); err != nil {
			t.Fatal(err)
		}
		started := make(chan error, 1)
		go func() { started <- n.Start(context.Background()) }()
		boot := sr.next(t)
		readRequestOf(t, boot)
		sr.reply(t, boot, im.InteractionModelProtocolID, im.OpcodeReportData, dataReport(t, 0, sr.imageWithOpCreds("ours"),
			eventReport(uint64(basicinfo.EventShutDown), 1), eventReport(uint64(basicinfo.EventStartUp), 2), leaveEvent(3, 3)))
		if err := <-started; err != nil {
			t.Fatal(err)
		}
		sub := sr.next(t)
		subscribeRequestOf(t, sub)
		sr.reply(t, sub, im.InteractionModelProtocolID, im.OpcodeReportData, dataReport(t, 33, nil))
		sr.reply(t, sub, im.InteractionModelProtocolID, im.OpcodeSubscribeResponse, subscribeResponse(t, 33))
		waitUntil(t, "the subscription", n.SubscriptionActive)
		close(release)
		synctest.Wait()
		client.WaitRegistry(sr.reg)
		for _, want := range []string{"leave_ignored", "shutdown_ignored", "startup_ignored"} {
			if !logs.has("matter.controller.client.registry." + want) {
				t.Errorf("no %s", want)
			}
		}
		if sr.reg.Size() != 1 || sr.session.IsPeerLost() || !n.SubscriptionActive() {
			t.Fatal("a reaction to an event of the read acted after the subscription became active")
		}
	})
}

// TestLeaveLatchNeedsLiveReport: a decommission's leave latch counts a
// Leave only when the subscription was active when its report arrived
// (CommissioningClient.ts:430-436 at b25ed960, whose listener runs inside
// the report's awaited emit) — a Leave of a report from before the
// subscription does not confirm the removal, although the subscription is
// active when the listener runs.
func TestLeaveLatchNeedsLiveReport(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		sr := newScriptedRegistry(t, seedCommissioned(3), nil)
		n := sr.reg.Get("peer1")
		sr.subscribe(t, n, sr.imageWithOpCreds("ours"), 33)
		if client.NoteLeaveOnLatch(sr.reg, n, 3, false) {
			t.Fatal("the latch counted a Leave of a report from before the subscription")
		}
		if !client.NoteLeaveOnLatch(sr.reg, n, 3, true) {
			t.Fatal("the latch missed a Leave of a live report")
		}
	})
}

// TestReactionLeave: Peers.#onLeave — a leave of a fabric index the peer's
// Fabrics does not list, or of another fabric's entry, is ignored; one of
// ours decommissions and deletes the node.
func TestReactionLeave(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		sr := newScriptedRegistry(t, seedCommissioned(3), nil)
		n := sr.reg.Get("peer1")
		reports := sr.imageWithOpCreds("ours")
		// A second entry of another fabric under index 4.
		reports[len(reports)-2].Value = im.AttributeValue{Value: tlvNode(func(e *tlv.Encoder, tag tlv.Tag) {
			e.StartArray(tag)
			for _, f := range []struct {
				root []byte
				idx  uint8
			}{{sr.fabric.RootPublicKey, 3}, {bytes.Repeat([]byte{4}, 65), 4}} {
				e.StartStruct(tlv.AnonymousTag())
				e.PutOctets(tlv.ContextTag(1), f.root)
				e.PutUint(tlv.ContextTag(2), 0xFFF1)
				e.PutUint(tlv.ContextTag(3), sr.fabric.FabricID)
				e.PutUint(tlv.ContextTag(4), 0x2222)
				e.PutUTF8(tlv.ContextTag(5), "ours")
				e.PutUint(tlv.ContextTag(0xFE), uint64(f.idx))
				_ = e.EndContainer()
			}
			_ = e.EndContainer()
		})}
		sr.subscribe(t, n, reports, 33)
		sr.pushEvents(t, 33, leaveEvent(10, 5), leaveEvent(11, 4))
		waitUntil(t, "the events", func() bool { sr.hooks.mu.Lock(); defer sr.hooks.mu.Unlock(); return len(sr.hooks.events) == 2 })
		// The going-live alive may still start the label reconciliation: the
		// bubble settles first (see client.WaitRegistry).
		synctest.Wait()
		client.WaitRegistry(sr.reg)
		if sr.reg.Size() != 1 || !n.IsCommissioned() {
			t.Fatal("a foreign leave deleted the node")
		}
		sr.pushEvents(t, 33, leaveEvent(12, 3))
		waitUntil(t, "the deletion", func() bool { return sr.reg.Size() == 0 })
		client.WaitRegistry(sr.reg)
		// The store is waited on, not read once (report AU: one -race run read
		// it before the erase was visible).
		waitUntil(t, "the erased store", func() bool { ids, _ := sr.store.NodeIDs(); return len(ids) == 0 })
		sr.hooks.mu.Lock()
		defer sr.hooks.mu.Unlock()
		if !slices.Equal(sr.hooks.decommissioned, []string{"peer1"}) || !sr.session.IsPeerLost() || sr.set.Get(scriptedAddr) != nil {
			t.Fatalf("after the leave: decommissioned %v, session lost %v", sr.hooks.decommissioned, sr.session.IsPeerLost())
		}
	})
}

// secondSession adds a secure session B with the same peer, adopted into
// the peer set, and the peer's half of it.
func (sr *scriptedRegistry) secondSession(t *testing.T) (b *exchange.Session, ch *channel.Session) {
	t.Helper()
	i2r, r2i := bytes.Repeat([]byte{0x33}, 16), bytes.Repeat([]byte{0x44}, 16)
	var err error
	b, err = sr.mgr.NewSecureSession(exchange.SecureSessionConfig{
		ID: 0x0102, Addr: peerAddr, FabricIndex: 1,
		Channel:        channel.Config{EncryptKey: i2r, DecryptKey: r2i, LocalNodeID: 0x1111, PeerNodeID: 0x2222, PeerSessionID: 0x7102},
		PeerParameters: exchange.PeerParameters(nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	ch, err = channel.New(channel.Config{EncryptKey: r2i, DecryptKey: i2r, LocalNodeID: 0x2222, PeerNodeID: 0x1111, PeerSessionID: 0x0102})
	if err != nil {
		t.Fatal(err)
	}
	sr.set.Adopt(b)
	return b, ch
}

// sendOn sends a message of the peer over session id with ch.
func (sr *scriptedRegistry) sendOn(t *testing.T, ch *channel.Session, id uint16, proto message.ProtocolHeader, payload []byte) {
	t.Helper()
	sr.counter++
	hdr := message.Header{SessionID: id, MessageCounter: 0x100 + sr.counter}
	sealed, err := ch.Encrypt(&hdr, hdr.SecurityFlags(), append(proto.Marshal(), payload...))
	if err != nil {
		t.Fatal(err)
	}
	sr.mgr.HandleDatagram(append(hdr.Marshal(), sealed.Ciphertext...), peerAddr)
}

// TestReactionStartUp: Peers.#onStartUp — the sessions registered before
// the peer's most recently active one are lost; that one and every session
// registered after it stay, whatever their creation times
// (Peers.ts:945-947, SessionManager.ts:722-735 at b25ed960). The port of
// ClientStartUpTest.ts's "closes the sessions registered before the
// current session when startUp fires" on the registry's scripted peer: the
// old session and the new one are created in the same instant, so a cut by
// creation time would keep both.
func TestReactionStartUp(t *testing.T) {
	t.Parallel()
	t.Run("closes the sessions registered before the current session when startUp fires", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			sr := newScriptedRegistry(t, seedCommissioned(3), nil)
			n := sr.reg.Get("peer1")
			sr.subscribe(t, n, sr.imageWithOpCreds("ours"), 33)
			b, ch := sr.secondSession(t)
			if !b.CreatedAt().Equal(sr.session.CreatedAt()) {
				t.Fatalf("created at %v and %v, want the same instant", sr.session.CreatedAt(), b.CreatedAt())
			}
			sr.clock.Advance(time.Second)
			// The rebooted device reports its StartUp over its new session,
			// which makes that one the most recently active.
			sr.sendOn(t, ch, 0x0102, message.ProtocolHeader{Initiator: true, NeedsAck: true, Opcode: im.OpcodeReportData, ExchangeID: 0x7101, ProtocolID: im.InteractionModelProtocolID},
				dataReport(t, 33, nil, eventReport(uint64(basicinfo.EventStartUp), 40)))
			waitUntil(t, "the old session's loss", sr.session.IsPeerLost)
			// The going-live alive may still start the label reconciliation: the
			// bubble settles first (see client.WaitRegistry).
			synctest.Wait()
			client.WaitRegistry(sr.reg)
			if b.IsPeerLost() || b.IsClosed() {
				t.Fatal("the newest session was lost")
			}
			if got := sr.mgr.SessionsFor(scriptedAddr); len(got) != 1 || got[0] != b {
				t.Fatalf("sessions %v", got)
			}
		})
	})
}

// TestReactionShutDown: Peers.#onShutdown — every session with the peer is
// lost (SessionManager.handlePeerShutdown, :624-631); the subscription
// stays registered (keepSubscriptions, :629).
func TestReactionShutDown(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		sr := newScriptedRegistry(t, seedCommissioned(3), nil)
		n := sr.reg.Get("peer1")
		sr.subscribe(t, n, sr.imageWithOpCreds("ours"), 33)
		b, _ := sr.secondSession(t)
		sr.pushEvents(t, 33, eventReport(uint64(basicinfo.EventShutDown), 41))
		waitUntil(t, "the sessions' loss", func() bool { return sr.session.IsPeerLost() && b.IsPeerLost() })
		// The going-live alive may still start the label reconciliation: the
		// bubble settles first (see client.WaitRegistry).
		synctest.Wait()
		client.WaitRegistry(sr.reg)
		if len(sr.subs.Active()) != 1 || sr.reg.Size() != 1 {
			t.Fatal("the shutdown closed the subscription or the node")
		}
	})
}
