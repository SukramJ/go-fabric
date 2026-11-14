// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"reflect"
	"testing"

	"github.com/SukramJ/go-fabric/im"
)

// TestReadAtEndpointConcatenatesToRead pins that the per-endpoint
// expansion ([im.EndpointExpander]) returns what Read returns, endpoint by
// endpoint in Read's order, for the wildcard paths an expansion walks
// (review II, decision 44).
func TestReadAtEndpointConcatenatesToRead(t *testing.T) {
	t.Parallel()
	srv1 := &fakeServerFull{id: 0x0006, readVal: uint8(0), readOK: true}
	srv2 := &fakeServerFull{id: 0x0006, readVal: uint8(1), readOK: true}
	d := NewTopologyDispatcher(makeTopology(makeEndpointFull(3, srv2), makeEndpointFull(2, srv1)))
	for _, path := range []im.ConcreteAttributePath{
		{},
		{Cluster: 0x0006, HasCluster: true},
		{Cluster: 0x0006, Attribute: 0x0000, HasCluster: true, HasAttribute: true},
		{Cluster: 0x0008, HasCluster: true},
	} {
		want := d.Read(context.Background(), path)
		got := make([]im.ReadResult, 0, len(want))
		for _, ep := range d.ExpansionEndpoints(path) {
			got = append(got, d.ReadAtEndpoint(context.Background(), path, ep)...)
		}
		if len(got) != len(want) || (len(want) > 0 && !reflect.DeepEqual(got, want)) {
			t.Errorf("path %+v: per-endpoint expansion = %+v, Read = %+v", path, got, want)
		}
	}
	if got := d.ReadAtEndpoint(context.Background(), im.ConcreteAttributePath{}, 99); got != nil {
		t.Errorf("ReadAtEndpoint(missing endpoint) = %+v, want nothing", got)
	}
}
