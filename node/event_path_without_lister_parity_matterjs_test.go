// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"errors"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/client/interaction"
	basicinfo "thingfabric.dev/go-fabric/cluster/spec/basicinformation"
	gencomm "thingfabric.dev/go-fabric/cluster/spec/generalcommissioning"
	"thingfabric.dev/go-fabric/fabrictest"
	"thingfabric.dev/go-fabric/im"
)

// TestSubscribeToEventsOfAClusterWithoutEventsIsRefused (review II,
// R2-P4-nw-05): a subscription whose only path is an event path on a
// cluster whose server lists no event — GeneralCommissioning's on the root
// endpoint, which implements no contract.ClusterEventLister — selects
// nothing, whether the path names an event or not, and is refused with
// INVALID_ACTION; the same path on BasicInformation, which has events,
// establishes. Mirrors matter.js
// packages/protocol/src/action/server/EventReadResponse.ts at db021da0: a
// wildcard event registers one path per event of the cluster type
// (:277-284), a named event the cluster type lacks is UnsupportedEvent
// (:216-218), only a registered path counts (:104-110), and
// packages/node/src/node/server/ServerSubscription.ts:706-711 refuses a
// subscription with no existent attribute or event path.
func TestSubscribeToEventsOfAClusterWithoutEventsIsRefused(t *testing.T) {
	t.Parallel()
	m := newMockPair(t, mockOptions{})
	subscribe := func(p im.ConcreteEventPath) (*fabrictest.Subscription, error) {
		return fabrictest.Subscribe(parityCtx(t), m.pair, interaction.SubscribeRequest{
			Events:             []im.ConcreteEventPath{p},
			MinIntervalFloor:   interaction.Interval(0),
			MaxIntervalCeiling: interaction.Interval(10 * time.Second),
			Updated:            func(*interaction.Report) error { return nil },
		})
	}
	for _, tc := range []struct {
		name string
		path im.ConcreteEventPath
	}{
		{"wildcard event", im.ConcreteEventPath{Endpoint: 0, Cluster: gencomm.ClusterID, HasEndpoint: true, HasCluster: true}},
		{"named event", im.ConcreteEventPath{Endpoint: 0, Cluster: gencomm.ClusterID, Event: 0x7E, HasEndpoint: true, HasCluster: true, HasEvent: true}},
	} {
		sub, err := subscribe(tc.path)
		if sub != nil {
			sub.Close()
		}
		var se *interaction.StatusError
		if !errors.As(err, &se) || se.Status != im.StatusInvalidAction {
			t.Errorf("%s on GeneralCommissioning: %v, want a StatusResponse of INVALID_ACTION", tc.name, err)
		}
	}
	sub, err := subscribe(im.ConcreteEventPath{Endpoint: 0, Cluster: basicinfo.ClusterID, HasEndpoint: true, HasCluster: true})
	if err != nil {
		t.Fatalf("wildcard event on BasicInformation: %v, want the subscription established", err)
	}
	sub.Close()
}
