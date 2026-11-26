// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Kill tests for the C4 survivors on the peer and inbound paths:
// case_initiator.go's connectPeer, subscription_reestablish.go's
// reestablishPeer, securechannel.go's caseSigma3Wait,
// devicetype_validation.go, group_messaging.go's dispatch gate and
// bdx_responder.go's protocol gate (phase 8 wave 2b, #602, decision 692).

package bridge

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/bdx"
	"thingfabric.dev/go-fabric/groups"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/im/subscription"
	"thingfabric.dev/go-fabric/mdns"
	"thingfabric.dev/go-fabric/transport/message"
	"thingfabric.dev/go-fabric/transport/mrp"
)

// countingProviderW2B2b is a CASE initiator provider that counts its calls
// and fails each.
type countingProviderW2B2b struct {
	mu    sync.Mutex
	calls int
}

func (p *countingProviderW2B2b) provide(context.Context, uint8, uint64) (*CaseInitiation, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	return nil, errors.New("no CASE today")
}

func (p *countingProviderW2B2b) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// resolverW2B2b resolves nothing.
type resolverW2B2b struct{}

func (resolverW2B2b) ResolveOperational(context.Context, [8]byte, uint64) ([]*net.UDPAddr, error) {
	return nil, nil
}

// withInitiatorW2B2b wires p and a resolver into b.
func withInitiatorW2B2b(b *Bridge, p *countingProviderW2B2b) {
	b.resumption.mu.Lock()
	b.resumption.caseInitiator = p.provide
	b.resumption.resolver = resolverW2B2b{}
	b.resumption.mu.Unlock()
}

// TestConnectPeerFailureIsNotAFabricUpdateW2B2b: a CASE that fails on its
// own is not reported as aborted by a fabric update. Kills
// case_initiator.go:128 (&& → ||).
func TestConnectPeerFailureIsNotAFabricUpdateW2B2b(t *testing.T) {
	t.Parallel()
	b := unstartedBridgeW2B2b(t)
	withInitiatorW2B2b(b, &countingProviderW2B2b{})
	_, err := b.connectPeer(context.Background(), 1, 0x10)
	if err == nil {
		t.Fatal("connectPeer succeeded with a failing provider")
	}
	if errors.Is(err, errFabricUpdated) {
		t.Fatalf("a failed CASE reported as a fabric update: %v", err)
	}
}

// TestReestablishSkipsABlockedPeerW2B2b: a peer already blocked is not
// contacted. Kills subscription_reestablish.go:164 (guard off).
func TestReestablishSkipsABlockedPeerW2B2b(t *testing.T) {
	t.Parallel()
	b := unstartedBridgeW2B2b(t)
	p := &countingProviderW2B2b{}
	withInitiatorW2B2b(b, p)
	k := peerKey{fabricIndex: 1, nodeID: 0x10}
	block := &peerBlockList{peers: map[peerKey]struct{}{k: {}}}
	rec := subscription.PeerSubscription{SubscriptionID: 1, FabricIndex: 1, PeerNodeID: 0x10, MaxIntervalCeiling: 60}
	if done := b.reestablishPeer(context.Background(), k, []subscription.PeerSubscription{rec}, block); len(done) != 0 {
		t.Fatalf("re-established %v for a blocked peer", done)
	}
	if n := p.count(); n != 0 {
		t.Fatalf("a blocked peer was contacted %d times", n)
	}
}

// blockingRegistryW2B2b lists one live session for every peer and blocks
// the peer while it does — a normal subscribe from the peer starting
// while its re-establishment connects.
type blockingRegistryW2B2b struct {
	fakeSessionRegistry
	block *peerBlockList
	k     peerKey
	sid   uint16
}

func (r *blockingRegistryW2B2b) SessionIDsForPeer(uint8, uint64) []uint16 {
	r.block.add(r.k)
	return []uint16{r.sid}
}

// refusingConnW2B2b records every send and refuses it, so a priming
// report sent on it fails at once instead of waiting for its answer.
type refusingConnW2B2b struct{ fakeConn }

func (c *refusingConnW2B2b) Send(msg []byte) error {
	_ = c.fakeConn.Send(msg)
	return errNoRoute
}

// TestReestablishStopsAtABlockMidwayW2B2b: a peer blocked once its
// re-establishment has connected gets none of its records tried: none is
// restored in the manager, not even for a moment, and nothing is sent.
// Kills subscription_reestablish.go:187 (guard off), which restores the
// record (and releases it when its priming report matches nothing).
func TestReestablishStopsAtABlockMidwayW2B2b(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	mgr := subscription.NewManager(subscription.Config{}, nil, nil)
	b.AttachSubscriptionManager(mgr)
	var mu sync.Mutex
	var closed []uint32
	mgr.SetOnSubscriptionClosed(func(id uint32) {
		mu.Lock()
		defer mu.Unlock()
		closed = append(closed, id)
	})
	attachPASETestPeer(t, b)
	k := peerKey{fabricIndex: 1, nodeID: 0x10}
	block := &peerBlockList{peers: map[peerKey]struct{}{}}
	b.AttachSessionRegistry(&blockingRegistryW2B2b{block: block, k: k, sid: paseTestSessionID})
	c := &refusingConnW2B2b{}
	b.sessionPeerAddrs.Store(paseTestSessionID, net.Addr(&connRoute{conn: c}))

	rec := subscription.PeerSubscription{
		SubscriptionID: 1, FabricIndex: 1, PeerNodeID: 0x10, MaxIntervalCeiling: 60, MaxInterval: 60,
		AttributeRequests: []im.ConcreteAttributePath{{HasEndpoint: true, Endpoint: 0}},
	}
	if done := b.reestablishPeer(context.Background(), k, []subscription.PeerSubscription{rec}, block); len(done) != 0 {
		t.Fatalf("re-established %v for a peer blocked midway", done)
	}
	if !block.has(k) {
		t.Fatal("the registry fake did not block the peer: the test exercises nothing")
	}
	mu.Lock()
	defer mu.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(closed) != 0 || len(c.sent) != 0 || mgr.Active() != 0 {
		t.Fatalf("a record of a peer blocked midway was tried: closed %v, %d datagrams sent, %d active", closed, len(c.sent), mgr.Active())
	}
}

// TestSigma3WaitTreatsZeroActiveAsDefaultW2B2b: an unset (zero) active
// interval waits as the default one. Kills securechannel.go:2564 (<= → <).
func TestSigma3WaitTreatsZeroActiveAsDefaultW2B2b(t *testing.T) {
	t.Parallel()
	unset := unstartedBridgeW2B2b(t)
	set, err := New(wbEmptySnapshotter, mdns.NewNoop(), Config{
		Listen:           "127.0.0.1:0",
		SessionIntervals: SessionIntervals{Active: mrp.SessionActiveIntervalDefault},
	}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got, want := unset.caseSigma3Wait(false), set.caseSigma3Wait(false); got != want {
		t.Fatalf("Sigma3 wait with Active unset = %v, with the default stated = %v", got, want)
	}
}

// TestDeviceTypeValidationStatesTheRadioW2B2b: a bridge without a GATT
// peripheral states that the node does not commission over BLE, whatever
// the topology said. Kills devicetype_validation.go:72 and :73 (!= → ==).
func TestDeviceTypeValidationStatesTheRadioW2B2b(t *testing.T) {
	t.Parallel()
	b := unstartedBridgeW2B2b(t)
	topo := assembleW2B2b(t, notifierSourceW2B2b{registrationsW2B2b: &registrationsW2B2b{}, servers: nil})
	topo.CommissionsOverBLE = true
	_ = b.validateDeviceTypes(topo)
	if topo.CommissionsOverBLE {
		t.Fatal("a bridge without a radio left the topology commissioning over BLE")
	}
}

// groupPortW2B2b is a group-messaging port whose Decode hands out msg.
type groupPortW2B2b struct{ msg *groups.Message }

func (p groupPortW2B2b) Decode(context.Context, []byte) (*groups.Message, error) { return p.msg, nil }

func (groupPortW2B2b) Memberships(context.Context) ([]groups.Membership, error) {
	return nil, nil
}
func (groupPortW2B2b) OnMembershipChanged(func())                  {}
func (groupPortW2B2b) OnGroupTableChanged(func(uint8))             {}
func (groupPortW2B2b) ForgetFabric(uint8)                          {}
func (groupPortW2B2b) ReportGroupMessage(groups.GroupMessageEvent) {}

// TestGroupMessageFromANonInitiatorIsDroppedW2B2b: a group message whose
// protocol header does not mark the initiator is dropped before its body
// is read. Kills group_messaging.go:319 (guard off), which reads the
// undecodable body and fails.
func TestGroupMessageFromANonInitiatorIsDroppedW2B2b(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	proto := message.ProtocolHeader{ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeInvokeRequest, ExchangeID: 1}
	payload := append(proto.Marshal(), 0xFF) // not TLV
	b.AttachGroupMessaging(groupPortW2B2b{msg: &groups.Message{Payload: payload, FabricIndex: 1, GroupID: 0x0101}})
	if err := b.dispatchGroupMessage(context.Background(), []byte{0}, nil); err != nil {
		t.Fatalf("a non-initiator's group message was read: %v", err)
	}
}

// startedResponderW2B2b is a BDX responder that counts the exchanges it
// was handed.
type startedResponderW2B2b struct {
	mu sync.Mutex
	n  int
}

func (r *startedResponderW2B2b) HandleExchange(context.Context, bdx.Peer, bdx.Message, bdx.Exchange) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.n++
}

// TestBDXGateLeavesOtherProtocolsW2B2b: a message of another protocol
// that opens an exchange is left to the ordinary dispatch; a BDX message
// that states the standard vendor id 0 opens a BDX exchange. Kills
// bdx_responder.go:225 (&& → ||: the vendor clause).
func TestBDXGateLeavesOtherProtocolsW2B2b(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	attachPASETestPeer(t, b)
	r := &startedResponderW2B2b{}
	b.AttachBDXResponder(r)
	hdr := &message.Header{SessionID: paseTestSessionID, MessageCounter: 5}
	proto := message.ProtocolHeader{ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeReadRequest, ExchangeID: 0x5300, Initiator: true}
	if b.deliverRespondedBDX(context.Background(), hdr, proto, []byte{0x15, 0x18}) {
		t.Fatal("an Interaction Model message was taken by the BDX responder")
	}
	// A BDX message naming the standard vendor explicitly is BDX.
	proto = message.ProtocolHeader{ProtocolID: bdx.ProtocolID, HasVendorID: true, VendorID: 0, Opcode: 0x01, ExchangeID: 0x5301, Initiator: true}
	if !b.deliverRespondedBDX(context.Background(), hdr, proto, []byte{0x15, 0x18}) {
		t.Fatal("a BDX message with vendor id 0 stated was not taken by the BDX responder")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.n != 1 {
		t.Fatalf("the responder was handed %d exchanges, want 1", r.n)
	}
}
