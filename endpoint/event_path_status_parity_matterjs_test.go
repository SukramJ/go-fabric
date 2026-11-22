// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"testing"

	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
)

// eventListingServer is a gateServer that lists its events.
type eventListingServer struct {
	gateServer
	events []uint32
}

func (s *eventListingServer) MatterEvents() []uint32 { return s.events }

// TestEventPathStatusWithoutEvent: a path that names no event exists where
// the cluster has an event a wildcard event path selects. matter.js
// registers a wildcard event path per event of the cluster type
// (packages/protocol/src/action/server/EventReadResponse.ts:277-284 at
// db021da0, `for (const event of cluster.type.events)` at :279), so a
// cluster without events registers nothing and counts as no existent path
// (:104-110). A server without an event lister lists no event
// ([contract.ClusterEventLister]): neither shape of path exists on it, as a
// concrete event the cluster type lacks is UnsupportedEvent (:216-218;
// review II, R2-P4-nw-05).
func TestEventPathStatusWithoutEvent(t *testing.T) {
	t.Parallel()
	withEvents := &eventListingServer{gateServer: gateServer{id: 0x0028}, events: []uint32{0x01}} // an event other than 0, the id a path without one carries
	without := &eventListingServer{gateServer: gateServer{id: 0x0006}, events: nil}
	unlisted := &gateServer{id: 0x0008}
	d := NewTopologyDispatcher(makeTopology(&Endpoint{ID: 2, Source: gateSource{srvs: []contract.ClusterServer{withEvents, without, unlisted}}}))
	at := func(ep uint16, cluster uint32) im.ConcreteEventPath {
		return im.ConcreteEventPath{Endpoint: ep, Cluster: cluster, HasEndpoint: true, HasCluster: true}
	}
	for _, c := range []struct {
		name string
		path im.ConcreteEventPath
		want im.StatusCode
	}{
		{"cluster with an event", at(2, 0x0028), im.StatusSuccess},
		{"cluster listing no event", at(2, 0x0006), im.StatusUnsupportedEvent},
		{"cluster without an event lister", at(2, 0x0008), im.StatusUnsupportedEvent},
		{"event of a cluster without an event lister", im.ConcreteEventPath{Endpoint: 2, Cluster: 0x0008, Event: 0x00, HasEndpoint: true, HasCluster: true, HasEvent: true}, im.StatusUnsupportedEvent},
		{"cluster the endpoint lacks", at(2, 0x0300), im.StatusUnsupportedCluster},
		{"endpoint the node lacks", at(9, 0x0028), im.StatusUnsupportedEndpoint},
	} {
		if got := d.EventPathStatus(c.path); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
