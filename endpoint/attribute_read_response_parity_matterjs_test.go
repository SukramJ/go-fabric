// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

// Ports of matter.js packages/node/test/node/AttributeReadResponseTest.ts at
// b25ed960, run through the module's read path (im.HandleReadRequest over a
// TopologyDispatcher): a status answers only a fully concrete request path.
// A path with a wildcard selects what exists and nothing else — matter.js
// packages/protocol/src/action/server/AttributeReadResponse.ts at db021da0
// sends any path missing an endpoint, cluster or attribute to addWildcard
// (:88-94), installs nothing for a named endpoint it lacks (:190-191),
// skips a named cluster an endpoint lacks (:341) and a named attribute a
// cluster lacks (:386-387); addStatus (:489) is reached from addConcrete
// and #authorize only.

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/im"
)

// wildcardStatusTopology is root (0), aggregator (1) and endpoint 2 serving
// OnOff (0x0006) with FeatureMap and ClusterRevision only.
func wildcardStatusTopology() *Topology {
	return makeTopology(&Endpoint{ID: 2, Source: singleServerSource{srv: &globalAttrServer{id: 0x0006}}})
}

func TestAttributeReadResponseWildcardStatusParity(t *testing.T) {
	t.Parallel()
	ctx := im.WithAuthModePASE(context.Background())
	read := func(t *testing.T, p im.ConcreteAttributePath) []im.AttributeReport {
		t.Helper()
		d := NewTopologyDispatcher(wildcardStatusTopology())
		return im.HandleReadRequest(ctx, d, im.ReadRequest{AttributeRequests: []im.ConcreteAttributePath{p}}).Reports
	}

	// AttributeReadResponseTest.ts:110-135 "reads non-existent concrete
	// endpoint": one AttributeStatus(UNSUPPORTED_ENDPOINT) for the path.
	t.Run("reads non-existent concrete endpoint", func(t *testing.T) {
		t.Parallel()
		p := im.ConcreteAttributePath{Endpoint: 9, Cluster: 0x0006, Attribute: 0xFFFD, HasEndpoint: true, HasCluster: true, HasAttribute: true}
		got := read(t, p)
		if len(got) != 1 || !got[0].IsStatus || got[0].Status.Status != im.StatusUnsupportedEndpoint || got[0].Path != p {
			t.Fatalf("reports %+v, want one UNSUPPORTED_ENDPOINT for %+v", got, p)
		}
	})

	// Its wildcard twins: the same absent endpoint, cluster or attribute
	// named beside a wildcard selects nothing and answers nothing.
	for name, p := range map[string]im.ConcreteAttributePath{
		"absent endpoint, wildcard attribute":             {Endpoint: 9, Cluster: 0x0006, HasEndpoint: true, HasCluster: true},
		"absent endpoint, wildcard cluster":               {Endpoint: 9, Attribute: 0xFFFD, HasEndpoint: true, HasAttribute: true},
		"absent endpoint, wildcard cluster and attribute": {Endpoint: 9, HasEndpoint: true},
		"absent cluster, wildcard attribute":              {Endpoint: 2, Cluster: 0x0999, HasEndpoint: true, HasCluster: true},
		"absent attribute, wildcard endpoint":             {Cluster: 0x0006, Attribute: 0x4000, HasCluster: true, HasAttribute: true},
		"absent global, wildcard endpoint and cluster":    {Attribute: 0xFFFA, HasAttribute: true},
		"absent global, wildcard cluster":                 {Endpoint: 2, Attribute: 0xFFFA, HasEndpoint: true, HasAttribute: true},
	} {
		t.Run("reads non-existent wildcard path: "+name, func(t *testing.T) {
			t.Parallel()
			if got := read(t, p); len(got) != 0 {
				t.Fatalf("reports %+v, want none: a wildcard path answers no status", got)
			}
		})
	}

	// AttributeReadResponseTest.ts:259-285 "reads attributeList global
	// Attribute full wildcard": a value per cluster, status 0 (endpoint 2
	// serves the clusters its materialisation adds beside OnOff).
	t.Run("reads attributeList global Attribute full wildcard", func(t *testing.T) {
		t.Parallel()
		got := read(t, im.ConcreteAttributePath{Attribute: 0xFFFB, HasAttribute: true})
		onOff := false
		for _, r := range got {
			if r.IsStatus || r.Path.Endpoint != 2 || r.Path.Attribute != 0xFFFB {
				t.Fatalf("report %+v, want AttributeList values of endpoint 2 only", r)
			}
			onOff = onOff || r.Path.Cluster == 0x0006
		}
		if !onOff {
			t.Fatalf("reports %+v, want one for 2/0x0006", got)
		}
	})
}
