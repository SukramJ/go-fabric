// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
	"github.com/SukramJ/go-fabric/transport/tcp"
)

// tcpHarness is a started bridge with TCP incoming, an established CASE
// session (the secure harness's keys and ids) and a fake session
// registry recording what the bridge closes.
type tcpHarness struct {
	t        *testing.T
	bridge   *Bridge
	peer     *channel.Session
	registry *fakeSessionRegistry
	ack      *mrp.AckTracker
	adv      *fakeAdvertiser
}

func newTCPHarness(t *testing.T) *tcpHarness {
	t.Helper()
	st := openHarnessStore(t)
	fabric := addHarnessFabric(t, st, harnessFabricID, harnessCompressedID)
	adv := newFakeAdvertiser()
	b, err := New(wbEmptySnapshotter, adv, Config{
		Listen:    "127.0.0.1:0",
		VendorID:  0x1234,
		ProductID: 0x5678,
		NodeLabel: "tcp-test",
		TCP:       TCPConfig{Incoming: true, Outgoing: true},
	}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	b.AttachACLLister(st)
	ack := mrp.NewAckTracker(20 * time.Millisecond)
	b.AttachAckTracker(ack)
	reg := &fakeSessionRegistry{}
	b.AttachSessionRegistry(reg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := b.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer stopCancel()
		_ = b.Stop(stopCtx)
	})
	if err := st.ReplaceACL(context.Background(), fabric, []store.ACLEntry{{
		FabricIndex: fabric, Privilege: store.PrivilegeAdminister, AuthMode: store.AuthModeCASE,
		Subjects: []uint64{harnessControllerNodeID},
	}}); err != nil {
		t.Fatalf("ReplaceACL: %v", err)
	}

	encKey := make([]byte, 16)
	decKey := make([]byte, 16)
	for i := range encKey {
		encKey[i] = byte(0x40 + i)
		decKey[i] = byte(0x80 + i)
	}
	bridgeSess, err := channel.New(channel.Config{
		EncryptKey: encKey, DecryptKey: decKey,
		LocalNodeID: 0x0B0B, PeerNodeID: harnessControllerNodeID,
		PeerSessionID: 0x0777, InitialCounter: 1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	peerSess, err := channel.New(channel.Config{
		EncryptKey: decKey, DecryptKey: encKey,
		LocalNodeID: harnessControllerNodeID, PeerNodeID: 0x0B0B,
		InitialCounter: 5000,
	})
	if err != nil {
		t.Fatal(err)
	}
	b.AttachSessionLookup(&harnessLookup{session: bridgeSess, fabric: fabric, subject: harnessControllerNodeID})
	return &tcpHarness{t: t, bridge: b, peer: peerSess, registry: reg, ack: ack, adv: adv}
}

// dial opens a TCP connection to the bridge's listener; replies land
// on the returned channel.
func (h *tcpHarness) dial() (*tcp.Conn, <-chan []byte) {
	h.t.Helper()
	h.bridge.mu.RLock()
	tl := h.bridge.tcpListener
	h.bridge.mu.RUnlock()
	if tl == nil {
		h.t.Fatal("bridge has no TCP listener")
	}
	replies := make(chan []byte, 16)
	var d tcp.Dialer
	c, err := d.Dial(context.Background(), tl.Addr().String(), func(_ *tcp.Conn, msg []byte, _ func()) { replies <- msg })
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { _ = c.Close() })
	return c, replies
}

// sealedInvoke is an InvokeRequest on exchange xid, sealed under the
// controller's session, with the R flag set as a UDP controller sends it.
func (h *tcpHarness) sealedInvoke(xid uint16) []byte {
	h.t.Helper()
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutBool(tlv.ContextTag(0), false)
	enc.PutBool(tlv.ContextTag(1), false)
	enc.StartArray(tlv.ContextTag(2))
	enc.StartStruct(tlv.AnonymousTag())
	im.ConcreteCommandPath{Endpoint: 0, Cluster: 0x0006, Command: 0x01, HasEndpoint: true, HasCluster: true, HasCommand: true}.MarshalTLV(enc, tlv.ContextTag(0))
	enc.StartStruct(tlv.ContextTag(1))
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	enc.PutUint(tlv.ContextTag(0xFF), 12)
	_ = enc.EndContainer()
	body, err := enc.Bytes()
	if err != nil {
		h.t.Fatal(err)
	}
	proto := message.ProtocolHeader{
		Initiator: true, NeedsAck: true, Opcode: im.OpcodeInvokeRequest,
		ExchangeID: xid, ProtocolID: im.InteractionModelProtocolID,
	}
	plain := append(proto.Marshal(), body...)
	hdr := message.Header{SessionID: harnessLocalSessionID}
	sealed, err := h.peer.Encrypt(&hdr, securityFlagsByte(&hdr), plain)
	if err != nil {
		h.t.Fatal(err)
	}
	return append(hdr.Marshal(), sealed.Ciphertext...)
}

// open decrypts one message the bridge sent under the session.
func (h *tcpHarness) open(msg []byte) message.ProtocolHeader {
	h.t.Helper()
	hdr, n, err := message.UnmarshalHeader(msg)
	if err != nil {
		h.t.Fatal(err)
	}
	body, _, err := h.peer.Decrypt(&hdr, securityFlagsByte(&hdr), append([]byte(nil), msg[n:]...))
	if err != nil {
		h.t.Fatal(err)
	}
	proto, _, err := message.UnmarshalProtocolHeader(body)
	if err != nil {
		h.t.Fatal(err)
	}
	return proto
}

func waitReply(t *testing.T, replies <-chan []byte) []byte {
	t.Helper()
	select {
	case r := <-replies:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("no reply over TCP")
		return nil
	}
}

// TestTCPRequestRepliedOnSameConnWithoutMRP: a request that arrived on a
// TCP connection is answered on it, and MRP is off there — the reply
// neither requests nor piggybacks an acknowledgement, and no standalone
// acknowledgement is owed for the request's R flag (matter.js
// MessageExchange.ts:448-452, :660, :680-686; Session.ts:355-356).
func TestTCPRequestRepliedOnSameConnWithoutMRP(t *testing.T) {
	h := newTCPHarness(t)
	conn, replies := h.dial()
	if err := conn.Send(h.sealedInvoke(0x300)); err != nil {
		t.Fatal(err)
	}
	proto := h.open(waitReply(t, replies))
	if proto.Opcode != im.OpcodeInvokeResponse || proto.ExchangeID != 0x300 {
		t.Fatalf("reply opcode=0x%02X exchange=0x%X", proto.Opcode, proto.ExchangeID)
	}
	if proto.NeedsAck || proto.HasAck {
		t.Fatalf("reply over TCP carries MRP flags: R=%v A=%v", proto.NeedsAck, proto.HasAck)
	}
	if n := h.ack.Pending(); n != 0 {
		t.Fatalf("%d acknowledgement obligations registered for a message over TCP", n)
	}
	if n := h.bridge.RunAckPumpOnce(time.Now().Add(time.Hour)); n != 0 {
		t.Fatalf("ack pump emitted %d standalone acks for TCP traffic", n)
	}
	raw, ok := h.bridge.sessionPeerAddrs.Load(harnessLocalSessionID)
	if !ok {
		t.Fatal("session not bound to the connection")
	}
	if r, isConn := raw.(*connRoute); !isConn || !r.conn.Reliable() {
		t.Fatalf("session bound to %T, want the connection", raw)
	}
}

// TestTCPConnectionDropEvictsBoundSessions: when the connection closes,
// every session bound to it is closed without a farewell, as matter.js
// ExchangeManager.ts:650-665 #onConnectionDisconnect does.
func TestTCPConnectionDropEvictsBoundSessions(t *testing.T) {
	h := newTCPHarness(t)
	conn, replies := h.dial()
	if err := conn.Send(h.sealedInvoke(0x301)); err != nil {
		t.Fatal(err)
	}
	waitReply(t, replies)
	_ = conn.Close()

	deadline := time.Now().Add(5 * time.Second)
	for {
		h.registry.mu.Lock()
		closed := append([]uint16(nil), h.registry.closed...)
		h.registry.mu.Unlock()
		if len(closed) == 1 && closed[0] == harnessLocalSessionID {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("registry closed %v, want [%d]", closed, harnessLocalSessionID)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, ok := h.bridge.sessionPeerAddrs.Load(harnessLocalSessionID); ok {
		t.Fatal("evicted session still has a route")
	}
	n := 0
	h.bridge.connRoutes.Range(func(_, _ any) bool { n++; return true })
	if n != 0 {
		t.Fatalf("%d connection routes left after the drop", n)
	}
}

// TestTCPListenerSharesUDPPortAndAdvertisesT: the TCP listener binds
// the UDP socket's port (ServerNetworkRuntime.ts:203-214) and the
// operational record carries T=6 for client+server (Advertiser.h:63-64
// bits, ServerNetworkRuntime.ts:334-338).
func TestTCPListenerSharesUDPPortAndAdvertisesT(t *testing.T) {
	h := newTCPHarness(t)
	h.bridge.mu.RLock()
	udpPort := h.bridge.listener.LocalAddr().Port
	tcpPort := h.bridge.tcpListener.Addr().Port
	h.bridge.mu.RUnlock()
	if udpPort != tcpPort {
		t.Fatalf("TCP port %d, UDP port %d", tcpPort, udpPort)
	}
	h.bridge.AnnounceFabric(context.Background(), harnessCompressedID, 0x1234)
	var tVal string
	for _, svc := range h.adv.Active() {
		for _, r := range svc.TXT {
			if r.Key == "T" {
				tVal = r.Value
			}
		}
	}
	if tVal != strconv.Itoa(1<<1|1<<2) {
		t.Fatalf("T = %q, want 6", tVal)
	}
}

// fakeConn is a reliable transport.Conn recording what is sent on it.
type fakeConn struct {
	mu   sync.Mutex
	sent [][]byte
}

func (f *fakeConn) Send(msg []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, append([]byte(nil), msg...))
	return nil
}

func (f *fakeConn) Remote() net.Addr { return &net.TCPAddr{IP: net.IPv6loopback, Port: 40000} }
func (f *fakeConn) Reliable() bool   { return true }

// TestReceiveOverReliableDropsUnsecuredAndGroup: session establishment
// over TCP is not served (owner work, ADR 0019) and group messages are
// UDP multicast only.
func TestReceiveOverReliableDropsUnsecuredAndGroup(t *testing.T) {
	b := newStartedBridgeWithSnapshotter(t, wbEmptySnapshotter)
	c := &fakeConn{}
	unsecured := message.Header{SessionID: 0, MessageCounter: 1}
	buf := append(unsecured.Marshal(), message.ProtocolHeader{Initiator: true, Opcode: 0x20, ProtocolID: mrp.SecureChannelProtocolID}.Marshal()...)
	if err := b.receive(context.Background(), buf, c); !errors.Is(err, ErrUnsecuredOverReliable) {
		t.Fatalf("unsecured over TCP: %v", err)
	}
	group := message.Header{SessionID: 0x0101, SessionType: message.SessionGroup, MessageCounter: 1}
	if err := b.receive(context.Background(), group.Marshal(), c); !errors.Is(err, ErrGroupOverReliable) {
		t.Fatalf("group over TCP: %v", err)
	}
	if len(c.sent) != 0 {
		t.Fatalf("%d replies to dropped messages", len(c.sent))
	}
	if isUnsecuredUnicast([]byte{0}) {
		t.Fatal("short buffer read as unsecured")
	}
}

// TestBindSessionRouteNeverSwitchesTransport: matter.js replaces a
// session's channel only UDP-for-UDP (MessageChannel.ts:140-148); a
// reused session id binds afresh.
func TestBindSessionRouteNeverSwitchesTransport(t *testing.T) {
	b := newStartedBridgeWithSnapshotter(t, wbEmptySnapshotter)
	sess, err := channel.New(channel.Config{EncryptKey: make([]byte, 16), DecryptKey: make([]byte, 16)})
	if err != nil {
		t.Fatal(err)
	}
	lookup := &harnessLookup{session: sess}
	b.AttachSessionLookup(lookup)
	sid := harnessLocalSessionID
	udpA := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}
	udpB := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 2}
	route := b.routeFor(&fakeConn{})

	b.bindSessionRoute(sid, udpA)
	b.bindSessionRoute(sid, udpB)
	if got, _ := b.sessionPeerAddrs.Load(sid); got != net.Addr(udpB) {
		t.Fatalf("UDP rebinding lost: %v", got)
	}
	b.bindSessionRoute(sid, route)
	if got, _ := b.sessionPeerAddrs.Load(sid); got != net.Addr(udpB) {
		t.Fatalf("UDP session moved to TCP: %v", got)
	}

	b.sessionPeerAddrs.Delete(sid)
	b.bindSessionRoute(sid, route)
	b.bindSessionRoute(sid, udpA)
	if got, _ := b.sessionPeerAddrs.Load(sid); got != route {
		t.Fatalf("TCP session moved to UDP: %v", got)
	}

	// The id is reused by a new session: it binds afresh.
	next, err := channel.New(channel.Config{EncryptKey: make([]byte, 16), DecryptKey: make([]byte, 16)})
	if err != nil {
		t.Fatal(err)
	}
	lookup.mu.Lock()
	lookup.session = next
	lookup.mu.Unlock()
	b.bindSessionRoute(sid, udpA)
	if got, _ := b.sessionPeerAddrs.Load(sid); got != net.Addr(udpA) {
		t.Fatalf("reused id kept the old session's route: %v", got)
	}
}

// TestUnsolicitedReportOverTCPHasNoMRP: an ongoing report routed to a
// TCP-bound session is sent on the connection without the R flag and is
// not tracked for retransmission (MessageExchange.ts:660, :752).
func TestUnsolicitedReportOverTCPHasNoMRP(t *testing.T) {
	h := newTCPHarness(t)
	c := &fakeConn{}
	route := h.bridge.routeFor(c)
	h.bridge.bindSessionRoute(harnessLocalSessionID, route)
	counter, err := h.bridge.sendUnsolicitedIM(subTarget{
		src: route, sessionID: harnessLocalSessionID, exchangeID: 0x400, hasAck: true, ackCounter: 9,
	}, im.OpcodeReportData, []byte{0x15, 0x18})
	if err != nil {
		t.Fatal(err)
	}
	if counter != 0 {
		t.Fatalf("report over TCP tracked under counter %d", counter)
	}
	if len(c.sent) != 1 {
		t.Fatalf("sent %d messages on the connection", len(c.sent))
	}
	proto := h.open(c.sent[0])
	if proto.NeedsAck || proto.HasAck {
		t.Fatalf("report over TCP carries MRP flags: R=%v A=%v", proto.NeedsAck, proto.HasAck)
	}
}

// TestRouteHelpers covers the nil and fallback branches of the route
// helpers the reply paths rely on.
func TestRouteHelpers(t *testing.T) {
	b := newStartedBridgeWithSnapshotter(t, wbEmptySnapshotter)
	var nilUDP *net.UDPAddr
	if !isNilRoute(nil) || !isNilRoute(nilUDP) || !isNilRoute((*connRoute)(nil)) || isNilRoute(&net.TCPAddr{}) {
		t.Fatal("isNilRoute")
	}
	if b.routeFor(nil) != nil || b.routeFor(b.udpConn(nil)) != nil {
		t.Fatal("routeFor on nil connection")
	}
	src := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9}
	if got := b.routeFor(b.udpConn(src)); got != net.Addr(src) {
		t.Fatalf("UDP adapter route = %v", got)
	}
	c := &fakeConn{}
	r := b.routeFor(c)
	if b.routeFor(c) != r || r.Network() != "tcp" || r.String() == "" || routeUsesMRP(r) || !routeUsesMRP(src) {
		t.Fatal("connection route identity / reliability")
	}
	if err := sendVia(nil, src, []byte{1}); !errors.Is(err, errNoRoute) {
		t.Fatalf("UDP without listener: %v", err)
	}
	if err := sendVia(nil, nilUDP, []byte{1}); !errors.Is(err, errNoRoute) {
		t.Fatalf("nil UDP: %v", err)
	}
	if err := sendVia(nil, (*connRoute)(nil), []byte{1}); !errors.Is(err, errNoRoute) {
		t.Fatalf("nil conn route: %v", err)
	}
	if err := sendVia(nil, &net.TCPAddr{}, []byte{1}); !errors.Is(err, errNoRoute) {
		t.Fatalf("foreign addr: %v", err)
	}
	if b.evictConn(&fakeConn{}) != nil {
		t.Fatal("evicting a connection that never carried a message")
	}
	if !autoAssignedPort("127.0.0.1:0") || autoAssignedPort("") || autoAssignedPort(":5540") {
		t.Fatal("autoAssignedPort")
	}
	if tcpBindAddr("", false, 5540) != "[::]:5540" || tcpBindAddr("", true, 1) != "0.0.0.0:1" || tcpBindAddr("127.0.0.1:0", false, 7) != "127.0.0.1:7" {
		t.Fatal("tcpBindAddr")
	}
}
