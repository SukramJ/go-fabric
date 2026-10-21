// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"context"
	"errors"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/controller/discovery"
	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/controller/handshake"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/controller/peer"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
)

// The scripted registry: a registry over the scripted manager whose one
// secure session is the peer {1, 0x2222}'s, adopted into the peer set, so
// a node restored with that peer address talks over it.

const (
	opCreds        uint32 = 0x003E
	opCredsFabrics uint32 = 0x0001
	opCredsCurrent uint32 = 0x0005
	cmdNOCResponse uint32 = 0x08
	cmdUpdateLabel uint32 = 0x09
	cmdRemove      uint32 = 0x0A
)

var scriptedAddr = handshake.PeerAddress{FabricIndex: 1, NodeID: 0x2222}

type scriptedRegistry struct {
	*scripted
	set    *peer.Set
	com    *commissioner.Commissioner
	fabric *commissioner.Fabric
	subs   *interaction.Subscriptions
	store  *client.MemoryStore
	reg    *client.Registry
	peer   *peer.Peer
	hooks  *regHooks
}

// regHooks records what the registry's nodes report to the host.
type regHooks struct {
	mu             sync.Mutex
	decommissioned []string
	events         []uint32
	alive          int
}

func (h *regHooks) hooksFor(id string) client.Hooks {
	return client.Hooks{
		Decommissioned: func() {
			h.mu.Lock()
			h.decommissioned = append(h.decommissioned, id)
			h.mu.Unlock()
		},
		Event: func(_ uint16, _, event uint32, _ interaction.EventData, _ any) {
			h.mu.Lock()
			h.events = append(h.events, event)
			h.mu.Unlock()
		},
		SubscriptionAlive: func() {
			h.mu.Lock()
			h.alive++
			h.mu.Unlock()
		},
	}
}

func newScriptedRegistry(t *testing.T, seed func(st *client.MemoryStore), mutate func(*client.RegistryConfig)) *scriptedRegistry {
	t.Helper()
	sr := &scriptedRegistry{scripted: newScripted(t), store: &client.MemoryStore{}, hooks: &regHooks{}}
	names := discovery.NewNames(&fakeMDNS{out: make(chan mdns.QueryResponse)}, discardLog())
	authority, err := ca.New(ca.Config{Store: &ca.MemoryStore{}})
	if err != nil {
		t.Fatal(err)
	}
	sr.fabric, err = commissioner.NewFabric(authority, commissioner.FabricConfig{FabricID: 0x0F0D, RootNodeID: 0x1111, VendorID: 0xFFF1, Label: "ours"})
	if err != nil {
		t.Fatal(err)
	}
	sr.set, err = peer.NewSet(peer.Context{Exchanges: sr.mgr, Names: names, Fabrics: sr.fabric.Lookup, Logger: discardLog()})
	if err != nil {
		t.Fatal(err)
	}
	sr.peer = sr.set.Adopt(sr.session)
	sr.subs = interaction.NewSubscriptions(interaction.SubscriptionsConfig{Clock: sr.clock})
	sr.com, err = commissioner.New(commissioner.Context{Exchanges: sr.mgr, Peers: sr.set, Subscriptions: sr.subs, Logger: discardLog(), Clock: sr.clock})
	if err != nil {
		t.Fatal(err)
	}
	if seed != nil {
		seed(sr.store)
	}
	cfg := client.RegistryConfig{
		Store: sr.store, Commissioner: sr.com, Peers: sr.set, Exchanges: sr.mgr, Fabrics: []*commissioner.Fabric{sr.fabric},
		Subscriptions: sr.subs, Logger: discardLog(), Clock: sr.clock, HooksFor: sr.hooks.hooksFor,
		Node: client.Config{Clock: sr.clock, Rand: func() float64 { return 0 }},
	}
	if mutate != nil {
		mutate(&cfg)
	}
	sr.reg, err = client.NewRegistry(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = sr.reg.Close(ctx)
		_ = sr.subs.Close(ctx)
		sr.com.Close()
		sr.set.Close(ctx)
		_ = names.Close()
	})
	return sr
}

// seedCommissioned stores peer1 as the node {1, 0x2222} with
// fabricIndexOnPeer (0: unknown).
func seedCommissioned(fabricIndexOnPeer uint8) func(st *client.MemoryStore) {
	return func(st *client.MemoryStore) {
		a := scriptedAddr
		_ = client.SaveRecord(st, "peer1", client.Record{
			PeerAddress: &a, FabricIndexOnPeer: fabricIndexOnPeer,
			Addresses:    []client.NetworkAddress{{IP: netip.MustParseAddr("fd00::1"), Port: 5540}},
			DiscoveredAt: time.UnixMilli(1_800_000_000_000),
		})
	}
}

func uintNode(v uint64) func(e *tlv.Encoder, tag tlv.Tag) {
	return func(e *tlv.Encoder, tag tlv.Tag) { e.PutUint(tag, v) }
}

// fabricsNode is an OperationalCredentials Fabrics list of one entry.
func fabricsNode(root []byte, fabricID uint64, label string, index uint8) func(e *tlv.Encoder, tag tlv.Tag) {
	return func(e *tlv.Encoder, tag tlv.Tag) {
		e.StartArray(tag)
		e.StartStruct(tlv.AnonymousTag())
		e.PutOctets(tlv.ContextTag(1), root)
		e.PutUint(tlv.ContextTag(2), 0xFFF1)
		e.PutUint(tlv.ContextTag(3), fabricID)
		e.PutUint(tlv.ContextTag(4), 0x2222)
		e.PutUTF8(tlv.ContextTag(5), label)
		e.PutUint(tlv.ContextTag(0xFE), uint64(index))
		_ = e.EndContainer()
		_ = e.EndContainer()
	}
}

// opCredsImage is the root's OperationalCredentials for the image: our
// fabric entry under the device-side index 3, labelled label.
func (sr *scriptedRegistry) opCredsImage(label string) []im.AttributeReport {
	return []im.AttributeReport{
		value(0, opCreds, opCredsFabrics, 7, tlvNode(fabricsNode(sr.fabric.RootPublicKey, sr.fabric.FabricID, label, 3))),
		value(0, opCreds, opCredsCurrent, 7, tlvNode(uintNode(3))),
	}
}

// imageWithOpCreds is image() with OperationalCredentials on the root.
func (sr *scriptedRegistry) imageWithOpCreds(label string) []im.AttributeReport {
	reports := image(5)
	for i := range reports {
		if reports[i].Path.Endpoint == 0 && reports[i].Path.Attribute == 1 && reports[i].Path.Cluster == 0x1D {
			reports[i].Value = im.AttributeValue{Value: listN(0x1D, uint64(basicinfo.ClusterID), uint64(opCreds))}
		}
	}
	return append(reports, sr.opCredsImage(label)...)
}

// subscribe starts the node and establishes its default subscription
// over the script: the bootstrap read answered with report, the
// subscription with id.
func (sr *scriptedRegistry) subscribe(t *testing.T, n *client.Node, report []im.AttributeReport, id uint32) {
	t.Helper()
	if err := client.SaveSettingsAutoSubscribe(n); err != nil {
		t.Fatal(err)
	}
	started := make(chan error, 1)
	go func() { started <- n.Start(context.Background()) }()
	boot := sr.next(t)
	readRequestOf(t, boot)
	sr.reply(t, boot, im.InteractionModelProtocolID, im.OpcodeReportData, dataReport(t, 0, report))
	if err := <-started; err != nil {
		t.Fatal(err)
	}
	sub := sr.next(t)
	subscribeRequestOf(t, sub)
	sr.reply(t, sub, im.InteractionModelProtocolID, im.OpcodeReportData, dataReport(t, id, nil))
	sr.reply(t, sub, im.InteractionModelProtocolID, im.OpcodeSubscribeResponse, subscribeResponse(t, id))
	waitUntil(t, "the subscription", n.SubscriptionActive)
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(scriptedHangBound)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// invokeOf decodes an InvokeRequest's single OperationalCredentials
// command: its id and fields.
func invokeOf(t *testing.T, m msg) (uint32, spec.Node) {
	t.Helper()
	if m.proto.Opcode != im.OpcodeInvokeRequest {
		t.Fatalf("opcode 0x%02X, want an InvokeRequest", m.proto.Opcode)
	}
	req, err := im.UnmarshalInvokeRequestTLV(tlv.NewDecoder(m.payload), func(_ im.ConcreteCommandPath, dec *tlv.Decoder, el tlv.Element) (any, error) {
		return spec.ReadContainer(dec, el)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Invokes) != 1 || req.Invokes[0].Path.Cluster != opCreds {
		t.Fatalf("invoke %+v", req)
	}
	fields, _ := req.Invokes[0].Fields.(spec.Node)
	return req.Invokes[0].Path.Command, fields
}

// answerCurrentFabricIndex answers the decommission's read of
// CurrentFabricIndex.
func (sr *scriptedRegistry) answerCurrentFabricIndex(t *testing.T, index uint64) {
	t.Helper()
	read := sr.next(t)
	rr := readRequestOf(t, read)
	if len(rr.AttributeRequests) != 1 || rr.AttributeRequests[0].Cluster != opCreds || rr.AttributeRequests[0].Attribute != opCredsCurrent || !rr.FabricFiltered {
		t.Fatalf("read %+v", rr)
	}
	sr.reply(t, read, im.InteractionModelProtocolID, im.OpcodeReportData, dataReport(t, 0, []im.AttributeReport{value(0, opCreds, opCredsCurrent, 1, tlvNode(uintNode(index)))}))
}

func fieldOf(fields spec.Node, tag uint8) (tlv.Element, bool) {
	for _, c := range fields.Children {
		if c.El.Tag == tlv.ContextTag(tag) {
			return c.El, true
		}
	}
	return tlv.Element{}, false
}

func hasUintField(fields spec.Node, tag uint8, want uint64) bool {
	el, ok := fieldOf(fields, tag)
	return ok && el.Uint == want
}

func leaveEvent(number uint64, fabricIndex uint8) im.EventReport {
	return im.EventReport{
		Path:   im.ConcreteEventPath{Endpoint: 0, Cluster: basicinfo.ClusterID, Event: basicinfo.EventLeave, HasEndpoint: true, HasCluster: true, HasEvent: true},
		Number: number, Priority: im.EventPriorityInfo, Timestamp: 1,
		Data: im.AttributeValue{Value: tlvNode(func(e *tlv.Encoder, tag tlv.Tag) { basicinfo.LeaveEvent{FabricIndex: fabricIndex}.EncodeTLV(e, tag) })},
	}
}

// nocResponse answers an invoke with NocResponse {status}.
func (sr *scriptedRegistry) nocResponse(t *testing.T, to msg, status uint8) {
	t.Helper()
	sr.reply(t, to, im.InteractionModelProtocolID, im.OpcodeInvokeResponse, encode(t, func(e *tlv.Encoder) {
		im.InvokeResponse{Responses: []im.InvokeResponseEntry{{
			Path: im.ConcreteCommandPath{Endpoint: 0, Cluster: opCreds, Command: cmdNOCResponse},
			Response: tlvNode(func(e *tlv.Encoder, tag tlv.Tag) {
				e.StartStruct(tag)
				e.PutUint(tlv.ContextTag(0), uint64(status))
				_ = e.EndContainer()
			}),
			HasResponse: true,
		}}}.MarshalTLV(e, func(e *tlv.Encoder, tag tlv.Tag, v any) { interaction.Raw(v.(spec.Node)).EncodeTLV(e, tag) })
	}))
}

// TestRegistryDecommission: ClientNode.decommission over
// CommissioningClient.decommission — the CurrentFabricIndex read when the
// index is unknown (CommissioningClient.ts:349-371), RemoveFabric with the
// peer-side index (:375, :399), the record cleared and the node deleted
// (ClientNode.ts:176-186), lifecycle.decommissioned.
func TestRegistryDecommission(t *testing.T) {
	t.Parallel()
	sr := newScriptedRegistry(t, seedCommissioned(0), nil)
	n := sr.reg.Get("peer1")
	if n == nil || !n.IsCommissioned() {
		t.Fatal("peer1 not restored commissioned")
	}
	done := make(chan error, 1)
	go func() { done <- sr.reg.Decommission(context.Background(), n) }()
	read := sr.next(t)
	rr := readRequestOf(t, read)
	if len(rr.AttributeRequests) != 1 || rr.AttributeRequests[0].Cluster != opCreds || rr.AttributeRequests[0].Attribute != opCredsCurrent || !rr.FabricFiltered {
		t.Fatalf("read %+v", rr)
	}
	// A second fabric operation meanwhile is refused (Peers.ts:596-601).
	if err := sr.reg.Commission(context.Background(), n, client.CommissionOptions{}); !errors.Is(err, client.ErrFabricOperationInProgress) {
		t.Fatalf("a concurrent commission: %v", err)
	}
	sr.reply(t, read, im.InteractionModelProtocolID, im.OpcodeReportData, dataReport(t, 0, []im.AttributeReport{value(0, opCreds, opCredsCurrent, 1, tlvNode(uintNode(3)))}))
	inv := sr.next(t)
	cmd, fields := invokeOf(t, inv)
	if cmd != cmdRemove || !hasUintField(fields, 0, 3) {
		t.Fatalf("command %d fields %+v", cmd, fields)
	}
	sr.nocResponse(t, inv, 0)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if sr.reg.Size() != 0 || sr.set.Get(scriptedAddr) != nil {
		t.Fatal("the node or its peer outlived the decommission")
	}
	if ids, _ := sr.store.NodeIDs(); len(ids) != 0 {
		t.Fatalf("stored %v", ids)
	}
	sr.hooks.mu.Lock()
	defer sr.hooks.mu.Unlock()
	if len(sr.hooks.decommissioned) != 1 || sr.hooks.decommissioned[0] != "peer1" {
		t.Fatalf("decommissioned hooks %v", sr.hooks.decommissioned)
	}
}

// TestRegistryDecommissionRefused: a non-Ok status is an error a leave
// cannot override (CommissioningClient.ts:396-404, :418-420); the node
// stays commissioned and kept.
func TestRegistryDecommissionRefused(t *testing.T) {
	t.Parallel()
	sr := newScriptedRegistry(t, seedCommissioned(3), nil)
	n := sr.reg.Get("peer1")
	done := make(chan error, 1)
	go func() { done <- sr.reg.Decommission(context.Background(), n) }()
	// The image has no OperationalCredentials: the index is read although
	// the record knows it (CommissioningClient.ts:353-357).
	sr.answerCurrentFabricIndex(t, 3)
	inv := sr.next(t)
	if cmd, fields := invokeOf(t, inv); cmd != cmdRemove || !hasUintField(fields, 0, 3) {
		t.Fatalf("command %d", cmd)
	}
	sr.nocResponse(t, inv, 11)
	if err := <-done; err == nil {
		t.Fatal("a refused removal succeeded")
	}
	if !n.IsCommissioned() || sr.reg.Size() != 1 || sr.set.Get(scriptedAddr) == nil {
		t.Fatal("a refused removal changed the node")
	}
}

// TestRegistryDecommissionConfirmed: an invoke that fails is a confirmed
// removal when the leave event arrived meanwhile or the session was lost
// to the removal (CommissioningClient.ts:405-417).
func TestRegistryDecommissionConfirmed(t *testing.T) {
	t.Parallel()
	t.Run("leave event", func(t *testing.T) {
		t.Parallel()
		sr := newScriptedRegistry(t, seedCommissioned(3), nil)
		n := sr.reg.Get("peer1")
		sr.subscribe(t, n, sr.imageWithOpCreds("ours"), 33)
		done := make(chan error, 1)
		go func() { done <- sr.reg.Decommission(context.Background(), n) }()
		inv := sr.next(t)
		// The leave of the device-side index arrives over the subscription,
		// then the command fails.
		sr.send(t, message.ProtocolHeader{Initiator: true, NeedsAck: true, Opcode: im.OpcodeReportData, ExchangeID: 0x7777, ProtocolID: im.InteractionModelProtocolID},
			dataReport(t, 33, nil, leaveEvent(20, 3)))
		waitUntil(t, "the leave event", func() bool { sr.hooks.mu.Lock(); defer sr.hooks.mu.Unlock(); return len(sr.hooks.events) > 0 })
		sr.reply(t, inv, im.InteractionModelProtocolID, im.OpcodeStatusResponse, encode(t, func(e *tlv.Encoder) { im.StatusResponse{Status: im.StatusFailure}.MarshalTLV(e) }))
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if sr.reg.Size() != 0 {
			t.Fatal("the node outlived a confirmed removal")
		}
	})
	t.Run("session closed", func(t *testing.T) {
		t.Parallel()
		sr := newScriptedRegistry(t, seedCommissioned(3), nil)
		n := sr.reg.Get("peer1")
		done := make(chan error, 1)
		go func() { done <- sr.reg.Decommission(context.Background(), n) }()
		sr.answerCurrentFabricIndex(t, 3)
		sr.next(t)
		sr.session.HandlePeerLoss(&exchange.PeerInitiatedCloseError{})
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if sr.reg.Size() != 0 {
			t.Fatal("the node outlived a confirmed removal")
		}
	})
	t.Run("unconfirmed", func(t *testing.T) {
		t.Parallel()
		sr := newScriptedRegistry(t, seedCommissioned(3), nil)
		n := sr.reg.Get("peer1")
		done := make(chan error, 1)
		go func() { done <- sr.reg.Decommission(context.Background(), n) }()
		sr.answerCurrentFabricIndex(t, 3)
		inv := sr.next(t)
		sr.reply(t, inv, im.InteractionModelProtocolID, im.OpcodeStatusResponse, encode(t, func(e *tlv.Encoder) { im.StatusResponse{Status: im.StatusFailure}.MarshalTLV(e) }))
		if err := <-done; err == nil || !n.IsCommissioned() {
			t.Fatalf("an unconfirmed removal: %v", err)
		}
	})
}

// TestRegistryCommissionGuards: CommissioningClient.commission's checks —
// already commissioned (:173-175), a fabric we do not control (:214-218),
// not located (:220-223) — and a commissioner error that leaves the node
// as it was.
func TestRegistryCommissionGuards(t *testing.T) {
	t.Parallel()
	sr := newScriptedRegistry(t, func(st *client.MemoryStore) {
		seedCommissioned(3)(st)
		_ = client.SaveRecord(st, "peer2", client.Record{DeviceIdentifier: "CAFE"})
		_ = client.SaveRecord(st, "peer3", client.Record{DeviceIdentifier: "BEEF", Addresses: []client.NetworkAddress{{IP: netip.MustParseAddr("192.0.2.1"), Port: 5540}}})
	}, nil)
	ctx := context.Background()
	if err := sr.reg.Commission(ctx, sr.reg.Get("peer1"), client.CommissionOptions{}); !errors.Is(err, client.ErrAlreadyCommissioned) {
		t.Fatalf("already commissioned: %v", err)
	}
	if err := sr.reg.Commission(ctx, sr.reg.Get("peer2"), client.CommissionOptions{}); !errors.Is(err, client.ErrNotLocated) {
		t.Fatalf("not located: %v", err)
	}
	other, _ := commissioner.NewFabric(sr.fabric.CA, commissioner.FabricConfig{FabricID: 9, RootNodeID: 5, FabricIndex: 2})
	if err := sr.reg.Commission(ctx, sr.reg.Get("peer2"), client.CommissionOptions{Options: commissioner.Options{Fabric: other}}); !errors.Is(err, client.ErrForeignFabric) {
		t.Fatalf("foreign fabric: %v", err)
	}
	// The commissioner fails (no PASE peer answers): the node keeps its
	// record.
	n := sr.reg.Get("peer3")
	cctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	if err := sr.reg.Commission(cctx, n, client.CommissionOptions{Options: commissioner.Options{Passcode: 20202021, Timeout: 100 * time.Millisecond}}); err == nil {
		t.Fatal("commissioning nothing succeeded")
	}
	if n.IsCommissioned() || len(n.Record().Addresses) != 1 {
		t.Fatalf("a failed commission changed the record %+v", n.Record())
	}
	// A held operation refuses another; a closed node refuses any.
	release, err := client.HoldOperation(sr.reg, n, "commission")
	if err != nil {
		t.Fatal(err)
	}
	if err := sr.reg.Decommission(ctx, n); !errors.Is(err, client.ErrFabricOperationInProgress) {
		t.Fatalf("held: %v", err)
	}
	release()
	_ = n.Close(ctx)
	if err := sr.reg.Decommission(ctx, n); !errors.Is(err, client.ErrClosed) {
		t.Fatalf("closed: %v", err)
	}
	// Decommissioning an uncommissioned node deletes it (ClientNode.ts:178-184).
	if err := sr.reg.Decommission(ctx, sr.reg.Get("peer2")); err != nil || sr.reg.Get("peer2") != nil {
		t.Fatalf("an uncommissioned node: %v", err)
	}
}
