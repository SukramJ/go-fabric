// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/transport/message"
)

// fabricSessionLookup resolves one CASE session on fabric 1 whose local
// operational node id is the session's.
type fabricSessionLookup struct {
	id   uint16
	sess *channel.Session
}

func (l fabricSessionLookup) Lookup(id uint16) (*channel.Session, bool) {
	return l.sess, id == l.id
}

func (l fabricSessionLookup) FabricFor(id uint16) (uint8, bool) { return 1, id == l.id }

// TestSubscriptionNamingThisNodeReportsChanges: an AttributePathIB naming
// this node's own operational node id selects this node's attributes for
// the subscription's change reports as it does for its priming report —
// matter.js filters a path's nodeId against the node's id on the session's
// fabric only (packages/protocol/src/action/server/AttributeReadResponse.ts
// :160-162 and :205-208 at db021da0, DataResponse.ts:57-62), and builds a
// subscription's updates through the same reader. The change notification
// names no node (review II, R2-P2-alloc-07).
func TestSubscriptionNamingThisNodeReportsChanges(t *testing.T) {
	t.Parallel()
	const (
		sessionID   uint16 = 0x0A07
		localNodeID uint64 = 0xAAAA0707
	)
	sess, err := channel.New(channel.Config{
		EncryptKey: bytes.Repeat([]byte{0x11}, 16), DecryptKey: bytes.Repeat([]byte{0x22}, 16),
		LocalNodeID: localNodeID, PeerNodeID: 0xBBBB0707, InitialCounter: 10,
	})
	if err != nil {
		t.Fatalf("channel.New: %v", err)
	}
	b := newStartedBridge(t)
	b.AttachSessionLookup(fabricSessionLookup{id: sessionID, sess: sess})
	reported := make(chan []im.ConcreteAttributePath, 4)
	mgr := subscription.NewManager(subscription.Config{}, func(_ context.Context, _ *subscription.Subscription, paths []im.ConcreteAttributePath) {
		reported <- paths
	}, nil)
	b.AttachSubscriptionManager(mgr)

	_, peerAddr := newSubscribeTestPeer(t)
	hdr := &message.Header{SessionID: sessionID, MessageCounter: 7}
	proto := message.ProtocolHeader{ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeSubscribeRequest, ExchangeID: 7}
	req := im.SubscribeRequest{
		AttributeRequests: []im.ConcreteAttributePath{{
			Node: localNodeID, Endpoint: 1, Cluster: 0x0006,
			HasNode: true, HasEndpoint: true, HasCluster: true,
		}},
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
	now := time.Now()
	sub.EndPriming(now)

	changed := im.ConcreteAttributePath{Endpoint: 1, Cluster: 0x0006, Attribute: 0x0000, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	mgr.OnAttributeChanged(changed)
	mgr.Tick(t.Context(), now.Add(2*time.Second))
	select {
	case paths := <-reported:
		if len(paths) != 1 || paths[0] != changed {
			t.Errorf("reported paths = %+v, want [%+v]", paths, changed)
		}
	default:
		t.Fatal("the change of an attribute the subscription's path selects was never reported: a path naming this node's own id matched no change")
	}
}

// TestSubscriptionNamingAnotherNodeReportsNoChange (review II,
// R2-P2-dr-req-06): a subscription path naming another node — node id 0
// among them, the node a change notification leaves unnamed — selects
// nothing in the change reports, as it selects nothing in the priming
// report. Only the path naming this node reports.
func TestSubscriptionNamingAnotherNodeReportsNoChange(t *testing.T) {
	t.Parallel()
	const (
		sessionID   uint16 = 0x0A08
		localNodeID uint64 = 0xAAAA0808
	)
	sess, err := channel.New(channel.Config{
		EncryptKey: bytes.Repeat([]byte{0x11}, 16), DecryptKey: bytes.Repeat([]byte{0x22}, 16),
		LocalNodeID: localNodeID, PeerNodeID: 0xBBBB0808, InitialCounter: 10,
	})
	if err != nil {
		t.Fatalf("channel.New: %v", err)
	}
	b := newStartedBridge(t)
	b.AttachSessionLookup(fabricSessionLookup{id: sessionID, sess: sess})
	reported := make(chan []im.ConcreteAttributePath, 4)
	mgr := subscription.NewManager(subscription.Config{}, func(_ context.Context, _ *subscription.Subscription, paths []im.ConcreteAttributePath) {
		reported <- paths
	}, nil)
	b.AttachSubscriptionManager(mgr)

	_, peerAddr := newSubscribeTestPeer(t)
	hdr := &message.Header{SessionID: sessionID, MessageCounter: 7}
	proto := message.ProtocolHeader{ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeSubscribeRequest, ExchangeID: 8}
	req := im.SubscribeRequest{
		AttributeRequests: []im.ConcreteAttributePath{
			{Node: localNodeID, Endpoint: 2, Cluster: 0x0008, HasNode: true, HasEndpoint: true, HasCluster: true},
			{Node: 0, HasNode: true},
			{Node: localNodeID + 1, HasNode: true},
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
	now := time.Now()
	sub.EndPriming(now)

	other := im.ConcreteAttributePath{Endpoint: 1, Cluster: 0x0006, Attribute: 0x0000, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	mgr.OnAttributeChanged(other)
	mgr.Tick(t.Context(), now.Add(2*time.Second))
	select {
	case paths := <-reported:
		t.Fatalf("reported %+v: a path naming another node matched a change of this node", paths)
	default:
	}

	mine := im.ConcreteAttributePath{Endpoint: 2, Cluster: 0x0008, Attribute: 0x0000, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	mgr.OnAttributeChanged(mine)
	mgr.Tick(t.Context(), now.Add(4*time.Second))
	select {
	case paths := <-reported:
		if len(paths) != 1 || paths[0] != mine {
			t.Errorf("reported paths = %+v, want [%+v]", paths, mine)
		}
	default:
		t.Fatal("the change the path naming this node selects was never reported")
	}
}

// TestReadPathNamingAnotherNode: a concrete path naming another node is
// answered UnsupportedNode, a wildcard naming another node selects nothing,
// and a path naming this node reads as one naming no node (matter.js
// AttributeReadResponse.ts:205-208 and :160-162).
func TestReadPathNamingAnotherNode(t *testing.T) {
	t.Parallel()
	d := &wideCountingDispatcher{endpoints: 2, attrs: 2}
	ctx := im.WithLocalNodeID(context.Background(), 0x77)
	concrete := im.ConcreteAttributePath{Node: 0x78, Endpoint: 1, Cluster: 0x0006, Attribute: 1, HasNode: true, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	rd := im.HandleReadRequest(ctx, d, im.ReadRequest{AttributeRequests: []im.ConcreteAttributePath{
		concrete,
		{Node: 0x78, HasNode: true},
		{Node: 0x77, Endpoint: 1, HasNode: true, HasEndpoint: true},
	}})
	if len(rd.Reports) != 1+d.attrs {
		t.Fatalf("reports = %+v, want the UnsupportedNode status and endpoint 1's %d attributes", rd.Reports, d.attrs)
	}
	if got := rd.Reports[0]; !got.IsStatus || got.Status.Status != im.StatusUnsupportedNode || got.Path != concrete {
		t.Errorf("first report = %+v, want UnsupportedNode on %+v", got, concrete)
	}
	for _, r := range rd.Reports[1:] {
		if r.IsStatus || r.Path.Endpoint != 1 {
			t.Errorf("report %+v, want a value of endpoint 1", r)
		}
	}
}
