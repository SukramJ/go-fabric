// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"testing"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/transport/message"
)

// TestRefusedReportIsClosedByItsWaiterOnly (review II, DR5b-c1-04): an
// error status that answers a report whose reporter still waits is handed
// to that reporter, which ends the subscription; the receive path does not
// end it a second time.
func TestRefusedReportIsClosedByItsWaiterOnly(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	m := subscription.NewManager(subscription.Config{}, nil, nil)
	b.AttachSubscriptionManager(m)
	attachPASETestPeer(t, b)
	sub, err := m.Subscribe(subscription.SubscribeArgs{FabricIndex: 1, PeerNodeID: 1, SessionID: paseTestSessionID, MaxIntervalCeiling: 60, AttributePaths: []im.ConcreteAttributePath{{}}})
	if err != nil {
		t.Fatal(err)
	}
	const exch uint16 = 0x4242
	b.rememberReportExchange(sub.ID, paseTestSessionID, exch)
	wait := b.armStatusResponseWait(paseTestSessionID, exch, true)
	body, err := EncodeStatusResponse(im.StatusResponse{Status: im.StatusFailure})
	if err != nil {
		t.Fatal(err)
	}
	_, peerAddr := newSubscribeTestPeer(t)
	hdr := &message.Header{SessionID: paseTestSessionID, MessageCounter: 9}
	proto := message.ProtocolHeader{ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeStatusResponse, ExchangeID: exch, Initiator: false}
	_ = b.absorbStatusResponse(peerAddr, hdr, proto, body)
	if status := <-wait; status != im.StatusFailure {
		t.Fatalf("the waiting reporter got %v, want Failure", status)
	}
	if _, err := m.Get(sub.ID); err != nil {
		t.Fatal("the receive path ended the subscription its waiting reporter ends")
	}
}
