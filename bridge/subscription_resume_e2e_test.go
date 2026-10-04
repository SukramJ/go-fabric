// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

// In-process restart test for subscription persistence and
// re-establishment (docs/adr/0008). A fake controller on a loopback UDP
// socket subscribes over a CASE session; the bridge is stopped and a new
// bridge is started over the same subscription store; the new bridge
// resolves the controller, opens CASE to it as the initiator, re-sends the
// priming report under the OLD subscription id, and ongoing reports flow
// on the new session. Mirrors matter.js
// packages/node/test/behavior/system/subscriptions/SubscriptionsServerTest.ts
// (restart → subscription re-established with the same id).

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/secure/operational"
	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// memSubscriptionStore is an in-memory [SubscriptionStore].
type memSubscriptionStore struct {
	mu   sync.Mutex
	rows map[uint32]memRow
}

type memRow struct {
	fabric  uint8
	node    uint64
	payload []byte
}

func newMemSubscriptionStore() *memSubscriptionStore {
	return &memSubscriptionStore{rows: make(map[uint32]memRow)}
}

func (s *memSubscriptionStore) SaveServerSubscription(_ context.Context, id uint32, fabric uint8, node uint64, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows[id] = memRow{fabric: fabric, node: node, payload: append([]byte(nil), payload...)}
	return nil
}

func (s *memSubscriptionStore) DeleteServerSubscription(_ context.Context, id uint32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rows, id)
	return nil
}

func (s *memSubscriptionStore) DeleteServerSubscriptionsByFabric(_ context.Context, fabric uint8) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, r := range s.rows {
		if r.fabric == fabric {
			delete(s.rows, id)
		}
	}
	return nil
}

func (s *memSubscriptionStore) LoadServerSubscriptions(context.Context) ([][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([][]byte, 0, len(s.rows))
	for _, r := range s.rows {
		out = append(out, r.payload)
	}
	return out, nil
}

func (s *memSubscriptionStore) ClearServerSubscriptions(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.rows)
	return nil
}

func (s *memSubscriptionStore) ids() []uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]uint32, 0, len(s.rows))
	for id := range s.rows {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

// resumeVerifier: NOCs are bare public keys; node ids come from a table.
type resumeVerifier struct{ nodes map[string]uint64 }

func (resumeVerifier) VerifyAndExtractPubKey(noc, _ []byte) (*ecdsa.PublicKey, error) {
	return caseTestVerifier{}.VerifyAndExtractPubKey(noc, nil)
}

func (v resumeVerifier) PeerNodeIDFromNOC(noc []byte) (uint64, error) {
	id, ok := v.nodes[string(noc)]
	if !ok {
		return 0, errors.New("unknown noc")
	}
	return id, nil
}

// resumeFabric is the shared fabric of device and controller.
type resumeFabric struct {
	ipk        [16]byte
	root       []byte
	cfid       [8]byte
	device     *sigma.Identity
	controller *sigma.Identity
	verifier   resumeVerifier
}

const (
	resumeFabricIndex  = 1
	resumeDeviceNode   = uint64(0xD0D0D0D0)
	resumeCtrlNode     = uint64(0xC0C0C0C0)
	resumeDeviceSIDRun = uint16(0x0101)
	resumeCtrlSIDRun1  = uint16(0x0202)
	resumeCtrlSIDRun2  = uint16(0x0303)
)

func newResumeFabric(t *testing.T) resumeFabric {
	t.Helper()
	ipk := newCaseTestIPK(t)
	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	root, err := rootKey.PublicKey.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	f := resumeFabric{
		ipk:        ipk,
		root:       root,
		cfid:       [8]byte{1, 2, 3, 4, 5, 6, 7, 8},
		device:     newCaseTestIdentity(t, resumeDeviceNode, 0xFAB1, ipk),
		controller: newCaseTestIdentity(t, resumeCtrlNode, 0xFAB1, ipk),
	}
	f.verifier = resumeVerifier{nodes: map[string]uint64{
		string(f.device.NOC):     resumeDeviceNode,
		string(f.controller.NOC): resumeCtrlNode,
	}}
	return f
}

func (f resumeFabric) ResolveSigma1Destination(dest [32]byte, random [sigma.RandomSize]byte) (*sigma.Identity, sigma.PeerVerifier, bool) {
	if sigma.ComputeDestinationID(f.controller.IPK, random, f.root, f.controller.FabricID, f.controller.NodeID) == dest {
		return f.controller, f.verifier, true
	}
	return nil, nil, false
}

// silentAnswer makes the fake controller swallow ReportData without a
// StatusResponse.
const silentAnswer = im.StatusCode(0xFF)

// reportSeen is one ReportData the fake controller received.
type reportSeen struct {
	subID     uint32
	sessionID uint16
	attrs     int
}

// fakeController plays the controller: a subscriber over a CASE session,
// and the CASE responder the restarted device dials.
type fakeController struct {
	t    *testing.T
	fab  resumeFabric
	conn *net.UDPConn
	addr *net.UDPAddr

	mu       sync.Mutex
	bridge   *net.UDPAddr
	sess     *channel.Session
	localSID uint16
	peerSID  uint16
	resp     *sigma.Responder
	answer   im.StatusCode
	counter  uint32

	reports chan reportSeen
	subResp chan uint32
	sigma1s chan struct{}
}

func newFakeController(t *testing.T, fab resumeFabric) *fakeController {
	t.Helper()
	conn, addr := newSubscribeTestPeer(t)
	c := &fakeController{
		t: t, fab: fab, conn: conn, addr: addr, answer: im.StatusSuccess,
		reports: make(chan reportSeen, 16),
		subResp: make(chan uint32, 4),
		sigma1s: make(chan struct{}, 4),
	}
	go c.serve()
	return c
}

func (c *fakeController) setBridge(a *net.UDPAddr) {
	c.mu.Lock()
	c.bridge = a
	c.mu.Unlock()
}

func (c *fakeController) bridgeAddr() *net.UDPAddr {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.bridge
}

func (c *fakeController) setSession(sess *channel.Session, localSID, peerSID uint16) {
	c.mu.Lock()
	c.sess, c.localSID, c.peerSID = sess, localSID, peerSID
	c.mu.Unlock()
}

func (c *fakeController) serve() {
	buf := make([]byte, 9000)
	for {
		n, _, err := c.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		c.handle(append([]byte(nil), buf[:n]...))
	}
}

func (c *fakeController) handle(datagram []byte) {
	hdr, hdrLen, err := message.UnmarshalHeader(datagram)
	if err != nil {
		return
	}
	body := datagram[hdrLen:]
	if hdr.SessionID == 0 {
		c.handleUnsecured(&hdr, body)
		return
	}
	c.mu.Lock()
	sess, localSID := c.sess, c.localSID
	c.mu.Unlock()
	if sess == nil || hdr.SessionID != localSID {
		return
	}
	plain, _, err := sess.Decrypt(&hdr, hdr.NonceSecurityFlags(), body)
	if err != nil {
		return
	}
	proto, protoLen, err := message.UnmarshalProtocolHeader(plain)
	if err != nil || proto.ProtocolID != im.InteractionModelProtocolID {
		return
	}
	payload := plain[protoLen:]
	switch proto.Opcode {
	case im.OpcodeReportData:
		subID, attrs := decodeReportSummary(payload)
		c.mu.Lock()
		answer := c.answer
		c.mu.Unlock()
		if !reportSuppressesResponse(payload) && answer != silentAnswer {
			sr, _ := EncodeStatusResponse(im.StatusResponse{Status: answer})
			c.sendSecureAck(proto, hdr.MessageCounter, im.OpcodeStatusResponse, sr)
		}
		c.reports <- reportSeen{subID: subID, sessionID: hdr.SessionID, attrs: attrs}
	case im.OpcodeSubscribeResponse:
		subID, _ := decodeReportSummary(payload) // SubscriptionId is tag 0 in both
		c.subResp <- subID
	}
}

func (c *fakeController) sendSecure(inReplyTo message.ProtocolHeader, opcode uint8, payload []byte) {
	c.sendSecureAck(inReplyTo, 0, opcode, payload)
}

// sendSecureAck sends on the secure session, piggybacking an ack of
// ackCounter when it is non-zero.
func (c *fakeController) sendSecureAck(inReplyTo message.ProtocolHeader, ackCounter uint32, opcode uint8, payload []byte) {
	c.mu.Lock()
	sess, peerSID := c.sess, c.peerSID
	c.mu.Unlock()
	proto := message.ProtocolHeader{
		Initiator:  !inReplyTo.Initiator,
		Opcode:     opcode,
		ExchangeID: inReplyTo.ExchangeID,
		ProtocolID: inReplyTo.ProtocolID,
		HasAck:     ackCounter != 0,
		AckCounter: ackCounter,
	}
	hdr := message.Header{SessionID: peerSID}
	enc, err := sess.Encrypt(&hdr, hdr.NonceSecurityFlags(), append(proto.Marshal(), payload...))
	if err != nil {
		c.t.Errorf("controller encrypt: %v", err)
		return
	}
	_, _ = c.conn.WriteToUDP(append(hdr.Marshal(), enc.Ciphertext...), c.bridgeAddr())
}

func (c *fakeController) sendUnsecured(to *message.Header, inReplyTo message.ProtocolHeader, opcode uint8, payload []byte) {
	c.mu.Lock()
	c.counter++
	counter := c.counter
	c.mu.Unlock()
	hdr := message.Header{SessionID: 0, MessageCounter: counter}
	if to.HasSourceNodeID {
		hdr.DestSize = message.DestNodeID
		hdr.DestNodeID = to.SourceNodeID
	}
	proto := message.ProtocolHeader{
		Initiator:  false,
		HasAck:     true,
		AckCounter: to.MessageCounter,
		Opcode:     opcode,
		ExchangeID: inReplyTo.ExchangeID,
		ProtocolID: mrp.SecureChannelProtocolID,
	}
	datagram := append(hdr.Marshal(), proto.Marshal()...)
	_, _ = c.conn.WriteToUDP(append(datagram, payload...), c.bridgeAddr())
}

// handleUnsecured answers the device's CASE handshake as the responder.
func (c *fakeController) handleUnsecured(hdr *message.Header, body []byte) {
	proto, protoLen, err := message.UnmarshalProtocolHeader(body)
	if err != nil || proto.ProtocolID != mrp.SecureChannelProtocolID {
		return
	}
	payload := body[protoLen:]
	switch proto.Opcode {
	case mrp.SCOpcodeSigma1:
		resp := sigma.NewResponder(c.fab.controller, c.fab.verifier, resumeCtrlSIDRun2)
		resp.SetIdentityResolver(c.fab)
		sigma2, err := resp.ProcessSigma1(payload)
		if err != nil {
			c.t.Errorf("controller ProcessSigma1: %v", err)
			return
		}
		c.mu.Lock()
		c.resp = resp
		c.mu.Unlock()
		c.sigma1s <- struct{}{}
		c.sendUnsecured(hdr, proto, mrp.SCOpcodeSigma2, sigma2.Marshal())
	case mrp.SCOpcodeSigma3:
		c.mu.Lock()
		resp := c.resp
		c.mu.Unlock()
		if resp == nil {
			return
		}
		if err := resp.ProcessSigma3(payload); err != nil {
			c.t.Errorf("controller ProcessSigma3: %v", err)
			return
		}
		keys, _ := resp.SessionKeys()
		sess, err := channel.New(channel.Config{
			EncryptKey:    keys.R2IKey[:],
			DecryptKey:    keys.I2RKey[:],
			LocalNodeID:   resumeCtrlNode,
			PeerNodeID:    resp.PeerNodeID(),
			PeerSessionID: resp.PeerSessionID(),
		})
		if err != nil {
			c.t.Errorf("controller session: %v", err)
			return
		}
		c.setSession(sess, resumeCtrlSIDRun2, resp.PeerSessionID())
		ok := mrp.EncodeStatusReport(mrp.SCStatusGeneralSuccess, uint32(mrp.SecureChannelProtocolID), mrp.SCStatusProtocolSessionEstablishmentSuccess, nil)
		c.sendUnsecured(hdr, proto, mrp.SCOpcodeStatusReport, ok)
	}
}

func (c *fakeController) subscribe(req im.SubscribeRequest) {
	enc := tlv.NewEncoder()
	req.MarshalTLV(enc)
	payload, err := enc.Bytes()
	if err != nil {
		c.t.Fatal(err)
	}
	c.sendSecure(message.ProtocolHeader{
		Initiator:  false, // so the request goes out with Initiator=true
		ExchangeID: 0x0777,
		ProtocolID: im.InteractionModelProtocolID,
	}, im.OpcodeSubscribeRequest, payload)
}

func (c *fakeController) nextReport(t *testing.T, within time.Duration) reportSeen {
	t.Helper()
	select {
	case r := <-c.reports:
		return r
	case <-time.After(within):
		t.Fatal("controller saw no ReportData in time")
		return reportSeen{}
	}
}

// decodeReportSummary reads SubscriptionId (context tag 0) and counts the
// AttributeReports (tag 1) of a ReportData or SubscribeResponse.
func decodeReportSummary(payload []byte) (subID uint32, attrs int) {
	dec := tlv.NewDecoder(payload)
	depth := 0
	inAttrs := false
	for {
		el, err := dec.Next()
		if errors.Is(err, io.EOF) || err != nil {
			return subID, attrs
		}
		switch {
		case el.IsEndContainer:
			depth--
			if depth == 1 {
				inAttrs = false
			}
		case el.IsContainer:
			if depth == 1 && el.Tag.Kind == tlv.TagKindContext && el.Tag.Number == 1 {
				inAttrs = true
			} else if depth == 2 && inAttrs {
				attrs++
			}
			depth++
		default:
			if depth == 1 && el.Tag.Kind == tlv.TagKindContext && el.Tag.Number == 0 {
				subID = uint32(el.Uint) //nolint:gosec // test decode of a uint32 field
			}
		}
	}
}

// reportSuppressesResponse reads SuppressResponse (context tag 4).
func reportSuppressesResponse(payload []byte) bool {
	dec := tlv.NewDecoder(payload)
	depth := 0
	for {
		el, err := dec.Next()
		if err != nil {
			return false
		}
		switch {
		case el.IsEndContainer:
			depth--
		case el.IsContainer:
			depth++
		default:
			if depth == 1 && el.Tag.Kind == tlv.TagKindContext && el.Tag.Number == 4 {
				return el.Bool
			}
		}
	}
}

// resumeDevice is one run of the device: a started bridge with a real
// operational session manager, an ACL'd AccessControl cluster to
// subscribe to, and the shared subscription store.
type resumeDevice struct {
	br       *Bridge
	sessions *operational.Manager
	subs     *subscription.Manager
}

type nopResumptionStore struct{}

func (nopResumptionStore) UpsertResumption(context.Context, store.ResumptionRecord) error { return nil }

func (nopResumptionStore) GetResumptionByID(context.Context, []byte) (store.ResumptionRecord, error) {
	return store.ResumptionRecord{}, errors.New("none")
}

func (nopResumptionStore) GetResumptionByPeer(context.Context, uint8, uint64) (store.ResumptionRecord, error) {
	return store.ResumptionRecord{}, errors.New("none")
}

func (nopResumptionStore) RemoveResumption(context.Context, uint8, uint64) error { return nil }

func startResumeDevice(t *testing.T, st SubscriptionStore, configure func(*Bridge, *operational.Manager)) *resumeDevice {
	t.Helper()
	br, err := New(wbEmptySnapshotter, mdns.NewNoop(), Config{Listen: "127.0.0.1:0", VendorID: 0xFFF1, ProductID: 0x8001, NodeLabel: "resume"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	acl := &aclStoreFake{entries: []store.ACLEntry{{FabricIndex: resumeFabricIndex, Privilege: store.PrivilegeAdminister, AuthMode: store.AuthModeCASE}}}
	ac, err := core.NewAccessControl(acl)
	if err != nil {
		t.Fatal(err)
	}
	br.AttachRootClusters([]contract.ClusterServer{ac})
	br.AttachACLLister(acl)
	sessions := operational.NewManager(nopResumptionStore{})
	br.AttachSessionLookup(NewOperationalSessionLookup(func(id uint16) (*channel.Session, bool) {
		e, err := sessions.Get(id)
		if err != nil || e == nil || e.Session == nil {
			return nil, false
		}
		return e.Session, true
	}).WithFabricResolver(func(id uint16) (uint8, bool) {
		e, err := sessions.Get(id)
		if err != nil || e == nil {
			return 0, false
		}
		return e.FabricIndex(), true
	}).WithSubjectResolver(func(id uint16) (uint64, []uint32, bool) {
		e, err := sessions.Get(id)
		if err != nil || e == nil || e.Session == nil {
			return 0, nil, false
		}
		return e.Session.PeerNodeID(), e.Session.PeerCATs(), true
	}))
	subs := subscription.NewManager(subscription.Config{TickInterval: 20 * time.Millisecond}, br.SubscriptionReporter(), nil)
	subs.SetEventReporter(br.SubscriptionEventReporter())
	br.AttachSubscriptionManager(subs)
	sessions.SetOnSessionClose(subs.CloseSession)
	br.AttachSessionRegistry(sessions)
	br.AttachSubscriptionStore(st)
	if configure != nil {
		configure(br, sessions)
	}
	ctx, cancel := context.WithCancel(context.Background())
	subs.Start(ctx)
	if err := br.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	d := &resumeDevice{br: br, sessions: sessions, subs: subs}
	t.Cleanup(func() {
		d.stop()
		cancel()
	})
	return d
}

func (d *resumeDevice) stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = d.br.Stop(ctx)
	d.subs.Stop()
}

func (d *resumeDevice) addr(t *testing.T) *net.UDPAddr {
	t.Helper()
	a, err := net.ResolveUDPAddr("udp", d.br.LocalAddr())
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// staticResolver answers the operational query with a fixed address.
type staticResolver struct {
	addr  *net.UDPAddr
	calls chan [8]byte
}

func (r staticResolver) ResolveOperational(_ context.Context, cfid [8]byte, _ uint64) ([]*net.UDPAddr, error) {
	if r.calls != nil {
		r.calls <- cfid
	}
	return []*net.UDPAddr{r.addr}, nil
}

func resumeInitiatorProvider(fab resumeFabric, sessions *operational.Manager) CaseInitiatorProvider {
	return func(_ context.Context, fabricIndex uint8, peerNodeID uint64) (*CaseInitiation, error) {
		sid, err := sessions.AllocateID()
		if err != nil {
			return nil, err
		}
		ini, err := sigma.NewPeerInitiator(sigma.InitiatorConfig{
			Identity: fab.device, Verifier: fab.verifier, SessionID: sid,
			PeerNodeID: peerNodeID, RootPublicKey: fab.root,
		})
		if err != nil {
			return nil, err
		}
		return &CaseInitiation{
			Initiator:          ini,
			CompressedFabricID: fab.cfid,
			OnEstablished: func(_ context.Context, res sigma.InitiatorResult) error {
				_, err := sessions.OpenFromSigmaAsInitiatorWithID(sid, fabricIndex, resumeDeviceNode, res.PeerNodeID, res.PeerSessionID, res.PeerCATs, res.Keys)
				return err
			},
			OnAbandoned: func() { sessions.ReleaseID(sid) },
		}, nil
	}
}

// subscribeOverCASE runs the first run's subscription: a pre-shared CASE
// session (the handshake itself is covered elsewhere) and a real
// SubscribeRequest over the wire. Returns the subscription id.
func subscribeOverCASE(t *testing.T, dev *resumeDevice, ctrl *fakeController) uint32 {
	t.Helper()
	var keys sigma.SessionKeys
	_, _ = rand.Read(keys.I2RKey[:])
	_, _ = rand.Read(keys.R2IKey[:])
	if _, err := dev.sessions.OpenFromSigmaWithID(resumeDeviceSIDRun, resumeFabricIndex, resumeDeviceNode, resumeCtrlNode, resumeCtrlSIDRun1, nil, keys); err != nil {
		t.Fatal(err)
	}
	sess, err := channel.New(channel.Config{
		EncryptKey: keys.I2RKey[:], DecryptKey: keys.R2IKey[:],
		LocalNodeID: resumeCtrlNode, PeerNodeID: resumeDeviceNode, PeerSessionID: resumeDeviceSIDRun,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctrl.setSession(sess, resumeCtrlSIDRun1, resumeDeviceSIDRun)
	ctrl.subscribe(im.SubscribeRequest{
		KeepSubscriptions:  true,
		MinIntervalFloor:   0,
		MaxIntervalCeiling: 120,
		FabricFiltered:     true,
		AttributeRequests:  []im.ConcreteAttributePath{accessControlACLPath()},
	})
	priming := ctrl.nextReport(t, 3*time.Second)
	if priming.attrs == 0 {
		t.Fatal("first-run priming report carried no attribute")
	}
	select {
	case id := <-ctrl.subResp:
		if id == 0 || id != priming.subID {
			t.Fatalf("SubscribeResponse id %#x, priming id %#x", id, priming.subID)
		}
		return id
	case <-time.After(3 * time.Second):
		t.Fatal("no SubscribeResponse")
		return 0
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestSubscriptionResumption_RestartEndToEnd is the restart scenario the
// whole feature exists for.
func TestSubscriptionResumption_RestartEndToEnd(t *testing.T) {
	runRestartEndToEnd(t, false)
}

// TestSubscriptionResumption_RestartEndToEndWithMRP runs the same restart
// with the MRP tracker wired, so the initiator's Sigma1/Sigma3 and the
// priming report travel reliably and the controller's replies carry the
// piggybacked acknowledgements.
func TestSubscriptionResumption_RestartEndToEndWithMRP(t *testing.T) {
	runRestartEndToEnd(t, true)
}

func runRestartEndToEnd(t *testing.T, withMRP bool) {
	t.Helper()
	fab := newResumeFabric(t)
	st := newMemSubscriptionStore()
	ctrl := newFakeController(t, fab)
	mrpWiring := func(br *Bridge) {
		if withMRP {
			br.AttachAckTracker(mrp.NewAckTracker(mrp.DefaultStandaloneAckDelay))
		}
	}

	// --- run 1: subscribe, persist ---------------------------------------
	run1 := startResumeDevice(t, st, func(br *Bridge, _ *operational.Manager) { mrpWiring(br) })
	ctrl.setBridge(run1.addr(t))
	subID := subscribeOverCASE(t, run1, ctrl)
	waitFor(t, "the subscription to be persisted", func() bool { return slices.Equal(st.ids(), []uint32{subID}) })

	// --- restart: the session closes with the bridge, the record stays ----
	run1.stop()
	if got := st.ids(); !slices.Equal(got, []uint32{subID}) {
		t.Fatalf("after Stop the store holds %v — a session close must not forget the subscription", got)
	}

	resolverCalls := make(chan [8]byte, 2)
	run2 := startResumeDevice(t, st, func(br *Bridge, sessions *operational.Manager) {
		mrpWiring(br)
		br.AttachCaseInitiatorProvider(resumeInitiatorProvider(fab, sessions))
		br.AttachOperationalResolver(staticResolver{addr: ctrl.addr, calls: resolverCalls})
	})
	ctrl.setBridge(run2.addr(t))
	if n := run2.br.FormerSubscriptionCount(); n != 1 {
		t.Fatalf("former subscriptions after Start = %d, want 1", n)
	}
	if got := st.ids(); len(got) != 0 {
		t.Fatalf("Start must begin the run's record from empty, store holds %v", got)
	}

	res := run2.br.ReestablishFormerSubscriptions(context.Background())
	if res.Former != 1 || !slices.Equal(res.Reestablished, []uint32{subID}) {
		t.Fatalf("ReestablishFormerSubscriptions = %+v, want subscription %#x back", res, subID)
	}
	if cfid := <-resolverCalls; cfid != fab.cfid {
		t.Fatalf("resolver asked for fabric %x", cfid)
	}

	// The priming report arrived on the NEW session under the OLD id.
	priming := ctrl.nextReport(t, time.Second)
	if priming.subID != subID || priming.sessionID != resumeCtrlSIDRun2 || priming.attrs == 0 {
		t.Fatalf("priming after restart = %+v, want id %#x on session %#x with data", priming, subID, resumeCtrlSIDRun2)
	}
	select {
	case id := <-ctrl.subResp:
		t.Fatalf("a re-established subscription must not send a SubscribeResponse (got %#x)", id)
	default:
	}
	if got, err := run2.subs.Get(subID); err != nil || got.SessionID == 0 {
		t.Fatalf("subscription %#x not live in the new manager: %v", subID, err)
	}
	if got := st.ids(); !slices.Equal(got, []uint32{subID}) {
		t.Fatalf("re-established subscription not recorded again: store %v", got)
	}

	// Ongoing reports flow on the new session under the old id.
	run2.subs.OnAttributeChanged(accessControlACLPath())
	ongoing := ctrl.nextReport(t, 2*time.Second)
	if ongoing.subID != subID || ongoing.sessionID != resumeCtrlSIDRun2 {
		t.Fatalf("ongoing report = %+v, want id %#x on the new session", ongoing, subID)
	}
}

// TestSubscriptionResumption_InvalidSubscriptionDropsTheRecord: a
// controller that no longer knows the subscription answers the priming
// report with InvalidSubscription; the subscription is not re-created and
// not recorded again (matter.js drops it).
func TestSubscriptionResumption_InvalidSubscriptionDropsTheRecord(t *testing.T) {
	fab := newResumeFabric(t)
	st := newMemSubscriptionStore()
	ctrl := newFakeController(t, fab)
	run1 := startResumeDevice(t, st, nil)
	ctrl.setBridge(run1.addr(t))
	subID := subscribeOverCASE(t, run1, ctrl)
	waitFor(t, "persist", func() bool { return len(st.ids()) == 1 })
	run1.stop()

	ctrl.mu.Lock()
	ctrl.answer = im.StatusInvalidSubscription
	ctrl.mu.Unlock()
	run2 := startResumeDevice(t, st, func(br *Bridge, sessions *operational.Manager) {
		br.AttachCaseInitiatorProvider(resumeInitiatorProvider(fab, sessions))
		br.AttachOperationalResolver(staticResolver{addr: ctrl.addr})
	})
	ctrl.setBridge(run2.addr(t))
	res := run2.br.ReestablishFormerSubscriptions(context.Background())
	if len(res.Reestablished) != 0 {
		t.Fatalf("re-established %v despite InvalidSubscription", res.Reestablished)
	}
	if _, err := run2.subs.Get(subID); err == nil {
		t.Fatal("rejected subscription still live")
	}
	if got := st.ids(); len(got) != 0 {
		t.Fatalf("rejected subscription recorded again: %v", got)
	}
}

// TestSubscriptionResumption_UnreachablePeerIsDropped: no initiator, no
// resolver — or a resolver that never answers — costs the peer its former
// subscriptions, within the 2 s budget, and nothing else.
func TestSubscriptionResumption_UnreachablePeerIsDropped(t *testing.T) {
	st := newMemSubscriptionStore()
	rec := subscription.PeerSubscription{
		SubscriptionID: 0x55, FabricIndex: 1, PeerNodeID: 0xC0,
		AttributeRequests: []im.ConcreteAttributePath{accessControlACLPath()},
		MinIntervalFloor:  1, MaxIntervalCeiling: 60, MaxInterval: 60, SendInterval: 48 * time.Second,
	}
	payload, err := subscription.MarshalPeerSubscription(rec)
	if err != nil {
		t.Fatal(err)
	}
	_ = st.SaveServerSubscription(context.Background(), rec.SubscriptionID, 1, 0xC0, payload)

	dev := startResumeDevice(t, st, nil) // nothing to reach the peer with
	start := time.Now()
	res := dev.br.ReestablishFormerSubscriptions(context.Background())
	if res.Former != 1 || len(res.Reestablished) != 0 {
		t.Fatalf("result = %+v", res)
	}
	if elapsed := time.Since(start); elapsed > subscription.ReestablishTimeout+time.Second {
		t.Fatalf("giving up took %s", elapsed)
	}
	if again := dev.br.ReestablishFormerSubscriptions(context.Background()); again.Former != 0 {
		t.Fatal("the former set must be consumed by the first call")
	}
	if got := st.ids(); len(got) != 0 {
		t.Fatalf("an unreachable peer's subscription must not be recorded again: %v", got)
	}
}

// TestSubscriptionResumption_PeerThatSubscribesIsSkipped: a peer that
// begins a normal subscribe while re-establishment runs is left alone
// (matter.js subscriptionEstablishmentStarted block-list). Two halves: a
// real SubscribeRequest lands the peer on the block list, and a blocked
// peer is never dialled.
func TestSubscriptionResumption_PeerThatSubscribesIsSkipped(t *testing.T) {
	fab := newResumeFabric(t)
	st := newMemSubscriptionStore()
	ctrl := newFakeController(t, fab)
	dialled := make(chan struct{}, 1)
	dev := startResumeDevice(t, st, func(br *Bridge, _ *operational.Manager) {
		br.AttachCaseInitiatorProvider(func(context.Context, uint8, uint64) (*CaseInitiation, error) {
			dialled <- struct{}{}
			return nil, errors.New("must not be reached")
		})
		br.AttachOperationalResolver(staticResolver{addr: ctrl.addr})
	})
	ctrl.setBridge(dev.addr(t))

	// Half 1: while a re-establishment is in flight, a normal subscribe
	// from the peer blocks it.
	block := &peerBlockList{peers: make(map[peerKey]struct{})}
	dev.br.resumption.mu.Lock()
	dev.br.resumption.block = block
	dev.br.resumption.mu.Unlock()
	_ = subscribeOverCASE(t, dev, ctrl)
	peer := peerKey{fabricIndex: resumeFabricIndex, nodeID: resumeCtrlNode}
	if !block.has(peer) {
		t.Fatal("a SubscribeRequest from the peer did not block its re-establishment")
	}

	// Half 2: a blocked peer is skipped without being dialled.
	recs := []subscription.PeerSubscription{{
		SubscriptionID: 0x77, FabricIndex: resumeFabricIndex, PeerNodeID: resumeCtrlNode,
		AttributeRequests: []im.ConcreteAttributePath{accessControlACLPath()}, MinIntervalFloor: 1, MaxIntervalCeiling: 60, MaxInterval: 60,
	}}
	if ids := dev.br.reestablishPeer(context.Background(), peer, recs, block); len(ids) != 0 {
		t.Fatalf("blocked peer re-established %v", ids)
	}
	select {
	case <-dialled:
		t.Fatal("a blocked peer was dialled")
	default:
	}
}

// TestSubscriptionResumption_DisabledRecordsNothing: the
// persistenceEnabled=false switch.
func TestSubscriptionResumption_DisabledRecordsNothing(t *testing.T) {
	fab := newResumeFabric(t)
	st := newMemSubscriptionStore()
	ctrl := newFakeController(t, fab)
	run1 := startResumeDevice(t, st, func(br *Bridge, _ *operational.Manager) { br.SetSubscriptionPersistence(false) })
	if run1.br.SubscriptionPersistenceEnabled() {
		t.Fatal("switch did not take")
	}
	ctrl.setBridge(run1.addr(t))
	_ = subscribeOverCASE(t, run1, ctrl)
	time.Sleep(50 * time.Millisecond)
	if got := st.ids(); len(got) != 0 {
		t.Fatalf("persistence disabled, store holds %v", got)
	}
}

// TestSubscriptionResumption_FabricRemovalForgetsRows: removing a fabric
// deletes its persisted subscriptions at once.
func TestSubscriptionResumption_FabricRemovalForgetsRows(t *testing.T) {
	fab := newResumeFabric(t)
	st := newMemSubscriptionStore()
	ctrl := newFakeController(t, fab)
	run1 := startResumeDevice(t, st, nil)
	ctrl.setBridge(run1.addr(t))
	_ = subscribeOverCASE(t, run1, ctrl)
	waitFor(t, "persist", func() bool { return len(st.ids()) == 1 })
	run1.br.EmitFabricRemoved(resumeFabricIndex)
	if got := st.ids(); len(got) != 0 {
		t.Fatalf("fabric removed, store still holds %v", got)
	}
}

// TestSubscriptionResumption_PeerReplacingSubscriptionForgetsIt: a
// KeepSubscriptions=false subscribe from the same peer terminates the old
// subscription, which is forgotten; the new one is recorded.
func TestSubscriptionResumption_PeerReplacingSubscriptionForgetsIt(t *testing.T) {
	fab := newResumeFabric(t)
	st := newMemSubscriptionStore()
	ctrl := newFakeController(t, fab)
	run1 := startResumeDevice(t, st, nil)
	ctrl.setBridge(run1.addr(t))
	first := subscribeOverCASE(t, run1, ctrl)
	waitFor(t, "persist", func() bool { return slices.Equal(st.ids(), []uint32{first}) })

	ctrl.subscribe(im.SubscribeRequest{
		KeepSubscriptions:  false,
		MaxIntervalCeiling: 60,
		AttributeRequests:  []im.ConcreteAttributePath{accessControlACLPath()},
	})
	second := ctrl.nextReport(t, 3*time.Second).subID
	<-ctrl.subResp
	waitFor(t, "the replacement to be recorded alone", func() bool { return slices.Equal(st.ids(), []uint32{second}) })
	if second == first {
		t.Fatal("replacement reused the id")
	}
}

// TestSubscriptionResumption_SilentPeerStopsThatPeersLoop: a priming
// report that goes unanswered is a transport failure, and the peer's
// remaining former subscriptions are not attempted (matter.js breaks the
// loop on TransientPeerCommunicationError / NoResponseTimeoutError).
func TestSubscriptionResumption_SilentPeerStopsThatPeersLoop(t *testing.T) {
	fab := newResumeFabric(t)
	st := newMemSubscriptionStore()
	for _, id := range []uint32{0x101, 0x102} {
		rec := subscription.PeerSubscription{
			SubscriptionID: id, FabricIndex: resumeFabricIndex, PeerNodeID: resumeCtrlNode,
			AttributeRequests: []im.ConcreteAttributePath{accessControlACLPath()},
			MinIntervalFloor:  1, MaxIntervalCeiling: 60, MaxInterval: 60, SendInterval: 48 * time.Second,
		}
		payload, err := subscription.MarshalPeerSubscription(rec)
		if err != nil {
			t.Fatal(err)
		}
		_ = st.SaveServerSubscription(context.Background(), id, resumeFabricIndex, resumeCtrlNode, payload)
	}
	ctrl := newFakeController(t, fab)
	ctrl.mu.Lock()
	ctrl.answer = silentAnswer
	ctrl.mu.Unlock()
	dev := startResumeDevice(t, st, func(br *Bridge, sessions *operational.Manager) {
		br.chunkStatusResponseTimeoutOverride = 200 * time.Millisecond
		br.AttachCaseInitiatorProvider(resumeInitiatorProvider(fab, sessions))
		br.AttachOperationalResolver(staticResolver{addr: ctrl.addr})
	})
	ctrl.setBridge(dev.addr(t))
	res := dev.br.ReestablishFormerSubscriptions(context.Background())
	if len(res.Reestablished) != 0 || res.Former != 2 {
		t.Fatalf("result = %+v", res)
	}
	_ = ctrl.nextReport(t, time.Second)
	select {
	case r := <-ctrl.reports:
		t.Fatalf("second priming report %+v sent after the peer went silent", r)
	case <-time.After(300 * time.Millisecond):
	}
	if dev.subs.Active() != 0 || len(st.ids()) != 0 {
		t.Fatalf("active=%d stored=%v, want nothing", dev.subs.Active(), st.ids())
	}
}
