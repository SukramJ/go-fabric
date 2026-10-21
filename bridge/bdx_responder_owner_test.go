// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
	"github.com/SukramJ/go-fabric/secure/operational"
	"github.com/SukramJ/go-fabric/transport/message"
)

// heldResponder is a BDX responder that holds its exchange until its
// context ends — a transfer still running when the bridge stops.
type heldResponder struct {
	entered  chan struct{}
	returned atomic.Bool
}

func (r *heldResponder) HandleExchange(ctx context.Context, _ bdx.Peer, _ bdx.Message, _ bdx.Exchange) {
	close(r.entered)
	<-ctx.Done()
	r.returned.Store(true)
}

// TestStopWaitsForTheBDXResponder pins the bridge as the owner of the
// per-exchange BDX responder goroutine (G1-14, ADR 0026 P5): Stop cancels
// it with the handler context and returns only after it returned.
func TestStopWaitsForTheBDXResponder(t *testing.T) {
	t.Parallel()
	responder := &heldResponder{entered: make(chan struct{})}
	fab := newResumeFabric(t)
	peer := newFakeController(t, fab)
	peer.mu.Lock()
	peer.secureHook = func(*message.Header, message.ProtocolHeader, []byte) bool { return false }
	peer.mu.Unlock()
	dev := startResumeDevice(t, newMemSubscriptionStore(), func(br *Bridge, sessions *operational.Manager) {
		br.AttachCaseInitiatorProvider(resumeInitiatorProvider(fab, sessions))
		br.AttachOperationalResolver(staticResolver{addr: peer.addr})
		br.AttachBDXResponder(responder)
	})
	peer.setBridge(dev.addr(t))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := dev.br.ConnectOtaProvider(ctx, resumeFabricIndex, resumeCtrlNode); err != nil {
		t.Fatalf("CASE to the peer: %v", err)
	}
	init, err := bdx.Init{
		TransferControl: bdx.TransferControl{ReceiverDrive: true},
		MaxBlockSize:    1024,
		FileDesignator:  []byte("ota/image.bin"),
	}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	sendBDX(peer, 0x4243, bdx.MessageReceiveInit, init)
	<-responder.entered
	if err := dev.br.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if !responder.returned.Load() {
		t.Fatal("Stop returned while the BDX responder still ran")
	}
}
