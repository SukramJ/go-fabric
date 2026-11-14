// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"bytes"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
)

// identityTestLookup resolves CASE sessions with a fabric and a subject.
type identityTestLookup struct {
	sess    map[uint16]*channel.Session
	fabric  map[uint16]uint8
	subject map[uint16]uint64
}

func (l identityTestLookup) Lookup(id uint16) (*channel.Session, bool) {
	s, ok := l.sess[id]
	return s, ok
}

func (l identityTestLookup) FabricFor(id uint16) (uint8, bool) {
	f, ok := l.fabric[id]
	return f, ok
}

func (l identityTestLookup) SubjectFor(id uint16) (nodeID uint64, cats []uint32, ok bool) {
	n, ok := l.subject[id]
	return n, nil, ok
}

// identityTestPair is a CASE session pair between the bridge and peer.
func identityTestPair(t *testing.T, bridgeNode, peerNode uint64, k1, k2 byte) (bridgeSide, peerSide *channel.Session) {
	t.Helper()
	bk, pk := bytes.Repeat([]byte{k1}, 16), bytes.Repeat([]byte{k2}, 16)
	bs, err := channel.New(channel.Config{EncryptKey: bk, DecryptKey: pk, LocalNodeID: bridgeNode, PeerNodeID: peerNode, InitialCounter: 100})
	if err != nil {
		t.Fatal(err)
	}
	ps, err := channel.New(channel.Config{EncryptKey: pk, DecryptKey: bk, LocalNodeID: peerNode, PeerNodeID: bridgeNode, InitialCounter: 200})
	if err != nil {
		t.Fatal(err)
	}
	return bs, ps
}

// TestSubscribeAttributesToTheSessionPeerNotTheHeader (review II,
// R2-P1-req2-02): a SubscribeRequest with KeepSubscriptions=false on
// session A whose message header claims session B's peer as its source
// node id tears down A's subscriptions only. The header's source node id
// is chosen by the sender and not bound to the session; matter.js matches
// on the session's peer address (InteractionServer.ts:647, :651 at
// db021da0).
func TestSubscribeAttributesToTheSessionPeerNotTheHeader(t *testing.T) {
	t.Parallel()
	const (
		bridgeNode, nodeA, nodeB uint64 = 0x1111, 0xAAAA, 0xBBBB
		sessA, sessB             uint16 = 10, 11
	)
	b := newStartedBridgeWithSnapshotter(t, manyTempSensorsSnapshotterForTest())
	b.chunkStatusResponseTimeoutOverride = time.Minute
	bA, pA := identityTestPair(t, bridgeNode, nodeA, 0x10, 0x20)
	bB, _ := identityTestPair(t, bridgeNode, nodeB, 0x30, 0x40)
	b.AttachSessionLookup(identityTestLookup{
		sess:    map[uint16]*channel.Session{sessA: bA, sessB: bB},
		fabric:  map[uint16]uint8{sessA: 1, sessB: 1},
		subject: map[uint16]uint64{sessA: nodeA, sessB: nodeB},
	})
	mgr := subscription.NewManager(subscription.Config{}, nil, nil)
	b.AttachSubscriptionManager(mgr)
	path := []im.ConcreteAttributePath{{HasEndpoint: true, Endpoint: 0, HasCluster: true, Cluster: 0x001D, HasAttribute: true, Attribute: 0}}
	victim, err := mgr.Subscribe(subscription.SubscribeArgs{FabricIndex: 1, PeerNodeID: nodeB, SessionID: sessB, MaxIntervalCeiling: 60, AttributePaths: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.SessionClosed(sessA) }) // ends the attacker's priming wait

	conn, _ := newSubscribeTestPeer(t)
	bridgeAddr, err := net.ResolveUDPAddr("udp", b.LocalAddr())
	if err != nil {
		t.Fatal(err)
	}
	bridgeAddr.IP = net.IPv4(127, 0, 0, 1)
	enc := tlv.NewEncoder()
	im.SubscribeRequest{KeepSubscriptions: false, MaxIntervalCeiling: 60, AttributeRequests: path}.MarshalTLV(enc)
	payload, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	proto := message.ProtocolHeader{ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeSubscribeRequest, ExchangeID: 77, Initiator: true, NeedsAck: true}
	hdr := message.Header{SessionID: sessA, HasSourceNodeID: true, SourceNodeID: nodeB}
	sealed, err := pA.Encrypt(&hdr, securityFlagsByte(&hdr), append(proto.Marshal(), payload...))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.WriteToUDP(append(hdr.Marshal(), sealed.Ciphertext...), bridgeAddr); err != nil {
		t.Fatal(err)
	}
	// The teardown runs before the priming report: the attacker's first
	// reply means it is done.
	if _, _, ok := readOneIMDatagram(t, conn, pA, 3*time.Second); !ok {
		t.Fatal("no reply to the SubscribeRequest reached session A's peer")
	}
	if _, err := mgr.Get(victim.ID); err != nil {
		t.Fatalf("session B's subscription is gone after a request on session A named B's node id in its header: %v", err)
	}
	for _, in := range mgr.Snapshot() {
		if in.SessionID == sessA && in.PeerNodeID != nodeA {
			t.Errorf("session A's subscription %d is recorded under peer %#x, want the session's peer %#x", in.ID, in.PeerNodeID, nodeA)
		}
	}
}
