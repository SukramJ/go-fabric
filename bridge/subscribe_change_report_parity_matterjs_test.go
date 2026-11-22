// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/cluster/onoff"
	"thingfabric.dev/go-fabric/contract"
	endpointpkg "thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/im/subscription"
)

// TestSubscriptionAttributeReportsCarryNoStatus (review II,
// R2-P4-imread-09): a change report carries the values of the changed
// paths and no status — a changed path whose read no longer succeeds (its
// endpoint removed) is left out. Mirrors matter.js
// packages/protocol/src/action/server/AttributeSubscriptionResponse.ts:91-93
// at db021da0 (addStatus suppressed "For Filtered responses"), which
// packages/node/src/node/server/ServerSubscription.ts:860-868 builds every
// update through.
func TestSubscriptionAttributeReportsCarryNoStatus(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	served := im.ConcreteAttributePath{Endpoint: 2, Cluster: 0x0006, Attribute: 0x0000, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	gone := im.ConcreteAttributePath{Endpoint: 9, Cluster: 0x0006, Attribute: 0x0000, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	dispatcher := pathDispatcher{
		served: {{Path: served, Value: im.AttributeValue{Value: true}, Status: im.StatusSuccess}},
		gone:   {{Path: gone, Status: im.StatusUnsupportedEndpoint}},
	}
	sub := &subscription.Subscription{AttributePaths: []im.ConcreteAttributePath{{}}}
	reports := b.subscriptionAttributeReports(context.Background(), dispatcher, subTarget{pase: true}, sub, []im.ConcreteAttributePath{gone, served})
	if len(reports) != 1 || reports[0].IsStatus || reports[0].Path != served {
		t.Fatalf("reports %+v, want the value of %+v alone", reports, served)
	}
}

// TestPrimingReportStatusesFirst (review II, R2-P4-imread-06): the
// statuses of a priming report's concrete paths precede its first value.
// Mirrors matter.js packages/node/src/node/server/ServerSubscription.ts:
// 643-671 at db021da0, which emits AttributeReadResponse's reports — every
// status of the request first (AttributeReadResponse.ts:88-100) — holding
// the statuses back until the first value.
func TestPrimingReportStatusesFirst(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	served := im.ConcreteAttributePath{Endpoint: 2, Cluster: 0x0006, Attribute: 0x0000, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	gone := im.ConcreteAttributePath{Endpoint: 9, Cluster: 0x0006, Attribute: 0x0000, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	dispatcher := pathDispatcher{
		served: {{Path: served, Value: im.AttributeValue{Value: true}, Status: im.StatusSuccess}},
		gone:   {{Path: gone, Status: im.StatusUnsupportedEndpoint}},
	}
	report, matched := b.buildInitialReport(im.WithAuthModePASE(context.Background()), dispatcher, im.SubscribeRequest{
		AttributeRequests: []im.ConcreteAttributePath{served, gone},
	})
	if matched != 1 || len(report.Reports) != 2 {
		t.Fatalf("matched %d, reports %+v, want 1 and two reports", matched, report.Reports)
	}
	if first, second := report.Reports[0], report.Reports[1]; !first.IsStatus || first.Path != gone || second.IsStatus || second.Path != served {
		t.Fatalf("reports %+v, want the status of %+v before the value of %+v", report.Reports, gone, served)
	}
}

// pathDispatcher answers each path it holds with the results it holds for
// it, any other path with nothing.
type pathDispatcher map[im.ConcreteAttributePath][]im.ReadResult

func (d pathDispatcher) Read(_ context.Context, p im.ConcreteAttributePath) []im.ReadResult {
	return d[p]
}

func (pathDispatcher) Write(context.Context, im.ConcreteAttributePath, im.AttributeValue) []im.WriteResult {
	return nil
}

func (pathDispatcher) Invoke(context.Context, im.ConcreteCommandPath, any) im.InvokeResult {
	return im.InvokeResult{}
}

// countingTopology is a TopologyDispatcher that counts the Read of each
// path.
type countingTopology struct {
	*endpointpkg.TopologyDispatcher
	mu    sync.Mutex
	reads map[im.ConcreteAttributePath]int
}

func (d *countingTopology) Read(ctx context.Context, p im.ConcreteAttributePath) []im.ReadResult {
	d.mu.Lock()
	d.reads[p]++
	d.mu.Unlock()
	return d.TopologyDispatcher.Read(ctx, p)
}

// TestPrimingReportReadsEachPathOnce (review II, R2-P5-write-06): a
// priming report over the endpoint dispatcher decides its concrete paths'
// statuses without reading them (im.AttributePathChecker) — an absent
// endpoint or cluster is not read — and reads every other concrete path
// once, the statuses still first. matter.js decides a concrete path's
// status from the model (AttributeReadResponse.ts:255-270 at db021da0)
// and reads its value once, in the producer (:283-308).
func TestPrimingReportReadsEachPathOnce(t *testing.T) {
	t.Parallel()
	srv, err := onoff.New(onoff.Config{Features: onoff.FeatureLT, Bridged: true})
	if err != nil {
		t.Fatal(err)
	}
	h := newAppHarness(t, appDevice{deviceType: 0x0100, servers: []contract.ClusterServer{srv}})
	epID := h.endpoints[0x0100]
	at := func(ep uint16, cluster, attr uint32) im.ConcreteAttributePath {
		return im.ConcreteAttributePath{Endpoint: ep, Cluster: cluster, Attribute: attr, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	}
	onOff, onTime := at(epID, onoff.ClusterID, onoff.AttrOnOff), at(epID, onoff.ClusterID, onoff.AttrOnTime)
	noCluster, noEndpoint := at(epID, 0x0999, 0), at(99, onoff.ClusterID, onoff.AttrOnOff)
	d := &countingTopology{TopologyDispatcher: endpointpkg.NewTopologyDispatcher(h.bridge.Topology()), reads: map[im.ConcreteAttributePath]int{}}
	report, matched := h.bridge.buildInitialReport(im.WithAuthModePASE(context.Background()), d, im.SubscribeRequest{
		AttributeRequests: []im.ConcreteAttributePath{onOff, noCluster, onTime, noEndpoint},
	})
	if matched != 2 || len(report.Reports) != 4 {
		t.Fatalf("matched %d, reports %+v; want 2 and four reports", matched, report.Reports)
	}
	for i, want := range []im.ConcreteAttributePath{noCluster, noEndpoint, onOff, onTime} {
		if r := report.Reports[i]; r.Path != want || r.IsStatus != (i < 2) {
			t.Errorf("report %d: %+v, want %+v (status %v)", i, r, want, i < 2)
		}
	}
	if want := map[im.ConcreteAttributePath]int{onOff: 1, onTime: 1}; !reflect.DeepEqual(d.reads, want) {
		t.Errorf("reads %v, want %v", d.reads, want)
	}
}
