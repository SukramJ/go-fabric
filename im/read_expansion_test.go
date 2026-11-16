// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"context"
	"reflect"
	"slices"
	"sync"
	"testing"
)

// endpointsDispatcher is a node of endpoints listed in eps (in its own,
// unsorted order), each with attrs attributes of cluster 6, that counts
// the results it hands out.
type endpointsDispatcher struct {
	fakeDispatcher
	eps      []uint16
	attrs    int
	produced int
}

func (d *endpointsDispatcher) at(ep uint16) []ReadResult {
	out := make([]ReadResult, 0, d.attrs)
	for a := d.attrs - 1; a >= 0; a-- { // descending: Ascending must sort
		out = append(out, ReadResult{
			Path:  ConcreteAttributePath{Endpoint: ep, Cluster: 6, Attribute: uint32(a), HasEndpoint: true, HasCluster: true, HasAttribute: true},
			Value: AttributeValue{Value: uint32(a)}, Status: StatusSuccess,
		})
	}
	d.produced += len(out)
	return out
}

func (d *endpointsDispatcher) Read(_ context.Context, p ConcreteAttributePath) []ReadResult {
	if p.HasEndpoint {
		return d.at(p.Endpoint)
	}
	var out []ReadResult
	for _, ep := range d.eps {
		out = append(out, d.at(ep)...)
	}
	return out
}

func (d *endpointsDispatcher) ExpansionEndpoints(ConcreteAttributePath) []uint16 { return d.eps }

func (d *endpointsDispatcher) ReadAtEndpoint(_ context.Context, _ ConcreteAttributePath, ep uint16) []ReadResult {
	return d.at(ep)
}

// nonExpandingDispatcher hides the EndpointExpander of the dispatcher it wraps.
type nonExpandingDispatcher struct{ d *endpointsDispatcher }

func (p nonExpandingDispatcher) Read(ctx context.Context, path ConcreteAttributePath) []ReadResult {
	return p.d.Read(ctx, path)
}

func (p nonExpandingDispatcher) Write(ctx context.Context, path ConcreteAttributePath, v AttributeValue) []WriteResult {
	return p.d.Write(ctx, path, v)
}

func (p nonExpandingDispatcher) Invoke(ctx context.Context, path ConcreteCommandPath, f any) InvokeResult {
	return p.d.Invoke(ctx, path, f)
}

// TestAttributeExpansionHoldsOneEndpoint pins the cursor of decision 44
// (review II): a wildcard path is expanded one endpoint at a time — no
// more than one endpoint's results are produced ahead of the consumer —
// paths in request order, and the read response is the same with and
// without the per-endpoint expansion.
func TestAttributeExpansionHoldsOneEndpoint(t *testing.T) {
	t.Parallel()
	paths := []ConcreteAttributePath{{}, {Endpoint: 1, HasEndpoint: true}, {Cluster: 6, HasCluster: true}}
	d := &endpointsDispatcher{eps: []uint16{5, 1, 3}, attrs: 4}
	e := NewAttributeExpansion(context.Background(), d, paths)
	taken := 0
	for {
		_, _, ok := e.Next()
		if !ok {
			break
		}
		taken++
		if ahead := d.produced - taken; ahead >= d.attrs {
			t.Fatalf("after %d results the dispatcher produced %d: %d ahead, want fewer than one endpoint's %d", taken, d.produced, ahead, d.attrs)
		}
	}
	if want := (3 + 1 + 3) * d.attrs; taken != want {
		t.Errorf("expansion yielded %d results, want %d", taken, want)
	}

	streamed := HandleReadRequest(context.Background(), &endpointsDispatcher{eps: d.eps, attrs: d.attrs}, ReadRequest{AttributeRequests: paths})
	whole := HandleReadRequest(context.Background(), nonExpandingDispatcher{&endpointsDispatcher{eps: d.eps, attrs: d.attrs}}, ReadRequest{AttributeRequests: paths})
	if !reflect.DeepEqual(streamed, whole) {
		t.Errorf("read response with the per-endpoint expansion differs from Read's:\n%+v\n%+v", streamed.Reports, whole.Reports)
	}
}

// TestAttributeExpansionAscendingSortsEachPath pins the priming order: a
// path's results in ascending (endpoint, cluster) order, a cluster's
// attributes in [CompareAttributeIDs] order, the paths in request order.
// Of the fake's OnOff attributes 0-2, only OnOff (0x0000) is in matter.js's
// model of the cluster; 1 and 2 are not and come before it.
func TestAttributeExpansionAscendingSortsEachPath(t *testing.T) {
	t.Parallel()
	d := &endpointsDispatcher{eps: []uint16{5, 1, 3}, attrs: 3}
	paths := []ConcreteAttributePath{{Endpoint: 3, HasEndpoint: true}, {}}
	var got []ConcreteAttributePath
	e := NewAttributeExpansion(context.Background(), d, paths).Ascending()
	for _, r, ok := e.Next(); ok; _, r, ok = e.Next() {
		got = append(got, ConcreteAttributePath{Endpoint: r.Path.Endpoint, Attribute: r.Path.Attribute})
	}
	want := make([]ConcreteAttributePath, 0, 12)
	for _, ep := range []uint16{3, 1, 3, 5} {
		for _, a := range []uint32{1, 2, 0} {
			want = append(want, ConcreteAttributePath{Endpoint: ep, Attribute: a})
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ascending expansion order = %v, want %v", got, want)
	}
}

// retainedDispatcher answers every read with the one slice it keeps, as a
// host's cache may.
type retainedDispatcher struct {
	fakeDispatcher
	results []ReadResult
}

func (d *retainedDispatcher) Read(context.Context, ConcreteAttributePath) []ReadResult {
	return d.results
}

// TestAttributeExpansionLeavesTheDispatchersSlice (review II,
// R2-P2-dr-req-04): the expansion reads the slice [Dispatcher.Read]
// returns and writes nothing into it — neither clears the entries it hands
// out nor sorts it in place under Ascending — so a dispatcher that keeps
// and returns the same slice answers every read alike. The Dispatcher
// contract does not hand the slice over.
func TestAttributeExpansionLeavesTheDispatchersSlice(t *testing.T) {
	t.Parallel()
	for _, ascending := range []bool{false, true} {
		path := func(attr uint32) ConcreteAttributePath {
			return ConcreteAttributePath{Endpoint: 3, Cluster: 6, Attribute: attr, HasEndpoint: true, HasCluster: true, HasAttribute: true}
		}
		d := &retainedDispatcher{results: []ReadResult{
			{Path: path(2), Value: AttributeValue{Value: true}, Status: StatusSuccess},
			{Path: path(1), Value: AttributeValue{Value: false}, Status: StatusSuccess},
		}}
		want := slices.Clone(d.results)
		request := []ConcreteAttributePath{{Endpoint: 3, Cluster: 6, HasEndpoint: true, HasCluster: true}}
		for read := range 2 {
			e := NewAttributeExpansion(context.Background(), d, request)
			if ascending {
				e.Ascending()
			}
			var got []ReadResult
			for {
				_, r, ok := e.Next()
				if !ok {
					break
				}
				got = append(got, r)
			}
			wantOrder := want
			if ascending {
				wantOrder = []ReadResult{want[1], want[0]}
			}
			if !reflect.DeepEqual(got, wantOrder) {
				t.Fatalf("ascending=%v read %d: results %+v, want %+v", ascending, read, got, wantOrder)
			}
			if !reflect.DeepEqual(d.results, want) {
				t.Fatalf("ascending=%v read %d: the dispatcher's slice became %+v, want it untouched %+v", ascending, read, d.results, want)
			}
		}
	}
}

// TestAttributeExpansionStatusOnlyForConcretePath (review II,
// R2-P4-imread-02, -03): whatever a dispatcher answers, the expansion hands
// out a status only for a fully concrete request path; a path with a
// wildcard yields its successful results alone. Mirrors matter.js
// packages/protocol/src/action/server/AttributeReadResponse.ts at db021da0:
// addStatus (:489) is reached from addConcrete and #authorize only, a
// wildcard path (:88-94) skips what does not exist (:190-191, :341,
// :386-387).
func TestAttributeExpansionStatusOnlyForConcretePath(t *testing.T) {
	t.Parallel()
	at := func(attr uint32) ConcreteAttributePath {
		return ConcreteAttributePath{Endpoint: 3, Cluster: 6, Attribute: attr, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	}
	d := &retainedDispatcher{results: []ReadResult{
		{Path: at(1), Status: StatusUnsupportedAttribute},
		{Path: at(2), Value: AttributeValue{Value: true}, Status: StatusSuccess},
	}}
	for _, tc := range []struct {
		name string
		path ConcreteAttributePath
		want []ReadResult
	}{
		{"wildcard attribute", ConcreteAttributePath{Endpoint: 3, Cluster: 6, HasEndpoint: true, HasCluster: true}, d.results[1:]},
		{"wildcard endpoint", ConcreteAttributePath{Cluster: 6, Attribute: 1, HasCluster: true, HasAttribute: true}, d.results[1:]},
		{"wildcard cluster", ConcreteAttributePath{Endpoint: 3, Attribute: 1, HasEndpoint: true, HasAttribute: true}, d.results[1:]},
		{"concrete", at(1), d.results},
	} {
		var got []ReadResult
		e := NewAttributeExpansion(context.Background(), d, []ConcreteAttributePath{tc.path})
		for _, r, ok := e.Next(); ok; _, r, ok = e.Next() {
			got = append(got, r)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: results %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

// statusOrderDispatcher answers endpoint 2: OnOff (0x0006) serves 0x0000
// and 0xFFFD, cluster 0x0999 is absent, and the fake ACL denies cluster
// 0x0300 on fabric 1.
type statusOrderDispatcher struct{ fakeDispatcher }

func (statusOrderDispatcher) Read(_ context.Context, p ConcreteAttributePath) []ReadResult {
	value := func(attr uint32) ReadResult {
		return ReadResult{
			Path:  ConcreteAttributePath{Endpoint: 2, Cluster: p.Cluster, Attribute: attr, HasEndpoint: true, HasCluster: true, HasAttribute: true},
			Value: AttributeValue{Value: attr}, Status: StatusSuccess,
		}
	}
	switch {
	case p.Cluster == 0x0999:
		return []ReadResult{{Path: p, Status: StatusUnsupportedCluster}}
	case p.HasAttribute:
		return []ReadResult{value(p.Attribute)}
	default:
		return []ReadResult{value(0x0000), value(0xFFFD)}
	}
}

func (statusOrderDispatcher) CheckACL(_ context.Context, _ uint8, _ uint64, _ []uint32, _ uint16, clusterID uint32, _ uint8) StatusCode {
	if clusterID == 0x0300 {
		return StatusUnsupportedAccess
	}
	return StatusSuccess
}

// TestReadReportsStatusesFirst (review II, R2-P4-imread-06): the statuses
// of a read's concrete paths — an existence status and an access denial —
// precede every value, in request order; the values follow in the
// request's path order. Mirrors matter.js
// packages/protocol/src/action/server/AttributeReadResponse.ts at db021da0:
// process registers every path first (:88-94), addConcrete adds a failing
// path's status to the chunk at once (:202-274, addStatus :489-501) while a
// resolving path only installs a producer (:283-308), and the producers run
// afterwards (:96-100).
func TestReadReportsStatusesFirst(t *testing.T) {
	t.Parallel()
	at := func(cluster, attr uint32) ConcreteAttributePath {
		return ConcreteAttributePath{Endpoint: 2, Cluster: cluster, Attribute: attr, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	}
	req := ReadRequest{AttributeRequests: []ConcreteAttributePath{
		at(0x0006, 0xFFFD),
		at(0x0999, 0x0000),
		at(0x0300, 0x0000),
		{Endpoint: 2, Cluster: 0x0006, HasEndpoint: true, HasCluster: true},
	}}
	ctx := WithFabricFilter(context.Background(), true, 1)
	got := HandleReadRequest(ctx, &statusOrderDispatcher{}, req).Reports
	type entry struct {
		path   ConcreteAttributePath
		status StatusCode
	}
	want := []entry{
		{at(0x0999, 0x0000), StatusUnsupportedCluster},
		{at(0x0300, 0x0000), StatusUnsupportedAccess},
		{at(0x0006, 0xFFFD), StatusSuccess},
		{at(0x0006, 0x0000), StatusSuccess},
		{at(0x0006, 0xFFFD), StatusSuccess},
	}
	order := make([]entry, 0, len(got))
	for _, r := range got {
		st := StatusSuccess
		if r.IsStatus {
			st = r.Status.Status
		}
		order = append(order, entry{r.Path, st})
	}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("report order %+v, want %+v", order, want)
	}
}

// checkingStatusOrderDispatcher is statusOrderDispatcher with an
// AttributePathChecker that answers the absent cluster without a read; it
// counts the reads of each concrete path.
type checkingStatusOrderDispatcher struct {
	statusOrderDispatcher
	mu    sync.Mutex
	reads map[ConcreteAttributePath]int
}

func (d *checkingStatusOrderDispatcher) Read(ctx context.Context, p ConcreteAttributePath) []ReadResult {
	d.mu.Lock()
	if d.reads == nil {
		d.reads = map[ConcreteAttributePath]int{}
	}
	d.reads[p]++
	d.mu.Unlock()
	return d.statusOrderDispatcher.Read(ctx, p)
}

func (*checkingStatusOrderDispatcher) AttributePathStatus(_ context.Context, p ConcreteAttributePath) StatusCode {
	if p.Cluster == 0x0999 {
		return StatusUnsupportedCluster
	}
	return StatusSuccess
}

// TestReadReportsStatusesFirstReadsEachPathOnce (review II,
// R2-P5-write-06): with a dispatcher that decides a concrete path's status
// without a read (AttributePathChecker) the report's order is the one
// TestReadReportsStatusesFirst holds, and no concrete path is read more
// than once — a path whose status the checker or the access gate decides
// is not read at all. matter.js decides a concrete path's status from the
// model and access (AttributeReadResponse.ts:250, :255-270, :272 at
// db021da0) and reads its value once, in the producer (:283-308).
func TestReadReportsStatusesFirstReadsEachPathOnce(t *testing.T) {
	t.Parallel()
	at := func(cluster, attr uint32) ConcreteAttributePath {
		return ConcreteAttributePath{Endpoint: 2, Cluster: cluster, Attribute: attr, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	}
	req := ReadRequest{AttributeRequests: []ConcreteAttributePath{
		at(0x0006, 0xFFFD),
		at(0x0999, 0x0000),
		at(0x0300, 0x0000),
		{Endpoint: 2, Cluster: 0x0006, HasEndpoint: true, HasCluster: true},
	}}
	ctx := WithFabricFilter(context.Background(), true, 1)
	d := &checkingStatusOrderDispatcher{}
	got := HandleReadRequest(ctx, d, req).Reports
	want := []struct {
		path   ConcreteAttributePath
		status StatusCode
	}{
		{at(0x0999, 0x0000), StatusUnsupportedCluster},
		{at(0x0300, 0x0000), StatusUnsupportedAccess},
		{at(0x0006, 0xFFFD), StatusSuccess},
		{at(0x0006, 0x0000), StatusSuccess},
		{at(0x0006, 0xFFFD), StatusSuccess},
	}
	if len(got) != len(want) {
		t.Fatalf("reports %+v, want %d", got, len(want))
	}
	for i, r := range got {
		st := StatusSuccess
		if r.IsStatus {
			st = r.Status.Status
		}
		if r.Path != want[i].path || st != want[i].status {
			t.Errorf("report %d: %+v %v, want %+v %v", i, r.Path, st, want[i].path, want[i].status)
		}
		if !r.IsStatus && r.Value.Value == nil {
			t.Errorf("report %d: a value report without its value", i)
		}
	}
	wantReads := map[ConcreteAttributePath]int{at(0x0006, 0xFFFD): 1, req.AttributeRequests[3]: 1}
	if !reflect.DeepEqual(d.reads, wantReads) {
		t.Errorf("reads %v, want %v", d.reads, wantReads)
	}
}
