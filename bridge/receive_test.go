// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

// White-box tests for Bridge.dispatch and AttachSessionLookup.
// This file lives in package bridge (not bridge_test) so it can call
// the unexported dispatch method and construct a bare Bridge struct
// directly.

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// ─── in-memory store fake ─────────────────────────────────────────────
// White-box tests share [FakeStore] with bridge_test.go via
// fakestore_helpers_test.go.

// ─── session lookup fake ─────────────────────────────────────────────────

// wbFakeSessionLookup implements SessionLookup for tests.
type wbFakeSessionLookup struct {
	session *channel.Session
	found   bool
}

func (f wbFakeSessionLookup) Lookup(_ uint16) (*channel.Session, bool) {
	return f.session, f.found
}

// ─── snapshotter ─────────────────────────────────────────────────────────

// wbEmptySnapshotter yields a topology with no bridged endpoints. A fresh
// assembler per call keeps the parallel tests that share this function
// off one another's endpoint-id store.
func wbEmptySnapshotter(ctx context.Context) (*endpoint.Topology, error) {
	return NewEmptySnapshotter()(ctx)
}

// ─── helpers ─────────────────────────────────────────────────────────────

// newStartedBridge constructs and starts a Bridge suitable for white-box
// dispatch testing.
func newStartedBridge(t *testing.T) *Bridge {
	t.Helper()
	b, err := New(
		wbEmptySnapshotter,
		mdns.NewNoop(),
		Config{
			Listen:    ":0",
			VendorID:  0x1234,
			ProductID: 0x5678,
			NodeLabel: "wb-test",
		},
		nil,
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
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
	return b
}

// buildHeader encodes a minimal Matter message header with the given
// SessionID and MessageCounter (no source/dest node IDs).
func buildHeader(sessionID uint16, msgCounter uint32) []byte {
	return message.Header{
		SessionID:      sessionID,
		MessageCounter: msgCounter,
	}.Marshal()
}

// buildProtocolHeader encodes a ProtocolHeader with the given ProtocolID
// and Opcode. ExchangeID is fixed at 1.
func buildProtocolHeader(protocolID uint16, opcode uint8) []byte {
	return message.ProtocolHeader{
		ProtocolID: protocolID,
		Opcode:     opcode,
		ExchangeID: 1,
	}.Marshal()
}

// buildIMReadRequestPayload returns a valid minimal TLV-encoded
// ReadRequestMessage (empty AttributeRequests, FabricFiltered=false).
func buildIMReadRequestPayload(t *testing.T) []byte {
	t.Helper()
	enc := tlv.NewEncoder()
	im.ReadRequest{}.MarshalTLV(enc)
	b, err := enc.Bytes()
	if err != nil {
		t.Fatalf("buildIMReadRequestPayload: %v", err)
	}
	return b
}

// buildIMSubscribeRequestPayload returns a valid minimal TLV-encoded
// SubscribeRequestMessage.
func buildIMSubscribeRequestPayload(t *testing.T) []byte {
	t.Helper()
	enc := tlv.NewEncoder()
	im.SubscribeRequest{
		MinIntervalFloor:   0,
		MaxIntervalCeiling: 60,
	}.MarshalTLV(enc)
	b, err := enc.Bytes()
	if err != nil {
		t.Fatalf("buildIMSubscribeRequestPayload: %v", err)
	}
	return b
}

// buildDatagram concatenates a message header, a protocol header, and a
// payload into a single wire buffer as dispatch expects.
func buildDatagram(hdr, protoHdr, payload []byte) []byte {
	out := make([]byte, 0, len(hdr)+len(protoHdr)+len(payload))
	out = append(out, hdr...)
	out = append(out, protoHdr...)
	out = append(out, payload...)
	return out
}

// loopbackSrc returns a non-nil UDP source address suitable for tests
// that exercise the reply-send path. Loopback IPv4 + an arbitrary port;
// the listener Send call goes to the OS but the test does not assert
// receipt — only that the reply path constructs without error.
func loopbackSrc() *net.UDPAddr {
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 65000}
}

// paseTestSessionID is the secure session the IM routing tests seal their
// requests on: the Interaction Model runs on secure sessions only
// (classifyIMOpcode drops session 0), and a PASE session keeps the
// commissioning channel's implicit Administer grant, so the requests
// reach the handlers as the former session-0 datagrams did.
const paseTestSessionID uint16 = 0x0F0F

// paseTestLookup is a [SessionLookup] and [SessionPASEResolver] holding
// one PASE session; peer is the peer's side of it, which seals the
// test's requests and opens the bridge's replies.
type paseTestLookup struct {
	id   uint16
	sess *channel.Session
	peer *channel.Session
}

func (l paseTestLookup) Lookup(id uint16) (*channel.Session, bool) {
	if id == l.id {
		return l.sess, true
	}
	return nil, false
}

func (l paseTestLookup) IsPASE(id uint16) (pase, ok bool) { return id == l.id, id == l.id }

// newPASETestSessions builds the bridge's and the peer's side of a
// session with fixed keys and returns the bridge's lookup and the peer's
// session.
func newPASETestSessions(t *testing.T) (paseTestLookup, *channel.Session) {
	t.Helper()
	bridgeKey := []byte("go-fabric-bridge")
	peerKey := []byte("go-fabric-peer-k")
	const (
		bridgeNodeID uint64 = 0xAAAA5555
		peerNodeID   uint64 = 0xBBBB6666
	)
	bridgeSess, err := channel.New(channel.Config{EncryptKey: bridgeKey, DecryptKey: peerKey, LocalNodeID: bridgeNodeID, PeerNodeID: peerNodeID, InitialCounter: 900})
	if err != nil {
		t.Fatalf("bridge session: %v", err)
	}
	peerSess, err := channel.New(channel.Config{EncryptKey: peerKey, DecryptKey: bridgeKey, LocalNodeID: peerNodeID, PeerNodeID: bridgeNodeID, InitialCounter: 1000})
	if err != nil {
		t.Fatalf("peer session: %v", err)
	}
	return paseTestLookup{id: paseTestSessionID, sess: bridgeSess, peer: peerSess}, peerSess
}

// attachPASETestPeer attaches a PASE session to b — or reuses the one
// already attached — and returns the peer's side of it.
func attachPASETestPeer(t *testing.T, b *Bridge) *channel.Session {
	t.Helper()
	b.mu.RLock()
	current, ok := b.sessions.(paseTestLookup)
	b.mu.RUnlock()
	if ok {
		return current.peer
	}
	lookup, peerSess := newPASETestSessions(t)
	b.AttachSessionLookup(lookup)
	return peerSess
}

// secureTestDatagram seals an IM request for b on its PASE test session,
// attaching the session on first use.
func secureTestDatagram(t *testing.T, b *Bridge, proto, payload []byte) []byte {
	t.Helper()
	return sealTestDatagram(t, attachPASETestPeer(t, b), proto, payload)
}

// openTestReplyFor returns the reply datagram got with its body
// decrypted when b runs the PASE test session, and got unchanged
// otherwise — the header-then-protocol-header decoders of the tests read
// either.
func openTestReplyFor(t *testing.T, b *Bridge, got []byte) []byte {
	t.Helper()
	b.mu.RLock()
	current, ok := b.sessions.(paseTestLookup)
	b.mu.RUnlock()
	if !ok {
		return got
	}
	return openTestReply(t, current.peer, got)
}

// sealTestDatagram seals a protocol header and payload from the peer on
// the PASE test session and returns the wire datagram.
func sealTestDatagram(t *testing.T, peerSess *channel.Session, proto, payload []byte) []byte {
	t.Helper()
	hdr := message.Header{SessionID: paseTestSessionID}
	body := append(append([]byte{}, proto...), payload...)
	enc, err := peerSess.Encrypt(&hdr, securityFlagsByte(&hdr), body)
	if err != nil {
		t.Fatalf("peer Encrypt: %v", err)
	}
	return append(hdr.Marshal(), enc.Ciphertext...)
}

// openTestReply decrypts a datagram the bridge sealed for the peer and
// returns it with the plaintext body: the message header, the protocol
// header and the payload.
func openTestReply(t *testing.T, peerSess *channel.Session, got []byte) []byte {
	t.Helper()
	hdr, hdrLen, err := message.UnmarshalHeader(got)
	if err != nil {
		t.Fatalf("UnmarshalHeader: %v", err)
	}
	plain, _, err := peerSess.Decrypt(&hdr, securityFlagsByte(&hdr), got[hdrLen:])
	if err != nil {
		t.Fatalf("peer Decrypt: %v", err)
	}
	return append(append([]byte{}, got[:hdrLen]...), plain...)
}

// ─── header decode failures ──────────────────────────────────────────────

// TestDispatch_TooShortBuffer verifies that a 2-byte buffer triggers a
// header-decode error from dispatch.
func TestDispatch_TooShortBuffer(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	err := b.dispatch(context.Background(), []byte{0x00, 0x01}, nil)
	if err == nil {
		t.Fatal("expected non-nil error for truncated header, got nil")
	}
}

// TestDispatch_NilSrcDoesNotPanic verifies that a nil src address is
// handled without panicking; any return value is acceptable.
func TestDispatch_NilSrcDoesNotPanic(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	hdr := buildHeader(0, 0)
	proto := buildProtocolHeader(im.InteractionModelProtocolID, im.OpcodeReadRequest)
	payload := buildIMReadRequestPayload(t)
	buf := buildDatagram(hdr, proto, payload)
	// Must not panic — return value intentionally ignored.
	_ = b.dispatch(context.Background(), buf, nil)
}

// ─── SessionID==0 (unsecured) routing ────────────────────────────────────

// TestDispatch_SecureChannelOpcode verifies that a SecureChannel datagram
// (ProtocolID=0x0000) returns nil error (handled by the stub).
func TestDispatch_SecureChannelOpcode(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	// Opcode 0x10 is StandaloneAck in the SecureChannel protocol.
	hdr := buildHeader(0, 1)
	proto := buildProtocolHeader(mrp.SecureChannelProtocolID, 0x10)
	// Non-empty payload so the pipeline does not early-return at the
	// empty-body path that silently drops standalone-ACK frames.
	payload := []byte{0xFF}
	buf := buildDatagram(hdr, proto, payload)
	err := b.dispatch(context.Background(), buf, nil)
	if err != nil {
		t.Errorf("expected nil from SecureChannel opcode, got %v", err)
	}
}

// TestDispatch_UnknownProtocolID verifies that an unknown ProtocolID
// surfaces ErrUnknownProtocol.
func TestDispatch_UnknownProtocolID(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	hdr := buildHeader(0, 2)
	proto := buildProtocolHeader(0x9999, 0x01)
	payload := []byte{0xAB}
	buf := buildDatagram(hdr, proto, payload)
	err := b.dispatch(context.Background(), buf, nil)
	if !errors.Is(err, ErrUnknownProtocol) {
		t.Errorf("want ErrUnknownProtocol, got %v", err)
	}
}

// TestDispatch_VendorQualifiedProtocolCollisionRejected verifies that a
// datagram whose protocol header carries a non-zero vendor id is rejected
// with ErrUnknownProtocol even when its low 16-bit ProtocolID collides
// with SecureChannel (0x0000) — a vendor-specific protocol must never
// route into the Common-vendor PASE/IM handlers just because the low bits
// match. Mirrors matter.js MessageCodec.ts:377, which keys dispatch on
// the full 32-bit (vendorId*0x10000 + protocolId) identifier. Also
// confirms the PASE handler that a naive ProtocolID-only switch would
// have invoked never sees the forged frame.
func TestDispatch_VendorQualifiedProtocolCollisionRejected(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	h := &recordingPaseHandler{}
	b.AttachPaseHandler(h)

	hdr := buildHeader(0, 10)
	proto := message.ProtocolHeader{
		HasVendorID: true,
		VendorID:    0xFFF1,
		ProtocolID:  mrp.SecureChannelProtocolID, // 0x0000 — collides with SecureChannel
		Opcode:      mrp.SCOpcodePake1,
		ExchangeID:  1,
	}.Marshal()
	payload := []byte{0x01}
	buf := buildDatagram(hdr, proto, payload)

	err := b.dispatch(context.Background(), buf, nil)
	if !errors.Is(err, ErrUnknownProtocol) {
		t.Errorf("want ErrUnknownProtocol, got %v", err)
	}
	if got := h.pake1Calls.Load(); got != 0 {
		t.Errorf("pake1Calls = %d, want 0 — vendor-qualified protocol must not reach the PASE handler", got)
	}
}

// TestDispatch_VendorIDZeroStillRoutesNormally verifies the boundary of
// the vendor-qualified guard: HasVendorID=true with VendorID=0x0000 (the
// Common vendor id) is NOT treated as vendor-specific — [Bridge.dispatch]
// only rejects a non-zero vendor id. A datagram shaped this way must
// still route into the IM handler as usual.
func TestDispatch_VendorIDZeroStillRoutesNormally(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	proto := message.ProtocolHeader{
		HasVendorID: true,
		VendorID:    0,
		ProtocolID:  im.InteractionModelProtocolID,
		Opcode:      im.OpcodeReadRequest,
		ExchangeID:  1,
	}.Marshal()
	payload := buildIMReadRequestPayload(t)
	buf := secureTestDatagram(t, b, proto, payload)
	if err := b.dispatch(context.Background(), buf, loopbackSrc()); err != nil {
		t.Errorf("expected nil for VendorID=0 (Common vendor), got %v", err)
	}
}

// ─── SessionID != 0 (encrypted) routing ──────────────────────────────────

// TestDispatch_SessionMissingSurfaces verifies that a non-zero SessionID
// without a registered session returns ErrSessionMissing.
func TestDispatch_SessionMissingSurfaces(t *testing.T) {
	t.Parallel()
	// Bridge without any attached session lookup → noopSessionLookup.
	b := newStartedBridge(t)
	hdr := buildHeader(42, 3)
	proto := buildProtocolHeader(im.InteractionModelProtocolID, im.OpcodeReadRequest)
	payload := buildIMReadRequestPayload(t)
	buf := buildDatagram(hdr, proto, payload)
	err := b.dispatch(context.Background(), buf, nil)
	if !errors.Is(err, ErrSessionMissing) {
		t.Errorf("want ErrSessionMissing, got %v", err)
	}
}

// TestAttachSessionLookup_NilRevertsToNoop verifies that attaching a
// lookup then reverting via AttachSessionLookup(nil) causes the next
// encrypted dispatch to surface ErrSessionMissing.
func TestAttachSessionLookup_NilRevertsToNoop(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	// Attach a real lookup (always misses), then revert to nil.
	b.AttachSessionLookup(wbFakeSessionLookup{found: false})
	b.AttachSessionLookup(nil)

	hdr := buildHeader(42, 4)
	proto := buildProtocolHeader(im.InteractionModelProtocolID, im.OpcodeReadRequest)
	payload := buildIMReadRequestPayload(t)
	buf := buildDatagram(hdr, proto, payload)
	err := b.dispatch(context.Background(), buf, nil)
	if !errors.Is(err, ErrSessionMissing) {
		t.Errorf("want ErrSessionMissing after nil revert, got %v", err)
	}
}

// ─── IM opcode routing (secure PASE session; session 0 is dropped) ──────

// TestDispatch_IMReadRoutes verifies that a valid IM ReadRequest on the
// unsecured session 0 is dropped before decode — ErrUnsecuredIM, no
// reply — and that the same request on a secure session is dispatched
// without error and the reply-send path runs. Mirrors matter.js
// InteractionServer.ts:161 (`requiresSecureSession = true`) and
// ExchangeManager.ts:391-412 (a security mismatch opens no exchange).
func TestDispatch_IMReadRoutes(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	peerConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	t.Cleanup(func() { _ = peerConn.Close() })
	peerAddr, ok := peerConn.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatalf("unexpected peer addr type %T", peerConn.LocalAddr())
	}
	proto := buildProtocolHeader(im.InteractionModelProtocolID, im.OpcodeReadRequest)
	payload := buildIMReadRequestPayload(t)

	unsecured := buildDatagram(buildHeader(0, 5), proto, payload)
	if err := b.dispatch(context.Background(), unsecured, peerAddr); !errors.Is(err, ErrUnsecuredIM) {
		t.Errorf("ReadRequest on session 0: err = %v, want ErrUnsecuredIM", err)
	}
	_ = peerConn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	rbuf := make([]byte, 1500)
	if n, _, err := peerConn.ReadFromUDP(rbuf); err == nil {
		t.Errorf("ReadRequest on session 0 was answered (%d-byte datagram); want it dropped", n)
	}

	secure := secureTestDatagram(t, b, proto, payload)
	if err := b.dispatch(context.Background(), secure, loopbackSrc()); err != nil {
		t.Errorf("expected nil for valid IM ReadRequest on a secure session, got %v", err)
	}
}

// TestDispatch_IMResponseOpcodeIsUnsupported verifies that a response
// opcode (ReportData=0x05) surfaces ErrUnsupportedOpcode because the
// bridge is never the read initiator in v1.1.
func TestDispatch_IMResponseOpcodeIsUnsupported(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	proto := buildProtocolHeader(im.InteractionModelProtocolID, im.OpcodeReportData)
	payload := []byte{0x01}
	buf := secureTestDatagram(t, b, proto, payload)
	err := b.dispatch(context.Background(), buf, nil)
	if !errors.Is(err, ErrUnsupportedOpcode) {
		t.Errorf("want ErrUnsupportedOpcode for ReportData opcode, got %v", err)
	}
}

// TestDispatch_IMSubscribeReplies verifies that a SubscribeRequest is
// decoded AND replied to at the dispatch() → handleSubscribeRequest
// routing boundary. buildIMSubscribeRequestPayload builds an empty
// request (no AttributeRequests, no EventRequests), which the
// matched-path gate in handleSubscribeRequest (subscribe.go) rejects
// with a top-level StatusResponse(InvalidAction) rather than
// establishing — matter.js InteractionServer.ts:628-633 ("No
// attributes or events requested"). A Subscribe naming a real matching
// path establishes normally instead; see
// TestHandleSubscribeRequest_MatchingPath_EstablishesAndReplies
// (subscribe_establish_test.go) for that positive-control case, and
// TestHandleSubscribeRequest_EmptyRequest_RejectsInvalidAction
// (subscribe_reject_test.go) for the same rejection driven directly
// against handleSubscribeRequest rather than through the wire-decode
// path exercised here.
func TestDispatch_IMSubscribeReplies(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)

	peerConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	t.Cleanup(func() { _ = peerConn.Close() })
	peerAddr, ok := peerConn.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatalf("unexpected peer addr type %T", peerConn.LocalAddr())
	}

	proto := buildProtocolHeader(im.InteractionModelProtocolID, im.OpcodeSubscribeRequest)
	payload := buildIMSubscribeRequestPayload(t)
	buf := secureTestDatagram(t, b, proto, payload)
	if err := b.dispatch(context.Background(), buf, peerAddr); err != nil {
		t.Errorf("expected nil for SubscribeRequest, got %v", err)
	}

	_ = peerConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	rbuf := make([]byte, 1500)
	n, _, err := peerConn.ReadFromUDP(rbuf)
	if err != nil {
		t.Fatalf("ReadFromUDP: %v", err)
	}
	got := openTestReplyFor(t, b, rbuf[:n])
	_, hdrLen, err := message.UnmarshalHeader(got)
	if err != nil {
		t.Fatalf("UnmarshalHeader: %v", err)
	}
	rproto, protoLen, err := message.UnmarshalProtocolHeader(got[hdrLen:])
	if err != nil {
		t.Fatalf("UnmarshalProtocolHeader: %v", err)
	}
	if rproto.Opcode != im.OpcodeStatusResponse {
		t.Fatalf("reply opcode = 0x%02X, want StatusResponse (0x%02X) — an empty Subscribe must be rejected, not established", rproto.Opcode, im.OpcodeStatusResponse)
	}
	if status := decodeStatusResponseCode(t, got[hdrLen+protoLen:]); status != im.StatusInvalidAction {
		t.Errorf("StatusResponse status = %v, want StatusInvalidAction (0x80)", status)
	}
}

// TestDispatch_IMInvalidOpcode verifies that an unknown IM opcode (0xFF)
// surfaces ErrUnsupportedOpcode.
func TestDispatch_IMInvalidOpcode(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	proto := buildProtocolHeader(im.InteractionModelProtocolID, 0xFF)
	payload := []byte{0x00}
	buf := secureTestDatagram(t, b, proto, payload)
	err := b.dispatch(context.Background(), buf, nil)
	if !errors.Is(err, ErrUnsupportedOpcode) {
		t.Errorf("want ErrUnsupportedOpcode for opcode 0xFF, got %v", err)
	}
}

// TestDispatch_NoDispatcherDropsSilently verifies that when the bridge
// has no assembled dispatcher (constructed bare without Start), dispatch
// returns nil for a valid IM ReadRequest — the datagram is silently
// dropped per the "commissioner will retry via MRP" design note.
func TestDispatch_NoDispatcherDropsSilently(t *testing.T) {
	t.Parallel()
	// White-box: construct only the fields dispatch touches when
	// dispatcher is nil. No listener, no mutex initialisation needed —
	// sync.RWMutex zero value is valid.
	b := &Bridge{
		logger:   slog.Default(),
		sessions: noopSessionLookup{},
	}
	proto := buildProtocolHeader(im.InteractionModelProtocolID, im.OpcodeReadRequest)
	payload := buildIMReadRequestPayload(t)
	buf := secureTestDatagram(t, b, proto, payload)
	if err := b.dispatch(context.Background(), buf, nil); err != nil {
		t.Errorf("expected nil when dispatcher is nil, got %v", err)
	}
}

// ─── checkTimedGate unit tests ───────────────────────────────────────────

// TestCheckTimedGate_UntimedRequestNoPriorProceeds verifies that a
// plain untimed Write/Invoke (TimedRequest flag clear, no preceding
// TimedRequest) proceeds.
func TestCheckTimedGate_UntimedRequestNoPriorProceeds(t *testing.T) {
	t.Parallel()
	b := &Bridge{}
	if status, gated := b.checkTimedGate(false, 1, 7); gated {
		t.Errorf("untimed/no-prior: gated=true, want false (status=%v)", status)
	}
}

// TestCheckTimedGate_UntimedRequestWithPriorRejects verifies that an
// untimed Write/Invoke (TimedRequest flag clear) that nonetheless
// follows a TimedRequest is rejected with TIMED_REQUEST_MISMATCH
// (0xC9) — the request's own flag disagrees with the preceding
// TimedRequest, which the spec forbids in both directions. The stale
// deadline is consumed on rejection.
// Mirrors chip src/app/WriteHandler.cpp:669-673.
func TestCheckTimedGate_UntimedRequestWithPriorRejects(t *testing.T) {
	t.Parallel()
	b := &Bridge{}
	b.routing.timedDeadlines.Store(timedKey{sessionID: 1, exchangeID: 7}, time.Now().Add(10*time.Second))
	status, gated := b.checkTimedGate(false, 1, 7)
	if !gated {
		t.Fatal("untimed/with-prior: gated=false, want true")
	}
	if status != im.StatusTimedRequestMismatch {
		t.Errorf("status = %v, want StatusTimedRequestMismatch (0xC9)", status)
	}
	if _, ok := b.routing.timedDeadlines.Load(timedKey{sessionID: 1, exchangeID: 7}); ok {
		t.Error("untimed/with-prior: stale deadline still present after rejection")
	}
}

// TestCheckTimedGate_TimedFlagWithoutPriorRejects verifies that a
// timed Write/Invoke without a preceding TimedRequest rejects with
// TIMED_REQUEST_MISMATCH (0xC9) — the flag claims a timed interaction the
// exchange never opened (matter.js InteractionServer.ts:409-412, :945-948).
func TestCheckTimedGate_TimedFlagWithoutPriorRejects(t *testing.T) {
	t.Parallel()
	b := &Bridge{}
	status, gated := b.checkTimedGate(true, 0, 99)
	if !gated {
		t.Fatal("timed without prior: gated=false, want true")
	}
	if status != im.StatusTimedRequestMismatch {
		t.Errorf("status = %v, want StatusTimedRequestMismatch (0xC9)", status)
	}
}

// TestCheckTimedGate_ExpiredDeadlineRejects verifies that a timed
// Write/Invoke arriving after the TimedRequest deadline rejects with
// TIMEOUT (0x94) and clears the stale deadline.
func TestCheckTimedGate_ExpiredDeadlineRejects(t *testing.T) {
	t.Parallel()
	b := &Bridge{}
	b.routing.timedDeadlines.Store(timedKey{sessionID: 2, exchangeID: 11}, time.Now().Add(-1*time.Millisecond))
	status, gated := b.checkTimedGate(true, 2, 11)
	if !gated {
		t.Fatal("expired: gated=false, want true")
	}
	if status != im.StatusTimeout {
		t.Errorf("status = %v, want StatusTimeout (0x94)", status)
	}
	if _, ok := b.routing.timedDeadlines.Load(timedKey{sessionID: 2, exchangeID: 11}); ok {
		t.Error("expired: stale deadline still present after rejection")
	}
}

// TestCheckTimedGate_ValidDeadlineProceeds verifies that a timed
// Write/Invoke arriving within its TimedRequest deadline proceeds and
// the deadline is consumed (a duplicate request would re-test as
// "no prior TimedRequest").
func TestCheckTimedGate_ValidDeadlineProceeds(t *testing.T) {
	t.Parallel()
	b := &Bridge{}
	b.routing.timedDeadlines.Store(timedKey{sessionID: 3, exchangeID: 13}, time.Now().Add(10*time.Second))
	if status, gated := b.checkTimedGate(true, 3, 13); gated {
		t.Errorf("valid: gated=true, want false (status=%v)", status)
	}
	if _, ok := b.routing.timedDeadlines.Load(timedKey{sessionID: 3, exchangeID: 13}); ok {
		t.Error("valid: deadline still present after consumption")
	}
	// Re-check on the same exchange now reads as missing-prior.
	status, gated := b.checkTimedGate(true, 3, 13)
	if !gated || status != im.StatusTimedRequestMismatch {
		t.Errorf("re-check: gated=%v status=%v, want gated=true status=TimedRequestMismatch", gated, status)
	}
}

// TestTimedRequest_SessionScopeIsolation verifies that a deadline registered
// for (sessionA, exchangeID) cannot be consumed by (sessionB, same exchangeID).
// The old map[uint16]time.Time key allowed a peer on a different session to
// consume a timed gate that was registered by another session.
func TestTimedRequest_SessionScopeIsolation(t *testing.T) {
	t.Parallel()
	b := &Bridge{}
	const (
		sessionA   = uint16(10)
		sessionB   = uint16(20)
		exchangeID = uint16(42)
	)
	// Register a deadline for session A, exchange 42.
	b.routing.timedDeadlines.Store(timedKey{sessionID: sessionA, exchangeID: exchangeID}, time.Now().Add(10*time.Second))

	// Attempt to consume from session B on the same exchange-ID:
	// must hit the "no prior TimedRequest" path (TIMED_REQUEST_MISMATCH).
	status, gated := b.checkTimedGate(true, sessionB, exchangeID)
	if !gated {
		t.Fatal("session B: gated=false, want true (different session must not consume session A's deadline)")
	}
	if status != im.StatusTimedRequestMismatch {
		t.Errorf("session B: status=%v, want StatusTimedRequestMismatch", status)
	}
	// Session A's deadline must still be present — session B's miss must not
	// have cleared it.
	if _, ok := b.routing.timedDeadlines.Load(timedKey{sessionID: sessionA, exchangeID: exchangeID}); !ok {
		t.Error("session A's deadline was erroneously consumed by session B's check")
	}
	// Session A itself must succeed.
	statusA, gatedA := b.checkTimedGate(true, sessionA, exchangeID)
	if gatedA {
		t.Errorf("session A: unexpectedly gated (status=%v)", statusA)
	}
}

// ─── subscription target capture tests ───────────────────────────────────

// TestCaptureSubTarget_StoresMetadata verifies that captureSubTarget
// records every routing field the reportSubscription pump needs. Skips
// when subID==0 (no manager) or src is nil so the receiver is safe to
// call from the early-fail branches in handleSubscribeRequest.
func TestCaptureSubTarget_StoresMetadata(t *testing.T) {
	t.Parallel()
	b := &Bridge{}
	src := &net.UDPAddr{IP: net.IPv4(192, 168, 1, 5), Port: 5540}
	hdr := &message.Header{
		SessionID:       7,
		HasSourceNodeID: true,
		SourceNodeID:    0xDEADBEEFCAFEBABE,
	}
	proto := message.ProtocolHeader{
		Initiator:  true,
		ExchangeID: 42,
	}
	b.captureSubTarget(99, src, hdr, proto, false)

	raw, ok := b.routing.subTargets.Load(uint32(99))
	if !ok {
		t.Fatal("subTarget for ID 99 not stored")
	}
	target, ok := raw.(subTarget)
	if !ok {
		t.Fatalf("subTargets value type = %T, want subTarget", raw)
	}
	if target.src != src {
		t.Errorf("src = %v, want %v", target.src, src)
	}
	if target.sessionID != 7 {
		t.Errorf("sessionID = %d, want 7", target.sessionID)
	}
	if target.exchangeID != 42 {
		t.Errorf("exchangeID = %d, want 42", target.exchangeID)
	}
	if !target.hasPeerSourceNodeID || target.peerSourceNodeID != 0xDEADBEEFCAFEBABE {
		t.Errorf("peerSourceNodeID echo missing or wrong: %+v", target)
	}
	if !target.peerInitiator {
		t.Error("peerInitiator = false, want true (peer opened the Subscribe exchange)")
	}
}

// TestCaptureSubTarget_SkipsZeroOrNil verifies that the early-return
// branches don't pollute the map.
func TestCaptureSubTarget_SkipsZeroOrNil(t *testing.T) {
	t.Parallel()
	b := &Bridge{}
	src := &net.UDPAddr{IP: net.IPv4(1, 2, 3, 4), Port: 1}
	hdr := &message.Header{}
	proto := message.ProtocolHeader{}

	b.captureSubTarget(0, src, hdr, proto, false) // subID==0
	b.captureSubTarget(1, nil, hdr, proto, false) // nil src
	b.captureSubTarget(2, src, nil, proto, false) // nil hdr

	count := 0
	b.routing.subTargets.Range(func(_, _ any) bool { count++; return true })
	if count != 0 {
		t.Errorf("unexpected entries in subTargets: %d", count)
	}
}
