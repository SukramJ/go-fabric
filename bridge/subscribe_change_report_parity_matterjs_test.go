// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
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
