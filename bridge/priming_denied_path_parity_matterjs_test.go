// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"log/slog"
	"testing"

	"thingfabric.dev/go-fabric/im"
)

// endpointZeroDenied grants the subject everything but endpoint 0, which it
// answers with status.
type endpointZeroDenied struct {
	fakeReadOnlyDispatcher
	status im.StatusCode
}

func (d *endpointZeroDenied) CheckACL(_ context.Context, _ uint8, _ uint64, _ []uint32, ep uint16, _ uint32, _ uint8) im.StatusCode {
	if ep == 0 {
		return d.status
	}
	return im.StatusSuccess
}

func (d *endpointZeroDenied) Read(_ context.Context, p im.ConcreteAttributePath) []im.ReadResult {
	if !p.HasEndpoint {
		p0, p1 := p, p
		p0.Endpoint, p0.HasEndpoint = 0, true
		p1.Endpoint, p1.HasEndpoint = 1, true
		return []im.ReadResult{{Path: p0, Status: im.StatusSuccess}, {Path: p1, Status: im.StatusSuccess}}
	}
	return []im.ReadResult{{Path: p, Status: im.StatusSuccess}}
}

// TestPrimingReportAnswersADeniedConcretePath (review II, R2-P4-nv-02): a
// concrete path the subject may not read is answered in the priming report
// with the access check's status, ahead of the values, and counts for no
// existent attribute; a wildcard path leaves the denied endpoint out. A
// re-established subscription reports no status. Mirrors matter.js at
// db021da0: AttributeReadResponse #addConcrete answers a denied concrete
// path with a status at its View pass and at its read-level pass
// (packages/protocol/src/action/server/AttributeReadResponse.ts:250-252,
// :272-274, #authorize :462-484) and does not count it as existent
// (:125-131); the priming report passes the statuses through, held back
// until the first value (packages/node/src/node/server/ServerSubscription.ts:643-671),
// as a Read answers it (im.ReadReports).
func TestPrimingReportAnswersADeniedConcretePath(t *testing.T) {
	t.Parallel()
	denied := accessControlACLPath()
	allowed := im.ConcreteAttributePath{Endpoint: 1, HasEndpoint: true, Cluster: 6, HasCluster: true, Attribute: 0, HasAttribute: true}
	wildcard := im.ConcreteAttributePath{Cluster: 6, HasCluster: true, Attribute: 0, HasAttribute: true}
	ctx := im.WithSubject(im.WithFabricFilter(context.Background(), true, 1), 0x1111, nil)
	b := &Bridge{logger: slog.New(slog.DiscardHandler)}

	for _, status := range []im.StatusCode{im.StatusUnsupportedAccess, im.StatusAccessRestricted} {
		d := &endpointZeroDenied{status: status}
		p := b.newPrimingSource(ctx, d, im.SubscribeRequest{AttributeRequests: []im.ConcreteAttributePath{allowed, denied, wildcard}})
		var got []im.AttributeReport
		for rep, ok := p.next(); ok; rep, ok = p.next() {
			got = append(got, rep)
		}
		if len(got) != 3 || !got[0].IsStatus || got[0].Path != denied || got[0].Status.Status != status ||
			got[1].IsStatus || got[1].Path != allowed || got[2].IsStatus || got[2].Path.Endpoint != 1 {
			t.Fatalf("checker status %v: priming reports %+v, want the status of %+v, then the values at endpoint 1", status, got, denied)
		}
		if p.matched != 2 {
			t.Errorf("checker status %v: %d existent attributes, want 2 (the denied path counts for none)", status, p.matched)
		}

		re := b.newPrimingSource(ctx, d, im.SubscribeRequest{AttributeRequests: []im.ConcreteAttributePath{denied, allowed}})
		re.suppressStatuses = true
		if rep, ok := re.next(); !ok || rep.IsStatus || rep.Path != allowed {
			t.Errorf("checker status %v, statuses suppressed: first report %+v (%t), want the value of %+v", status, rep, ok, allowed)
		}
	}

	// A subscription whose only concrete path is denied matches nothing.
	if b.newPrimingSource(ctx, &endpointZeroDenied{status: im.StatusUnsupportedAccess}, im.SubscribeRequest{AttributeRequests: []im.ConcreteAttributePath{denied}}).matches() {
		t.Error("a denied concrete path established the subscription")
	}
}

// countingReads is an endpointZeroDenied that counts the dispatcher reads.
type countingReads struct {
	endpointZeroDenied
	reads int
}

func (d *countingReads) Read(ctx context.Context, p im.ConcreteAttributePath) []im.ReadResult {
	d.reads++
	return d.endpointZeroDenied.Read(ctx, p)
}

// TestPrimingMatchesStopsAtTheFirstMatch: whether a Subscribe matches
// anything (matter.js ServerSubscription.ts:707-711 at db021da0, "no
// attributes or events are matching the query") is decided by reading the
// request's paths in turn up to the first readable attribute; the order the
// priming report gives its statuses plays no part in it.
func TestPrimingMatchesStopsAtTheFirstMatch(t *testing.T) {
	t.Parallel()
	ctx := im.WithSubject(im.WithFabricFilter(context.Background(), true, 1), 0x1111, nil)
	b := &Bridge{logger: slog.New(slog.DiscardHandler)}
	paths := make([]im.ConcreteAttributePath, 5)
	for i := range paths {
		paths[i] = im.ConcreteAttributePath{Endpoint: 1, HasEndpoint: true, Cluster: 6, HasCluster: true, Attribute: uint32(i), HasAttribute: true}
	}
	d := &countingReads{endpointZeroDenied: endpointZeroDenied{status: im.StatusUnsupportedAccess}}
	if !b.primingMatches(ctx, d, im.SubscribeRequest{AttributeRequests: paths}) {
		t.Fatal("a readable concrete path did not match")
	}
	if d.reads != 1 {
		t.Errorf("%d dispatcher reads to find the first match, want 1", d.reads)
	}
}
