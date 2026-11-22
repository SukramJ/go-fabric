// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/secure/channel"
	"thingfabric.dev/go-fabric/transport/message"
)

// TestRefuseUnicastPrivacy: a frame without the P bit, or too short to
// carry the Security Flags, passes untouched; one with the P bit is
// refused on the unsecured session and on any other unicast session
// (review II, R2-P6-err-0312).
func TestRefuseUnicastPrivacy(t *testing.T) {
	t.Parallel()
	plain := append((&message.Header{SessionID: 7, MessageCounter: 0xCAFE}).Marshal(), bytes.Repeat([]byte{0x42}, 32)...)
	orig := append([]byte(nil), plain...)
	if err := refuseUnicastPrivacy(plain); err != nil || !bytes.Equal(plain, orig) {
		t.Fatalf("a frame without the P bit: err=%v, mutated=%v", err, !bytes.Equal(plain, orig))
	}
	if err := refuseUnicastPrivacy([]byte{0x01, 0x02}); err != nil {
		t.Fatalf("a short buffer: %v, want nil (the header decode reports it)", err)
	}
	pase := append((&message.Header{SessionID: 0, Privacy: true}).Marshal(), make([]byte, 32)...)
	if err := refuseUnicastPrivacy(pase); !errors.Is(err, errPrivacyOnUnsecured) {
		t.Fatalf("P bit on session 0: %v", err)
	}
	unicast := append((&message.Header{SessionID: 0x1234, MessageCounter: 1, Privacy: true}).Marshal(), make([]byte, 32)...)
	if err := refuseUnicastPrivacy(unicast); !errors.Is(err, errPrivacyOnUnicast) {
		t.Fatalf("P bit on a unicast session: %v", err)
	}
}

// TestPrivacy_UnicastWithPBitIsDroppedThroughDispatch: a unicast datagram
// carrying the P bit that reaches the receive pipeline is dropped there
// before its header is read, though its session is held — privacy is
// defined for group messages only, and a group datagram takes its own path
// first (review II, R2-P6-err-0312, R2-P6-err-0810).
func TestPrivacy_UnicastWithPBitIsDroppedThroughDispatch(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	sess, err := channel.New(channel.Config{EncryptKey: bytes.Repeat([]byte{0xAA}, 16), DecryptKey: bytes.Repeat([]byte{0xBB}, 16), LocalNodeID: 1, PeerNodeID: 2})
	if err != nil {
		t.Fatal(err)
	}
	b.AttachSessionLookup(sessionLookupFunc(func(uint16) (*channel.Session, bool) { return sess, true }))
	datagram := append((&message.Header{SessionID: 0x1234, MessageCounter: 1, Privacy: true}).Marshal(), bytes.Repeat([]byte{0xCC}, 32)...)
	if err := b.dispatch(context.Background(), datagram, loopbackSrc()); !errors.Is(err, errPrivacyOnUnicast) {
		t.Fatalf("dispatch of a unicast P frame: %v, want errPrivacyOnUnicast", err)
	}
}

// TestSendReply_NeverSetsThePrivacyFlag: the unicast reply carries no P
// bit, whatever the request header says, and the peer decrypts it as it
// is. matter.js sends unicast with hasPrivacyEnhancements false
// (MessageExchange.ts:734) and refuses a unicast message that carries
// the flag (hasPrivacyEnhancements, MessageCodec.ts:250-253), as the bridge's inbound
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
