// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bdx_test

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
)

type memStorage map[string][]byte

func (s memStorage) Open(_ context.Context, name string) (bdx.Blob, error) {
	b, ok := s[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return bytes.NewReader(b), nil
}

var testPeer = bdx.Peer{FabricIndex: 1, NodeID: 0x1234, SessionID: 7}

// TestResponder_ServesAnEnabledScope: a ReceiveInit for `<scope>/<file>`
// of an enabled peer is served from the scope's storage; the started
// listener sees the session before it negotiates and registers its
// observers (BdxProtocol.ts:110-194), and the session is gone once the
// transfer ended.
func TestResponder_ServesAnEnabledScope(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r := bdx.NewResponder(nil)
	data := testData(2000)
	if !r.EnablePeerForScope(testPeer, "ota", memStorage{"img": data}, bdx.ScopeConfig{PreferredDriverModes: []bdx.DriverMode{bdx.ReceiverDrive}, MessageTimeout: time.Minute}) {
		t.Fatal("enable refused")
	}
	type seen struct {
		scope, file string
		finished    uint64
		closed      bool
	}
	got := make(chan seen, 1)
	off := r.OnSessionStarted(func(s *bdx.ServerSession, scope string) {
		var sn seen
		sn.scope, sn.file = scope, s.File()
		s.OnFinished(func(n uint64) { sn.finished = n })
		s.OnClosed(func() { sn.closed = true; got <- sn })
		if r.SessionFor(testPeer, "ota") != s {
			t.Error("SessionFor does not return the started session")
		}
	})
	defer off()

	a, b := newPipe(1178)
	init := bdx.Init{TransferControl: bdx.TransferControl{ReceiverDrive: true}, MaxBlockSize: 1024, FileDesignator: []byte("ota/img")}
	payload, _ := init.Encode()
	go r.HandleExchange(ctx, testPeer, bdx.Message{ProtocolID: bdx.ProtocolID, Opcode: uint8(bdx.MessageReceiveInit), Payload: payload}, b)

	// The first message is already taken: the receiver continues after it.
	m := bdx.NewMessenger(a, time.Second)
	if _, v, err := m.Next(ctx, bdx.MessageReceiveAccept); err != nil || !v.(bdx.ReceiveAccept).TransferControl.ReceiverDrive {
		t.Fatalf("accept %v %v", v, err)
	}
	var out []byte
	for c := uint32(0); ; c++ {
		_ = m.SendBlockQuery(ctx, c)
		typ, blk, err := m.ReadBlock(ctx)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, blk.Data...)
		if typ == bdx.MessageBlockEOF {
			_ = m.SendBlockAckEOF(ctx, c)
			break
		}
	}
	select {
	case sn := <-got:
		if sn.scope != "ota" || sn.file != "img" || sn.finished != 2000 || !sn.closed {
			t.Fatalf("session %+v", sn)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the session never closed")
	}
	if !bytes.Equal(out, data) || r.SessionFor(testPeer, "ota") != nil {
		t.Fatal("transfer or session bookkeeping wrong")
	}
}

// TestResponder_ScopeRules: enabling again with another configuration is
// refused while a transfer runs, disabling without force fails then, and
// a forced disable closes the transfer (BdxProtocol.ts:57-108).
func TestResponder_ScopeRules(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r := bdx.NewResponder(nil)
	st := memStorage{"img": testData(5000)}
	cfg := bdx.ScopeConfig{PreferredDriverModes: []bdx.DriverMode{bdx.ReceiverDrive}}
	r.EnablePeerForScope(testPeer, "ota", st, cfg)
	if !r.EnablePeerForScope(testPeer, "ota", st, bdx.ScopeConfig{MaxBlockSize: 10}) {
		t.Fatal("a reconfiguration without a running transfer was refused")
	}
	r.EnablePeerForScope(testPeer, "ota", st, cfg)

	started := make(chan *bdx.ServerSession, 1)
	r.OnSessionStarted(func(s *bdx.ServerSession, _ string) { started <- s })
	a, b := newPipe(1178)
	payload, _ := bdx.Init{TransferControl: bdx.TransferControl{ReceiverDrive: true}, MaxBlockSize: 1024, FileDesignator: []byte("ota/img")}.Encode()
	handled := make(chan struct{})
	go func() {
		r.HandleExchange(ctx, testPeer, bdx.Message{ProtocolID: bdx.ProtocolID, Opcode: uint8(bdx.MessageReceiveInit), Payload: payload}, b)
		close(handled)
	}()
	s := <-started
	if s.Peer() != testPeer {
		t.Fatalf("peer %+v", s.Peer())
	}
	if _, _, err := bdx.NewMessenger(a, time.Second).Next(ctx, bdx.MessageReceiveAccept); err != nil {
		t.Fatal(err)
	}
	if r.EnablePeerForScope(testPeer, "ota", st, bdx.ScopeConfig{MaxBlockSize: 10}) {
		t.Fatal("a reconfiguration during a transfer was accepted")
	}
	if !r.EnablePeerForScope(testPeer, "ota", st, cfg) {
		t.Fatal("the same configuration during a transfer was refused")
	}
	if err := r.DisablePeerForScope(testPeer, "ota", false); !errors.Is(err, bdx.ErrActiveSession) {
		t.Fatalf("disable without force: %v", err)
	}
	if err := r.DisablePeerForScope(testPeer, "ota", true); err != nil {
		t.Fatal(err)
	}
	select {
	case <-handled:
	case <-time.After(5 * time.Second):
		t.Fatal("a forced disable did not end the transfer")
	}
}

// TestResponder_Refusals: a first message that is no *Init is answered
// UnexpectedMessage (BdxProtocol.ts:167-171); an Init for a scope the peer
// is not enabled for, or without a file, FileDesignatorUnknown (:132-137).
func TestResponder_Refusals(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	r := bdx.NewResponder(nil)
	r.EnablePeerForScope(testPeer, "ota", memStorage{}, bdx.ScopeConfig{})
	initFor := func(fd string) []byte {
		p, _ := bdx.Init{TransferControl: bdx.TransferControl{ReceiverDrive: true}, MaxBlockSize: 64, FileDesignator: []byte(fd)}.Encode()
		return p
	}
	for _, tc := range []struct {
		name  string
		first bdx.Message
		peer  bdx.Peer
		code  bdx.StatusCode
	}{
		{"BlockQuery first", bdx.Message{ProtocolID: bdx.ProtocolID, Opcode: uint8(bdx.MessageBlockQuery), Payload: bdx.CounterOnly{}.Encode()}, testPeer, bdx.StatusUnexpectedMessage},
		{"other scope", bdx.Message{ProtocolID: bdx.ProtocolID, Opcode: uint8(bdx.MessageReceiveInit), Payload: initFor("log/x")}, testPeer, bdx.StatusFileDesignatorUnknown},
		{"no file", bdx.Message{ProtocolID: bdx.ProtocolID, Opcode: uint8(bdx.MessageReceiveInit), Payload: initFor("ota")}, testPeer, bdx.StatusFileDesignatorUnknown},
		{"other peer", bdx.Message{ProtocolID: bdx.ProtocolID, Opcode: uint8(bdx.MessageReceiveInit), Payload: initFor("ota/x")}, bdx.Peer{FabricIndex: 1, NodeID: 9}, bdx.StatusFileDesignatorUnknown},
		{"missing file", bdx.Message{ProtocolID: bdx.ProtocolID, Opcode: uint8(bdx.MessageReceiveInit), Payload: initFor("ota/x")}, testPeer, bdx.StatusFileDesignatorUnknown},
		{"SendInit without sink storage", bdx.Message{ProtocolID: bdx.ProtocolID, Opcode: uint8(bdx.MessageSendInit), Payload: initFor("ota/x")}, testPeer, bdx.StatusFileDesignatorUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a, b := newPipe(1178)
			r.HandleExchange(ctx, tc.peer, tc.first, b)
			expectStatus(t, a, tc.code)
		})
	}
}
