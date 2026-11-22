// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"crypto/hkdf"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/clock/clocktest"
	"thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/mdns"
	"thingfabric.dev/go-fabric/secure/channel"
	"thingfabric.dev/go-fabric/secure/operational"
	"thingfabric.dev/go-fabric/secure/sigma"
	"thingfabric.dev/go-fabric/store"
	"thingfabric.dev/go-fabric/transport/message"
	"thingfabric.dev/go-fabric/transport/mrp"
	"thingfabric.dev/go-fabric/transport/udp"
)

// caseAdministerACL grants Administer to one CASE subject on one fabric and
// nothing else: a request on a PASE session is served only through the
// implicit Administer grant of the commissioning channel.
type caseAdministerACL struct {
	fabric  uint8
	subject uint64
}

func (a caseAdministerACL) ListACL(_ context.Context, fabric uint8) ([]store.ACLEntry, error) {
	if fabric != a.fabric {
		return nil, nil
	}
	return []store.ACLEntry{{
		FabricIndex: a.fabric, Privilege: store.PrivilegeAdminister, AuthMode: store.AuthModeCASE,
		Subjects: []uint64{a.subject},
	}}, nil
}

// peerDatagram is one datagram the commissioner's socket received,
// decrypted with the session its header names.
type peerDatagram struct {
	sessionID uint16
	proto     message.ProtocolHeader
	body      []byte // the plaintext after the protocol header
}

// readPeerDatagrams reads what reaches conn until wait passes without a
// datagram, decrypting each with the peer session its header names.
func readPeerDatagrams(t *testing.T, conn *net.UDPConn, wait time.Duration, peers map[uint16]*channel.Session) []peerDatagram {
	t.Helper()
	var got []peerDatagram
	buf := make([]byte, udp.MaxDatagramSize)
	for {
		if err := conn.SetReadDeadline(time.Now().Add(wait)); err != nil {
			t.Fatalf("SetReadDeadline: %v", err)
		}
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			return got
		}
		hdr, hdrLen, err := message.UnmarshalHeader(buf[:n])
		if err != nil {
			t.Fatalf("UnmarshalHeader: %v", err)
		}
		peer, ok := peers[hdr.SessionID]
		if !ok {
			t.Fatalf("a datagram on session %d, which the commissioner does not hold", hdr.SessionID)
		}
		plain, _, err := peer.Decrypt(&hdr, securityFlagsByte(&hdr), append([]byte(nil), buf[hdrLen:n]...))
		if err != nil {
			t.Fatalf("Decrypt on session %d: %v", hdr.SessionID, err)
		}
		proto, protoLen, err := message.UnmarshalProtocolHeader(plain)
		if err != nil {
			t.Fatalf("UnmarshalProtocolHeader: %v", err)
		}
		got = append(got, peerDatagram{sessionID: hdr.SessionID, proto: proto, body: plain[protoLen:]})
	}
}

// TestCommissioningCompleteClearsThePASESessionAtOnce pins Matter
// §11.10.6.6 step 4 as matter.js performs it — the PASE session is closed
// inside the command, before the response is built, silently and with no
// timer (FailsafeContext.ts:156-157, :191-196,
// Session.ts:331-344, NodeSession.ts:450-453 at b25ed960) — on a bridge
// with a real session table: after a CommissioningComplete answered OK
// over CASE, (a) the PASE session AddNOC adopted onto the fabric is gone
// without the clock moving, (b) a request on its id is dropped as an
// unknown session instead of being served with the commissioning
// channel's implicit Administer grant, and (c) no CloseSession is sent
// for it, not even by the bridge's Stop.
func TestCommissioningCompleteClearsThePASESessionAtOnce(t *testing.T) {
	t.Parallel()
	const (
		casePeerSession uint16 = 0x0191
		pasePeerSession uint16 = 0x0177
		bridgeNodeID    uint64 = 0x0B0B
		peerNodeID      uint64 = 0x1234
		fabric          uint8  = 1
	)
	clk := clocktest.New(time.Unix(1_700_000_000, 0))
	var logs syncBuffer
	b, err := New(wbEmptySnapshotter, mdns.NewNoop(), Config{Listen: "127.0.0.1:0", Clock: clk},
		slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	startCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := b.Start(startCtx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	stopped := false
	t.Cleanup(func() {
		if !stopped {
			_ = b.Stop(context.Background())
		}
	})
	b.AttachACLLister(caseAdministerACL{fabric: fabric, subject: peerNodeID})
	gc, err := core.NewGeneralCommissioning(core.GeneralCommissioningConfig{
		LocationCapability: core.RegulatoryIndoor, FailSafeMaxSeconds: 600,
	})
	if err != nil {
		t.Fatal(err)
	}
	b.AttachRootClusters([]contract.ClusterServer{gc})

	// The session table, wired as a host wires it (node/security.go).
	mgr := operational.NewManager(nil)
	mgr.SetClock(clk)
	mgr.SetOnSessionClose(b.SessionClosed)
	b.AttachSessionRegistry(mgr)
	get := func(id uint16) (*operational.Entry, bool) {
		e, err := mgr.Get(id)
		return e, err == nil && e != nil
	}
	b.AttachSessionLookup(NewOperationalSessionLookup(func(id uint16) (*channel.Session, bool) {
		if e, ok := get(id); ok {
			return e.Session, true
		}
		return nil, false
	}).WithFabricResolver(func(id uint16) (uint8, bool) {
		if e, ok := get(id); ok {
			return e.FabricIndex(), true
		}
		return 0, false
	}).WithSubjectResolver(func(id uint16) (uint64, []uint32, bool) {
		if e, ok := get(id); ok && e.Session != nil {
			return e.Session.PeerNodeID(), e.Session.PeerCATs(), true
		}
		return 0, nil, false
	}).WithPASEResolver(func(id uint16) (bool, bool) {
		if e, ok := get(id); ok {
			return e.IsPASE(), true
		}
		return false, false
	}))

	// The commissioning's PASE session, adopted onto the fabric by AddNOC,
	// and the CASE session CommissioningComplete arrives on.
	secret := []byte("pase-shared-secret")
	paseStake, err := mgr.AllocateStake()
	if err != nil {
		t.Fatal(err)
	}
	paseSession := paseStake.ID
	if _, err := mgr.OpenFromPaseWithStake(paseStake, bridgeNodeID, peerNodeID+1, pasePeerSession, secret); err != nil {
		t.Fatalf("OpenFromPaseWithStake: %v", err)
	}
	if err := mgr.AdoptFabricIndex(paseSession, fabric); err != nil {
		t.Fatalf("AdoptFabricIndex: %v", err)
	}
	derived, err := hkdf.Key(sha256.New, secret, nil, "SessionKeys", 48)
	if err != nil {
		t.Fatal(err)
	}
	pasePeer, err := channel.New(channel.Config{
		EncryptKey: derived[0:16], DecryptKey: derived[16:32],
		LocalNodeID: peerNodeID + 1, PeerNodeID: bridgeNodeID, InitialCounter: 3000,
	})
	if err != nil {
		t.Fatal(err)
	}
	var keys sigma.SessionKeys
	for i := range keys.I2RKey {
		keys.I2RKey[i], keys.R2IKey[i] = byte(0x10+i), byte(0x60+i)
	}
	caseStake, err := mgr.AllocateStake()
	if err != nil {
		t.Fatal(err)
	}
	caseSession := caseStake.ID
	if _, err := mgr.OpenFromSigmaWithStake(caseStake, fabric, bridgeNodeID, peerNodeID, casePeerSession, nil, keys); err != nil {
		t.Fatalf("OpenFromSigmaWithStake: %v", err)
	}
	casePeer, err := channel.New(channel.Config{
		EncryptKey: keys.I2RKey[:], DecryptKey: keys.R2IKey[:],
		LocalNodeID: peerNodeID, PeerNodeID: bridgeNodeID, InitialCounter: 4000,
	})
	if err != nil {
		t.Fatal(err)
	}
	peers := map[uint16]*channel.Session{pasePeerSession: pasePeer, casePeerSession: casePeer}

	peerConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	t.Cleanup(func() { _ = peerConn.Close() })
	peerAddr, _ := peerConn.LocalAddr().(*net.UDPAddr)
	exchange := uint16(0x5000)
	send := func(peer *channel.Session, session uint16, payload []byte) error {
		exchange++
		return b.dispatch(context.Background(), encryptIMFromPeer(t, peer, session, message.ProtocolHeader{
			Opcode: im.OpcodeInvokeRequest, ExchangeID: exchange, Initiator: true, NeedsAck: true,
		}, payload), peerAddr)
	}
	ccPath := im.ConcreteCommandPath{Endpoint: 0, Cluster: generalCommissioningClusterID, Command: 0x04, HasEndpoint: true}

	// The commissioner's CommissioningComplete over PASE is refused and
	// answered on the PASE session (the bridge learns its route), which
	// stays open: the control for (c).
	if err := send(pasePeer, paseSession, encodeInvokeRequestSuppress(t, false, ccPath)); err != nil {
		t.Fatalf("dispatch of CommissioningComplete over PASE: %v", err)
	}
	if got := readPeerDatagrams(t, peerConn, 300*time.Millisecond, peers); len(got) == 0 || got[0].sessionID != pasePeerSession {
		t.Fatalf("the CommissioningComplete over PASE was not answered on the PASE session: %+v", got)
	}
	if _, ok := get(paseSession); !ok {
		t.Fatal("a refused CommissioningComplete closed the PASE session")
	}

	// The fail-safe of the commissioning, AddNOC having moved it onto the
	// fabric, then CommissioningComplete over CASE.
	if err := gc.ArmFailSafeFor(context.Background(), 60, fabric); err != nil {
		t.Fatalf("ArmFailSafeFor: %v", err)
	}
	if err := send(casePeer, caseSession, encodeInvokeRequestSuppress(t, false, ccPath)); err != nil {
		t.Fatalf("dispatch of CommissioningComplete over CASE: %v", err)
	}
	if gc.FailSafeArmed() {
		t.Fatal("the fail-safe is still armed: CommissioningComplete did not succeed, the test checks nothing")
	}

	// (a) The PASE session is gone without the clock moving.
	if _, ok := get(paseSession); ok {
		t.Errorf("(a) PASE session %d still open after CommissioningComplete answered OK", paseSession)
	}
	if _, ok := get(caseSession); !ok {
		t.Errorf("(a) the CASE session %d the command arrived on was closed", caseSession)
	}
	found := false
	for line := range strings.Lines(logs.String()) {
		if strings.Contains(line, "msg=matter.session.pase_close ") &&
			strings.Contains(line, " reason=commissioning_complete ") &&
			strings.HasSuffix(line, fmt.Sprintf(" ids=%d\n", paseSession)) {
			found = true
		}
	}
	if !found {
		t.Errorf("(a) no matter.session.pase_close line naming session %d (TC-DD-3.5 reads it)", paseSession)
	}

	// The response to CommissioningComplete, on CASE.
	afterCC := readPeerDatagrams(t, peerConn, 300*time.Millisecond, peers)

	// (b) A request on the PASE session — ArmFailSafe, which needs
	// Administer, granted to the CASE subject only — is not served.
	if err := send(pasePeer, paseSession, encodeArmFailSafe(t, 60)); !errors.Is(err, ErrSessionMissing) {
		t.Errorf("(b) dispatch of ArmFailSafe on the PASE session = %v, want ErrSessionMissing", err)
	}
	afterProbe := readPeerDatagrams(t, peerConn, 300*time.Millisecond, peers)
	for _, d := range afterProbe {
		if d.sessionID == pasePeerSession && d.proto.ProtocolID == im.InteractionModelProtocolID {
			t.Errorf("(b) the bridge answered on the PASE session after CommissioningComplete: opcode %#x", d.proto.Opcode)
		}
	}
	if gc.FailSafeArmed() {
		t.Error("(b) ArmFailSafe on the PASE session re-armed the fail-safe after CommissioningComplete")
	}

	// (c) No CloseSession goes out for it, not even at Stop.
	stopped = true
	if err := b.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	got := slices.Concat(afterCC, afterProbe)
	got = append(got, readPeerDatagrams(t, peerConn, 300*time.Millisecond, peers)...)
	for _, d := range got {
		if d.sessionID == pasePeerSession && d.proto.ProtocolID == mrp.SecureChannelProtocolID &&
			d.proto.Opcode == mrp.SCOpcodeStatusReport {
			t.Errorf("(c) a CloseSession went out on the PASE session cleared by CommissioningComplete")
		}
	}
}
