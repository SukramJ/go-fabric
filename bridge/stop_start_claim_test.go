// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/endpointtest"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// countingContact is a contact source that counts the callbacks the bridge
// has registered with it and not unregistered.
type countingContact struct{ live atomic.Int32 }

func (*countingContact) MatterMeasurementClass() contract.MeasurementClass {
	return contract.MeasurementContact
}

func (*countingContact) MatterBoolValue() (value, observed bool) { return false, true }

func (s *countingContact) OnMatterValueChanged(func()) func() {
	s.live.Add(1)
	var once sync.Once
	return func() { once.Do(func() { s.live.Add(-1) }) }
}

// heldPaseHandler keeps the handler of a PBKDFParamRequest running until
// it is released: a handler in flight, which Stop waits for.
type heldPaseHandler struct {
	noopPaseHandler
	entered, release chan struct{}
}

func (h heldPaseHandler) ProcessPBKDFParamRequest([]byte) (opcode uint8, payload []byte, err error) {
	close(h.entered)
	<-h.release
	return 0, nil, ErrPaseHandlerMissing
}

// TestStartDuringStopIsRefused: a Stop that is still waiting for the
// handlers in flight has not stopped the bridge, and a Start in that
// window is refused — with ErrStopped since a bridge is single-run
// (R2-P2-stop-05), with ErrAlreadyStarted before. Before review II Stop gave the
// start claim back first and went on to tear down afterwards — the host's
// value and switch listeners, the advertiser, the servers' transitions,
// the commissioning window's timer — so a Start in between had the run it
// had just wired taken apart under it: it served, but no value change
// reached a subscriber until the next Reassemble (R2-P0-rv-A06).
func TestStartDuringStopIsRefused(t *testing.T) {
	t.Parallel()
	src := &countingContact{}
	asm, err := endpoint.New(endpointtest.NewFakeStore(), endpointtest.AssemblerConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	b := newStartedBridgeWithSnapshotter(t, func(ctx context.Context) (*endpoint.Topology, error) {
		return asm.Assemble(ctx, []endpoint.Snapshot{{Scope: "s", Endpoints: []endpoint.Spec{{
			StableKey: endpoint.StringKey("contact"), DeviceAddress: "C", DeviceType: 0x0015,
			FriendlyName: "Contact", ChannelAddress: "C:1", Measurement: src,
		}}}})
	})
	b.AttachSubscriptionManager(subscription.NewManager(subscription.Config{}, nil, nil))
	if got := src.live.Load(); got != 1 {
		t.Fatalf("%d listeners registered on a started bridge, want 1", got)
	}

	// A handler in flight: Stop waits for it.
	held := heldPaseHandler{entered: make(chan struct{}), release: make(chan struct{})}
	b.AttachPaseHandler(held)
	_, port, err := net.SplitHostPort(b.LocalAddr())
	if err != nil {
		t.Fatalf("LocalAddr %q: %v", b.LocalAddr(), err)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	peer, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: portNumber})
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	hdr := message.Header{MessageCounter: 1, HasSourceNodeID: true, SourceNodeID: 0x5A5A}
	proto := message.ProtocolHeader{Initiator: true, Opcode: mrp.SCOpcodePBKDFParamRequest, ExchangeID: 7, ProtocolID: mrp.SecureChannelProtocolID}
	if _, err := peer.Write(append(hdr.Marshal(), proto.Marshal()...)); err != nil {
		t.Fatalf("send: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	select {
	case <-held.entered:
	case <-ctx.Done():
		t.Fatal("the handler never ran")
	}

	// Stop cancels the serve context on its way into the wait for the
	// handler, which is still running.
	serving := b.handlerContext()
	stopped := make(chan error, 1)
	go func() { stopped <- b.Stop(ctx) }()
	select {
	case <-serving.Done():
	case <-ctx.Done():
		t.Fatal("Stop never cancelled the serve context")
	}

	startErr := b.Start(ctx)
	close(held.release)
	if err := <-stopped; err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !errors.Is(startErr, ErrStopped) {
		b.mu.RLock()
		started := b.started
		b.mu.RUnlock()
		t.Errorf("Start while Stop was waiting: %v, want ErrStopped (the bridge is started: %v, with %d host listeners)", startErr, started, src.live.Load())
	}
	if got := src.live.Load(); got != 0 {
		t.Errorf("%d host listeners registered after Stop returned, want 0", got)
	}
	// Once Stop has returned the claim is free again.
	if b.startClaim.Load() {
		t.Error("the start claim is still held after Stop returned")
	}
}
