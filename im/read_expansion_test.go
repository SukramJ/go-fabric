// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"context"
	"reflect"
	"slices"
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
// path's results in ascending (endpoint, cluster, attribute) order, the
// paths in request order.
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
		for a := range uint32(3) {
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
