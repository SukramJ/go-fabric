// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"sync"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/cluster/camera"
	wrdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportrequestor"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/tlv"
)

// offerCountingPort counts the Offers that reach a requestor's port.
type offerCountingPort struct {
	mu     sync.Mutex
	offers int
}

func (p *offerCountingPort) Offer(context.Context, camera.WebRTCSession, wrdef.OfferRequest) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.offers++
}
func (*offerCountingPort) Answer(context.Context, camera.WebRTCSession, string) {}
func (*offerCountingPort) IceCandidates(context.Context, camera.WebRTCSession, []camera.ICECandidate) {
}
func (*offerCountingPort) End(context.Context, camera.WebRTCSession, uint8) {}

// TestRequestorSignalOverUDPIsNeverHeld: the WebRtcTransportRequestor's
// signalling commands carry the Large Message quality
// (schema/large_message_gen.go, WebRtcTransportRequestor), and a session
// over UDP carries no Large Message (routeCarriesLargeMessages, conn.go),
// so an Offer over UDP is answered InvalidTransportType by the invoke gate
// before the requestor sees it — also while the host holds the peer's
// unknown signals. A held signal, and with it a reliable message whose
// response waits for a registration, exists only over TCP, where MRP does
// not run; no standalone acknowledgement of a held Invoke is owed on UDP
// (P1-G repair, Fable finding F4).
func TestRequestorSignalOverUDPIsNeverHeld(t *testing.T) {
	t.Parallel()
	port := &offerCountingPort{}
	r, err := camera.NewRequestor(camera.RequestorConfig{Port: port})
	if err != nil {
		t.Fatal(err)
	}
	const deviceTypeCameraController = 0x0147
	h := newAppHarness(t, appDevice{deviceType: deviceTypeCameraController, servers: []contract.ClusterServer{r}})
	release := r.HoldUnknownSignals(h.fabric, harnessControllerNodeID)
	defer release()

	start := time.Now()
	resp := h.invoke(h.endpoints[deviceTypeCameraController], wrdef.ClusterID, wrdef.CmdOffer, func(enc *tlv.Encoder) {
		enc.PutUint(tlv.ContextTag(0), 7)
		enc.PutUTF8(tlv.ContextTag(1), "offer")
	})
	if _, _, status, isStatus := invokeResult(t, resp); !isStatus || status != im.StatusInvalidTransportType {
		t.Fatalf("status %v (status %v), want InvalidTransportType", status, isStatus)
	}
	if waited := time.Since(start); waited >= camera.EarlySignalHoldTime {
		t.Fatalf("the Offer was answered after %v, the hold time", waited)
	}
	port.mu.Lock()
	defer port.mu.Unlock()
	if port.offers != 0 {
		t.Fatalf("%d offers reached the port", port.offers)
	}
}
