// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/transport/message"
)

// TestRefusedSubscribeTearsDownFirst pins the order of matter.js
// InteractionServer.handleSubscribeRequest at db021da0 (F-db021da0-14,
// review II R2-P2-sess-04): the path count is checked first (:635-641), then
// a KeepSubscriptions=false request cancels the peer's subscriptions
// (:647-665), and only then is a request naming no path (:667-672) or an
// illegal one (validateReadPaths, :699) refused. So the last two cost the
// peer its subscriptions; a request over the path ceiling does not.
func TestRefusedSubscribeTearsDownFirst(t *testing.T) {
	t.Parallel()
	const (
		bridgeNode, peerNode uint64 = 0x1111, 0xAAAA
		sess                 uint16 = 10
	)
	good := im.ConcreteAttributePath{HasEndpoint: true, Endpoint: 0, HasCluster: true, Cluster: 0x001D, HasAttribute: true, Attribute: 0}
	// A wildcard cluster with a concrete non-global attribute: illegal.
	illegal := im.ConcreteAttributePath{HasEndpoint: true, Endpoint: 0, HasAttribute: true, Attribute: 0}
	tooMany := make([]im.ConcreteAttributePath, im.MaxReadPaths+1)
	for i := range tooMany {
		tooMany[i] = good
	}
	for _, tc := range []struct {
		name     string
		paths    []im.ConcreteAttributePath
		tornDown bool
	}{
		{"illegal path", []im.ConcreteAttributePath{illegal}, true},
		{"no path", nil, true},
		{"over the path ceiling", tooMany, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := newStartedBridgeWithSnapshotter(t, manyTempSensorsSnapshotterForTest())
			bs, _ := identityTestPair(t, bridgeNode, peerNode, 0x10, 0x20)
			b.AttachSessionLookup(identityTestLookup{
				sess:    map[uint16]*channel.Session{sess: bs},
				fabric:  map[uint16]uint8{sess: 1},
				subject: map[uint16]uint64{sess: peerNode},
			})
			mgr := subscription.NewManager(subscription.Config{}, nil, nil)
			b.AttachSubscriptionManager(mgr)
			earlier, err := mgr.Subscribe(subscription.SubscribeArgs{FabricIndex: 1, PeerNodeID: peerNode, SessionID: sess, MaxIntervalCeiling: 60, AttributePaths: []im.ConcreteAttributePath{good}})
			if err != nil {
				t.Fatal(err)
			}
			peerConn, peerAddr := openPeerSocket(t)
			t.Cleanup(func() { _ = peerConn.Close() })
			proto := message.ProtocolHeader{ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeSubscribeRequest, ExchangeID: 77, Initiator: true, NeedsAck: true}
			req := im.SubscribeRequest{KeepSubscriptions: false, MaxIntervalCeiling: 60, AttributeRequests: tc.paths}
			_ = b.handleSubscribeRequest(context.Background(), peerAddr, &message.Header{SessionID: sess}, proto, req)

			_, getErr := mgr.Get(earlier.ID)
			if gone := getErr != nil; gone != tc.tornDown {
				t.Fatalf("earlier subscription gone=%v after the refused KeepSubscriptions=false request, want %v", gone, tc.tornDown)
			}
			if n := len(mgr.Snapshot()); n > 1 {
				t.Fatalf("%d subscriptions after a refused request", n)
			}
		})
	}
}
