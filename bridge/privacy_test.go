// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/transport/message"
)

// TestPrivacy_NonPrivacyDatagramPassthrough verifies that a header
// without the P bit is left untouched: maybeUnmaskPrivacy skips a
// non-privacy frame even when no session is wired.
func TestPrivacy_NonPrivacyDatagramPassthrough(t *testing.T) {
	t.Parallel()
	hdr := message.Header{SessionID: 7, MessageCounter: 0xCAFE}
	buf := append(hdr.Marshal(), bytes.Repeat([]byte{0x42}, 32)...) // header + 16-byte body + 16-byte MIC
	orig := append([]byte(nil), buf...)

	b := &Bridge{}
	if err := b.maybeUnmaskPrivacy(buf); err != nil {
		t.Fatalf("maybeUnmaskPrivacy on non-privacy frame: %v", err)
	}
	if !bytes.Equal(buf, orig) {
		t.Errorf("non-privacy buffer mutated: got %x want %x", buf, orig)
	}
}

// TestPrivacy_OutboundInboundRoundTrip verifies that a Privacy-
// flagged datagram round-trips correctly: outbound mask + inbound
// unmask cancel out, leaving the same plaintext header. Drives the
// XOR symmetry test against a real channel.Session.
func TestPrivacy_OutboundInboundRoundTrip(t *testing.T) {
	t.Parallel()
	bridgeKey := bytes.Repeat([]byte{0xAA}, 16)
	peerKey := bytes.Repeat([]byte{0xBB}, 16)
	const sessionID uint16 = 0x1234
	bridgeSess, err := channel.New(channel.Config{
		EncryptKey:  bridgeKey,
		DecryptKey:  peerKey,
		LocalNodeID: 1,
		PeerNodeID:  2,
	})
	if err != nil {
		t.Fatalf("bridge sess: %v", err)
	}
	peerSess, err := channel.New(channel.Config{
		EncryptKey:  peerKey,
		DecryptKey:  bridgeKey,
		LocalNodeID: 2,
		PeerNodeID:  1,
	})
	if err != nil {
		t.Fatalf("peer sess: %v", err)
	}

	// Build a Privacy-flagged datagram: 4-byte header prefix +
	// 4-byte counter + 32 bytes body (16 ciphertext-ish + 16 MIC).
	// Privacy is defined for group messages, so the header carries the
	// group session type; a unicast frame with the P bit is dropped
	// before any unmasking (see TestPrivacy_UnicastWithPBitIsDropped).
	hdr := message.Header{
		SessionID:      sessionID,
		MessageCounter: 0xDEADBEEF,
		SessionType:    message.SessionGroup,
		Privacy:        true,
	}
	body := bytes.Repeat([]byte{0xCC}, 32)
	datagram := append(hdr.Marshal(), body...)

	// Snapshot the protected slice (counter bytes) BEFORE outbound
	// mask so we can verify XOR-symmetry after a roundtrip.
	originalProtected := append([]byte(nil), datagram[4:8]...)

	if err := maskGroupDatagram(bridgeSess, sessionID, datagram); err != nil {
		t.Fatalf("applyOutboundPrivacy: %v", err)
	}
	if bytes.Equal(datagram[4:8], originalProtected) {
		t.Errorf("outbound mask did not modify protected slice; mask is broken or session keys collide")
	}

	// Inbound unmask using the peer's privacy key (which equals the
	// bridge's, since both sessions share a complementary key pair).
	b := &Bridge{}
	b.sessions = sessionLookupFunc(func(id uint16) (*channel.Session, bool) {
		if id == sessionID {
			return peerSess, true
		}
		return nil, false
	})
	if err := b.maybeUnmaskPrivacy(datagram); err != nil {
		t.Fatalf("maybeUnmaskPrivacy: %v", err)
	}
	if !bytes.Equal(datagram[4:8], originalProtected) {
		t.Errorf("roundtrip did not recover original protected slice:\n got=%x\nwant=%x",
			datagram[4:8], originalProtected)
	}
}

// TestPrivacy_TooShortBuffer verifies that a buffer shorter than 4
// bytes returns nil (caller's UnmarshalHeader will catch it).
func TestPrivacy_TooShortBuffer(t *testing.T) {
	t.Parallel()
	b := &Bridge{}
	if err := b.maybeUnmaskPrivacy([]byte{0x01, 0x02}); err != nil {
		t.Errorf("short buffer: want nil, got %v", err)
	}
}

// TestPrivacy_NoSessionLookup_ReturnsError verifies that a privacy-flagged
// datagram with no session lookup wired returns an error.
func TestPrivacy_NoSessionLookup_ReturnsError(t *testing.T) {
	t.Parallel()
	// Build a datagram with the P bit set and a non-zero SessionID.
	hdr := message.Header{SessionID: 0x0001, Privacy: true}
	buf := append(hdr.Marshal(), make([]byte, 32)...)
	b := &Bridge{} // no sessions wired
	err := b.maybeUnmaskPrivacy(buf)
	if err == nil {
		t.Error("no session lookup: want error, got nil")
	}
}

// TestPrivacy_SessionNotFound_ReturnsError verifies that when the
// session lookup does not find the session ID, an error is returned.
func TestPrivacy_SessionNotFound_ReturnsError(t *testing.T) {
	t.Parallel()
	hdr := message.Header{SessionID: 0x0001, Privacy: true}
	buf := append(hdr.Marshal(), make([]byte, 32)...)
	b := &Bridge{}
	// Wires a lookup that always misses.
	b.sessions = sessionLookupFunc(func(_ uint16) (*channel.Session, bool) {
		return nil, false
	})
	err := b.maybeUnmaskPrivacy(buf)
	if err == nil {
		t.Error("session not found: want error, got nil")
	}
}

// TestPrivacy_PASESessionIDRejected verifies that a P-bit on
// SessionID=0 (PASE pre-fabric) surfaces an error per spec.
func TestPrivacy_PASESessionIDRejected(t *testing.T) {
	t.Parallel()
	hdr := message.Header{
		SessionID: 0, // PASE
		Privacy:   true,
	}
	buf := append(hdr.Marshal(), make([]byte, 32)...)
	b := &Bridge{}
	err := b.maybeUnmaskPrivacy(buf)
	if err == nil {
		t.Error("P bit on SessionID=0 should error; got nil")
	}
}

// ─── privacyHeaderEnd ────────────────────────────────────────────────────────

// TestPrivacyHeaderEnd_TooShort verifies that a buffer shorter than 4
// bytes returns its own length.
func TestPrivacyHeaderEnd_TooShort(t *testing.T) {
	t.Parallel()
	for _, n := range []int{0, 1, 3} {
		buf := make([]byte, n)
		if got := privacyHeaderEnd(buf); got != n {
			t.Errorf("len=%d: want %d, got %d", n, n, got)
		}
	}
}

// TestPrivacyHeaderEnd_NoNodeIDs returns 8 (flags + sessionID/secflags + counter).
func TestPrivacyHeaderEnd_NoNodeIDs(t *testing.T) {
	t.Parallel()
	// flags=0x00: no SourceNodeID bit, DestSize=0 → end = 4+4 = 8.
	buf := make([]byte, 20)
	buf[0] = 0x00
	if got := privacyHeaderEnd(buf); got != 8 {
		t.Errorf("no NodeIDs: want 8, got %d", got)
	}
}

// TestPrivacyHeaderEnd_WithSourceNodeID adds 8 bytes for SourceNodeID.
func TestPrivacyHeaderEnd_WithSourceNodeID(t *testing.T) {
	t.Parallel()
	// flags bit 0x04 = HasSourceNodeID → end = 4+4+8 = 16.
	buf := make([]byte, 30)
	buf[0] = 0x04
	if got := privacyHeaderEnd(buf); got != 16 {
		t.Errorf("SourceNodeID: want 16, got %d", got)
	}
}

// TestPrivacyHeaderEnd_Dest64bit adds 8 bytes for DestNodeID (flags & 0x03 == 1).
func TestPrivacyHeaderEnd_Dest64bit(t *testing.T) {
	t.Parallel()
	// flags & 0x03 == 1 → DestNodeID 64-bit (8 bytes) → end = 4+4+8 = 16.
	buf := make([]byte, 30)
	buf[0] = 0x01
	if got := privacyHeaderEnd(buf); got != 16 {
		t.Errorf("Dest64bit: want 16, got %d", got)
	}
}

// TestPrivacyHeaderEnd_Dest16bit adds 2 bytes for DestGroupID (flags & 0x03 == 2).
func TestPrivacyHeaderEnd_Dest16bit(t *testing.T) {
	t.Parallel()
	// flags & 0x03 == 2 → DestNodeID 16-bit (group, 2 bytes) → end = 4+4+2 = 10.
	buf := make([]byte, 20)
	buf[0] = 0x02
	if got := privacyHeaderEnd(buf); got != 10 {
		t.Errorf("Dest16bit: want 10, got %d", got)
	}
}

// TestPrivacyHeaderEnd_ClampedToLength verifies that if the computed
// end exceeds the actual buffer length, the function clamps to len(buf).
func TestPrivacyHeaderEnd_ClampedToLength(t *testing.T) {
	t.Parallel()
	// flags=0x04 would imply end=16, but only 10 bytes are provided.
	buf := make([]byte, 10)
	buf[0] = 0x04 // SourceNodeID flag → end would be 16 but buf is only 10
	if got := privacyHeaderEnd(buf); got != 10 {
		t.Errorf("clamped: want 10, got %d", got)
	}
}

// TestMaybeUnmaskPrivacy_ClosedSession_ReturnsError verifies that a
// Privacy-flagged inbound datagram whose session has been closed
// surfaces an error from PeerPrivacyKey. Exercises lines 67-70.
func TestMaybeUnmaskPrivacy_ClosedSession_ReturnsError(t *testing.T) {
	t.Parallel()
	bridgeKey := bytes.Repeat([]byte{0x11}, 16)
	peerKey := bytes.Repeat([]byte{0x22}, 16)
	const sessionID uint16 = 0x0007
	sess, err := channel.New(channel.Config{
		EncryptKey:  bridgeKey,
		DecryptKey:  peerKey,
		LocalNodeID: 7,
		PeerNodeID:  8,
	})
	if err != nil {
		t.Fatalf("channel.New: %v", err)
	}
	sess.Close()

	// Build a Privacy-flagged datagram (P bit in secFlags / byte 3)
	// with enough bytes to pass the MIC-tail check.
	hdr := message.Header{SessionID: sessionID, Privacy: true}
	buf := append(hdr.Marshal(), make([]byte, 32)...)

	b := &Bridge{}
	b.sessions = sessionLookupFunc(func(id uint16) (*channel.Session, bool) {
		if id == sessionID {
			return sess, true
		}
		return nil, false
	})
	if err := b.maybeUnmaskPrivacy(buf); err == nil {
		t.Error("maybeUnmaskPrivacy with closed session: want error, got nil")
	}
}

// TestMaybeUnmaskPrivacy_TooShortForMIC_AfterSessionResolve verifies that
// a Privacy-flagged datagram that is long enough to have the P-bit checked
// but too short for the MIC tail check (len < 4+privacyMICSuffix) returns
// an error. Exercises lines 75-77.
func TestMaybeUnmaskPrivacy_TooShortForMIC_AfterSessionResolve(t *testing.T) {
	t.Parallel()
	bridgeKey := bytes.Repeat([]byte{0x33}, 16)
	peerKey := bytes.Repeat([]byte{0x44}, 16)
	const sessionID uint16 = 0x0009
	sess, err := channel.New(channel.Config{
		EncryptKey:  bridgeKey,
		DecryptKey:  peerKey,
		LocalNodeID: 9,
		PeerNodeID:  10,
	})
	if err != nil {
		t.Fatalf("channel.New: %v", err)
	}

	// Build a Privacy-flagged datagram that is > 4 bytes (so we pass the
	// initial length check) but < 4+privacyMICSuffix bytes (so the MIC-tail
	// check fails). Marshal gives 8 bytes; append 5 more → 13 < 18.
	hdr := message.Header{SessionID: sessionID, Privacy: true}
	buf := append(hdr.Marshal(), make([]byte, 5)...)

	b := &Bridge{}
	b.sessions = sessionLookupFunc(func(id uint16) (*channel.Session, bool) {
		if id == sessionID {
			return sess, true
		}
		return nil, false
	})
	if err := b.maybeUnmaskPrivacy(buf); err == nil {
		t.Error("maybeUnmaskPrivacy with short MIC tail: want error, got nil")
	}
}

// TestSendReply_NeverSetsThePrivacyFlag: the unicast reply carries no P
// bit, whatever the request header says, and the peer decrypts it as it
// is. matter.js sends unicast with hasPrivacyEnhancements false
// (MessageExchange.ts:718 at 862ad75f) and drops a unicast message that
// carries the flag (ExchangeManager.ts:230-235), as the bridge's inbound
// gate does, so a P-flagged request never reaches the reply path; the
// test calls sendReply directly to pin that the echo is gone
// (review 2026-11 W6-reg-07).
func TestSendReply_NeverSetsThePrivacyFlag(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	const (
		localSessionID uint16 = 7
		peerSessionID  uint16 = 0x1234
	)
	bridgeSess, peerSess := activitySessionPair(t, peerSessionID)
	b.AttachSessionLookup(sessionLookupFunc(func(id uint16) (*channel.Session, bool) {
		if id == localSessionID {
			return bridgeSess, true
		}
		return nil, false
	}))

	peerConn, peerAddr := newSubscribeTestPeer(t)
	requestHdr := &message.Header{SessionID: localSessionID, MessageCounter: 42, Privacy: true}
	requestProto := message.ProtocolHeader{
		Initiator:  true,
		ExchangeID: 1,
		ProtocolID: im.InteractionModelProtocolID,
	}
	body, err := EncodeStatusResponse(im.StatusResponse{Status: im.StatusSuccess})
	if err != nil {
		t.Fatalf("EncodeStatusResponse: %v", err)
	}
	if err := b.sendReply(peerAddr, requestHdr, requestProto, im.OpcodeStatusResponse, body); err != nil {
		t.Fatalf("sendReply: %v", err)
	}

	_ = peerConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	rbuf := make([]byte, 1500)
	n, _, err := peerConn.ReadFromUDP(rbuf)
	if err != nil {
		t.Fatalf("ReadFromUDP: %v", err)
	}
	datagram := rbuf[:n]
	if datagram[3]&secFlagPrivacyBit != 0 {
		t.Fatal("the unicast reply carries the P bit")
	}
	hdr, hdrLen, err := message.UnmarshalHeader(datagram)
	if err != nil {
		t.Fatalf("UnmarshalHeader: %v", err)
	}
	if _, _, err := peerSess.Decrypt(&hdr, datagram[3], datagram[hdrLen:]); err != nil {
		t.Fatalf("peer Decrypt of the reply: %v", err)
	}
}

// TestPrivacy_UnicastWithPBitIsDropped — privacy enhancements are defined
// for group messages only; a unicast frame carrying the P bit is invalid
// and is dropped rather than unmasked and handed to a unicast session.
// Mirrors matter.js ExchangeManager.ts:230-235 and the chip SDK.
func TestPrivacy_UnicastWithPBitIsDropped(t *testing.T) {
	t.Parallel()
	bridgeKey := bytes.Repeat([]byte{0xAA}, 16)
	peerKey := bytes.Repeat([]byte{0xBB}, 16)
	sess, err := channel.New(channel.Config{EncryptKey: bridgeKey, DecryptKey: peerKey, LocalNodeID: 1, PeerNodeID: 2})
	if err != nil {
		t.Fatal(err)
	}
	b := &Bridge{}
	b.sessions = sessionLookupFunc(func(uint16) (*channel.Session, bool) { return sess, true })
	hdr := message.Header{SessionID: 0x1234, MessageCounter: 1, Privacy: true}
	datagram := append(hdr.Marshal(), bytes.Repeat([]byte{0xCC}, 32)...)
	if err := b.maybeUnmaskPrivacy(datagram); err == nil {
		t.Fatal("unicast frame with the P bit was accepted; it must be dropped")
	}
}

// TestPrivacy_RoundTripCoversSourceAndDestinationNodeIDs — with a Source
// Node ID and a 64-bit Destination Node ID the protected region is 20
// bytes. Masking only the first AES block left the last four bytes of
// the destination id in the clear on send and obfuscated on receive;
// the receiver then authenticated a header the sender never sent and
// dropped the frame on the tag check. matter.js runs the CTR keystream
// over the whole region (MessagePrivacy.ts:53-56).
func TestPrivacy_RoundTripCoversSourceAndDestinationNodeIDs(t *testing.T) {
	t.Parallel()
	bridgeKey := bytes.Repeat([]byte{0xAA}, 16)
	peerKey := bytes.Repeat([]byte{0xBB}, 16)
	const sessionID uint16 = 0x4321
	bridgeSess, err := channel.New(channel.Config{EncryptKey: bridgeKey, DecryptKey: peerKey, LocalNodeID: 1, PeerNodeID: 2})
	if err != nil {
		t.Fatal(err)
	}
	peerSess, err := channel.New(channel.Config{EncryptKey: peerKey, DecryptKey: bridgeKey, LocalNodeID: 2, PeerNodeID: 1})
	if err != nil {
		t.Fatal(err)
	}
	hdr := message.Header{
		SessionID:       sessionID,
		MessageCounter:  0x01020304,
		SessionType:     message.SessionGroup,
		Privacy:         true,
		HasSourceNodeID: true,
		SourceNodeID:    0x1111_2222_3333_4444,
		DestSize:        message.DestNodeID,
		DestNodeID:      0x5555_6666_7777_8888,
	}
	datagram := append(hdr.Marshal(), bytes.Repeat([]byte{0xCC}, 32)...)
	if got := privacyHeaderEnd(datagram); got != 24 {
		t.Fatalf("privacyHeaderEnd = %d, want 24 (4 + counter 4 + source 8 + dest 8)", got)
	}
	original := append([]byte(nil), datagram[4:24]...)

	if err := maskGroupDatagram(bridgeSess, sessionID, datagram); err != nil {
		t.Fatalf("applyOutboundPrivacy: %v", err)
	}
	if bytes.Equal(datagram[20:24], original[16:20]) {
		t.Fatal("outbound mask left the last four bytes of the destination node id in the clear")
	}

	b := &Bridge{}
	b.sessions = sessionLookupFunc(func(id uint16) (*channel.Session, bool) { return peerSess, id == sessionID })
	if err := b.maybeUnmaskPrivacy(datagram); err != nil {
		t.Fatalf("maybeUnmaskPrivacy: %v", err)
	}
	if !bytes.Equal(datagram[4:24], original) {
		t.Fatalf("20-byte region did not round-trip:\n got=%x\nwant=%x", datagram[4:24], original)
	}
}

// maskGroupDatagram applies the Matter §4.7.3.1 privacy mask to a
// privacy-flagged datagram (header || ciphertext || MIC) in place, as the
// sender of a group message does: the inverse of maybeUnmaskPrivacy, for
// the round-trip tests of the group path. The bridge sends no unicast
// message with the P bit (review 2026-11 W6-reg-07), so the masking lives
// here and not in the reply path.
func maskGroupDatagram(sess *channel.Session, sessionID uint16, datagram []byte) error {
	if sess == nil || len(datagram) < 4 || datagram[3]&secFlagPrivacyBit == 0 {
		return nil
	}
	if len(datagram) < 4+privacyMICSuffix {
		return fmt.Errorf("privacy outbound: datagram too short (%d bytes) for MIC tail", len(datagram))
	}
	privacyKey, err := sess.PrivacyKey()
	if err != nil {
		return fmt.Errorf("privacy outbound: derive key: %w", err)
	}
	mic := datagram[len(datagram)-16:]
	hdrEnd := privacyHeaderEnd(datagram)
	if hdrEnd <= 4 {
		return nil
	}
	protected := datagram[4:hdrEnd]
	mask, err := channel.PrivacyKeystream(privacyKey, sessionID, mic, len(protected))
	if err != nil {
		return fmt.Errorf("privacy outbound: mask derive: %w", err)
	}
	return channel.ApplyPrivacyMask(mask, protected)
}
