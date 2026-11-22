// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"testing"

	"thingfabric.dev/go-fabric/bridge"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/tlv"
)

// wireSource serves one cluster server on an endpoint.
type wireSource struct{ srv contract.ClusterServer }

func (wireSource) MatterDeviceType() uint16 { return 0x0100 }
func (s wireSource) MatterClusterServers() []contract.ClusterServer {
	return []contract.ClusterServer{s.srv}
}

// readWire is srv's attribute attr as the request ctx carries reads it on
// the wire: the dispatcher's read (endpoint.TopologyDispatcher, which
// attaches the scope of a fabric-scoped attribute to a request's result)
// written by the bridge's writer with that scope applied
// (im.MaskFabricScoped), as an attribute report's encoder applies it.
func readWire(ctx context.Context, t *testing.T, srv contract.ClusterServer, attr uint32) []byte {
	t.Helper()
	ep := &endpoint.Endpoint{ID: 1, DeviceType: 0x0100, SourceKey: endpoint.StringKey("wire"), Scope: "s", DeviceAddress: "d", Source: wireSource{srv}}
	d := endpoint.NewTopologyDispatcher(&endpoint.Topology{Endpoints: []*endpoint.Endpoint{ep}})
	results := d.Read(ctx, im.ConcreteAttributePath{Endpoint: 1, Cluster: srv.MatterClusterID(), Attribute: attr, HasEndpoint: true, HasCluster: true, HasAttribute: true})
	if len(results) != 1 || results[0].Status != im.StatusSuccess {
		t.Fatalf("read of 0x%04X: %+v", attr, results)
	}
	b, err := bridge.AttributeValueTLV(results[0].Value.Value)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].FabricScope == nil {
		return b
	}
	masked, ok := im.MaskFabricScoped(b, *results[0].FabricScope)
	if !ok {
		t.Fatalf("read of 0x%04X cannot be scoped", attr)
	}
	return masked
}

// wireEntry is one entry of a list on the wire: its FabricIndex and the
// context tags of its fields.
type wireEntry struct {
	fabric uint8
	tags   []uint32
}

// wireEntries decodes a list on the wire into its entries.
func wireEntries(t *testing.T, b []byte) []wireEntry {
	t.Helper()
	dec := tlv.NewDecoder(b)
	if el, err := dec.Next(); err != nil || el.Type != tlv.TypeArray {
		t.Fatalf("no list: % X", b)
	}
	var out []wireEntry
	for {
		el, err := dec.Next()
		if err != nil {
			t.Fatal(err)
		}
		if el.IsEndContainer {
			return out
		}
		var e wireEntry
		for depth := 1; depth > 0; {
			f, err := dec.Next()
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case f.IsEndContainer:
				depth--
				continue
			case depth == 1:
				e.tags = append(e.tags, f.Tag.Number)
				if f.Tag.Number == 0xFE {
					e.fabric = uint8(f.Uint)
				}
			}
			if f.IsContainer {
				depth++
			}
		}
		out = append(out, e)
	}
}
