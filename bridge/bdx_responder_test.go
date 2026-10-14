// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
	"github.com/SukramJ/go-fabric/secure/operational"
	"github.com/SukramJ/go-fabric/transport/message"
)

// memStorage is a bdx.Storage over named byte slices.
type memStorage map[string][]byte

func (s memStorage) Open(_ context.Context, name string) (bdx.Blob, error) {
	b, ok := s[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return bytes.NewReader(b), nil
}

// bdxPeer is the fake peer's side of the routing test: it records every
// BDX-related message the device sends on the secure session.
type bdxPeerLog struct {
	got chan bdx.Message
}

func newBDXSessionHarness(t *testing.T, responder BDXResponder) (*fakeController, *bdxPeerLog) {
	t.Helper()
	fab := newResumeFabric(t)
	peer := newFakeController(t, fab)
	log := &bdxPeerLog{got: make(chan bdx.Message, 32)}
	peer.mu.Lock()
	peer.secureHook = func(_ *message.Header, proto message.ProtocolHeader, payload []byte) bool {
		if proto.ProtocolID == bdx.ProtocolID || (proto.ProtocolID == bdx.SecureChannelProtocolID && proto.Opcode == bdx.OpcodeStatusReport) {
			log.got <- bdx.Message{ProtocolID: proto.ProtocolID, Opcode: proto.Opcode, Payload: append([]byte(nil), payload...)}
			return true
		}
		return false
	}
	peer.mu.Unlock()
	dev := startResumeDevice(t, newMemSubscriptionStore(), func(br *Bridge, sessions *operational.Manager) {
		br.AttachCaseInitiatorProvider(resumeInitiatorProvider(fab, sessions))
		br.AttachOperationalResolver(staticResolver{addr: peer.addr})
		if responder != nil {
			br.AttachBDXResponder(responder)
		}
	})
	peer.setBridge(dev.addr(t))
	// The device opens the CASE session to the peer, as an OTA requestor
	// reaches its provider; the peer then opens BDX exchanges on it.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := dev.br.ConnectOtaProvider(ctx, resumeFabricIndex, resumeCtrlNode); err != nil {
		t.Fatalf("CASE to the peer: %v", err)
	}
	return peer, log
}

// sendBDX sends a BDX message as the initiator of exchange id.
func sendBDX(peer *fakeController, exchangeID uint16, t bdx.MessageType, payload []byte) {
	peer.sendSecure(message.ProtocolHeader{Initiator: false, ExchangeID: exchangeID, ProtocolID: bdx.ProtocolID}, uint8(t), payload)
}

func (l *bdxPeerLog) next(t *testing.T) bdx.Message {
	t.Helper()
	select {
	case m := <-l.got:
		return m
	case <-time.After(5 * time.Second):
		t.Fatal("the device sent no BDX message")
		return bdx.Message{}
	}
}

// TestBDXResponder_RoutesReceiveInit: a ReceiveInit a peer opens on a
// secure session reaches the attached responder, which serves the file as
// the following sender on the same exchange — ReceiveAccept, a Block per
// BlockQuery, the last as BlockEof — the way matter.js's ExchangeManager
// hands a new exchange to BdxProtocol.onNewExchange
// (ExchangeManager.ts:407-440, BdxProtocol.ts:110-173).
func TestBDXResponder_RoutesReceiveInit(t *testing.T) {
	t.Parallel()
	image := bytes.Repeat([]byte{0xA5, 0x5A, 0x01}, 700) // 2100 bytes
	responder := bdx.NewResponder(nil)
	peer, log := newBDXSessionHarness(t, responder)
	responder.EnablePeerForScope(bdx.Peer{FabricIndex: resumeFabricIndex, NodeID: resumeCtrlNode}, "ota", memStorage{"image.bin": image},
		bdx.ScopeConfig{PreferredDriverModes: []bdx.DriverMode{bdx.ReceiverDrive}})

	const exchangeID = 0x4242
	init, err := bdx.Init{
		TransferControl: bdx.TransferControl{ReceiverDrive: true},
		MaxBlockSize:    1024,
		FileDesignator:  []byte("ota/image.bin"),
	}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	sendBDX(peer, exchangeID, bdx.MessageReceiveInit, init)
	m := log.next(t)
	if bdx.MessageType(m.Opcode) != bdx.MessageReceiveAccept {
		t.Fatalf("first answer %s, want ReceiveAccept", bdx.MessageType(m.Opcode))
	}
	accept, err := bdx.DecodeReceiveAccept(m.Payload)
	if err != nil || !accept.TransferControl.ReceiverDrive || accept.MaxBlockSize != 1024 {
		t.Fatalf("ReceiveAccept %+v, %v", accept, err)
	}
	var got []byte
	for counter := uint32(0); ; counter++ {
		sendBDX(peer, exchangeID, bdx.MessageBlockQuery, bdx.CounterOnly{BlockCounter: counter}.Encode())
		m := log.next(t)
		blk, err := bdx.DecodeBlock(m.Payload)
		if err != nil || blk.BlockCounter != counter {
			t.Fatalf("block %d: %+v, %v", counter, blk, err)
		}
		got = append(got, blk.Data...)
		if bdx.MessageType(m.Opcode) == bdx.MessageBlockEOF {
			sendBDX(peer, exchangeID, bdx.MessageBlockAckEOF, bdx.CounterOnly{BlockCounter: counter}.Encode())
			break
		}
		if bdx.MessageType(m.Opcode) != bdx.MessageBlock {
			t.Fatalf("answer %s, want Block", bdx.MessageType(m.Opcode))
		}
	}
	if !bytes.Equal(got, image) {
		t.Fatalf("received %d bytes, served %d", len(got), len(image))
	}
	deadline := time.Now().Add(5 * time.Second)
	for responder.SessionFor(bdx.Peer{FabricIndex: resumeFabricIndex, NodeID: resumeCtrlNode}, "ota") != nil {
		if time.Now().After(deadline) {
			t.Fatal("the session did not end after BlockAckEof")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestBDXResponder_RefusedWithoutResponder: without an attached responder a
// BDX exchange a peer opens is not answered — the bridge takes it as a
// protocol it has no handler for (matter.js ExchangeManager.ts:441-455:
// acknowledged when it asks for it, then dropped).
func TestBDXResponder_RefusedWithoutResponder(t *testing.T) {
	t.Parallel()
	peer, log := newBDXSessionHarness(t, nil)
	init, _ := bdx.Init{TransferControl: bdx.TransferControl{ReceiverDrive: true}, MaxBlockSize: 1024, FileDesignator: []byte("ota/x")}.Encode()
	sendBDX(peer, 0x4343, bdx.MessageReceiveInit, init)
	select {
	case m := <-log.got:
		t.Fatalf("a bridge without a BDX responder answered %+v", m)
	case <-time.After(500 * time.Millisecond):
	}

	// The dispatch itself: no responder, the message is not taken, and
	// the protocol switch reports it unknown.
	b := newStartedBridge(t)
	hdr := message.Header{SessionID: 7}
	proto := message.ProtocolHeader{Initiator: true, ExchangeID: 1, ProtocolID: bdx.ProtocolID, Opcode: uint8(bdx.MessageReceiveInit)}
	if b.deliverRespondedBDX(context.Background(), &hdr, proto, init) {
		t.Fatal("a bridge without a responder took a BDX exchange")
	}
	buf := buildDatagram(buildHeader(0, 3), proto.Marshal(), init)
	if err := b.dispatch(context.Background(), buf, nil); !errors.Is(err, ErrUnknownProtocol) {
		t.Fatalf("dispatch = %v, want ErrUnknownProtocol", err)
	}
}

// TestBDXResponder_UnknownScope: a ReceiveInit naming a scope the peer is
// not enabled for is answered with a FileDesignatorUnknown StatusReport
// (BdxProtocol.ts:132-137, :159-164).
func TestBDXResponder_UnknownScope(t *testing.T) {
	t.Parallel()
	responder := bdx.NewResponder(nil)
	peer, log := newBDXSessionHarness(t, responder)
	init, _ := bdx.Init{TransferControl: bdx.TransferControl{ReceiverDrive: true}, MaxBlockSize: 1024, FileDesignator: []byte("ota/x")}.Encode()
	sendBDX(peer, 0x4444, bdx.MessageReceiveInit, init)
	m := log.next(t)
	if m.ProtocolID != bdx.SecureChannelProtocolID || m.Opcode != bdx.OpcodeStatusReport {
		t.Fatalf("answer %+v, want a StatusReport", m)
	}
	sr, err := bdx.DecodeStatusReport(m.Payload)
	if err != nil || sr.GeneralStatus != bdx.GeneralFailure || sr.ProtocolStatus != bdx.StatusFileDesignatorUnknown || sr.ProtocolID != bdx.ProtocolID {
		t.Fatalf("StatusReport %+v, %v", sr, err)
	}
}
