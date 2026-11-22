// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"crypto/rand"
	"runtime"
	"testing"

	"thingfabric.dev/go-fabric/secure/spake2"
	"thingfabric.dev/go-fabric/transport/message"
	"thingfabric.dev/go-fabric/transport/mrp"
)

// TestBLEMessageQueuedAtCloseIsNotHandled: a message queued on a BLE
// connection's receiver behind one still in dispatch is not handled once the
// connection closed — it takes no PASE claim and sends nothing — and the
// connection is evicted only after the handler in flight returned, so no
// route of the dead connection is left behind (review II, R2-P6-conc-0306).
func TestBLEMessageQueuedAtCloseIsNotHandled(t *testing.T) {
	r := newUnsecuredRig(t)
	provider := NewPerExchangePaseProvider(func() *PaseAdapter {
		a := newPaseAdapterWithVerifier(t)
		a.SetPBKDFParams(1000, []byte("SPAKE2P Key Salt"), 1)
		return a
	})
	r.b.AttachPaseHandlerProvider(provider.Resolve)
	conn := &fakeConn{}
	rc := r.b.newConnReceiver(conn)
	gate := make(chan struct{}) // a predecessor still in dispatch
	rc.mu.Lock()
	rc.prev = gate
	rc.mu.Unlock()
	initRand := make([]byte, spake2.PBKDFRandomSize)
	if _, err := rand.Read(initRand); err != nil {
		t.Fatal(err)
	}
	hdr := message.Header{MessageCounter: 1, HasSourceNodeID: true, SourceNodeID: 0xB1E}
	proto := message.ProtocolHeader{Initiator: true, Opcode: mrp.SCOpcodePBKDFParamRequest, ExchangeID: 0x0707, ProtocolID: mrp.SecureChannelProtocolID}
	msg := append(hdr.Marshal(), proto.Marshal()...)
	msg = append(msg, buildTestPBKDFParamRequest(t, initRand)...)
	rc.deliver(msg)
	closed := make(chan struct{})
	go func() { rc.close(); close(closed) }()
	for { // the close has begun before the predecessor hands its turn on
		rc.mu.Lock()
		done := rc.done
		rc.mu.Unlock()
		if done {
			break
		}
		runtime.Gosched()
	}
	close(gate)
	<-closed
	rc.wait()
	if _, routed := r.b.connRoutes.Load(conn); routed {
		t.Error("a route of the closed connection is left behind")
	}
	r.b.mu.RLock()
	claimed := !r.b.paseInFlightSince.IsZero()
	r.b.mu.RUnlock()
	if claimed {
		t.Error("the queued message took the PASE claim after the close")
	}
	conn.mu.Lock()
	sent := len(conn.sent)
	conn.mu.Unlock()
	if sent != 0 {
		t.Errorf("%d replies sent on the closed connection", sent)
	}
}
