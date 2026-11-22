// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

// An event path's node id is evaluated as an attribute path's is, on the
// Read, the priming report and the subscription's later reports. Mirrors
// matter.js packages/protocol/src/action/server/EventReadResponse.ts at
// db021da0: a concrete path naming another node answers UnsupportedNode
// before any access check (#addConcrete :159-162) and registers nothing, a
// wildcard path naming another node selects nothing (#addWildcard
// :138-140); only a registered path counts as existent (:104-110) for
// ServerSubscription.ts:707-711. The node's id is its id on the session's
// fabric, UNSPECIFIED (0) without one (DataResponse.ts:57-62).

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/im/subscription"
	"thingfabric.dev/go-fabric/secure/channel"
	"thingfabric.dev/go-fabric/tlv"
	"thingfabric.dev/go-fabric/transport/message"
	"thingfabric.dev/go-fabric/transport/udp"
)

// startUpEventDispatcher is eventPathDispatcher's node: endpoint 0 serves
// BasicInformation (0x0028) with the StartUp event (0x00).
func startUpEventDispatcher() eventPathDispatcher {
	revision := im.ConcreteAttributePath{Endpoint: 0, Cluster: 0x0028, Attribute: clusterRevisionAttributeID, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	ok := []im.ReadResult{{Path: revision, Value: im.AttributeValue{Value: uint16(4)}, Status: im.StatusSuccess}}
	return eventPathDispatcher{pathDispatcher{
		revision: ok,
		{HasAttribute: true, Attribute: clusterRevisionAttributeID}:                                    ok,
		{Endpoint: 0, HasEndpoint: true, HasAttribute: true, Attribute: clusterRevisionAttributeID}:    ok,
		{Cluster: 0x0028, HasCluster: true, HasAttribute: true, Attribute: clusterRevisionAttributeID}: ok,
	}}
}

// eventReportsOf splits reports into event data and statuses.
func eventReportsOf(reports []im.EventReport) (data int, statuses []im.StatusCode) {
	for _, r := range reports {
		if r.IsStatus {
			statuses = append(statuses, r.Status.Status)
		} else {
			data++
		}
	}
	return data, statuses
}

// TestPrimingEventPathNamingAnotherNode: the priming report and the
// establish decision of a subscription evaluate each event path's node.
func TestPrimingEventPathNamingAnotherNode(t *testing.T) {
	t.Parallel()
	const local, other = uint64(0x2222), uint64(0x9999)
	b := newStartedBridge(t)
	b.EmitEvent(0, 0x0028, 0x00, nil, contract.EventPriorityCritical)
	d := startUpEventDispatcher()
	ctx := im.WithLocalNodeID(im.WithAuthModePASE(im.WithFabricFilter(context.Background(), false, 0)), local)
	startUp := im.ConcreteEventPath{Endpoint: 0, Cluster: 0x0028, Event: 0x00, HasEndpoint: true, HasCluster: true, HasEvent: true}
	named := func(p im.ConcreteEventPath, node uint64) im.ConcreteEventPath {
		p.Node, p.HasNode = node, true
		return p
	}
	for _, c := range []struct {
		name     string
		path     im.ConcreteEventPath
		matched  int
		data     int
		statuses []im.StatusCode
	}{
		{"concrete, this node", named(startUp, local), 1, 1, nil},
		{"concrete, another node", named(startUp, other), 0, 0, []im.StatusCode{im.StatusUnsupportedNode}},
		{"wildcard, another node", im.ConcreteEventPath{Node: other, HasNode: true}, 0, 0, nil},
		{"wildcard, this node", im.ConcreteEventPath{Node: local, HasNode: true}, 1, 1, nil},
	} {
		report, matched := b.buildInitialReport(ctx, d, im.SubscribeRequest{EventRequests: []im.ConcreteEventPath{c.path}})
		data, statuses := eventReportsOf(report.EventReports)
		if matched != c.matched || data != c.data || len(statuses) != len(c.statuses) || (len(statuses) == 1 && statuses[0] != c.statuses[0]) {
			t.Errorf("%s: matched %d, events %d, statuses %v; want %d, %d, %v", c.name, matched, data, statuses, c.matched, c.data, c.statuses)
		}
	}
}

// TestSubscriptionEventPathsForThisNode: the subscription keeps the event
// paths of this node only, without their node, so a later event matches
// as it matched the priming report and a path naming another node matches
// nothing.
func TestSubscriptionEventPathsForThisNode(t *testing.T) {
	t.Parallel()
	const (
		sessionID   uint16 = 0x0A09
		localNodeID uint64 = 0xAAAA0909
	)
	sess, err := channel.New(channel.Config{
		EncryptKey: bytes.Repeat([]byte{0x11}, 16), DecryptKey: bytes.Repeat([]byte{0x22}, 16),
		LocalNodeID: localNodeID, PeerNodeID: 0xBBBB0909, InitialCounter: 10,
	})
	if err != nil {
		t.Fatalf("channel.New: %v", err)
	}
	b := newStartedBridge(t)
	b.AttachSessionLookup(fabricSessionLookup{id: sessionID, sess: sess})
	mgr := subscription.NewManager(subscription.Config{}, nil, nil)
	b.AttachSubscriptionManager(mgr)

	_, peerAddr := newSubscribeTestPeer(t)
	hdr := &message.Header{SessionID: sessionID, MessageCounter: 7}
	proto := message.ProtocolHeader{ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeSubscribeRequest, ExchangeID: 9}
	startUp := im.ConcreteEventPath{Endpoint: 0, Cluster: 0x0028, Event: 0x00, HasEndpoint: true, HasCluster: true, HasEvent: true}
	mine := startUp
	mine.Node, mine.HasNode = localNodeID, true
	req := im.SubscribeRequest{
		EventRequests: []im.ConcreteEventPath{
			mine,
			{Node: localNodeID + 1, HasNode: true},
			{Node: 0, HasNode: true, Endpoint: 1, HasEndpoint: true},
		},
		MaxIntervalCeiling: 60,
	}
	var stamp im.ReportData
	subID, err := b.registerSubscription(peerAddr, hdr, proto, req, &stamp)
	if err != nil {
		t.Fatalf("registerSubscription: %v", err)
	}
	sub, err := mgr.Get(subID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(sub.EventPaths) != 1 || sub.EventPaths[0] != startUp {
		t.Fatalf("subscription event paths %+v, want [%+v]", sub.EventPaths, startUp)
	}
}

// TestReadEventPathNamingAnotherNode: a Read on a session without a fabric
// (this node's id UNSPECIFIED) answers a concrete event path naming a node
// with UNSUPPORTED_NODE and reads no event for it, and a wildcard naming a
// node reads nothing.
func TestReadEventPathNamingAnotherNode(t *testing.T) {
	t.Parallel()
	const sessionID uint16 = 93
	b := newStartedBridge(t)
	peerConn, peerSess, peerAddr := chunkTestPeer(t, b, sessionID)
	markPASE(b, sessionID)
	b.EmitEvent(0, 0x0028, 0x00, nil, contract.EventPriorityCritical)
	d := startUpEventDispatcher()

	req := im.ReadRequest{EventRequests: []im.ConcreteEventPath{
		{Node: 0x9999, Endpoint: 0, Cluster: 0x0028, Event: 0x00, HasNode: true, HasEndpoint: true, HasCluster: true, HasEvent: true},
		{Node: 0x9999, HasNode: true},
	}}
	hdr := &message.Header{SessionID: sessionID, MessageCounter: 5000}
	proto := message.ProtocolHeader{ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeReadRequest, ExchangeID: 0x2203, Initiator: true}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := b.dispatchReadRequest(ctx, peerAddr, hdr, proto, d, req); err != nil {
		t.Fatalf("dispatchReadRequest: %v", err)
	}
	rd := readEventReport(t, peerConn, peerSess)
	data, statuses := eventReportsOf(rd.EventReports)
	if data != 0 || len(statuses) != 1 || statuses[0] != im.StatusUnsupportedNode {
		t.Fatalf("events %d, statuses %v; want no event and one UNSUPPORTED_NODE", data, statuses)
	}
}

// readEventReport reads the ReportData the bridge sent to the peer.
func readEventReport(t *testing.T, peerConn *net.UDPConn, peerSess *channel.Session) im.ReportData {
	t.Helper()
	buf := make([]byte, udp.MaxDatagramSize)
	for {
		if err := peerConn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
			t.Fatalf("SetReadDeadline: %v", err)
		}
		n, from, err := peerConn.ReadFromUDP(buf)
		if err != nil {
			t.Fatalf("no report: %v", err)
		}
		if port, ok := chunkPeerBridgePort.Load(peerConn); ok && from.Port != port.(int) {
			continue // another test's traffic
		}
		hdr, hdrLen, err := message.UnmarshalHeader(buf[:n])
		if err != nil {
			t.Fatalf("UnmarshalHeader: %v", err)
		}
		plain, _, err := peerSess.Decrypt(&hdr, securityFlagsByte(&hdr), buf[hdrLen:n])
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}
		proto, protoLen, err := message.UnmarshalProtocolHeader(plain)
		if err != nil {
			t.Fatalf("UnmarshalProtocolHeader: %v", err)
		}
		if proto.Opcode != im.OpcodeReportData {
			continue // a standalone ack
		}
		rd, err := im.UnmarshalReportDataTLV(tlv.NewDecoder(plain[protoLen:]), nil, nil)
		if err != nil {
			t.Fatalf("UnmarshalReportDataTLV: %v", err)
		}
		return rd
	}
}
