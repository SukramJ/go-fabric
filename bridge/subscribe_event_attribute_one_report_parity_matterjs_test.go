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

	"github.com/SukramJ/go-fabric/contract"
	endpointpkg "github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/udp"
)

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

	m := subscription.NewManager(subscription.Config{}, b.SubscriptionReporter(), nil)
	m.SetEventReporter(b.SubscriptionEventReporter())
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

	m.Tick(context.Background(), t0.Add(time.Second))
	got, ok := readReportData(t, peerConn, peerSess, 5*time.Second)
	if !ok {
		t.Fatal("no report when the floor ended")
	}
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
