// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

// An attribute change and an event that are due together reach the
// subscriber in one ReportData: matter.js
// packages/node/src/node/server/ServerSubscription.ts:539-555 at db021da0
// (#sendUpdate takes the attribute filter and the events' minimum number
// in one pass and calls #sendUpdateMessage once) and :860-887, :926-936
// (#iterateDataUpdate yields the attributes, then the events, into one
// DataReport). matter.js has no test for it at that commit; this one is
// the module's own (review II, W2-conc-04).

import (
	"context"
	"net"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/contract"
	endpointpkg "thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/im/subscription"
	"thingfabric.dev/go-fabric/secure/channel"
	"thingfabric.dev/go-fabric/tlv"
	"thingfabric.dev/go-fabric/transport/message"
	"thingfabric.dev/go-fabric/transport/mrp"
	"thingfabric.dev/go-fabric/transport/udp"
)

// answerOngoingReport answers the ongoing report of sessionID that waits
// for the peer's StatusResponse with status, as an inbound StatusResponse
// does (absorbStatusResponse → signalStatusResponseRX).
func answerOngoingReport(t *testing.T, b *Bridge, sessionID uint16, status im.StatusCode) {
	t.Helper()
	var key mrp.ExchangeKey
	waitFor(t, "a report waiting for its StatusResponse", func() bool {
		found := false
		b.routing.statusResponseWaits.Range(func(k, _ any) bool {
			ek, ok := k.(mrp.ExchangeKey)
			if ok && ek.SessionID == sessionID && ek.Initiator {
				key, found = ek, true
			}
			return !found
		})
		return found
	})
	if !b.signalStatusResponseRX(key.SessionID, key.ExchangeID, true, status) {
		t.Fatal("the StatusResponse found no waiting report")
	}
}

// readReportData reads the bridge's next ReportData from the peer socket
// within wait and decodes it; ok is false when none arrives.
func readReportData(t *testing.T, peerConn *net.UDPConn, peerSess *channel.Session, wait time.Duration) (report im.ReportData, ok bool) {
	t.Helper()
	buf := make([]byte, udp.MaxDatagramSize)
	deadline := time.Now().Add(wait)
	for {
		if err := peerConn.SetReadDeadline(deadline); err != nil {
			t.Fatalf("SetReadDeadline: %v", err)
		}
		n, from, err := peerConn.ReadFromUDP(buf)
		if err != nil {
			return im.ReportData{}, false
		}
		if port, known := chunkPeerBridgePort.Load(peerConn); known && from.Port != port.(int) {
			continue // another test's traffic on a reused port
		}
		hdr, hdrLen, err := message.UnmarshalHeader(buf[:n])
		if err != nil {
			t.Fatalf("UnmarshalHeader: %v", err)
		}
		plain, _, err := peerSess.Decrypt(&hdr, securityFlagsByte(&hdr), buf[hdrLen:n])
		if err != nil {
			t.Fatalf("peer.Decrypt: %v", err)
		}
		proto, protoLen, err := message.UnmarshalProtocolHeader(plain)
		if err != nil {
			t.Fatalf("UnmarshalProtocolHeader: %v", err)
		}
		if proto.Opcode != im.OpcodeReportData {
			t.Fatalf("opcode %#x, want a ReportData", proto.Opcode)
		}
		report, err = im.UnmarshalReportDataTLV(tlv.NewDecoder(plain[protoLen:]), nil, nil)
		if err != nil {
			t.Fatalf("UnmarshalReportDataTLV: %v", err)
		}
		return report, true
	}
}

func TestEventAndAttributeChangeDueTogetherAreOneReportData(t *testing.T) {
	t.Parallel()
	const sessionID uint16 = 79
	b := newStartedBridgeWithSnapshotter(t, manyTempSensorsSnapshotterForTest())
	peerConn, peerSess, peerAddr := chunkTestPeer(t, b, sessionID)

	var attrPath im.ConcreteAttributePath
	for _, ep := range b.Topology().Endpoints {
		if ep != nil && !ep.IsRoot() && !ep.IsAggregator() {
			if paths := endpointpkg.ReportablePaths(ep); len(paths) > 0 {
				attrPath = paths[0]
				break
			}
		}
	}
	if !attrPath.HasAttribute {
		t.Fatal("the topology has no reportable attribute")
	}

	m := subscription.NewManager(subscription.Config{}, nil, nil)
	b.AttachSubscriptionManager(m)
	sub, err := m.Subscribe(subscription.SubscribeArgs{
		FabricIndex: 1, PeerNodeID: 0xBBBB4444, SessionID: sessionID,
		MinIntervalFloor: 1, MaxIntervalCeiling: 60,
		AttributePaths: []im.ConcreteAttributePath{attrPath},
		// One wildcard path with the subscriber's isUrgent flag, as Apple's
		// MTRDevice subscribes.
		EventPaths: []im.ConcreteEventPath{{IsUrgent: true}},
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	b.routing.subTargets.Store(sub.ID, subTarget{
		pase: true, // a PASE subscriber: the report is not ACL-gated
		src:  peerAddr, hasPeerSourceNodeID: true, peerSourceNodeID: 0xBBBB4444,
		exchangeID: 31, sessionID: sessionID, peerInitiator: true,
	})
	t0 := time.Now()
	sub.TouchLastReport(t0)

	// A critical event and an attribute change, 200 ms after the last report.
	b.EmitEvent(attrPath.Endpoint, 0x003B, 0x01, uint8(1), contract.EventPriority(im.EventPriorityCritical))
	m.OnAttributeChanged(attrPath)

	m.Tick(context.Background(), t0.Add(200*time.Millisecond))
	if got, ok := readReportData(t, peerConn, peerSess, 300*time.Millisecond); ok {
		t.Fatalf("a report inside MinIntervalFloor: %d attribute and %d event reports; a critical event does not pass the floor", len(got.Reports), len(got.EventReports))
	}

	// The report's reporter holds the tick until the peer's
	// StatusResponse settles it ([Bridge.deliverOngoingReport]): the
	// test answers it once it has read the report.
	ticked := make(chan struct{})
	go func() {
		defer close(ticked)
		m.Tick(context.Background(), t0.Add(time.Second))
	}()
	got, ok := readReportData(t, peerConn, peerSess, 5*time.Second)
	if !ok {
		t.Fatal("no report when the floor ended")
	}
	answerOngoingReport(t, b, sessionID, im.StatusSuccess)
	<-ticked
	if !got.HasSubscription || got.SubscriptionID != sub.ID {
		t.Fatalf("report of subscription %d (present %v), want %d", got.SubscriptionID, got.HasSubscription, sub.ID)
	}
	if len(got.Reports) != 1 || got.Reports[0].Path.Endpoint != attrPath.Endpoint || got.Reports[0].Path.Cluster != attrPath.Cluster || got.Reports[0].Path.Attribute != attrPath.Attribute {
		t.Fatalf("attribute reports = %+v, want the one changed path %+v", got.Reports, attrPath)
	}
	if len(got.EventReports) != 1 || got.EventReports[0].Path.Cluster != 0x003B || got.EventReports[0].Path.Event != 0x01 {
		t.Fatalf("event reports = %+v, want the one event in the same ReportData", got.EventReports)
	}
	if got.SuppressResponse {
		t.Error("SuppressResponse set on a report that carries data")
	}

	// Nothing is left: no second ReportData a tick or a floor later.
	m.Tick(context.Background(), t0.Add(time.Second+250*time.Millisecond))
	m.Tick(context.Background(), t0.Add(2*time.Second+250*time.Millisecond))
	if again, ok := readReportData(t, peerConn, peerSess, 300*time.Millisecond); ok {
		t.Fatalf("a second report followed: %d attribute and %d event reports", len(again.Reports), len(again.EventReports))
	}
}
