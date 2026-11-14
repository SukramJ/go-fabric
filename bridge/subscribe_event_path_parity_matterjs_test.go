// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/im"
)

// eventPathDispatcher is a node whose endpoint 0 serves BasicInformation
// (0x0028) with the StartUp event (0x00) and no ReachableChanged (0x03).
type eventPathDispatcher struct{ pathDispatcher }

func (eventPathDispatcher) EventPathStatus(p im.ConcreteEventPath) im.StatusCode {
	switch {
	case p.Endpoint != 0:
		return im.StatusUnsupportedEndpoint
	case p.Cluster != 0x0028:
		return im.StatusUnsupportedCluster
	case p.Event != 0x00:
		return im.StatusUnsupportedEvent
	}
	return im.StatusSuccess
}

// TestAllowedEventPathsCountsExistingEvents (review II, R2-P4-imread-08)
// ports matter.js packages/node/test/node/EventReadResponseTest.ts:299-326
// at db021da0, "reads non-existent concrete event": an event the cluster
// does not have is no existent event path (counts.existent 0), so it does
// not establish a subscription on its own. Mirrors EventReadResponse.ts at
// db021da0: #addConcrete answers UnsupportedEvent before it registers the
// path (:216-218, :235) and a wildcard endpoint registers an event only
// where its cluster has it (:286-291); ServerSubscription.ts:707-711
// refuses a subscription with no existent attribute or event path.
func TestAllowedEventPathsCountsExistingEvents(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	revision := func(ep uint16) im.ConcreteAttributePath {
		return im.ConcreteAttributePath{Endpoint: ep, Cluster: 0x0028, Attribute: clusterRevisionAttributeID, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	}
	d := eventPathDispatcher{pathDispatcher{
		revision(0): {{Path: revision(0), Value: im.AttributeValue{Value: uint16(4)}, Status: im.StatusSuccess}},
		{Cluster: 0x0028, Attribute: clusterRevisionAttributeID, HasCluster: true, HasAttribute: true}: {
			{Path: revision(0), Value: im.AttributeValue{Value: uint16(4)}, Status: im.StatusSuccess},
		},
	}}
	auth := im.EventReadAuthorizer{PASE: true, Paths: d}
	for _, tc := range []struct {
		name string
		path im.ConcreteEventPath
		want int
	}{
		{"concrete event", im.ConcreteEventPath{Endpoint: 0, Cluster: 0x0028, Event: 0x00, HasEndpoint: true, HasCluster: true, HasEvent: true}, 1},
		{"non-existent concrete event", im.ConcreteEventPath{Endpoint: 0, Cluster: 0x0028, Event: 0x03, HasEndpoint: true, HasCluster: true, HasEvent: true}, 0},
		{"non-existent event, wildcard endpoint", im.ConcreteEventPath{Cluster: 0x0028, Event: 0x03, HasCluster: true, HasEvent: true}, 0},
		{"event, wildcard endpoint", im.ConcreteEventPath{Cluster: 0x0028, Event: 0x00, HasCluster: true, HasEvent: true}, 1},
	} {
		if got := b.allowedEventPaths(context.Background(), d, auth, []im.ConcreteEventPath{tc.path}); got != tc.want {
			t.Errorf("%s: %d allowed event paths, want %d", tc.name, got, tc.want)
		}
	}
}
